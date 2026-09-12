package dav

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

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

type ifMatchKey struct{}

// withIfMatch carries a conditional header past the protocol library, which
// hands a backend the path of the thing to delete and nothing else.
func withIfMatch(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, ifMatchKey{}, value)
}

// ifMatchFrom is the If-Match of the request being served, if it had one.
func ifMatchFrom(ctx context.Context) string {
	found, _ := ctx.Value(ifMatchKey{}).(string)
	return found
}

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
	// request, so plain HTTP is refused rather than merely discouraged.
	//
	// What counts as not-in-the-clear is deliberately wider here than the
	// test used for the session cookie. That one reads X-Forwarded-Proto
	// only from an address the operator listed as a proxy, because a
	// forged header would otherwise let somebody mark a cookie Secure that
	// should not be. Here the header is taken from anybody, because the
	// only thing a forger gains is permission to send their own password
	// over their own plaintext connection -- they harm nobody but
	// themselves, and there is nothing to steal that they do not already
	// have. Being strict instead had a cost paid immediately: a server
	// behind a CDN that had never needed to list its proxies refused every
	// request with a 403 that said nothing about why.
	if !isOverTLS(request) {
		http.Error(response, "contacts are served over HTTPS only", http.StatusForbidden)
		return nil, false
	}

	username, password, ok := request.BasicAuth()
	if !ok || strings.TrimSpace(username) == "" || password == "" {
		self.askForCredentials(response)
		return nil, false
	}

	// The limiter is keyed by where the request came from, as it is
	// everywhere else this credential is presented, and it is asked only
	// when a sign-in has actually failed.
	//
	// Keying it by the username instead was wrong twice over. A caller
	// choosing the key can spend everybody's budget: naming a different
	// address each time buys an unlimited number of password hashes from
	// one machine, and filling the registry with invented names pushes out
	// the buckets that the submission listener and IMAP share, turning the
	// server-wide limit off for an hour. And counting every request rather
	// than every failure would have throttled the ordinary case, since one
	// phone synchronizing its contacts makes a request per card.
	from := api.RemoteAddress(request, configuration.Server.TrustedProxies)

	var signedIn *session
	if err := self.database.TransactionContext(request.Context(), func(tx db.Transaction) error {
		mailbox, appPassword, err := access.AuthenticateAppPasswordWithID(tx, username, password)
		if err != nil {
			return err
		}
		// AuthenticateAppPasswordWithID has already recorded that this app
		// password was used, which is what the app-password page shows; a
		// second write here would be one per request for nothing.
		signedIn = &session{userID: mailbox.UserID, mailbox: mailbox, appPasswordID: appPassword.ID}
		return nil
	}); err != nil {
		if errors.Is(err, access.ErrInvalidAppPassword) {
			// Every way of being wrong is one answer, and the function
			// above spends a password hash even when refusing, so that a
			// guess learns nothing from how long it took. A failure is
			// also what the limiter counts, so that guessing gets slower
			// while ordinary use never does.
			if self.limiter != nil && !self.limiter.Allow(from) {
				response.Header().Set("Retry-After", "60")
				http.Error(response, "too many attempts; wait a minute", http.StatusTooManyRequests)
				return nil, false
			}
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

// isOverTLS says whether the password in this request's header reached the
// server encrypted: over TLS here, over TLS to something in front that said
// so, or over no network at all.
func isOverTLS(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	if strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return isLoopback(request)
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
