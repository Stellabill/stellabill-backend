package graphql

import "context"

type contextKey string

const (
	callerIDKey  contextKey = "callerID"
	tenantIDKey  contextKey = "tenantID"
	rolesKey     contextKey = "roles"
)

// WithCallerContext injects callerID, tenantID, and roles into a context.
func WithCallerContext(ctx context.Context, callerID, tenantID string, roles []string) context.Context {
	ctx = context.WithValue(ctx, callerIDKey, callerID)
	ctx = context.WithValue(ctx, tenantIDKey, tenantID)
	ctx = context.WithValue(ctx, rolesKey, roles)
	return ctx
}

func callerIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(callerIDKey).(string)
	return v
}

func tenantIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(tenantIDKey).(string)
	return v
}

func rolesFromCtx(ctx context.Context) []string {
	v, _ := ctx.Value(rolesKey).([]string)
	return v
}
