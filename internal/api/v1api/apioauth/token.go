package apioauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// How long a token from this flow lasts.
//
// A month. A program still in use renews without anybody noticing; one that
// was set up and forgotten stops working, which is the point of it expiring
// at all.
const authorizedTokenLifetime = 30 * 24 * time.Hour

// tokenResponse is what a program gets, in the shape RFC 6749 fixes and that
// this repository's own client already parses at internal/mcp/oauth.go.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// oauthError is the error shape the specification fixes. A program reads
// these, so the code matters more than the sentence beside it.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeOAuthError(response http.ResponseWriter, status int, code, description string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(&oauthError{Error: code, ErrorDescription: description}); err != nil {
		log.Errorf("failed to write an error: %s", err)
	}
}

func (self *oauth) tokenView(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "the body is not a form")
		return
	}
	switch request.PostForm.Get("grant_type") {
	case "authorization_code":
		self.exchangeCode(response, request)
	case "refresh_token":
		self.refreshToken(response, request)
	default:
		writeOAuthError(response, http.StatusBadRequest, "unsupported_grant_type",
			"this server issues tokens for an authorization code or a refresh token")
	}
}

// exchangeCode turns a collected approval into a token.
func (self *oauth) exchangeCode(response http.ResponseWriter, request *http.Request) {
	code := request.PostForm.Get("code")
	verifier := request.PostForm.Get("code_verifier")
	clientId := clientIdOf(request)
	redirectURI := request.PostForm.Get("redirect_uri")

	// The approval is an identifier and a secret, joined. The identifier
	// finds the row and the secret proves this is the program it was issued
	// to rather than somebody who saw it go past.
	authorizationId, key, found := strings.Cut(code, ".")
	if !found || authorizationId == "" || key == "" {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that is not an authorization code")
		return
	}

	// Taken and deleted in one statement, so a code presented twice finds
	// nothing the second time however close together the two arrive.
	authorization, keyHash, err := self.database.SpendOAuthAuthorization(authorizationId, time.Now())
	if err != nil {
		log.Errorf("could not spend an authorization: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the authorization could not be read")
		return
	}
	if authorization == nil || !constantTimeEqual(keyHash, hashOf(key)) {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"that authorization has been used, has expired, or was not issued here")
		return
	}

	// The same program, coming back to the same place. An approval issued to
	// one client and collected by another, or sent to one address and
	// collected against another, is refused.
	if authorization.ClientID != clientId || (redirectURI != "" && authorization.RedirectURI != redirectURI) {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"that authorization was issued to a different program, or for a different address")
		return
	}

	// PKCE. The program sent a hash when it asked and sends the original now.
	// This is what makes an approval captured in transit worthless: whoever
	// captured it does not have the verifier.
	if !verifierMatches(verifier, authorization.CodeChallenge) {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "the verifier does not match the challenge")
		return
	}

	user := self.authenticator.UserByID(authorization.UserID)
	if user == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant", "that account no longer exists")
		return
	}

	client, err := self.database.GetOAuthClient(authorization.ClientID)
	name := "an authorized program"
	if err == nil && client != nil && client.Name != "" {
		name = client.Name
	}

	self.issue(response, user.ID, name, authorization.ClientID, authorization.Resource)
}

// refreshToken replaces a token with a fresh one and retires the old.
func (self *oauth) refreshToken(response http.ResponseWriter, request *http.Request) {
	previous, err := self.authenticator.RedeemRefresh(request.PostForm.Get("refresh_token"))
	if err != nil || previous == nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"that refresh token has been used, has expired, or was not issued here")
		return
	}
	// A refresh names the token it renews, so the client it belongs to is not
	// taken from the request: a client identifier in the body would be
	// somebody else's claim about whose token this is.
	if clientId := clientIdOf(request); clientId != "" && clientId != previous.ClientID {
		writeOAuthError(response, http.StatusBadRequest, "invalid_grant",
			"that refresh token belongs to a different program")
		return
	}

	// Retired before the replacement is minted. The other order leaves both
	// working if the second step fails, and a refresh token that still works
	// after being spent is the one thing a rotation is supposed to prevent.
	if err := self.authenticator.RevokeTokenByID(previous.ID); err != nil {
		log.Errorf("could not retire the token being refreshed: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the token could not be replaced")
		return
	}
	self.issue(response, previous.UserID, previous.Name, previous.ClientID, previous.Resource)
}

// clientIdOf is the client a token request names, in the body or in an HTTP
// Basic header.
//
// Clients here are public and are told to send their identifier in the body,
// but RFC 6749 lets a client put it in the Authorization header instead, and
// some do out of habit. Reading only the body refused those for having no
// client at all. Nothing is checked about the password half: these clients
// hold no secret, and whatever a client sends there proves nothing.
func clientIdOf(request *http.Request) string {
	if clientId := strings.TrimSpace(request.PostForm.Get("client_id")); clientId != "" {
		return clientId
	}
	if username, _, ok := request.BasicAuth(); ok {
		if decoded, err := url.QueryUnescape(username); err == nil {
			return strings.TrimSpace(decoded)
		}
	}
	return ""
}

// issue mints the token and answers with it.
func (self *oauth) issue(response http.ResponseWriter, userId, name, clientId, resource string) {
	_, value, refresh, err := self.authenticator.IssueAuthorizedToken(
		userId, name, clientId, resource, authorizedTokenLifetime)
	if err != nil {
		log.Errorf("could not issue an authorized token: %s", err)
		writeOAuthError(response, http.StatusInternalServerError, "server_error", "the token could not be issued")
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(response).Encode(&tokenResponse{
		AccessToken:  value,
		TokenType:    "Bearer",
		ExpiresIn:    int64(authorizedTokenLifetime / time.Second),
		RefreshToken: refresh,
		Scope:        scopeMCP,
	}); err != nil {
		log.Errorf("failed to answer with a token: %s", err)
	}
}

// revokeView retires a token a program no longer wants.
//
// A program that is being removed should be able to put its token beyond use
// without a person going to find it in a list. It presents the token itself,
// which is the only proof needed: whoever holds it can already use it.
func (self *oauth) revokeView(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeOAuthError(response, http.StatusBadRequest, "invalid_request", "the body is not a form")
		return
	}
	value := request.PostForm.Get("token")
	if token, err := self.authenticator.RedeemRefresh(value); err == nil && token != nil {
		if err := self.authenticator.RevokeTokenByID(token.ID); err != nil {
			log.Errorf("could not revoke a token: %s", err)
		}
	} else if id, ok := self.authenticator.TokenIDOf(value); ok {
		if err := self.authenticator.RevokeTokenByID(id); err != nil {
			log.Errorf("could not revoke a token: %s", err)
		}
	}
	// Always 200, whatever was presented. The specification says so, and the
	// reason is sound: answering differently for a token that existed would
	// turn this into a way of asking whether one does.
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
}

// verifierMatches checks a PKCE verifier against the challenge that was
// stored, using S256: the challenge is the base64url of the SHA-256 of the
// verifier, with no padding.
func verifierMatches(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare(
		[]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// hashOf writes a secret down the way the rest of this server does.
func hashOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
