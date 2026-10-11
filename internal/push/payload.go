package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
)

// What a notification says, encrypted for the one phone it goes to.
//
// Neither Apple nor the relay may read a notification: what an alert says is
// somebody's mail. A phone makes a 256-bit key when it registers and gives it
// to its own server, which keeps it sealed. The server encrypts each
// notification's content with that key (AES-256-GCM, the nonce in front of
// the ciphertext and the tag behind it, which is CryptoKit's "combined" form
// on the phone), and the app's notification service extension decrypts it
// before iOS shows anything.

// PhoneKeyBytes is the length of a phone's payload key.
const PhoneKeyBytes = 32

// Content is a notification as the phone shows it, before encryption.
type Content struct {
	// NotificationKind is what this is: alert, herdr_question,
	// goal_needs_you, speak_first, approval, question, answer or test.
	NotificationKind string `json:"notificationKind"`

	ConversationID string `json:"conversationId,omitempty"`
	RunID          string `json:"runId,omitempty"`
	CallID         string `json:"callId,omitempty"`

	Title string `json:"title"`
	Body  string `json:"body"`

	// Buttons are what can be pressed on the notification itself.
	Buttons []Button `json:"buttons,omitempty"`

	// ThreadID groups notifications on the lock screen.
	ThreadID string `json:"threadId,omitempty"`

	// For a coding session's question: where to send the answer.
	Computer            string `json:"computer,omitempty"`
	PaneID              string `json:"paneId,omitempty"`
	QuestionFingerprint string `json:"questionFingerprint,omitempty"`
}

// Button is one action on a notification.
type Button struct {
	Label string `json:"label"`

	// ButtonAction is what pressing it does: approve, deny, answer:<option
	// number>, reply or open.
	ButtonAction string `json:"buttonAction"`
}

// Delivery is a notification as a server hands it to the relay: the content
// already encrypted for the phone, and the little Apple needs to route it.
// The relay writes everything Apple shows itself (relay.go), so a server
// holding a registration can make the phone show only an encrypted
// notification from the app, never words of its own.
type Delivery struct {
	// Sealed is the content, encrypted for the phone.
	Sealed string `json:"sealed"`

	// ThreadID groups notifications on the lock screen, and CollapseID makes
	// a newer notification replace an older one. Both are hashed with the
	// phone's key, so Apple and the relay never see a conversation or a
	// card's identifier.
	ThreadID   string `json:"threadId,omitempty"`
	CollapseID string `json:"collapseId,omitempty"`

	// ExpiresAt is when Apple gives up on a phone it cannot reach; zero is
	// deliveryLifetime from now.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`

	// ActivityState is, for a Live Activity, the one word it shows: one of
	// activityStates. A Live Activity's delivery carries nothing else.
	ActivityState string `json:"activityState,omitempty"`

	// IsActivityEnded ends the Live Activity, showing ActivityState.
	IsActivityEnded bool `json:"isActivityEnded,omitempty"`

	// ActivityChangedAt is when the turn came to ActivityState, on the
	// server's clock. iOS shows the update with the latest aps.timestamp, so
	// it is what orders a turn's states; the relay's own clock would order
	// them by when each reached it.
	ActivityChangedAt time.Time `json:"activityChangedAt,omitzero"`
}

// activityStates are what a Live Activity may be told: the turn is working,
// waits for the person, is done, or failed.
var activityStates = map[string]bool{"working": true, "waiting": true, "done": true, "failed": true}

// activityDismissal is how long an ended Live Activity stays on the lock
// screen, so the person sees how it ended.
const activityDismissal = 15 * time.Minute

// activityChangedAtAgeMost and activityChangedAtSkewMost bound how far a
// server's ActivityChangedAt may be from the relay's clock before the relay
// uses its own instead: a server with a wrong clock, or one that writes a
// time in the future, must not put its updates ahead of every later one.
const (
	activityChangedAtAgeMost  = 5 * time.Minute
	activityChangedAtSkewMost = 30 * time.Second
)

// activityTimestamp is the aps.timestamp of a Live Activity's update: when
// the server says the state changed, when that is believable, and now
// otherwise.
func (self *Delivery) activityTimestamp(now time.Time) int64 {
	changedAt := self.ActivityChangedAt
	if changedAt.IsZero() || changedAt.Before(now.Add(-activityChangedAtAgeMost)) || changedAt.After(now.Add(activityChangedAtSkewMost)) {
		changedAt = now
	}
	return changedAt.Unix()
}

const (
	// deliveryLifetime is how long Apple keeps trying a phone that is off,
	// unless the server says otherwise: a card is still worth seeing an hour
	// later, and a newer one replaces it.
	deliveryLifetime = time.Hour

	// deliveryLifetimeMost is the longest a server may ask for.
	deliveryLifetimeMost = 24 * time.Hour
)

// envelopeIdPattern is a thread or collapse identifier as EncryptContent
// makes one.
var envelopeIdPattern = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

// notification is the delivery as Apple takes it, with the payload written
// here rather than by the server that asked.
func (self *Delivery) notification(now time.Time) (*Notification, error) {
	if self.ActivityState != "" || self.IsActivityEnded || !self.ActivityChangedAt.IsZero() {
		return nil, fmt.Errorf("%w: a state is for a Live Activity, and this registration is the phone's", ErrRefused)
	}
	if self.Sealed == "" {
		return nil, fmt.Errorf("%w: the notification has no content", ErrRefused)
	}
	if _, err := base64.StdEncoding.Strict().DecodeString(self.Sealed); err != nil {
		return nil, fmt.Errorf("%w: the notification's content is not sealed", ErrRefused)
	}
	for _, identifier := range []string{self.ThreadID, self.CollapseID} {
		if identifier != "" && !envelopeIdPattern.MatchString(identifier) {
			return nil, fmt.Errorf("%w: a thread or collapse identifier is up to 64 hex digits", ErrRefused)
		}
	}
	expiresAt := self.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(deliveryLifetime)
	}
	if expiresAt.Before(now) {
		return nil, fmt.Errorf("%w: the notification has expired", ErrRefused)
	}
	if expiresAt.After(now.Add(deliveryLifetimeMost)) {
		expiresAt = now.Add(deliveryLifetimeMost)
	}
	payload, err := alertPayload(self.Sealed, self.ThreadID)
	if err != nil {
		return nil, err
	}
	if len(payload) > payloadBytesMost {
		return nil, ErrTooLarge
	}
	return &Notification{
		PushType: PushTypeAlert, Priority: 10, CollapseID: self.CollapseID, ExpiresAt: expiresAt, Payload: payload,
	}, nil
}

// activityNotification is the delivery as Apple takes it for a Live
// Activity: written here from the state alone.
func (self *Delivery) activityNotification(now time.Time) (*Notification, error) {
	if self.Sealed != "" || self.ThreadID != "" || self.CollapseID != "" {
		return nil, fmt.Errorf("%w: a Live Activity is told a state and nothing else", ErrRefused)
	}
	if !activityStates[self.ActivityState] {
		return nil, fmt.Errorf("%w: %q is not a state a Live Activity shows", ErrRefused, self.ActivityState)
	}
	aps := map[string]any{
		"timestamp":     self.activityTimestamp(now),
		"event":         "update",
		"content-state": map[string]string{"activityState": self.ActivityState},
	}
	if self.IsActivityEnded {
		aps["event"] = "end"
		aps["dismissal-date"] = now.Add(activityDismissal).Unix()
	}
	payload, err := json.Marshal(map[string]any{"aps": aps})
	if err != nil {
		return nil, err
	}
	return &Notification{PushType: PushTypeLiveActivity, ExpiresAt: now.Add(deliveryLifetime), Payload: payload}, nil
}

// alertPayload is what Apple carries: in the open only "New message", in case
// the phone cannot decrypt, and the sealed content for the app's notification
// service extension to decrypt before iOS shows anything.
func alertPayload(sealed, threadId string) ([]byte, error) {
	aps := map[string]any{
		"alert":           map[string]string{"title": "TeaNode", "body": "New message"},
		"mutable-content": 1,
		"sound":           "default",
	}
	if threadId != "" {
		aps["thread-id"] = threadId
	}
	return json.Marshal(map[string]any{"aps": aps, "sealed": sealed})
}

// envelopeId hashes an identifier with the phone's key: the same for the
// same conversation on the same phone, meaningless to anybody else.
func envelopeId(key []byte, label, value string) string {
	if value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(label + "\x00" + value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// EncryptContent seals content for the phone holding key, shortening the body
// when the payload the relay will make of it would be over what APNs takes.
func EncryptContent(key []byte, content *Content) (*Delivery, error) {
	if len(key) != PhoneKeyBytes {
		return nil, fmt.Errorf("push: a phone's key is %d bytes, not %d", len(key), PhoneKeyBytes)
	}
	threadId := envelopeId(key, "thread", content.ThreadID)
	collapseId := envelopeId(key, "card", content.CallID)
	// Base64 makes the ciphertext a third larger, and the "aps" dictionary
	// around it takes some. A body too long for what is left is cut to the
	// most characters that fit, found by halving, since a character is one
	// byte or four depending on the language.
	wrap := func(body string) (*Delivery, int, error) {
		withBody := *content
		withBody.Body = body
		plain, err := json.Marshal(&withBody)
		if err != nil {
			return nil, 0, err
		}
		sealed, err := seal(key, plain)
		if err != nil {
			return nil, 0, err
		}
		payload, err := alertPayload(sealed, threadId)
		if err != nil {
			return nil, 0, err
		}
		return &Delivery{Sealed: sealed, ThreadID: threadId, CollapseID: collapseId}, len(payload), nil
	}
	whole, size, err := wrap(content.Body)
	if err != nil || size <= payloadBytesMost {
		return whole, err
	}
	runes := []rune(content.Body)
	fitting, low, high := (*Delivery)(nil), 0, len(runes)
	for low < high {
		middle := (low + high + 1) / 2
		tried, size, err := wrap(string(runes[:middle]) + "…")
		if err != nil {
			return nil, err
		}
		if size <= payloadBytesMost {
			fitting, low = tried, middle
		} else {
			high = middle - 1
		}
	}
	if fitting == nil {
		return nil, ErrTooLarge
	}
	return fitting, nil
}

func seal(key, plain []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, nil)), nil
}

// DecryptContent reverses EncryptContent's sealing, for tests and for anybody
// checking what a phone will see.
func DecryptContent(key []byte, sealed string) (*Content, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < aead.NonceSize() {
		return nil, fmt.Errorf("push: the sealed content is too short")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return nil, err
	}
	var content Content
	return &content, json.Unmarshal(plain, &content)
}
