package ingestion

import (
	"context"
	"errors"

	"stellarbill-backend/internal/timeutil"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Service errors.
var (
	ErrDuplicateEvent = errors.New("duplicate event: idempotency key already processed")
	ErrOutOfOrder     = errors.New("out-of-order event: sequence_num is not greater than latest")
)

// Service implements EventConsumer by parsing, deduplicating, and persisting
// contract events into the read-model store.
type Service struct {
	repo   EventRepository
	logger *logrus.Entry
}

// NewService constructs an ingestion Service.
func NewService(repo EventRepository, logger *logrus.Logger) *Service {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Service{
		repo:   repo,
		logger: logger.WithField("component", "ingestion"),
	}
}

// Consume validates, deduplicates, and persists a raw contract event.
// It satisfies the EventConsumer interface.
func (s *Service) Consume(ctx context.Context, raw RawEvent) (*ContractEvent, error) {
	// 1. Parse and validate.
	parsed, err := Parse(raw)
	if err != nil {
		s.logger.WithError(err).WithField("idempotency_key", raw.IdempotencyKey).
			Warn("event parse failed")
		return nil, err
	}

	// 2. Idempotency check.
	exists, err := s.repo.ExistsByIdempotencyKey(ctx, parsed.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if exists {
		s.logger.WithField("idempotency_key", parsed.IdempotencyKey).
			Info("duplicate event skipped")
		return nil, ErrDuplicateEvent
	}

	// 3. Out-of-order detection.
	latestSeq, err := s.repo.LatestSequenceForContract(ctx, parsed.ContractID)
	if err != nil {
		return nil, err
	}
	if parsed.SequenceNum > 0 && parsed.SequenceNum <= latestSeq {
		s.logger.WithFields(logrus.Fields{
			"contract_id": parsed.ContractID,
			"received":    parsed.SequenceNum,
			"latest":      latestSeq,
		}).Warn("out-of-order event detected")
		return nil, ErrOutOfOrder
	}

	// 4. Build normalised record.
	event := &ContractEvent{
		ID:             uuid.New().String(),
		IdempotencyKey: parsed.IdempotencyKey,
		EventType:      parsed.EventType,
		ContractID:     parsed.ContractID,
		TenantID:       parsed.TenantID,
		Payload:        parsed.Payload,
		OccurredAt:     parsed.OccurredAt,
		IngestedAt:     timeutil.NowUTC(),
		SequenceNum:    parsed.SequenceNum,
		Status:         "processed",
	}

	// 5. Persist.
	if err := s.repo.Insert(ctx, event); err != nil {
		s.logger.WithError(err).WithField("event_id", event.ID).
			Error("failed to persist contract event")
		return nil, err
	}

	s.logger.WithFields(logrus.Fields{
		"event_id":    event.ID,
		"event_type":  event.EventType,
		"contract_id": event.ContractID,
	}).Info("contract event ingested")

	return event, nil
}
