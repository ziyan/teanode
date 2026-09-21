package llm

import (
	"context"
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
	"time"
)

// Signing in for the first time, to get the refresh token everything else
// here runs on.
//
// This is the half a person does once, at a browser, and it happens on
// their own machine rather than on the server: the server has no browser
// and no business holding somebody's password. What comes back is a refresh
// token, which is what goes into the configuration.
//
// The grant is the usual one for a program that cannot keep a secret. There
// is no client secret -- anyone can read it out of a command line tool, so
// it would protect nothing -- and in its place the program invents a secret
// per sign-in, sends its fingerprint when it asks, and shows the secret
// itself when it redeems the code. Nobody who intercepts the code can use
// it without that secret.

// The address the service sends the browser back to. Registered with the
// application, so it is this or nothing: a different port is refused before
// the person has finished reading the page.
const signInRedirect = "http://localhost:1455/auth/callback"

// signInWait is how long to wait at the browser before giving up.
const signInWait = 5 * time.Minute

// SignInResult is what a sign-in hands back.
type SignInResult struct {
	// RefreshToken is the one to configure. It is the only part worth
	// keeping: the access token beside it lasts an hour.
	RefreshToken string

	// Account is which of the person's accounts the work bills to, read
	// out of the token the service issued.
	Account string

	// Plan is what the account is entitled to, for saying so afterwards.
	// Empty where the service did not say.
	Plan string
}

// SignInFlow is a sign-in waiting at the browser.
type SignInFlow struct {
	// Address is the page to open. A person opens it, signs in, and the
	// service sends the browser back here.
	Address string

	verifier string
	state    string
	listener net.Listener
	tokenUrl string
	clientId string
}

// BeginSignIn starts a sign-in and returns the page to open.
//
// Nothing happens until somebody opens it. Close ends the wait, and must be
// called whether or not the sign-in finishes, or the port stays taken.
func BeginSignIn(kind string) (*SignInFlow, error) {
	clientId, tokenUrl, authorizeUrl, err := signInEndpoints(kind)
	if err != nil {
		return nil, err
	}
	verifier, err := randomToken()
	if err != nil {
		return nil, err
	}
	state, err := randomToken()
	if err != nil {
		return nil, err
	}

	// Bound to the loopback and to the one port the service will send a
	// browser back to.
	listener, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return nil, fmt.Errorf("llm: cannot wait for the browser on port 1455, which is the only one this sign-in may use: %w", err)
	}

	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientId},
		"redirect_uri":          {signInRedirect},
		"scope":                 {"openid profile email offline_access"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		// Without this the service issues a token good for an hour and
		// nothing to renew it with, and the sign-in has to be done again
		// every hour for ever.
		"prompt": {"login"},
	}
	return &SignInFlow{
		Address:  authorizeUrl + "?" + query.Encode(),
		verifier: verifier,
		state:    state,
		listener: listener,
		tokenUrl: tokenUrl,
		clientId: clientId,
	}, nil
}

// Close ends the wait and gives the port back.
func (self *SignInFlow) Close() {
	if self.listener != nil {
		_ = self.listener.Close()
	}
}

// Wait waits for the browser to come back, and trades what it brings for a
// refresh token.
func (self *SignInFlow) Wait(ctx context.Context) (*SignInResult, error) {
	type arrival struct {
		code string
		err  error
	}
	arrived := make(chan arrival, 1)

	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			query := request.URL.Query()
			if problem := query.Get("error"); problem != "" {
				said := query.Get("error_description")
				if said == "" {
					said = problem
				}
				writeSignInPage(writer, "That did not work: "+said)
				arrived <- arrival{err: fmt.Errorf("llm: the sign-in was refused: %s", said)}
				return
			}
			// The state is the answer to "is this the browser I sent?".
			// Without it anybody able to reach this port could hand it a
			// code of their own and have the server sign in as them.
			if query.Get("state") != self.state {
				writeSignInPage(writer, "That did not work: it was not this sign-in.")
				arrived <- arrival{err: errors.New("llm: the browser came back for a different sign-in")}
				return
			}
			code := query.Get("code")
			if code == "" {
				writeSignInPage(writer, "That did not work: the service sent nothing back.")
				arrived <- arrival{err: errors.New("llm: the service sent no code")}
				return
			}
			writeSignInPage(writer, "Signed in. You can close this page and go back to the terminal.")
			arrived <- arrival{code: code}
		}),
	}
	go func() { _ = server.Serve(self.listener) }()
	defer func() { _ = server.Close() }()

	timed, cancel := context.WithTimeout(ctx, signInWait)
	defer cancel()

	select {
	case <-timed.Done():
		return nil, fmt.Errorf("llm: nobody finished the sign-in within %s", signInWait)
	case came := <-arrived:
		if came.err != nil {
			return nil, came.err
		}
		return self.redeem(ctx, came.code)
	}
}

// redeem trades the code for tokens.
func (self *SignInFlow) redeem(ctx context.Context, code string) (*SignInResult, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {signInRedirect},
		"client_id":     {self.clientId},
		"code_verifier": {self.verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.tokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("llm: cannot reach the sign-in: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	var answer struct {
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	_ = json.NewDecoder(response.Body).Decode(&answer)
	if response.StatusCode/100 != 2 {
		said := strings.TrimSpace(answer.Description)
		if said == "" {
			said = strings.TrimSpace(answer.Error)
		}
		if said == "" {
			said = http.StatusText(response.StatusCode)
		}
		return nil, &SignInError{Status: response.StatusCode, Said: said}
	}
	if strings.TrimSpace(answer.RefreshToken) == "" {
		// Nearly always the scope: without offline_access the service
		// issues an access token and nothing to renew it with.
		return nil, errors.New("llm: the sign-in gave no refresh token, so there would be nothing to renew with")
	}

	result := &SignInResult{RefreshToken: answer.RefreshToken}
	result.Account, result.Plan = accountOfToken(answer.IDToken)
	return result, nil
}

// accountOfToken reads which account and plan a token was issued for.
//
// Read, not verified: the claim is not signed to this program, and nothing
// is trusted on it. It is here so that a person is told what they have just
// signed in as rather than having to go and look.
func accountOfToken(token string) (account, plan string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", ""
	}
	for key, value := range claims {
		if !strings.HasSuffix(key, "/auth") {
			continue
		}
		if fields, ok := value.(map[string]any); ok {
			if id, ok := fields["chatgpt_account_id"].(string); ok {
				account = id
			}
			if named, ok := fields["chatgpt_plan_type"].(string); ok {
				plan = named
			}
		}
	}
	return account, plan
}

// signInEndpoints is where a kind signs in.
func signInEndpoints(kind string) (clientId, tokenUrl, authorizeUrl string, err error) {
	switch kind {
	case "openai-codex":
		return codexClientId, codexTokenUrl, "https://auth.openai.com/oauth/authorize", nil
	}
	return "", "", "", fmt.Errorf("llm: %q is not a provider kind that signs in", kind)
}

// randomToken is a secret nobody can guess, written so it survives a URL.
func randomToken() (string, error) {
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("llm: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// writeSignInPage is what the browser shows when it comes back.
func writeSignInPage(writer http.ResponseWriter, said string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(writer,
		"<!doctype html><meta charset=utf-8><title>TeaNode</title>"+
			"<body style=\"font:16px system-ui;margin:4rem auto;max-width:32rem\"><p>%s</p>",
		said)
}
