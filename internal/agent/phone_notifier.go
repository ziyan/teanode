package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// PhoneNotifierSettings is what a PhoneNotifier needs: the database that
// holds the phones, the configuration that says how to reach the relay, and
// the lifetime of the notifications it sends in the background.
type PhoneNotifierSettings struct {
	Database db.Database

	// Configuration is read for every notification, so a change to the push
	// settings applies without a restart.
	Configuration func() *config.Configuration

	// Context ends the notifications still being sent; WaitGroup counts them,
	// so that whoever owns the notifier can wait for them to finish.
	Context   context.Context
	WaitGroup *sync.WaitGroup

	// Sender replaces the relay, for a test that stands in for it. Nil is the
	// relay the configuration names.
	Sender push.Sender
}

// PhoneNotifier sends a notification to a person's phones, encrypted for each
// one, through this server's own relay or the relay over HTTP. It needs a
// database and a configuration and nothing else, so a server with no model,
// and so no agent, sends the phones what happens in a chat the way the agent
// sends what it says.
type PhoneNotifier struct {
	settings *PhoneNotifierSettings

	mutex       sync.Mutex
	sender      push.Sender
	fingerprint string

	// override replaces the sender in a test, which stands in for the relay.
	override push.Sender

	// placeVisitNotices remembers which arrivals and departures were told
	// lately, so that a phone flickering at the edge of a place is not told
	// again and again (location_notify.go).
	placeVisitNotices placeVisitNotices
}

// NewPhoneNotifier is a notifier that builds its sender when the first
// notification is sent.
func NewPhoneNotifier(settings *PhoneNotifierSettings) *PhoneNotifier {
	return &PhoneNotifier{settings: settings, override: settings.Sender}
}

// Sender is how this server sends to phones now, or nil while notifications
// are off.
func (self *PhoneNotifier) Sender() (push.Sender, error) {
	configuration := self.settings.Configuration()
	settings := configuration.Agent.Push
	if !settings.IsEnabled() {
		return nil, nil
	}
	relay := settings.Relay
	isLocal := relay.Enabled && relay.HasKey()
	sum := sha256.Sum256([]byte(fmt.Sprintf("%t\x00%s\x00%s\x00%s\x00%s\x00%s", isLocal, settings.ResolvedRelayURL(),
		relay.APNSKeyID, relay.APNSTeamID, relay.ResolvedTopic(), relay.APNSKey)))
	fingerprint := hex.EncodeToString(sum[:])

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.override != nil {
		return self.override, nil
	}
	if self.sender != nil && self.fingerprint == fingerprint {
		return self.sender, nil
	}
	if isLocal {
		apns, err := push.NewAPNs(relay.APNSKeyID, relay.APNSTeamID, relay.ResolvedTopic(), relay.APNSKey)
		if err != nil {
			return nil, err
		}
		relay, err := push.NewRelay(apns)
		if err != nil {
			return nil, err
		}
		self.sender = relay
	} else {
		self.sender = push.NewRelayClient(settings.ResolvedRelayURL())
	}
	self.fingerprint = fingerprint
	return self.sender, nil
}

// NotifyPhone sends one notification to one phone and records how it went.
func (self *PhoneNotifier) NotifyPhone(ctx context.Context, phone *models.AgentPhone, content *push.Content) error {
	sender, err := self.Sender()
	if err != nil {
		return err
	}
	if sender == nil {
		return fmt.Errorf("agent: notifications to phones are off on this server")
	}
	if !phone.CanBeNotified() {
		return fmt.Errorf("agent: the phone has not registered for notifications; open the TeaNode app on it")
	}
	opened, err := OpenConnectionSecret(self.settings.Configuration(), phone.PayloadKey)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(opened)
	if err != nil {
		return fmt.Errorf("agent: the phone's notification key cannot be read: %w", err)
	}
	delivery, err := push.EncryptContent(key, content)
	if err != nil {
		return err
	}
	sendContext, cancel := context.WithTimeout(ctx, phonePushTimeout)
	defer cancel()
	sendError := sender.Send(sendContext, phone.PushRegistration, delivery)

	isGone := errors.Is(sendError, push.ErrGone)
	errorText := ""
	if sendError != nil {
		errorText = sendError.Error()
	}
	if err := self.settings.Database.TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) error {
		return tx.NoteAgentPhoneDelivery(phone.ID, time.Now(), errorText, isGone)
	}); err != nil {
		log.Warningf("cannot record a notification to phone %s: %s", phone.ID, err)
	}
	return sendError
}

// NotifyPhones sends a notification to each of the person's phones that
// wants its kind, in the background: nothing that raises one waits on Apple.
func (self *PhoneNotifier) NotifyPhones(agentId, notificationKind string, content *push.Content) {
	if sender, err := self.Sender(); err != nil || sender == nil {
		return
	}
	var phones []*models.AgentPhone
	if err := self.settings.Database.TransactionContext(self.settings.Context, func(tx db.Transaction) (err error) {
		phones, err = tx.ListAgentPhones(agentId)
		return err
	}); err != nil {
		log.Warningf("cannot list the phones to notify: %s", err)
		return
	}
	for _, phone := range phones {
		if !phone.Wants(notificationKind) || !phone.CanBeNotified() {
			continue
		}
		self.settings.WaitGroup.Add(1)
		go func(phone *models.AgentPhone) {
			defer self.settings.WaitGroup.Done()
			if err := self.NotifyPhone(self.settings.Context, phone, content); err != nil {
				log.Noticef("a notification did not reach phone %s: %s", phone.ID, err)
			}
		}(phone)
	}
}

// OpenConnectionSecret opens a credential that Agent.SealSecret sealed with
// the server secret, for whoever has the configuration and no agent.
func OpenConnectionSecret(configuration *config.Configuration, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	box, err := connectionBox(configuration)
	if err != nil {
		return "", err
	}
	opened, err := box.Open(value)
	if err != nil {
		return "", err
	}
	return string(opened), nil
}

func connectionBox(configuration *config.Configuration) (*secretbox.Box, error) {
	secret := configuration.Secret()
	if len(secret) == 0 {
		return nil, fmt.Errorf("agent: the server has no secret to seal credentials with")
	}
	return secretbox.New(secret, connectionSecretLabel)
}

// SealConnectionSecret seals a credential with the server secret, for
// storing; OpenConnectionSecret opens it. Agent.SealSecret is it for the
// agent, and a phone's notification key is sealed with it on a server with no
// agent.
func SealConnectionSecret(configuration *config.Configuration, value string) (string, error) {
	box, err := connectionBox(configuration)
	if err != nil {
		return "", err
	}
	return box.Seal([]byte(value))
}

// PhoneBody is text as a notification shows it: plain, and short enough that
// a lock screen is not the place it is read in full. A notification is
// plain text, so the suggested replies and any other hidden marker, a
// link's target and the Markdown marks are taken off first.
func PhoneBody(text string) string {
	text = strings.Join(strings.Fields(models.PlainText(text)), " ")
	runes := []rune(text)
	if len(runes) > 600 {
		return string(runes[:600]) + "…"
	}
	return text
}
