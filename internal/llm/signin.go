package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A provider that signs in rather than taking a key.
//
// A key is a secret that stands still: it is written in the configuration
// and sent with every request until somebody changes it. A sign-in is not.
// It is a pair -- a refresh token that lasts, and an access token that does
// not -- and the short one has to be fetched before the first request and
// again before it runs out.
//
// This is that half, on its own and with no provider in it, because the
// same shape serves any service whose sign-in follows the usual grant: a
// refresh token kept in the configuration, an access token kept in memory
// and never written down, and one call to trade the first for the second.
//
// The access token is not written to the configuration. It lives an hour, it
// can always be fetched again, and a secret written to disk is a secret to
// look after. The refresh token is the one the operator configures and the
// one worth protecting. Several instances sharing one database are the
// exception, and are described at SignInStore.

// signInLeeway is how long before an access token runs out it is treated as
// already gone.
//
// A token that expires while a request is in flight fails that request, and
// the reading is a great many requests. A minute is longer than any call
// this makes and shorter than any token's life.
const signInLeeway = time.Minute

// signInFallbackLife is how long a token is assumed good for when the
// service says nothing and the token itself cannot be read.
//
// An hour, which is what these are. Short enough that a wrong guess costs
// one refused call and a refresh, rather than an outage.
const signInFallbackLife = time.Hour

// signIn holds a refresh token and trades it for access tokens.
//
// Safe for several callers: the reading runs its batches in parallel and
// they would otherwise all refresh at once, each invalidating the last
// where the service rotates refresh tokens.
type signIn struct {
	tokenUrl string
	clientId string
	refresh  string
	http     *http.Client

	// refreshMutex lets one caller of this process at a time trade the
	// refresh token, and mutex guards the fields below it. They are two
	// because the refresh waits on the network and on a lock other
	// instances hold, and the configuration reaching this sign-in while it
	// waits (adopt) must not wait behind it.
	refreshMutex sync.Mutex
	mutex        sync.Mutex
	access       string
	expires      time.Time

	// rejectedAccess is the access token the service refused, so that the
	// same one is not taken again from what another instance stored.
	rejectedAccess string

	// store and provider are set when several instances share this
	// sign-in's refresh token; see SignInStore. Without a store the
	// refresh token is this process's alone.
	store    SignInStore
	provider string

	// held is every refresh token this sign-in has held, so that a
	// provider built from one of them -- the configuration a moment behind
	// a rotation -- is given this sign-in rather than a second one.
	held map[string]bool

	// rotated is called when the service answers with a new refresh token,
	// so that whoever keeps the configuration can write it down. A service
	// that rotates and a caller that does not store the new one is signed
	// out at the next refresh.
	rotated func(refresh string)
}

func newSignIn(tokenUrl, clientId, refresh string, client *http.Client) (*signIn, error) {
	if strings.TrimSpace(tokenUrl) == "" || strings.TrimSpace(clientId) == "" {
		return nil, errors.New("llm: a sign-in needs an address and a client")
	}
	if strings.TrimSpace(refresh) == "" {
		return nil, errors.New("llm: a sign-in needs a refresh token; sign in and configure the one it gives")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &signIn{
		tokenUrl: strings.TrimSpace(tokenUrl),
		clientId: strings.TrimSpace(clientId),
		refresh:  strings.TrimSpace(refresh),
		http:     client,
		held:     map[string]bool{strings.TrimSpace(refresh): true},
	}, nil
}

// signIns are the sign-ins this process holds, one per refresh token.
//
// A service that rotates refresh tokens gives a new one each time an old one
// is used, and the old one stops working. Two sign-ins built from the same
// token are two holders of one credential: the first to refresh leaves the
// other holding a token the service no longer honours, and if the one that
// refreshed was a throwaway -- a provider built to list the models on a
// settings page -- the new token went nowhere. So there is one sign-in per
// token, and everything built from that token uses it.
var signIns = struct {
	sync.Mutex
	held []*signIn
}{}

// sharedSignIn is the sign-in already held for this refresh token, or a new
// one. unsaid is what a new one does with a rotated token until whoever keeps
// the configuration says otherwise; a sign-in already held keeps its own.
func sharedSignIn(tokenUrl, clientId, refresh string, client *http.Client, unsaid func(string)) (*signIn, error) {
	refresh = strings.TrimSpace(refresh)
	signIns.Lock()
	defer signIns.Unlock()
	for _, existing := range signIns.held {
		if existing.tokenUrl == strings.TrimSpace(tokenUrl) && existing.clientId == strings.TrimSpace(clientId) && existing.hasHeld(refresh) {
			return existing, nil
		}
	}
	made, err := newSignIn(tokenUrl, clientId, refresh, client)
	if err != nil {
		return nil, err
	}
	made.rotated = unsaid
	signIns.held = append(signIns.held, made)
	return made, nil
}

// hasHeld says whether this sign-in holds, or once held, a refresh token.
func (self *signIn) hasHeld(refresh string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return refresh != "" && self.held[refresh]
}

// SignInAccessToken is an access token and what it was bought with.
type SignInAccessToken struct {
	AccessToken string

	// ForRefreshToken is the refresh token the sign-in held when it fetched
	// the access token. One bought by another refresh token may belong to
	// another account, and is not taken.
	ForRefreshToken string
	ExpiresAt       time.Time
}

// SignInStore is where the instances sharing one database keep one sign-in
// between them.
//
// A service that rotates refresh tokens honours each only once. Instances
// that each hold their own copy and each refresh when their own access token
// runs out use the same one at once, the service refuses the second, and may
// treat the reuse as theft and revoke the whole chain, which signs every
// instance out. So a refresh happens under a lock all instances take, and
// the instance that has it first looks at what the others stored: a newer
// refresh token is taken up, and an access token still good is used, and
// only otherwise is the service asked, with the rotated tokens stored before
// the lock is let go.
type SignInStore interface {
	// WhileLocked does work holding the lock of a provider's sign-in, waiting
	// for another instance that holds it.
	WhileLocked(ctx context.Context, provider string, work func(context.Context) error) error

	// LoadRefreshToken re-reads the refresh token the configuration holds for
	// a provider now, rather than the copy this instance last heard of.
	LoadRefreshToken(ctx context.Context, provider string) (string, error)

	// LoadAccessToken is the access token last stored for a provider, or nil.
	LoadAccessToken(ctx context.Context, provider string) (*SignInAccessToken, error)

	// Save stores the refresh token and the access token a refresh gave.
	Save(ctx context.Context, provider, refreshToken string, accessToken *SignInAccessToken) error
}

// share has this sign-in keep its refresh token with the other instances
// through a store, under the provider's name.
func (self *signIn) share(provider string, store SignInStore) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.provider, self.store = provider, store
}

// heldAccess is the access token held, if it is good for the next moment.
func (self *signIn) heldAccess() (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.access != "" && time.Now().Add(signInLeeway).Before(self.expires) {
		return self.access, true
	}
	return "", false
}

// takeAccess holds an access token another instance fetched, if it was
// bought with the refresh token held and is good for the next moment, and
// says whether it did.
func (self *signIn) takeAccess(fetched *SignInAccessToken) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if fetched == nil || fetched.AccessToken == "" || fetched.ForRefreshToken != self.refresh ||
		fetched.AccessToken == self.rejectedAccess || !time.Now().Add(signInLeeway).Before(fetched.ExpiresAt) {
		return false
	}
	self.access, self.expires = fetched.AccessToken, fetched.ExpiresAt
	return true
}

// token is an access token good for the next moment, fetching one where
// what is held has run out or was never fetched.
func (self *signIn) token(ctx context.Context) (string, error) {
	if access, isGood := self.heldAccess(); isGood {
		return access, nil
	}

	self.refreshMutex.Lock()
	defer self.refreshMutex.Unlock()
	// Whoever held the refresh before this caller may have fetched it.
	if access, isGood := self.heldAccess(); isGood {
		return access, nil
	}

	self.mutex.Lock()
	store, provider := self.store, self.provider
	self.mutex.Unlock()
	if store == nil {
		return self.fetch(ctx)
	}
	var access string
	err := store.WhileLocked(ctx, provider, func(ctx context.Context) error {
		var err error
		access, err = self.tokenSharing(ctx, store, provider)
		return err
	})
	return access, err
}

// tokenSharing is token with the lock of the provider's sign-in held.
func (self *signIn) tokenSharing(ctx context.Context, store SignInStore, provider string) (string, error) {
	stored, err := store.LoadRefreshToken(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("llm: cannot read the stored sign-in of the %s provider: %w", provider, err)
	}
	// A newer refresh token than the one held was stored by another
	// instance after it refreshed, and the one held is no longer honoured.
	self.adoptWith(ctx, stored)
	if access, isGood := self.heldAccess(); isGood {
		return access, nil
	}
	fetched, err := store.LoadAccessToken(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("llm: cannot read the stored access token of the %s provider: %w", provider, err)
	}
	if self.takeAccess(fetched) {
		access, _ := self.heldAccess()
		return access, nil
	}

	access, err := self.fetch(ctx)
	if err != nil {
		return "", err
	}
	self.mutex.Lock()
	refresh, expires := self.refresh, self.expires
	self.mutex.Unlock()
	if err := store.Save(ctx, provider, refresh, &SignInAccessToken{AccessToken: access, ForRefreshToken: refresh, ExpiresAt: expires}); err != nil {
		// The access token is good and this instance holds the rotated
		// refresh token, so the request in hand goes ahead. What is lost is
		// the other instances' and the next start's copy of it.
		log.Warningf("the %s provider refreshed its sign-in and the new tokens could not be stored; the other instances will need signing in again: %s", provider, err)
	}
	return access, nil
}

// forget throws away the access token so the next call fetches another.
//
// For the answer that says the token is no good: it may have been revoked,
// or the service may have restarted behind it, and either way what is held
// is worth nothing and a second request with it would fail the same way.
func (self *signIn) forget() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.rejectedAccess = self.access
	self.access = ""
	self.expires = time.Time{}
}

// signInStoreWait is how long adopting a refresh token waits to read the
// access token stored with it.
const signInStoreWait = 10 * time.Second

// adopt takes a refresh token given from outside -- a new sign-in, or the
// one another instance stored after a rotation -- and drops the access token
// the old one bought, which may belong to another account. The one already
// held, or one held before, is left alone, access token and all: a
// configuration a moment behind a rotation says so with the old token.
//
// Where the sign-in is shared, the access token stored for the new refresh
// token is taken with it, so that an instance hearing of another's refresh
// does not throw away a token that is still good and fetch another.
func (self *signIn) adopt(refresh string) {
	ctx, cancel := context.WithTimeout(context.Background(), signInStoreWait)
	defer cancel()
	self.adoptWith(ctx, refresh)
}

func (self *signIn) adoptWith(ctx context.Context, refresh string) {
	refresh = strings.TrimSpace(refresh)
	self.mutex.Lock()
	if refresh == "" || self.held[refresh] {
		self.mutex.Unlock()
		return
	}
	self.refresh = refresh
	self.held[refresh] = true
	self.access = ""
	self.expires = time.Time{}
	store, provider := self.store, self.provider
	self.mutex.Unlock()

	if store == nil {
		return
	}
	fetched, err := store.LoadAccessToken(ctx, provider)
	if err != nil {
		// Nothing is lost by not having it: the next call fetches one.
		return
	}
	self.takeAccess(fetched)
}

// current is the refresh token held now, the newest a rotation gave.
func (self *signIn) current() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.refresh
}

// fetch trades the refresh token for an access token. The refresh mutex is
// held, and the mutex is not: the network is slow and a configuration
// arriving meanwhile (adopt) must not wait for it.
func (self *signIn) fetch(ctx context.Context) (string, error) {
	self.mutex.Lock()
	refresh := self.refresh
	self.mutex.Unlock()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {self.clientId},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.tokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := self.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("llm: cannot reach the sign-in: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil && response.StatusCode/100 == 2 {
		return "", fmt.Errorf("llm: the sign-in answered something unreadable: %w", err)
	}
	if response.StatusCode/100 != 2 {
		// The service's own words, because "the sign-in failed" does not
		// tell an operator whether to sign in again or wait.
		said := strings.TrimSpace(answer.Description)
		if said == "" {
			said = strings.TrimSpace(answer.Error)
		}
		if said == "" {
			said = http.StatusText(response.StatusCode)
		}
		return "", &SignInError{Status: response.StatusCode, Said: said}
	}
	if strings.TrimSpace(answer.AccessToken) == "" {
		return "", errors.New("llm: the sign-in gave no access token")
	}

	var expires time.Time
	switch {
	case answer.ExpiresIn > 0:
		expires = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	default:
		// The token says when it runs out, where it is one that says
		// anything. Read rather than trusted: the claim is not signed to
		// us and a wrong answer costs only an early refresh.
		if when, ok := expiryOfJwt(answer.AccessToken); ok {
			expires = when
		} else {
			expires = time.Now().Add(signInFallbackLife)
		}
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.access, self.expires = answer.AccessToken, expires

	// A rotated refresh token has to be kept or the next refresh signs out.
	// Where the sign-in is shared the caller stores it with the lock held,
	// rather than telling whoever keeps the configuration after the fact.
	if rotated := strings.TrimSpace(answer.RefreshToken); rotated != "" && rotated != self.refresh {
		self.refresh = rotated
		self.held[rotated] = true
		if self.rotated != nil && self.store == nil {
			self.rotated(rotated)
		}
	}
	return self.access, nil
}

// SignInError is the sign-in refusing, with what it said.
type SignInError struct {
	Status int
	Said   string
}

func (self *SignInError) Error() string {
	return fmt.Sprintf("llm: the sign-in answered %d: %s", self.Status, self.Said)
}

// Reauthorize says whether the refresh token itself is no good, so that a
// person has to sign in again rather than the server waiting for it to come
// right on its own.
func (self *SignInError) Reauthorize() bool {
	return self.Status == http.StatusBadRequest || self.Status == http.StatusUnauthorized
}

// expiryOfJwt reads the expiry out of a token that is one, without checking
// the signature: this is a hint about when to fetch another, not a claim
// anything is trusted on.
func expiryOfJwt(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Expires int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Expires <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Expires, 0), true
}
