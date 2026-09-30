package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/Keane81/Cabby/backend/auth/internal/config"
	"github.com/Keane81/Cabby/backend/auth/internal/grpcserver"
	"github.com/Keane81/Cabby/backend/auth/internal/migrate"
	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/service"
	"github.com/Keane81/Cabby/backend/auth/migrations"
	"github.com/Keane81/Cabby/backend/lifecycle"
)

const (
	// migrateAttempts and migrateDelay bound how long auth waits for PostgreSQL: compose brings
	// both containers up at the same moment, so the first attempts are expected to fail (R-06).
	migrateAttempts = 30
	migrateDelay    = time.Second

	// stopTimeout stays inside the 10 s grace period compose allows, so a process that refuses to
	// end is killed rather than left hanging.
	stopTimeout = 5 * time.Second
)

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply the embedded migrations and exit")
	flag.Parse()

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, *migrateOnly); err != nil {
		logger.Error().Err(err).Msg("auth stopped with error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger zerolog.Logger, migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// pgxpool.New only parses the DSN, and the text of its failure echoes the string it was
	// handed — the password of the database account among it. The cause is named, not repeated
	// (FR-004).
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("auth: the database address is not usable")
	}
	// Closing runs after both listeners have stopped, so a request still in flight keeps the
	// storage it needs until the very end.
	defer pool.Close()

	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		return err
	}
	if err := migrate.UpWaiting(ctx, pool, scripts, migrateAttempts, migrateDelay); err != nil {
		return err
	}
	// The line is the report quickstart §3 reads back: compose starts the service next to a database
	// that may still be coming up, and the schema it applies is what the first request depends on.
	logger.Info().Msg("migrations applied")
	if migrateOnly {
		return nil
	}
	return serve(ctx, logger, cfg, pool)
}

// serve wires the case layer onto its two transports and runs until a signal arrives or one of the
// listeners ends.
func serve(ctx context.Context, logger zerolog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	metrics := grpcserver.NewMetrics()
	svc := service.New(
		metrics.Cabbers(repo.NewCabbers(pool)),
		metrics.Sessions(repo.NewSessions(pool)),
		password.Default,
		logger,
		time.Now,
	)

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return err
	}
	metricsListener, err := net.Listen("tcp", cfg.MetricsAddress)
	if err != nil {
		_ = grpcListener.Close()
		return err
	}

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(metrics.UnaryInterceptor(logger)))
	grpcserver.NewServer(svc).Register(grpcServer)

	// The purge shares the lifetime of the process: ending the serve context is what stops it, so
	// a shutdown never leaves a delete running against a pool that is about to close.
	servingCtx, stopServing := context.WithCancel(ctx)
	defer stopServing()
	go svc.RunCleanup(servingCtx, service.CleanupInterval)

	logger.Info().
		Str("grpc_address", grpcListener.Addr().String()).
		Str("metrics_address", metricsListener.Addr().String()).
		Msg("auth listening")

	err = lifecycle.Run(ctx, stopTimeout,
		lifecycle.GRPC("grpc", grpcListener, grpcServer),
		lifecycle.HTTP("metrics", metricsListener, metrics.Handler()),
	)
	if err == nil {
		logger.Info().Msg("auth stopped")
	}
	return err
}
