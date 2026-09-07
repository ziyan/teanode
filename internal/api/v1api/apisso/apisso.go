// Package apisso is the two HTTP paths a single sign-on passes through: the
// one that sends the browser to the identity provider, and the one it comes
// back to. Everything else about signing in is the authenticator's.
package apisso

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/sso"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apisso")

const (
	pathStart    = "/api/v1/sso/{provider}/start"
	pathCallback = "/api/v1/sso/{provider}/callback"

	// cookieName holds the signed state between the two paths.
	cookieName = "teanode_sso"
)

type component struct {
	database      db.Database
	configuration config.Store
	authenticator web.Authenticator
	service       *sso.Service
}

func New(database db.Database, configuration config.Store, authenticator web.Authenticator, settings *api.Settings) (web.Component, error) {
	return &component{
		database:      database,
		configuration: configuration,
		authenticator: authenticator,
		service:       sso.New(settings.Secret),
	}, nil
}

func (self *component) AddRoutes(router *mux.Router) error {
	router.Path(pathStart).Methods(http.MethodGet).HandlerFunc(self.start)
	router.Path(pathCallback).Methods(http.MethodGet).HandlerFunc(self.callback)
	return nil
}

// provider is the configured provider a path names, or nil.
func (self *component) provider(request *http.Request) *sso.Provider {
	id := mux.Vars(request)["provider"]
	for _, candidate := range self.configuration.Current().SSO.Providers {
		if candidate.ID == id {
			return &sso.Provider{
				ID:           candidate.ID,
				Name:         candidate.Name,
				Issuer:       candidate.Issuer,
				ClientID:     candidate.ClientID,
				ClientSecret: candidate.ClientSecret,
				GroupsClaim:  candidate.GroupsClaim,
				CreateUsers:  candidate.CreateUsers,
			}
		}
	}
	return nil
}

// redirectURL is where the provider sends the browser back: this server, as
// the browser reached it.
func redirectURL(request *http.Request, providerId string) string {
	scheme := "https"
	if request.TLS == nil && !strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/api/v1/sso/%s/callback", scheme, request.Host, url.PathEscape(providerId))
}

func (self *component) start(response http.ResponseWriter, request *http.Request) {
	provider := self.provider(request)
	if provider == nil {
		http.Error(response, "no such provider", http.StatusNotFound)
		return
	}
	// Where to land afterwards: a path on this site and nothing else, so
	// the sign-in cannot be used to send somebody elsewhere.
	returnTo := safeReturn(request.URL.Query().Get("return"))
	authURL, cookie, err := self.service.Begin(request.Context(), *provider, redirectURL(request, provider.ID), returnTo)
	if err != nil {
		log.Errorf("cannot start single sign-on with %q: %s", provider.ID, err)
		http.Error(response, "the identity provider could not be reached", http.StatusBadGateway)
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name:     cookieName,
		Value:    cookie,
		Path:     "/api/v1/sso/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(response, request, authURL, http.StatusFound)
}

func (self *component) callback(response http.ResponseWriter, request *http.Request) {
	provider := self.provider(request)
	if provider == nil {
		http.Error(response, "no such provider", http.StatusNotFound)
		return
	}
	query := request.URL.Query()
	if problem := query.Get("error"); problem != "" {
		log.Noticef("single sign-on with %q refused by the provider: %s %s", provider.ID, problem, query.Get("error_description"))
		self.fail(response, request, "refused")
		return
	}
	cookie, err := request.Cookie(cookieName)
	if err != nil {
		self.fail(response, request, "state")
		return
	}
	// The cookie is spent whatever happens next.
	http.SetCookie(response, &http.Cookie{Name: cookieName, Value: "", Path: "/api/v1/sso/", MaxAge: -1, HttpOnly: true})

	claims, returnTo, err := self.service.Complete(request.Context(), *provider, redirectURL(request, provider.ID), cookie.Value, query.Get("state"), query.Get("code"))
	if err != nil {
		log.Warningf("single sign-on with %q failed: %s", provider.ID, err)
		if errors.Is(err, sso.ErrBadState) {
			self.fail(response, request, "state")
		} else {
			self.fail(response, request, "verify")
		}
		return
	}

	var username string
	var created bool
	err = self.database.TransactionContext(db.ContextWithAuditPrincipal(request.Context(), db.AuditPrincipal{ActorKind: models.AuditActorSystem, SourceIP: remoteAddress(request)}), func(tx db.Transaction) error {
		user, made, err := access.SignInWithIdentity(tx, provider.ID, &access.IdentityClaims{
			Subject:  claims.Subject,
			Email:    claims.Email,
			Name:     claims.Name,
			Username: claims.Username,
			Groups:   claims.Groups,
		}, provider.CreateUsers)
		if err != nil {
			return err
		}
		username, created = user.Username, made
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, access.ErrNoAccount):
			log.Noticef("single sign-on with %q by %q: no account here", provider.ID, claims.Subject)
			self.fail(response, request, "noaccount")
		case errors.Is(err, access.ErrAccountDisabled):
			self.fail(response, request, "disabled")
		default:
			log.Errorf("single sign-on with %q failed: %s", provider.ID, err)
			self.fail(response, request, "failed")
		}
		return
	}
	if err := self.authenticator.StartSession(response, request, username); err != nil {
		log.Errorf("single sign-on with %q signed %q in but no session could be started: %s", provider.ID, username, err)
		self.fail(response, request, "failed")
		return
	}
	if created {
		log.Noticef("single sign-on with %q created account %q", provider.ID, username)
	} else {
		log.Noticef("single sign-on with %q signed in %q", provider.ID, username)
	}
	http.Redirect(response, request, returnTo, http.StatusFound)
}

// fail sends the browser back to the sign-in page with a code it turns into
// a sentence of its own: the address bar is not a place for one anybody
// could write.
func (self *component) fail(response http.ResponseWriter, request *http.Request, code string) {
	http.Redirect(response, request, "/?sso="+url.QueryEscape(code), http.StatusFound)
}

// remoteAddress is where the request came from, as the audit trail records
// it: the proxy's idea when there is one, else the connection's.
func remoteAddress(request *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// safeReturn is a path on this site, or "/": no scheme, no host, and no
// backslash, which browsers read as a slash and would carry off-site.
func safeReturn(value string) string {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n") {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/") {
		return "/"
	}
	return value
}
