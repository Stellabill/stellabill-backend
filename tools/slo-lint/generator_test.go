package main

import (
	"strings"
	"testing"
)

func TestGenerateRules(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		wantErr     string
		wantContain []string
	}{
		{
			name: "valid slo",
			yamlContent: `
name: "API_Availability"
objective: 99.9
description: "API must be available"
labels:
  service: api
numerator: 'sum(rate(http_requests_total{status=~"5.."}[{{.window}}]))'
denominator: 'sum(rate(http_requests_total[{{.window}}]))'
windows:
  - fast: "1h"
    slow: "6h"
`,
			wantErr: "",
			wantContain: []string{
				"API_Availability_slo_rules",
				"slo:burn_rate:fast:1h",
				"slo:burn_rate:slow:6h",
				"SLOBurnRateHigh_API_Availability",
				"> 0",
			},
		},
		{
			name: "denominator is zero",
			yamlContent: `
name: "API_Availability"
objective: 99.9
description: "API must be available"
numerator: 'sum(rate(http_requests_total{status=~"5.."}[{{.window}}]))'
denominator: "0"
windows:
  - fast: "1h"
    slow: "6h"
`,
			wantErr: "denominator cannot be empty or zero",
		},
		{
			name: "denominator is empty",
			yamlContent: `
name: "API_Availability"
objective: 99.9
numerator: 'sum(rate(http_requests_total{status=~"5.."}[{{.window}}]))'
denominator: ""
windows:
  - fast: "1h"
    slow: "6h"
`,
			wantErr: "denominator cannot be empty or zero",
		},
		{
			name: "empty window",
			yamlContent: `
name: "API_Availability"
objective: 99.9
numerator: 'sum(rate(http_requests_total{status=~"5.."}[{{.window}}]))'
denominator: 'sum(rate(http_requests_total[{{.window}}]))'
windows:
  - fast: ""
    slow: "6h"
`,
			wantErr: "empty window provided in fast or slow",
		},
		{
			name: "no windows",
			yamlContent: `
name: "API_Availability"
objective: 99.9
numerator: 'sum(rate(http_requests_total{status=~"5.."}[{{.window}}]))'
denominator: 'sum(rate(http_requests_total[{{.window}}]))'
windows: []
`,
			wantErr: "at least one window is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := GenerateRules([]byte(tt.yamlContent))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			outStr := string(out)
			for _, want := range tt.wantContain {
				if !strings.Contains(outStr, want) {
					t.Errorf("output missing expected string %q\nOutput: %s", want, outStr)
				}
			}
		})
	}
}
