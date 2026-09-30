//go:build integration

// Repository tests run against a real PostgreSQL: `CABBY_AUTH_DB_URL=postgres://... make -C
// backend/auth test-integration`. Without the variable each case skips itself.
package repo_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/migrate"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmailIsUnique(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	cabbers := repo.NewCabbers(pool)

	const email = "ivan@example.com"
	first, err := cabbers.Create(ctx, repo.Cabber{Name: "Иван", Email: email, PasswordHash: fakeHash("1234")})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if first == "" {
		t.Fatal("Create returned an empty identifier")
	}

	if _, err := cabbers.Create(ctx, repo.Cabber{Name: "Друг", Email: email, PasswordHash: fakeHash("abcd")}); !errors.Is(err, repo.ErrEmailTaken) {
		t.Fatalf("second Create = %v, want ErrEmailTaken", err)
	}

	// The rejected attempt must not have touched the stored account.
	stored, err := cabbers.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if stored.ID != first || stored.Name != "Иван" || stored.PasswordHash != fakeHash("1234") {
		t.Fatalf("failed registration changed the account: %+v", stored)
	}
}

// TestTakenEmailStaysTaken covers FR-027: the address does not free up when its owner can no
// longer create a session, because nothing in v1 removes an account.
func TestTakenEmailStaysTaken(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	cabbers := repo.NewCabbers(pool)
	sessions := repo.NewSessions(pool)

	const email = "petr@example.com"
	id, err := cabbers.Create(ctx, repo.Cabber{Name: "Пётр", Email: email, PasswordHash: fakeHash("1234")})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := now()
	digest := []byte("digest-of-a-session-that-will-die")
	if err := sessions.Create(ctx, repo.Session{
		TokenHash: digest, CabberID: id, CreatedAt: now,
		ExpiresAt: now.Add(time.Hour), LastSeenAt: now,
	}); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if _, err := sessions.Revoke(ctx, digest, now.Add(time.Minute)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := cabbers.Create(ctx, repo.Cabber{Name: "Другой", Email: email, PasswordHash: fakeHash("abcd")}); !errors.Is(err, repo.ErrEmailTaken) {
		t.Fatalf("Create after the owner lost access = %v, want ErrEmailTaken", err)
	}
}

func TestNameLengthCountsCharacters(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	cabbers := repo.NewCabbers(pool)

	long := strings.Repeat("п", 65) // 130 bytes, 65 characters
	if _, err := cabbers.Create(ctx, repo.Cabber{
		Name: long, Email: "long@example.com", PasswordHash: fakeHash("1234"),
	}); err == nil {
		t.Fatal("the database accepted a 65-character name")
	}

	sixtyFour := strings.Repeat("п", 64) // 128 bytes, 64 characters: legal, because length is counted in characters
	if _, err := cabbers.Create(ctx, repo.Cabber{
		Name: sixtyFour, Email: "sixtyfour@example.com", PasswordHash: fakeHash("1234"),
	}); err != nil {
		t.Fatalf("the database rejected a 64-character name: %v", err)
	}

	if _, err := cabbers.Create(ctx, repo.Cabber{
		Name: " Иван", Email: "padded@example.com", PasswordHash: fakeHash("1234"),
	}); err == nil {
		t.Fatal("the database accepted a name with leading whitespace")
	}
}

func TestStoredHashIsNotThePassword(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	cabbers := repo.NewCabbers(pool)

	const plain = "1234"
	if _, err := cabbers.Create(ctx, repo.Cabber{
		Name: "Иван", Email: "hash@example.com", PasswordHash: fakeHash(plain),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stored, err := cabbers.FindByEmail(ctx, "hash@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if strings.Contains(stored.PasswordHash, plain) {
		t.Fatalf("the stored value %q contains the password", stored.PasswordHash)
	}
	if !strings.HasPrefix(stored.PasswordHash, "$argon2id$") {
		t.Fatalf("stored hash %q is not a PHC string", stored.PasswordHash)
	}
}

func TestFindByEmailMissingCabber(t *testing.T) {
	cabbers := repo.NewCabbers(testPool(t))

	if _, err := cabbers.FindByEmail(context.Background(), "ghost@example.com"); !errors.Is(err, repo.ErrCabberNotFound) {
		t.Fatalf("FindByEmail = %v, want ErrCabberNotFound", err)
	}
}

// testPool returns a pool working inside a private schema with 0001_cabber applied, so runs do
// not observe each other's rows.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CABBY_AUTH_DB_URL")
	if dsn == "" {
		t.Skip("CABBY_AUTH_DB_URL is not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse CABBY_AUTH_DB_URL: %v", err)
	}
	schema := fmt.Sprintf("repo_test_%d", time.Now().UnixNano())
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})

	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := migrate.Up(ctx, pool, scripts); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

// fakeHash keeps these tests off Argon2: the repository stores whatever string it is given,
// and a derived digest never contains the password it came from.
func fakeHash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return "$argon2id$v=19$m=16,t=1,p=1$c2FsdA$" + hex.EncodeToString(sum[:])
}

// now truncates to the microsecond precision PostgreSQL keeps, so read-back values compare
// exactly.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
