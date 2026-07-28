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
	"fmt"
	"os"

	"stellarbill-backend/openapi"
)

// defaultSpecPath is used when no argument is provided.
const defaultSpecPath = "openapi/openapi.yaml"

func main() {
	specPath := defaultSpecPath
	if len(os.Args) > 1 {
		specPath = os.Args[1]
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-lint: cannot read %q: %v\n", specPath, err)
		os.Exit(1)
	}

	// Re-use the project's loader so validation rules (e.g. schema refs) are
	// applied consistently with the rest of the toolchain.
	doc, err := openapi.LoadFromBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-lint: invalid OpenAPI document %q: %v\n", specPath, err)
		os.Exit(1)
	}

	result := LintDoc(doc)
	if result.OK() {
		fmt.Printf("openapi-lint: all operations in %q have valid examples ✓\n", specPath)
		return
	}

	fmt.Fprintf(os.Stderr, "openapi-lint: %d violation(s) found in %q:\n\n",
		len(result.Violations), specPath)
	for i, v := range result.Violations {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, v)
	}
	fmt.Fprintln(os.Stderr)
	os.Exit(1)
}
