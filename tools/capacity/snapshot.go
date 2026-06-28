package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type MetricPoint struct {
	Name   string
	Labels map[string]string
	Value  float64
}

type Snapshot struct {
	Points []MetricPoint
}

func readSnapshotFile(path string) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	return ParseSnapshotFromScanner(scanner)
}

func ParseSnapshotFromScanner(r interface {
	Scan() bool
	Text() string
	Err() error
}) (Snapshot, error) {
	var points []MetricPoint
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		point, ok, err := parseMetricLine(line)
		if err != nil {
			return Snapshot{}, err
		}
		if ok {
			points = append(points, point)
		}
	}
	if err := r.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Points: points}, nil
}

func parseMetricLine(line string) (MetricPoint, bool, error) {
	nameAndLabels, valuePart, ok := strings.Cut(line, " ")
	if !ok {
		return MetricPoint{}, false, nil
	}

	value, err := strconv.ParseFloat(strings.TrimSpace(valuePart), 64)
	if err != nil {
		return MetricPoint{}, false, fmt.Errorf("parse metric value %q: %w", valuePart, err)
	}

	point := MetricPoint{Value: value}
	if strings.Contains(nameAndLabels, "{") {
		name, labels, ok := strings.Cut(nameAndLabels, "{")
		if !ok {
			return MetricPoint{}, false, fmt.Errorf("parse labels from %q", line)
		}
		point.Name = name
		labelText := strings.TrimSuffix(labels, "}")
		parsed, err := parseLabels(labelText)
		if err != nil {
			return MetricPoint{}, false, err
		}
		point.Labels = parsed
	} else {
		point.Name = nameAndLabels
		point.Labels = map[string]string{}
	}

	return point, true, nil
}

func parseLabels(raw string) (map[string]string, error) {
	labels := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return labels, nil
	}

	parts := splitRespectingQuotes(raw, ',')
	for _, part := range parts {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return nil, fmt.Errorf("invalid label segment %q", part)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		value = strings.ReplaceAll(value, `\"`, `"`)
		labels[key] = value
	}
	return labels, nil
}

func splitRespectingQuotes(s string, sep rune) []string {
	var parts []string
	start := 0
	inQuotes := false
	for i, r := range s {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case sep:
			if !inQuotes {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func (s Snapshot) Sum(name string, filters map[string]string) float64 {
	var total float64
	for _, p := range s.Points {
		if p.Name != name {
			continue
		}
		if !labelsMatch(p.Labels, filters) {
			continue
		}
		total += p.Value
	}
	return total
}

func labelsMatch(labels map[string]string, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	for k, v := range filters {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func newSnapshotFromString(text string) (Snapshot, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	return ParseSnapshotFromScanner(scanner)
}
