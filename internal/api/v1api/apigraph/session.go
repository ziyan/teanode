package apigraph

import (
	"context"
	"errors"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/web"
)

// Logging in lives here rather than behind REST endpoints of its own, so that
// there is one API. A browser needs a cookie rather than a bearer token, and a
// cookie is a response header — which is the only reason these resolvers reach
// for the response writer, and the only place in the schema that does.
//
// These are also the only operations that do not require an operator: you
// cannot be one before logging in, and a server with no account has nobody who
// could be. Every other resolver authorises itself, which
// TestEveryOperationAuthorises enforces.

type SessionQuery interface {
	// Get who this request is authenticated as, and whether this server has
	// been claimed yet
	GetSession(ctx context.Context) (*SessionState, error)

	// List this account's signed-in browsers
	ListSessions(ctx context.Context, arguments ListSessionsArguments) ([]*Session, error)
}

type SessionMutation interface {
	// Exchange a username and password for a session cookie
	Login(ctx context.Context, arguments LoginArguments) (*SessionState, error)

	// Clear the session cookie
	Logout(ctx context.Context) (*SessionState, error)

	// Claim a server that has no account yet, creating the first one
	CreateFirstAccount(ctx context.Context, arguments CreateFirstAccountArguments) (*SessionState, error)

	// Change the password of the account this request is authenticated as
	ChangePassword(ctx context.Context, arguments ChangePasswordArguments) (*SessionState, error)

	// Sign every browser out, everywhere, by replacing the key that signs
	// session cookies. This one included.
	RevokeAllSessions(ctx context.Context) (*SessionState, error)

	// End one signed-in browser
	RevokeSession(ctx context.Context, arguments RevokeSessionArguments) error
}

// SessionState is who the caller is, and what the dashboard should show them.
type SessionState struct {
	// Whether this request is authenticated
	Authenticated bool `json:"authenticated"`

	// Whether this server has an account at all. False means it has never
	// been claimed, and the dashboard offers to create the first one instead
	// of asking for a password.
	AuthenticationRequired bool `json:"authenticationRequired"`

	// The account this request is authenticated as, empty when there is none
	Username string `json:"username"`

	// What to call that person, when they have said. Empty otherwise; the
	// dashboard falls back to the username, which is what it had before there
	// was anywhere to say a name.
	Name string `json:"name,omitempty"`

	// Whether this server offers passkeys, so the sign-in form shows the
	// passkey button only where pressing it could work. Told to an anonymous
	// caller on purpose: it is the one thing about the configuration the form
	// has to know, and pressing the button would have revealed it anyway.
	PasskeysEnabled bool `json:"passkeysEnabled"`

	// SSOProviders are the identity providers to offer buttons for, by id
	// and name; empty when there are none.
	SSOProviders []*SSOProviderInfo `json:"ssoProviders"`

	// ID of the account, empty for the console and for nobody
	UserID string `json:"userId,omitempty"`

	// What the caller may do, resolved from their groups. The web UI hides
	// what they cannot; every mutation is checked again on the server.
	Permissions *models.EffectivePermissions `json:"permissions,omitempty"`

	// Whether the caller holds any permission that opens the management
	// side of the web UI
	Manages bool `json:"manages"`
}

func (self *graph) sessionState(ctx context.Context) *SessionState {
	state := &SessionState{
		AuthenticationRequired: self.authenticator.Required(),
		PasskeysEnabled:        self.config.Current().Passkey.Enabled,
		SSOProviders:           self.ssoProviders(),
	}
	if request := api.ContextRequest(ctx); request != nil {
		state.Username, state.Authenticated = self.authenticator.Authenticate(request)
	}
	state.Name = self.displayName(state.Username)
	if principal := api.ContextPrincipal(ctx); principal != nil {
		state.UserID = principal.UserID()
		state.Permissions = principal.Permissions
		state.Manages = principal.Permissions.Manages()
	}
	return state
}

// signedInAs is the state every resolver that has just established who
// somebody is returns.
//
// A constructor rather than a struct literal in six places, because that is
// how a field added later ends up filled in on five of them. Adding Name was
// exactly that change.
func (self *graph) signedInAs(ctx context.Context, username string) *SessionState {
	state := &SessionState{
		Authenticated:          true,
		AuthenticationRequired: true,
		Username:               username,
		Name:                   self.displayName(username),
		PasskeysEnabled:        self.config.Current().Passkey.Enabled,
		SSOProviders:           self.ssoProviders(),
	}
	// What they may do, resolved now: the request that signed them in
	// started as nobody, so the principal on the context is empty.
	if tx := api.ContextTransaction(ctx); tx != nil {
		user, err := tx.GetUserByUsername(username)
		if err == nil && user != nil {
			if principal, err := self.resolvePrincipal(tx, username, user); err == nil && principal != nil {
				state.UserID = principal.UserID()
				state.Permissions = principal.Permissions
				state.Manages = principal.Permissions.Manages()
			}
		}
	}
	return state
}

// displayName is what to call somebody, when they have said. Read here rather
// than left to a second request: the shell asks who it is signed in as before
// it draws anything, and a name arriving later means the rail renders the
// username and then changes its mind.
func (self *graph) displayName(username string) string {
	if username == "" || username == config.LocalUsername {
		return ""
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil || user == nil {
		return ""
	}
	return user.Name
}

func (self *graph) GetSession(ctx context.Context) (*SessionState, error) {
	return self.sessionState(ctx), nil
}

type LoginArguments struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (self *graph) Login(ctx context.Context, arguments LoginArguments) (*SessionState, error) {
	request, response := api.ContextRequest(ctx), api.ContextResponse(ctx)
	if request == nil || response == nil {
		return nil, api.ErrInvalidArguments
	}

	if err := self.authenticator.Login(response, request, arguments.Username, arguments.Password); err != nil {
		if errors.Is(err, web.ErrTooManyAttempts) {
			// Said plainly. Somebody with the right password deserves to know
			// why it is being refused, and an attacker learns only that the
			// limit they have just hit exists.
			return nil, api.ErrTooManyRequests
		}
		if errors.Is(err, web.ErrInvalidCredentials) {
			// Deliberately the same answer for an unknown username and a wrong
			// password, so the reply cannot be used to discover which accounts
			// exist.
			return nil, api.ErrNotLoggedIn
		}
		log.Errorf("failed to log in: %s", err)
		return nil, err
	}

	return self.signedInAs(ctx, arguments.Username), nil
}

func (self *graph) Logout(ctx context.Context) (*SessionState, error) {
	request, response := api.ContextRequest(ctx), api.ContextResponse(ctx)
	if request == nil || response == nil {
		return nil, api.ErrInvalidArguments
	}
	self.authenticator.Logout(response, request)
	return &SessionState{AuthenticationRequired: self.authenticator.Required()}, nil
}

type CreateFirstAccountArguments struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateFirstAccount claims a server that has nobody.
//
// It works only while no account exists. That is the whole security model of a
// first run: whoever reaches a freshly installed server first claims it, in
// exactly the same way that whoever can edit teanode.yaml owns it anyway. Once
// an account exists this refuses, so it cannot add a second one — "teanode
// user add" is for that.
func (self *graph) CreateFirstAccount(ctx context.Context, arguments CreateFirstAccountArguments) (*SessionState, error) {
	if self.authenticator.Required() {
		return nil, api.ErrAlreadyExists
	}

	// Recorded as coming from where it came from: the first arrival is
	// nobody yet, and the address is what the audit row can say.
	claim := db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorSystem, SourceIP: remoteAddress(api.ContextRequest(ctx))})
	if err := self.authenticator.CreateFirstUser(claim, arguments.Username, arguments.Password); err != nil {
		if errors.Is(err, web.ErrInvalidAccount) || errors.Is(err, web.ErrAccountExists) {
			return nil, err
		}
		log.Errorf("failed to create the first account: %s", err)
		return nil, err
	}

	// Log them straight in, so claiming a server is one step rather than two.
	request, response := api.ContextRequest(ctx), api.ContextResponse(ctx)
	if request != nil && response != nil {
		if err := self.authenticator.Login(response, request, arguments.Username, arguments.Password); err != nil {
			log.Errorf("created the account but could not log in: %s", err)
		}
	}

	return self.signedInAs(ctx, arguments.Username), nil
}

type ChangePasswordArguments struct {
	// The password in use now, which is required even though the caller
	// already holds a session: a session can be an unattended browser
	CurrentPassword string `json:"currentPassword"`

	NewPassword string `json:"newPassword"`
}

func (self *graph) ChangePassword(ctx context.Context, arguments ChangePasswordArguments) (*SessionState, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	username := api.ContextAuthenticatedUsername(ctx)
	if username == "" {
		return nil, api.ErrNotLoggedIn
	}

	if err := self.authenticator.ChangePassword(username, arguments.CurrentPassword, arguments.NewPassword); err != nil {
		if errors.Is(err, web.ErrInvalidCredentials) || errors.Is(err, web.ErrInvalidAccount) {
			return nil, err
		}
		log.Errorf("failed to change the password: %s", err)
		return nil, err
	}

	// The session stays valid: its signature covers the username and expiry,
	// not the password, and forcing a re-login after a deliberate change is
	// friction with no security benefit.
	return self.signedInAs(ctx, username), nil
}

// Session is one signed-in browser, as the list shows it.
type Session struct {
	// ID of the Session
	ID string `json:"id"`

	// Whether this is the session reading the list. It gets no revoke button,
	// because signing out is what ends your own.
	Current bool `json:"current"`

	// When somebody logged in
	Created time.Time `json:"created"`

	// When it stops working
	Expires *time.Time `json:"expires,omitempty"`

	// When it was last used. Recorded at most once a minute, so it is
	// accurate to about that.
	LastUsed *time.Time `json:"lastUsed,omitempty"`

	// Where it was last used from, for somebody deciding whether a session in
	// their list is theirs
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`

	// When it was ended, or null while it still works
	Revoked *time.Time `json:"revoked,omitempty"`
}

type ListSessionsArguments struct {
	// Include sessions that have been ended, so that a revocation is visible
	// for a while rather than the row disappearing.
	IncludeRevoked *bool `json:"includeRevoked" graphapi:"nullable"`

	// Whose sessions. Only the console may name somebody else.
	Username *string `json:"username" graphapi:"nullable"`
}

// ListSessions returns this account's signed-in browsers, newest first.
func (self *graph) ListSessions(ctx context.Context, arguments ListSessionsArguments) ([]*Session, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}

	username, err := self.owner(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}
	stored, err := self.authenticator.ListSessions(username, arguments.IncludeRevoked != nil && *arguments.IncludeRevoked)
	if err != nil {
		return nil, err
	}

	// Marked here rather than left to the browser to work out: the identifier
	// is in an HttpOnly cookie, so the page cannot read its own.
	var current string
	if request := api.ContextRequest(ctx); request != nil {
		current = self.authenticator.CurrentSessionID(request)
	}

	sessions := make([]*Session, 0, len(stored))
	for _, session := range stored {
		sessions = append(sessions, &Session{
			ID:        session.ID,
			Current:   session.ID == current && current != "",
			Created:   session.CreatedAt,
			Expires:   optionalTime(session.ExpiresAt),
			LastUsed:  optionalTime(session.UsedAt),
			IP:        session.IP,
			UserAgent: session.UserAgent,
			Revoked:   optionalTime(session.RevokedAt),
		})
	}
	return sessions, nil
}

type RevokeSessionArguments struct {
	// ID of the Session to end
	SessionID string `json:"sessionId"`
}

// RevokeSession ends one signed-in browser.
func (self *graph) RevokeSession(ctx context.Context, arguments RevokeSessionArguments) error {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return err
	}

	username, err := self.owner(ctx, nil)
	if err != nil {
		return err
	}
	if err := self.authenticator.RevokeSession(username, arguments.SessionID); err != nil {
		return err
	}
	log.Noticef("%s revoked session %s", username, arguments.SessionID)
	return nil
}

// RevokeAllSessions ends every session this account has, including the one
// asking.
//
// It used to replace the key every cookie was signed with, because the server
// kept nothing and invalidating the signature was the only way to end a
// session — which ended everybody's, on every account. Now each is a row, so
// this ends exactly this person's.
//
// The caller is signed out too. Anything else would mean keeping one browser
// alive for whoever asked to sign them all out, which is not what they asked.
func (self *graph) RevokeAllSessions(ctx context.Context) (*SessionState, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}

	username, err := self.owner(ctx, nil)
	if err != nil {
		return nil, err
	}
	ended, err := self.authenticator.RevokeSessions(username, "")
	if err != nil {
		return nil, err
	}

	log.Noticef("%s ended %d of their sessions, including this one", username, ended)
	return &SessionState{AuthenticationRequired: self.authenticator.Required()}, nil
}

// SSOProviderInfo is what the sign-in page knows about a provider: enough
// for a button, and no secret.
type SSOProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (self *graph) ssoProviders() []*SSOProviderInfo {
	providers := []*SSOProviderInfo{}
	for _, provider := range self.config.Current().SSO.Providers {
		providers = append(providers, &SSOProviderInfo{ID: provider.ID, Name: provider.Name})
	}
	return providers
}
