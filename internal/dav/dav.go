// Package dav serves a person's address book to their phone and their
// desktop over CardDAV.
//
// CardDAV is a way of keeping address books in step over HTTP, defined in
// RFC 6352. It is WebDAV -- HTTP with a few extra methods -- with rules about
// what an address book looks like. The methods beyond ordinary HTTP that
// matter here are PROPFIND, which asks for properties of a URL and, with a
// "Depth: 1" header, of everything directly inside it; and REPORT, which runs
// a named query. The protocol itself is handled by go-webdav; what this
// package supplies is who the caller is, where things live, and the storage
// underneath.
//
// A client signs in with HTTP Basic authentication: one of a mailbox's
// addresses as the username, and one of that mailbox's app passwords as the
// password, exactly as a mail program signs in over IMAP. The account's own
// password is never accepted. The address book a client then sees is the one
// belonging to the account that owns that mailbox.
package dav

import (
	"net/http"
	"strings"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	"github.com/gorilla/mux"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("dav")

const (
	// Prefix is where this is mounted. It is given to the CardDAV handler
	// as well, and it must be: the library decides what kind of resource a
	// URL names by counting the path segments after its prefix, so a
	// handler that does not know where it is mounted reads every resource
	// as something one level deeper than it is.
	Prefix = "/dav"

	// contactsSegment is the fixed name of the address-book home set,
	// leaving room for a calendars segment beside it later.
	contactsSegment = "contacts"

	// The layout, which is not ours to choose. The library reads the kind
	// of a resource off how deep it is:
	//
	//     /dav/{userId}/                             the principal
	//     /dav/{userId}/contacts/                    the home set
	//     /dav/{userId}/contacts/{bookId}/           an address book
	//     /dav/{userId}/contacts/{bookId}/{id}.vcf   a contact
	//
	// so a person, their books and their cards are at one, two, three and
	// four segments and nowhere else.
	cardSuffix = ".vcf"
)

type component struct {
	database      db.Database
	configuration config.Store

	// limiter is the shared credential limiter -- the same buckets the
	// submission listener and IMAP count against -- or nil when an
	// operator has turned the limit off.
	limiter *ratelimit.Registry
}

// New builds the DAV component. It is a web.Component, registered in
// internal/cmd/server/run.go beside the API.
func New(database db.Database, configuration config.Store, limiter *ratelimit.Registry) (web.Component, error) {
	return &component{database: database, configuration: configuration, limiter: limiter}, nil
}

// AddRoutes mounts the whole subtree on one route and dispatches inside it.
//
// One route, deliberately. The router this joins is built with
// StrictSlash(true), which answers a request for a collection without its
// trailing slash with a redirect -- and a redirect is not harmless here: an
// HTTP client turns a 301 into a GET, so a PROPFIND that gets redirected
// arrives as a GET and is answered "method not allowed". Matching the prefix
// and routing in Go means nothing under /dav can ever be redirected.
func (self *component) AddRoutes(router *mux.Router) error {
	router.PathPrefix(Prefix).HandlerFunc(self.serve)
	// How a client finds any of this when somebody types only a mail
	// address: RFC 6764 says to look here first.
	router.Path("/.well-known/carddav").HandlerFunc(self.wellKnown)
	router.Path("/.well-known/caldav").HandlerFunc(self.wellKnown)
	return nil
}

// wellKnown sends a client from the address it guessed to where this
// actually lives. A permanent redirect, which is what the specification asks
// for and what clients cache.
func (self *component) wellKnown(response http.ResponseWriter, request *http.Request) {
	http.Redirect(response, request, Prefix+"/", http.StatusMovedPermanently)
}

// serve is every request under the mount.
func (self *component) serve(response http.ResponseWriter, request *http.Request) {
	signedIn, ok := self.authenticate(response, request)
	if !ok {
		return
	}

	// What the path names, by depth, after the mount.
	rest := strings.Trim(strings.TrimPrefix(request.URL.Path, Prefix), "/")
	var segments []string
	if rest != "" {
		segments = strings.Split(rest, "/")
	}

	// The mount itself: the one question a client asks before it knows
	// anything, which is who it is signed in as.
	if len(segments) == 0 {
		self.servePrincipal(response, request, signedIn)
		return
	}

	// Everything else belongs to exactly one account, and only that
	// account may reach it. Refused rather than hidden: that other
	// accounts exist is not a secret, and answering "not found" sends some
	// clients into a retry loop looking for a collection they were told
	// about.
	if segments[0] != signedIn.userID {
		http.Error(response, "that is not your address book", http.StatusForbidden)
		return
	}

	// The principal itself.
	if len(segments) == 1 {
		self.servePrincipal(response, request, signedIn)
		return
	}

	if segments[1] != contactsSegment {
		http.Error(response, "no such collection", http.StatusNotFound)
		return
	}

	handler := &carddav.Handler{Backend: &backend{component: self, signedIn: signedIn}, Prefix: Prefix}
	handler.ServeHTTP(response, request.WithContext(withSignedIn(request.Context(), signedIn)))
}

// servePrincipal answers for the person: who they are, and where their
// address books live. The CardDAV handler does not serve this -- it has its
// own helper in the library -- and discovery stops at the first step without
// it.
func (self *component) servePrincipal(response http.ResponseWriter, request *http.Request, signedIn *session) {
	webdav.ServePrincipal(response, request, &webdav.ServePrincipalOptions{
		CurrentUserPrincipalPath: principalPath(signedIn.userID),
		HomeSets: []webdav.BackendSuppliedHomeSet{
			carddav.NewAddressBookHomeSet(homeSetPath(signedIn.userID)),
		},
		Capabilities: []webdav.Capability{carddav.CapabilityAddressBook},
	})
}

func principalPath(userId string) string { return Prefix + "/" + userId + "/" }

func homeSetPath(userId string) string {
	return Prefix + "/" + userId + "/" + contactsSegment + "/"
}

func bookPath(userId, addressBookId string) string {
	return homeSetPath(userId) + addressBookId + "/"
}

func contactPath(userId, addressBookId, contactId string) string {
	return bookPath(userId, addressBookId) + contactId + cardSuffix
}
