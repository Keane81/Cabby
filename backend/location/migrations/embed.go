// Package migrations exposes the SQL pairs embedded into the location binary, so a
// container started from `FROM scratch` can apply its own schema without external
// tooling (research R-06).
package migrations

import "embed"

// FS holds files named `<version>_<name>.up.sql` and `<version>_<name>.down.sql`.
//
//go:embed *.sql
var FS embed.FS

// LockKey is the advisory lock key of the location database; it only has to be stable and not shared with
// another pg_advisory_lock user inside it.
const LockKey = int64(0x4c6f_63_6174)
