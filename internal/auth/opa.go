package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/open-policy-agent/opa/rego"
)

type OPAPolicyEngine struct {
	mu           sync.RWMutex
	policyQuery  *rego.PreparedEvalQuery
	policyPath   string
	expectedHash string
}

func NewOPAPolicyEngine(ctx context.Context, policyPath string, expectedHash string) (*OPAPolicyEngine, error) {
	engine := &OPAPolicyEngine{
		policyPath:   policyPath,
		expectedHash: expectedHash,
	}

	if err := engine.loadPolicy(ctx); err != nil {
		return nil, fmt.Errorf("failed to load initial policy: %w", err)
	}

	go engine.watchSignals(ctx)

	return engine, nil
}

func (e *OPAPolicyEngine) loadPolicy(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := os.ReadFile(e.policyPath)
	if err != nil {
		e.policyQuery = nil
		return fmt.Errorf("read policy file: %w", err)
	}

	if e.expectedHash != "" {
		hash := sha256.Sum256(data)
		actualHash := hex.EncodeToString(hash[:])
		if actualHash != e.expectedHash {
			e.policyQuery = nil
			return errors.New("policy signature verification failed")
		}
	}

	query, err := rego.New(
		rego.Query("data.admin.authz.allow"),
		rego.Module(e.policyPath, string(data)),
	).PrepareForEval(ctx)

	if err != nil {
		e.policyQuery = nil
		return fmt.Errorf("prepare rego query: %w", err)
	}

	e.policyQuery = &query
	return nil
}

func (e *OPAPolicyEngine) watchSignals(ctx context.Context) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c:
			_ = e.loadPolicy(ctx)
		}
	}
}

func (e *OPAPolicyEngine) Authorize(ctx context.Context, token string) (bool, error) {
	e.mu.RLock()
	query := e.policyQuery
	e.mu.RUnlock()

	if query == nil {
		return false, nil
	}

	results, err := query.Eval(ctx, rego.EvalInput(map[string]interface{}{
		"token": token,
	}))
	if err != nil {
		return false, err
	}
	if len(results) == 0 {
		return false, nil
	}
	allow, ok := results[0].Expressions[0].Value.(bool)
	if !ok {
		return false, nil
	}
	return allow, nil
}
