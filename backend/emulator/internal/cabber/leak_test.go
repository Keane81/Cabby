package cabber

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
)

// TestLogsCarryNoSecretsNorPositions drives a cabber through every failing path at debug level and
// asserts that no password, token, email, name or coordinate reaches a log line (FR-015).
func TestLogsCarryNoSecretsNorPositions(t *testing.T) {
	var logs bytes.Buffer
	h := newHarness(zerolog.New(&logs).Level(zerolog.DebugLevel), nil)
	h.api.register = func(call int, email string) gateway.Kind {
		if call == 1 {
			return gateway.Conflict // forces the second address
		}
		return gateway.OK
	}
	h.api.record = func(call int, p sentPosition) gateway.Kind {
		switch call {
		case 2:
			return gateway.InvalidRequest // the one defect that is logged
		case 3:
			return gateway.Unauthorized // forces a re-login
		}
		return gateway.OK
	}
	h.stopAfterSends(8)
	h.run(0)

	// A second cabber that never starts: registration fails for good.
	failing := newHarness(zerolog.New(&logs).Level(zerolog.DebugLevel), nil)
	failing.api.register = func(int, string) gateway.Kind { return gateway.InvalidRequest }
	failing.run(0)

	if logs.Len() == 0 {
		t.Fatal("no log lines were produced, so the test proves nothing")
	}
	secrets := []string{
		h.cabber.id.Password, h.cabber.id.Email(0), h.cabber.id.Email(1), h.cabber.id.Name,
		"token-1", "token-2", "Bearer",
	}
	for _, p := range h.api.sent {
		for _, v := range []float64{p.lat, p.lon} {
			secrets = append(secrets, strconv.FormatFloat(v, 'f', -1, 64), fmt.Sprint(v))
		}
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(logs.String(), secret) {
			t.Errorf("log output contains %q:\n%s", secret, logs.String())
		}
	}
}
