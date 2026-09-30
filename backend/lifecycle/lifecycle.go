// Package lifecycle runs the listeners of one service process and stops them together: a signal
// ends all of them gracefully, and a listener that ends on its own takes the others down with it,
// because a process serving half of its contract looks healthy to whatever watches it.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server is one listener of the process: how to run it and how to stop it.
type Server struct {
	Name string
	// Serve blocks until the listener ends. A nil result and http.ErrServerClosed both mean it
	// was stopped.
	Serve func() error
	// Shutdown drains the open calls and gives up on the ones that outlive ctx.
	Shutdown func(ctx context.Context) error
}

// Run serves every server until ctx is done or one of them ends, then stops all of them within
// stopTimeout and waits for each to leave. It returns nil only after a clean stop that ctx asked
// for; a server that ended by itself, even without an error, is a failure.
func Run(ctx context.Context, stopTimeout time.Duration, servers ...Server) error {
	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(servers))
	for _, server := range servers {
		go func() { results <- result{server.Name, server.Serve()} }()
	}

	var failures []error
	pending := len(servers)
	select {
	case <-ctx.Done():
	case ended := <-results:
		pending--
		if ended.err == nil || errors.Is(ended.err, http.ErrServerClosed) {
			ended.err = errors.New("stopped unexpectedly")
		}
		failures = append(failures, fmt.Errorf("%s listener: %w", ended.name, ended.err))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(stopCtx); err != nil {
			failures = append(failures, fmt.Errorf("%s listener: %w", server.Name, err))
		}
	}
	for ; pending > 0; pending-- {
		stopped := <-results
		if stopped.err != nil && !errors.Is(stopped.err, http.ErrServerClosed) {
			failures = append(failures, fmt.Errorf("%s listener: %w", stopped.name, stopped.err))
		}
	}
	return errors.Join(failures...)
}

// HTTP wraps an http.Server with the timeouts every public and metrics listener of the repository
// uses.
func HTTP(name string, listener net.Listener, handler http.Handler) Server {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return Server{
		Name:     name,
		Serve:    func() error { return server.Serve(listener) },
		Shutdown: server.Shutdown,
	}
}

// GRPCServer is the part of *grpc.Server the lifecycle needs; it keeps this module free of the
// grpc dependency.
type GRPCServer interface {
	Serve(net.Listener) error
	GracefulStop()
	Stop()
}

// GRPC wraps a gRPC server. Shutdown drains the open calls and cuts the ones that outlive ctx.
func GRPC(name string, listener net.Listener, server GRPCServer) Server {
	return Server{
		Name:  name,
		Serve: func() error { return server.Serve(listener) },
		Shutdown: func(ctx context.Context) error {
			stopped := make(chan struct{})
			go func() {
				server.GracefulStop()
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-ctx.Done():
				server.Stop()
				<-stopped
			}
			return nil
		},
	}
}
