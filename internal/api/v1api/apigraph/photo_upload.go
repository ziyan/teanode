package apigraph

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/ratelimit"
)

// photoRealm is what a client shows somebody when it asks for the password,
// the same as the address book and calendar servers give.
const photoRealm = `Basic realm="TeaNode", charset="UTF-8"`

// photoStore is the library's store, made on first use. Photos need a blob
// store that can stream, which the filesystem and an object store both can;
// a graph built without one answers that photos are unavailable.
func (self *graph) photoStore() (*photo.Store, error) {
	self.photoStoreOnce.Do(func() {
		if files, isStreaming := self.storage.(storage.StreamFiles); isStreaming {
			self.photoStoreValue = photo.New(self.database, files, self.config.Current)
		}
	})
	if self.photoStoreValue == nil {
		return nil, &models.ServiceUnavailableError{Service: models.ServicePhotos}
	}
	return self.photoStoreValue, nil
}

// photoPerson is whose library an HTTP request is for, or the answer to
// send because nobody's: the session the middleware found, or, where a phone
// uploads in the background, a device password in an HTTP Basic header that
// grants photos. The account's own password is never accepted here.
func (self *graph) photoPerson(response http.ResponseWriter, request *http.Request, isBasicAllowed bool) (*models.User, context.Context, bool) {
	var user *models.User
	var impersonator *models.User
	username := api.UsernameFromRequest(request)
	switch {
	case username != "" && username != config.LocalUsername:
		found, err := self.database.GetUserByUsername(username)
		if err != nil {
			http.Error(response, "failed to read the account", http.StatusInternalServerError)
			return nil, nil, false
		}
		if found == nil || found.Disabled() {
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return nil, nil, false
		}
		user = found
		if impersonator, err = self.impersonatorOf(request, user); err != nil {
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return nil, nil, false
		}
	case isBasicAllowed:
		var isAuthenticated bool
		user, isAuthenticated = self.photoBasicPerson(response, request)
		if !isAuthenticated {
			return nil, nil, false
		}
	default:
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, nil, false
	}
	if !self.isServiceEnabled(models.ServicePhotos) {
		http.Error(response, "photos are not available on this server", http.StatusForbidden)
		return nil, nil, false
	}
	var permissions *models.EffectivePermissions
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		permissions, err = transaction.EffectivePermissions(user.ID)
		return err
	}); err != nil {
		http.Error(response, "failed to read what the account may do", http.StatusInternalServerError)
		return nil, nil, false
	}
	if !permissions.Has(models.PermissionPhotosUse) {
		http.Error(response, "this account has no photo library", http.StatusForbidden)
		return nil, nil, false
	}
	ctx := db.ContextWithAuditPrincipal(request.Context(), self.auditPrincipal(request, user, impersonator))
	return user, ctx, true
}

// photoBasicPerson checks a device password. It is the check the address book
// and calendar servers make (access.AuthenticateSyncCredential), with the same
// limits around it: not over a connection that carries the password in the
// clear, and not from an address that has already failed too often.
func (self *graph) photoBasicPerson(response http.ResponseWriter, request *http.Request) (*models.User, bool) {
	username, password, hasBasic := request.BasicAuth()
	if !hasBasic || strings.TrimSpace(username) == "" || password == "" {
		response.Header().Set("WWW-Authenticate", photoRealm)
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, false
	}
	// A password in a header on every request is a password given away if the
	// connection is not encrypted. Loopback is not a network, and a proxy in
	// front may have ended the encryption, which it says in this header; a
	// forger of the header only harms themselves.
	if !isPasswordSafeOnTheWire(request) {
		http.Error(response, "photos are uploaded with a device password over HTTPS only", http.StatusForbidden)
		return nil, false
	}
	// Looked at before the password is checked and spent only on a failure,
	// so that guessing runs the budget down and is then turned away before
	// any hashing is done, and a phone uploading a thousand photos with the
	// right password never touches it.
	var from string
	if self.settings != nil && self.settings.AuthLimiter != nil {
		from = ratelimit.AddressKey(api.RemoteAddress(request, self.config.Current().Server.TrustedProxies))
		if self.settings.AuthLimiter.For(from).Available() <= 0 {
			response.Header().Set("Retry-After", "60")
			http.Error(response, "too many attempts; wait a minute", http.StatusTooManyRequests)
			return nil, false
		}
	}
	var credential *models.SyncCredential
	var user *models.User
	if err := self.database.TransactionContext(request.Context(), func(transaction db.Transaction) (err error) {
		credential, err = access.AuthenticateSyncCredential(transaction, username, password)
		if err != nil {
			return err
		}
		user, err = transaction.GetUser(credential.UserID)
		return err
	}); err != nil {
		if errors.Is(err, access.ErrInvalidAppPassword) {
			if self.settings != nil && self.settings.AuthLimiter != nil {
				self.settings.AuthLimiter.For(from).Take(1)
			}
			response.Header().Set("WWW-Authenticate", photoRealm)
			http.Error(response, "not signed in", http.StatusUnauthorized)
			return nil, false
		}
		log.Errorf("cannot check a photo upload sign-in: %s", err)
		http.Error(response, "cannot check that just now", http.StatusServiceUnavailable)
		return nil, false
	}
	if user == nil || user.Disabled() {
		http.Error(response, "not signed in", http.StatusUnauthorized)
		return nil, false
	}
	if !credential.Allows(models.ServicePhotos) {
		http.Error(response, "this device password does not grant photos", http.StatusForbidden)
		return nil, false
	}
	return user, true
}

// isPasswordSafeOnTheWire says whether a password in this request's header
// reached the server encrypted, or never left the machine.
func isPasswordSafeOnTheWire(request *http.Request) bool {
	if request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	host := request.RemoteAddr
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	}
	return api.IsLoopbackHost(strings.Trim(host, "[]"))
}

func (self *graph) photoUploadCollectionView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.photoPerson(response, request, true)
	if !isAuthorized {
		return
	}
	store, err := self.photoStore()
	if err != nil {
		http.Error(response, "photo storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	store.ServeCollection(response, request.WithContext(ctx), user.ID)
}

func (self *graph) photoUploadView(response http.ResponseWriter, request *http.Request) {
	user, ctx, isAuthorized := self.photoPerson(response, request, true)
	if !isAuthorized {
		return
	}
	store, err := self.photoStore()
	if err != nil {
		http.Error(response, "photo storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	store.ServeUpload(response, request.WithContext(ctx), user.ID, mux.Vars(request)["photoUploadId"])
}
