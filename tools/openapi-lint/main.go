// openapi-lint checks that every HTTP operation in an OpenAPI specification
// has at least one response example (and, when a request body is declared, at
// least one request-body example).  Every example is also validated against
// its declared JSON Schema so invalid examples are caught before they ship.
//
// Usage:
//
//	go run ./tools/openapi-lint [openapi-file]
//
// The default spec path is openapi/openapi.yaml.  The tool exits 0 when all
// operations pass, 1 when violations are found.
package main

import (
	"os"

	"stellarbill-backend/openapi"
)

// defaultSpecPath is used when no argument is provided.
const defaultSpecPath = "openapi/openapi.yaml"

// readFile and loadSpec are package-level vars so tests can swap them out
// without forking a subprocess.  In production they delegate to the real
// implementations below.
var (
	readFile = os.ReadFile
	loadSpec = openapi.LoadFromBytes
)

func main() {
	os.Exit(run(os.Args[1:]))
}
