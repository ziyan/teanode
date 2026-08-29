// Package web provides the HTTP server and routing infrastructure.
package web

import (
	"github.com/gorilla/mux"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("web")

type Settings struct {
}

type Component interface {
	AddRoutes(*mux.Router) error
}
