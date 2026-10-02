package server

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/api"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
)

// The operations a cabber app would ask for next: a password it changed, a recovery it started, an
// access it restored. None of them is in the published contract, and each of them is a promise the
// feature of spec 003 makes not to answer with anything but the contract's own refusal (FR-026,
// SC-011).
var absentOperations = []struct {
	desc   string
	method string
	path   string
	body   string
}{
	{"a password change", http.MethodPost, "/cabbers/password",
		`{"email":"` + cabberEmail + `","password":"` + cabberPassword + `"}`},
	{"a password recovery", http.MethodPost, "/cabbers/password/recovery",
		`{"email":"` + cabberEmail + `"}`},
	{"a session recovery", http.MethodPost, "/cabber/session/recover",
		`{"email":"` + cabberEmail + `","password":"` + cabberPassword + `"}`},
	{"a recovery read back", http.MethodGet, "/cabber/session/recover", ""},
	{"a password change by a method the path does not serve", http.MethodPut, "/cabbers/password",
		`{"email":"` + cabberEmail + `"}`},
}

// TestAbsentOperationsAnswerUnknownOperation is SC-011: a request for an operation the contract does
// not publish is refused as unknown, not as a method or a body defect — which is what tells a client
// that nothing on this port will ever do it.
func TestAbsentOperationsAnswerUnknownOperation(t *testing.T) {
	operations := &stubOperations{
		cabber:  authclient.Cabber{ID: "cabber-1", Email: cabberEmail},
		session: authclient.Session{AccessToken: cabberAccess},
	}
	handler := routerWith(operations, io.Discard)

	for _, request := range absentOperations {
		response := serveWith(handler, request.method, request.path, request.body)

		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d %s, want 404", request.method, request.path, response.Code, response.Body)
			continue
		}
		if detail := decodeError(t, response.Body.Bytes()); detail.Code != CodeUnknownOperation {
			t.Errorf("%s %s: code = %q, want %q", request.method, request.path, detail.Code, CodeUnknownOperation)
		}
		// An Allow header would say the path is known and only its method is wrong. That is the
		// answer for /cabbers?GET, and a lie for a path no operation owns.
		if allow := response.Header().Get("Allow"); allow != "" {
			t.Errorf("%s %s: Allow = %q, want no header on an unknown operation", request.method, request.path, allow)
		}
	}
	// No side effect: the refusal is the whole answer, and nothing of it is asked of auth.
	if operations.calls != 0 {
		t.Errorf("the service was called %d times, want none", operations.calls)
	}
}

// TestAbsentOperationsAreAbsentFromTheContract keeps the two halves of the promise together: the
// router refuses these operations, and the document a client reads publishes none of them. A path
// added to the contract without a router, or the other way round, fails here.
func TestAbsentOperationsAreAbsentFromTheContract(t *testing.T) {
	document := string(api.OpenAPIDocument)

	for _, request := range absentOperations {
		if contractPathDefined(document, request.path) {
			t.Errorf("%s is defined in the contract although the feature publishes no such operation", request.path)
		}
	}
}

// TestLocationPathPublishesOnlyTheRecord keeps the deliberate absence of any way to read, change or
// remove location records (spec 004 Assumptions): the path of the contract declares POST and nothing
// else, and no operation reads like a read of locations.
func TestLocationPathPublishesOnlyTheRecord(t *testing.T) {
	document := string(api.OpenAPIDocument)
	start := strings.Index(document, "\n  "+pathCabberLocation+":\n")
	if start < 0 {
		t.Fatalf("the contract does not define %s", pathCabberLocation)
	}
	block := document[start+1:]
	if next := regexp.MustCompile(`(?m)^ {2}/\S+:$`).FindStringIndex(block[1:]); next != nil {
		block = block[:next[0]+1]
	}
	for _, method := range []string{"get", "put", "patch", "delete", "head"} {
		if regexp.MustCompile(`(?m)^ {4}` + method + `:$`).MatchString(block) {
			t.Errorf("the contract declares %s on %s", method, pathCabberLocation)
		}
	}
	if !regexp.MustCompile(`(?m)^ {4}post:$`).MatchString(block) {
		t.Errorf("the contract does not declare POST on %s", pathCabberLocation)
	}
	for _, operation := range []string{"getCabberLocation", "listCabberLocations", "deleteCabberLocation", "updateCabberLocation"} {
		if contractDeclaresOperation(document, operation) {
			t.Errorf("the contract declares operation %q", operation)
		}
	}
}
