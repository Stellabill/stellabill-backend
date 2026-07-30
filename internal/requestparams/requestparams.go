package requestparams

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	searchPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
)

type ValidationError struct {
	Location string
	Name     string
	Reason   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s parameter %q: %s", e.Location, e.Name, e.Reason)
}

type StringRule struct {
	MaxLen    int
	Pattern   *regexp.Regexp
	Enum      map[string]struct{}
	Lowercase bool
	Uppercase bool
}

type IntRule struct {
	Min int
	Max int
}

type QueryRules struct {
	Strings map[string]StringRule
	Ints    map[string]IntRule
}

type SanitizedQuery struct {
	Strings map[string]string
	Ints    map[string]int
}

func IdentifierRule(maxLen int) StringRule {
	return StringRule{
		MaxLen:  maxLen,
		Pattern: identifierPattern,
	}
}

func SearchRule(maxLen int) StringRule {
	return StringRule{
		MaxLen:  maxLen,
		Pattern: searchPattern,
	}
}

func CurrencyRule() StringRule {
	return StringRule{
		MaxLen:    3,
		Pattern:   currencyPattern,
		Uppercase: true,
	}
}

func EnumRule(maxLen int, lowercase bool, values ...string) StringRule {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}

	return StringRule{
		MaxLen:    maxLen,
		Pattern:   identifierPattern,
		Enum:      allowed,
		Lowercase: lowercase,
	}
}

func NormalizePathID(name, value string) (string, error) {
	normalized, err := normalizeString(value)
	if err != nil {
		return "", &ValidationError{Location: "path", Name: name, Reason: err.Error()}
	}
	if utf8.RuneCountInString(normalized) > 64 {
		return "", &ValidationError{Location: "path", Name: name, Reason: "must be 64 characters or fewer"}
	}
	if !identifierPattern.MatchString(normalized) {
		return "", &ValidationError{Location: "path", Name: name, Reason: "must contain only letters, numbers, dots, underscores, and hyphens"}
	}
	return normalized, nil
}

func SanitizeQuery(values url.Values, rules QueryRules) (SanitizedQuery, error) {
	sanitized := SanitizedQuery{
		Strings: make(map[string]string, len(rules.Strings)),
		Ints:    make(map[string]int, len(rules.Ints)),
	}

	for name, rawValues := range values {
		if _, ok := rules.Strings[name]; !ok {
			if _, ok := rules.Ints[name]; !ok {
				return SanitizedQuery{}, &ValidationError{Location: "query", Name: name, Reason: "unsupported parameter"}
			}
		}

		if len(rawValues) != 1 {
			return SanitizedQuery{}, &ValidationError{Location: "query", Name: name, Reason: "must be provided exactly once"}
		}

		raw := rawValues[0]
		if rule, ok := rules.Strings[name]; ok {
			value, err := sanitizeString(name, raw, rule)
			if err != nil {
				return SanitizedQuery{}, err
			}
			sanitized.Strings[name] = value
			continue
		}

		rule := rules.Ints[name]
		value, err := sanitizeInt(name, raw, rule)
		if err != nil {
			return SanitizedQuery{}, err
		}
		sanitized.Ints[name] = value
	}

	return sanitized, nil
}

func sanitizeString(name, raw string, rule StringRule) (string, error) {
	normalized, err := normalizeString(raw)
	if err != nil {
		return "", &ValidationError{Location: "query", Name: name, Reason: err.Error()}
	}

	if rule.Lowercase {
		normalized = strings.ToLower(normalized)
	}
	if rule.Uppercase {
		normalized = strings.ToUpper(normalized)
	}

	if rule.MaxLen > 0 && utf8.RuneCountInString(normalized) > rule.MaxLen {
		return "", &ValidationError{Location: "query", Name: name, Reason: fmt.Sprintf("must be %d characters or fewer", rule.MaxLen)}
	}
	if rule.Pattern != nil && !rule.Pattern.MatchString(normalized) {
		return "", &ValidationError{Location: "query", Name: name, Reason: "contains invalid characters"}
	}
	if len(rule.Enum) > 0 {
		if _, ok := rule.Enum[normalized]; !ok {
			return "", &ValidationError{Location: "query", Name: name, Reason: "contains an unsupported value"}
		}
	}

	return normalized, nil
}

func sanitizeInt(name, raw string, rule IntRule) (int, error) {
	normalized, err := normalizeString(raw)
	if err != nil {
		return 0, &ValidationError{Location: "query", Name: name, Reason: err.Error()}
	}
	if normalized == "" {
		return 0, &ValidationError{Location: "query", Name: name, Reason: "must not be empty"}
	}
	for _, r := range normalized {
		if r < '0' || r > '9' {
			return 0, &ValidationError{Location: "query", Name: name, Reason: "must be a base-10 integer"}
		}
	}

	value64, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return 0, &ValidationError{Location: "query", Name: name, Reason: "must be a valid integer"}
	}

	value := int(value64)
	if value < rule.Min || value > rule.Max {
		return 0, &ValidationError{
			Location: "query",
			Name:     name,
			Reason:   fmt.Sprintf("must be between %d and %d", rule.Min, rule.Max),
		}
	}

	return value, nil
}

// ParseFields parses the `?fields=` query parameter value, validates each
// field name against the provided allowlist, and returns the deduplicated,
// ordered set of allowed fields.
//
// The raw value is expected to be a comma-separated list of field names
// (e.g. "id,name,status"). Each field name is trimmed and validated against
// the allowed set. Unknown or forbidden field names return a ValidationError
// with Location "query" and Name "fields".
//
// When raw is empty, nil is returned (meaning "all fields").
func ParseFields(raw string, allowed []string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = struct{}{}
	}

	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if !identifierPattern.MatchString(trimmed) {
			return nil, &ValidationError{
				Location: "query",
				Name:     "fields",
				Reason:   fmt.Sprintf("%q is not a valid field name", trimmed),
			}
		}
		if _, ok := allowedSet[trimmed]; !ok {
			return nil, &ValidationError{
				Location: "query",
				Name:     "fields",
				Reason:   fmt.Sprintf("unknown or forbidden field %q", trimmed),
			}
		}
		if _, ok := seen[trimmed]; ok {
			continue // skip duplicates
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	if len(result) == 0 {
		return nil, &ValidationError{
			Location: "query",
			Name:     "fields",
			Reason:   "must specify at least one valid field",
		}
	}

	return result, nil
}

func normalizeString(value string) (string, error) {
	normalized := norm.NFKC.String(strings.TrimSpace(value))
	if !utf8.ValidString(normalized) {
		return "", fmt.Errorf("must be valid UTF-8")
	}
	if normalized == "" {
		return "", fmt.Errorf("must not be empty")
	}
	return normalized, nil
}
