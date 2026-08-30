package main

import (
	"bytes"
	"errors"
	"fmt"
	"text/template"

	"gopkg.in/yaml.v3"
)

type SLOWindow struct {
	Fast string `yaml:"fast"`
	Slow string `yaml:"slow"`
}

type SLO struct {
	Name        string            `yaml:"name"`
	Objective   float64           `yaml:"objective"`
	Description string            `yaml:"description"`
	Labels      map[string]string `yaml:"labels"`
	Numerator   string            `yaml:"numerator"`
	Denominator string            `yaml:"denominator"`
	Windows     []SLOWindow       `yaml:"windows"`
}

type PrometheusRuleGroup struct {
	Name  string           `yaml:"name"`
	Rules []PrometheusRule `yaml:"rules"`
}

type PrometheusRule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        string            `yaml:"expr"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

type PrometheusGroups struct {
	Groups []PrometheusRuleGroup `yaml:"groups"`
}

// GenerateRules parses an SLO YAML and generates Prometheus rules.
func GenerateRules(yamlContent []byte) ([]byte, error) {
	var slo SLO
	if err := yaml.Unmarshal(yamlContent, &slo); err != nil {
		return nil, fmt.Errorf("failed to parse SLO yaml: %w", err)
	}

	if slo.Name == "" {
		return nil, errors.New("SLO name is required")
	}
	if slo.Numerator == "" {
		return nil, errors.New("numerator is required")
	}
	if slo.Denominator == "" || slo.Denominator == "0" {
		return nil, errors.New("denominator cannot be empty or zero")
	}
	if len(slo.Windows) == 0 {
		return nil, errors.New("at least one window is required")
	}

	errorBudget := 1.0 - (slo.Objective / 100.0)

	var rules []PrometheusRule

	for _, win := range slo.Windows {
		if win.Fast == "" || win.Slow == "" {
			return nil, errors.New("empty window provided in fast or slow")
		}

		// Fast window burn rate recording rule
		fastExpr, err := renderQuery(slo.Numerator, slo.Denominator, win.Fast)
		if err != nil {
			return nil, err
		}
		rules = append(rules, PrometheusRule{
			Record: fmt.Sprintf("slo:burn_rate:fast:%s", win.Fast),
			Expr:   fastExpr,
			Labels: mergeLabels(slo.Labels, map[string]string{"slo": slo.Name}),
		})

		// Slow window burn rate recording rule
		slowExpr, err := renderQuery(slo.Numerator, slo.Denominator, win.Slow)
		if err != nil {
			return nil, err
		}
		rules = append(rules, PrometheusRule{
			Record: fmt.Sprintf("slo:burn_rate:slow:%s", win.Slow),
			Expr:   slowExpr,
			Labels: mergeLabels(slo.Labels, map[string]string{"slo": slo.Name}),
		})

		// Alerting rule for multi-window
		// We use an arbitrary threshold like 14.4x for fast and slow.
		burnRateThreshold := 14.4
		alertExpr := fmt.Sprintf("(slo:burn_rate:fast:%s{slo=\"%s\"} / %f > %f) and (slo:burn_rate:slow:%s{slo=\"%s\"} / %f > %f)",
			win.Fast, slo.Name, errorBudget, burnRateThreshold,
			win.Slow, slo.Name, errorBudget, burnRateThreshold)

		rules = append(rules, PrometheusRule{
			Alert: fmt.Sprintf("SLOBurnRateHigh_%s", slo.Name),
			Expr:  alertExpr,
			Labels: mergeLabels(slo.Labels, map[string]string{
				"severity": "critical",
				"slo":      slo.Name,
			}),
			Annotations: map[string]string{
				"summary":     fmt.Sprintf("High SLO burn rate for %s", slo.Name),
				"description": slo.Description,
			},
		})
	}

	group := PrometheusRuleGroup{
		Name:  slo.Name + "_slo_rules",
		Rules: rules,
	}

	groups := PrometheusGroups{
		Groups: []PrometheusRuleGroup{group},
	}

	return yaml.Marshal(&groups)
}

func renderQuery(numerator, denominator, window string) (string, error) {
	num, err := renderTemplate(numerator, window)
	if err != nil {
		return "", err
	}
	den, err := renderTemplate(denominator, window)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s / (%s > 0)", num, den), nil
}

func renderTemplate(tmplStr, window string) (string, error) {
	tmpl, err := template.New("query").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{"window": window})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func mergeLabels(a, b map[string]string) map[string]string {
	c := make(map[string]string)
	for k, v := range a {
		c[k] = v
	}
	for k, v := range b {
		c[k] = v
	}
	return c
}
