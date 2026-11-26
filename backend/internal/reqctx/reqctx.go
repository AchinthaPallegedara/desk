// Package reqctx carries per-request values (request ID) across packages.
package reqctx

import "context"

type key int

const requestIDKey key = iota

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}
