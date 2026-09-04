// Package apigraph implements the GraphQL endpoint, which is the whole of the
// management API.
//
// Queries over mail, deliveries and reports read the database. Everything
// about domains, aliases, credentials, accounts and tokens reads and writes
// the configuration file through config.Store, so a change made here ends up
// in teanode.yaml and survives a restart. That is why the command line client
// goes through this endpoint too rather than editing the file: the running
// server is the only writer.
package apigraph

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/graphql-go/graphql"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/dns"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/graphapi"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apigraph")

type graph struct {
	database db.Database
	config   config.Store
	storage  storage.Storage
	locator  geoip.Locator
	verifier dns.Verifier
	mailer   mailer.Mailer
	settings *api.Settings

	// upgrade knows what has been released and can replace this binary with
	// it. Nil when the server was started without it, in which case the
	// dashboard is told there is nothing to say rather than shown an empty
	// card.
	upgrade upgrade.Manager
	schema  graphql.Schema

	// started is when this process built the API, which is close enough to
	// when it started to be what the dashboard shows as uptime.
	started time.Time

	// authenticator is used only by the session and passkey resolvers, which
	// are the only ones a browser calls before it is anybody.
	authenticator web.Authenticator

	// ceremonies parks half-finished WebAuthn challenges. In this process by
	// default; in Redis when one is configured, which is what makes passkeys
	// work behind a load balancer.
	ceremonies ceremony.Store
}

// New builds the GraphQL component, generating the schema by reflection over
// the Query, Mutation and Subscription interfaces.
func New(database db.Database, configuration config.Store, messages storage.Storage, locator geoip.Locator, verifier dns.Verifier, sender mailer.Mailer, upgrader upgrade.Manager, authenticator web.Authenticator, ceremonies ceremony.Store, settings *api.Settings) (web.Component, error) {
	self := &graph{
		database:      database,
		config:        configuration,
		storage:       messages,
		locator:       locator,
		verifier:      verifier,
		mailer:        sender,
		upgrade:       upgrader,
		authenticator: authenticator,
		ceremonies:    ceremonies,
		settings:      settings,
		started:       time.Now(),
	}

	graphApi := graphapi.New()
	var query Query = self
	var mutation Mutation = self
	if err := graphApi.Register(&query, &mutation, nil); err != nil {
		return nil, err
	}
	schema, err := graphApi.Build()
	if err != nil {
		return nil, err
	}
	self.schema = schema
	return self, nil
}

func (self *graph) AddRoutes(router *mux.Router) error {
	router.Path(api.PathGraphQL).Methods(http.MethodGet).HandlerFunc(self.webSocketView)
	router.Path(api.PathGraphQL).Methods(http.MethodPost).HandlerFunc(self.graphView)
	return nil
}
