package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/repository"

	"github.com/google/uuid"
)

var (
	// ErrPlanTemplateNotFound is returned when a plan template doesn't exist.
	ErrPlanTemplateNotFound = errors.New("plan template not found")

	// ErrPlanTemplateDeprecated is returned when trying to use a deprecated plan template.
	ErrPlanTemplateDeprecated = errors.New("plan template is deprecated")

	// ErrDuplicatePlanName is returned when a merchant tries to create a plan with a duplicate name.
	ErrDuplicatePlanName = errors.New("plan name already exists for this merchant")

	// ErrInvalidPlanTemplate is returned when plan template validation fails.
	ErrInvalidPlanTemplate = errors.New("invalid plan template")
)

// PlanTemplateService defines the business logic interface for plan templates.
type PlanTemplateService interface {
	RegisterPlan(ctx context.Context, merchantID string, req RegisterPlanRequest) (*PlanTemplateDetail, error)
	DeprecatePlan(ctx context.Context, merchantID string, planID string) error
	GetPlanTemplate(ctx context.Context, merchantID string, planID string) (*PlanTemplateDetail, error)
	ListPlanTemplates(ctx context.Context, merchantID string, includeDeprecated bool) ([]*PlanTemplateDetail, error)
}

// planTemplateService is the concrete implementation.
type planTemplateService struct {
	repo           repository.PlanTemplateRepository
	outboxPublisher outbox.Publisher
}

// NewPlanTemplateService creates a new plan template service.
func NewPlanTemplateService(repo repository.PlanTemplateRepository, outboxPublisher outbox.Publisher) PlanTemplateService {
	return &planTemplateService{
		repo:           repo,
		outboxPublisher: outboxPublisher,
	}
}

// RegisterPlanRequest holds the data for registering a new plan template.
type RegisterPlanRequest struct {
	Name            string `json:"name" binding:"required"`
	AmountCents     int64  `json:"amount_cents" binding:"required,min=0"`
	Currency        string `json:"currency" binding:"required,len=3"`
	IntervalSeconds int    `json:"interval_seconds" binding:"required,min=1"`
	TrialSeconds    int    `json:"trial_seconds" binding:"min=0"`
}

// PlanTemplateDetail is the response payload for plan templates.
type PlanTemplateDetail struct {
	ID              string     `json:"id"`
	MerchantID      string     `json:"merchant_id"`
	Name            string     `json:"name"`
	AmountCents     int64      `json:"amount_cents"`
	Currency        string     `json:"currency"`
	IntervalSeconds int        `json:"interval_seconds"`
	TrialSeconds    int        `json:"trial_seconds"`
	DeprecatedAt    *time.Time `json:"deprecated_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// PlanRegisteredEvent is emitted when a plan template is registered.
type PlanRegisteredEvent struct {
	PlanID          string    `json:"plan_id"`
	MerchantID      string    `json:"merchant_id"`
	Name            string    `json:"name"`
	AmountCents     int64     `json:"amount_cents"`
	Currency        string    `json:"currency"`
	IntervalSeconds int       `json:"interval_seconds"`
	TrialSeconds    int       `json:"trial_seconds"`
	Timestamp       time.Time `json:"timestamp"`
}

// PlanDeprecatedEvent is emitted when a plan template is deprecated.
type PlanDeprecatedEvent struct {
	PlanID     string    `json:"plan_id"`
	MerchantID string    `json:"merchant_id"`
	Timestamp  time.Time `json:"timestamp"`
}

// RegisterPlan creates a new plan template and emits a PlanRegisteredEvent.
func (s *planTemplateService) RegisterPlan(ctx context.Context, merchantID string, req RegisterPlanRequest) (*PlanTemplateDetail, error) {
	// Validate request
	if err := validateRegisterPlanRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPlanTemplate, err)
	}

	// Check for duplicate name
	existing, err := s.repo.FindByMerchantAndName(ctx, merchantID, req.Name)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrDuplicatePlanName
	}

	// Create plan template
	template := &repository.PlanTemplateRow{
		ID:              uuid.New().String(),
		MerchantID:      merchantID,
		Name:            req.Name,
		AmountCents:     req.AmountCents,
		Currency:        req.Currency,
		IntervalSeconds: req.IntervalSeconds,
		TrialSeconds:    req.TrialSeconds,
	}

	if err := s.repo.Create(ctx, template); err != nil {
		return nil, err
	}

	// Emit event
	event := PlanRegisteredEvent{
		PlanID:          template.ID,
		MerchantID:      merchantID,
		Name:            req.Name,
		AmountCents:     req.AmountCents,
		Currency:        req.Currency,
		IntervalSeconds: req.IntervalSeconds,
		TrialSeconds:    req.TrialSeconds,
		Timestamp:       time.Now().UTC(),
	}

	if err := s.publishEvent(ctx, "plan.registered", event, template.ID, "plan_template"); err != nil {
		// Log error but don't fail the operation
		// The template was created successfully
	}

	// Return detail
	return &PlanTemplateDetail{
		ID:              template.ID,
		MerchantID:      template.MerchantID,
		Name:            template.Name,
		AmountCents:     template.AmountCents,
		Currency:        template.Currency,
		IntervalSeconds: template.IntervalSeconds,
		TrialSeconds:    template.TrialSeconds,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}, nil
}

// DeprecatePlan marks a plan template as deprecated and emits a PlanDeprecatedEvent.
func (s *planTemplateService) DeprecatePlan(ctx context.Context, merchantID string, planID string) error {
	// Verify the plan exists and belongs to the merchant
	template, err := s.repo.FindByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrPlanTemplateNotFound
		}
		return err
	}

	if template.MerchantID != merchantID {
		return ErrForbidden
	}

	if template.DeprecatedAt != nil {
		// Already deprecated
		return nil
	}

	// Deprecate the plan
	if err := s.repo.Deprecate(ctx, planID, merchantID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrPlanTemplateNotFound
		}
		return err
	}

	// Emit event
	event := PlanDeprecatedEvent{
		PlanID:     planID,
		MerchantID: merchantID,
		Timestamp:  time.Now().UTC(),
	}

	if err := s.publishEvent(ctx, "plan.deprecated", event, planID, "plan_template"); err != nil {
		// Log error but don't fail the operation
	}

	return nil
}

// GetPlanTemplate retrieves a single plan template.
func (s *planTemplateService) GetPlanTemplate(ctx context.Context, merchantID string, planID string) (*PlanTemplateDetail, error) {
	template, err := s.repo.FindByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrPlanTemplateNotFound
		}
		return nil, err
	}

	if template.MerchantID != merchantID {
		return nil, ErrForbidden
	}

	return toPlanTemplateDetail(template), nil
}

// ListPlanTemplates returns all plan templates for a merchant.
func (s *planTemplateService) ListPlanTemplates(ctx context.Context, merchantID string, includeDeprecated bool) ([]*PlanTemplateDetail, error) {
	templates, err := s.repo.ListByMerchant(ctx, merchantID, includeDeprecated)
	if err != nil {
		return nil, err
	}

	details := make([]*PlanTemplateDetail, len(templates))
	for i, t := range templates {
		details[i] = toPlanTemplateDetail(t)
	}

	return details, nil
}

// publishEvent publishes an event to the outbox.
func (s *planTemplateService) publishEvent(ctx context.Context, eventType string, eventData interface{}, aggregateID string, aggregateType string) error {
	if s.outboxPublisher == nil {
		return nil // No-op if outbox is not configured
	}

	data, err := json.Marshal(eventData)
	if err != nil {
		return err
	}

	event := &outbox.Event{
		ID:            uuid.New(),
		EventType:     eventType,
		EventData:     data,
		AggregateID:   &aggregateID,
		AggregateType: &aggregateType,
		OccurredAt:    time.Now().UTC(),
		Status:        outbox.StatusPending,
		RetryCount:    0,
		MaxRetries:    3,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	return s.outboxPublisher.Publish(ctx, event)
}

// toPlanTemplateDetail converts a repository row to a service detail.
func toPlanTemplateDetail(template *repository.PlanTemplateRow) *PlanTemplateDetail {
	return &PlanTemplateDetail{
		ID:              template.ID,
		MerchantID:      template.MerchantID,
		Name:            template.Name,
		AmountCents:     template.AmountCents,
		Currency:        template.Currency,
		IntervalSeconds: template.IntervalSeconds,
		TrialSeconds:    template.TrialSeconds,
		DeprecatedAt:    template.DeprecatedAt,
		CreatedAt:       template.CreatedAt,
		UpdatedAt:       template.UpdatedAt,
	}
}

// validateRegisterPlanRequest validates the register plan request.
func validateRegisterPlanRequest(req RegisterPlanRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.AmountCents < 0 {
		return errors.New("amount_cents must be non-negative")
	}
	if len(req.Currency) != 3 {
		return errors.New("currency must be a 3-letter ISO code")
	}
	if req.IntervalSeconds <= 0 {
		return errors.New("interval_seconds must be positive")
	}
	if req.TrialSeconds < 0 {
		return errors.New("trial_seconds must be non-negative")
	}
	return nil
}
