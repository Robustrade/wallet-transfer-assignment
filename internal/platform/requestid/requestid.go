package requestid

import "context"

type contextKey struct{}

// With stores the request id on ctx.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request id, or an empty string.
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
