package reconciliation

import (
	"context"
	"fmt"
	"time"
)

// Snapshot represents a single subscription state exported from the contract/ledger.
type Snapshot struct {
	SubscriptionID string           `json:"subscription_id"`
	Status         string           `json:"status"`
	Amount         int64            `json:"amount"`
	Currency       string           `json:"currency"`
	Interval       string           `json:"interval"`
	Balances       map[string]int64 `json:"balances"`
	ExportedAt     time.Time        `json:"exported_at"`
}

// BackendSubscription represents the subscription as stored in the backend DB.
type BackendSubscription struct {
	SubscriptionID string           `json:"subscription_id"`
	Status         string           `json:"status"`
	Amount         int64            `json:"amount"`
	Currency       string           `json:"currency"`
	Interval       string           `json:"interval"`
	Balances       map[string]int64 `json:"balances"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// FieldMismatch records a single differing field between backend and contract.
type FieldMismatch struct {
	Field         string `json:"field"`
	BackendValue  string `json:"backend_value"`
	ContractValue string `json:"contract_value"`
}

// Report contains the reconciliation result for a subscription.
type Report struct {
	SubscriptionID string              `json:"subscription_id"`
	Matched        bool                `json:"matched"`
	Mismatches     []FieldMismatch     `json:"mismatches"`
	Backend        BackendSubscription `json:"backend"`
	Contract       Snapshot            `json:"contract"`
}

// Adapter defines how to fetch contract snapshots from an integration layer.
type Adapter interface {
	// FetchSnapshots returns current contract snapshots. Implementations may return
	// partial data; callers must handle missing items.
	FetchSnapshots(ctx context.Context) ([]Snapshot, error)
}

// Reconciler performs comparisons between backend state and contract snapshots.
type Reconciler struct {
	// Clock can be overridden in tests; nil will use time.Now.
	Clock func() time.Time
}

// New creates a new Reconciler.
func New() *Reconciler {
	return &Reconciler{Clock: time.Now}
}

// Compare compares a backend subscription with a contract snapshot and returns a report.
// If snapshot is nil (not available) the report marks a single mismatch about missing snapshot.
func (r *Reconciler) Compare(backend BackendSubscription, contract *Snapshot) Report {
	var rep Report
	rep.SubscriptionID = backend.SubscriptionID
	rep.Backend = backend
	if contract == nil {
		rep.Matched = false
		rep.Mismatches = append(rep.Mismatches, FieldMismatch{
			Field:         "contract_snapshot",
			BackendValue:  "present",
			ContractValue: "missing",
		})
		return rep
	}
	rep.Contract = *contract

	// stale snapshot check: if contract exported much earlier than backend updated.
	if contract.ExportedAt.Before(backend.UpdatedAt.Add(-24 * time.Hour)) {
		rep.Mismatches = append(rep.Mismatches, FieldMismatch{
			Field:         "snapshot_stale",
			BackendValue:  backend.UpdatedAt.UTC().String(),
			ContractValue: contract.ExportedAt.UTC().String(),
		})
	}

	// compare key scalar fields
	if backend.Status != contract.Status {
		rep.Mismatches = append(rep.Mismatches, FieldMismatch{
			Field:         "status",
			BackendValue:  backend.Status,
			ContractValue: contract.Status,
		})
	}
	if backend.Amount != contract.Amount || backend.Currency != contract.Currency {
		rep.Mismatches = append(rep.Mismatches, FieldMismatch{
			Field:         "amount",
			BackendValue:  fmt.Sprintf("%d %s", backend.Amount, backend.Currency),
			ContractValue: fmt.Sprintf("%d %s", contract.Amount, contract.Currency),
		})
	}
	if backend.Interval != contract.Interval {
		rep.Mismatches = append(rep.Mismatches, FieldMismatch{
			Field:         "interval",
			BackendValue:  backend.Interval,
			ContractValue: contract.Interval,
		})
	}

	// compare balances map - check keys and values
	// keys present in backend but not in contract and vice versa are mismatches
	// collect a canonical string for each differing entry
	for k, v := range backend.Balances {
		if cv, ok := contract.Balances[k]; ok {
			if v != cv {
				rep.Mismatches = append(rep.Mismatches, FieldMismatch{
					Field:         fmt.Sprintf("balances.%s", k),
					BackendValue:  fmt.Sprintf("%d", v),
					ContractValue: fmt.Sprintf("%d", cv),
				})
			}
		} else {
			rep.Mismatches = append(rep.Mismatches, FieldMismatch{
				Field:         fmt.Sprintf("balances.%s", k),
				BackendValue:  fmt.Sprintf("%d", v),
				ContractValue: "missing",
			})
		}
	}
	for k, cv := range contract.Balances {
		if _, ok := backend.Balances[k]; !ok {
			rep.Mismatches = append(rep.Mismatches, FieldMismatch{
				Field:         fmt.Sprintf("balances.%s", k),
				BackendValue:  "missing",
				ContractValue: fmt.Sprintf("%d", cv),
			})
		}
	}

	rep.Matched = len(rep.Mismatches) == 0
	return rep
}

