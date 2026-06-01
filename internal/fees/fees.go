// Package fees provides fee history and trend analysis for billing plans.
package fees

import (
	"context"
	"sort"
	"time"
)

// Fee represents a single fee entry for a subscription.
type Fee struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscription_id"`
	PlanID         string    `json:"plan_id"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	Kind           string    `json:"kind"` // "charge", "refund", "adjustment"
	Status         string    `json:"status"` // "pending", "paid", "failed", "refunded"
	BillingPeriod  string    `json:"billing_period"` // e.g. "2025-01"
	CreatedAt      time.Time `json:"created_at"`
}

// TrendPoint is a single data point in a fee trend series.
type TrendPoint struct {
	Period      string `json:"period"`       // e.g. "2025-01"
	AmountCents int64  `json:"amount_cents"`
	Count       int    `json:"count"`
}

// Trend holds aggregated trend data for a subscription or plan.
type Trend struct {
	SubscriptionID string       `json:"subscription_id,omitempty"`
	PlanID         string       `json:"plan_id,omitempty"`
	Currency       string       `json:"currency"`
	Points         []TrendPoint `json:"points"`
	TotalCents     int64        `json:"total_cents"`
	AvgCents       int64        `json:"avg_cents"`
}

// Service defines the fees domain operations.
type Service interface {
	// ListFees returns fees for a subscription, optionally filtered by status/kind.
	ListFees(ctx context.Context, subscriptionID, status, kind string) ([]Fee, error)
	// GetHistory returns the fee history for a subscription ordered by created_at desc.
	GetHistory(ctx context.Context, subscriptionID string, limit int) ([]Fee, error)
	// GetTrends returns aggregated monthly fee trends for a subscription.
	GetTrends(ctx context.Context, subscriptionID string) (*Trend, error)
}

// MemoryService is an in-memory implementation of Service for dev/test.
type MemoryService struct {
	fees []Fee
}

// NewMemoryService returns a MemoryService seeded with sample data.
func NewMemoryService() *MemoryService {
	now := time.Now().UTC()
	fees := []Fee{
		{ID: "fee-001", SubscriptionID: "sub-123", PlanID: "p1", AmountCents: 2999, Currency: "USD", Kind: "charge", Status: "paid", BillingPeriod: now.AddDate(0, -2, 0).Format("2006-01"), CreatedAt: now.AddDate(0, -2, 0)},
		{ID: "fee-002", SubscriptionID: "sub-123", PlanID: "p1", AmountCents: 2999, Currency: "USD", Kind: "charge", Status: "paid", BillingPeriod: now.AddDate(0, -1, 0).Format("2006-01"), CreatedAt: now.AddDate(0, -1, 0)},
		{ID: "fee-003", SubscriptionID: "sub-123", PlanID: "p1", AmountCents: 2999, Currency: "USD", Kind: "charge", Status: "pending", BillingPeriod: now.Format("2006-01"), CreatedAt: now},
		{ID: "fee-004", SubscriptionID: "sub-456", PlanID: "p1", AmountCents: 4999, Currency: "USD", Kind: "charge", Status: "paid", BillingPeriod: now.AddDate(0, -1, 0).Format("2006-01"), CreatedAt: now.AddDate(0, -1, 0)},
		{ID: "fee-005", SubscriptionID: "sub-456", PlanID: "p1", AmountCents: 4999, Currency: "USD", Kind: "charge", Status: "paid", BillingPeriod: now.Format("2006-01"), CreatedAt: now},
	}
	return &MemoryService{fees: fees}
}

func (s *MemoryService) ListFees(_ context.Context, subscriptionID, status, kind string) ([]Fee, error) {
	var out []Fee
	for _, f := range s.fees {
		if f.SubscriptionID != subscriptionID {
			continue
		}
		if status != "" && f.Status != status {
			continue
		}
		if kind != "" && f.Kind != kind {
			continue
		}
		out = append(out, f)
	}
	if out == nil {
		out = []Fee{}
	}
	return out, nil
}

func (s *MemoryService) GetHistory(_ context.Context, subscriptionID string, limit int) ([]Fee, error) {
	var out []Fee
	for _, f := range s.fees {
		if f.SubscriptionID == subscriptionID {
			out = append(out, f)
		}
	}
	// Sort descending by created_at
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []Fee{}
	}
	return out, nil
}

func (s *MemoryService) GetTrends(_ context.Context, subscriptionID string) (*Trend, error) {
	periodMap := map[string]*TrendPoint{}
	var currency string

	for _, f := range s.fees {
		if f.SubscriptionID != subscriptionID || f.Kind != "charge" {
			continue
		}
		currency = f.Currency
		p, ok := periodMap[f.BillingPeriod]
		if !ok {
			periodMap[f.BillingPeriod] = &TrendPoint{Period: f.BillingPeriod, AmountCents: f.AmountCents, Count: 1}
		} else {
			p.AmountCents += f.AmountCents
			p.Count++
		}
	}

	points := make([]TrendPoint, 0, len(periodMap))
	for _, p := range periodMap {
		points = append(points, *p)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Period < points[j].Period })

	var total int64
	for _, p := range points {
		total += p.AmountCents
	}
	var avg int64
	if len(points) > 0 {
		avg = total / int64(len(points))
	}

	return &Trend{
		SubscriptionID: subscriptionID,
		Currency:       currency,
		Points:         points,
		TotalCents:     total,
		AvgCents:       avg,
	}, nil
}
