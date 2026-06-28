package outbox

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
)

type ChaosPublisher struct {
	inner Publisher
	prob  float64
}

func NewChaosPublisher(inner Publisher) Publisher {
	prob, _ := strconv.ParseFloat(os.Getenv("CHAOS_OUTBOX_PROB"), 64)
	return &ChaosPublisher{inner: inner, prob: prob}
}

func (p *ChaosPublisher) Publish(ctx context.Context, event *Event) error {
	if !isStagingEnv() || p.prob <= 0 || rand.Float64() >= p.prob {
		return p.inner.Publish(ctx, event)
	}
	ChaosOutboxCancellationsTotal.Inc()
	return context.Canceled
}

func isStagingEnv() bool {
	return strings.ToLower(os.Getenv("ENV")) == "staging"
}
