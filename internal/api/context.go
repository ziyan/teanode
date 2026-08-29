package api

import (
	"context"
	"net/http"

	"github.com/ziyan/teanode/internal/db"
)

type contextKey int

const (
	requestKey contextKey = iota
	responseKey
	txKey
	authenticatedUsernameKey
)

// AuthenticatedUsernameHeader carries the operator established by the
// authentication middleware to the handlers behind it. It is stripped from
// every incoming request first, so a client cannot set it themselves.
const AuthenticatedUsernameHeader = "X-TeaNode-Authenticated-User"

func ContextWithRequest(ctx context.Context, request *http.Request) context.Context {
	return context.WithValue(ctx, requestKey, request)
}

func ContextRequest(ctx context.Context) *http.Request {
	value := ctx.Value(requestKey)
	if value != nil {
		return value.(*http.Request)
	}
	return nil
}

// ContextWithResponse carries the response writer to the resolvers.
//
// Almost nothing needs it: a GraphQL reply is the return value, not something
// a resolver writes. Logging in is the exception, because a browser's
// credential is a cookie and a cookie is a response header.
func ContextWithResponse(ctx context.Context, response http.ResponseWriter) context.Context {
	return context.WithValue(ctx, responseKey, response)
}

func ContextResponse(ctx context.Context) http.ResponseWriter {
	value := ctx.Value(responseKey)
	if value != nil {
		return value.(http.ResponseWriter)
	}
	return nil
}

func ContextWithTx(ctx context.Context, tx db.Transaction) context.Context {
	return context.WithValue(ctx, txKey, tx)
}

func ContextTx(ctx context.Context) db.Transaction {
	value := ctx.Value(txKey)
	if value != nil {
		return value.(db.Transaction)
	}
	return nil
}

// ContextWithAuthenticatedUsername records the operator the authentication
// middleware established, so that a resolver can attribute a change.
func ContextWithAuthenticatedUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, authenticatedUsernameKey, username)
}

func ContextAuthenticatedUsername(ctx context.Context) string {
	value := ctx.Value(authenticatedUsernameKey)
	if value != nil {
		return value.(string)
	}
	return ""
}

// UsernameFromRequest reads the operator established by the authentication
// middleware. It is empty when the server has no accounts configured, which
// leaves the API open — a state the server warns about at startup.
func UsernameFromRequest(request *http.Request) string {
	if request == nil {
		return ""
	}
	return request.Header.Get(AuthenticatedUsernameHeader)
}
