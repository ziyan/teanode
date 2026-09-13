package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
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

// Tokens are what an authorization gave. ClientID is kept beside them
// because a client this server registered for itself is not in the
// configuration, and refreshing needs the same one that was authorized.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
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

	// ClientName is what a client registered here is called.
	ClientName string

	Client *http.Client
}

// client is for an address the operator typed: the connected server's own.
// Unguarded, because declaring a server at a private address is a thing an
// operator does on purpose -- one running beside this on the same host, or
// inside the same network -- and they may already declare one that runs as a
// command.
func (self *OAuthSettings) client() *http.Client {
	if self.Client != nil {
		return self.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// followed is for an address the connected *server* chose: the authorization
// servers its own metadata names.
//
// Guarded when the declared server is out on the internet, because then a
// document written by the far end is choosing where this server connects, and
// unguarded that reaches private addresses, the metadata service, and this
// server's own API on loopback.
//
// Not guarded when the operator declared the server at a private or loopback
// address, because then private addresses are the deployment: a connected
// server inside somebody's network naming an authorization server inside the
// same network is the ordinary case, and refusing it would be refusing what
// they set up. The trust boundary is the operator's network, and they put the
// server inside it on purpose.
func (self *OAuthSettings) followed() *http.Client {
	if self.Client != nil {
		return self.Client
	}
	if declaredPrivately(self.ServerURL) {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return safefetch.Client()
}

// declaredPrivately is whether the address the operator typed for this server
// is one only their own network can reach.
func declaredPrivately(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if loopback(host) {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	resolved, err := net.LookupIP(host)
	if err != nil || len(resolved) == 0 {
		return false
	}
	for _, ip := range resolved {
		if !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
			return false
		}
	}
	return true
}

// usableEndpoint refuses an authorization or token endpoint this server will
// not send a person's secrets to.
//
// The endpoints come out of a document the connected server chose, and what
// goes to them is the authorization code, the PKCE verifier and, where the
// operator configured one, the client secret. Single sign-on has required an
// https issuer that names itself since it was written; this is the same rule,
// in the place it was missing.
func usableEndpoint(endpoint string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return fmt.Errorf("mcp: %q is not an address", endpoint)
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !loopback(parsed.Hostname()) {
		return fmt.Errorf("mcp: %s is not https, and a person's credentials are not sent over anything else", endpoint)
	}
	if parsed.Host == "" {
		return fmt.Errorf("mcp: %q names no host", endpoint)
	}
	return nil
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
	named := map[string]bool{}
	if resource, err := fetchJSON[struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}](ctx, settings.client(), origin+"/.well-known/oauth-protected-resource"); err == nil {
		for _, authorizationServer := range resource.AuthorizationServers {
			for _, candidate := range wellKnown(authorizationServer, "oauth-authorization-server") {
				// The server named this one, so it is followed with the
				// guard on.
				named[candidate] = true
				candidates = append(candidates, candidate)
			}
		}
	}
	candidates = append(candidates, wellKnown(settings.ServerURL, "oauth-authorization-server")...)
	candidates = append(candidates, origin+"/.well-known/oauth-authorization-server", origin+"/.well-known/openid-configuration")
	var lastErr error
	for _, candidate := range candidates {
		fetch := settings.client()
		if named[candidate] {
			fetch = settings.followed()
		}
		metadata, err := fetchJSON[Metadata](ctx, fetch, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
			continue
		}
		if err := usableEndpoint(metadata.AuthorizationEndpoint); err != nil {
			lastErr = err
			continue
		}
		if err := usableEndpoint(metadata.TokenEndpoint); err != nil {
			lastErr = err
			continue
		}
		// A document has to name itself. Fetched from one address and
		// claiming another, it is somebody else's metadata -- which is the
		// whole point of the issuer field.
		if metadata.Issuer != "" && !sameIssuer(metadata.Issuer, candidate) {
			lastErr = fmt.Errorf("mcp: the metadata at %s says it belongs to %s", candidate, metadata.Issuer)
			continue
		}
		return &metadata, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization endpoints published")
	}
	return nil, fmt.Errorf("mcp: cannot discover the authorization server of %s: %w", settings.ServerURL, lastErr)
}

// loopback is a host that never leaves this machine, where plain HTTP carries
// nothing anybody else can read. An operator running a connected server beside
// this one is the case this exists for.
func loopback(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	if address := net.ParseIP(host); address != nil {
		return address.IsLoopback()
	}
	return false
}

// sameIssuer is whether a document fetched from one address may claim to
// belong to an issuer. The issuer names an origin; the document lives under
// it, at a well-known path.
func sameIssuer(issuer, fetched string) bool {
	claimed, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return false
	}
	from, err := url.Parse(strings.TrimSpace(fetched))
	if err != nil {
		return false
	}
	return strings.EqualFold(claimed.Scheme, from.Scheme) && strings.EqualFold(claimed.Host, from.Host)
}

// wellKnown is where an issuer publishes a document, in both the shapes
// that are used: the segment goes between the host and the issuer's own
// path, which is what the specification says, and the older form that
// appends it, which is what several servers actually serve.
func wellKnown(issuer, document string) []string {
	parsed, err := url.Parse(strings.TrimSuffix(issuer, "/"))
	if err != nil || parsed.Host == "" {
		return nil
	}
	origin := parsed.Scheme + "://" + parsed.Host
	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" {
		return []string{origin + "/.well-known/" + document}
	}
	return []string{origin + "/.well-known/" + document + path, origin + path + "/.well-known/" + document}
}

// Register asks the authorization server for a client of our own, which is
// how a server that publishes no client id is reached: the operator
// declares the server and nothing else, and the client is made on the
// first authorization. Servers that want a client registered by hand say
// so by publishing no registration endpoint.
func Register(ctx context.Context, settings *OAuthSettings, metadata *Metadata) (string, string, error) {
	if metadata.RegistrationEndpoint == "" {
		return "", "", fmt.Errorf("mcp: %s publishes no client id and no way to register one; set agent.mcp.servers[].oauth.clientId", settings.ServerURL)
	}
	body, err := json.Marshal(map[string]any{
		"client_name":                settings.ClientName,
		"redirect_uris":              []string{settings.RedirectURL},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      strings.Join(settings.Scopes, " "),
	})
	if err != nil {
		return "", "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := settings.client().Do(request)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		answer, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return "", "", fmt.Errorf("mcp: %s refused to register a client: %d %s", metadata.RegistrationEndpoint, response.StatusCode, strings.TrimSpace(string(answer)))
	}
	var registered struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&registered); err != nil {
		return "", "", err
	}
	if registered.ClientID == "" {
		return "", "", fmt.Errorf("mcp: %s registered a client with no id", metadata.RegistrationEndpoint)
	}
	return registered.ClientID, registered.ClientSecret, nil
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

	// ClientID is the client the flow was begun with, which is the
	// configured one, or the one registered for it here. Finishing the
	// flow must use the same.
	ClientID string
}

// Begin starts a code flow with PKCE.
func Begin(ctx context.Context, settings *OAuthSettings) (*Authorization, error) {
	metadata, err := Discover(ctx, settings)
	if err != nil {
		return nil, err
	}
	clientId := settings.ClientID
	if clientId == "" {
		registered, secret, err := Register(ctx, settings, metadata)
		if err != nil {
			return nil, err
		}
		clientId, settings.ClientID = registered, registered
		if secret != "" {
			settings.ClientSecret = secret
		}
	}
	verifier := randomToken(32)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	state := randomToken(16)
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientId},
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
	return &Authorization{URL: metadata.AuthorizationEndpoint + separator + values.Encode(), State: state, Verifier: verifier, ClientID: clientId}, nil
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
	tokens, err := tokenRequest(ctx, settings, metadata.TokenEndpoint, values)
	if err != nil {
		return nil, err
	}
	tokens.ClientID = settings.ClientID
	return tokens, nil
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
	tokens.ClientID = settings.ClientID
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
