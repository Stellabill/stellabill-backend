// Package pact contains the Pact provider verification tests.
//
// The broker client connects to a remote Pact Broker to fetch the latest
// consumer pacts for the provider and publish verification results back.
//
// Usage:
//
//	PACT_BROKER_URL=https://your-broker.example.com
//	PACT_BROKER_TOKEN=<token>
//	go test ./tests/pact/... -v -timeout 120s
package pact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	providerName = "stellabill-backend"
)

// PactInteraction represents a single interaction from a Pact contract.
type PactInteraction struct {
	Description   string `json:"description"`
	ProviderState string `json:"providerState"`
	Request       struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    interface{}       `json:"body"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    interface{}       `json:"body"`
	} `json:"response"`
}

// PactFile represents a Pact contract file with consumer and provider metadata.
type PactFile struct {
	Consumer     struct{ Name string } `json:"consumer"`
	Provider     struct{ Name string } `json:"provider"`
	Interactions []PactInteraction     `json:"interactions"`
}

// BrokerConfig holds the connection details for a Pact Broker.
type BrokerConfig struct {
	BaseURL string
	Token   string
}

// BrokerPactFile represents a pact file fetched from the broker.
type BrokerPactFile struct {
	Consumer     struct{ Name string }        `json:"consumer"`
	Provider     struct{ Name string }        `json:"provider"`
	Interactions []PactInteraction            `json:"interactions"`
	Metadata     map[string]interface{}       `json:"metadata,omitempty"`
	Links        map[string]BrokerLink        `json:"_links,omitempty"`
}

// BrokerLink is a HAL-style link from the Pact Broker.
type BrokerLink struct {
	Href   string `json:"href"`
	Name   string `json:"name,omitempty"`
	Title  string `json:"title,omitempty"`
	Templated bool `json:"templated,omitempty"`
}

// VerificationResult is the payload posted back to the Pact Broker.
type VerificationResult struct {
	Success           bool      `json:"success"`
	ProviderName      string    `json:"providerName"`
	ProviderVersion   string    `json:"providerApplicationVersion"`
	VerificationDate  string    `json:"verificationDate"`
	VerificationType  string    `json:"verificationType,omitempty"`
}

// BrokerFromEnv reads broker configuration from environment variables.
// Returns nil when PACT_BROKER_URL is not set (local-fixture mode).
func BrokerFromEnv() *BrokerConfig {
	url := os.Getenv("PACT_BROKER_URL")
	if url == "" {
		return nil
	}
	return &BrokerConfig{
		BaseURL: url,
		Token:   os.Getenv("PACT_BROKER_TOKEN"),
	}
}

// FetchLatestPacts retrieves all latest pact files for the provider from
// the broker. Uses HAL-style navigation from the broker root.
func (b *BrokerConfig) FetchLatestPacts() ([]BrokerPactFile, error) {
	// Discover the provider endpoint from the broker root.
	root, err := b.getJSON(b.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("fetch broker root: %w", err)
	}

	var rootLinks struct {
		Links map[string]BrokerLink `json:"_links"`
	}
	if err := json.Unmarshal(root, &rootLinks); err != nil {
		return nil, fmt.Errorf("parse broker root links: %w", err)
	}

	providerLink, ok := rootLinks.Links["pb:latest-provider-pacts"]
	if !ok {
		return nil, fmt.Errorf("broker root missing pb:latest-provider-pacts link")
	}

	pactsData, err := b.getJSON(providerLink.Href)
	if err != nil {
		return nil, fmt.Errorf("fetch provider pacts: %w", err)
	}

	var pactsResponse struct {
		Embedded struct {
			Pacts []BrokerPactFile `json:"pacts"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(pactsData, &pactsResponse); err != nil {
		return nil, fmt.Errorf("parse provider pacts: %w", err)
	}

	// Fetch each pact's full content.
	for i, pactRef := range pactsResponse.Embedded.Pacts {
		pactLink, ok := pactRef.Links["self"]
		if !ok {
			continue
		}
		fullData, err := b.getJSON(pactLink.Href)
		if err != nil {
			return nil, fmt.Errorf("fetch pact content for %s: %w", pactRef.Consumer.Name, err)
		}
		var fullPact BrokerPactFile
		if err := json.Unmarshal(fullData, &fullPact); err != nil {
			return nil, fmt.Errorf("parse pact content for %s: %w", pactRef.Consumer.Name, err)
		}
		pactsResponse.Embedded.Pacts[i] = fullPact
	}

	var result []BrokerPactFile
	for _, p := range pactsResponse.Embedded.Pacts {
		if len(p.Interactions) > 0 {
			result = append(result, p)
		}
	}
	return result, nil
}

// PublishVerificationResult posts a verification result back to the broker.
func (b *BrokerConfig) PublishVerificationResult(consumerName string, success bool) error {
	// First fetch the pact to get the verification link.
	pacts, err := b.FetchLatestPacts()
	if err != nil {
		return fmt.Errorf("fetch pacts for publishing: %w", err)
	}

	var pactVersionLink string
	for _, p := range pacts {
		if p.Consumer.Name == consumerName {
			if link, ok := p.Links["pb:verification-results"]; ok {
				pactVersionLink = link.Href
			}
			break
		}
	}
	if pactVersionLink == "" {
		return fmt.Errorf("no pb:verification-results link found for consumer %s", consumerName)
	}

	result := VerificationResult{
		Success:          success,
		ProviderName:     providerName,
		ProviderVersion:  os.Getenv("GIT_COMMIT"),
		VerificationDate: time.Now().UTC().Format(time.RFC3339),
		VerificationType: "provider",
	}

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal verification result: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, pactVersionLink, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create verification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post verification result: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("broker returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// getJSON performs an authenticated GET request and returns the response body.
func (b *BrokerConfig) getJSON(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/hal+json")
	if b.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	return io.ReadAll(resp.Body)
}
