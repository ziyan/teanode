// Package apisend implements the endpoint that sends a templated message.
//
// It is the one part of the API that is not for an operator: a credential
// authenticates with HTTP basic authentication, the same username and
// password it would use for SMTP submission, so an application that already
// has one can send without speaking SMTP.
package apisend

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apisend")

type send struct {
	database db.Database
	config   config.Store
	locator  geoip.Locator
	mailer   mailer.Mailer
	settings *api.Settings
}

// New builds the send component.
func New(database db.Database, configuration config.Store, locator geoip.Locator, sender mailer.Mailer, settings *api.Settings) (web.Component, error) {
	return &send{
		database: database,
		config:   configuration,
		locator:  locator,
		mailer:   sender,
		settings: settings,
	}, nil
}

func (self *send) AddRoutes(router *mux.Router) error {
	router.Path(api.PathSend).Methods(http.MethodPost).HandlerFunc(self.sendView)
	return nil
}
