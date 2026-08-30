package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <slo-yaml-file> [slo-yaml-file...]\n", os.Args[0])
		os.Exit(1)
	}

	hasError := false

	for _, pattern := range os.Args[1:] {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving pattern %s: %v\n", pattern, err)
			hasError = true
			continue
		}

		for _, match := range matches {
			content, err := os.ReadFile(match)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", match, err)
				hasError = true
				continue
			}

			rules, err := GenerateRules(content)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error generating rules for %s: %v\n", match, err)
				hasError = true
				continue
			}

			// In a real environment, we might write these to a file or stdout
			// Since this is primarily a linter and CI step, we will print it so we can pipe it
			fmt.Printf("---\n# Generated from %s\n%s\n", match, string(rules))
		}
	}

	if hasError {
		os.Exit(1)
	}
}
