package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/locationclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/requestid"
	"github.com/rs/zerolog"
)

const (
	locationCabber = "0b6f1e1c-6a2c-4c3e-9d7a-0a1b2c3d4e5f"
	locationBody   = `{"latitude":55.7558,"longitude":37.6173}`
)

// stubLocations is the location service behind the Locations port: a test sets the one answer its
// handler has to report and reads back what reached the port.
type stubLocations struct {
	receivedAt time.Time
	err        error

	calls        int
	sawCabber    string
	sawLatitude  float64
	sawLongitude float64
	sawRequestID string
}

func (s *stubLocations) RecordCabberLocation(ctx context.Context, cabberID string, latitude, longitude float64) (time.Time, error) {
	s.calls++
	s.sawCabber, s.sawLatitude, s.sawLongitude = cabberID, latitude, longitude
	s.sawRequestID = requestid.From(ctx)
	return s.receivedAt, s.err
}

func locationRouter(operations authclient.Operations, locations *stubLocations, logs *bytes.Buffer) (*Router, *Metrics) {
	metrics := NewMetrics()
	logger := zerolog.Nop()
	if logs != nil {
		logger = zerolog.New(logs)
	}
	return NewRouter(func() bool { return true }, logger, metrics, operations, locations), metrics
}

func recordRequest(handler http.Handler, authorization, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, pathCabberLocation, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	handler.ServeHTTP(response, request)
	return response
}

func TestRecordLocationAnswersTheTimeOfReceiptForTheCabberOfTheAccess(t *testing.T) {
	operations := &stubOperations{cabberID: locationCabber}
	locations := &stubLocations{receivedAt: time.Date(2026, 10, 1, 9, 30, 12, 345678000, time.FixedZone("x", 3*3600))}
	handler, _ := locationRouter(operations, locations, nil)

	response := recordRequest(handler, "Bearer "+cabberAccess, locationBody)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
	want := `{"received_at":"2026-10-01T06:30:12.345678Z"}`
	if got := strings.TrimSpace(response.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if operations.sawAccess != cabberAccess {
		t.Errorf("auth saw the access %q", operations.sawAccess)
	}
	// FR-009: the cabber is the one the session confirmed, the coordinates are the ones sent.
	if locations.sawCabber != locationCabber || locations.sawLatitude != 55.7558 || locations.sawLongitude != 37.6173 {
		t.Errorf("location saw %q %v %v", locations.sawCabber, locations.sawLatitude, locations.sawLongitude)
	}
	if locations.sawRequestID == "" || locations.sawRequestID != operations.sawRequestID {
		t.Errorf("the request id did not reach both services alike: auth %q, location %q",
			operations.sawRequestID, locations.sawRequestID)
	}
}

// TestRecordLocationRefusesWithoutAConfirmedAccessAndCallsNoLocation is US1-5 and FR-008.
func TestRecordLocationRefusesWithoutAConfirmedAccessAndCallsNoLocation(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		authorization string
		err           error
		wantStatus    int
		wantCode      ErrorCode
	}{
		{"no header", "", nil, http.StatusUnauthorized, CodeUnauthorized},
		{"another scheme", "Basic dXNlcjpwYXNz", nil, http.StatusUnauthorized, CodeUnauthorized},
		{"an empty token", "Bearer ", nil, http.StatusUnauthorized, CodeUnauthorized},
		{"an access the service refuses", "Bearer " + cabberAccess, authclient.ErrUnauthorized, http.StatusUnauthorized, CodeUnauthorized},
		{"auth unavailable", "Bearer " + cabberAccess, authclient.ErrUnavailable, http.StatusServiceUnavailable, CodeServiceUnavailable},
		{"auth failing", "Bearer " + cabberAccess, authclient.ErrInternal, http.StatusInternalServerError, CodeInternalError},
	} {
		operations := &stubOperations{cabberID: locationCabber, err: tc.err}
		locations := &stubLocations{}
		handler, _ := locationRouter(operations, locations, nil)

		response := recordRequest(handler, tc.authorization, locationBody)

		if response.Code != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", tc.desc, response.Code, tc.wantStatus)
			continue
		}
		if got := decodeError(t, response.Body.Bytes()).Code; got != tc.wantCode {
			t.Errorf("%s: code = %q, want %q", tc.desc, got, tc.wantCode)
		}
		if locations.calls != 0 {
			t.Errorf("%s: location was called %d times", tc.desc, locations.calls)
		}
	}
}

// TestRecordLocationRefusalIsTheOneOfTheOtherSessionOperations: the 401 of a record cannot be told
// from the 401 of an exit by a client (FR-008, spec 003 FR-016).
func TestRecordLocationRefusalIsTheOneOfTheOtherSessionOperations(t *testing.T) {
	operations := &stubOperations{err: authclient.ErrUnauthorized}
	handler, _ := locationRouter(operations, &stubLocations{}, nil)

	record := recordRequest(handler, "Bearer "+cabberAccess, locationBody)
	exit := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
	request.Header.Set("Authorization", "Bearer "+cabberAccess)
	handler.ServeHTTP(exit, request)

	if record.Code != exit.Code || record.Body.String() != exit.Body.String() {
		t.Errorf("record answered %d %s, exit answered %d %s", record.Code, record.Body, exit.Code, exit.Body)
	}
}

// TestRecordLocationRejectsABodyBeforeLocationAndNamesTheField is US1-3 and US1-4 at the gateway.
func TestRecordLocationRejectsABodyBeforeLocationAndNamesTheField(t *testing.T) {
	for _, tc := range []struct {
		desc, body, wantField string
	}{
		{"latitude missing", `{"longitude":1}`, "latitude"},
		{"longitude missing", `{"latitude":1}`, "longitude"},
		{"latitude null", `{"latitude":null,"longitude":1}`, "latitude"},
		{"latitude a string", `{"latitude":"55.7","longitude":1}`, "latitude"},
		{"longitude a string", `{"latitude":1,"longitude":"37.6"}`, "longitude"},
		{"longitude a boolean", `{"latitude":1,"longitude":true}`, "longitude"},
		{"latitude an object", `{"latitude":{},"longitude":1}`, "latitude"},
		{"latitude beyond a float", `{"latitude":1e999,"longitude":1}`, "latitude"},
		{"both wrong: latitude first", `{"latitude":"x","longitude":"y"}`, "latitude"},
		{"empty object", `{}`, "latitude"},
		{"not JSON", `latitude=1`, ""},
		{"not an object", `[1,2]`, ""},
		{"empty body", ``, ""},
		{"a second object", locationBody + locationBody, ""},
		{"an unknown property", `{"latitude":1,"longitude":1,"speed":3}`, ""},
		// FR-009: a body cannot name the cabber the record is made for.
		{"a property naming the cabber", `{"cabber_id":"` + locationCabber + `","latitude":1,"longitude":1}`, ""},
		{"over one kibibyte", `{"latitude":1,"longitude":1,"pad":"` + strings.Repeat("x", 1100) + `"}`, ""},
	} {
		operations := &stubOperations{cabberID: locationCabber}
		locations := &stubLocations{}
		handler, _ := locationRouter(operations, locations, nil)

		response := recordRequest(handler, "Bearer "+cabberAccess, tc.body)

		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", tc.desc, response.Code, response.Body)
			continue
		}
		detail := decodeError(t, response.Body.Bytes())
		if detail.Code != CodeInvalidRequest || detail.Field != tc.wantField {
			t.Errorf("%s: code %q field %q, want invalid_request/%q", tc.desc, detail.Code, detail.Field, tc.wantField)
		}
		if locations.calls != 0 {
			t.Errorf("%s: location was called", tc.desc)
		}
	}
}

func TestRecordLocationMapsTheAnswersOfLocation(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		err        error
		wantStatus int
		wantCode   ErrorCode
		wantField  string
	}{
		{"latitude out of range", locationclient.Invalid{Field: "latitude", Reason: "out_of_range"}, http.StatusBadRequest, CodeInvalidRequest, "latitude"},
		{"longitude out of range", locationclient.Invalid{Field: "longitude", Reason: "out_of_range"}, http.StatusBadRequest, CodeInvalidRequest, "longitude"},
		{"a rejection naming no field", locationclient.Invalid{}, http.StatusBadRequest, CodeInvalidRequest, ""},
		{"location unavailable", locationclient.ErrUnavailable, http.StatusServiceUnavailable, CodeServiceUnavailable, ""},
		{"location failing", locationclient.ErrInternal, http.StatusInternalServerError, CodeInternalError, ""},
	} {
		handler, _ := locationRouter(&stubOperations{cabberID: locationCabber}, &stubLocations{err: tc.err}, nil)

		response := recordRequest(handler, "Bearer "+cabberAccess, locationBody)

		if response.Code != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", tc.desc, response.Code, tc.wantStatus)
			continue
		}
		detail := decodeError(t, response.Body.Bytes())
		if detail.Code != tc.wantCode || detail.Field != tc.wantField {
			t.Errorf("%s: code %q field %q, want %q/%q", tc.desc, detail.Code, detail.Field, tc.wantCode, tc.wantField)
		}
	}
}

// TestRecordLocationNeverAnswersTooFrequent is US1-6 and FR-007: any number of requests in a row are
// each answered 201 and each reaches location; nothing in the gateway counts them against a limit.
func TestRecordLocationNeverAnswersTooFrequent(t *testing.T) {
	operations := &stubOperations{cabberID: locationCabber}
	locations := &stubLocations{receivedAt: time.Now()}
	handler, _ := locationRouter(operations, locations, nil)

	for index := range 300 {
		if response := recordRequest(handler, "Bearer "+cabberAccess, locationBody); response.Code != http.StatusCreated {
			t.Fatalf("request %d = %d, want 201", index, response.Code)
		}
	}
	if locations.calls != 300 {
		t.Fatalf("location was called %d times, want 300", locations.calls)
	}
}

func TestOtherMethodsOnTheLocationPathAreNotAllowed(t *testing.T) {
	locations := &stubLocations{}
	handler, _ := locationRouter(&stubOperations{cabberID: locationCabber}, locations, nil)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(method, pathCabberLocation, strings.NewReader(locationBody))
		request.Header.Set("Authorization", "Bearer "+cabberAccess)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, response.Code)
			continue
		}
		if allow := response.Header().Get("Allow"); allow != http.MethodPost {
			t.Errorf("%s: Allow = %q, want POST", method, allow)
		}
		if got := decodeError(t, response.Body.Bytes()).Code; got != CodeMethodNotAllowed {
			t.Errorf("%s: code = %q", method, got)
		}
	}
	if locations.calls != 0 {
		t.Error("a method the contract does not publish reached location")
	}
}

// TestRecordLocationCountsTheOperationAndTheCallsBehindIt checks the series the dashboard reads.
func TestRecordLocationCountsTheOperationAndTheCallsBehindIt(t *testing.T) {
	operations := &stubOperations{cabberID: locationCabber}
	handler, metrics := locationRouter(operations, &stubLocations{receivedAt: time.Now()}, nil)

	recordRequest(handler, "Bearer "+cabberAccess, locationBody)
	recordRequest(handler, "", locationBody)

	exported := scrape(t, metrics)
	for _, want := range []string{
		`cabby_gateway_cabber_requests_total{operation="record_location",outcome="success"} 1`,
		`cabby_gateway_cabber_requests_total{operation="record_location",outcome="unauthorized"} 1`,
		`cabby_gateway_cabber_dependency_duration_seconds_count{operation="record_location"} 1`,
		`cabby_gateway_cabber_dependency_duration_seconds_count{operation="verify_session"} 1`,
	} {
		if !strings.Contains(exported, want) {
			t.Errorf("missing %q in\n%s", want, exported)
		}
	}
}

// TestNoSinkOfTheGatewayCarriesACoordinate is FR-011 and SC-006 on the public side: whatever the
// request was — accepted, rejected by the body, rejected by location — no response, log line or metric
// repeats the pair the client sent.
func TestNoSinkOfTheGatewayCarriesACoordinate(t *testing.T) {
	const (
		latText = "12.3456789"
		lonText = "98.7654321"
		body    = `{"latitude":` + latText + `,"longitude":` + lonText + `}`
	)
	var logs bytes.Buffer
	operations := &stubOperations{cabberID: locationCabber}
	locations := &stubLocations{receivedAt: time.Now()}
	handler, metrics := locationRouter(operations, locations, &logs)

	var answers []string
	record := func(authorization, payload string) {
		answers = append(answers, recordRequest(handler, authorization, payload).Body.String())
	}
	record("Bearer "+cabberAccess, body)
	record("Bearer "+cabberAccess, `{"latitude":`+latText+`,"longitude":"`+lonText+`"}`)
	record("Bearer "+cabberAccess, `{"latitude":`+latText+`,"longitude":`+lonText+`,"cabber_id":"x"}`)
	locations.err = locationclient.Invalid{Field: "latitude", Reason: "out_of_range"}
	record("Bearer "+cabberAccess, body)
	locations.err = locationclient.ErrUnavailable
	record("Bearer "+cabberAccess, body)
	record("", body)

	sinks := append([]string{logs.String(), scrape(t, metrics)}, answers...)
	for _, sink := range sinks {
		for _, value := range []string{latText, lonText, "12.34567", "98.76543"} {
			if strings.Contains(sink, value) {
				t.Errorf("a sink repeats a coordinate: %s", sink)
			}
		}
	}
	if !json.Valid([]byte(answers[0])) {
		t.Errorf("the answer is not JSON: %s", answers[0])
	}
}
