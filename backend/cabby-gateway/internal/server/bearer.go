package server

import (
	"net/http"
	"strings"
)

// bearerScheme is the only authorization scheme the published contract describes.
const bearerScheme = "Bearer"

// bearerToken reads the access an account operation is to be carried out under (FR-016). A header
// that cannot name a bearer token is answered here, before the auth service is called: such a
// request brings no credential the service could confirm, so sending it on would report a state the
// client never claimed. The answer is the one of an access that is not valid, and it has to stay
// indistinguishable from an expired or a foreign one (FR-018, SC-003).
func bearerToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	scheme, access, hasAccess := strings.Cut(r.Header.Get("Authorization"), " ")
	if !hasAccess || !strings.EqualFold(scheme, bearerScheme) || access == "" {
		writeInvalidAccess(w)
		return "", false
	}
	return access, true
}
