package server

import (
	"net/http"

	"github.com/Keane81/Cabby/backend/cabby-gateway/api"
)

// contractContentType is the media type of the served OpenAPI YAML document.
const contractContentType = "text/yaml; charset=utf-8"

// serveContract writes the embedded canonical contract. It is read-only, has no
// side effects, and is intentionally not counted in health-check metrics.
func serveContract(w http.ResponseWriter) {
	w.Header().Set("Content-Type", contractContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.OpenAPIDocument)
}
