package web

import (
	"net/http"

	"github.com/gorilla/mux"
)

// staticComponent serves whatever is not part of the API. It is registered
// last and matches everything, so the dashboard's own routing works: a reader
// who reloads the page on a deep link gets the application, not a 404.
type staticComponent struct {
	handler http.Handler
}

// NewStaticComponent serves a handler for every path the API did not claim.
func NewStaticComponent(handler http.Handler) Component {
	return &staticComponent{handler: handler}
}

func (self *staticComponent) AddRoutes(router *mux.Router) error {
	router.PathPrefix("/").Handler(self.handler)
	return nil
}
