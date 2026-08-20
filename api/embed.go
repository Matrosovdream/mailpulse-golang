// Package api carries the hand-written OpenAPI description of the HTTP surface.
//
// The description lives here as YAML rather than being generated from struct
// tags, so it is the one place to state what an endpoint means rather than only
// what shape it has. It is embedded into the binary so the docs endpoint serves
// the same bytes the repository holds, with no file to ship alongside.
//
// It is maintained by hand, which means it can drift: when you add or change a
// route in internal/delivery/http/route/route.go, update openapi.yaml in the
// same change.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
