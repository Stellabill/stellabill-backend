package security

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestMaskPII(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "customer ID in message",
			input:    "Processing request for customer_12345",
			expected: "Processing request for cust_***",
		},
		{
			name:     "subscription ID in message",
			input:    "Subscription sub_abcdefg created",
			expected: "Subscription sub_*** created",
		},
		{
			name:     "job ID in message",
			input:    "Job job_9876 completed",
			expected: "Job job_*** completed",
		},
		{
			name:     "amount masked",
			input:    "Amount 19.99 charged",
			expected: "Amount $*.** charged",
		},
		{
			name:     "amount without decimal masked",
			input:    "Amount 1999",
			expected: "Amount $*.**",
		},
		{
			name:     "email masked",
			input:    "Contact user@example.com for support",
			expected: "Contact e***@*** for support",
		},
		{
			name:     "token redacted",
			input:    "Bearer abc123 token used",
			expected: "Bearer ***REDACTED*** used",
		},
		{
			name:     "password redacted",
			input:    "passwordMySecret123",
			expected: "password***REDACTED***",
		},
		{
			name:     "mixed PII",
			input:    "Customer cust_abc123 paid 49.99 via subscription sub_456",
			expected: "Customer cust_*** paid $*.** via subscription sub_***",
		},
		{
			name:     "no PII",
			input:    "Normal log message with no sensitive data",
			expected: "Normal log message with no sensitive data",
		},
		{
			name:     "short ID",
			input:    "cust_12",
			expected: "cust_***",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskPII(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "masked fields - partial mask",
			input: map[string]interface{}{
				"customer":   "cust_abc123",
				"amount":     "19.99",
				"email":      "alice@example.com",
			},
			expected: map[string]interface{}{
				"customer": "cust***",
				"amount":   "$*.**",
				"email":    "e***@***",
			},
		},
		{
			name: "fully redacted fields",
			input: map[string]interface{}{
				"password": "super_secret",
				"token":    "jwt.token.here",
				"api_key":  "AKIAIOSFODNN7EXAMPLE",
			},
			expected: map[string]interface{}{
				"password": "***REDACTED***",
				"token":    "***REDACTED***",
				"api_key":  "***REDACTED***",
			},
		},
		{
			name: "nested maps",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"email": "bob@example.com",
					"id":    "user_007",
				},
			},
			expected: map[string]interface{}{
				"user": map[string]interface{}{
					"email": "e***@***",
					"id":    "user***",
				},
			},
		},
		{
			name: "slice of maps",
			input: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"subscription_id": "sub_123"},
					map[string]interface{}{"amount": "100.50"},
				},
			},
			expected: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"subscription_id": "sub***"},
					map[string]interface{}{"amount": "$*.**"},
				},
			},
		},
		{
			name: "non string fields untouched",
			input: map[string]interface{}{
				"count":  42,
				"active": true,
			},
			expected: map[string]interface{}{
				"count":  42,
				"active": true,
			},
		},
		{
			name: "nil input",
			input:    nil,
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactMap(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactStringField(t *testing.T) {
	tests := []struct {
		fieldName string
		value     string
		expected  string
	}{
		{
			fieldName: "customer",
			value:     "cust_abcdef123",
			expected:  "cust***",
		},
		{
			fieldName: "subscription_id",
			value:     "sub_xyz999",
			expected:  "sub***",
		},
		{
			fieldName: "amount",
			value:     "1999.99",
			expected:  "$*.**",
		},
		{
			fieldName: "email",
			value:     "alice@example.com",
			expected:  "e***@***",
		},
		{
			fieldName: "password",
			value:     "s3cr3t!",
			expected:  "***REDACTED***",
		},
		{
			fieldName: "token",
			value:     "Bearer abc.def.ghi",
			expected:  "***REDACTED***",
		},
		{
			fieldName: "nonpii",
			value:     "somevalue",
			expected:  "somevalue",
		},
		{
			fieldName: "description",
			value:     "contains cust_xyz inside",
			expected:  "contains cust_*** inside",
		},
	}
	for _, tt := range tests {
		t.Run(tt.fieldName+"_"+tt.value, func(t *testing.T) {
			result := RedactStringField(tt.fieldName, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactError(t *testing.T) {
	err1 := errors.New("failed to process payment for customer_abc123")
	redacted := RedactError(err1)
	assert.NotNil(t, redacted)
	assert.NotContains(t, redacted.Error(), "customer_abc123")
	assert.Contains(t, redacted.Error(), "cust_")

	assert.Nil(t, RedactError(nil))
}

func TestLooksSensitiveValue(t *testing.T) {
	assert.Equal(t, "cust***", maskCustomerID("abc123"))
	assert.Equal(t, "***", maskCustomerID("ab"))
	assert.Equal(t, "$*.**", maskAmount("19.99"))
	assert.Equal(t, "$*.**", maskAmount("0"))
	assert.Equal(t, "sub***", maskSubscriptionID("sub_xyz"))
	assert.Equal(t, "job***", maskJobID("job_999"))
}

func TestRedactZapFields(t *testing.T) {
	fields := []zap.Field{
		zap.String("customer", "cust_12345"),
		zap.Int("count", 5),
		zap.Error(errors.New("failed for customer_abc")),
	}
	redacted := RedactZapFields(fields)
	assert.Len(t, redacted, 3)
	assert.Equal(t, "cust***", redacted[0].String)
	assert.Equal(t, 5, redacted[1].Integer)
	err := redacted[2].Interface.(error)
	assert.NotContains(t, err.Error(), "customer_abc")
}

type testCore struct {
	zapcore.Core
	entries    []zapcore.Entry
	fieldsList [][]zapcore.Field
}

func (t *testCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	t.entries = append(t.entries, entry)
	t.fieldsList = append(t.fieldsList, fields)
	return nil
}

func (t *testCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(entry, t)
}

func (t *testCore) With(fields []zapcore.Field) zapcore.Core {
	return t
}

// TestRedactingCore_Integration tests the full core redaction pipeline.
func TestRedactingCore_Integration(t *testing.T) {
	tc := &testCore{}
	core := NewRedactingCore(tc)

	entry := zapcore.Entry{
		Message: "Error for customer_abc and amount 99.99",
		Level:   zapcore.ErrorLevel,
	}
	fields := []zapcore.Field{
		zap.String("customer", "cust_123"),
		zap.String("amount", "1999.99"),
		zap.Int64("count", 42),
	}

	_ = core.Write(entry, fields)

	assert.Len(t, tc.entries, 1)
	assert.NotContains(t, tc.entries[0].Message, "customer_abc")
	assert.Contains(t, tc.entries[0].Message, "cust_***")
	assert.Contains(t, tc.entries[0].Message, "$*.**")

	assert.Len(t, tc.fieldsList, 1)
	redactedFields := tc.fieldsList[0]
	var custVal, amtVal string
	var countVal int64
	for _, f := range redactedFields {
		switch f.Key {
		case "customer":
			custVal = f.String
		case "amount":
			amtVal = f.String
		case "count":
			countVal = f.Integer
		}
	}
	assert.Equal(t, "cust***", custVal)
	assert.Equal(t, "$*.**", amtVal)
	assert.Equal(t, int64(42), countVal)
}
