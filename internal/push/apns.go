// Package push delivers notifications to people's phones through Apple's
// push service (APNs), and seals what a phone hands its server so that only
// the relay that sealed it can use it.
//
// A TeaNode server never talks to Apple for its own phones unless it is the
// relay: Apple takes notifications for an app only from a key of the
// developer account that publishes it. The relay (internal/api/v1api/apipush)
// holds that key; every other server sends to the relay
// (relay_client.go), and what it sends is already encrypted for the phone
// (payload.go).
package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The two APNs hosts: production for App Store and TestFlight builds, the
// sandbox for builds installed from Xcode.
const (
	ProductionHost = "https://api.push.apple.com"
	SandboxHost    = "https://api.sandbox.push.apple.com"
)

// Push types: an ordinary notification, or an update to a Live Activity on
// the lock screen.
const (
	PushTypeAlert        = "alert"
	PushTypeLiveActivity = "liveactivity"
)

// payloadBytesMost is the largest payload APNs accepts for an alert.
const payloadBytesMost = 4096

// tokenLifetime is how long one signed authentication token is used. Apple
// refuses one older than an hour and also one replaced too often, so it is
// kept for most of the hour.
const tokenLifetime = 50 * time.Minute

// What a send can come to besides success. A caller acts on these: a gone
// device is deleted, the others are retried later or reported.
var (
	// ErrGone is APNs saying the device token is no longer valid: the app
	// was deleted, or the phone gave the app a new token.
	ErrGone = errors.New("push: the phone is no longer registered for notifications")

	// ErrTooLarge is a payload over what APNs takes.
	ErrTooLarge = errors.New("push: the notification is too large")

	// ErrRefused is APNs refusing the request for a reason a retry will not
	// change, such as a topic the key does not cover.
	ErrRefused = errors.New("push: Apple refused the notification")
)

// Notification is what is delivered: the payload APNs passes to the phone,
// and how APNs should treat it.
type Notification struct {
	// PushType is PushTypeAlert or PushTypeLiveActivity.
	PushType string `json:"pushType"`

	// Priority is 10 to deliver now, 5 to let the phone save power.
	Priority int `json:"priority,omitempty"`

	// CollapseID makes a newer notification replace an older one with the
	// same identifier, such as a coding session's latest state.
	CollapseID string `json:"collapseId,omitempty"`

	// ExpiresAt is when APNs should give up on a phone it cannot reach.
	// Zero delivers once or not at all.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`

	// Payload is the JSON APNs delivers: the "aps" dictionary and anything
	// beside it.
	Payload json.RawMessage `json:"payload"`
}

// APNs sends notifications to Apple with a token-based authentication key.
type APNs struct {
	keyID  string
	teamID string
	topic  string
	key    *ecdsa.PrivateKey
	http   *http.Client

	// Hosts are where production and sandbox requests go: Apple's, except
	// in tests.
	productionHost string
	sandboxHost    string

	mutex    sync.Mutex
	token    string
	signedAt time.Time
}

// NewAPNs builds a client from an APNs authentication key: its identifier,
// the team it belongs to, the app's bundle identifier, and the .p8 file.
func NewAPNs(keyID, teamID, topic, keyPEM string) (*APNs, error) {
	key, err := ParseKey(keyPEM)
	if err != nil {
		return nil, err
	}
	if keyID == "" || teamID == "" || topic == "" {
		return nil, fmt.Errorf("push: an APNs key needs its key ID, team ID and topic")
	}
	return &APNs{
		keyID:  keyID,
		teamID: teamID,
		topic:  topic,
		key:    key,
		// HTTP/2, which APNs requires, is what Go's client negotiates over
		// TLS on its own. One client keeps the connection open between
		// notifications, as Apple asks.
		http:           &http.Client{Timeout: 30 * time.Second},
		productionHost: ProductionHost,
		sandboxHost:    SandboxHost,
	}, nil
}

// UseHosts sends through client to the given hosts instead of Apple's: for a
// test standing in a server of its own.
func (self *APNs) UseHosts(client *http.Client, productionHost, sandboxHost string) {
	self.http, self.productionHost, self.sandboxHost = client, productionHost, sandboxHost
}

// ParseKey reads an APNs authentication key, a PKCS #8 P-256 key: the .p8
// file Apple hands out, or the base64 between its first and last lines on
// one line, which is what a secret read from a pipe can carry.
func ParseKey(keyPEM string) (*ecdsa.PrivateKey, error) {
	var der []byte
	if block, _ := pem.Decode([]byte(keyPEM)); block != nil {
		der = block.Bytes
	} else if decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(keyPEM), "")); err == nil {
		der = decoded
	} else {
		return nil, fmt.Errorf("push: the APNs key is neither the .p8 file nor its base64")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("push: the APNs key cannot be read: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("push: the APNs key is not an elliptic curve key")
	}
	return key, nil
}

// Send delivers one notification to one device.
func (self *APNs) Send(ctx context.Context, deviceToken string, isSandbox bool, notification *Notification) error {
	if len(notification.Payload) > payloadBytesMost {
		return ErrTooLarge
	}
	host := self.productionHost
	if isSandbox {
		host = self.sandboxHost
	}
	token, err := self.authenticationToken(time.Now())
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+deviceToken, bytes.NewReader(notification.Payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "bearer "+token)
	request.Header.Set("apns-push-type", notification.PushType)
	topic := self.topic
	if notification.PushType == PushTypeLiveActivity {
		topic += ".push-type.liveactivity"
	}
	request.Header.Set("apns-topic", topic)
	// Apple limits how many urgent Live Activity updates an app sends, so
	// those default to the power-saving priority.
	priority := notification.Priority
	if priority == 0 {
		priority = 10
		if notification.PushType == PushTypeLiveActivity {
			priority = 5
		}
	}
	request.Header.Set("apns-priority", strconv.Itoa(priority))
	expiration := int64(0)
	if !notification.ExpiresAt.IsZero() {
		expiration = notification.ExpiresAt.Unix()
	}
	request.Header.Set("apns-expiration", strconv.FormatInt(expiration, 10))
	if notification.CollapseID != "" {
		request.Header.Set("apns-collapse-id", notification.CollapseID)
	}

	response, err := self.http.Do(request)
	if err != nil {
		// Without the address: it ends in the device token, and this error
		// travels to logs, to the server that asked and to the dashboard.
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("push: Apple could not be reached: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var refusal struct {
		Reason string `json:"reason"`
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	_ = json.Unmarshal(body, &refusal)
	switch response.StatusCode {
	case http.StatusGone:
		return ErrGone
	case http.StatusBadRequest:
		if refusal.Reason == "BadDeviceToken" || refusal.Reason == "DeviceTokenNotForTopic" {
			return fmt.Errorf("%w (%s)", ErrGone, refusal.Reason)
		}
	case http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case http.StatusForbidden:
		// An expired authentication token is made again for the next send.
		// Only that one: Apple refuses a token replaced too often, so making
		// one for every refusal, such as a key that does not cover the
		// topic, would turn one misconfiguration into two.
		if refusal.Reason == "ExpiredProviderToken" {
			self.mutex.Lock()
			self.token = ""
			self.mutex.Unlock()
		}
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return fmt.Errorf("push: Apple answered %d %s; try again later", response.StatusCode, refusal.Reason)
	}
	return fmt.Errorf("%w: %d %s", ErrRefused, response.StatusCode, refusal.Reason)
}

// authenticationToken is the signed JSON Web Token APNs authenticates a
// request with: ES256 over the key ID, the team and the time it was signed.
// The standard library's ECDSA does the signing; nothing else here needs a
// JWT library.
func (self *APNs) authenticationToken(now time.Time) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.token != "" && now.Sub(self.signedAt) < tokenLifetime {
		return self.token, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": self.keyID})
	claims, _ := json.Marshal(map[string]any{"iss": self.teamID, "iat": now.Unix()})
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	signatureR, signatureS, err := ecdsa.Sign(rand.Reader, self.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("push: the APNs token could not be signed: %w", err)
	}
	// A JWS ES256 signature is r and s, each 32 bytes, side by side.
	signature := make([]byte, 64)
	signatureR.FillBytes(signature[:32])
	signatureS.FillBytes(signature[32:])
	self.token = signing + "." + base64.RawURLEncoding.EncodeToString(signature)
	self.signedAt = now
	return self.token, nil
}
