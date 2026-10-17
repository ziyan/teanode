package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/version"
)

// Turns in a chat. A person addresses their agent in a chat and it answers
// there, to everybody in it, under its name. The answer of each round is a
// message of the chat; what the turn did to find it (its tool calls and
// their results, its notes and its cards) stays rows of the owner's own
// conversation for the chat, which nobody else reads.
//
// There are two turns. In an owner turn the agent answers its owner, with its
// usual tools and its cards. In a member turn it answers somebody else in the
// chat whom its owner allowed to ask it there, and is held to what everybody
// in the chat may see: the chat and the web, no card, nothing of the owner's.
// internal/chat decides which turn a message starts; this is how they run.
// docs/subsystems/conversations.md says why.

// ChatTurn is what makes a turn one in a chat.
type ChatTurn struct {
	// ChatID is the chat, which the conversation of the turn is the owner's
	// own for.
	ChatID string

	// MessageID is the chat message that addressed the agent. It is empty for
	// the turn a late answer to a card brings, which has no message of the
	// chat to answer.
	MessageID string

	// AskerUserID and AskerName are who wrote it.
	AskerUserID string
	AskerName   string

	// IsMemberTurn says somebody other than the owner asked, whom the owner
	// allowed to.
	IsMemberTurn bool
}

// The bounds of a member turn: the owner's budget pays for it, and somebody
// else chose to spend it.
const (
	memberTurnMaximumRounds = 8
	memberTurnSearches      = 6
	memberTurnFetches       = 6

	// memberTurnsWaitingMost is how many turns of an agent in one chat may be
	// running or waiting before a question from somebody else is refused.
	memberTurnsWaitingMost = 3
)

// ErrAgentBusy says an agent has as many turns waiting in a chat as it takes
// from other people.
var ErrAgentBusy = errors.New("agent: that agent is busy; ask again in a moment")

// memberTurnTools are the only tools a member turn is offered: the web, the
// time, and the conversation tool, which a member turn may only read this
// chat with (see HeldToChat).
var memberTurnTools = map[string]bool{"web_search": true, "web_fetch": true, "datetime": true, "conversation": true, "tool_search": true}

// chatHistoryRows is how much of a chat conversation a turn reads when no
// compaction has folded the older part into a note.
const chatHistoryRows = 400

// isMemberTurn says somebody other than the owner asked.
func (self *AskSettings) isMemberTurn() bool {
	return self != nil && self.Chat != nil && self.Chat.IsMemberTurn
}

// isAnsweringChatMessage says the turn answers a message of the chat, which
// is stored already as the chat's, rather than words of its own.
func (self *AskSettings) isAnsweringChatMessage() bool {
	return self.Chat != nil && self.Chat.MessageID != ""
}

// applyChatSettings makes a turn in a chat what it is: on the chat surface,
// and for a member turn held to what everybody in the chat may see. It is the
// one place that holds, so no caller can start a member turn with more.
func applyChatSettings(settings *AskSettings) error {
	conversation := settings.Conversation
	if !conversation.IsChat() || conversation.ChatID == "" {
		return fmt.Errorf("agent: a turn in a chat is in a conversation of a chat")
	}
	if settings.Chat == nil {
		// A late answer to a card in a chat: the owner's, with no message of
		// the chat to answer.
		settings.Chat = &ChatTurn{ChatID: conversation.ChatID, AskerUserID: settings.Owner.ID}
	}
	if settings.Chat.ChatID != conversation.ChatID {
		return fmt.Errorf("agent: the turn names another chat than its conversation is for")
	}
	if settings.Agent.UserID != settings.Owner.ID || conversation.AgentID != settings.Agent.ID {
		return fmt.Errorf("agent: a turn in a chat is in its agent's own conversation")
	}
	settings.Surface = "chat"
	settings.Headless = false
	if !settings.Chat.IsMemberTurn {
		return nil
	}
	if settings.Chat.MessageID == "" || settings.Chat.AskerUserID == "" || settings.Chat.AskerUserID == settings.Owner.ID {
		return fmt.Errorf("agent: a member turn answers a message of somebody else in the chat")
	}
	// How hard the owner's agent thinks is the owner's to choose; somebody
	// else asking gets the default.
	settings.Effort = ""
	settings.ReadOnly = true
	settings.Allow = maps.Clone(memberTurnTools)
	settings.isUnattended = true
	settings.IsPersonOmitted = true
	settings.PreApproved = nil
	settings.Viewing = nil
	settings.References = nil
	settings.Pictures = nil
	settings.ReadOnlyTools = nil
	settings.Research = false
	settings.MaxRounds = memberTurnMaximumRounds
	settings.CallLimits = map[string]int{"web_search": memberTurnSearches, "web_fetch": memberTurnFetches}
	return nil
}

// HeldToChatConversation is, for a member turn, the one conversation the
// conversation tool may read: the owner's own for this chat, whose rows it
// reads as chat messages only. Empty for every other turn.
func (self *AskRun) HeldToChatConversation() string {
	if self.settings.isMemberTurn() {
		return self.settings.Conversation.ID
	}
	return ""
}

// IsAnsweringInChat says the turn's words are posted to the room of a chat.
func (self *AskRun) IsAnsweringInChat() bool {
	return self.settings.Chat != nil || (self.settings.Conversation != nil && self.settings.Conversation.IsChat())
}

// chatAuthorName is a person's name as the model reads it beside what they
// wrote: on one line and short, because it is typed by whoever wrote it.
func chatAuthorName(name string) string {
	cleaned := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, name)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return "Someone"
	}
	return cutRunes(cleaned, 60)
}

// chatWords is what somebody else wrote in the chat as the model reads it:
// their name outside, their words inside the fence. They are data, never
// instructions to the agent of somebody else.
func chatWords(authorName, ownerName string, isAgent bool, words string) string {
	verb := "wrote"
	if isAgent {
		// Most agents are called "Agent": whose it is tells two of them apart.
		if strings.TrimSpace(ownerName) != "" {
			authorName = models.AgentSpeakerLabel(authorName, ownerName)
		} else {
			authorName += " (an agent)"
		}
		verb = "said"
	}
	return chatAuthorName(authorName) + " " + verb + " in the chat:\n" + fenced(words)
}

// chatTurnRows turns the rows of a chat conversation, the chat's messages and
// the owner's own rows by time, into the rows the model's history is made
// from, as the owner reads the chat. What the owner wrote is their turn, what
// their agent said is its answer, and what anybody else or any other agent
// said is data inside the fence under the author's name. A round's words and
// its calls were stored apart, the words as a message of the chat and the
// calls as the owner's own row; they are put back as the one assistant turn
// they were. The message that started the turn is left out, for the caller to
// put last.
func chatTurnRows(rows []*models.AgentMessage, ownerUserId, agentId, triggerId string) []*models.AgentMessage {
	converted := make([]*models.AgentMessage, 0, len(rows))
	isOwnAnswer := map[*models.AgentMessage]bool{}
	for _, row := range rows {
		if triggerId != "" && row.ID == triggerId {
			continue
		}
		if row.AuthorKind == "" {
			// The owner's own row: calls, results, notes, a compaction.
			if row.Role == string(llm.RoleAssistant) && len(row.ToolCalls) > 0 && strings.TrimSpace(row.Content) == "" && len(converted) > 0 {
				if previous := converted[len(converted)-1]; isOwnAnswer[previous] && len(previous.ToolCalls) == 0 {
					previous.ToolCalls = row.ToolCalls
					previous.ID = row.ID
					previous.Usage = row.Usage
					continue
				}
			}
			converted = append(converted, row)
			continue
		}
		copied := *row
		copied.Reactions = nil
		switch {
		case row.AuthorKind == models.ChatAuthorAgent && row.AuthorAgentID == agentId:
			copied.Role = string(llm.RoleAssistant)
			copied.Content = strings.TrimSpace(row.Content)
			isOwnAnswer[&copied] = true
		case row.AuthorKind == models.ChatAuthorPerson && row.AuthorUserID == ownerUserId:
			copied.Role = string(llm.RoleUser)
			copied.Content = historyTurn(&copied)
			copied.Attachments = nil
		default:
			copied.Role = string(llm.RoleUser)
			copied.Content = chatWords(row.AuthorName, row.AuthorOwnerName, row.AuthorKind == models.ChatAuthorAgent, historyTurn(&copied))
			copied.Attachments = nil
		}
		converted = append(converted, &copied)
	}
	return converted
}

// memberTurnHistoryTokens is the most history a turn somebody else asked for
// reads: the newest of the chat that fit. It is never compacted, since a
// compaction would be written into the owner's conversation and made from what
// their own agent found.
const memberTurnHistoryTokens = askHistoryTokens / 2

// loadChatHistory is the chat conversation as the model gets it. For the
// owner it is the conversation as they read it. For anybody else's question it
// is what everybody in the chat read: the chat's messages and none of the rows
// the owner's own agent wrote there, whose tool results hold what it found in
// their mail, memory or files, and none of the compaction notes made from them.
func (self *AskRun) loadChatHistory(tx db.Transaction) ([]llm.ChatMessage, error) {
	settings := self.settings
	rows, err := tx.ListChatTurnRows(settings.Conversation, chatHistoryRows, settings.isMemberTurn())
	if err != nil {
		return nil, err
	}
	converted := chatTurnRows(rows, settings.Owner.ID, settings.Agent.ID, settings.Chat.MessageID)
	if settings.isMemberTurn() {
		return trimmedHistory(historyOf(converted, ""), memberTurnHistoryTokens), nil
	}
	return historyOf(converted, settings.Conversation.CompactedThrough), nil
}

// trimmedHistory drops the oldest messages until the rest fit in the tokens
// given, never leaving the history starting on a tool's answer.
func trimmedHistory(history []llm.ChatMessage, tokens int) []llm.ChatMessage {
	for len(history) > 1 && llm.EstimateTokens(renderHistory(history)) > tokens {
		history = history[1:]
	}
	for len(history) > 0 && history[0].Role == llm.RoleTool {
		history = history[1:]
	}
	return history
}

// chatTrigger is the message that addressed the agent, as the model reads it
// last: the owner's own words as their turn, anybody else's inside the fence.
func (self *AskRun) chatTrigger(ctx context.Context) llm.ChatMessage {
	settings := self.settings
	message := userTurn(ctx, self.agent.settings.Storage, personText(settings), settings.Attachments, settings.Pictures, settings.References)
	if !settings.isMemberTurn() {
		return message
	}
	message.Content = chatWords(settings.Chat.AskerName, "", false, message.Content)
	if len(message.Parts) > 0 {
		message.Parts[0].Text = message.Content
	}
	return message
}

// chatPromptFacts is who is in the room, for the conduct's chat section.
type chatPromptFacts struct {
	// People is everybody in the chat now, one of them marked as the person
	// the agent acts for.
	People string

	// AskerName is, in a member turn, who asked.
	AskerName    string
	IsMemberTurn bool
}

// chatPrompt reads who is in the chat. Names are typed by whoever has them,
// and this goes into the part of the prompt that is the server's own, so each
// is made one short line and quoted.
func (self *AskRun) chatPrompt(ctx context.Context) *chatPromptFacts {
	settings := self.settings
	if settings.Chat == nil {
		return nil
	}
	var chat *models.Chat
	err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		chat, err = tx.GetChat(settings.Chat.ChatID)
		return err
	})
	if err != nil || chat == nil {
		log.Debugf("cannot read who is in chat %q for the prompt: %v", settings.Chat.ChatID, err)
		return self.withAsker(&chatPromptFacts{People: "the people there", IsMemberTurn: settings.isMemberTurn()})
	}
	names := []string{}
	for _, member := range chat.ActiveMembers() {
		name := fmt.Sprintf("%q", chatAuthorName(member.Name))
		if member.UserID == settings.Owner.ID {
			name += " (the person you act for)"
		}
		names = append(names, name)
	}
	people := strings.Join(names, ", ")
	if count := len(names); count > 1 {
		people = strings.Join(names[:count-1], ", ") + " and " + names[count-1]
	}
	return self.withAsker(&chatPromptFacts{People: people, IsMemberTurn: settings.isMemberTurn()})
}

// withAsker names, in a member turn, who asked: one short line, quoted,
// because the asker typed their own name and it sits in the server's part of
// the prompt.
func (self *AskRun) withAsker(facts *chatPromptFacts) *chatPromptFacts {
	if facts.IsMemberTurn {
		facts.AskerName = fmt.Sprintf("%q", chatAuthorName(self.settings.Chat.AskerName))
	}
	return facts
}

// memberSituation is what a turn somebody else asked for is told of where it
// is: the chat, and none of the owner's mailboxes, calendars, goals or what
// they may do.
func memberSituation(owner *models.User, agent *models.Agent, configuration *config.Configuration) string {
	return strings.Join([]string{
		fmt.Sprintf("You are answering in a chat for %s, whose agent you are.", personName(owner)),
		"Here you can read this chat and look things up on the web, and nothing else: their mail, memory, files, calendar, money and devices are closed to you in this turn, and you cannot change anything or put a question to them.",
		fmt.Sprintf("Their time zone is %s. Their language is %s.", Location(owner).String(), languageName(Language(agent, owner))),
		fmt.Sprintf("This server is %s, running TeaNode %s.", configuration.Server.Name, version.Version()),
	}, "\n")
}

// ---- what a turn in a chat says while it runs -----------------------------

// The pace of what the others see of a turn.
const (
	// chatTextEvery is the most often streamed words go out: one event for
	// each few words would be a notification between instances for each.
	chatTextEvery = 250 * time.Millisecond

	// chatThinkingEvery is how often "is thinking" is said again; a client
	// shows it for longer than this and drops it when the answer comes.
	chatThinkingEvery = 4 * time.Second

	// chatMembersFresh is how long the members a turn names its events to are
	// kept: a person taken out of the chat stops hearing the turn within it.
	chatMembersFresh = 2 * time.Second
)

// chatRunProgress is what a turn in a chat keeps while it runs.
type chatRunProgress struct {
	mutex sync.Mutex

	// pending is the streamed words not yet told, and flushedAt when words
	// last went out. hasStreamed says words went out that no stored message
	// has replaced yet, which a screen draws as a provisional line.
	pending     strings.Builder
	flushedAt   time.Time
	hasStreamed bool

	// members are who is in the chat, as of readAt.
	members []*models.ChatMember
	readAt  time.Time
}

// chatMembers are the people in the chat now.
func (self *AskRun) chatMembers() []*models.ChatMember {
	progress := &self.chatProgress
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	if progress.members != nil && time.Since(progress.readAt) < chatMembersFresh {
		return progress.members
	}
	var chat *models.Chat
	if err := self.agent.settings.Database.TransactionContext(self.agent.ctx, func(tx db.Transaction) (err error) {
		chat, err = tx.GetChat(self.settings.Chat.ChatID)
		return err
	}); err != nil || chat == nil {
		return progress.members
	}
	progress.members, progress.readAt = chat.ActiveMembers(), time.Now()
	return progress.members
}

// chatOwnerName is what the chat calls the turn's owner, the name that tells
// their agent from another person's of the same name. The owner's own name
// when the chat cannot be read.
func (self *AskRun) chatOwnerName() string {
	for _, member := range self.chatMembers() {
		if member.UserID == self.settings.Owner.ID && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	return self.settings.Owner.DisplayName()
}

// tellChat publishes an event to the conversation of each member the build
// function names, in the chat hub.
func (self *AskRun) tellChat(build func(member *models.ChatMember) Event) {
	events := []Event{}
	for _, member := range self.chatMembers() {
		if member.ConversationID == "" {
			continue
		}
		event := build(member)
		if event.Kind == "" {
			continue
		}
		event.ConversationID = member.ConversationID
		events = append(events, event)
	}
	if len(events) > 0 {
		self.agent.PublishChatEvents(events)
	}
}

// streamChatText tells every member the words as they come, as a message that
// the stored one replaces.
func (self *AskRun) streamChatText(delta string) {
	progress := &self.chatProgress
	progress.mutex.Lock()
	progress.pending.WriteString(delta)
	var text string
	if time.Since(progress.flushedAt) >= chatTextEvery {
		text = progress.pending.String()
		progress.pending.Reset()
		progress.flushedAt = time.Now()
		progress.hasStreamed = progress.hasStreamed || text != ""
	}
	progress.mutex.Unlock()
	if text == "" {
		return
	}
	agentId, agentName, ownerName := self.settings.Agent.ID, self.settings.Agent.DisplayName(), self.chatOwnerName()
	self.tellChat(func(*models.ChatMember) Event {
		return Event{Kind: EventText, RunID: self.ID, Text: text, Name: agentName, AuthorAgentID: agentId, AuthorOwnerName: ownerName}
	})
}

// ChatStreamingKey is the identifier of the provisional line a screen draws
// the words of a run as while they come, before the stored message replaces
// it. The dashboard and the iPhone app build the same name from the run's
// identifier (streamingKeyOf, streamingId).
func ChatStreamingKey(runId string) string {
	return "streaming-" + runId
}

// forgetChatText drops the words not yet told: the message they were part of
// is stored and told whole, which replaces the provisional line on a screen.
func (self *AskRun) forgetChatText() {
	progress := &self.chatProgress
	progress.mutex.Lock()
	progress.pending.Reset()
	progress.hasStreamed = false
	progress.mutex.Unlock()
}

// retractChatText takes the provisional line off every member's screen when
// the words that were streamed will not be stored as a message: the turn was
// stopped, it failed, or what it said came to nothing once the hidden markers
// were taken out. Without it the words stay on screens as a line no message
// ever replaces, until each person reloads. It is a deleted event naming the
// provisional line, which the clients take off rather than mark deleted.
func (self *AskRun) retractChatText() {
	if self.settings.Chat == nil {
		return
	}
	progress := &self.chatProgress
	progress.mutex.Lock()
	hasStreamed := progress.hasStreamed
	progress.pending.Reset()
	progress.hasStreamed = false
	progress.mutex.Unlock()
	if !hasStreamed {
		return
	}
	key := ChatStreamingKey(self.ID)
	self.tellChat(func(*models.ChatMember) Event {
		return Event{Kind: EventDeleted, MessageID: key}
	})
}

// startThinkingInChat tells every member but the owner, whose own screen
// shows the turn, that the agent is thinking, now and every few seconds until
// the function returned is called. Nobody learns which tool it is using.
func (self *AskRun) startThinkingInChat() (stop func()) {
	if self.settings.Chat == nil {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	tell := func() {
		agentId, agentName, ownerId, ownerName := self.settings.Agent.ID, self.settings.Agent.DisplayName(), self.settings.Owner.ID, self.chatOwnerName()
		self.tellChat(func(member *models.ChatMember) Event {
			if member.UserID == ownerId {
				return Event{}
			}
			return Event{Kind: EventTyping, RunID: self.ID, Name: agentName, AuthorAgentID: agentId, AuthorOwnerName: ownerName}
		})
	}
	self.agent.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.agent.waitGroup.Done()
		tell()
		ticker := time.NewTicker(chatThinkingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tell()
			case <-done:
				return
			case <-self.ctx.Done():
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// ---- what a turn says in the chat ----------------------------------------

// postedChatMessage is a message the turn has stored in the chat, to be told
// once the transaction has committed.
type postedChatMessage struct {
	message *models.ChatMessage
	chat    *models.Chat
}

// postChatAnswer stores the words of a round as a message of the chat,
// written by the owner's agent under its name. Nil for a round that said
// nothing. The owner is still in the chat or it is an error: an agent speaks
// for a member.
func (self *AskRun) postChatAnswer(tx db.Transaction, content string) (*postedChatMessage, error) {
	content = strings.TrimSpace(models.StripHiddenMarkers(content))
	if content == "" {
		return nil, nil
	}
	settings := self.settings
	chat, err := tx.GetChat(settings.Chat.ChatID)
	if err != nil {
		return nil, err
	}
	if chat == nil || chat.Member(settings.Owner.ID) == nil || chat.Member(settings.Owner.ID).IsRemoved {
		return nil, fmt.Errorf("the person is no longer in this chat")
	}
	stored, err := tx.AppendChatMessage(&models.ChatMessage{
		ChatID:        chat.ID,
		AuthorUserID:  settings.Owner.ID,
		AuthorName:    settings.Agent.DisplayName(),
		AuthorAgentID: settings.Agent.ID,
		AgentRunID:    self.ID,
		Body:          cutRunes(content, models.ChatMessageMostCharacters),
	})
	if err != nil {
		return nil, err
	}
	return &postedChatMessage{message: stored, chat: chat}, nil
}

// announceChatAnswer tells every member what the agent said, once it is
// stored, and the phones of those who are not looking. A message an agent
// wrote starts no turn of any agent.
func (self *AskRun) announceChatAnswer(posted *postedChatMessage) {
	if posted == nil {
		// A round that said nothing storable: words already streamed for it
		// have no message to be replaced by.
		self.retractChatText()
		return
	}
	self.forgetChatText()
	settings := self.settings
	shown := db.ChatMessageAsAgentMessage(posted.message, "", nil)
	shown.AuthorAvatarColorIndex = avatar.InitialColorIndex(settings.Owner.ID)
	events := []Event{}
	recipients := []ChatRecipient{}
	for _, member := range posted.chat.Members {
		if member.IsRemoved || member.ConversationID == "" {
			continue
		}
		copied := *shown
		copied.ConversationID = member.ConversationID
		copied.IsMine = member.UserID == settings.Owner.ID
		events = append(events, Event{Kind: EventSaid, ConversationID: member.ConversationID, MessageID: shown.ID, Message: &copied})
		recipients = append(recipients, ChatRecipient{UserID: member.UserID, AgentID: member.AgentID, ConversationID: member.ConversationID, MutedUntil: member.MutedUntil})
	}
	if self.agent.settings.ChatEvents == nil {
		return
	}
	self.agent.PublishChatEvents(events)
	notified := *shown
	self.agent.settings.ChatEvents.NotifyChatMessage(&notified, db.ChatViewOf(posted.chat, settings.Owner.ID).Title, posted.chat.ChatKind == models.ChatKindGroup, recipients)
}

// tellAskerItFailed tells somebody who asked another person's agent in a
// chat that it could not answer, in a note only they see. Nothing of what went
// wrong is told: it is the owner's turn they have the use of, not the owner's
// business.
func (self *AskRun) tellAskerItFailed() {
	settings := self.settings
	if !settings.isMemberTurn() {
		return
	}
	for _, member := range self.chatMembers() {
		if member.UserID != settings.Chat.AskerUserID || member.ConversationID == "" {
			continue
		}
		if err := self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
			_, err := tx.AppendAgentMessage(models.NewAgentNote(member.ConversationID, models.NoteFailed, ""))
			return err
		}); err != nil {
			log.Warningf("cannot tell the person who asked that the answer failed: %s", err)
			return
		}
		self.agent.PublishChatEvents([]Event{{
			Kind: EventNote, ConversationID: member.ConversationID, Note: models.NoteText(models.NoteFailed, ""),
			NoteKind: string(models.NoteFailed),
		}})
		return
	}
}
