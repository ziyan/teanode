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

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// Notifications on the person's phones, through the TeaNode app.
//
// Three things reach a phone, each only of the kinds the phone wants:
//
//   - The agent saying something on its own in the main conversation: an
//     alert, a coding session's question, a goal that needs the person, the
//     agent starting a conversation. These are read back from the
//     conversation with a cursor per phone, like a chat app's relay, so one
//     is never lost to a restart or sent twice by two instances.
//   - A card waiting for the person, approval or question, raised while
//     nobody is looking at the dashboard or the app.
//   - An answer to something asked from the app, finished after the person
//     left it.
//
// Each is encrypted for the phone (internal/push) and sent through the relay.

const (
	// phonePushEvery is how often the agent's own turns are looked for.
	phonePushEvery = 10 * time.Second

	// phonePushSettle leaves a turn alone until it has finished writing.
	phonePushSettle = 3 * time.Second

	// phonePushAtOnce bounds what one look sends to one phone; the rest
	// waits for the next look.
	phonePushAtOnce = 10

	// phonePushTimeout bounds one notification's trip through the relay.
	phonePushTimeout = 30 * time.Second
)

// phoneSenders builds, and rebuilds when the settings change, what sends a
// notification: this server's own relay when it is one, or the relay over
// HTTP.
type phoneSenders struct {
	mutex       sync.Mutex
	sender      push.Sender
	fingerprint string

	// override replaces the sender in a test, which stands in for the relay.
	override push.Sender
}

// phoneSender is how this server sends to phones now, or nil while
// notifications are off.
func (self *Agent) phoneSender() (push.Sender, error) {
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

	self.phones.mutex.Lock()
	defer self.phones.mutex.Unlock()
	if self.phones.override != nil {
		return self.phones.override, nil
	}
	if self.phones.sender != nil && self.phones.fingerprint == fingerprint {
		return self.phones.sender, nil
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
		self.phones.sender = relay
	} else {
		self.phones.sender = push.NewRelayClient(settings.ResolvedRelayURL())
	}
	self.phones.fingerprint = fingerprint
	return self.phones.sender, nil
}

// NotifyPhone sends one notification to one phone and records how it went.
func (self *Agent) NotifyPhone(ctx context.Context, phone *models.AgentPhone, content *push.Content) error {
	sender, err := self.phoneSender()
	if err != nil {
		return err
	}
	if sender == nil {
		return fmt.Errorf("agent: notifications to phones are off on this server")
	}
	if !phone.CanBeNotified() {
		return fmt.Errorf("agent: the phone has not registered for notifications; open the TeaNode app on it")
	}
	opened, err := self.OpenSecret(phone.PayloadKey)
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

// notifyPhones sends a notification to each of the person's phones that
// wants its kind, in the background: nothing that raises one waits on Apple.
func (self *Agent) notifyPhones(agentId, notificationKind string, content *push.Content) {
	if sender, err := self.phoneSender(); err != nil || sender == nil {
		return
	}
	var phones []*models.AgentPhone
	if err := self.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) (err error) {
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
		self.waitGroup.Add(1)
		go func(phone *models.AgentPhone) {
			defer self.waitGroup.Done()
			if err := self.NotifyPhone(self.ctx, phone, content); err != nil {
				log.Noticef("a notification did not reach phone %s: %s", phone.ID, err)
			}
		}(phone)
	}
}

// startPhonePushes starts the loop that sends the agent's own turns.
func (self *Agent) startPhonePushes() {
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		ticker := time.NewTicker(phonePushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-self.ctx.Done():
				return
			case <-ticker.C:
				self.pushOwnTurns(self.ctx, time.Now())
			}
		}
	}()
}

// pushOwnTurns sends each phone what the agent said on its own in the main
// conversation since the phone was last sent anything.
func (self *Agent) pushOwnTurns(ctx context.Context, now time.Time) {
	if sender, err := self.phoneSender(); err != nil || sender == nil {
		return
	}
	var phones []*models.AgentPhone
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		phones, err = tx.ListNotifiableAgentPhones()
		return err
	}); err != nil {
		log.Warningf("cannot list the phones to notify: %s", err)
		return
	}
	for _, phone := range phones {
		if !phone.Wants(models.NotificationMessage) {
			continue
		}
		answers, agentName, err := self.claimOwnTurns(ctx, phone, now)
		if err != nil {
			log.Warningf("cannot read what to send phone %s: %s", phone.ID, err)
			continue
		}
		for _, answer := range answers {
			content := &push.Content{
				NotificationKind: models.NotificationMessage,
				ConversationID:   answer.ConversationID,
				Title:            agentName,
				Body:             phoneBody(answer.Content),
				ThreadID:         answer.ConversationID,
				Buttons:          []push.Button{{Label: "Reply", ButtonAction: "reply"}, {Label: "Open", ButtonAction: "open"}},
			}
			if err := self.NotifyPhone(ctx, phone, content); err != nil {
				log.Noticef("an answer did not reach phone %s: %s", phone.ID, err)
				if errors.Is(err, push.ErrGone) {
					break
				}
			}
		}
	}
}

// claimOwnTurns moves the phone's cursor past the agent's own turns written
// since it was last moved, and returns them; nothing when another instance
// moved it first.
//
// The cursor moves before anything is sent, so a turn reaches a phone at most
// once: a send that fails is not tried again, which for a notification is
// the better failure than a second copy. Turns are read only once they are
// phonePushSettle old, so one still being written is not stepped past.
func (self *Agent) claimOwnTurns(ctx context.Context, phone *models.AgentPhone, now time.Time) ([]*models.AgentMessage, string, error) {
	var answers []*models.AgentMessage
	agentName := "TeaNode"
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		person, err := tx.GetAgent(phone.AgentID)
		if err != nil || person == nil || !person.Active() {
			return err
		}
		if strings.TrimSpace(person.Name) != "" {
			agentName = person.Name
		}
		mains, err := tx.ListAgentConversations(phone.AgentID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
		if err != nil || len(mains) == 0 {
			return err
		}
		before := now.Add(-phonePushSettle)
		found, err := tx.ListAgentOwnTurnAnswers(mains[0].ID, phone.PushedThrough, before, phonePushAtOnce)
		if err != nil {
			return err
		}
		through := ""
		if len(found) == phonePushAtOnce {
			through = found[len(found)-1].ID
		} else if through, err = tx.LastAgentMessageID(mains[0].ID, before); err != nil {
			return err
		}
		if through <= phone.PushedThrough {
			return nil
		}
		isAdvanced, err := tx.AdvanceAgentPhonePush(phone.ID, phone.PushedThrough, through)
		if err != nil || !isAdvanced {
			return err
		}
		answers = found
		return nil
	})
	return answers, agentName, err
}

// notifyInteraction tells the person's phones about a card raised while
// nobody is looking at the dashboard or the app.
func (self *AskRun) notifyInteraction(interaction *models.AgentInteraction) {
	if interaction == nil || self.settings.Agent == nil {
		return
	}
	if self.isWatched(time.Now()) {
		return
	}
	content := &push.Content{
		ConversationID: interaction.ConversationID,
		RunID:          interaction.RunID,
		CallID:         interaction.CallID,
		Title:          "Waiting for you",
		Body:           phoneBody(interaction.InteractionText),
		ThreadID:       interaction.ConversationID,
	}
	notificationKind := models.NotificationQuestion
	if interaction.InteractionKind == models.InteractionApproval {
		notificationKind = models.NotificationApproval
		content.Title = "Approve?"
		// Only the tool's name: an approval card's line can carry what the
		// call was given, a command or a token among it, and a notification
		// is read on a lock screen. The card in the app shows the rest.
		content.Body = "Wants to use " + interaction.ToolName + "."
		content.Buttons = []push.Button{{Label: "Approve", ButtonAction: "approve"}, {Label: "Deny", ButtonAction: "deny"}}
	} else {
		for index, choice := range interaction.InteractionChoices {
			if index == 3 {
				break
			}
			content.Buttons = append(content.Buttons, push.Button{Label: choice, ButtonAction: fmt.Sprintf("answer:%d", index+1)})
		}
		content.Buttons = append(content.Buttons, push.Button{Label: "Reply", ButtonAction: "reply"})
	}
	content.NotificationKind = notificationKind
	self.agent.notifyPhones(self.settings.Agent.ID, notificationKind, content)
}

// notifyAnswer tells the person's phones that an answer is ready, when
// nobody is looking at a dashboard or the app; see isAnswerForPhone for
// which answers.
func (self *AskRun) notifyAnswer(answer string) {
	if self.settings.Agent == nil || strings.TrimSpace(answer) == "" || !self.isAnswerForPhone() {
		return
	}
	if self.isWatched(time.Now()) {
		return
	}
	agentName := self.settings.Agent.Name
	if strings.TrimSpace(agentName) == "" {
		agentName = "TeaNode"
	}
	self.agent.notifyPhones(self.settings.Agent.ID, models.NotificationAnswer, &push.Content{
		NotificationKind: models.NotificationAnswer,
		ConversationID:   self.settings.Conversation.ID,
		RunID:            self.ID,
		Title:            agentName,
		Body:             phoneBody(answer),
		ThreadID:         self.settings.Conversation.ID,
		Buttons:          []push.Button{{Label: "Reply", ButtonAction: "reply"}, {Label: "Open", ButtonAction: "open"}},
	})
}

// isWatched says whether the person is looking at a dashboard or the app
// now, taking a turn they wrote themselves as their latest input.
func (self *AskRun) isWatched(now time.Time) bool {
	lastWroteAt := time.Time{}
	if !self.settings.isUnattended && !self.settings.Headless && self.settings.subagentDepth == 0 && !models.IsOwnTurn(self.settings.Message) {
		self.mutex.Lock()
		lastWroteAt = self.lastWroteAt
		self.mutex.Unlock()
	}
	return self.agent.isWatching(self.settings.Agent.UserID, lastWroteAt, now)
}

// isAnswerForPhone says whether the turn's answer is one a phone is told
// about: one to something asked from the app, in any conversation, and one
// in the main conversation to something asked on the dashboard, where the
// answer otherwise waits unread. The command line and the API wait for the
// answer and show it, the agent's own turns in the main conversation reach
// the phone through pushOwnTurns, an answer on a call is heard, and one
// asked from a linked chat or by mail is sent there.
func (self *AskRun) isAnswerForPhone() bool {
	if self.settings.isUnattended || self.settings.subagentDepth > 0 {
		return false
	}
	if isAppSurface(self.settings.Surface) {
		return true
	}
	if self.settings.Conversation == nil || self.settings.Conversation.Kind != models.AgentConversationMain {
		return false
	}
	if models.IsOwnTurn(self.settings.Message) {
		return false
	}
	switch self.settings.Surface {
	case "drawer", "extension", "page":
		return true
	}
	return false
}

// phoneBody is text as a notification shows it: plain, and short enough that
// a lock screen is not the place it is read in full.
func phoneBody(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	for _, mark := range []string{"**", "__", "`"} {
		text = strings.ReplaceAll(text, mark, "")
	}
	runes := []rune(text)
	if len(runes) > 600 {
		return string(runes[:600]) + "…"
	}
	return text
}
