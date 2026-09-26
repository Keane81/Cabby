// Package password hashes and verifies cabber passwords with Argon2id and stores the result
// as a PHC string, so the algorithm and its parameters travel with the hash (research R-07).
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	algorithm = "argon2id"
	// phcFields is the number of `$`-separated parts of a PHC string: an empty leading
	// part, the algorithm, the version, the parameters, the salt and the key.
	phcFields = 6
)

// Params are the Argon2id cost parameters. They are encoded in the PHC string, which keeps
// old hashes verifiable after the defaults change.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // iterations
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}

// Default is the lower bound of the OWASP recommendation for argon2id, chosen to fit the
// memory and latency budget of v1 (R-07, plan.md §Constraints).
var Default = Params{Memory: 19 * 1024, Time: 2, Threads: 1, KeyLen: 32, SaltLen: 16}

// slots bounds concurrent derivations: each one holds Memory KiB, so two at a time keep the
// service inside its RSS budget.
var slots = make(chan struct{}, 2)

// Hash derives a PHC string for plain. Callers pass Default; a smaller Params is expected in
// tests, where full-cost derivation would dominate the run time.
func Hash(ctx context.Context, plain string, params Params) (string, error) {
	if params.SaltLen == 0 || params.KeyLen == 0 {
		return "", errors.New("password: SaltLen and KeyLen must be positive")
	}
	release, err := reserve(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	salt := make([]byte, params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, params.Time, params.Memory, params.Threads, params.KeyLen)
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		algorithm, argon2.Version, params.Memory, params.Time, params.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify recomputes the key with the parameters stored in encoded and compares them in
// constant time. A malformed or unknown hash shape is an error, never a silent rejection,
// so a corrupted record cannot be mistaken for a wrong password.
func Verify(ctx context.Context, encoded, plain string) (bool, error) {
	params, salt, key, err := parse(encoded)
	if err != nil {
		return false, err
	}
	release, err := reserve(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	candidate := argon2.IDKey([]byte(plain), salt, params.Time, params.Memory, params.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

var (
	errMalformed = errors.New("password: stored hash is malformed")
	errParams    = errors.New("password: stored hash has unusable parameters")
)

func parse(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != phcFields || parts[0] != "" || parts[1] != algorithm {
		return Params{}, nil, nil, errMalformed
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(parts[2], "v="), 10, 32)
	if err != nil || version != argon2.Version {
		return Params{}, nil, nil, errMalformed
	}
	params, err := parseParams(parts[3])
	if err != nil {
		return Params{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Params{}, nil, nil, errMalformed
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, errMalformed
	}
	return params, salt, key, nil
}

func parseParams(field string) (Params, error) {
	parsed := Params{}
	for _, pair := range strings.Split(field, ",") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			return Params{}, errMalformed
		}
		number, err := strconv.ParseUint(value, 10, 32)
		if err != nil || number == 0 {
			return Params{}, errParams
		}
		switch name {
		case "m":
			parsed.Memory = uint32(number)
		case "t":
			parsed.Time = uint32(number)
		case "p":
			parsed.Threads = uint8(number)
		default:
			return Params{}, errMalformed
		}
	}
	if parsed.Memory == 0 || parsed.Time == 0 || parsed.Threads == 0 {
		return Params{}, errParams
	}
	return parsed, nil
}

func reserve(ctx context.Context) (func(), error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("password: waiting for a derivation slot: %w", ctx.Err())
	}
}
