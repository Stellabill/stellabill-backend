package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xeipuuv/gojsonschema"
	"stellarbill-backend/openapi"
)

// OpenAPIRequestBodyValidation enables runtime request-body validation against the embedded OpenAPI spec.
//
// Intended for dev mode only (higher CPU overhead), and only for JSON request bodies.
func OpenAPIRequestBodyValidation() gin.HandlerFunc {
	// Load spec once per process; openapi.Load() embeds YAML.
	spec, err := openapi.Load()
	if err != nil {
		// Fail safe: if spec can't load, do not block requests.
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		openapiPath := ginPathToOpenAPIPath(c.FullPath(), c.Request.URL.Path)
		if openapiPath == "" {
			c.Next()
			return
		}

		method := strings.ToUpper(c.Request.Method)
		pathItem := spec.Paths.Find(openapiPath)
		if pathItem == nil {
			c.Next()
			return
		}

		op := pathItem.GetOperation(method)
		if op == nil || op.RequestBody == nil {
			c.Next()
			return
		}

		reqBody := op.RequestBody
		if reqBody.Required != nil && !*reqBody.Required {
			// Optional request body; if absent, let it through.
			if c.Request.Body == nil || c.Request.ContentLength == 0 {
				c.Next()
				return
			}
		}

		// Only validate JSON bodies.
		content := reqBody.Value.Content
		media := content["application/json"]
		if media == nil {
			c.Next()
			return
		}

		if c.Request.Body == nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "validation_failed",
				"message": "missing request body",
			})
			return
		}

		// Read body fully then restore so handlers can still bind.
		raw, err := c.GetRawData()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "validation_failed",
				"message": "invalid request body",
			})
			return
		}
		c.Request.Body = ioNopCloser(bytes.NewReader(raw))

		// If body is empty and not required, allow.
		if len(bytes.TrimSpace(raw)) == 0 {

			if reqBody.Required == nil || !*reqBody.Required {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "validation_failed",
				"message": "missing request body",
			})
			return
		}

		// Convert spec schema into JSON-schema form for gojsonschema.
		// We validate raw JSON using the schema from OpenAPI.
		if media.Schema == nil {
			c.Next()
			return
		}

		// Marshal schema to JSON for gojsonschema.
		schemaJSON, err := json.Marshal(media.Schema)
		if err != nil {
			c.Next()
			return
		}

		// gojsonschema requires JSON schema as document.
		schemaLoader := gojsonschema.NewBytesLoader(schemaJSON)
		jsonLoader := gojsonschema.NewBytesLoader(raw)

		result, err := gojsonschema.Validate(schemaLoader, jsonLoader)
		if err != nil {
			c.Next()
			return
		}

		if !result.Valid() {
			// Best-effort compact errors.
			details := make([]gin.H, 0, len(result.Errors()))
			for _, e := range result.Errors() {
				details = append(details, gin.H{
					"field":   e.Field(),
					"message": e.String(),
				})
			}

			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "validation_failed",
				"message": "request body does not conform to OpenAPI schema",
				"details": details,
			})
			return
		}

		c.Next()
	}
}



// minimal io.NopCloser alternative to avoid importing io everywhere in this file.
func ioNopCloser(r *bytes.Reader) *nopCloser { return &nopCloser{r: r} }

type nopCloser struct{ r *bytes.Reader }

func (n *nopCloser) Read(p []byte) (int, error) { return n.r.Read(p) }
func (n *nopCloser) Close() error              { return nil }

// ginPathToOpenAPIPath converts Gin route patterns to OpenAPI path patterns.
// It prefers gin's FullPath() (e.g. /api/v1/items/:id) but can fall back to URL path.
func ginPathToOpenAPIPath(fullPath string, urlPath string) string {
	candidate := fullPath
	if candidate == "" {
		candidate = urlPath
	}

	parts := strings.Split(candidate, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

