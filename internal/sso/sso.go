// Package sso signs people in through an OpenID Connect identity provider.
//
// The authorization-code flow with PKCE, a signed and expiring state, and a
// client that refuses to talk to a private address: everything the browser
// is redirected to, and everything this server fetches, is checked against
// the issuer the operator configured and nothing else.
package sso

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Provider is one identity provider as configured.
type Provider struct {
	// ID names the provider in paths and in user_identity rows: short,
	// lower-case, stable. Renaming one orphans every identity under it.
	ID string

	// Name is what the sign-in button says.
	Name string

	// Issuer is the OpenID Connect issuer URL; discovery is at
	// <issuer>/.well-known/openid-configuration.
	Issuer string

	ClientID     string
	ClientSecret string

	// GroupsClaim is the claim carrying group names, "groups" by default.
	GroupsClaim string

	// CreateUsers says whether somebody who signs in with no account here
	// gets one made.
	CreateUsers bool
}

// Claims is what the provider said about the person who signed in.
type Claims struct {
	Subject  string
	Email    string
	Name     string
	Username string
	Groups   []string
}

// Service runs the flow for every provider, caching discovery.
type Service struct {
	secret []byte
	client *http.Client

	mutex     sync.Mutex
	providers map[string]*oidc.Provider
}

// New makes a service whose state and cookies are signed with the secret.
func New(secret []byte) *Service {
	return &Service{
		secret:    secret,
		client:    guardedClient(),
		providers: map[string]*oidc.Provider{},
	}
}

// stateLifetime is how long a sign-in may take between leaving for the
// provider and coming back.
const stateLifetime = 10 * time.Minute

var (
	ErrBadState  = errors.New("sso: the sign-in did not start here, or took too long")
	ErrNoSubject = errors.New("sso: the provider named nobody")
)

// pending is what the browser carries between start and callback, signed.
type pending struct {
	Provider string    `json:"provider"`
	State    string    `json:"state"`
	Nonce    string    `json:"nonce"`
	Verifier string    `json:"verifier"`
	Expires  time.Time `json:"expires"`
	Return   string    `json:"return,omitempty"`
}

// Begin starts a sign-in: the URL to send the browser to, and the value to
// keep in a cookie until it comes back.
func (self *Service) Begin(ctx context.Context, provider Provider, redirectURL string, returnTo string) (string, string, error) {
	discovered, err := self.discover(ctx, provider)
	if err != nil {
		return "", "", err
	}
	state := random(24)
	nonce := random(24)
	verifier := oauth2.GenerateVerifier()
	config := self.oauthConfig(provider, discovered, redirectURL)
	authURL := config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	cookie, err := self.seal(&pending{
		Provider: provider.ID,
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Expires:  time.Now().Add(stateLifetime),
		Return:   returnTo,
	})
	if err != nil {
		return "", "", err
	}
	return authURL, cookie, nil
}

// Complete finishes a sign-in: the code and state the provider sent back,
// against the cookie the browser kept. Returns what the provider said about
// the person, and where they were going.
func (self *Service) Complete(ctx context.Context, provider Provider, redirectURL string, cookie string, state string, code string) (*Claims, string, error) {
	kept, err := self.open(cookie)
	if err != nil {
		return nil, "", ErrBadState
	}
	if kept.Provider != provider.ID || kept.State == "" || !subtleEqual(kept.State, state) || time.Now().After(kept.Expires) {
		return nil, "", ErrBadState
	}
	discovered, err := self.discover(ctx, provider)
	if err != nil {
		return nil, "", err
	}
	config := self.oauthConfig(provider, discovered, redirectURL)
	token, err := config.Exchange(oidc.ClientContext(ctx, self.client), code, oauth2.VerifierOption(kept.Verifier))
	if err != nil {
		return nil, "", fmt.Errorf("sso: the provider refused the code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, "", errors.New("sso: the provider sent no id token")
	}
	idToken, err := discovered.Verifier(&oidc.Config{ClientID: provider.ClientID}).Verify(oidc.ClientContext(ctx, self.client), rawIDToken)
	if err != nil {
		return nil, "", fmt.Errorf("sso: the id token did not verify: %w", err)
	}
	if idToken.Nonce != kept.Nonce {
		return nil, "", ErrBadState
	}
	raw := map[string]any{}
	if err := idToken.Claims(&raw); err != nil {
		return nil, "", fmt.Errorf("sso: the id token's claims did not parse: %w", err)
	}
	claims := claimsFrom(raw, provider.groupsClaim())
	// Groups are often left out of the id token to keep it small, and sent
	// from the userinfo endpoint instead.
	if claims.Groups == nil || claims.Email == "" {
		if info, err := discovered.UserInfo(oidc.ClientContext(ctx, self.client), oauth2.StaticTokenSource(token)); err == nil {
			more := map[string]any{}
			if err := info.Claims(&more); err == nil {
				extra := claimsFrom(more, provider.groupsClaim())
				if claims.Groups == nil {
					claims.Groups = extra.Groups
				}
				if claims.Email == "" {
					claims.Email = extra.Email
				}
				if claims.Name == "" {
					claims.Name = extra.Name
				}
				if claims.Username == "" {
					claims.Username = extra.Username
				}
			}
		}
	}
	if claims.Subject == "" {
		return nil, "", ErrNoSubject
	}
	if claims.Groups == nil {
		claims.Groups = []string{}
	}
	return claims, kept.Return, nil
}

func (self *Provider) groupsClaim() string {
	if self.GroupsClaim == "" {
		return "groups"
	}
	return self.GroupsClaim
}

func claimsFrom(raw map[string]any, groupsClaim string) *Claims {
	text := func(key string) string {
		if value, ok := raw[key].(string); ok {
			return strings.TrimSpace(value)
		}
		return ""
	}
	claims := &Claims{
		Subject:  text("sub"),
		Email:    text("email"),
		Name:     text("name"),
		Username: text("preferred_username"),
	}
	if claims.Username == "" {
		claims.Username = text("nickname")
	}
	if groups, ok := raw[groupsClaim]; ok {
		switch value := groups.(type) {
		case []any:
			claims.Groups = []string{}
			for _, group := range value {
				if name, ok := group.(string); ok && name != "" {
					claims.Groups = append(claims.Groups, name)
				}
			}
		case string:
			claims.Groups = []string{}
			for _, name := range strings.Split(value, " ") {
				if name != "" {
					claims.Groups = append(claims.Groups, name)
				}
			}
		}
	}
	return claims
}

func (self *Service) oauthConfig(provider Provider, discovered *oidc.Provider, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// discover reads the provider's configuration once and keeps it. The issuer
// has to be https and must not be a private address; the library checks
// that what is fetched names the same issuer.
func (self *Service) discover(ctx context.Context, provider Provider) (*oidc.Provider, error) {
	issuer := strings.TrimRight(strings.TrimSpace(provider.Issuer), "/")
	if err := checkIssuer(issuer); err != nil {
		return nil, err
	}
	self.mutex.Lock()
	cached := self.providers[provider.ID+"\x00"+issuer]
	self.mutex.Unlock()
	if cached != nil {
		return cached, nil
	}
	discovered, err := oidc.NewProvider(oidc.ClientContext(ctx, self.client), issuer)
	if err != nil {
		return nil, fmt.Errorf("sso: cannot read the provider's configuration: %w", err)
	}
	self.mutex.Lock()
	self.providers[provider.ID+"\x00"+issuer] = discovered
	self.mutex.Unlock()
	return discovered, nil
}

// Forget drops what was discovered, for when the settings change.
func (self *Service) Forget() {
	self.mutex.Lock()
	self.providers = map[string]*oidc.Provider{}
	self.mutex.Unlock()
}

func checkIssuer(issuer string) error {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("sso: the issuer has to be an https URL")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && isPrivate(ip) {
		return errors.New("sso: the issuer must not be a private address")
	}
	return nil
}

// guardedClient is an HTTP client that will not connect to a private,
// loopback or link-local address, whatever name it was given: a provider
// URL is operator input, and this server sits beside things a browser
// cannot reach.
func guardedClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, candidate := range addresses {
				if isPrivate(candidate.IP) {
					return nil, fmt.Errorf("sso: %s resolves to a private address", host)
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("sso: %s resolves to nothing", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("sso: too many redirects")
		}
		return nil
	}}
}

func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// --- the signed cookie -----------------------------------------------------------

func (self *Service) seal(value *pending) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, self.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (self *Service) open(cookie string) (*pending, error) {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil, ErrBadState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrBadState
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrBadState
	}
	mac := hmac.New(sha256.New, self.secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, ErrBadState
	}
	var value pending
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, ErrBadState
	}
	return &value, nil
}

func subtleEqual(left, right string) bool {
	return hmac.Equal([]byte(left), []byte(right))
}

func random(length int) string {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}
