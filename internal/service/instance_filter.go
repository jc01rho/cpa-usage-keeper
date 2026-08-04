package service

import (
	"context"
	"strings"
)

type instanceFilterContextKey struct{}

// ContextWithInstanceFilter carries an optional read-only instance scope
// through existing dashboard provider interfaces without widening auth seams.
func ContextWithInstanceFilter(ctx context.Context, instanceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, instanceFilterContextKey{}, strings.TrimSpace(instanceID))
}

func InstanceFilterFromContext(ctx context.Context) string {
	value, _ := InstanceFilterSelectionFromContext(ctx)
	return value
}

// InstanceFilterSelectionFromContext distinguishes an explicit all-instance
// selection (present with an empty value) from callers that did not traverse
// the HTTP instance-filter middleware.
func InstanceFilterSelectionFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(instanceFilterContextKey{}).(string)
	return strings.TrimSpace(value), ok
}
