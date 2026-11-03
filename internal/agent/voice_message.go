package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/voicemessage"
)

// The words under a voice message are written by the server after the message
// is posted, from the recording, by the same file transcription recordings
// use. The sender's agent pays for them, so a sender with no agent, or none
// that can spend, gets none and the message still plays.

const (
	// voiceMessageClaimLifetime is how long a server's claim on a message
	// holds before another may take it, so a server that died mid-way does not
	// keep a message waiting forever, and two servers do not pay for the same
	// words.
	// It outlives the provider's own limit on a transcription, so a server
	// still waiting on the provider is not overtaken by another that pays a
	// second time.
	voiceMessageClaimLifetime = 11 * time.Minute

	// voiceMessageGiveUpAfter is how long a message waits for its words before
	// the sweep says it will not have them.
	voiceMessageGiveUpAfter = 30 * time.Minute

	// voiceMessageSweepEvery and voiceMessageSweepBatch bound the sweep: it
	// looks about as often as the tick, and takes a few at a time so that one
	// slow provider does not hold the rest for long.
	voiceMessageSweepEvery = 10 * time.Second
	voiceMessageSweepBatch = 10
)

// VoiceMessageTranscriptState is the state a voice message from this sender
// starts in: waiting for its words, or off when there will be none because
// voice is off on the server, or the sender's agent is off, may not be used,
// or has spent its budget.
func (self *Agent) VoiceMessageTranscriptState(tx db.Transaction, sender *models.Agent, owner *models.User) string {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || !configuration.Agent.Voice.Enabled || configuration.Agent.VoiceProvider() == nil || self.settings.Registry == nil {
		return models.VoiceTranscriptOff
	}
	if sender == nil || owner == nil || !sender.Active() {
		return models.VoiceTranscriptOff
	}
	permissions, err := tx.EffectivePermissions(owner.ID)
	if err != nil || !permissions.Has(models.PermissionAgentUse) {
		return models.VoiceTranscriptOff
	}
	if err := RequireBudget(tx, configuration, sender, owner, time.Now()); err != nil {
		return models.VoiceTranscriptOff
	}
	return models.VoiceTranscriptWaiting
}

// voiceMessageModelName is the transcription model, written provider:model, or
// empty when voice has none: the voice settings' model through their
// OpenAI-compatible provider, which is what recordings use for their second
// pass.
func (self *Agent) voiceMessageModelName() string {
	configuration := self.settings.Configuration()
	provider := configuration.Agent.VoiceProvider()
	if !configuration.Agent.Voice.Enabled || provider == nil {
		return ""
	}
	return provider.Name + ":" + configuration.Agent.Voice.EffectiveTranscriptionModel()
}

// TranscribeVoiceMessageSoon starts writing the words of a message just
// posted, beside whatever the caller does next. A server that is stopping
// starts nothing, and the sweep finds the message once it is back.
func (self *Agent) TranscribeVoiceMessageSoon(messageId string) {
	if self.ctx == nil || self.ctx.Err() != nil {
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		if err := self.TranscribeVoiceMessage(self.ctx, messageId); err != nil && self.ctx.Err() == nil {
			log.Warningf("cannot write the words of voice message %q yet: %s", messageId, err)
		}
	}()
}

// TranscribeVoiceMessage writes the words of a voice message that is waiting
// for them, and tells the members. It claims the message first, so that of
// several servers one works on it. An error means the words were not written
// and the message is still waiting, to be tried again once the claim has run
// out; a message that cannot be transcribed by any retry ends failed or off
// instead, with no error.
func (self *Agent) TranscribeVoiceMessage(ctx context.Context, messageId string) error {
	var message *models.ChatMessage
	var sender *models.Agent
	var owner *models.User
	var chat *models.Chat
	var state string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if message, err = tx.ClaimVoiceMessageTranscript(messageId, voiceMessageClaimLifetime); err != nil || message == nil {
			return err
		}
		if chat, err = tx.GetChat(message.ChatID); err != nil {
			return err
		}
		if owner, err = tx.GetUser(message.AuthorUserID); err != nil || owner == nil {
			return err
		}
		if sender, err = tx.GetAgentByUser(owner.ID); err != nil {
			return err
		}
		state = self.VoiceMessageTranscriptState(tx, sender, owner)
		return nil
	}); err != nil {
		return err
	}
	if message == nil {
		return nil
	}
	if chat == nil || owner == nil || len(message.Attachments) == 0 {
		return self.finishVoiceMessage(ctx, message, models.VoiceTranscriptFailed, "", nil)
	}
	if state == models.VoiceTranscriptOff {
		return self.finishVoiceMessage(ctx, message, models.VoiceTranscriptOff, "", nil)
	}

	modelName := self.voiceMessageModelName()
	selection, err := self.settings.Registry.TranscribingFor(modelName, false, false)
	if err != nil {
		log.Warningf("voice message %q has no words, %q cannot transcribe: %s", message.ID, modelName, err)
		return self.finishVoiceMessage(ctx, message, models.VoiceTranscriptFailed, "", nil)
	}
	recording := message.Attachments[0]
	content, err := self.settings.Storage.GetFile(ctx, recording.ID)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(chat.Members))
	for _, member := range chat.ActiveMembers() {
		if name := strings.TrimSpace(member.Name); name != "" {
			names = append(names, name)
		}
	}
	response, err := selection.Transcriber.Transcribe(ctx, &llm.TranscriptionRequest{
		Model: selection.Model, MediaFilename: "voice.m4a", MediaContentType: voicemessage.ContentType,
		MediaBytes: content, MediaDurationMS: recording.AudioDurationMS,
		VocabularyPrompt: strings.Join(names, ", "),
	})
	if err != nil {
		var apiError *llm.APIError
		// A refusal of the recording itself (not a limit, not the provider
		// being down) would be refused again.
		if errors.As(err, &apiError) && apiError.Status >= 400 && apiError.Status < 500 && apiError.Status != 429 && apiError.Status != 408 {
			log.Warningf("voice message %q has no words, the provider refused it: %s", message.ID, err)
			return self.finishVoiceMessage(ctx, message, models.VoiceTranscriptFailed, "", nil)
		}
		return err
	}
	// What was heard is paid for whether or not the message is still there to
	// show it.
	return self.finishVoiceMessage(ctx, message, models.VoiceTranscriptDone, strings.TrimSpace(response.TranscriptText), func(tx db.Transaction) error {
		return RecordUsageIn(tx, sender.ID, "", modelName, "voice", response.TranscriptionUsage.Usage)
	})
}

// finishVoiceMessage writes how a waiting message ended and tells the members,
// in one go with whatever the caller writes beside it. A message that was
// deleted, or finished by another server, meanwhile is not told about, and
// what the caller writes beside it is not written: the usage of words nobody
// keeps is not charged.
func (self *Agent) finishVoiceMessage(ctx context.Context, message *models.ChatMessage, transcriptState, transcriptText string, alongside func(tx db.Transaction) error) error {
	var finished *models.ChatMessage
	var chat *models.Chat
	var author *models.User
	// The words were paid for, so they are kept even when the request that
	// started them was canceled.
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), jobCompletionTimeout)
	defer cancelCompletion()
	if err := self.settings.Database.TransactionContext(completionContext, func(tx db.Transaction) (err error) {
		if finished, err = tx.FinishVoiceMessageTranscript(message.ID, transcriptState, transcriptText); err != nil || finished == nil {
			return err
		}
		if alongside != nil {
			if err = alongside(tx); err != nil {
				return err
			}
		}
		if chat, err = tx.GetChat(finished.ChatID); err != nil {
			return err
		}
		users, err := tx.GetUsersByID([]string{finished.AuthorUserID})
		author = users[finished.AuthorUserID]
		return err
	}); err != nil {
		return err
	}
	if finished != nil && chat != nil {
		self.publishEditedVoiceMessage(finished, chat, author)
	}
	return nil
}

// publishEditedVoiceMessage tells the members who can read the message that it
// changed, as an edit to a message does: the clients draw the message again,
// with its words. A person who joined after it was said was never shown it.
func (self *Agent) publishEditedVoiceMessage(message *models.ChatMessage, chat *models.Chat, author *models.User) {
	shown := db.ChatMessageAsAgentMessage(message, "", nil)
	key := message.AuthorUserID
	if key == "" {
		key = message.AuthorName
	}
	shown.AuthorAvatarColorIndex = avatar.InitialColorIndex(key)
	shown.AuthorAvatarURL = api.UserAvatarURL(author)
	events := []Event{}
	for _, member := range chat.ActiveMembers() {
		if member.ConversationID == "" || message.CreatedAt.Before(member.JoinedAt) {
			continue
		}
		copied := *shown
		copied.ConversationID = member.ConversationID
		copied.IsMine = message.AuthorUserID != "" && message.AuthorUserID == member.UserID
		events = append(events, Event{Kind: EventEdited, ConversationID: member.ConversationID, MessageID: shown.ID, Message: &copied})
	}
	if len(events) > 0 {
		self.PublishChatEvents(events)
	}
}

// sweepVoiceMessages gives up on the voice messages that have waited too long
// and starts the ones nobody is working on: a server restarted between posting
// a message and writing its words, or a provider that was down. The sweep runs
// whether or not the agent is on, since a message waiting on an agent that has
// since been switched off ends off, which is the sweep's to say.
func (self *Agent) sweepVoiceMessages(ctx context.Context, now time.Time) {
	if now.Sub(self.lastVoiceMessageSweep) < voiceMessageSweepEvery {
		return
	}
	self.lastVoiceMessageSweep = now
	// Calls are transcribed only where calls are kept.
	if self.settings.CallRecorder != nil {
		self.sweepChatCallTranscripts(ctx)
	}
	var failed []*models.ChatMessage
	var waitingIds []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if failed, err = tx.FailStaleVoiceMessages(now.Add(-voiceMessageGiveUpAfter)); err != nil {
			return err
		}
		waitingIds, err = tx.ListWaitingVoiceMessageIDs(voiceMessageClaimLifetime, voiceMessageSweepBatch)
		return err
	}); err != nil {
		log.Warningf("cannot sweep the voice messages that are waiting: %s", err)
		return
	}
	for _, message := range failed {
		var chat *models.Chat
		var author *models.User
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			if chat, err = tx.GetChat(message.ChatID); err != nil {
				return err
			}
			users, err := tx.GetUsersByID([]string{message.AuthorUserID})
			author = users[message.AuthorUserID]
			return err
		}); err != nil || chat == nil {
			continue
		}
		self.publishEditedVoiceMessage(message, chat, author)
	}
	if len(waitingIds) == 0 || !self.transcribingVoiceMessages.CompareAndSwap(false, true) {
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		defer self.transcribingVoiceMessages.Store(false)
		for _, messageId := range waitingIds {
			if ctx.Err() != nil {
				return
			}
			if err := self.TranscribeVoiceMessage(ctx, messageId); err != nil && ctx.Err() == nil {
				log.Warningf("cannot write the words of voice message %q yet: %s", messageId, err)
			}
		}
	}()
}
