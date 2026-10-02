package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/config"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/locationclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/server"
	"github.com/Keane81/Cabby/backend/lifecycle"
	"github.com/rs/zerolog"
)

// stopTimeout stays inside the grace period compose allows, so a process that refuses to end is
// killed rather than left hanging.
const stopTimeout = 5 * time.Second

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error().Err(err).Msg("gateway stopped with error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger zerolog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	authClient, err := authclient.Dial(cfg.AuthAddress)
	if err != nil {
		return err
	}
	// Closing runs after the listeners have shut down: a request still in flight may need the
	// dependency until the very end.
	defer func() { _ = authClient.Close() }()
	locationClient, err := locationclient.Dial(cfg.LocationAddress)
	if err != nil {
		return err
	}
	defer func() { _ = locationClient.Close() }()

	publicListener, err := net.Listen("tcp", cfg.PublicAddress)
	if err != nil {
		return err
	}
	metricsListener, err := net.Listen("tcp", cfg.MetricsAddress)
	if err != nil {
		_ = publicListener.Close()
		return err
	}

	metrics := server.NewMetrics()
	var ready atomic.Bool
	ready.Store(true)
	// The health-check reports the drain as soon as the signal arrives, before the listeners stop.
	go func() {
		<-ctx.Done()
		ready.Store(false)
	}()

	logger.Info().
		Str("public_address", publicListener.Addr().String()).
		Str("metrics_address", metricsListener.Addr().String()).
		Str("auth_address", cfg.AuthAddress).
		Str("location_address", cfg.LocationAddress).
		Msg("gateway listening")

	err = lifecycle.Run(ctx, stopTimeout,
		lifecycle.HTTP("public", publicListener, server.NewRouter(ready.Load, logger, metrics, authClient, locationClient)),
		lifecycle.HTTP("metrics", metricsListener, metrics.Handler()),
	)
	if err == nil {
		logger.Info().Msg("gateway stopped")
	}
	return err
}
