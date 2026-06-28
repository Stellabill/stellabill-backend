package service

import (
	"math"
	"time"
)

// FeeRecord represents a single fee entry.
type FeeRecord struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Amount      float64   `json:"amount"`
	Currency    string    `json:"currency"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// FeeTrend holds trend analysis for a fee type over a period.
type FeeTrend struct {
	Type           string  `json:"type"`
	PeriodStart    string  `json:"period_start"`
	PeriodEnd      string  `json:"period_end"`
	TotalAmount    float64 `json:"total_amount"`
	AverageAmount  float64 `json:"average_amount"`
	Count          int     `json:"count"`
	ChangePercent  float64 `json:"change_percent"`
}

// FeeHistory is the response for fee history with trend analysis.
type FeeHistory struct {
	Records []FeeRecord `json:"records"`
	Trends  []FeeTrend  `json:"trends"`
}

// FeeService defines the interface for fee operations.
type FeeService interface {
	GetFeeHistory(feeType string, from, to time.Time) (*FeeHistory, error)
}

// inMemoryFeeService is a mock implementation for dev/test.
type inMemoryFeeService struct {
	records []FeeRecord
}

// NewFeeService returns a FeeService backed by in-memory mock data.
func NewFeeService() FeeService {
	now := time.Now().UTC()
	return &inMemoryFeeService{
		records: []FeeRecord{
			{ID: "fee-001", Type: "transaction", Amount: 1.50, Currency: "USD", Description: "Transaction fee", CreatedAt: now.AddDate(0, 0, -30)},
			{ID: "fee-002", Type: "transaction", Amount: 2.00, Currency: "USD", Description: "Transaction fee", CreatedAt: now.AddDate(0, 0, -20)},
			{ID: "fee-003", Type: "transaction", Amount: 1.75, Currency: "USD", Description: "Transaction fee", CreatedAt: now.AddDate(0, 0, -10)},
			{ID: "fee-004", Type: "subscription", Amount: 5.00, Currency: "USD", Description: "Subscription fee", CreatedAt: now.AddDate(0, 0, -25)},
			{ID: "fee-005", Type: "subscription", Amount: 5.00, Currency: "USD", Description: "Subscription fee", CreatedAt: now.AddDate(0, 0, -5)},
		},
	}
}

func (s *inMemoryFeeService) GetFeeHistory(feeType string, from, to time.Time) (*FeeHistory, error) {
	var filtered []FeeRecord
	for _, r := range s.records {
		if !r.CreatedAt.Before(from) && !r.CreatedAt.After(to) {
			if feeType == "" || r.Type == feeType {
				filtered = append(filtered, r)
			}
		}
	}
	if filtered == nil {
		filtered = []FeeRecord{}
	}

	trends := computeTrends(filtered, from, to)
	return &FeeHistory{Records: filtered, Trends: trends}, nil
}

// computeTrends groups records by type and computes basic trend metrics.
func computeTrends(records []FeeRecord, from, to time.Time) []FeeTrend {
	type bucket struct {
		total float64
		count int
	}
	byType := map[string]*bucket{}
	for _, r := range records {
		b := byType[r.Type]
		if b == nil {
			b = &bucket{}
			byType[r.Type] = b
		}
		b.total += r.Amount
		b.count++
	}

	periodStart := from.Format(time.RFC3339)
	periodEnd := to.Format(time.RFC3339)

	trends := make([]FeeTrend, 0, len(byType))
	for t, b := range byType {
		avg := 0.0
		if b.count > 0 {
			avg = b.total / float64(b.count)
		}
		// Simple trend: compare first half vs second half of the period
		mid := from.Add(to.Sub(from) / 2)
		var firstHalf, secondHalf float64
		for _, r := range records {
			if r.Type != t {
				continue
			}
			if r.CreatedAt.Before(mid) {
				firstHalf += r.Amount
			} else {
				secondHalf += r.Amount
			}
		}
		changePercent := 0.0
		if firstHalf != 0 {
			changePercent = math.Round(((secondHalf-firstHalf)/firstHalf)*10000) / 100
		}
		trends = append(trends, FeeTrend{
			Type:          t,
			PeriodStart:   periodStart,
			PeriodEnd:     periodEnd,
			TotalAmount:   math.Round(b.total*100) / 100,
			AverageAmount: math.Round(avg*100) / 100,
			Count:         b.count,
			ChangePercent: changePercent,
		})
	}
	return trends
}
