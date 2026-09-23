package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/api"
	"github.com/rs/zerolog"
)

func TestContractServedByteIdentical(t *testing.T) {
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics())

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != contractContentType {
		t.Fatalf("content type = %q, want %q", got, contractContentType)
	}
	if !bytes.Equal(response.Body.Bytes(), api.OpenAPIDocument) {
		t.Fatal("served contract differs from the embedded canonical document")
	}
}

func TestContractNotCountedAsHealthCheck(t *testing.T) {
	metrics := NewMetrics()
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), metrics)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	assertMetric(t, metrics, "success", 0)
	assertMetric(t, metrics, "failure", 0)
}

func TestContractDescribesHealthAndExcludesMetrics(t *testing.T) {
	doc := string(api.OpenAPIDocument)
	if !strings.Contains(doc, "openapi: 3.1.0") {
		t.Fatal("contract is not OpenAPI 3.1")
	}
	if !contractPathDefined(doc, "/healthz") {
		t.Fatal("contract does not define the health-check path")
	}
	if !contractPathDefined(doc, "/openapi.yaml") {
		t.Fatal("contract does not define the discovery path")
	}
	if contractPathDefined(doc, "/metrics") {
		t.Fatal("internal metrics interface must not be a path in the external contract")
	}
}

// contractPathDefined reports whether p is declared as a path key (two-space
// indented under `paths:`) in the canonical contract. Prose mentions do not count.
func contractPathDefined(doc, p string) bool {
	return regexp.MustCompile(`(?m)^ {2}` + regexp.QuoteMeta(p) + `:$`).MatchString(doc)
}

func TestCanonicalContractMatchesRepositoryReference(t *testing.T) {
	const reference = "../../../../specs/002-gateway-external-contract/contracts/openapi.yaml"
	want, err := os.ReadFile(reference)
	if err != nil {
		t.Fatalf("read reference contract %s: %v", reference, err)
	}
	if !bytes.Equal(api.OpenAPIDocument, want) {
		t.Fatal("canonical api/openapi.yaml drifted from the repository reference contract")
	}
}

func TestRouterPathsAreDefinedInContract(t *testing.T) {
	doc := string(api.OpenAPIDocument)
	for _, path := range []string{pathHealth, pathContract} {
		if !contractPathDefined(doc, path) {
			t.Fatalf("router path %q is not defined in the published contract", path)
		}
	}
}
