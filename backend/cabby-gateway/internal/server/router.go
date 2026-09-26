package server

import (
	"net/http"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/rs/zerolog"
)

const (
	pathHealth   = "/healthz"
	pathContract = "/openapi.yaml"
	// The cabber operations of the published contract: the account of a cabber and the accesses it
	// opens.
	pathCabbers       = "/cabbers"
	pathCabberSession = "/cabber/session"
)

// Router dispatches public-port requests by path and method. It serves the
// health-check operation, the published external contract and the cabber operations,
// and rejects everything else with the unified error envelope. Only the cabber
// operations and the health-check are counted in its metrics.
type Router struct {
	ready      func() bool
	logger     zerolog.Logger
	observer   *Metrics
	operations authclient.Operations
}

// NewRouter builds the public-port handler. observer may be nil when metrics
// are not collected; operations is the auth service the cabber operations run on.
func NewRouter(ready func() bool, logger zerolog.Logger, observer *Metrics, operations authclient.Operations) *Router {
	return &Router{ready: ready, logger: logger, observer: observer, operations: operations}
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
	case pathCabbers:
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		rt.counted(operationRegister, w, r, rt.registerCabber)
	case pathCabberSession:
		switch r.Method {
		case http.MethodPost:
			rt.counted(operationLogin, w, r, rt.signInCabber)
		case http.MethodDelete:
			rt.counted(operationLogout, w, r, rt.signOutCabber)
		default:
			writeMethodNotAllowed(w, http.MethodPost+", "+http.MethodDelete)
		}
	default:
		writeUnknownOperation(w)
	}
}
