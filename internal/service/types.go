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
	ID             string         `json:"id"`
	PlanID         string         `json:"plan_id"`
	Customer       string         `json:"customer"`
	Status         string         `json:"status"`
	Interval       string         `json:"interval"`
	Plan           *PlanMetadata  `json:"plan,omitempty"`
	BillingSummary BillingSummary `json:"billing_summary"`
}

// ResponseEnvelope is the top-level JSON object returned by the endpoint.
type ResponseEnvelope struct {
	APIVersion string              `json:"api_version"`
	Data       *SubscriptionDetail `json:"data,omitempty"`
	Warnings   []string            `json:"warnings,omitempty"`
}
