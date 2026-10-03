package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/config"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/httpapi"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/store"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/ui"
	"github.com/Keane81/Cabby/backend/lifecycle"
)

// stopTimeout stays inside the 10 s grace period compose allows.
const stopTimeout = 5 * time.Second

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error().Err(err).Msg("dbviewer stopped with error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger zerolog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	authPool, err := store.NewPool(ctx, cfg.AuthDatabaseURL)
	if err != nil {
		return errors.New("dbviewer: the auth database address is not usable")
	}
	defer authPool.Close()
	locationPool, err := store.NewPool(ctx, cfg.LocationDatabaseURL)
	if err != nil {
		return errors.New("dbviewer: the location database address is not usable")
	}
	defer locationPool.Close()

	backend := store.New([]store.Source{
		{Name: "auth", Pool: authPool},
		{Name: "location", Pool: locationPool},
	}, store.DefaultQueryTimeout)

	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return err
	}
	logger.Info().Str("http_address", listener.Addr().String()).Msg("dbviewer listening")

	err = lifecycle.Run(ctx, stopTimeout,
		lifecycle.HTTP("http", listener, httpapi.New(backend, ui.FS, logger)),
	)
	if err == nil {
		logger.Info().Msg("dbviewer stopped")
	}
	return err
}
