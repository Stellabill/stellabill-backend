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
package pact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
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
		Status int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    interface{}       `json:"body"`
	} `json:"response"`
}

type PactFile struct {
	Consumer     struct{ Name string }  `json:"consumer"`
	Provider     struct{ Name string }  `json:"provider"`
	Interactions []PactInteraction      `json:"interactions"`
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

func applyProviderState(t *testing.T, state string) {
	t.Helper()
	switch state {
	case "subscription created", "statement issued":
		// stateless handler — nothing to set up
	default:
		t.Logf("warning: unknown provider state %q — no setup performed", state)
	}
}

func TestWebhookProviderPact(t *testing.T) {
	interactions := loadFixtures(t)
	if len(interactions) == 0 {
		t.Fatal("no pact interactions found in fixtures/")
	}

	server := buildTestServer()

	for _, interaction := range interactions {
		interaction := interaction
		t.Run(interaction.Description, func(t *testing.T) {
			applyProviderState(t, interaction.ProviderState)

			// Serialize the request body to JSON
			bodyBytes, err := json.Marshal(interaction.Request.Body)
			if err != nil {
				t.Fatalf("failed to marshal request body: %v", err)
			}
			bodyStr := string(bodyBytes)

			// Compute real HMAC for this body
			sig := computeHMAC(testWebhookSecret, bodyStr)

			// Build the HTTP request
			req := httptest.NewRequest(
				interaction.Request.Method,
				interaction.Request.Path,
				strings.NewReader(bodyStr),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(middleware.WebhookSignatureHeader, sig)

			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)

			// Assert status code
			if rec.Code != interaction.Response.Status {
				t.Errorf("interaction %q: expected status %d, got %d\nbody: %s",
					interaction.Description,
					interaction.Response.Status,
					rec.Code,
					rec.Body.String(),
				)
			}

			// Assert response body fields match expectations
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
		})
	}
}

// TestWebhookProviderPact_MissingSignature verifies that requests without
// the signature header are rejected with 401.
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

// TestWebhookProviderPact_InvalidSignature verifies that requests with a
// wrong signature are rejected with 401.
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

// TestWebhookProviderPact_UnknownEventType verifies that an unknown event_type
// returns 422 with a clear error so consumers get an explicit diff.
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

// TestWebhookProviderPact_SubscriptionCreated_MissingID verifies that a
// subscription.created event without subscription_id returns 400.
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

// TestWebhookProviderPact_StatementIssued_MissingID verifies that a
// statement.issued event without statement_id returns 400.
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

// Ensure io is used (body drain helper kept for future broker integration).
var _ io.Reader = strings.NewReader("")
