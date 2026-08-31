package tracing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("TAIL_MAX_TRACES", "")
	t.Setenv("TAIL_MAX_SPANS", "")
	t.Setenv("TAIL_DECISION_WINDOW", "")
	t.Setenv("TAIL_LATENCY_THRESHOLD", "")

	cfg, err := tailConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 10000, cfg.maxTraces)
	assert.Equal(t, 500, cfg.maxSpans)
	assert.Equal(t, 10*time.Second, cfg.decisionWindow)
	assert.Equal(t, 500*time.Millisecond, cfg.latency)
}

func TestTailConfigFromEnv_Override(t *testing.T) {
	t.Setenv("TAIL_MAX_TRACES", "200")
	t.Setenv("TAIL_MAX_SPANS", "50")
	t.Setenv("TAIL_DECISION_WINDOW", "30s")
	t.Setenv("TAIL_LATENCY_THRESHOLD", "1s")

	cfg, err := tailConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 200, cfg.maxTraces)
	assert.Equal(t, 50, cfg.maxSpans)
	assert.Equal(t, 30*time.Second, cfg.decisionWindow)
	assert.Equal(t, 1*time.Second, cfg.latency)
}

func TestTailConfigFromEnv_InvalidValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "zero max traces", key: "TAIL_MAX_TRACES", value: "0"},
		{name: "negative max spans", key: "TAIL_MAX_SPANS", value: "-1"},
		{name: "invalid window", key: "TAIL_DECISION_WINDOW", value: "invalid"},
		{name: "invalid latency", key: "TAIL_LATENCY_THRESHOLD", value: "not-a-duration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Reset all env vars
			t.Setenv("TAIL_MAX_TRACES", "")
			t.Setenv("TAIL_MAX_SPANS", "")
			t.Setenv("TAIL_DECISION_WINDOW", "")
			t.Setenv("TAIL_LATENCY_THRESHOLD", "")
			t.Setenv(tt.key, tt.value)

			cfg, err := tailConfigFromEnv()
			require.NoError(t, err)
			// Invalid values are silently ignored; defaults apply
			assert.Equal(t, 10000, cfg.maxTraces)
			assert.Equal(t, 500, cfg.maxSpans)
		})
	}
}

func testTailConfig() tailConfig {
	return tailConfig{
		maxTraces:      100,
		maxSpans:       10,
		decisionWindow: time.Hour,
		latency:        100 * time.Millisecond,
	}
}
