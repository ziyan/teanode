package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
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

// Firebase Cloud Messaging, the way Android phones are reached: the relay's
// counterpart of apns.go. The relay holds a Firebase project's service
// account, signs a short-lived Google access token with its key, and posts
// each notification to the HTTP v1 API. Nothing here needs Google's client
// libraries: the signing is the standard library's RSA, as the APNs token is
// its ECDSA.

// FCMMessagesHost is where FCM takes messages; the project is in the path.
const FCMMessagesHost = "https://fcm.googleapis.com"

// fcmScope is the one permission the relay asks Google for.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// fcmTokenURI is where a service account's JSON says to trade a signed
// assertion for an access token, unless the file says otherwise.
const fcmTokenURI = "https://oauth2.googleapis.com/token"

// accessTokenLifetime is how long an access token is asked to be good for,
// which is the longest Google allows.
const accessTokenLifetime = time.Hour

// accessTokenRenewBefore is how long before an access token expires the next
// send trades for a new one, so that a send never starts with a token that
// runs out on the way.
const accessTokenRenewBefore = 5 * time.Minute

// FCMMessage is what is delivered to an Android phone: data only, so that the
// app is woken to draw the notification itself after decrypting it, and how
// FCM should treat it.
type FCMMessage struct {
	// Data is what the app receives. Every value is a string, as FCM
	// requires.
	Data map[string]string `json:"data"`

	// IsHighPriority wakes a phone that is dozing, as a chat message or a
	// ring needs. Android lowers a high priority message's app when it does
	// not show a notification for it, so only content the app shows is sent
	// this way.
	IsHighPriority bool `json:"isHighPriority"`

	// CollapseKey makes a newer message replace an older one that has not
	// been delivered yet.
	CollapseKey string `json:"collapseKey,omitempty"`

	// TimeToLive is how long FCM keeps trying a phone it cannot reach.
	TimeToLive time.Duration `json:"timeToLive"`
}

// Data keys of the message the app receives. The values of PushKind tell the
// app what to do with it before it decrypts anything: show a notification, or
// ring for an incoming call with a full-screen intent.
const (
	FCMDataSealed   = "sealed"
	FCMDataPushKind = "pushKind"
	FCMDataThreadID = "threadId"

	FCMPushKindNotification = "notification"
	FCMPushKindCall         = "call"
)

// serviceAccount is the part of a Google service account's JSON file the
// relay uses.
type serviceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// FCM sends messages to Android phones through Firebase Cloud Messaging.
type FCM struct {
	account serviceAccount
	key     *rsa.PrivateKey
	http    *http.Client

	// messagesHost is where messages go: Google's, except in tests.
	messagesHost string

	mutex             sync.Mutex
	accessToken       string
	accessTokenExpiry time.Time
}

// NewFCM builds a client from a service account's JSON file, the one the
// Firebase console hands out under Project settings, Service accounts.
func NewFCM(serviceAccountJSON string) (*FCM, error) {
	var account serviceAccount
	if err := json.Unmarshal([]byte(serviceAccountJSON), &account); err != nil {
		return nil, fmt.Errorf("push: the FCM service account is not a JSON file: %w", err)
	}
	if account.Type != "service_account" || account.ProjectID == "" || account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("push: the FCM service account needs type service_account, project_id, client_email and private_key")
	}
	if account.PrivateKeyID == "" || strings.Contains(account.PrivateKeyID, ".") {
		return nil, fmt.Errorf("push: the FCM service account needs a private_key_id")
	}
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("push: the FCM service account's private_key is not a PEM key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("push: the FCM service account's private_key cannot be read: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("push: the FCM service account's private_key is not an RSA key")
	}
	if account.TokenURI == "" {
		account.TokenURI = fcmTokenURI
	}
	return &FCM{
		account:      account,
		key:          key,
		http:         &http.Client{Timeout: 30 * time.Second},
		messagesHost: FCMMessagesHost,
	}, nil
}

// UseHost sends through client to host instead of Google's: for a test
// standing in a server of its own. The service account's token_uri is the
// other address, and a test puts its own in the file.
func (self *FCM) UseHost(client *http.Client, messagesHost string) {
	self.http, self.messagesHost = client, messagesHost
}

// keyID is what is written in front of a registration sealed under this
// service account's key when there is no APNs key to seal with.
func (self *FCM) keyID() string {
	return self.account.PrivateKeyID
}

// secret is the bytes registrations are sealed under when there is no APNs
// key to seal with: the service account's private key.
func (self *FCM) secret() []byte {
	return x509.MarshalPKCS1PrivateKey(self.key)
}

// Send delivers one message to one Android phone.
func (self *FCM) Send(ctx context.Context, deviceToken string, message *FCMMessage) error {
	if fcmDataBytes(message.Data) > payloadBytesMost {
		return ErrTooLarge
	}
	accessToken, err := self.accessTokenAt(ctx, time.Now())
	if err != nil {
		return err
	}
	android := map[string]any{"priority": "NORMAL"}
	if message.IsHighPriority {
		android["priority"] = "HIGH"
	}
	if message.CollapseKey != "" {
		android["collapse_key"] = message.CollapseKey
	}
	if seconds := int64(message.TimeToLive / time.Second); seconds > 0 {
		android["ttl"] = strconv.FormatInt(seconds, 10) + "s"
	}
	body, err := json.Marshal(map[string]any{"message": map[string]any{
		"token": deviceToken, "data": message.Data, "android": android,
	}})
	if err != nil {
		return err
	}
	address := self.messagesHost + "/v1/projects/" + url.PathEscape(self.account.ProjectID) + "/messages:send"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := self.http.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("push: Firebase could not be reached: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusOK {
		return nil
	}
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return self.sendFailure(response.StatusCode, answer)
}

// sendFailure maps an FCM refusal to what a caller acts on: ErrGone for a
// token that will never work again, ErrRefused for a request a retry will not
// change, and a plain error for what is worth trying again later.
func (self *FCM) sendFailure(statusCode int, answer []byte) error {
	var refusal struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(answer, &refusal)
	errorCode := refusal.Error.Status
	for _, detail := range refusal.Error.Details {
		if detail.ErrorCode != "" {
			errorCode = detail.ErrorCode
		}
	}
	switch errorCode {
	case "UNREGISTERED", "SENDER_ID_MISMATCH":
		// The app was uninstalled or cleared its data, or the token was made
		// for another Firebase project.
		return fmt.Errorf("%w (%s)", ErrGone, errorCode)
	case "INVALID_ARGUMENT":
		// The same code answers a bad token and a bad message; only the
		// words say which. The message is made here, so a refusal that does
		// not name the token is ours to see.
		if strings.Contains(strings.ToLower(refusal.Error.Message), "registration token") {
			return fmt.Errorf("%w (%s)", ErrGone, errorCode)
		}
	case "UNAUTHENTICATED", "THIRD_PARTY_AUTH_ERROR":
		// An access token Google no longer takes is traded for again.
		self.mutex.Lock()
		self.accessToken = ""
		self.mutex.Unlock()
		return fmt.Errorf("push: Firebase did not accept the relay's credential (%d %s); try again later", statusCode, errorCode)
	}
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return fmt.Errorf("push: Firebase answered %d %s; try again later", statusCode, errorCode)
	}
	return fmt.Errorf("%w: %d %s", ErrRefused, statusCode, errorCode)
}

// fcmDataBytes is what a message's data weighs against FCM's 4 KB limit:
// every key and value.
func fcmDataBytes(data map[string]string) int {
	total := 0
	for key, value := range data {
		total += len(key) + len(value)
	}
	return total
}

// accessTokenAt is the Google access token a request is authorized with,
// traded for with a signed assertion when there is none or it is about to
// expire.
func (self *FCM) accessTokenAt(ctx context.Context, now time.Time) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.accessToken != "" && now.Before(self.accessTokenExpiry.Add(-accessTokenRenewBefore)) {
		return self.accessToken, nil
	}
	assertion, err := self.signedAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := self.http.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return "", fmt.Errorf("push: Google could not be reached to authorize the relay: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	var granted struct {
		AccessToken      string `json:"access_token"`
		ExpiresInSeconds int64  `json:"expires_in"`
		Error            string `json:"error"`
	}
	_ = json.Unmarshal(body, &granted)
	if response.StatusCode != http.StatusOK || granted.AccessToken == "" {
		return "", fmt.Errorf("push: Google did not authorize the relay (%d %s)", response.StatusCode, granted.Error)
	}
	self.accessToken = granted.AccessToken
	self.accessTokenExpiry = now.Add(time.Duration(granted.ExpiresInSeconds) * time.Second)
	return self.accessToken, nil
}

// signedAssertion is the JSON Web Token Google trades for an access token:
// RS256 over the service account, the permission asked for and the hour it
// is asked for.
func (self *FCM) signedAssertion(now time.Time) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": self.account.PrivateKeyID})
	claims, _ := json.Marshal(map[string]any{
		"iss": self.account.ClientEmail, "scope": fcmScope, "aud": self.account.TokenURI,
		"iat": now.Unix(), "exp": now.Add(accessTokenLifetime).Unix(),
	})
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, self.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("push: the FCM assertion could not be signed: %w", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
