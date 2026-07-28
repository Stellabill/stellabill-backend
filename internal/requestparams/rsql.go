package requestparams

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Masterminds/squirrel"
)

// ParseOptions configures how RSQL filters are validated.
type ParseOptions struct {
	AllowUnindexed bool
	AllowedFields  map[string]FieldRule
}

// FieldRule defines the allowed column mapping and whether it is considered indexed.
type FieldRule struct {
	Indexed bool
	Column  string
}

// RSQLFilter is a parsed and validated RSQL expression that can be compiled
// into a parameterized squirrel query.
type RSQLFilter struct {
	root *rsqlNode
	raw  string
}

type rsqlNode struct {
	op       string
	field    string
	operator string
	values   []string
	children []*rsqlNode
	column   string
}

var defaultAllowedFields = map[string]FieldRule{
	"amount":          {Indexed: true, Column: "total_amount"},
	"customer_id":     {Indexed: true, Column: "customer_id"},
	"kind":            {Indexed: true, Column: "kind"},
	"status":          {Indexed: true, Column: "status"},
	"subscription_id": {Indexed: true, Column: "subscription_id"},
}

// ParseRSQL parses a safe, allowlisted RSQL expression.
func ParseRSQL(input string) (*RSQLFilter, error) {
	return ParseRSQLWithOptions(input, ParseOptions{})
}

// ParseRSQLWithOptions parses an RSQL expression with custom validation rules.
func ParseRSQLWithOptions(input string, opts ParseOptions) (*RSQLFilter, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("filter must not be empty")
	}
	if opts.AllowedFields == nil {
		opts.AllowedFields = defaultAllowedFields
	}
	parser := &rsqlParser{input: trimmed, opts: opts}
	root, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	return &RSQLFilter{root: root, raw: trimmed}, nil
}

// Fingerprint returns a stable fingerprint for the parsed filter.
func (f *RSQLFilter) Fingerprint() string {
	if f == nil || f.raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(f.raw)))
	return hex.EncodeToString(sum[:])[:16]
}

// ToSquirrel compiles the filter to a parameterized squirrel.Sqlizer.
func (f *RSQLFilter) ToSquirrel() (squirrel.Sqlizer, error) {
	if f == nil || f.root == nil {
		return nil, fmt.Errorf("filter is empty")
	}
	return f.root.toSquirrel()
}

type rsqlParser struct {
	input string
	opts  ParseOptions
	pos   int
}

func (p *rsqlParser) parseExpression() (*rsqlNode, error) {
	return p.parseOr()
}

func (p *rsqlParser) parseOr() (*rsqlNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peekRune() == ',' {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &rsqlNode{op: "or", children: []*rsqlNode{left, right}}
	}
	return left, nil
}

func (p *rsqlParser) parseAnd() (*rsqlNode, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peekRune() == ';' {
		p.pos++
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = &rsqlNode{op: "and", children: []*rsqlNode{left, right}}
	}
	return left, nil
}

func (p *rsqlParser) parsePrimary() (*rsqlNode, error) {
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("unexpected end of filter")
	}
	if p.peekRune() == '(' {
		p.pos++
		node, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("expected closing parenthesis")
		}
		p.pos++
		return node, nil
	}
	return p.parsePredicate()
}

func (p *rsqlParser) parsePredicate() (*rsqlNode, error) {
	field, err := p.parseIdentifier()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("unexpected end of filter")
	}

	operator, err := p.parseOperator()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()

	rule, ok := p.opts.AllowedFields[field]
	if !ok {
		return nil, fmt.Errorf("unsupported field %q", field)
	}
	if !rule.Indexed && !p.opts.AllowUnindexed {
		return nil, fmt.Errorf("field %q is not indexed; explicit override required", field)
	}

	var values []string
	if operator == "in" || operator == "out" {
		values, err = p.parseListValue()
		if err != nil {
			return nil, err
		}
	} else {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		values = []string{value}
	}

	return &rsqlNode{
		op:       "predicate",
		field:    field,
		operator: operator,
		values:   values,
		column:   rule.Column,
	}, nil
}

func (p *rsqlParser) parseOperator() (string, error) {
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("expected operator")
	}
	if p.input[p.pos] == '=' {
		p.pos++
		if p.pos < len(p.input) && p.input[p.pos] == '=' {
			p.pos++
			return "eq", nil
		}
		start := p.pos
		for p.pos < len(p.input) && p.input[p.pos] != '=' {
			p.pos++
		}
		if p.pos >= len(p.input) || p.input[p.pos] != '=' {
			return "", fmt.Errorf("invalid operator")
		}
		operator := p.input[start:p.pos]
		p.pos++
		if operator == "gt" || operator == "ge" || operator == "lt" || operator == "le" || operator == "in" || operator == "out" || operator == "eq" || operator == "ne" {
			return operator, nil
		}
		return "", fmt.Errorf("unsupported operator %q", operator)
	}
	return "", fmt.Errorf("unsupported operator syntax")
}

func (p *rsqlParser) parseIdentifier() (string, error) {
	start := p.pos
	for p.pos < len(p.input) {
		r := rune(p.input[p.pos])
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("expected field name")
	}
	return p.input[start:p.pos], nil
}

func (p *rsqlParser) parseValue() (string, error) {
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("expected value")
	}
	var builder strings.Builder
	for p.pos < len(p.input) {
		r := p.input[p.pos]
		if r == ';' || r == ',' || r == ')' {
			break
		}
		if r == '\\' {
			if p.pos+1 >= len(p.input) {
				return "", fmt.Errorf("dangling escape sequence")
			}
			builder.WriteByte(p.input[p.pos+1])
			p.pos += 2
			continue
		}
		builder.WriteByte(r)
		p.pos++
	}
	if builder.Len() == 0 {
		return "", fmt.Errorf("expected value")
	}
	value := builder.String()
	if utf8.ValidString(value) {
		return value, nil
	}
	return "", fmt.Errorf("value contains invalid UTF-8")
}

func (p *rsqlParser) parseListValue() ([]string, error) {
	p.skipWhitespace()
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return nil, fmt.Errorf("expected opening parenthesis")
	}
	p.pos++
	p.skipWhitespace()
	var values []string
	for {
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("expected closing parenthesis")
		}
		if p.input[p.pos] == ')' {
			p.pos++
			return values, nil
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		p.skipWhitespace()
		if p.pos < len(p.input) && p.input[p.pos] == ',' {
			p.pos++
			p.skipWhitespace()
			continue
		}
		p.skipWhitespace()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
			return values, nil
		}
		return nil, fmt.Errorf("expected comma or closing parenthesis")
	}
}

func (p *rsqlParser) skipWhitespace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t' || p.input[p.pos] == '\n' || p.input[p.pos] == '\r') {
		p.pos++
	}
}

func (p *rsqlParser) peekRune() byte {
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (n *rsqlNode) toSquirrel() (squirrel.Sqlizer, error) {
	switch n.op {
	case "predicate":
		return n.predicateToSquirrel()
	case "and":
		parts := make([]squirrel.Sqlizer, 0, len(n.children))
		for _, child := range n.children {
			part, err := child.toSquirrel()
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
		return squirrel.And(parts), nil
	case "or":
		parts := make([]squirrel.Sqlizer, 0, len(n.children))
		for _, child := range n.children {
			part, err := child.toSquirrel()
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
		return squirrel.Or(parts), nil
	default:
		return nil, fmt.Errorf("unsupported node type %q", n.op)
	}
}

func (n *rsqlNode) predicateToSquirrel() (squirrel.Sqlizer, error) {
	column := n.column
	if column == "" {
		column = n.field
	}
	switch n.operator {
	case "eq":
		return squirrel.Expr(fmt.Sprintf("%s = ?", column), n.values[0]), nil
	case "ne":
		return squirrel.Expr(fmt.Sprintf("%s != ?", column), n.values[0]), nil
	case "gt":
		return squirrel.Expr(fmt.Sprintf("%s > ?", column), n.values[0]), nil
	case "ge":
		return squirrel.Expr(fmt.Sprintf("%s >= ?", column), n.values[0]), nil
	case "lt":
		return squirrel.Expr(fmt.Sprintf("%s < ?", column), n.values[0]), nil
	case "le":
		return squirrel.Expr(fmt.Sprintf("%s <= ?", column), n.values[0]), nil
	case "in":
		placeholders := make([]string, len(n.values))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		return squirrel.Expr(fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), toInterfaceSlice(n.values)...), nil
	case "out":
		placeholders := make([]string, len(n.values))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		return squirrel.Expr(fmt.Sprintf("%s NOT IN (%s)", column, strings.Join(placeholders, ", ")), toInterfaceSlice(n.values)...), nil
	default:
		return nil, fmt.Errorf("unsupported operator %q", n.operator)
	}
}

func toInterfaceSlice(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}
