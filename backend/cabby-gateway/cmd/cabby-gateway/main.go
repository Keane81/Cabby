package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/server"
	"github.com/rs/zerolog"
)

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
	var ready atomic.Bool
	publicPort := os.Getenv("CABBY_GATEWAY_PORT")
	if publicPort == "" {
		publicPort = "8080"
	}
	publicListener, err := net.Listen("tcp", net.JoinHostPort("", publicPort))
	if err != nil {
		return err
	}
	metricsListener, err := net.Listen("tcp", ":9091")
	if err != nil {
		_ = publicListener.Close()
		return err
	}
	metrics := server.NewMetrics()
	ready.Store(true)

	publicHTTP := &http.Server{
		Handler:           server.NewRouter(ready.Load, logger, metrics),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	metricsHTTP := &http.Server{
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	type serveResult struct {
		name string
		err  error
	}
	serveError := func(result serveResult) error {
		if errors.Is(result.err, http.ErrServerClosed) {
			return nil
		}
		if result.err == nil {
			return fmt.Errorf("%s listener stopped unexpectedly", result.name)
		}
		return fmt.Errorf("%s listener: %w", result.name, result.err)
	}
	serveResults := make(chan serveResult, 2)
	go func() { serveResults <- serveResult{"public", publicHTTP.Serve(publicListener)} }()
	go func() { serveResults <- serveResult{"metrics", metricsHTTP.Serve(metricsListener)} }()
	logger.Info().
		Str("public_address", publicListener.Addr().String()).
		Str("metrics_address", metricsListener.Addr().String()).
		Msg("gateway listening")

	var firstResult *serveResult
	select {
	case <-ctx.Done():
	case result := <-serveResults:
		firstResult = &result
	}
	ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := errors.Join(publicHTTP.Shutdown(shutdownCtx), metricsHTTP.Shutdown(shutdownCtx))

	resultsToRead := 2
	if firstResult != nil {
		resultsToRead--
		shutdownErr = errors.Join(shutdownErr, serveError(*firstResult))
	}
	for range resultsToRead {
		result := <-serveResults
		shutdownErr = errors.Join(shutdownErr, serveError(result))
	}
	if shutdownErr == nil {
		logger.Info().Msg("gateway stopped")
	}
	return shutdownErr
}
