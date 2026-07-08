package saga

import (
	"context"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("saga not found")

type memoryStore struct {
	mu      sync.RWMutex
	sagas   map[string]*Saga
	results map[string]map[string]*StepResult
}

func NewMemoryStore() Store {
	return &memoryStore{
		sagas:   make(map[string]*Saga),
		results: make(map[string]map[string]*StepResult),
	}
}

func (s *memoryStore) Save(_ context.Context, saga *Saga) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copy := *saga
	copy.Context = NewSagaContext(nil)
	for k, v := range saga.Context.Raw() {
		copy.Context.Set(k, v)
	}
	s.sagas[saga.ID] = &copy
	return nil
}

func (s *memoryStore) Load(_ context.Context, sagaID string) (*Saga, []StepResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	saga, exists := s.sagas[sagaID]
	if !exists {
		return nil, nil, ErrNotFound
	}

	copy := *saga
	copy.Context = NewSagaContext(nil)
	for k, v := range saga.Context.Raw() {
		copy.Context.Set(k, v)
	}

	var results []StepResult
	if stepMap, ok := s.results[sagaID]; ok {
		for _, sr := range stepMap {
			srCopy := *sr
			results = append(results, srCopy)
		}
	}

	return &copy, results, nil
}

func (s *memoryStore) SaveStepResult(_ context.Context, sagaID string, sr *StepResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sagas[sagaID]; !exists {
		return ErrNotFound
	}

	if s.results[sagaID] == nil {
		s.results[sagaID] = make(map[string]*StepResult)
	}

	copy := *sr
	if sr.ExecutedAt != nil {
		t := *sr.ExecutedAt
		copy.ExecutedAt = &t
	}
	if sr.CompensatedAt != nil {
		t := *sr.CompensatedAt
		copy.CompensatedAt = &t
	}

	s.results[sagaID][sr.StepKey] = &copy
	return nil
}

func (s *memoryStore) ListRunning(_ context.Context) ([]*Saga, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sagas []*Saga
	for _, saga := range s.sagas {
		if saga.Status == SagaRunning || saga.Status == SagaCompensating {
			copy := *saga
			copy.Context = NewSagaContext(nil)
			for k, v := range saga.Context.Raw() {
				copy.Context.Set(k, v)
			}
			sagas = append(sagas, &copy)
		}
	}

	sortByCreatedAt(sagas)
	return sagas, nil
}

func sortByCreatedAt(sagas []*Saga) {
	for i := 0; i < len(sagas); i++ {
		for j := i + 1; j < len(sagas); j++ {
			if sagas[j].CreatedAt.Before(sagas[i].CreatedAt) {
				sagas[i], sagas[j] = sagas[j], sagas[i]
			}
		}
	}
}

func (s *memoryStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sagas = make(map[string]*Saga)
	s.results = make(map[string]map[string]*StepResult)
}
