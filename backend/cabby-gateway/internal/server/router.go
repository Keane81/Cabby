package server

import (
	"net/http"
	"strings"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/rs/zerolog"
)

const (
	pathHealth   = "/healthz"
	pathContract = "/openapi.yaml"
	// The cabber operations of the published contract: the account of a cabber and the sessions it
	// opens.
	pathCabbers       = "/cabbers"
	pathCabberSession = "/cabber/session"
)

// Router dispatches public-port requests to the operations of the published contract and rejects
// everything else with the unified error envelope. Only the cabber operations and the
// health-check are counted in its metrics.
type Router struct {
	mux        *http.ServeMux
	ready      func() bool
	logger     zerolog.Logger
	observer   *Metrics
	operations authclient.Operations
}

// NewRouter builds the public-port handler. observer may be nil when metrics
// are not collected; operations is the auth service the cabber operations run on.
func NewRouter(ready func() bool, logger zerolog.Logger, observer *Metrics, operations authclient.Operations) *Router {
	rt := &Router{mux: http.NewServeMux(), ready: ready, logger: logger, observer: observer, operations: operations}

	// One line per operation of the contract: the mux owns the method check and the Allow header,
	// so a new operation is a new line here and nothing else.
	rt.mux.HandleFunc(http.MethodGet+" "+pathHealth, func(w http.ResponseWriter, _ *http.Request) {
		handleHealth(w, rt.ready, rt.logger, rt.observer)
	})
	rt.mux.HandleFunc(http.MethodGet+" "+pathContract, func(w http.ResponseWriter, _ *http.Request) {
		serveContract(w)
	})
	rt.mux.HandleFunc(http.MethodPost+" "+pathCabbers, rt.counted(operationRegister, rt.registerCabber))
	rt.mux.HandleFunc(http.MethodPost+" "+pathCabberSession, rt.counted(operationCreateSession, rt.createCabberSession))
	rt.mux.HandleFunc(http.MethodDelete+" "+pathCabberSession, rt.counted(operationDeleteSession, rt.deleteCabberSession))
	return rt
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	probe := r
	if r.Method == http.MethodHead {
		// A GET pattern of the mux also takes HEAD, which the contract does not publish. A method no
		// route registers makes the mux answer as it does for any other unsupported one.
		probe = r.Clone(r.Context())
		probe.Method = http.MethodTrace
	}
	handler, pattern := rt.mux.Handler(probe)
	if pattern == "" {
		// The mux answers an unknown path or method in plain text; its answer is turned into the
		// envelope of the contract before it reaches the client.
		handler.ServeHTTP(&envelopeWriter{ResponseWriter: w}, r)
		return
	}
	handler.ServeHTTP(w, r)
}

// envelopeWriter rewrites the two plain-text failures of http.ServeMux — 404 and 405 — into the
// unified error envelope. The Allow header of a 405 is already set by the mux and stays.
type envelopeWriter struct {
	http.ResponseWriter
	rewritten bool
}

func (e *envelopeWriter) WriteHeader(status int) {
	switch status {
	case http.StatusNotFound:
		e.rewritten = true
		writeUnknownOperation(e.ResponseWriter)
	case http.StatusMethodNotAllowed:
		e.rewritten = true
		writeMethodNotAllowed(e.ResponseWriter, publishedMethods(e.Header().Get("Allow")))
	default:
		e.ResponseWriter.WriteHeader(status)
	}
}

func (e *envelopeWriter) Write(body []byte) (int, error) {
	if e.rewritten {
		return len(body), nil
	}
	return e.ResponseWriter.Write(body)
}

// publishedMethods drops the HEAD the mux adds next to every GET: the contract lists GET alone.
func publishedMethods(allow string) string {
	methods := strings.Split(allow, ", ")
	published := methods[:0]
	for _, method := range methods {
		if method != http.MethodHead {
			published = append(published, method)
		}
	}
	return strings.Join(published, ", ")
}
