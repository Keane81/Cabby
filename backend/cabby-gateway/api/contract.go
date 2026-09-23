// Package api holds the canonical published external contract for cabby-gateway.
package api

import _ "embed"

// OpenAPIDocument is the canonical OpenAPI 3.1 contract for external clients.
// It is embedded at build time so the document served at runtime always matches
// the binary and the committed source of truth.
//
//go:embed openapi.yaml
var OpenAPIDocument []byte
