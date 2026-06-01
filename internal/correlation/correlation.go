package correlation

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	jobIDKey    contextKey = "job_id"
)

func NewID() string {
	return uuid.New().String()
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFromContext(ctx context.Context) string {
	id, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return id
}

func WithJobID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, jobIDKey, id)
}

func JobIDFromContext(ctx context.Context) string {
	id, ok := ctx.Value(jobIDKey).(string)
	if !ok {
		return ""
	}
	return id
}