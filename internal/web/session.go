package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/util/security"
)

// SessionCookieName is the cookie holding a signed session.
const SessionCookieName = "teanode_session"

var (
	// ErrInvalidCredentials is returned by Login for both an unknown username
	// and a wrong password, so that the reply cannot be used to discover
	// which usernames exist.
	ErrInvalidCredentials = errors.New("web: incorrect username or password")

	// ErrTooManyAttempts is returned when an address has used up its login
	// attempts. Distinct from ErrInvalidCredentials so that the caller can
	// answer differently: telling somebody they are being rate limited is not
	// a disclosure, and leaving them to guess why their correct password is
	// being refused is unkind.
	ErrTooManyAttempts = errors.New("web: too many login attempts, try again later")

	// ErrInvalidAccount is returned when a proposed account would not work.
	// Its message reaches the browser, so it is written for a person rather
	// than carrying the usual package prefix.
	ErrInvalidAccount = errors.New("web: that account will not work")

	// ErrAccountExists is returned when the first-run setup is attempted on a
	// server that already has an account.
	ErrAccountExists = errors.New("web: this server already has an account; sign in instead")

	ErrSessionExpired = errors.New("web: session expired")
	ErrSessionInvalid = errors.New("web: session is not valid")
)

// Authenticator decides who may use the dashboard and the API.
type Authenticator interface {
	// Authenticate returns the operator this request belongs to, and whether
	// authentication is satisfied at all. When no dashboard users are
	// configured every request is allowed and the username is empty.
	Authenticate(request *http.Request) (username string, ok bool)

	// Login verifies a password, stores a session and writes its cookie.
	Login(response http.ResponseWriter, request *http.Request, username, password string) error

	// StartSession stores a session and writes its cookie for somebody whose
	// identity has already been established some other way — by an
	// authenticator answering a WebAuthn challenge, which this package does
	// not verify and should not have to know about.
	//
	// It does no rate limiting, because there is nothing here to guess: the
	// caller has already checked a signature against a stored public key.
	StartSession(response http.ResponseWriter, request *http.Request, username string) error

	// Logout ends the session this request carries and clears the cookie.
	Logout(response http.ResponseWriter, request *http.Request)

	// AllowLoginAttempt counts one attempt to sign in from wherever the
	// request came from, and says whether it may go ahead. Login counts
	// its own; this is for the other ways in — a passkey ceremony, say —
	// so that they are limited the same way.
	AllowLoginAttempt(request *http.Request) bool

	// CurrentSessionID is the session this request is using, or empty when it
	// is authenticated some other way. The dashboard needs it to mark one row
	// in the list as the one you are reading it from.
	CurrentSessionID(request *http.Request) string

	// ListSessions returns an operator's sessions, newest first.
	ListSessions(username string, includeRevoked bool) ([]*models.Session, error)

	// RevokeSession ends one session belonging to an operator.
	RevokeSession(username, sessionId string) error

	// RevokeSessions ends every session an operator has, optionally keeping
	// the one making the request, and returns how many it ended.
	RevokeSessions(username string, except string) (int64, error)

	// IssueToken mints an API token for an operator, returning the stored
	// row and the token string, which is the only time it can be read.
	IssueToken(username, name string, lifetime time.Duration) (*models.Token, string, error)

	// IssueAuthorizedToken mints the token a program collects after somebody
	// approved it, returning the row, the token and the refresh secret.
	IssueAuthorizedToken(userId, name, clientId, resource string, lifetime time.Duration) (*models.Token, string, string, error)

	// RedeemRefresh checks a refresh secret and returns the token it renews,
	// without minting the replacement.
	RedeemRefresh(value string) (*models.Token, error)

	// RevokeTokenByID retires a token without knowing whose it is, for a
	// refresh replacing the one it renewed, and says whether this call was
	// the one that retired it.
	RevokeTokenByID(tokenId string) (bool, error)

	// UserByID is the account a token or an approval belongs to.
	UserByID(userId string) *models.User

	// UserByName is the account somebody is signed in as.
	UserByName(username string) *models.User

	// TokenIDOf is the identifier inside an access token, when the value is
	// one this server minted. It verifies the signature, so an identifier
	// only comes back for a credential that was issued here.
	TokenIDOf(value string) (string, bool)

	// ListTokens returns an operator's tokens, newest first.
	ListTokens(username string, includeRevoked bool) ([]*models.Token, error)

	// RevokeToken ends one token belonging to an operator.
	RevokeToken(username, tokenId string) error

	// UpdateToken renames one of an operator's tokens or gives it a new
	// lifetime, counted from now; a lifetime of zero never expires. Nil
	// leaves either as it is.
	UpdateToken(username, tokenId string, name *string, lifetime *time.Duration) (*models.Token, error)

	// Scavenge removes sessions and tokens that are no longer worth keeping.
	Scavenge() error

	// Required reports whether any user is configured. When false the
	// dashboard is open to whoever can reach it, and the interface offers to
	// create the first account instead of asking for one.
	Required() bool

	// CreateFirstUser claims a server that has no account yet.
	CreateFirstUser(ctx context.Context, username, password string) error

	// ChangePassword replaces an account's password, after checking the
	// current one.
	ChangePassword(username, current, replacement string) error
}

// How many login attempts one address may make. A person retries a handful of
// times; a program does not stop.
const (
	loginAttemptsPerMinute   = 10
	loginAttemptBurst        = 20
	loginAddressesRemembered = 4096
)

// CredentialStore is where sessions and tokens live.
//
// Narrower than db.Database on purpose: this package needs fourteen methods
// out of it, and saying so means a test can stand up the authenticator without
// a PostgreSQL. db.Database satisfies it.
type CredentialStore interface {
	db.SessionOperation
	db.TokenOperation
	db.UserLookup

	// ScavengeOAuth sweeps approvals nobody collected and registrations
	// nobody approved, alongside the sessions and tokens swept here.
	ScavengeOAuth(now time.Time) (int64, error)

	// TransactionContext is for the two writes to the user table made here:
	// claiming a fresh server, and a person changing their own password.
	TransactionContext(ctx context.Context, function func(db.Transaction) error) error
}

type authenticator struct {
	config   config.Store
	database CredentialStore

	// loginLimiter bounds how often one address may try to log in. The
	// password hash is bcrypt, so each attempt already costs the attacker
	// time — but it costs this server the same time, which is the reason to
	// refuse early rather than to rely on the hash being slow.
	loginLimiter *ratelimit.Registry
}

// sessionKey signs session cookies, and tokenKey signs API tokens.
//
// Two different secrets on purpose. Rotating the session key ends every
// session on the server without touching anybody's tokens, which is the
// break-glass that used to be the only way to end a session at all. Tokens are
// signed with the server secret, so they outlive that.
//
// Both are read per use rather than captured, so replacing one takes effect on
// the next request instead of at the next restart.
func (self *authenticator) sessionKey() []byte {
	return self.config.Current().SessionKey()
}

func (self *authenticator) tokenKey() []byte {
	return self.config.Current().Secret()
}

// NewAuthenticator builds an Authenticator over the session and token tables.
func NewAuthenticator(configuration config.Store, database CredentialStore) (Authenticator, error) {
	if len(configuration.Current().SessionKey()) == 0 {
		return nil, fmt.Errorf("web: no session key; config.EnsureSecrets should have generated one")
	}
	if database == nil {
		return nil, fmt.Errorf("web: no database; sessions and tokens are stored there")
	}
	return &authenticator{
		config:   configuration,
		database: database,
		// Ten attempts a minute after an initial twenty. A person who has
		// forgotten which password they used will not notice; a program
		// working through a list will not get far.
		loginLimiter: ratelimit.NewRegistry(loginAttemptsPerMinute/60.0, loginAttemptBurst, loginAddressesRemembered, time.Hour),
	}, nil
}

func (self *authenticator) Required() bool {
	count, err := self.database.CountUsers()
	if err != nil {
		log.Errorf("could not count the accounts: %s", err)
		// Refusing is the safe direction: a database that cannot be read
		// cannot authenticate anybody either.
		return true
	}
	return count > 0
}

func (self *authenticator) Authenticate(request *http.Request) (string, bool) {
	username, _, ok := self.authenticate(request)
	return username, ok
}

// AllowLoginAttempt is the login limiter, for the sign-in paths that do not
// go through Login.
func (self *authenticator) AllowLoginAttempt(request *http.Request) bool {
	return self.loginLimiter.Allow(api.RemoteAddress(request, self.trustedProxies()))
}

// CurrentSessionID is the session a request is using, or empty when it is
// authenticated by a token or by nothing at all.
func (self *authenticator) CurrentSessionID(request *http.Request) string {
	_, sessionId, _ := self.authenticate(request)
	return sessionId
}

// authenticate resolves a request to an operator, and to the session it is
// using when it is using one.
func (self *authenticator) authenticate(request *http.Request) (string, string, bool) {
	// A bearer token is checked before the cookie, because a client that sent
	// one meant to use it, and falling back to an ambient session would hide
	// a revoked or mistyped token behind whoever happens to be logged in.
	if header := request.Header.Get("Authorization"); header != "" {
		username, ok := self.authenticateBearer(header, request)
		return username, "", ok
	}

	if !self.Required() {
		return "", "", true
	}

	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		return "", "", false
	}
	session := self.resolveSession(cookie.Value, request)
	if session == nil {
		return "", "", false
	}

	// A session for an account that has since been removed stops working
	// immediately, without waiting for the row to expire.
	user := self.findUserById(session.UserID)
	if user == nil || user.Disabled() {
		return "", "", false
	}
	// A stored account is never the console, whatever it is named: the
	// name is what says "console" downstream, and an account may not be
	// given it, but a row that somehow has it must not be believed.
	if models.IsReservedUsername(user.Username) {
		return "", "", false
	}
	return user.Username, session.ID, true
}

// resolveSession turns a cookie into the session it names, or nil.
//
// Every way of failing returns nil and says nothing about which way, so an
// unknown identifier, a wrong secret, an expired session and a revoked one
// cannot be told apart from outside.
func (self *authenticator) resolveSession(value string, request *http.Request) *models.Session {
	id, key, ok := parse(kindSession, SessionPrefix, value, self.sessionKey())
	if !ok {
		return nil
	}

	session, keyHash, err := self.database.GetSession(id)
	if err != nil {
		log.Errorf("could not read session %q: %s", id, err)
		return nil
	}
	if session == nil || !matches(keyHash, key) || !session.Active(time.Now()) {
		return nil
	}

	self.touch(session.ID, session.UsedAt, request, self.database.TouchSession)
	return session
}

// touch records that a credential was used, at most once every
// db.TouchInterval.
//
// Compared against the stored value rather than anything held in memory, so
// several instances do not each keep their own idea of when it was last
// written — and a dashboard left open on its refresh timer costs one row
// update a minute rather than one per poll.
func (self *authenticator) touch(
	id string,
	used time.Time,
	request *http.Request,
	write func(string, time.Time, string, string) error,
) {
	now := time.Now()
	if !used.IsZero() && now.Sub(used) < db.TouchInterval {
		return
	}
	ip, userAgent := requestOrigin(request, self.trustedProxies())
	if err := write(id, now, ip, userAgent); err != nil {
		// Not fatal. The request is authenticated either way, and a column
		// nobody reads more precisely than "this morning" is not worth
		// refusing somebody entry over.
		log.Warningf("could not record the use of %q: %s", id, err)
	}
}

// authenticateBearer resolves an Authorization header. Two kinds of token are
// accepted: one belonging to an account and stored in the token table, and one
// minted on the fly by a client that can read the server secret, which is how
// the command line client authenticates on the server itself.
func (self *authenticator) authenticateBearer(header string, request *http.Request) (string, bool) {
	value, ok := bearerToken(header)
	if !ok {
		return "", false
	}

	if strings.HasPrefix(value, config.LocalTokenPrefix) {
		if !self.config.Current().VerifyLocalToken(value) {
			return "", false
		}
		return config.LocalUsername, true
	}

	id, key, ok := parse(kindToken, TokenPrefix, value, self.tokenKey())
	if !ok {
		return "", false
	}
	token, keyHash, err := self.database.GetToken(id)
	if err != nil {
		log.Errorf("could not read token %q: %s", id, err)
		return "", false
	}
	if token == nil || !matches(keyHash, key) || !token.Active(time.Now()) {
		return "", false
	}
	// A token issued for one thing is refused everywhere else.
	//
	// Without this the resource a program asked for would be decoration and
	// the token a skeleton key: somebody who got hold of one minted for the
	// agent tools endpoint could spend it against the whole management API.
	// A token with no resource is one somebody minted by hand, which is what
	// every token was before programs could be authorized, and it keeps
	// meaning what it meant.
	if token.Resource != "" && !allowsResource(token.Resource, request) {
		log.Warningf("token %s was offered at %s but was issued for %s", token.ID, request.URL.Path, token.Resource)
		return "", false
	}

	// A token acts as the account it belongs to, so removing the account
	// takes its tokens with it.
	user := self.findUserById(token.UserID)
	if user == nil || user.Disabled() {
		return "", false
	}

	self.touch(token.ID, token.UsedAt, request, self.database.TouchToken)
	return user.Username, true
}

// allowsResource says whether a request is for the thing a token was issued
// for.
//
// Compared by path rather than by whole address. The resource was written
// down as the address the program asked through, and one server answers on
// more than one name -- a hostname, a loopback address, whatever a proxy
// forwards -- so comparing the whole thing would refuse a token for arriving
// by a different door of the same building.
func allowsResource(resource string, request *http.Request) bool {
	parsed, err := url.Parse(resource)
	if err != nil {
		return false
	}
	return parsed.Path == request.URL.Path
}

// bearerToken extracts the credential from an Authorization header, accepting
// the scheme in any case because RFC 7235 says it is case insensitive.
func bearerToken(header string) (string, bool) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func (self *authenticator) Login(response http.ResponseWriter, request *http.Request, username, password string) error {
	// Keyed by address, not by username: the username in a guess is chosen by
	// whoever is guessing, so counting per username lets them reset the count
	// by changing it.
	// Counted against the client rather than the proxy in front of it. Behind
	// a CDN every attempt shares one address, so one person guessing
	// passwords would use up the allowance for everybody.
	if !self.loginLimiter.Allow(api.RemoteAddress(request, self.trustedProxies())) {
		return ErrTooManyAttempts
	}

	user := self.findUser(username)
	if user == nil || user.Disabled() || user.PasswordHash == "" {
		// Spend the time anyway, so that a missing user and a wrong password
		// take about as long and cannot be told apart by timing. A disabled
		// user, and one who signs in only with a passkey or through an
		// identity provider, are refused the same way for the same reason.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$"+strings.Repeat("x", 53)), []byte(password))
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}

	return self.startSession(response, request, user)
}

// StartSession is the half of Login that follows the password check, for a
// caller that established who this is some other way.
func (self *authenticator) StartSession(response http.ResponseWriter, request *http.Request, username string) error {
	user := self.findUser(username)
	if user == nil || user.Disabled() {
		return ErrInvalidCredentials
	}
	return self.startSession(response, request, user)
}

func (self *authenticator) startSession(response http.ResponseWriter, request *http.Request, user *models.User) error {
	lifetime := self.config.Current().Session.Lifetime.Duration()
	expiry := time.Now().Add(lifetime)

	id, value, keyHash := issue(kindSession, SessionPrefix, self.sessionKey())
	ip, userAgent := requestOrigin(request, self.trustedProxies())
	if _, err := self.database.CreateSession(&models.Session{
		ID:        id,
		UserID:    user.ID,
		ExpiresAt: expiry,
		UsedAt:    time.Now(),
		IP:        ip,
		UserAgent: userAgent,
	}, keyHash); err != nil {
		return fmt.Errorf("web: cannot store the session: %w", err)
	}

	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   self.isSecureRequest(request),
		SameSite: http.SameSiteLaxMode,
	})
	log.Noticef("%s logged in from %s, session %s", user.Username, request.RemoteAddr, id)
	return nil
}

func (self *authenticator) Logout(response http.ResponseWriter, request *http.Request) {
	// The row is ended as well as the cookie cleared. Clearing only the
	// cookie would leave a working session behind for anybody who had a copy
	// of it, which is the thing a session table is for.
	if cookie, err := request.Cookie(SessionCookieName); err == nil {
		if id, _, ok := parse(kindSession, SessionPrefix, cookie.Value, self.sessionKey()); ok {
			if err := self.database.RevokeSession(id, time.Now()); err != nil {
				log.Errorf("could not revoke session %q on logout: %s", id, err)
			}
		}
	}

	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   self.isSecureRequest(request),
		SameSite: http.SameSiteLaxMode,
	})
}

// isSecureRequest reports whether the browser reached us over HTTPS.
//
// A reverse proxy that terminates TLS forwards plain HTTP, so request.TLS is
// nil even though the browser used HTTPS. Without accounting for that, the
// session cookie would go out without the Secure flag on exactly the
// deployments that most need it, and a browser would then be willing to send
// it over plain HTTP.
//
// X-Forwarded-Proto can be forged by a client talking to the server directly,
// which would only cause the cookie to be marked Secure when it need not be —
// the safe direction to be wrong in. An operator who exposes the server
// directly should serve HTTPS themselves, in which case request.TLS is set.
// trustedProxies is what the operator has said sits in front of this server,
// read at each use so a change takes effect without a restart.
func (self *authenticator) trustedProxies() []string {
	return self.config.Current().Server.TrustedProxies
}

func (self *authenticator) isSecureRequest(request *http.Request) bool {
	return api.IsSecure(request, self.trustedProxies())
}

func (self *authenticator) CreateFirstUser(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)

	if username == "" {
		return fmt.Errorf("%w: a username is required", ErrInvalidAccount)
	}
	if len(username) > 64 || strings.ContainsAny(username, " \t\r\n") {
		return fmt.Errorf("%w: a username may not contain spaces and must be under 64 characters", ErrInvalidAccount)
	}
	if password == "" {
		return fmt.Errorf("%w: a password is required", ErrInvalidAccount)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), security.PasswordCost)
	if err != nil {
		return fmt.Errorf("web: cannot hash the password: %w", err)
	}

	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Re-checked inside the transaction, because two people could reach
		// a fresh server at the same moment and only one may win.
		count, err := tx.CountUsers()
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrAccountExists
		}
		user, err := tx.CreateUser(&models.User{Username: username, PasswordHash: string(hash)})
		if err != nil {
			return err
		}
		// The first person is the administrator, and a member like everyone
		// else. The roles and groups are seeded here if a first start did
		// not get to it.
		if _, err := access.EnsureSeeded(tx); err != nil {
			return err
		}
		if _, err := access.EnsureMailbox(tx, user); err != nil {
			return err
		}
		return access.AddUserToGroups(tx, user.ID, models.GroupNameAdministrators, models.GroupNameMembers)
	}); err != nil {
		return err
	}

	log.Noticef("created the first dashboard account %q; this server is now claimed", username)
	return nil
}

// ChangePassword replaces an account's password.
//
// The current password is required even though the caller already holds a
// valid session: a session can be an unattended browser, and knowing the
// current password is what distinguishes the account's owner from whoever
// walked past their desk.
func (self *authenticator) ChangePassword(username, current, replacement string) error {
	user := self.findUser(username)
	if user == nil {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
		return ErrInvalidCredentials
	}
	if replacement == "" {
		return fmt.Errorf("%w: a new password is required", ErrInvalidAccount)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(replacement), security.PasswordCost)
	if err != nil {
		return fmt.Errorf("web: cannot hash the password: %w", err)
	}

	if err := self.database.TransactionContext(context.Background(), func(tx db.Transaction) error {
		_, err := tx.UpdateUser(user.ID, func(user *models.User) error {
			user.PasswordHash = string(hash)
			return nil
		})
		return err
	}); err != nil {
		return err
	}

	log.Noticef("%s changed their password", username)
	return nil
}

func (self *authenticator) findUser(username string) *models.User {
	if username == "" {
		return nil
	}
	user, err := self.database.GetUserByUsername(username)
	if err != nil {
		log.Errorf("could not read the account %q: %s", username, err)
		return nil
	}
	return user
}

// findUserById is how a stored session or token names its account. The
// identifier is what is stored, because a username can be changed and a
// session must not be ended by its owner renaming themselves.
func (self *authenticator) findUserById(userId string) *models.User {
	if userId == "" {
		return nil
	}
	user, err := self.database.GetUser(userId)
	if err != nil {
		log.Errorf("could not read the account %q: %s", userId, err)
		return nil
	}
	return user
}

// ListSessions returns an account's sessions, newest first.
//
// Named by username here and stored by identifier: the caller is a request
// that has been authenticated as somebody, and the name is what it knows. The
// name is filled back in on the way out, from the configuration rather than
// from a column, so it cannot be a stale copy.
func (self *authenticator) ListSessions(username string, includeRevoked bool) ([]*models.Session, error) {
	user := self.findUser(username)
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	sessions, err := self.database.ListSessions(user.ID, &db.SessionOptions{IncludeRevoked: includeRevoked})
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		session.Username = user.Username
	}
	return sessions, nil
}

// RevokeSession ends one session.
//
// The operator is checked against the row rather than trusted from the
// argument, so that a request cannot end somebody else's session by naming its
// identifier — which is otherwise the one thing a session list hands out.
func (self *authenticator) RevokeSession(username, sessionId string) error {
	user := self.findUser(username)
	if user == nil {
		return ErrInvalidCredentials
	}
	session, _, err := self.database.GetSession(sessionId)
	if err != nil {
		return err
	}
	// Somebody else's session is reported the same way as no session, so
	// that revoking is not a way of finding out which identifiers exist.
	if session == nil || session.UserID != user.ID {
		return api.ErrNotFound
	}
	return self.database.RevokeSession(sessionId, time.Now())
}

func (self *authenticator) RevokeSessions(username string, except string) (int64, error) {
	user := self.findUser(username)
	if user == nil {
		return 0, ErrInvalidCredentials
	}
	return self.database.RevokeSessionsByUser(user.ID, time.Now(), except)
}

// IssueToken mints an API token. The string it returns is the only time the
// token can be read; what is stored is a hash of half of it.
func (self *authenticator) IssueToken(username, name string, lifetime time.Duration) (*models.Token, string, error) {
	user := self.findUser(username)
	if user == nil {
		return nil, "", ErrInvalidCredentials
	}

	id, value, keyHash := issue(kindToken, TokenPrefix, self.tokenKey())
	token := &models.Token{ID: id, UserID: user.ID, Name: name}
	if lifetime > 0 {
		token.ExpiresAt = time.Now().Add(lifetime)
	}

	stored, err := self.database.CreateToken(token, keyHash)
	if err != nil {
		return nil, "", err
	}
	stored.Username = user.Username
	log.Noticef("%s issued API token %s (%q)", username, id, name)
	return stored, value, nil
}

func (self *authenticator) ListTokens(username string, includeRevoked bool) ([]*models.Token, error) {
	user := self.findUser(username)
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	tokens, err := self.database.ListTokens(user.ID, &db.SessionOptions{IncludeRevoked: includeRevoked})
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		token.Username = user.Username
	}
	return tokens, nil
}

// RevokeToken ends one token, checking it belongs to this operator for the
// same reason RevokeSession does.
func (self *authenticator) RevokeToken(username, tokenId string) error {
	user := self.findUser(username)
	if user == nil {
		return ErrInvalidCredentials
	}
	token, _, err := self.database.GetToken(tokenId)
	if err != nil {
		return err
	}
	// Somebody else's token is reported the same way as no token, so that
	// revoking is not a way of finding out which identifiers exist.
	if token == nil || token.UserID != user.ID {
		return api.ErrNotFound
	}
	return self.database.RevokeToken(tokenId, time.Now())
}

// UpdateToken changes one token, checking it belongs to this operator for
// the same reason RevokeToken does. The secret stays the same, so whatever
// holds the token goes on working without being handed a new one.
//
// A program's token is refused: it expires within the hour and is renewed
// in a new row each time, so a name or an expiry set on one would be gone
// at the next renewal. Revoking it is how a program is stopped.
func (self *authenticator) UpdateToken(username, tokenId string, name *string, lifetime *time.Duration) (*models.Token, error) {
	user := self.findUser(username)
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	token, _, err := self.database.GetToken(tokenId)
	if err != nil {
		return nil, err
	}
	if token == nil || token.UserID != user.ID {
		return nil, api.ErrNotFound
	}
	if !token.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: that token was revoked, so there is nothing to change; issue a new one", api.ErrInvalidArguments)
	}
	if token.ClientID != "" {
		return nil, fmt.Errorf("%w: that token belongs to a program, which renews it on its own; revoke it to stop the program", api.ErrInvalidArguments)
	}
	change := &db.TokenChange{}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: a name is required, so that a token can be recognized later", api.ErrInvalidArguments)
		}
		change.Name = &trimmed
	}
	now := time.Now()
	if lifetime != nil {
		if *lifetime < 0 {
			return nil, fmt.Errorf("%w: a lifetime has to be in the future", api.ErrInvalidArguments)
		}
		change.ShouldSetExpiry = true
		if *lifetime > 0 {
			change.ExpiresAt = now.Add(*lifetime)
		}
	}
	updated, err := self.database.UpdateToken(tokenId, change, now)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, api.ErrNotFound
	}
	updated.Username = user.Username
	log.Noticef("%s changed API token %s (%q, expires %s)", username, tokenId, updated.Name, describeExpiry(updated.ExpiresAt))
	return updated, nil
}

// describeExpiry is an expiry for the log.
func describeExpiry(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return "never"
	}
	return expiresAt.Format(time.RFC3339)
}

// Scavenge removes what is no longer worth keeping: sessions past their expiry
// and rows revoked long enough ago that nobody is still looking at them.
//
// Wired to a ticker by the server. A sweep that is written and never scheduled
// is a table that grows forever, which is what happens when nobody notices.
func (self *authenticator) Scavenge() error {
	now := time.Now()

	sessions, err := self.database.ScavengeSessions(now)
	if err != nil {
		return err
	}
	tokens, err := self.database.ScavengeTokens(now)
	if err != nil {
		return err
	}
	// Registration needs no credential, so without this sweep the table of
	// programs that introduced themselves grows with every one that ever
	// did, approved or not.
	programs, err := self.database.ScavengeOAuth(now)
	if err != nil {
		return err
	}
	if sessions > 0 || tokens > 0 || programs > 0 {
		log.Noticef("removed %d expired sessions, %d expired tokens and %d unused program registrations or approvals",
			sessions, tokens, programs)
	}
	return nil
}

// IssueAuthorizedToken mints the token a program collects after somebody
// approved it, and the secret it renews with.
//
// An ordinary token, with two things added: the client that holds it, so a
// person reading their list sees a program rather than a row, and the resource
// it is good for, so it is refused anywhere else. Everything that already
// works for a token minted by hand -- expiry, revocation, acting as the
// account, appearing in the audit log -- works for this one, because it is the
// same kind of thing.
func (self *authenticator) IssueAuthorizedToken(userId, name, clientId, resource string, lifetime time.Duration) (*models.Token, string, string, error) {
	id, value, keyHash := issue(kindToken, TokenPrefix, self.tokenKey())
	// The refresh secret is signed with the same key but a different kind, so
	// it verifies only where a refresh is expected. It carries the access
	// token's identifier, which is what a refresh has to name.
	refreshKey := security.GenerateRandomString(16, security.LowerAlphaNumeric)
	refreshValue := RefreshPrefix + security.EncodeToken(kindRefresh, id, refreshKey, self.tokenKey())

	token := &models.Token{ID: id, UserID: userId, Name: name, ClientID: clientId, Resource: resource}
	if lifetime > 0 {
		token.ExpiresAt = time.Now().Add(lifetime)
	}

	stored, err := self.database.CreateAuthorizedToken(token, keyHash, hashKey(refreshKey))
	if err != nil {
		return nil, "", "", err
	}
	log.Noticef("issued API token %s to the authorized client %s (%q)", id, clientId, name)
	return stored, value, refreshValue, nil
}

// RedeemRefresh checks a refresh secret and returns the token it renews.
//
// It does not mint the replacement: the caller does that, so that retiring the
// old token and issuing the new one stay in one place.
func (self *authenticator) RedeemRefresh(value string) (*models.Token, error) {
	id, key, ok := parse(kindRefresh, RefreshPrefix, value, self.tokenKey())
	if !ok {
		return nil, ErrInvalidCredentials
	}
	token, refreshHash, err := self.database.GetTokenRefresh(id)
	if err != nil {
		return nil, err
	}
	// A token with no refresh hash was minted by hand and cannot be renewed;
	// matches on an empty hash would be a comparison against nothing.
	if token == nil || refreshHash == "" || !matches(refreshHash, key) {
		return nil, ErrInvalidCredentials
	}
	// Refreshable rather than Active: a program renews when its access has
	// run out, often only after a request was refused, so an expired access
	// token is the usual case here rather than a reason to refuse.
	if !token.Refreshable(time.Now()) {
		return nil, ErrInvalidCredentials
	}
	return token, nil
}

// RevokeTokenByID retires a token by its identifier alone.
//
// RevokeToken takes an account as well, because a person revoking a token
// must not be able to name somebody else's. A refresh has already proved it
// holds the token it is retiring, so there is nobody to check it against.
func (self *authenticator) RevokeTokenByID(tokenId string) (bool, error) {
	return self.database.RetireToken(tokenId, time.Now())
}

// UserByID is the account an identifier names, or nil.
func (self *authenticator) UserByID(userId string) *models.User {
	return self.findUserById(userId)
}

// UserByName is the account a name signs in as, or nil.
func (self *authenticator) UserByName(username string) *models.User {
	return self.findUser(username)
}

// TokenIDOf reads the identifier out of an access token this server minted.
//
// The signature is checked, so this cannot be used to name a row somebody
// made up. The secret half is not compared, because the caller is revoking:
// holding the token is the only claim revoking it needs, and a wrong secret
// simply finds a row that whoever asked already had.
func (self *authenticator) TokenIDOf(value string) (string, bool) {
	id, _, ok := parse(kindToken, TokenPrefix, value, self.tokenKey())
	return id, ok
}
