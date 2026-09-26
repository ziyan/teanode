package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// Signing in with a code typed on another device.
//
// The sign-in at a browser sends the browser back to a port on the machine
// that started it, which is no use to a server somewhere else: the person
// is at a dashboard, not at the server. So the server asks the service for
// a one-time code, the person opens the service's own page on any device,
// signs in there and types the code, and the server, asking every few
// seconds, is handed an authorization code to redeem like any other. The
// person's password never passes through the server, and neither does
// anything a person could reuse: the code works once, for fifteen minutes.
//
// The account has to allow it. Where it does not, the service says so when
// the code is asked for, and that is what the person is shown.

// ErrSignInPending is a sign-in whose code has not been entered yet.
var ErrSignInPending = errors.New("llm: the code has not been entered yet")

// deviceSignInInterval is how often to ask when the service does not say.
const deviceSignInInterval = 5 * time.Second

// DeviceSignIn is a sign-in waiting for its code to be entered.
type DeviceSignIn struct {
	// UserCode is what the person types on the service's page.
	UserCode string

	// VerificationAddress is that page.
	VerificationAddress string

	// ExpiresAt is when the code stops working.
	ExpiresAt time.Time

	issuer       string
	clientId     string
	tokenUrl     string
	deviceAuthId string
	interval     time.Duration
	http         *http.Client

	// Waits do not overlap, and the answer is kept: a code is redeemed
	// once, and a second finish asked for while the first is still out
	// must get the same answer rather than a refusal.
	mutex  sync.Mutex
	result *SignInResult
}

// BeginDeviceSignIn asks the service for a one-time code.
func BeginDeviceSignIn(ctx context.Context, kind string) (*DeviceSignIn, error) {
	switch kind {
	case config.AgentProviderKindCodex:
		return beginDeviceSignIn(ctx, codexIssuer, codexClientId, codexTokenUrl, nil)
	}
	return nil, fmt.Errorf("llm: %q is not a provider kind that signs in", kind)
}

func beginDeviceSignIn(ctx context.Context, issuer, clientId, tokenUrl string, client *http.Client) (*DeviceSignIn, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	var answer struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     any    `json:"interval"`
		ExpiresAt    string `json:"expires_at"`
	}
	status, err := postDeviceJSON(ctx, client, issuer+"/api/accounts/deviceauth/usercode", map[string]string{"client_id": clientId}, &answer)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, &SignInError{Status: status, Said: "the service would not give a code; the account may not allow signing in with a code from another device"}
	}
	if answer.DeviceAuthID == "" || answer.UserCode == "" {
		return nil, errors.New("llm: the service gave no code")
	}
	started := &DeviceSignIn{
		UserCode:            answer.UserCode,
		VerificationAddress: issuer + "/codex/device",
		ExpiresAt:           time.Now().Add(15 * time.Minute),
		issuer:              issuer,
		clientId:            clientId,
		tokenUrl:            tokenUrl,
		deviceAuthId:        answer.DeviceAuthID,
		interval:            deviceSignInInterval,
		http:                client,
	}
	if when, err := time.Parse(time.RFC3339Nano, answer.ExpiresAt); err == nil {
		started.ExpiresAt = when
	}
	// The interval comes as a number or as a number written as text.
	switch interval := answer.Interval.(type) {
	case float64:
		if interval > 0 {
			started.interval = time.Duration(interval * float64(time.Second))
		}
	case string:
		if seconds, err := strconv.Atoi(strings.TrimSpace(interval)); err == nil && seconds > 0 {
			started.interval = time.Duration(seconds) * time.Second
		}
	}
	return started, nil
}

// Wait asks until the code is entered and then redeems it. It answers
// ErrSignInPending when the context ends first, so that a caller can wait
// a little at a time.
func (self *DeviceSignIn) Wait(ctx context.Context) (*SignInResult, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.result != nil {
		return self.result, nil
	}
	for {
		if time.Now().After(self.ExpiresAt) {
			return nil, errors.New("llm: the code has expired; start the sign-in again")
		}
		code, verifier, err := self.poll(ctx)
		if err == nil {
			result, err := redeemCode(ctx, self.tokenUrl, self.clientId, code, self.issuer+"/deviceauth/callback", verifier)
			if err != nil {
				return nil, err
			}
			self.result = result
			return result, nil
		}
		if !errors.Is(err, ErrSignInPending) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ErrSignInPending
		case <-time.After(self.interval):
		}
	}
}

// poll asks once whether the code has been entered.
func (self *DeviceSignIn) poll(ctx context.Context) (code, verifier string, err error) {
	var answer struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
		Error             struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	status, err := postDeviceJSON(ctx, self.http, self.issuer+"/api/accounts/deviceauth/token",
		map[string]string{"device_auth_id": self.deviceAuthId, "user_code": self.UserCode}, &answer)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ErrSignInPending
		}
		return "", "", err
	}
	if status/100 == 2 && answer.AuthorizationCode != "" {
		return answer.AuthorizationCode, answer.CodeVerifier, nil
	}
	// Not yet entered is a refusal that says so, or a plain not found.
	if strings.HasSuffix(answer.Error.Code, "_pending") || status == http.StatusNotFound || (status == http.StatusForbidden && answer.Error.Code == "") {
		return "", "", ErrSignInPending
	}
	said := strings.TrimSpace(answer.Error.Message)
	if said == "" {
		said = http.StatusText(status)
	}
	return "", "", &SignInError{Status: status, Said: said}
}

// postDeviceJSON posts a JSON body and reads a JSON answer, whatever the
// status: the service explains a refusal in the body.
func postDeviceJSON(ctx context.Context, client *http.Client, address string, body any, answer any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("llm: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encoded))
	if err != nil {
		return 0, fmt.Errorf("llm: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("llm: cannot reach the sign-in: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_ = json.NewDecoder(response.Body).Decode(answer)
	return response.StatusCode, nil
}
