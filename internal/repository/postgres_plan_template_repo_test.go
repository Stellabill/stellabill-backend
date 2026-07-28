package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
)

// ---- minimal in-memory fake planDB for unit tests ----

type fakePlanTemplateDB struct {
	rows map[string]*ptRow
}

type ptRow struct {
	id              string
	merchantID      string
	name            string
	amountCents     int64
	currency        string
	intervalSeconds int
	trialSeconds    int
	deprecatedAt    *time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

func newFakePlanTemplateDB() *fakePlanTemplateDB {
	return &fakePlanTemplateDB{rows: make(map[string]*ptRow)}
}

// QueryContext satisfies planDB for QueryRowContext path
func (f *fakePlanTemplateDB) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, nil // not used directly in these unit tests
}

// QueryRowContext is not needed since we test via ExecContext/QueryContext
func (f *fakePlanTemplateDB) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return nil
}

// ExecContext is not needed
func (f *fakePlanTemplateDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, nil
}

// --- We use a dedicated stub that intercepts the calls ---

// mockPlanTemplateRepo is a pure in-memory implementation of PlanTemplateRepository
// used to test the service layer without a real DB.
type mockPlanTemplateRepo struct {
	templates map[string]*repository.PlanTemplateRow
	// simulate error injection
	createErr     error
	findErr       error
	deprecateErr  error
}

func newMockPlanTemplateRepo() *mockPlanTemplateRepo {
	return &mockPlanTemplateRepo{templates: make(map[string]*repository.PlanTemplateRow)}
}

func (m *mockPlanTemplateRepo) Create(_ context.Context, t *repository.PlanTemplateRow) error {
	if m.createErr != nil {
		return m.createErr
	}
	// Check unique(merchant_id, name)
	for _, existing := range m.templates {
		if existing.MerchantID == t.MerchantID && existing.Name == t.Name {
			return repository.ErrNotFound // simulate constraint violation differently
		}
	}
	copy := *t
	copy.CreatedAt = time.Now().UTC()
	copy.UpdatedAt = time.Now().UTC()
	m.templates[t.ID] = &copy
	return nil
}

func (m *mockPlanTemplateRepo) FindByID(_ context.Context, id string) (*repository.PlanTemplateRow, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	row, ok := m.templates[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copy := *row
	return &copy, nil
}

func (m *mockPlanTemplateRepo) FindByMerchantAndName(_ context.Context, merchantID, name string) (*repository.PlanTemplateRow, error) {
	for _, row := range m.templates {
		if row.MerchantID == merchantID && row.Name == name {
			copy := *row
			return &copy, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *mockPlanTemplateRepo) ListByMerchant(_ context.Context, merchantID string, includeDeprecated bool) ([]*repository.PlanTemplateRow, error) {
	var results []*repository.PlanTemplateRow
	for _, row := range m.templates {
		if row.MerchantID != merchantID {
			continue
		}
		if !includeDeprecated && row.DeprecatedAt != nil {
			continue
		}
		copy := *row
		results = append(results, &copy)
	}
	return results, nil
}

func (m *mockPlanTemplateRepo) Deprecate(_ context.Context, id, merchantID string) error {
	if m.deprecateErr != nil {
		return m.deprecateErr
	}
	row, ok := m.templates[id]
	if !ok || row.MerchantID != merchantID {
		return repository.ErrNotFound
	}
	now := time.Now().UTC()
	row.DeprecatedAt = &now
	return nil
}

// ---- interface assertion ----
var _ repository.PlanTemplateRepository = (*mockPlanTemplateRepo)(nil)

// ---- TESTS ----

func TestMockPlanTemplateRepo_Create(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	template := &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Basic Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 2592000,
		TrialSeconds:    0,
	}

	if err := repo.Create(ctx, template); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	// Verify it was stored
	got, err := repo.FindByID(ctx, "pt-001")
	if err != nil {
		t.Fatalf("FindByID() unexpected error: %v", err)
	}
	if got.Name != "Basic Plan" {
		t.Errorf("Name = %q, want %q", got.Name, "Basic Plan")
	}
}

func TestMockPlanTemplateRepo_Create_DuplicateName(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	first := &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Basic Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 2592000,
	}

	second := &repository.PlanTemplateRow{
		ID:              "pt-002", // different ID
		MerchantID:      "merchant-1",
		Name:            "Basic Plan", // same name
		AmountCents:     1999,
		Currency:        "USD",
		IntervalSeconds: 2592000,
	}

	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create() first unexpected error: %v", err)
	}

	// Second with same merchant_id+name should fail
	err := repo.Create(ctx, second)
	if err == nil {
		t.Error("expected error for duplicate merchant+name, got nil")
	}
}

func TestMockPlanTemplateRepo_FindByID_NotFound(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "nonexistent")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMockPlanTemplateRepo_FindByMerchantAndName(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	template := &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Pro Plan",
		AmountCents:     4999,
		Currency:        "EUR",
		IntervalSeconds: 86400,
	}

	if err := repo.Create(ctx, template); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	got, err := repo.FindByMerchantAndName(ctx, "merchant-1", "Pro Plan")
	if err != nil {
		t.Fatalf("FindByMerchantAndName() unexpected error: %v", err)
	}
	if got.ID != "pt-001" {
		t.Errorf("ID = %q, want %q", got.ID, "pt-001")
	}
}

func TestMockPlanTemplateRepo_FindByMerchantAndName_NotFound(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	_, err := repo.FindByMerchantAndName(ctx, "merchant-1", "nonexistent")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMockPlanTemplateRepo_ListByMerchant_ExcludesDeprecated(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	active := &repository.PlanTemplateRow{
		ID:              "pt-active",
		MerchantID:      "merchant-1",
		Name:            "Active Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 86400,
	}

	deprecated := &repository.PlanTemplateRow{
		ID:              "pt-deprecated",
		MerchantID:      "merchant-1",
		Name:            "Old Plan",
		AmountCents:     499,
		Currency:        "USD",
		IntervalSeconds: 86400,
	}

	repo.Create(ctx, active)
	repo.Create(ctx, deprecated)

	// Deprecate one
	repo.Deprecate(ctx, "pt-deprecated", "merchant-1")

	// List without deprecated
	results, err := repo.ListByMerchant(ctx, "merchant-1", false)
	if err != nil {
		t.Fatalf("ListByMerchant() unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("got %d templates, want 1", len(results))
	}
	if results[0].ID != "pt-active" {
		t.Errorf("ID = %q, want pt-active", results[0].ID)
	}
}

func TestMockPlanTemplateRepo_ListByMerchant_IncludesDeprecated(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	for i, name := range []string{"Plan A", "Plan B"} {
		repo.Create(ctx, &repository.PlanTemplateRow{
			ID:              "pt-" + name,
			MerchantID:      "merchant-1",
			Name:            name,
			AmountCents:     int64(999 * (i + 1)),
			Currency:        "USD",
			IntervalSeconds: 86400,
		})
	}

	repo.Deprecate(ctx, "pt-Plan A", "merchant-1")

	results, err := repo.ListByMerchant(ctx, "merchant-1", true)
	if err != nil {
		t.Fatalf("ListByMerchant(includeDeprecated=true) unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d templates, want 2", len(results))
	}
}

func TestMockPlanTemplateRepo_Deprecate(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	repo.Create(ctx, &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Basic Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 86400,
	})

	if err := repo.Deprecate(ctx, "pt-001", "merchant-1"); err != nil {
		t.Fatalf("Deprecate() unexpected error: %v", err)
	}

	got, _ := repo.FindByID(ctx, "pt-001")
	if got.DeprecatedAt == nil {
		t.Error("DeprecatedAt should be set after deprecation")
	}
}

func TestMockPlanTemplateRepo_Deprecate_WrongMerchant(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	repo.Create(ctx, &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Basic Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 86400,
	})

	err := repo.Deprecate(ctx, "pt-001", "wrong-merchant")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for wrong merchant, got %v", err)
	}
}

func TestMockPlanTemplateRepo_Deprecate_NotFound(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	err := repo.Deprecate(ctx, "nonexistent", "merchant-1")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPlanTemplateRow_DeprecatedAt_InitiallyNil(t *testing.T) {
	repo := newMockPlanTemplateRepo()
	ctx := context.Background()

	repo.Create(ctx, &repository.PlanTemplateRow{
		ID:              "pt-001",
		MerchantID:      "merchant-1",
		Name:            "Basic Plan",
		AmountCents:     999,
		Currency:        "USD",
		IntervalSeconds: 86400,
	})

	got, _ := repo.FindByID(ctx, "pt-001")
	if got.DeprecatedAt != nil {
		t.Error("DeprecatedAt should be nil for a newly created template")
	}
}
