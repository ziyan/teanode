package web

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/db"
)

type Server interface {
	http.Handler
}

type server struct {
	database db.Database
	settings *Settings
	router   *mux.Router
}

func NewServer(database db.Database, settings *Settings, components []Component) (Server, error) {
	router := mux.NewRouter().StrictSlash(true)
	for _, component := range components {
		if err := component.AddRoutes(router); err != nil {
			return nil, err
		}
	}
	return &server{
		database: database,
		settings: settings,
		router:   router,
	}, nil
}

func (self *server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	self.router.ServeHTTP(response, request.WithContext(contextWithServer(request.Context(), self)))
}
