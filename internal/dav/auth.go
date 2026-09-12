package dav

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// realm is what a client shows somebody when it asks for the password.
const realm = `Basic realm="TeaNode", charset="UTF-8"`

// session is who is on the other end of a request: the mailbox they signed in
// as, and the account that owns it, whose address book they will see.
type session struct {
	userID        string
	mailbox       *models.Mailbox
	appPasswordID string
}

type signedInKey struct{}

func withSignedIn(ctx context.Context, signedIn *session) context.Context {
	return context.WithValue(ctx, signedInKey{}, signedIn)
}

// signedInFrom is who the request is from. The library's backend methods take
// only a context, so this is how the backend learns whose address book it is
// looking at.
func signedInFrom(ctx context.Context) *session {
	found, _ := ctx.Value(signedInKey{}).(*session)
	return found
}

// authenticate establishes who is calling, or answers them and returns false.
//
// HTTP Basic, because that is the only thing a contacts application can do:
// it cannot follow a browser sign-in, hold a session cookie or use a passkey.
// The username is one of a mailbox's addresses and the password one of that
// mailbox's app passwords -- the same credential, checked by the same
// function, as a mail program signing in over IMAP. The account's own
// password is never accepted here.
func (self *component) authenticate(response http.ResponseWriter, request *http.Request) (*session, bool) {
	configuration := self.configuration.Current()

	// A credential sent in the clear is a credential given away, and Basic
	// authentication puts the password in a header on every single
	// request. So this is refused rather than merely discouraged -- unless
	// the request never left the machine, which is how a development
	// server with no certificate is tried locally.
	if !api.IsSecure(request, configuration.Server.TrustedProxies) && !isLoopback(request) {
		http.Error(response, "contacts are served over HTTPS only", http.StatusForbidden)
		return nil, false
	}

	username, password, ok := request.BasicAuth()
	if !ok || strings.TrimSpace(username) == "" || password == "" {
		self.askForCredentials(response)
		return nil, false
	}

	// The same buckets every other way of presenting a credential counts
	// against, so that a client retrying in a loop cannot be used to grind
	// app passwords faster than a mail program could.
	if self.limiter != nil && !self.limiter.Allow(strings.ToLower(strings.TrimSpace(username))) {
		response.Header().Set("Retry-After", "60")
		http.Error(response, "too many attempts; wait a minute", http.StatusTooManyRequests)
		return nil, false
	}

	var signedIn *session
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		mailbox, appPassword, err := access.AuthenticateAppPasswordWithID(tx, username, password)
		if err != nil {
			return err
		}
		signedIn = &session{userID: mailbox.UserID, mailbox: mailbox, appPasswordID: appPassword.ID}
		// So that the app-password page can say when a device last used
		// it, which is how somebody decides which one to revoke.
		return tx.TouchAppPassword(appPassword.ID, time.Now())
	}); err != nil {
		if errors.Is(err, access.ErrInvalidAppPassword) {
			// Every way of being wrong is one answer, and the function
			// above spends a password hash even when refusing, so that a
			// guess learns nothing from how long it took.
			self.askForCredentials(response)
			return nil, false
		}
		log.Errorf("cannot check a DAV sign-in: %s", err)
		http.Error(response, "cannot check that just now", http.StatusServiceUnavailable)
		return nil, false
	}

	// Reading and writing an address book is something a member does with
	// their own account. An operator who takes the permission away from a
	// role takes the phones with it.
	allowed := false
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		permissions, err := tx.EffectivePermissions(signedIn.userID)
		if err != nil {
			return err
		}
		allowed = permissions.Has(models.PermissionContactsUse)
		return nil
	}); err != nil {
		log.Errorf("cannot read what a DAV caller may do: %s", err)
		http.Error(response, "cannot check that just now", http.StatusServiceUnavailable)
		return nil, false
	}
	if !allowed {
		http.Error(response, "this account does not keep an address book here", http.StatusForbidden)
		return nil, false
	}
	return signedIn, true
}

func (self *component) askForCredentials(response http.ResponseWriter) {
	response.Header().Set("WWW-Authenticate", realm)
	http.Error(response, "sign in with a mail address and an app password", http.StatusUnauthorized)
}

// isLoopback says whether the request came from this machine. A password in
// a header that never reaches a network is not a password on the wire, and a
// development server has no certificate to offer.
func isLoopback(request *http.Request) bool {
	host := request.RemoteAddr
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	}
	address := net.ParseIP(strings.Trim(host, "[]"))
	return address != nil && address.IsLoopback()
}
