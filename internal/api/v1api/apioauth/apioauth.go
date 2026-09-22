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
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apioauth")

type oauth struct {
	config config.Store
}

// New builds the OAuth component.
func New(configuration config.Store) (web.Component, error) {
	return &oauth{config: configuration}, nil
}

func (self *oauth) AddRoutes(router *mux.Router) error {
	router.Path(api.PathOAuthProtectedResource).Methods(http.MethodGet).HandlerFunc(self.protectedResourceView)
	router.Path(api.PathOAuthProtectedResourceMCP).Methods(http.MethodGet).HandlerFunc(self.protectedResourceView)
	router.Path(api.PathOAuthAuthorizationServer).Methods(http.MethodGet).HandlerFunc(self.authorizationServerView)
	return nil
}
