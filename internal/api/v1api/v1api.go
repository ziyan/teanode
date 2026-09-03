// Package v1api serves version 1 of the HTTP API, mounted at /api/v1.
//
// It is a composition rather than an implementation: apigraph provides the
// GraphQL endpoint that is the whole management API — logging in included —
// apisend the endpoint an application uses to send a templated message, and
// apimail the one that hands back a stored message as a file.
// Keeping them apart means each has only the dependencies it uses, and a
// version 2 can reuse the parts it does not intend to change.
//
// There is one management API, not a GraphQL one with REST endpoints beside it
// for the awkward parts. Logging in was the awkward part: a browser holds a
// cookie rather than a bearer token, and a cookie is a response header. That
// is a reason to give a resolver the response writer, not a reason to keep a
// second protocol.
package v1api

import (
	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/api/v1api/apimail"
	"github.com/ziyan/teanode/internal/api/v1api/apimedia"
	"github.com/ziyan/teanode/internal/api/v1api/apisend"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/dns"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/web"
)

type v1 struct {
	components []web.Component
}

// New builds every component of version 1.
func New(
	database db.Database,
	configuration config.Store,
	messages storage.Storage,
	locator geoip.Locator,
	verifier dns.Verifier,
	sender mailer.Mailer,
	upgrader upgrade.Manager,
	authenticator web.Authenticator,
	ceremonies ceremony.Store,
	settings *api.Settings,
) (web.Component, error) {
	graph, err := apigraph.New(database, configuration, messages, locator, verifier, sender, upgrader, authenticator, ceremonies, settings)
	if err != nil {
		return nil, err
	}
	send, err := apisend.New(database, configuration, locator, sender, settings)
	if err != nil {
		return nil, err
	}
	raw, err := apimail.New(database, configuration, messages)
	if err != nil {
		return nil, err
	}
	pictures, err := apimedia.New(database, configuration, messages)
	if err != nil {
		return nil, err
	}
	return &v1{components: []web.Component{graph, send, raw, pictures}}, nil
}

func (self *v1) AddRoutes(router *mux.Router) error {
	for _, component := range self.components {
		if err := component.AddRoutes(router); err != nil {
			return err
		}
	}
	return nil
}
