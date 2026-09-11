package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth 2.1 for a server that wants a person's authorization: the
// endpoints found from the server's metadata, a code flow with PKCE, and
// a refresh when the token expires. No secret is needed for a public
// client; one is sent when the operator configured it.

// Metadata is what an authorization server publishes about itself.
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// Tokens are what an authorization gave.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
}

// Expired says whether the access token is past its time, with a minute
// of grace.
func (self *Tokens) Expired(now time.Time) bool {
	return !self.ExpiresAt.IsZero() && now.Add(time.Minute).After(self.ExpiresAt)
}

// OAuthSettings are what a flow needs.
type OAuthSettings struct {
	// ServerURL is the MCP server, whose origin publishes the metadata
	// when the endpoints are not configured.
	ServerURL string

	ClientID     string
	ClientSecret string
	Scopes       []string

	// AuthorizationURL and TokenURL, when set, skip discovery.
	AuthorizationURL string
	TokenURL         string

	// RedirectURL is where the code comes back to.
	RedirectURL string

	Client *http.Client
}

func (self *OAuthSettings) client() *http.Client {
	if self.Client != nil {
		return self.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Discover reads the authorization server's metadata: the protected
// resource's metadata first, then the well-known document at the origin.
func Discover(ctx context.Context, settings *OAuthSettings) (*Metadata, error) {
	if settings.AuthorizationURL != "" && settings.TokenURL != "" {
		return &Metadata{AuthorizationEndpoint: settings.AuthorizationURL, TokenEndpoint: settings.TokenURL}, nil
	}
	server, err := url.Parse(settings.ServerURL)
	if err != nil {
		return nil, err
	}
	origin := server.Scheme + "://" + server.Host
	candidates := []string{}
	// The protected resource may name its authorization server.
	if resource, err := fetchJSON[struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}](ctx, settings.client(), origin+"/.well-known/oauth-protected-resource"); err == nil {
		for _, authorizationServer := range resource.AuthorizationServers {
			candidates = append(candidates, strings.TrimSuffix(authorizationServer, "/")+"/.well-known/oauth-authorization-server")
		}
	}
	candidates = append(candidates, origin+"/.well-known/oauth-authorization-server", origin+"/.well-known/openid-configuration")
	var lastErr error
	for _, candidate := range candidates {
		metadata, err := fetchJSON[Metadata](ctx, settings.client(), candidate)
		if err != nil {
			lastErr = err
			continue
		}
		if metadata.AuthorizationEndpoint != "" && metadata.TokenEndpoint != "" {
			return &metadata, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization endpoints published")
	}
	return nil, fmt.Errorf("mcp: cannot discover the authorization server of %s: %w", settings.ServerURL, lastErr)
}

func fetchJSON[T any](ctx context.Context, client *http.Client, address string) (T, error) {
	var value T
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return value, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return value, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return value, fmt.Errorf("%s answered %d", address, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil {
		return value, err
	}
	return value, nil
}

// Authorization is a flow begun: where to send the person, and what to
// keep until they come back.
type Authorization struct {
	URL      string
	State    string
	Verifier string
}

// Begin starts a code flow with PKCE.
func Begin(ctx context.Context, settings *OAuthSettings) (*Authorization, error) {
	metadata, err := Discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	verifier := randomToken(32)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	state := randomToken(16)
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {settings.ClientID},
		"redirect_uri":          {settings.RedirectURL},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if len(settings.Scopes) > 0 {
		values.Set("scope", strings.Join(settings.Scopes, " "))
	}
	resource := settings.ServerURL
	if resource != "" {
		values.Set("resource", resource)
	}
	separator := "?"
	if strings.Contains(metadata.AuthorizationEndpoint, "?") {
		separator = "&"
	}
	return &Authorization{URL: metadata.AuthorizationEndpoint + separator + values.Encode(), State: state, Verifier: verifier}, nil
}

// Exchange turns the code the person came back with into tokens.
func Exchange(ctx context.Context, settings *OAuthSettings, code, verifier string) (*Tokens, error) {
	metadata, err := Discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {settings.RedirectURL},
		"client_id":     {settings.ClientID},
		"code_verifier": {verifier},
	}
	if settings.ServerURL != "" {
		values.Set("resource", settings.ServerURL)
	}
	return tokenRequest(ctx, settings, metadata.TokenEndpoint, values)
}

// Refresh trades a refresh token for new tokens.
func Refresh(ctx context.Context, settings *OAuthSettings, refreshToken string) (*Tokens, error) {
	metadata, err := Discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {settings.ClientID},
	}
	tokens, err := tokenRequest(ctx, settings, metadata.TokenEndpoint, values)
	if err != nil {
		return nil, err
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}
	return tokens, nil
}

func tokenRequest(ctx context.Context, settings *OAuthSettings, endpoint string, values url.Values) (*Tokens, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if settings.ClientSecret != "" {
		request.SetBasicAuth(settings.ClientID, settings.ClientSecret)
	}
	response, err := settings.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: the token endpoint answered %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("mcp: the token answer is not JSON: %w", err)
	}
	if decoded.AccessToken == "" {
		return nil, fmt.Errorf("mcp: the token answer carries no access token")
	}
	tokens := &Tokens{AccessToken: decoded.AccessToken, RefreshToken: decoded.RefreshToken, Scope: decoded.Scope}
	if decoded.ExpiresIn > 0 {
		tokens.ExpiresAt = time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	}
	return tokens, nil
}

func randomToken(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}
