// Package apioauth answers the questions a program asks before it has a
// credential: who authorizes this server, and where do I go to be let in.
//
// The MCP endpoint in apigraph is a protected resource. It takes a bearer
// token and never sees a password. What was missing was the other half of
// that arrangement: a refusal that says where to go, and the two documents
// naming the authorization server and its endpoints. Without them a program
// asking for those documents fell through to the dashboard's catch-all route
// and was handed a web page, which it could only report as a server wanting
// an interactive login.
//
// internal/mcp/oauth.go is this package's mirror image, and was written
// first. It discovers, registers, and exchanges against somebody else's
// server; this package is discovered, registers, and exchanges against ours.
// Where the two must agree about a shape, that file is the reference, because
// it is the one already proven against servers we did not write.
package apioauth

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apioauth")

type oauth struct {
	config        config.Store
	database      db.Database
	authenticator web.Authenticator
}

// New builds the OAuth component.
//
// The database and the authenticator may be nil, which serves the discovery
// documents and nothing else. That is what the tests covering those documents
// use, and it keeps them from having to stand up a database to read two pieces
// of JSON.
func New(configuration config.Store, database db.Database, authenticator web.Authenticator) (web.Component, error) {
	return &oauth{config: configuration, database: database, authenticator: authenticator}, nil
}

func (self *oauth) AddRoutes(router *mux.Router) error {
	router.Path(api.PathOAuthProtectedResource).Methods(http.MethodGet).HandlerFunc(self.protectedResourceView)
	router.Path(api.PathOAuthProtectedResourceMCP).Methods(http.MethodGet).HandlerFunc(self.protectedResourceView)
	router.Path(api.PathOAuthAuthorizationServer).Methods(http.MethodGet).HandlerFunc(self.authorizationServerView)
	if self.database == nil || self.authenticator == nil {
		return nil
	}
	router.Path(api.PathOAuthRegister).Methods(http.MethodPost).HandlerFunc(self.registerView)
	router.Path(api.PathOAuthToken).Methods(http.MethodPost).HandlerFunc(self.tokenView)
	router.Path(api.PathOAuthRevoke).Methods(http.MethodPost).HandlerFunc(self.revokeView)
	// PathOAuthAuthorize is deliberately not here. It is where a person is
	// sent to approve, so it is a page rather than an endpoint, and it is
	// drawn by the dashboard: that way approving reuses the sign-in the
	// dashboard already has, with passkeys and single sign-on, rather than
	// this package growing a second place that accepts a password. Not
	// claiming the route is what lets it fall through to the dashboard, the
	// same way /cli does.
	return nil
}
