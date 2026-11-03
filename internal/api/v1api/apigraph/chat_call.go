package apigraph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/call"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/web"
)

// Calls in chats. A person in a chat starts a call; everybody else in it is
// rung (on their phones and in their open dashboards), except anyone who set
// the chat's notifications to off. Anyone in the chat may join while it goes,
// and it ends when the last person leaves, or as missed when nobody answers
// before the ringing stops. Each device on a call connects to the server's
// media server (internal/call): it opens ChatCallSignals, then joins with
// JoinChatCallMedia, and answers the offers the signals bring with
// AnswerChatCallMedia. docs/subsystems/chat-calls.md tells the whole of it.
//
// What a call is for the person asking is built in one place,
// describeChatCall, from the call and the chat as their conversation shows
// it. Every change is told to the people with a part in the call through
// ChatCallEvents, on every instance (notifyChatCall).

// ChatCallQuery reads calls. Needs circle:use, and a place in the chat.
type ChatCallQuery interface {
	// A chat's calls, newest first, at most first (50 when left out),
	// started before the time given when one is.
	ListChatCalls(ctx context.Context, arguments ListChatCallsArguments) ([]*ChatCallView, error)

	// One call, with its transcript when it was transcribed.
	ReadChatCall(ctx context.Context, arguments ChatCallArguments) (*ChatCallDetailView, error)

	// The calls going that the caller has a part in: ringing them, or that
	// they are on. A tab or a phone that opens reads these to ring or to
	// show a call to go back to.
	ListGoingChatCalls(ctx context.Context) ([]*ChatCallView, error)

	// The call going in a chat, null when none is: what a chat that opens
	// shows as a banner to join, whether or not the call rang the caller.
	ReadGoingChatCall(ctx context.Context, arguments ReadGoingChatCallArguments) (*ChatCallView, error)
}

// ReadGoingChatCallArguments name the chat by the caller's conversation for
// it.
type ReadGoingChatCallArguments struct {
	ConversationID string `json:"conversationId"`
}

// ChatCallMutation starts, answers and ends calls. Needs circle:use, and a
// place in the chat.
type ChatCallMutation interface {
	// Starts a call in the chat, ringing everybody else in it (or only the
	// people in userIds), with the caller on it. When a call is going in the chat already, it is that
	// call, for the caller to join. isVideoOn says the caller starts with
	// their camera on.
	StartChatCall(ctx context.Context, arguments StartChatCallArguments) (*ChatCallView, error)

	// Answers a call ringing the caller, or joins one going in their chat.
	// A device then joins its media with JoinChatCallMedia.
	JoinChatCall(ctx context.Context, arguments AnswerChatCallArguments) (*ChatCallView, error)

	// Says no to a call ringing the caller.
	DeclineChatCall(ctx context.Context, arguments AnswerChatCallArguments) (*ChatCallView, error)

	// Takes the caller off a call; it ends when nobody is left on it.
	LeaveChatCall(ctx context.Context, arguments ChatCallArguments) (*ChatCallView, error)

	// Connects the caller's device to the call's audio and video: the
	// device's offer, and the server's answer. Open ChatCallSignals first:
	// the server's later offers come there, answered with
	// AnswerChatCallMedia, in order.
	JoinChatCallMedia(ctx context.Context, arguments JoinChatCallMediaArguments) (*ChatCallMediaView, error)

	// The device's answer to the latest offer ChatCallSignals brought.
	AnswerChatCallMedia(ctx context.Context, arguments AnswerChatCallMediaArguments) (bool, error)

	// Chooses whose camera the caller's device is sent whole, the others'
	// coming small once several are on: the person in userId, or whoever is
	// speaking when it is null.
	SetChatCallFeatured(ctx context.Context, arguments SetChatCallFeaturedArguments) (bool, error)

	// Tells the others that the caller's camera is on or off; the device
	// starts or stops sending its picture itself.
	SetChatCallVideo(ctx context.Context, arguments SetChatCallVideoArguments) (*ChatCallView, error)

	// Tells the others that the caller's microphone is off or on; the device
	// stops or starts sending its sound itself.
	SetChatCallMuted(ctx context.Context, arguments SetChatCallMutedArguments) (*ChatCallView, error)

	// Turns on transcribing the call, to be summarized into the chat when it
	// ends. Everybody on it, and whoever joins later, is transcribed and told
	// so; nobody is asked. The caller's agent pays for it.
	AskChatCallTranscription(ctx context.Context, arguments ChatCallArguments) (*ChatCallView, error)
}

// ChatCallSubscription follows calls as they happen.
type ChatCallSubscription interface {
	// Each change to a call in a chat the caller is in, as it is now: it
	// rings them, somebody answered, joined or left, a camera went on, it
	// ended. An open chat shows a banner to join its call from these. Needs
	// circle:use.
	ChatCallEvents(ctx context.Context) (<-chan *ChatCallView, error)

	// What the media server tells the caller's device on a call: offers to
	// answer with AnswerChatCallMedia, one at a time and in order. Opened
	// before JoinChatCallMedia, on the instance holding the call.
	ChatCallSignals(ctx context.Context, arguments ChatCallArguments) (<-chan *ChatCallSignalView, error)
}

// ChatCallArguments name a call.
type ChatCallArguments struct {
	CallID string `json:"callId"`
}

// AnswerChatCallArguments name the call answered or declined, and the
// phone that did it when a phone did: the caller's other phones stop
// ringing, and that one, which knows already, is not told.
type AnswerChatCallArguments struct {
	CallID       string `json:"callId"`
	AgentPhoneID string `json:"agentPhoneId" graphapi:"nullable"`
}

// ListChatCallsArguments name the chat by the caller's conversation for it.
type ListChatCallsArguments struct {
	ConversationID string `json:"conversationId"`
	// At most 200; 50 when left out
	First int `json:"first" graphapi:"nullable"`
	// Calls started before this time, for the page after one
	Before *time.Time `json:"before" graphapi:"nullable"`
}

// StartChatCallArguments name the chat by the caller's conversation for it.
// UserIDs, when given, are the people in it to ring instead of everybody;
// the rest may still join from the chat's banner.
type StartChatCallArguments struct {
	ConversationID string   `json:"conversationId"`
	IsVideoOn      bool     `json:"isVideoOn" graphapi:"nullable"`
	UserIDs        []string `json:"userIds" graphapi:"nullable"`
}

// JoinChatCallMediaArguments carry the device's offer. shouldTellSpeaker
// asks for ChatCallSignals to tell who is speaking too.
type JoinChatCallMediaArguments struct {
	CallID            string `json:"callId"`
	OfferSDP          string `json:"offerSdp"`
	ShouldTellSpeaker bool   `json:"shouldTellSpeaker" graphapi:"nullable"`
}

// SetChatCallFeaturedArguments name whose camera the device is sent whole.
type SetChatCallFeaturedArguments struct {
	CallID string `json:"callId"`
	UserID string `json:"userId" graphapi:"nullable"`
}

// AnswerChatCallMediaArguments carry the device's answer.
type AnswerChatCallMediaArguments struct {
	CallID    string `json:"callId"`
	AnswerSDP string `json:"answerSdp"`
}

// SetChatCallVideoArguments say whether the caller's camera is on.
type SetChatCallVideoArguments struct {
	CallID    string `json:"callId"`
	IsVideoOn bool   `json:"isVideoOn"`
}

// SetChatCallMutedArguments say whether the caller's microphone is off.
type SetChatCallMutedArguments struct {
	CallID  string `json:"callId"`
	IsMuted bool   `json:"isMuted"`
}

// ChatCallView is a call as the person asking sees it.
type ChatCallView struct {
	ID string `json:"id"`

	// The asker's conversation for the chat, the chat's name as they see it,
	// and whether it is a group.
	ConversationID string `json:"conversationId"`
	ChatTitle      string `json:"chatTitle"`
	IsGroup        bool   `json:"isGroup"`

	StartedByUserID *string    `json:"startedByUserId"`
	StartedByName   string     `json:"startedByName"`
	StartedAt       time.Time  `json:"startedAt"`
	AnsweredAt      *time.Time `json:"answeredAt"`
	EndedAt         *time.Time `json:"endedAt"`

	// ended, missed, declined or failed; empty while it goes
	EndReason string `json:"endReason"`
	IsGoing   bool   `json:"isGoing"`

	// How long it went from the first answer, to its end or to now
	DurationMS int64 `json:"durationMs"`

	// The asker's part: ringing, answered, declined, missed, or none for
	// whoever started it; empty when they had none. isAskerOn says they are
	// on it now.
	AskerRingState string `json:"askerRingState"`
	IsAskerOn      bool   `json:"isAskerOn"`

	// Who asked for a transcript, empty when nobody did; how it stands
	// (waiting, done or failed, empty before the call ends); and the
	// summary's message in the chat once there is one.
	TranscriptionAskedByUserID *string `json:"transcriptionAskedByUserId"`
	TranscriptState            string  `json:"transcriptState"`
	SummaryMessageID           *string `json:"summaryMessageId"`

	Members []*ChatCallMemberView `json:"members"`
}

// ChatCallMemberView is one person's part in a call.
type ChatCallMemberView struct {
	UserID           string  `json:"userId"`
	Name             string  `json:"name"`
	AvatarURL        *string `json:"avatarUrl"`
	AvatarColorIndex int     `json:"avatarColorIndex"`

	// ringing, answered, declined, missed, or none for whoever started it
	RingState string `json:"ringState"`
	IsOn      bool   `json:"isOn"`
	IsVideoOn bool   `json:"isVideoOn"`
	IsMuted   bool   `json:"isMuted"`

	// yes for everybody while the call is transcribed; it stays for app
	// builds that still read it (transcriptionAskedByUserId says the same)
	TranscriptionConsent string `json:"transcriptionConsent"`

	JoinedAt *time.Time `json:"joinedAt"`
	LeftAt   *time.Time `json:"leftAt"`
}

// ChatCallDetailView is a call with its transcript, which is its
// recording's: read by the recording's owner, and by a member it is shared
// with at the transcript level.
type ChatCallDetailView struct {
	Call       *ChatCallView             `json:"call"`
	Transcript []*ChatCallTranscriptLine `json:"transcript"`

	// The recording the call became, when the caller may read it; whether it
	// is the caller's own (opened among their recordings) or shared with
	// them (opened among those shared with them).
	RecordingID      *string `json:"recordingId"`
	IsRecordingOwned bool    `json:"isRecordingOwned"`
}

// ChatCallTranscriptLine is one passage of a transcribed call under its
// speaker, timed from the start of the call.
type ChatCallTranscriptLine struct {
	ID          string `json:"id"`
	CallID      string `json:"callId"`
	UserID      string `json:"userId"`
	SpeakerName string `json:"speakerName"`
	StartMS     int64  `json:"startMs"`
	EndMS       int64  `json:"endMs"`
	Text        string `json:"text"`
}

// chatCallTranscriptLinesMost bounds the transcript a call's read returns;
// the recording has the whole of a longer one.
const chatCallTranscriptLinesMost = 2000

// ChatCallMediaView is the server's answer to a device's offer.
type ChatCallMediaView struct {
	AnswerSDP string `json:"answerSdp"`
}

// ChatCallSignalView is an offer the media server makes a device.
type ChatCallSignalView struct {
	// An offer to answer, empty when the signal says who is speaking.
	OfferSDP string `json:"offerSdp"`
	// Who is speaking now, for a device that asked (shouldTellSpeaker).
	SpeakingUserID *string `json:"speakingUserId"`
}

// chatCallsListedMost bounds a page of a chat's calls.
const chatCallsListedMost = 200

// callServer is the media server, nil when calls are off on this server or
// its ports could not be opened.
func (self *graph) callServer() *call.Server {
	if self.settings == nil || self.settings.Calls == nil || !self.config.Current().Calls.Enabled {
		return nil
	}
	return self.settings.Calls
}

var errCallsOff = fmt.Errorf("%w: calls are off on this server", api.ErrInvalidArguments)

// requireChatCall finds a call the caller has a place in the chat of, as
// their conversation for the chat shows it.
func (self *graph) requireChatCall(ctx context.Context, callId string) (*chatSession, *models.ChatCall, error) {
	user, found, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx := self.transaction(ctx)
	chatCall, err := tx.GetChatCall(strings.TrimSpace(callId))
	if err != nil {
		return nil, nil, err
	}
	if chatCall == nil {
		return nil, nil, api.ErrNotFound
	}
	chat, err := tx.GetChat(chatCall.ChatID)
	if err != nil {
		return nil, nil, err
	}
	if chat == nil {
		return nil, nil, api.ErrNotFound
	}
	member := chat.Member(user.ID)
	if member == nil || member.ConversationID == "" {
		return nil, nil, api.ErrNotFound
	}
	// A person who left the chat before the call started never had a part
	// in it.
	if member.RemovedAt != nil && member.RemovedAt.Before(chatCall.StartedAt) {
		return nil, nil, api.ErrNotFound
	}
	conversation, err := tx.GetAgentConversation(member.ConversationID)
	if err != nil {
		return nil, nil, err
	}
	if conversation == nil || conversation.AgentID != found.ID {
		return nil, nil, api.ErrNotFound
	}
	return &chatSession{User: user, Agent: found, Conversation: conversation, Chat: chat, Member: member}, chatCall, nil
}

// requireGoingChatCall is requireChatCall for a change: the call must still
// be going, and the caller still in its chat.
func (self *graph) requireGoingChatCall(ctx context.Context, callId string) (*chatSession, *models.ChatCall, error) {
	session, chatCall, err := self.requireChatCall(ctx, callId)
	if err != nil {
		return nil, nil, err
	}
	if !chatCall.IsGoing() {
		return nil, nil, fmt.Errorf("%w: this call has ended", api.ErrInvalidArguments)
	}
	if session.Member.IsRemoved {
		return nil, nil, fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	return session, chatCall, nil
}

// describeChatCall is a call as the person in session sees it.
func describeChatCall(session *chatSession, chatCall *models.ChatCall, now time.Time) *ChatCallView {
	chatView := db.ChatViewOf(session.Chat, session.User.ID)
	view := &ChatCallView{
		ID: chatCall.ID, ConversationID: session.Conversation.ID, ChatTitle: chatView.Title,
		IsGroup:   session.Chat.ChatKind == models.ChatKindGroup,
		StartedAt: chatCall.StartedAt, AnsweredAt: chatCall.AnsweredAt, EndedAt: chatCall.EndedAt,
		EndReason: string(chatCall.EndReason), IsGoing: chatCall.IsGoing(), DurationMS: chatCall.DurationMS(now),
		TranscriptState: chatCall.TranscriptState,
		Members:         []*ChatCallMemberView{},
	}
	if chatCall.StartedByUserID != "" {
		startedBy := chatCall.StartedByUserID
		view.StartedByUserID = &startedBy
		if member := session.Chat.Member(startedBy); member != nil {
			view.StartedByName = member.Name
		}
	}
	if chatCall.TranscriptionAskedByUserID != "" {
		askedBy := chatCall.TranscriptionAskedByUserID
		view.TranscriptionAskedByUserID = &askedBy
	}
	if chatCall.SummaryMessageID != "" {
		summary := chatCall.SummaryMessageID
		view.SummaryMessageID = &summary
	}
	for index := range chatCall.Members {
		part := &chatCall.Members[index]
		memberView := &ChatCallMemberView{
			UserID: part.UserID, RingState: string(part.RingState), IsOn: part.IsOn(), IsVideoOn: part.IsVideoOn,
			IsMuted: part.IsMuted, TranscriptionConsent: part.TranscriptionConsent, JoinedAt: part.JoinedAt, LeftAt: part.LeftAt,
			AvatarColorIndex: avatar.InitialColorIndex(part.UserID),
		}
		// Drawn as the chat draws them (describeChatMembers), without
		// changing the session's chat, which the caller goes on to use.
		if member := session.Chat.Member(part.UserID); member != nil {
			memberView.Name = member.Name
			if member.User != nil {
				memberView.AvatarURL = avatarUrl(member.User)
			}
		}
		if part.UserID == session.User.ID {
			view.AskerRingState = string(part.RingState)
			view.IsAskerOn = part.IsOn()
		}
		view.Members = append(view.Members, memberView)
	}
	return view
}

// chatCallUserIds are the people told of each change to a call: those with
// a part in it, and everybody else still in its chat, whose open chat shows a
// banner to join it while it goes.
func chatCallUserIds(chatCall *models.ChatCall, chat *models.Chat) []string {
	userIds := make([]string, 0, len(chatCall.Members))
	for _, member := range chatCall.Members {
		userIds = append(userIds, member.UserID)
	}
	if chat != nil {
		for _, member := range chat.ActiveMembers() {
			if member.UserID != "" && !slices.Contains(userIds, member.UserID) {
				userIds = append(userIds, member.UserID)
			}
		}
	}
	return userIds
}

// notifyChatCall tells every tab and phone of the people in the call's chat,
// on every instance, once the change is committed.
func (self *graph) notifyChatCall(ctx context.Context, chatCall *models.ChatCall) {
	tx := self.transaction(ctx)
	chat, err := tx.GetChat(chatCall.ChatID)
	if err != nil {
		log.Warningf("cannot read the chat of call %q: %s", chatCall.ID, err)
	}
	callId, userIds := chatCall.ID, chatCallUserIds(chatCall, chat)
	tx.AfterCommit(func() {
		if err := self.database.NotifyChatCallEvent(callId, userIds); err != nil {
			log.Warningf("cannot tell the people on call %q that it changed: %s", callId, err)
		}
	})
}

func (self *graph) ListChatCalls(ctx context.Context, arguments ListChatCallsArguments) ([]*ChatCallView, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	first := arguments.First
	if first <= 0 {
		first = 50
	}
	if first > chatCallsListedMost {
		first = chatCallsListedMost
	}
	calls, err := self.transaction(ctx).ListChatCalls(session.Chat.ID, arguments.Before, first)
	if err != nil {
		return nil, translateError(err)
	}
	now := time.Now()
	views := []*ChatCallView{}
	for _, chatCall := range calls {
		// A member sees the calls of the time they were in the chat.
		if chatCall.StartedAt.Before(session.Member.JoinedAt) || (session.Member.RemovedAt != nil && chatCall.StartedAt.After(*session.Member.RemovedAt)) {
			continue
		}
		views = append(views, describeChatCall(session, chatCall, now))
	}
	return views, nil
}

func (self *graph) ReadChatCall(ctx context.Context, arguments ChatCallArguments) (*ChatCallDetailView, error) {
	session, chatCall, err := self.requireChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	detail := &ChatCallDetailView{Call: describeChatCall(session, chatCall, time.Now()), Transcript: []*ChatCallTranscriptLine{}}
	if chatCall.RecordingID == "" || chatCall.TranscriptionAskedByUserID == "" {
		return detail, nil
	}
	tx := self.transaction(ctx)
	ownerId := chatCall.TranscriptionAskedByUserID
	if ownerId != session.User.ID {
		level, err := tx.GetRecordingShareLevel(session.User.ID, chatCall.RecordingID)
		if err != nil || level != models.RecordingShareLevelTranscript {
			// Not shared with them (they share no circle with its owner): the
			// summary in the chat is what they have.
			return detail, nil
		}
	}
	lines, err := chatCallTranscriptOf(tx, ownerId, chatCall)
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
		return detail, nil
	}
	if err != nil {
		return nil, err
	}
	recordingId := chatCall.RecordingID
	detail.Transcript, detail.RecordingID, detail.IsRecordingOwned = lines, &recordingId, ownerId == session.User.ID
	return detail, nil
}

// chatCallTranscriptOf reads a call's transcript from its recording, as its
// owner reads it: each passage under the name of whose microphone it was.
func chatCallTranscriptOf(tx db.Transaction, ownerId string, chatCall *models.ChatCall) ([]*ChatCallTranscriptLine, error) {
	media, err := tx.ListRecordingMedia(ownerId, chatCall.RecordingID)
	if err != nil {
		return nil, err
	}
	speakerOfMedia := map[string]*models.RecordingMedia{}
	for _, each := range media {
		speakerOfMedia[each.ID] = each
	}
	lines := []*ChatCallTranscriptLine{}
	for offset := 0; len(lines) < chatCallTranscriptLinesMost; {
		page, runs, total, err := tx.ReadRecordingTranscriptTimelinePage(ownerId, chatCall.RecordingID, offset, 500)
		if err != nil {
			return nil, err
		}
		mediaOfRun := map[string]string{}
		for _, run := range runs {
			mediaOfRun[run.ID] = run.RecordingMediaID
		}
		for _, segment := range page {
			text := segment.EffectiveTranscriptText
			if strings.TrimSpace(text) == "" {
				text = segment.TranscriptText
			}
			line := &ChatCallTranscriptLine{
				ID: segment.ID, CallID: chatCall.ID, SpeakerName: segment.LocalSpeakerID,
				StartMS: segment.SegmentStartMS, EndMS: segment.SegmentEndMS, Text: strings.TrimSpace(text),
			}
			if speaker := speakerOfMedia[mediaOfRun[segment.RecordingTranscriptRunID]]; speaker != nil && speaker.HasKnownSpeaker() {
				line.UserID, line.SpeakerName = speaker.SpeakerUserID, speaker.SpeakerName
			}
			if line.Text != "" && len(lines) < chatCallTranscriptLinesMost {
				lines = append(lines, line)
			}
		}
		offset += len(page)
		if len(page) == 0 || int64(offset) >= total {
			break
		}
	}
	return lines, nil
}

func (self *graph) ListGoingChatCalls(ctx context.Context) ([]*ChatCallView, error) {
	user, _, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, err
	}
	calls, err := self.transaction(ctx).ListGoingChatCallsOfUser(user.ID)
	if err != nil {
		return nil, err
	}
	views := []*ChatCallView{}
	for _, chatCall := range calls {
		part := chatCall.Member(user.ID)
		if part == nil || (part.RingState != models.ChatCallRinging && !part.IsOn()) {
			continue
		}
		session, _, err := self.requireChatCall(ctx, chatCall.ID)
		if err != nil {
			continue
		}
		views = append(views, describeChatCall(session, chatCall, time.Now()))
	}
	return views, nil
}

func (self *graph) ReadGoingChatCall(ctx context.Context, arguments ReadGoingChatCallArguments) (*ChatCallView, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if session.Member.IsRemoved {
		return nil, nil
	}
	going, err := self.transaction(ctx).GetGoingChatCall(session.Chat.ID)
	if err != nil || going == nil {
		return nil, err
	}
	return describeChatCall(session, going, time.Now()), nil
}

func (self *graph) StartChatCall(ctx context.Context, arguments StartChatCallArguments) (*ChatCallView, error) {
	server := self.callServer()
	if server == nil {
		return nil, errCallsOff
	}
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := self.requireChatWritable(tx, session); err != nil {
		return nil, err
	}
	if going, err := tx.GetGoingChatCall(session.Chat.ID); err != nil {
		return nil, err
	} else if going != nil {
		return describeChatCall(session, going, time.Now()), nil
	}
	rung, err := chatCallRung(session, arguments.UserIDs)
	if err != nil {
		return nil, err
	}
	chatCall, err := tx.CreateChatCall(session.Chat.ID, session.User.ID, self.settings.BackendID, rung)
	if errors.Is(err, db.ErrAlreadyExists) {
		// Started by somebody else a moment ago.
		if going, err := tx.GetGoingChatCall(session.Chat.ID); err == nil && going != nil {
			return describeChatCall(session, going, time.Now()), nil
		}
	}
	if err != nil {
		return nil, translateError(err)
	}
	if arguments.IsVideoOn {
		if chatCall, err = tx.SetChatCallVideo(chatCall.ID, session.User.ID, true); err != nil {
			return nil, err
		}
	}
	// The call's line in the chat, which every member's chat draws from the
	// call as it goes and after it ends.
	line, err := tx.AppendChatMessage(&models.ChatMessage{
		ChatID: session.Chat.ID, AuthorUserID: session.User.ID, AuthorName: session.Member.Name, CallID: chatCall.ID,
	})
	if err != nil {
		return nil, translateError(err)
	}
	line.Call = chatCall
	self.publishCallLine(ctx, session, line)
	self.notifyChatCall(ctx, chatCall)
	self.ringChatCall(ctx, session, chatCall, rung)
	return describeChatCall(session, chatCall, time.Now()), nil
}

// chatCallRung are the people a new call rings: everybody else still in the
// chat, or only the ones chosen, but never whoever turned the chat's
// notifications off.
func chatCallRung(session *chatSession, chosenUserIds []string) ([]string, error) {
	for _, userId := range chosenUserIds {
		member := session.Chat.Member(userId)
		if userId == session.User.ID || member == nil || member.IsRemoved {
			return nil, fmt.Errorf("%w: only somebody else in the chat can be called", api.ErrInvalidArguments)
		}
	}
	var rung []string
	for _, member := range session.Chat.ActiveMembers() {
		if member.UserID == session.User.ID || member.UserID == "" || member.NotificationLevel == models.ChatNotificationOff {
			continue
		}
		if chosenUserIds != nil && !slices.Contains(chosenUserIds, member.UserID) {
			continue
		}
		rung = append(rung, member.UserID)
	}
	return rung, nil
}

// publishCallLine puts the call's line in the open chats of the members,
// without telling their phones of a message: the call rings them itself.
func (self *graph) publishCallLine(ctx context.Context, session *chatSession, line *models.ChatMessage) {
	hub := self.chatHub()
	if hub == nil {
		return
	}
	shown := db.ChatMessageAsAgentMessage(line, "", nil)
	events := []agent.Event{}
	for _, member := range session.Chat.ActiveMembers() {
		if member.ConversationID == "" {
			continue
		}
		events = append(events, agent.Event{Kind: agent.EventSaid, ConversationID: member.ConversationID, MessageID: line.ID, Message: copyForMember(shown, member)})
	}
	self.transaction(ctx).AfterCommit(func() { hub.PublishChatEvents(events) })
}

func (self *graph) JoinChatCall(ctx context.Context, arguments AnswerChatCallArguments) (*ChatCallView, error) {
	if self.callServer() == nil {
		return nil, errCallsOff
	}
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := self.requireChatWritable(tx, session); err != nil {
		return nil, err
	}
	member := chatCall.Member(session.User.ID)
	wasRinging := member != nil && member.RingState == models.ChatCallRinging
	if chatCall, err = tx.JoinChatCall(chatCall.ID, session.User.ID); err != nil {
		return nil, translateError(err)
	}
	// The person's other phones stop ringing, when it rang them.
	if wasRinging {
		self.unringChatCall(ctx, chatCall, []string{session.User.ID}, strings.TrimSpace(arguments.AgentPhoneID))
	}
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

func (self *graph) DeclineChatCall(ctx context.Context, arguments AnswerChatCallArguments) (*ChatCallView, error) {
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if chatCall, err = tx.DeclineChatCall(chatCall.ID, session.User.ID); err != nil {
		return nil, translateError(err)
	}
	self.unringChatCall(ctx, chatCall, []string{session.User.ID}, strings.TrimSpace(arguments.AgentPhoneID))
	// Nobody else is on it and nobody else is rung: everybody said no.
	if chatCall.AnsweredAt == nil && !isAnyoneRung(chatCall) {
		if chatCall, err = self.endChatCall(ctx, chatCall, models.ChatCallDeclined); err != nil {
			return nil, err
		}
	}
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

func isAnyoneRung(chatCall *models.ChatCall) bool {
	for _, member := range chatCall.Members {
		if member.RingState == models.ChatCallRinging {
			return true
		}
	}
	return false
}

func (self *graph) LeaveChatCall(ctx context.Context, arguments ChatCallArguments) (*ChatCallView, error) {
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if chatCall, err = tx.LeaveChatCall(chatCall.ID, session.User.ID); err != nil {
		return nil, translateError(err)
	}
	if server := self.callServer(); server != nil {
		callId, userId := chatCall.ID, session.User.ID
		tx.AfterCommit(func() {
			server.Leave(callId, userId)
			self.chatCallSignals.forget(callId, userId)
		})
	}
	if chatCall.OnCount() == 0 {
		// The last one left; a call nobody else answered was missed by the
		// ones it rang.
		endReason := models.ChatCallEnded
		if chatCall.AnsweredAt == nil {
			endReason = models.ChatCallMissed
		}
		if chatCall, err = self.endChatCall(ctx, chatCall, endReason); err != nil {
			return nil, err
		}
	}
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

// endChatCall ends a call and takes everybody off the media server once that
// is committed; the transcript asked for, if any, is written after.
func (self *graph) endChatCall(ctx context.Context, chatCall *models.ChatCall, endReason models.ChatCallEndReason) (*models.ChatCall, error) {
	tx := self.transaction(ctx)
	ended, err := tx.EndChatCall(chatCall.ID, endReason)
	if err != nil {
		return nil, translateError(err)
	}
	// Whoever's phone still rings stops; nobody else's is told.
	self.unringChatCall(ctx, ended, ringingUserIds(chatCall), "")
	callId, isTranscriptWaiting := ended.ID, ended.TranscriptState == models.ChatCallTranscriptWaiting
	tx.AfterCommit(func() {
		if server := self.callServer(); server != nil {
			server.End(callId)
		}
		if isTranscriptWaiting {
			if worker := self.agentWorker(); worker != nil {
				worker.TranscribeChatCallSoon(callId)
			}
		}
	})
	return ended, nil
}

func (self *graph) JoinChatCallMedia(ctx context.Context, arguments JoinChatCallMediaArguments) (*ChatCallMediaView, error) {
	server := self.callServer()
	if server == nil {
		return nil, errCallsOff
	}
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	if !chatCall.Member(session.User.ID).IsOn() {
		return nil, fmt.Errorf("%w: join the call first", api.ErrInvalidArguments)
	}
	if chatCall.Instance != self.settings.BackendID {
		return nil, fmt.Errorf("%w: this call is held by another instance of the server", api.ErrInvalidArguments)
	}
	if strings.TrimSpace(arguments.OfferSDP) == "" || len(arguments.OfferSDP) > chatCallDescriptionMostBytes {
		return nil, fmt.Errorf("%w: that is not an offer", api.ErrInvalidArguments)
	}
	box := self.chatCallSignals.box(chatCall.ID, session.User.ID)
	box.drain()
	answerSDP, err := server.Join(chatCall.ID, session.User.ID, arguments.OfferSDP, arguments.ShouldTellSpeaker, box.send)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return &ChatCallMediaView{AnswerSDP: answerSDP}, nil
}

// chatCallDescriptionMostBytes bounds an offer or an answer a device sends.
const chatCallDescriptionMostBytes = 64 << 10

func (self *graph) SetChatCallFeatured(ctx context.Context, arguments SetChatCallFeaturedArguments) (bool, error) {
	server := self.callServer()
	if server == nil {
		return false, errCallsOff
	}
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return false, err
	}
	featured := strings.TrimSpace(arguments.UserID)
	if featured != "" && chatCall.Member(featured) == nil {
		return false, fmt.Errorf("%w: that person has no part in this call", api.ErrInvalidArguments)
	}
	if chatCall.Instance != self.settings.BackendID {
		return false, fmt.Errorf("%w: this call is held by another instance of the server", api.ErrInvalidArguments)
	}
	if err := server.Feature(chatCall.ID, session.User.ID, featured); err != nil {
		return false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return true, nil
}

// signalView is a signal of the media server as a device reads it.
func signalView(signal call.Signal) *ChatCallSignalView {
	view := &ChatCallSignalView{OfferSDP: signal.OfferSDP}
	if signal.SpeakingUserID != "" {
		speaking := signal.SpeakingUserID
		view.SpeakingUserID = &speaking
	}
	return view
}

func (self *graph) AnswerChatCallMedia(ctx context.Context, arguments AnswerChatCallMediaArguments) (bool, error) {
	server := self.callServer()
	if server == nil {
		return false, errCallsOff
	}
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(arguments.AnswerSDP) == "" || len(arguments.AnswerSDP) > chatCallDescriptionMostBytes {
		return false, fmt.Errorf("%w: that is not an answer", api.ErrInvalidArguments)
	}
	if err := server.Answer(chatCall.ID, session.User.ID, arguments.AnswerSDP); err != nil {
		return false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return true, nil
}

func (self *graph) SetChatCallVideo(ctx context.Context, arguments SetChatCallVideoArguments) (*ChatCallView, error) {
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	if chatCall, err = self.transaction(ctx).SetChatCallVideo(chatCall.ID, session.User.ID, arguments.IsVideoOn); err != nil {
		return nil, translateError(err)
	}
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

func (self *graph) SetChatCallMuted(ctx context.Context, arguments SetChatCallMutedArguments) (*ChatCallView, error) {
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	if chatCall, err = self.transaction(ctx).SetChatCallMuted(chatCall.ID, session.User.ID, arguments.IsMuted); err != nil {
		return nil, translateError(err)
	}
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

func (self *graph) AskChatCallTranscription(ctx context.Context, arguments ChatCallArguments) (*ChatCallView, error) {
	session, chatCall, err := self.requireGoingChatCall(ctx, arguments.CallID)
	if err != nil {
		return nil, err
	}
	if !chatCall.Member(session.User.ID).IsOn() {
		return nil, fmt.Errorf("%w: only somebody on the call may ask for it to be transcribed", api.ErrInvalidArguments)
	}
	if self.agentWorker() == nil {
		return nil, fmt.Errorf("%w: calls cannot be transcribed on this server: its agent is off", api.ErrInvalidArguments)
	}
	if chatCall, err = self.transaction(ctx).AskChatCallTranscription(chatCall.ID, session.User.ID); err != nil {
		return nil, translateError(err)
	}
	self.keepVoices(ctx, chatCall.ID)
	self.notifyChatCall(ctx, chatCall)
	return describeChatCall(session, chatCall, time.Now()), nil
}

// keepVoices starts keeping everybody's microphone on the call for its
// transcript, once the ask is committed. Only the instance holding the call
// hears it; a call held elsewhere keeps nothing here.
func (self *graph) keepVoices(ctx context.Context, callId string) {
	if self.settings == nil || self.settings.CallRecorder == nil {
		return
	}
	recorder := self.settings.CallRecorder
	self.transaction(ctx).AfterCommit(func() { recorder.Want(callId, true) })
}

// ChatCallEvents reads each call the caller has a part in again when it
// changes, and tells it as the caller sees it.
func (self *graph) ChatCallEvents(ctx context.Context) (<-chan *ChatCallView, error) {
	var userId string
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		user, _, err := self.requireChatPerson(api.ContextWithTransaction(ctx, tx))
		if err != nil {
			return err
		}
		userId = user.ID
		return nil
	}); err != nil {
		return nil, err
	}
	callIds, release, err := self.chatCallEvents.subscribe(self.database, userId)
	if err != nil {
		return nil, err
	}
	views := make(chan *ChatCallView)
	go func() {
		defer deferutil.Recover()
		defer close(views)
		defer release()
		for {
			select {
			case <-ctx.Done():
				return
			case callId := <-callIds:
				var view *ChatCallView
				if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
					session, chatCall, err := self.requireChatCall(api.ContextWithTransaction(ctx, tx), callId)
					if err != nil {
						return err
					}
					view = describeChatCall(session, chatCall, time.Now())
					return nil
				}); err != nil {
					continue
				}
				select {
				case views <- view:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return views, nil
}

// ChatCallSignals hands the caller's device the media server's offers.
func (self *graph) ChatCallSignals(ctx context.Context, arguments ChatCallArguments) (<-chan *ChatCallSignalView, error) {
	var callId, userId string
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		session, chatCall, err := self.requireGoingChatCall(api.ContextWithTransaction(ctx, tx), arguments.CallID)
		if err != nil {
			return err
		}
		if chatCall.Instance != self.settings.BackendID {
			return fmt.Errorf("%w: this call is held by another instance of the server", api.ErrInvalidArguments)
		}
		callId, userId = chatCall.ID, session.User.ID
		return nil
	}); err != nil {
		return nil, err
	}
	// Offers made before this device joined were drained by its join.
	box := self.chatCallSignals.box(callId, userId)
	views := make(chan *ChatCallSignalView)
	go func() {
		defer deferutil.Recover()
		defer close(views)
		for {
			select {
			case <-ctx.Done():
				return
			case signal := <-box.signals:
				select {
				case views <- signalView(signal):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return views, nil
}

// TickChatCalls stops the ringing of the calls on this instance that have
// rung too long (graph.tickChatCalls). The server calls it every few seconds.
func TickChatCalls(ctx context.Context, component web.Component, now time.Time) error {
	built, isGraph := component.(*graph)
	if !isGraph {
		return fmt.Errorf("apigraph: not the graph")
	}
	return built.tickChatCalls(ctx, now)
}

// FailChatCallsLeftGoing ends the calls this instance held when it stopped
// (graph.failChatCallsLeftGoing). The server calls it when it starts.
func FailChatCallsLeftGoing(ctx context.Context, component web.Component) error {
	built, isGraph := component.(*graph)
	if !isGraph {
		return fmt.Errorf("apigraph: not the graph")
	}
	return built.failChatCallsLeftGoing(ctx)
}

// tickChatCalls stops the ringing of the calls on this instance that have
// rung for calls.ringSeconds: whoever has not answered missed it, and a call
// nobody answered ends. Run every few seconds by the server.
func (self *graph) tickChatCalls(ctx context.Context, now time.Time) error {
	ringFor := time.Duration(self.config.Current().Calls.RingSeconds) * time.Second
	var going []*models.ChatCall
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		going, err = tx.ListGoingChatCalls(self.settings.BackendID)
		return err
	}); err != nil {
		return err
	}
	for _, chatCall := range going {
		if !isAnyoneRung(chatCall) || now.Sub(chatCall.StartedAt) < ringFor {
			continue
		}
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
			txContext := api.ContextWithTransaction(ctx, tx)
			stopped, err := tx.StopRingingChatCall(chatCall.ID)
			if err != nil {
				return err
			}
			// The phones still ringing stop, the call answered or not.
			self.unringChatCall(txContext, stopped, ringingUserIds(chatCall), "")
			if stopped.AnsweredAt == nil {
				if stopped, err = self.endChatCall(txContext, stopped, models.ChatCallMissed); err != nil {
					return err
				}
			}
			self.notifyChatCall(txContext, stopped)
			return nil
		}); err != nil && !errors.Is(err, db.ErrNotFound) {
			log.Warningf("cannot stop call %q ringing: %s", chatCall.ID, err)
		}
	}
	return nil
}

// failChatCallsLeftGoing ends the calls this instance held when it stopped:
// their audio and video were in its memory. Run when the server starts.
func (self *graph) failChatCallsLeftGoing(ctx context.Context) error {
	var going []*models.ChatCall
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		going, err = tx.ListGoingChatCalls(self.settings.BackendID)
		return err
	}); err != nil {
		return err
	}
	for _, chatCall := range going {
		if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
			txContext := api.ContextWithTransaction(ctx, tx)
			ended, err := self.endChatCall(txContext, chatCall, models.ChatCallFailed)
			if err != nil {
				return err
			}
			self.notifyChatCall(txContext, ended)
			return nil
		}); err != nil && !errors.Is(err, db.ErrNotFound) {
			log.Warningf("cannot end call %q left going: %s", chatCall.ID, err)
		}
	}
	return nil
}

// deviceLost takes a person off a call when their device's connection
// failed without their leaving: a tab closed, a phone that lost its network
// for good. The call ends when nobody is left on it, as when the last person
// leaves.
func (self *graph) deviceLost(callId, userId string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	self.chatCallSignals.forget(callId, userId)
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		txContext := api.ContextWithTransaction(ctx, tx)
		chatCall, err := tx.LeaveChatCall(callId, userId)
		if err != nil {
			return err
		}
		if chatCall.OnCount() == 0 {
			endReason := models.ChatCallEnded
			if chatCall.AnsweredAt == nil {
				endReason = models.ChatCallMissed
			}
			if chatCall, err = self.endChatCall(txContext, chatCall, endReason); err != nil {
				return err
			}
		}
		self.notifyChatCall(txContext, chatCall)
		return nil
	}); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Warningf("cannot take a lost device off call %q: %s", callId, err)
	}
}
