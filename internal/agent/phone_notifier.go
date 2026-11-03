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
	"sync/atomic"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/util/deferutil"
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
	if !phone.CanBeNotified() {
		return fmt.Errorf("agent: the phone has not registered for notifications; open the TeaNode app on it")
	}
	sendError := self.sendToPhone(ctx, phone, phone.PushRegistration, content)

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

// sendToPhone seals the content for the phone, in its person's language, and
// sends it through the relay to the sealed registration given: the phone's
// ordinary one, or the one it is rung through.
func (self *PhoneNotifier) sendToPhone(ctx context.Context, phone *models.AgentPhone, pushRegistration string, content *push.Content) error {
	sender, err := self.Sender()
	if err != nil {
		return err
	}
	if sender == nil {
		return fmt.Errorf("agent: notifications to phones are off on this server")
	}
	opened, err := OpenConnectionSecret(self.settings.Configuration(), phone.PayloadKey)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(opened)
	if err != nil {
		return fmt.Errorf("agent: the phone's notification key cannot be read: %w", err)
	}
	delivery, err := push.EncryptContent(key, content.ForLanguage(self.phoneLanguage(ctx, phone)))
	if err != nil {
		return err
	}
	sendContext, cancel := context.WithTimeout(ctx, phonePushTimeout)
	defer cancel()
	return sender.Send(sendContext, pushRegistration, delivery)
}

// RingPhone rings one phone for a call through its VoIP registration, which
// wakes the app to show the incoming call. A registration Apple says is gone
// is cleared, leaving the phone's ordinary one alone. It does not record a
// delivery: the phone's last delivery is about its notifications.
func (self *PhoneNotifier) RingPhone(ctx context.Context, phone *models.AgentPhone, content *push.Content) error {
	if !phone.CanRing() {
		return fmt.Errorf("agent: the phone has not registered to be rung; open the TeaNode app on it")
	}
	sendError := self.sendToPhone(ctx, phone, phone.VoIPPushRegistration, content)
	if errors.Is(sendError, push.ErrGone) {
		if err := self.settings.Database.TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) error {
			return tx.SetAgentPhoneVoIPPushRegistration(phone.ID, "")
		}); err != nil {
			log.Warningf("cannot clear the VoIP registration of phone %s: %s", phone.ID, err)
		}
	}
	return sendError
}

// phoneLanguage is the language a phone's notifications are written in:
// what its person's agent talks to them in (Language), so a person reading
// the app in Chinese is not told in English. English when it cannot be
// read.
func (self *PhoneNotifier) phoneLanguage(ctx context.Context, phone *models.AgentPhone) string {
	language := ""
	if err := self.settings.Database.TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) error {
		person, err := tx.GetAgent(phone.AgentID)
		if err != nil || person == nil {
			return err
		}
		user, err := tx.GetUser(person.UserID)
		if err != nil {
			return err
		}
		language = Language(person, user)
		return nil
	}); err != nil {
		log.Warningf("cannot read the language of phone %s, so it is told in English: %s", phone.ID, err)
	}
	return push.PhoneLanguage(language)
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

// RingPhones rings each of the person's phones for a call, and waits for
// the relay to answer for all of them, since the call wants to know whether
// anybody could be reached. content is the call as the phone shows it: the
// caller fills ConversationID and CallID, and the kind is NotificationCall.
//
// A phone that can ring is sent a VoIP push, which wakes the app. A phone
// that cannot (its app is too old to register one, or Apple said its VoIP
// token is gone), and one whose VoIP push failed, gets an ordinary alert
// with the same content, so it still shows "somebody is calling". The
// phone's own choice of notification kinds does not apply: whether to ring
// a member at all is for the caller to decide. The count is of phones
// reached either way.
func (self *PhoneNotifier) RingPhones(agentId string, content *push.Content) (rangCount int) {
	if sender, err := self.Sender(); err != nil || sender == nil {
		return 0
	}
	var phones []*models.AgentPhone
	if err := self.settings.Database.TransactionContext(self.settings.Context, func(tx db.Transaction) (err error) {
		phones, err = tx.ListAgentPhones(agentId)
		return err
	}); err != nil {
		log.Warningf("cannot list the phones to ring: %s", err)
		return 0
	}
	var waitGroup sync.WaitGroup
	var reachedCount atomic.Int64
	for _, phone := range phones {
		if !phone.CanRing() && !phone.CanBeNotified() {
			continue
		}
		waitGroup.Add(1)
		self.settings.WaitGroup.Add(1)
		go func(phone *models.AgentPhone) {
			defer self.settings.WaitGroup.Done()
			defer waitGroup.Done()
			if phone.CanRing() {
				err := self.RingPhone(self.settings.Context, phone, content)
				if err == nil {
					reachedCount.Add(1)
					return
				}
				log.Noticef("a call did not ring phone %s: %s", phone.ID, err)
			}
			if !phone.CanBeNotified() {
				return
			}
			if err := self.NotifyPhone(self.settings.Context, phone, content); err != nil {
				log.Noticef("a call notification did not reach phone %s: %s", phone.ID, err)
				return
			}
			reachedCount.Add(1)
		}(phone)
	}
	waitGroup.Wait()
	return int(reachedCount.Load())
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

// UnringPhones tells each of the person's phones that can be rung that a
// call no longer rings them, through the same VoIP registration, so that a
// phone still ringing stops. A phone that cannot be rung is told nothing: it
// was shown an alert, which a later alert would not take back. It returns
// without waiting for the relay.
func (self *PhoneNotifier) UnringPhones(agentId string, content *push.Content, exceptPhoneId string) {
	if sender, err := self.Sender(); err != nil || sender == nil {
		return
	}
	var phones []*models.AgentPhone
	if err := self.settings.Database.TransactionContext(self.settings.Context, func(tx db.Transaction) (err error) {
		phones, err = tx.ListAgentPhones(agentId)
		return err
	}); err != nil {
		log.Warningf("cannot list the phones to stop ringing: %s", err)
		return
	}
	for _, phone := range phones {
		// The phone that answered or declined knows already, and a VoIP push
		// it is sent must show a call, which would flash on its screen.
		if !phone.CanRing() || phone.ID == exceptPhoneId {
			continue
		}
		self.settings.WaitGroup.Add(1)
		go func(phone *models.AgentPhone) {
			defer deferutil.Recover()
			defer self.settings.WaitGroup.Done()
			if err := self.RingPhone(self.settings.Context, phone, content); err != nil {
				log.Noticef("a call's end did not reach phone %s: %s", phone.ID, err)
			}
		}(phone)
	}
}
