package web

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

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
	ErrInvalidAccount = errors.New("that account will not work")

	// ErrAccountExists is returned when the first-run setup is attempted on a
	// server that already has an account.
	ErrAccountExists = errors.New("this server already has an account; sign in instead")

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

	// ListTokens returns an operator's tokens, newest first.
	ListTokens(username string, includeRevoked bool) ([]*models.Token, error)

	// RevokeToken ends one token belonging to an operator.
	RevokeToken(username, tokenId string) error

	// Scavenge removes sessions and tokens that are no longer worth keeping.
	Scavenge() error

	// Required reports whether any user is configured. When false the
	// dashboard is open to whoever can reach it, and the interface offers to
	// create the first account instead of asking for one.
	Required() bool

	// CreateFirstUser claims a server that has no account yet.
	CreateFirstUser(username, password string) error

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
	return len(self.config.Current().Users) > 0
}

func (self *authenticator) Authenticate(request *http.Request) (string, bool) {
	username, _, ok := self.authenticate(request)
	return username, ok
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
	user := self.findUserByID(session.UserID)
	if user == nil {
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
	ip, userAgent := requestOrigin(request)
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
	// A token acts as the account it belongs to, so removing the account
	// takes its tokens with it.
	user := self.findUserByID(token.UserID)
	if user == nil {
		return "", false
	}

	self.touch(token.ID, token.UsedAt, request, self.database.TouchToken)
	return user.Username, true
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
	if !self.loginLimiter.Allow(remoteAddress(request)) {
		return ErrTooManyAttempts
	}

	user := self.findUser(username)
	if user == nil {
		// Spend the time anyway, so that a missing user and a wrong password
		// take about as long and cannot be told apart by timing.
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
	if user == nil {
		return ErrInvalidCredentials
	}
	return self.startSession(response, request, user)
}

func (self *authenticator) startSession(response http.ResponseWriter, request *http.Request, user *config.User) error {
	lifetime := self.config.Current().Session.Lifetime.Duration()
	expiry := time.Now().Add(lifetime)

	id, value, keyHash := issue(kindSession, SessionPrefix, self.sessionKey())
	ip, userAgent := requestOrigin(request)
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
		Secure:   isSecureRequest(request),
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
		Secure:   isSecureRequest(request),
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
// remoteAddress is the address a rate limit is counted against. The port is
// dropped, because a limit that counts per port counts every connection
// separately and therefore counts nothing.
func remoteAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func isSecureRequest(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}
	forwarded := request.Header.Get("X-Forwarded-Proto")
	if forwarded == "" {
		return false
	}
	// A chain of proxies appends, so the client's own protocol is first.
	if index := strings.Index(forwarded, ","); index >= 0 {
		forwarded = forwarded[:index]
	}
	return strings.EqualFold(strings.TrimSpace(forwarded), "https")
}

func (self *authenticator) CreateFirstUser(username, password string) error {
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

	if err := self.config.Update(func(configuration *config.Configuration) error {
		// Re-checked inside the update, because two people could reach a
		// fresh server at the same moment and only one may win.
		if len(configuration.Users) > 0 {
			return ErrAccountExists
		}
		configuration.Users = []*config.User{
			{ID: config.NewID(), Username: username, PasswordHash: string(hash)},
		}
		return nil
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

	if err := self.config.Update(func(configuration *config.Configuration) error {
		for _, candidate := range configuration.Users {
			if candidate != nil && candidate.Username == username {
				candidate.PasswordHash = string(hash)
				return nil
			}
		}
		return ErrInvalidCredentials
	}); err != nil {
		return err
	}

	log.Noticef("%s changed their password", username)
	return nil
}

func (self *authenticator) findUser(username string) *config.User {
	if username == "" {
		return nil
	}
	for _, user := range self.config.Current().Users {
		if user != nil && user.Username == username {
			return user
		}
	}
	return nil
}

// findUserByID is how a stored session or token names its account. The
// identifier is what is stored, because a username can be changed and a
// session must not be ended by its owner renaming themselves.
func (self *authenticator) findUserByID(userId string) *config.User {
	if userId == "" {
		return nil
	}
	for _, user := range self.config.Current().Users {
		if user != nil && user.ID == userId {
			return user
		}
	}
	return nil
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
	if session == nil || session.UserID != user.ID {
		return ErrSessionInvalid
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
	if token == nil || token.UserID != user.ID {
		return ErrSessionInvalid
	}
	return self.database.RevokeToken(tokenId, time.Now())
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
	if sessions > 0 || tokens > 0 {
		log.Noticef("removed %d expired sessions and %d expired tokens", sessions, tokens)
	}
	return nil
}
