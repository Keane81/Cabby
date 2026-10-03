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

	"github.com/Keane81/Cabby/backend/lifecycle"
	"github.com/Keane81/Cabby/backend/location/internal/config"
	"github.com/Keane81/Cabby/backend/location/internal/grpcserver"
	"github.com/Keane81/Cabby/backend/location/internal/metrics"
	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/Keane81/Cabby/backend/location/internal/service"
	"github.com/Keane81/Cabby/backend/location/migrations"
	"github.com/Keane81/Cabby/backend/platform/migrate"
)

const (
	// migrateAttempts and migrateDelay bound how long location waits for PostgreSQL: compose brings
	// both containers up at the same moment, so the first attempts are expected to fail.
	migrateAttempts = 30
	migrateDelay    = time.Second

	// maxConnections bounds the pool (plan.md §Constraints): when the database cannot keep up, calls
	// wait for a connection until their deadline and then fail, instead of piling up.
	maxConnections = 16

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
		logger.Error().Err(err).Msg("location stopped with error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger zerolog.Logger, migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// pgxpool.ParseConfig only parses the DSN, and the text of its failure echoes the string it was
	// handed — the password of the database role among it. The cause is named, not repeated.
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("location: the database address is not usable")
	}
	poolConfig.MaxConns = maxConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("location: the database address is not usable")
	}
	// Closing runs after both listeners have stopped, so a request still in flight keeps the
	// storage it needs until the very end.
	defer pool.Close()

	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		return err
	}
	if err := migrate.New(migrations.LockKey).UpWaiting(ctx, pool, scripts, migrateAttempts, migrateDelay); err != nil {
		return err
	}
	// The line is the report quickstart §2 reads back.
	logger.Info().Msg("migrations applied")
	if migrateOnly {
		return nil
	}
	return serve(ctx, logger, cfg, pool)
}

// serve wires the case layer onto its two transports and runs until a signal arrives or one of the
// listeners ends.
func serve(ctx context.Context, logger zerolog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	metrics := metrics.New()
	svc := service.New(metrics.Locations(repo.NewLocations(pool)), logger, time.Now)

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

	logger.Info().
		Str("grpc_address", grpcListener.Addr().String()).
		Str("metrics_address", metricsListener.Addr().String()).
		Msg("location listening")

	err = lifecycle.Run(ctx, stopTimeout,
		lifecycle.GRPC("grpc", grpcListener, grpcServer),
		lifecycle.HTTP("metrics", metricsListener, metrics.Handler()),
	)
	if err == nil {
		logger.Info().Msg("location stopped")
	}
	return err
}
