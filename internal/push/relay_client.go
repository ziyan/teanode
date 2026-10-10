package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// Paths of the relay's two endpoints, under the relay's address.
const (
	PathRegistrations = api.PathPushRegistrations
	PathNotifications = api.PathPushNotifications
)

// RegisterRequest is what a phone sends the relay to register.
type RegisterRequest struct {
	DeviceToken string `json:"deviceToken"`
	IsSandbox   bool   `json:"isSandbox"`
}

// RegisterResponse is the sealed registration the phone hands its server.
type RegisterResponse struct {
	PushRegistration string `json:"pushRegistration"`
}

// DeliverRequest is what a server sends the relay for one notification.
type DeliverRequest struct {
	PushRegistration string    `json:"pushRegistration"`
	Delivery         *Delivery `json:"delivery"`
}

// ErrorResponse is what the relay answers when it does not deliver.
type ErrorResponse struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

// Error codes the relay answers with, which a sender maps back to errors.
const (
	ErrorCodeGone      = "gone"
	ErrorCodeTooLarge  = "too_large"
	ErrorCodeRefused   = "refused"
	ErrorCodeLimited   = "limited"
	ErrorCodeUnreached = "unreached"
)

// RelayClient sends notifications to a relay over HTTP.
type RelayClient struct {
	baseURL string
	http    *http.Client
}

// NewRelayClient sends to the relay at baseURL, such as
// https://mail.teanode.com.
func NewRelayClient(baseURL string) *RelayClient {
	return &RelayClient{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

// Send asks the relay to deliver a notification.
func (self *RelayClient) Send(ctx context.Context, pushRegistration string, delivery *Delivery) error {
	body, err := json.Marshal(&DeliverRequest{PushRegistration: pushRegistration, Delivery: delivery})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.baseURL+PathNotifications, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := self.http.Do(request)
	if err != nil {
		return fmt.Errorf("push: the relay at %s could not be reached: %w", self.baseURL, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var refusal ErrorResponse
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	_ = json.Unmarshal(answer, &refusal)
	switch refusal.ErrorCode {
	case ErrorCodeGone:
		return fmt.Errorf("%w: %s", ErrGone, refusal.ErrorMessage)
	case ErrorCodeTooLarge:
		return ErrTooLarge
	case ErrorCodeRefused:
		return fmt.Errorf("%w: %s", ErrRefused, refusal.ErrorMessage)
	}
	return fmt.Errorf("push: the relay answered %d: %s", response.StatusCode, refusal.ErrorMessage)
}
