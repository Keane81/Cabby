// Package migrations exposes the SQL pairs embedded into the location binary, so a
// container started from `FROM scratch` can apply its own schema without external
// tooling (research R-06).
package migrations

import "embed"

// FS holds files named `<version>_<name>.up.sql` and `<version>_<name>.down.sql`.
//
//go:embed *.sql
var FS embed.FS
