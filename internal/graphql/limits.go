package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
	"github.com/graphql-go/graphql/language/visitor"
)

const (
	// MaxQueryDepth is the maximum nesting depth allowed in a GraphQL query.
	MaxQueryDepth = 5
	// MaxQueryComplexity is the maximum field-count allowed in a GraphQL query.
	MaxQueryComplexity = 50
)

// ValidateQuery checks depth and complexity limits on the parsed AST.
// Returns an error when either limit is exceeded.
func ValidateQuery(doc *ast.Document) error {
	depth, complexity := measureQuery(doc)
	if depth > MaxQueryDepth {
		return fmt.Errorf("query depth %d exceeds maximum allowed depth of %d", depth, MaxQueryDepth)
	}
	if complexity > MaxQueryComplexity {
		return fmt.Errorf("query complexity %d exceeds maximum allowed complexity of %d", complexity, MaxQueryComplexity)
	}
	return nil
}

// ValidateQueryString parses the given query string and applies ValidateQuery.
// Intended for use in tests.
func ValidateQueryString(query string) error {
	doc, err := parser.Parse(parser.ParseParams{
		Source: source.NewSource(&source.Source{Body: []byte(query)}),
	})
	if err != nil {
		return err
	}
	return ValidateQuery(doc)
}

// measureQuery returns the maximum depth and total field count for the document.
func measureQuery(doc *ast.Document) (maxDepth, complexity int) {
	currentDepth := 0

	visitor.Visit(doc, &visitor.VisitorOptions{
		Enter: func(p visitor.VisitFuncParams) (string, interface{}) {
			switch p.Node.(type) {
			case *ast.SelectionSet:
				currentDepth++
				if currentDepth > maxDepth {
					maxDepth = currentDepth
				}
			case *ast.Field:
				complexity++
			}
			return visitor.ActionNoChange, nil
		},
		Leave: func(p visitor.VisitFuncParams) (string, interface{}) {
			switch p.Node.(type) {
			case *ast.SelectionSet:
				currentDepth--
			}
			return visitor.ActionNoChange, nil
		},
	}, nil)

	return maxDepth, complexity
}
