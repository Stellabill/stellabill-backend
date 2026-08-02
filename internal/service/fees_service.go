package service

import (
	"context"
	"errors"
	"math"
	"time"

	"stellarbill-backend/internal/errcode"

	"github.com/shopspring/decimal"
)

// CurrencyScale defines the number of decimal places for a given ISO 4217
// currency code.  Currencies not listed default to 2 decimal places.
//
//   - 0 decimal places: JPY, KRW, VND, CLP, GNF, MGA, PYG, RWF, UGX, XAF, XOF, XPF
//   - 3 decimal places: BHD, IQD, JOD, KWD, LYD, OMR, TND
var CurrencyScale = map[string]int32{
	// Zero-decimal currencies
	"JPY": 0, "KRW": 0, "VND": 0, "CLP": 0,
	"GNF": 0, "MGA": 0, "PYG": 0, "RWF": 0,
	"UGX": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	// Three-decimal currencies
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3,
	"LYD": 3, "OMR": 3, "TND": 3,
}

// scaleForCurrency returns the number of minor-unit decimal places for the
// given ISO 4217 currency code. Unknown currencies default to 2.
func scaleForCurrency(currency string) int32 {
	if s, ok := CurrencyScale[currency]; ok {
		return s
	}
	return 2
}

// ErrInvalidAmount is returned when an input amount is negative or unparseable.
var ErrInvalidAmount = errors.New("amount must be non-negative")

// ErrInvalidTaxRate is returned when a tax rate is outside [0, 1].
var ErrInvalidTaxRate = errors.New("tax rate must be between 0 and 1 inclusive")

// ErrInvalidParts is returned when the number of proration parts is ≤ 0.
var ErrInvalidParts = errors.New("parts must be greater than zero")

func init() {
	errcode.Register(func(err error) bool { return errors.Is(err, ErrInvalidAmount) }, errcode.CodeFeeInvalidAmount)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrInvalidTaxRate) }, errcode.CodeFeeInvalidTaxRate)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrInvalidParts) }, errcode.CodeFeeInvalidParts)
}

// MoneyAmount holds a currency-aware decimal amount.
type MoneyAmount struct {
	Value    decimal.Decimal
	Currency string
}

// RoundMoney rounds amount to the correct number of minor units for the
// given currency using banker's rounding (RoundHalfEven) to minimise
// systematic drift across large volumes of transactions.
func RoundMoney(amount decimal.Decimal, currency string) decimal.Decimal {
	scale := scaleForCurrency(currency)
	return amount.RoundBank(scale)
}

// ProrateFee splits amount into n equal parts for currency, distributing
// any sub-unit remainder to the last part so the sum is exactly amount.
//
// Invariants guaranteed:
//   - len(result) == parts
//   - sum(result) == RoundMoney(amount, currency)
//   - every result[i] >= 0
func ProrateFee(amount decimal.Decimal, currency string, parts int) ([]decimal.Decimal, error) {
	if amount.IsNegative() {
		return nil, ErrInvalidAmount
	}
	if parts <= 0 {
		return nil, ErrInvalidParts
	}

	rounded := RoundMoney(amount, currency)
	scale := scaleForCurrency(currency)

	// Each part in minor units avoids floating-point drift.
	//   base    = floor(rounded / parts)
	//   remainder is distributed to the first r parts (standard "largest-remainder")
	minor := rounded.Shift(scale)                            // e.g. 10.00 USD → 1000
	partsD := decimal.NewFromInt(int64(parts))
	base := minor.Div(partsD).Floor()                        // integer quotient
	remainder := minor.Sub(base.Mul(partsD))                 // 0 ≤ remainder < parts
	remainderInt := remainder.IntPart()

	result := make([]decimal.Decimal, parts)
	for i := 0; i < parts; i++ {
		v := base
		if int64(i) < remainderInt {
			v = v.Add(decimal.NewFromInt(1))
		}
		result[i] = v.Shift(-scale)
	}
	return result, nil
}

// TaxSplit decomposes amount into a tax component and a net component for the
// given currency and tax rate (expressed as a fraction in [0,1]).
//
// tax = RoundMoney(amount × rate, currency)
// net = amount − tax   (exact, may carry sub-unit precision before external rounding)
//
// Invariants guaranteed:
//   - tax + net == amount  (after rounding applied to tax)
//   - tax >= 0, net >= 0
//   - rate must be in [0, 1]
func TaxSplit(amount decimal.Decimal, currency string, rate decimal.Decimal) (tax, net decimal.Decimal, err error) {
	if amount.IsNegative() {
		return decimal.Zero, decimal.Zero, ErrInvalidAmount
	}
	zero := decimal.Zero
	one := decimal.NewFromInt(1)
	if rate.LessThan(zero) || rate.GreaterThan(one) {
		return decimal.Zero, decimal.Zero, ErrInvalidTaxRate
	}

	tax = RoundMoney(amount.Mul(rate), currency)
	net = amount.Sub(tax)
	return tax, net, nil
}

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
	Type          string  `json:"type"`
	PeriodStart   string  `json:"period_start"`
	PeriodEnd     string  `json:"period_end"`
	TotalAmount   float64 `json:"total_amount"`
	AverageAmount float64 `json:"average_amount"`
	Count         int     `json:"count"`
	ChangePercent float64 `json:"change_percent"`
}

// FeeHistory is the response for fee history with trend analysis.
type FeeHistory struct {
	Records []FeeRecord `json:"records"`
	Trends  []FeeTrend  `json:"trends"`

	// LastRefreshedAt is the time the underlying revenue aggregate
	// (mv_fee_revenue_monthly) was last refreshed. It is nil when the aggregate
	// has never been refreshed, or when the report is served from raw data
	// rather than the materialized view.
	LastRefreshedAt *time.Time `json:"last_refreshed_at,omitempty"`

	// Stale indicates the materialized view's data is older than the freshness
	// threshold but is being served anyway (stale-but-served). Clients can use
	// this to surface a "data may be delayed" notice.
	Stale bool `json:"stale,omitempty"`
}

// FreshnessProvider reports the freshness of the fee-revenue materialized view.
// It is implemented by the refresh worker so the report handler can annotate
// responses with last_refreshed_at and a stale-but-served flag without coupling
// the HTTP layer to the database.
type FreshnessProvider interface {
	// IsStale reports whether the aggregate is older than the staleness
	// threshold as of now. lastRefreshed is the recorded refresh time;
	// never is true when the view has never been refreshed.
	IsStale(ctx context.Context, now time.Time) (stale bool, lastRefreshed time.Time, never bool, err error)
}

// WithFreshness annotates a FeeHistory with freshness metadata from the
// provider. A nil provider leaves the response unannotated (raw-data path).
// Errors from the provider are returned so the caller can decide whether to
// fail the request or serve without freshness metadata.
func (h *FeeHistory) WithFreshness(ctx context.Context, p FreshnessProvider, now time.Time) error {
	if h == nil || p == nil {
		return nil
	}
	stale, lastRefreshed, never, err := p.IsStale(ctx, now)
	if err != nil {
		return err
	}
	h.Stale = stale
	if never {
		h.LastRefreshedAt = nil
		return nil
	}
	lr := lastRefreshed
	h.LastRefreshedAt = &lr
	return nil
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
