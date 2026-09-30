package lifecycle

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// fake is a Server that blocks in Serve until Shutdown releases it.
func fake(name string, serveErr error) (Server, chan struct{}) {
	release := make(chan struct{})
	shutdowns := make(chan struct{}, 1)
	return Server{
		Name: name,
		Serve: func() error {
			<-release
			return serveErr
		},
		Shutdown: func(context.Context) error {
			shutdowns <- struct{}{}
			close(release)
			return nil
		},
	}, shutdowns
}

func TestRunStopsEveryServerWhenContextEnds(t *testing.T) {
	first, firstStopped := fake("first", http.ErrServerClosed)
	second, secondStopped := fake("second", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Run(ctx, time.Second, first, second); err != nil {
		t.Fatalf("Run = %v, want nil after a requested stop", err)
	}
	if len(firstStopped) != 1 || len(secondStopped) != 1 {
		t.Fatal("not every server was shut down")
	}
}

func TestRunTreatsAnEndedServerAsFailureAndStopsTheOthers(t *testing.T) {
	ended := Server{
		Name:     "ended",
		Serve:    func() error { return nil },
		Shutdown: func(context.Context) error { return nil },
	}
	other, otherStopped := fake("other", nil)

	err := Run(context.Background(), time.Second, ended, other)
	if err == nil {
		t.Fatal("Run = nil, want a failure for a listener that ended by itself")
	}
	if len(otherStopped) != 1 {
		t.Error("the other server was left running")
	}
}

func TestRunReportsTheCauseOfAFailedServer(t *testing.T) {
	cause := errors.New("accept failed")
	failed := Server{
		Name:     "failed",
		Serve:    func() error { return cause },
		Shutdown: func(context.Context) error { return nil },
	}
	if err := Run(context.Background(), time.Second, failed); !errors.Is(err, cause) {
		t.Fatalf("Run = %v, want it to wrap %v", err, cause)
	}
}

type stubbornGRPC struct {
	release chan struct{}
	forced  chan struct{}
}

func (s *stubbornGRPC) Serve(net.Listener) error { <-s.release; return nil }
func (s *stubbornGRPC) GracefulStop()            { <-s.release }
func (s *stubbornGRPC) Stop()                    { close(s.forced); close(s.release) }

func TestGRPCShutdownForcesStopPastTheDeadline(t *testing.T) {
	stub := &stubbornGRPC{release: make(chan struct{}), forced: make(chan struct{})}
	server := GRPC("grpc", nil, stub)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown = %v", err)
	}
	select {
	case <-stub.forced:
	default:
		t.Fatal("a call that outlived the deadline was not cut")
	}
}
