package server

import (
	"net/http"

	"github.com/rs/zerolog"
)

const (
	pathHealth   = "/healthz"
	pathContract = "/openapi.yaml"
)

// Router dispatches public-port requests by path and method. It serves the
// health-check operation and the published external contract, and rejects
// everything else with the unified error envelope. Only requests that reach the
// health-check operation are counted in its metrics.
type Router struct {
	ready    func() bool
	logger   zerolog.Logger
	observer *Metrics
}

// NewRouter builds the public-port handler. observer may be nil when metrics
// are not collected.
func NewRouter(ready func() bool, logger zerolog.Logger, observer *Metrics) *Router {
	return &Router{ready: ready, logger: logger, observer: observer}
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case pathHealth:
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		handleHealth(w, rt.ready, rt.logger, rt.observer)
	case pathContract:
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		serveContract(w)
	default:
		writeUnknownOperation(w)
	}
}
