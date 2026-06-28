package graphql

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

// Handler is the Gin handler for the GraphQL endpoint.
type Handler struct {
	schema gql.Schema
}

// NewHandler builds the GraphQL schema from the provided services and returns a Handler.
func NewHandler(svc Services) (*Handler, error) {
	schema, err := gql.NewSchema(gql.SchemaConfig{
		Query: buildQueryType(svc),
	})
	if err != nil {
		return nil, err
	}
	return &Handler{schema: schema}, nil
}

// graphqlRequest is the JSON body for a GraphQL POST request.
type graphqlRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
}

// ServeHTTP handles POST /api/v1/graphql.
func (h *Handler) ServeHTTP(c *gin.Context) {
	callerID, _ := c.Get("callerID")
	tenantID, _ := c.Get("tenantID")
	roles, _ := c.Get("roles")

	callerIDStr, _ := callerID.(string)
	tenantIDStr, _ := tenantID.(string)

	var roleSlice []string
	switch r := roles.(type) {
	case []string:
		roleSlice = r
	}

	var req graphqlRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": "invalid JSON body"}}})
		return
	}

	if req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": "query is required"}}})
		return
	}

	// Parse and validate depth/complexity before execution.
	doc, parseErr := parser.Parse(parser.ParseParams{
		Source: source.NewSource(&source.Source{Body: []byte(req.Query)}),
	})
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": parseErr.Error()}}})
		return
	}

	if err := ValidateQuery(doc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []gin.H{{"message": err.Error()}}})
		return
	}

	ctx := WithCallerContext(c.Request.Context(), callerIDStr, tenantIDStr, roleSlice)

	result := gql.Do(gql.Params{
		Schema:         h.schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        ctx,
	})

	status := http.StatusOK
	if len(result.Errors) > 0 {
		status = http.StatusUnprocessableEntity
	}
	c.JSON(status, result)
}
