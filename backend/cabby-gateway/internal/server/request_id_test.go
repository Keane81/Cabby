package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/rs/zerolog"
)

// requestIDPattern is the shape of the identifier CHK034 fixes: 16 hex characters, which is what
// auth reads back from the metadata of the call.
var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestCabberRequestsAreLoggedByOneID covers the gateway half of the substitution for tracing: the
// line of a request names it by the same identifier the operation was given, and two requests never
// share one.
func TestCabberRequestsAreLoggedByOneID(t *testing.T) {
	var logs bytes.Buffer
	operations := &stubOperations{cabber: authclient.Cabber{ID: "cabber-1", Email: cabberEmail}}
	handler := NewRouter(func() bool { return true }, zerolog.New(&logs), NewMetrics(), operations)
	body := `{"name":"` + cabberName + `","email":"` + cabberEmail + `","password":"` + cabberPassword + `"}`

	serveWith(handler, http.MethodPost, pathCabbers, body)
	first := loggedEvent(t, &logs)
	serveWith(handler, http.MethodPost, pathCabbers, body)
	second := loggedEvent(t, &logs)

	firstID, secondID := loggedID(t, first), loggedID(t, second)
	if firstID == secondID {
		t.Errorf("two requests shared the identifier %q", firstID)
	}
	for _, event := range []map[string]any{first, second} {
		if event["operation"] != operationRegister {
			t.Errorf("operation = %v, want %q", event["operation"], operationRegister)
		}
		if status, ok := event["status"].(float64); !ok || int(status) != http.StatusCreated {
			t.Errorf("status = %v, want 201", event["status"])
		}
	}
	// The identifier of the line is the one the port was handed, so the value that leaves the
	// process towards auth is the value the log searches by.
	if operations.sawRequestID != secondID {
		t.Errorf("the operation saw %q, want the identifier of its own line %q", operations.sawRequestID, secondID)
	}
}

// TestDeleteSessionLineNamesTheRequestThePortSaw runs the same rule over the path that reads a credential
// out of a header instead of a body.
func TestDeleteSessionLineNamesTheRequestThePortSaw(t *testing.T) {
	var logs bytes.Buffer
	operations := &stubOperations{}
	handler := routerWith(operations, &logs)

	response := deleteSessionRequest(t, handler, "Bearer "+cabberAccess)
	if response.Code != http.StatusNoContent {
		t.Fatalf("DELETE /cabber/session = %d, want 204", response.Code)
	}
	event := loggedEvent(t, &logs)
	if event["operation"] != operationDeleteSession {
		t.Errorf("operation = %v, want %q", event["operation"], operationDeleteSession)
	}
	if id := loggedID(t, event); id != operations.sawRequestID {
		t.Errorf("the operation saw %q, want the identifier of its own line %q", operations.sawRequestID, id)
	}
}

// TestARejectedRequestStillNamesItself keeps the identifier on the branch no dependency is reached
// for: a 400 has no gRPC call behind it, and its line still carries the value a search is made by.
func TestARejectedRequestStillNamesItself(t *testing.T) {
	var logs bytes.Buffer
	operations := &stubOperations{}
	handler := NewRouter(func() bool { return true }, zerolog.New(&logs), NewMetrics(), operations)

	response := serveWith(handler, http.MethodPost, pathCabbers, `{"name":`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	event := loggedEvent(t, &logs)
	loggedID(t, event)
	if event["level"] != "warn" {
		t.Errorf("level = %v, want a warning for a 400", event["level"])
	}
}

// loggedEvent reads the last line the router wrote.
func loggedEvent(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatalf("last log line %q is not JSON: %v", lines[len(lines)-1], err)
	}
	return event
}

// loggedID reads the identifier of one logged request and refuses a value of another shape.
func loggedID(t *testing.T, event map[string]any) string {
	t.Helper()
	id, _ := event["request_id"].(string)
	if !requestIDPattern.MatchString(id) {
		t.Fatalf("request_id = %q, want 16 hex characters: %v", id, event)
	}
	return id
}
