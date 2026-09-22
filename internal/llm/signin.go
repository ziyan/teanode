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
// The access token is deliberately not persisted. It lives an hour, it can
// always be fetched again, and a secret written to disk is a secret to look
// after. The refresh token is the one the operator configures and the one
// worth protecting.

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

	mutex   sync.Mutex
	access  string
	expires time.Time

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
	}, nil
}

// token is an access token good for the next moment, fetching one where
// what is held has run out or was never fetched.
func (self *signIn) token(ctx context.Context) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.access != "" && time.Now().Add(signInLeeway).Before(self.expires) {
		return self.access, nil
	}
	return self.fetch(ctx)
}

// forget throws away the access token so the next call fetches another.
//
// For the answer that says the token is no good: it may have been revoked,
// or the service may have restarted behind it, and either way what is held
// is worth nothing and a second request with it would fail the same way.
func (self *signIn) forget() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.access = ""
	self.expires = time.Time{}
}

// fetch trades the refresh token for an access token. The mutex is held.
func (self *signIn) fetch(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {self.refresh},
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

	self.access = answer.AccessToken
	switch {
	case answer.ExpiresIn > 0:
		self.expires = time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)
	default:
		// The token says when it runs out, where it is one that says
		// anything. Read rather than trusted: the claim is not signed to
		// us and a wrong answer costs only an early refresh.
		if when, ok := expiryOfJwt(answer.AccessToken); ok {
			self.expires = when
		} else {
			self.expires = time.Now().Add(signInFallbackLife)
		}
	}

	// A rotated refresh token has to be kept or the next refresh signs out.
	if rotated := strings.TrimSpace(answer.RefreshToken); rotated != "" && rotated != self.refresh {
		self.refresh = rotated
		if self.rotated != nil {
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
