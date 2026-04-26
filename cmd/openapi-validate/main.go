package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/openapi"
)

func main() {
	// Load OpenAPI specification
	doc, err := openapi.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to load OpenAPI spec:", err)
		os.Exit(1)
	}

	// Create a minimal gin engine and register routes
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)

	// Get registered routes
	engineRoutes := engine.Routes()

	// Build set of implemented routes
	implementedPaths := make(map[string]map[string]bool)
	for _, r := range engineRoutes {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		openAPIPath := ginPathToOpenAPIPath(r.Path)
		if implementedPaths[openAPIPath] == nil {
			implementedPaths[openAPIPath] = make(map[string]bool)
		}
		implementedPaths[openAPIPath][r.Method] = true
	}

	// Check that all implemented routes are in spec
	specPaths := doc.Paths.Map()
	var hasError bool
	for openAPIPath, methods := range implementedPaths {
		item := specPaths[openAPIPath]
		if item == nil {
			fmt.Fprintf(os.Stderr, "ERROR: Route path %q not found in OpenAPI spec\n", openAPIPath)
			hasError = true
			continue
		}
		for method := range methods {
			op := item.GetOperation(method)
			if op == nil {
				fmt.Fprintf(os.Stderr, "ERROR: Method %s for path %q not found in OpenAPI spec\n", method, openAPIPath)
				hasError = true
			}
		}
	}

	// Check that all spec routes are implemented
	for specPath, pathItem := range specPaths {
		if !strings.HasPrefix(specPath, "/api/") {
			continue
		}
		methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
		for _, method := range methods {
			var op *openapi3.Operation
			switch method {
			case "GET":
				op = pathItem.Get
			case "POST":
				op = pathItem.Post
			case "PUT":
				op = pathItem.Put
			case "PATCH":
				op = pathItem.Patch
			case "DELETE":
				op = pathItem.Delete
			case "OPTIONS":
				op = pathItem.Options
			case "HEAD":
				op = pathItem.Head
			}
			if op == nil {
				continue
			}
			if !implementedPaths[specPath][method] {
				fmt.Fprintf(os.Stderr, "ERROR: OpenAPI spec defines %s %q but route not implemented\n", method, specPath)
				hasError = true
			}
		}
	}

	if hasError {
		fmt.Fprintln(os.Stderr, "OpenAPI contract validation FAILED")
		os.Exit(1)
	}

	fmt.Println("OpenAPI contract validation PASSED")
}

func ginPathToOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
