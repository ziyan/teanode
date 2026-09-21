package web

import (
	"context"
)

type contextKey int

const (
	serverKey contextKey = iota
)

func contextWithServer(ctx context.Context, server *server) context.Context {
	return context.WithValue(ctx, serverKey, server)
}

//nolint:unused
func contextServer(ctx context.Context) *server {
	return ctx.Value(serverKey).(*server)
}
