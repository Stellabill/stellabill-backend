package service

// PlanMetadata is the plan subset embedded in the response.
type PlanMetadata struct {
	PlanID      string `json:"plan_id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description,omitempty"`
}

// BillingSummary holds normalized billing fields.
type BillingSummary struct {
	AmountCents     int64   `json:"amount_cents"`
	Currency        string  `json:"currency"`
	NextBillingDate *string `json:"next_billing_date"`
}

// SubscriptionDetail is the payload placed in ResponseEnvelope.Data.
type SubscriptionDetail struct {
	ID             string         `json:"id" redacted:"false"`
	PlanID         string         `json:"plan_id" redacted:"false"`
	Customer       string         `json:"customer,omitempty" redacted:"true"`
	Status         string         `json:"status"`
	Interval       string         `json:"interval"`
	Plan           *PlanMetadata  `json:"plan,omitempty"`
	BillingSummary BillingSummary `json:"billing_summary" redacted:"amount"`
}

// MarshalJSON implements redacted JSON marshaling
func (sd *SubscriptionDetail) MarshalJSON() ([]byte, error) {
	type Alias SubscriptionDetail
	data := struct {
		*Alias
		Customer string `json:"customer,omitempty"`
	}{
		Alias: (*Alias)(sd),
	}
	if data.Customer != "" {
		data.Customer = "cust_***" // Minimal redaction - hide full ID
	}
	return json.Marshal(data)
}

// ResponseEnvelope is the top-level JSON object returned by the endpoint.
type ResponseEnvelope struct {
	APIVersion string              `json:"api_version"`
	Data       *SubscriptionDetail `json:"data,omitempty"`
	Warnings   []string            `json:"warnings,omitempty"`
}
