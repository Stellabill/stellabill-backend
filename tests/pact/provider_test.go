// Package pact contains the Pact provider verification tests for the
// StellaBill webhook receiver.
//
// Run with:
//
//	go test ./tests/pact/... -v -timeout 120s
//
// The verifier spins up a real HTTP server backed by the webhook handler,
// replays each interaction from the local fixture pacts, and asserts that
// the provider responses match the consumer expectations.
//
// Provider states:
//
//	"subscription created" — no-op; handler is stateless for this event
//	"statement issued"     — no-op; handler is stateless for this event
//
// To use a remote Pact Broker instead of local fixtures, set:
//
//	PACT_BROKER_URL=https://your-broker.example.com
//	PACT_BROKER_TOKEN=<token>
//
// When PACT_BROKER_URL is set, the verifier fetches pacts from the broker,
// verifies each interaction, and publishes the verification result back
// to the broker.
package pact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const testWebhookSecret = "test-webhook-secret-for-pact"

func buildTestServer() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	wh := handlers.NewWebhookHandler()
	r.POST("/webhooks",
		middleware.WebhookVerification(testWebhookSecret),
		wh.Receive,
	)
	return r
}

func computeHMAC(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func loadFixtures(t *testing.T) []PactInteraction {
	t.Helper()
	dir := filepath.Join("fixtures")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read fixtures dir %q: %v", dir, err)
	}
	var all []PactInteraction
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("failed to read fixture %q: %v", e.Name(), err)
		}
		var pf PactFile
		if err := json.Unmarshal(data, &pf); err != nil {
			t.Fatalf("failed to parse fixture %q: %v", e.Name(), err)
		}
		all = append(all, pf.Interactions...)
	}
	return all
}

func loadInteractionsFromBroker(t *testing.T, broker *BrokerConfig) []PactInteraction {
	t.Helper()
	pacts, err := broker.FetchLatestPacts()
	if err != nil {
		t.Fatalf("failed to fetch pacts from broker: %v", err)
	}
	var all []PactInteraction
	for _, pact := range pacts {
		all = append(all, pact.Interactions...)
	}
	if len(all) == 0 {
		t.Fatal("no pact interactions found from broker")
	}
	return all
}

func applyProviderState(t *testing.T, state string) {
	t.Helper()
	switch state {
	case "subscription created", "statement issued":
		// stateless handler — nothing to set up
	default:
		t.Logf("warning: unknown provider state %q — no setup performed", state)
	}
}

func verifyInteraction(t *testing.T, server *gin.Engine, interaction PactInteraction) {
	t.Helper()

	bodyBytes, err := json.Marshal(interaction.Request.Body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	bodyStr := string(bodyBytes)

	sig := computeHMAC(testWebhookSecret, bodyStr)

	req := httptest.NewRequest(
		interaction.Request.Method,
		interaction.Request.Path,
		strings.NewReader(bodyStr),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.WebhookSignatureHeader, sig)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != interaction.Response.Status {
		t.Errorf("interaction %q: expected status %d, got %d\nbody: %s",
			interaction.Description,
			interaction.Response.Status,
			rec.Code,
			rec.Body.String(),
		)
	}

	if interaction.Response.Body != nil {
		var got map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("interaction %q: failed to decode response: %v", interaction.Description, err)
		}
		expected, ok := interaction.Response.Body.(map[string]interface{})
		if !ok {
			t.Fatalf("interaction %q: fixture response body is not an object", interaction.Description)
		}
		for key, wantVal := range expected {
			gotVal, exists := got[key]
			if !exists {
				t.Errorf("interaction %q: response missing field %q", interaction.Description, key)
				continue
			}
			if wantVal != gotVal {
				t.Errorf("interaction %q: field %q = %v, want %v",
					interaction.Description, key, gotVal, wantVal)
			}
		}
	}
}

func TestWebhookProviderPact(t *testing.T) {
	broker := BrokerFromEnv()

	var interactions []PactInteraction
	if broker != nil {
		t.Log("using Pact Broker at", broker.BaseURL)
		interactions = loadInteractionsFromBroker(t, broker)
	} else {
		t.Log("using local fixtures")
		interactions = loadFixtures(t)
	}

	server := buildTestServer()
	allPassed := true

	for _, interaction := range interactions {
		interaction := interaction
		t.Run(interaction.Description, func(t *testing.T) {
			applyProviderState(t, interaction.ProviderState)
			verifyInteraction(t, server, interaction)
			if t.Failed() {
				allPassed = false
			}
		})
	}

	// Publish verification results to broker when configured.
	if broker != nil {
		// Use the GIT_COMMIT env var set by CI, or a fallback tag.
		providerVersion := os.Getenv("GIT_COMMIT")
		if providerVersion == "" {
			providerVersion = "local-verification"
		}

		for _, pact := range getConsumersFromBroker(t, broker) {
			if err := broker.PublishVerificationResult(pact, allPassed); err != nil {
				t.Logf("warning: failed to publish verification result for %s: %v", pact, err)
			} else {
				t.Logf("verification result published to broker for consumer %s (success=%v)", pact, allPassed)
			}
		}
	}
}

func getConsumersFromBroker(t *testing.T, broker *BrokerConfig) []string {
	t.Helper()
	pacts, err := broker.FetchLatestPacts()
	if err != nil {
		t.Fatalf("failed to list consumers from broker: %v", err)
	}
	var consumers []string
	seen := make(map[string]bool)
	for _, p := range pacts {
		name := p.Consumer.Name
		if !seen[name] {
			consumers = append(consumers, name)
			seen[name] = true
		}
	}
	if len(consumers) == 0 {
		consumers = append(consumers, "stellabill-webhook-consumer")
	}
	return consumers
}

func TestWebhookProviderPact_MissingSignature(t *testing.T) {
	server := buildTestServer()

	body := `{"event_type":"subscription.created","data":{"subscription_id":"sub_1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Webhook-Signature header

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "missing_signature" {
		t.Errorf("expected error=missing_signature, got %v", resp["error"])
	}
}

func TestWebhookProviderPact_InvalidSignature(t *testing.T) {
	server := buildTestServer()

	body := `{"event_type":"subscription.created","data":{"subscription_id":"sub_1"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.WebhookSignatureHeader, "deadbeef")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWebhookProviderPact_UnknownEventType(t *testing.T) {
	server := buildTestServer()

	body := `{"event_type":"payment.unknown","data":{"foo":"bar"}}`
	sig := computeHMAC(testWebhookSecret, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.WebhookSignatureHeader, sig)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "unknown_event_type" {
		t.Errorf("expected error=unknown_event_type, got %v", resp["error"])
	}
}

func TestWebhookProviderPact_SubscriptionCreated_MissingID(t *testing.T) {
	server := buildTestServer()

	body := `{"event_type":"subscription.created","data":{}}`
	sig := computeHMAC(testWebhookSecret, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.WebhookSignatureHeader, sig)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookProviderPact_StatementIssued_MissingID(t *testing.T) {
	server := buildTestServer()

	body := `{"event_type":"statement.issued","data":{}}`
	sig := computeHMAC(testWebhookSecret, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.WebhookSignatureHeader, sig)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// Ensure unused imports compile.
var _ = fmt.Sprintf
var _ io.Reader = strings.NewReader("")
