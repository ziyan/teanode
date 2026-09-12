// Package channel is the primary conversation on a chat app: a person's
// own bot in Telegram or Discord, run by the server, through which they
// talk to their agent. The manager runs one bot per channel row — on
// exactly one of the server's instances, by a claim on the row — and the
// turn is the same for every app: a linked chat's message starts a turn
// on the main conversation, the bot types while it works, streams the
// answer into a message it keeps editing, sends what the agent made, asks
// on the same cards the drawer shows, and answers a handful of commands.
package channel

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

var log = logging.MustGetLogger("channel")

// The rhythm of the manager and of a turn.
const (
	// tickEvery is how often the manager looks at the channel rows.
	tickEvery = 30 * time.Second
	// claimFor is how long a claim on a row holds unless renewed.
	claimFor = 90 * time.Second
	// typingEvery is how often the bot says it is typing while a turn
	// runs; Telegram shows it for five seconds, Discord for ten.
	typingEvery = 4 * time.Second
	// previewEvery is how often the streamed answer is edited into the
	// preview message: often enough to read along, seldom enough for the
	// app's limits.
	previewEvery = 1200 * time.Millisecond
)

// Chat is a linked chat as the turn writes into it.
type Chat interface {
	// Send sends a message, replying to one when replyTo is given, and
	// returns the new message's id.
	Send(ctx context.Context, text, replyTo string) (string, error)
	// Edit replaces a message's text.
	Edit(ctx context.Context, messageId, text string) error
	// Delete removes a message of the bot's.
	Delete(ctx context.Context, messageId string) error
	// Typing shows the bot typing.
	Typing(ctx context.Context) error
	// SendFile sends a file, with a caption.
	SendFile(ctx context.Context, name, contentType string, content []byte, caption string) error
	// Limit is the most characters one message may carry.
	Limit() int
}

// Incoming is a message the bot received.
type Incoming struct {
	// ChatID is where it was said and what a link names; ChatName what
	// to call that.
	ChatID   string
	ChatName string
	// SenderID and SenderName are who said it.
	SenderID   string
	SenderName string
	MessageID  string
	Text       string
	// Group says it came from a group rather than a private chat, and
	// ToBot whether it was addressed to the bot there — a reply to it, a
	// mention.
	Group bool
	ToBot bool
	Files []IncomingFile
}

// IncomingFile is a file on a message, fetched when wanted.
type IncomingFile struct {
	Name        string
	ContentType string
	Fetch       func(ctx context.Context) ([]byte, error)
}

// Bot is one app's connection for one token: it delivers what the bot
// hears to the handler, with a Chat to answer through.
type Bot interface {
	// Name is what the app calls the bot.
	Name() string
	// Run delivers messages until the context ends or the connection
	// fails for good.
	Run(ctx context.Context, handle func(ctx context.Context, incoming *Incoming, chat Chat)) error
}

// Opener makes a Bot for a token.
type Opener func(ctx context.Context, token string) (Bot, error)

// Settings is what the manager needs.
type Settings struct {
	Worker        *agent.Agent
	Database      db.Database
	Storage       storage.Storage
	Configuration func() *config.Configuration
	// Instance names this server instance, for the claims.
	Instance string
	// Openers make a bot per app kind.
	Openers map[models.AgentChannelKind]Opener
}

// Manager runs the bots.
type Manager struct {
	settings *Settings
	ctx      context.Context
	cancel   context.CancelFunc
	wait     sync.WaitGroup

	mutex   sync.Mutex
	running map[string]*runningBot // by channel id
}

type runningBot struct {
	channel  *models.AgentChannel
	modified time.Time
	cancel   context.CancelFunc
	done     chan struct{}
}

// New makes a manager; Start runs it.
func New(settings *Settings) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{settings: settings, ctx: ctx, cancel: cancel, running: map[string]*runningBot{}}
}

// Start looks at the rows now and every tick after.
func (self *Manager) Start() {
	self.wait.Add(1)
	go func() {
		defer self.wait.Done()
		self.tick()
		ticker := time.NewTicker(tickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				self.tick()
			case <-self.ctx.Done():
				return
			}
		}
	}()
}

// Stop ends every bot and gives the claims up.
func (self *Manager) Stop() {
	self.cancel()
	self.wait.Wait()
	self.mutex.Lock()
	bots := self.running
	self.running = map[string]*runningBot{}
	self.mutex.Unlock()
	for id, bot := range bots {
		bot.cancel()
		<-bot.done
		_ = self.settings.Database.Transaction(func(tx db.Transaction) error {
			return tx.ReleaseAgentChannel(id, self.settings.Instance)
		})
	}
}

// Poke looks at the rows now rather than at the next tick: after a
// person changed something.
func (self *Manager) Poke() {
	go self.tick()
}

// tick claims what should run here, starts what is claimed and not
// running, restarts what changed, and stops what is no longer ours.
func (self *Manager) tick() {
	if self.ctx.Err() != nil {
		return
	}
	if !agent.FeatureAllowed(self.settings.Configuration(), "chatApps") {
		self.stopAll()
		return
	}
	var channels []*models.AgentChannel
	if err := self.settings.Database.Transaction(func(tx db.Transaction) (err error) {
		channels, err = tx.ListEnabledAgentChannels()
		return err
	}); err != nil {
		log.Warningf("cannot list the chat apps: %s", err)
		return
	}
	wanted := map[string]*models.AgentChannel{}
	for _, channel := range channels {
		if _, known := self.settings.Openers[channel.Kind]; !known {
			continue
		}
		claimed := false
		if err := self.settings.Database.Transaction(func(tx db.Transaction) (err error) {
			claimed, err = tx.ClaimAgentChannel(channel.ID, self.settings.Instance, time.Now().Add(claimFor))
			return err
		}); err != nil {
			log.Warningf("cannot claim the %s bot of agent %s: %s", channel.Kind, channel.AgentID, err)
			continue
		}
		if claimed {
			wanted[channel.ID] = channel
		}
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id, bot := range self.running {
		channel, still := wanted[id]
		if !still || !channel.ModifiedAt.Equal(bot.modified) {
			bot.cancel()
			delete(self.running, id)
		}
	}
	for id, channel := range wanted {
		if _, running := self.running[id]; running {
			continue
		}
		self.start(channel)
	}
}

func (self *Manager) stopAll() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id, bot := range self.running {
		bot.cancel()
		delete(self.running, id)
	}
}

// start runs one bot until it stops or is stopped; what ends it is noted
// on the row for the person to see.
func (self *Manager) start(channel *models.AgentChannel) {
	ctx, cancel := context.WithCancel(self.ctx)
	bot := &runningBot{channel: channel, modified: channel.ModifiedAt, cancel: cancel, done: make(chan struct{})}
	self.running[channel.ID] = bot
	self.wait.Add(1)
	go func() {
		defer self.wait.Done()
		defer close(bot.done)
		err := self.run(ctx, channel)
		if err != nil && ctx.Err() == nil {
			log.Warningf("the %s bot of agent %s stopped: %s", channel.Kind, channel.AgentID, err)
			self.note(channel.ID, "", err.Error(), nil)
		}
		self.mutex.Lock()
		if self.running[channel.ID] == bot {
			delete(self.running, channel.ID)
		}
		self.mutex.Unlock()
		if ctx.Err() != nil {
			_ = self.settings.Database.Transaction(func(tx db.Transaction) error {
				return tx.ReleaseAgentChannel(channel.ID, self.settings.Instance)
			})
		}
	}()
}

func (self *Manager) note(channelId, botName, lastError string, seenAt *time.Time) {
	_ = self.settings.Database.Transaction(func(tx db.Transaction) error {
		return tx.NoteAgentChannel(channelId, botName, lastError, seenAt)
	})
}

// run opens the bot and serves it.
func (self *Manager) run(ctx context.Context, channel *models.AgentChannel) error {
	token, err := self.settings.Worker.OpenSecret(channel.Token)
	if err != nil {
		return fmt.Errorf("the token could not be opened: %w", err)
	}
	bot, err := self.settings.Openers[channel.Kind](ctx, token)
	if err != nil {
		return err
	}
	now := time.Now()
	self.note(channel.ID, bot.Name(), "", &now)
	log.Noticef("the %s bot %q of agent %s is running on %s", channel.Kind, bot.Name(), channel.AgentID, self.settings.Instance)
	conversation := &chatState{manager: self, channelId: channel.ID, agentId: channel.AgentID, kind: channel.Kind, bot: bot}
	return bot.Run(ctx, conversation.handle)
}

// chatState is one bot's side of the conversation: the card waiting for
// an answer, the turn under way.
type chatState struct {
	manager   *Manager
	channelId string
	agentId   string
	kind      models.AgentChannelKind
	bot       Bot

	mutex   sync.Mutex
	pending *pendingCard
	turning bool
}

type pendingCard struct {
	run      *agent.AskRun
	callId   string
	question bool
}

// handle is what the bot heard: a link, a command, a card's answer, or
// a turn.
func (self *chatState) handle(ctx context.Context, incoming *Incoming, chat Chat) {
	channel, err := self.channel()
	if err != nil || channel == nil {
		return
	}
	text := strings.TrimSpace(incoming.Text)
	name, arguments := parseCommand(text)
	if !channel.Linked() {
		if name == "link" {
			self.link(ctx, channel, incoming, chat, arguments)
			return
		}
		if incoming.Group && !incoming.ToBot && name == "" {
			return
		}
		_, _ = chat.Send(ctx, "This bot is not linked to a chat yet. On your agent page, under Chat apps, find the code, and send it here as: /link CODE", incoming.MessageID)
		return
	}
	if incoming.ChatID != channel.LinkedID {
		if incoming.Group && !incoming.ToBot {
			return
		}
		_, _ = chat.Send(ctx, "This bot is linked to another chat. Unlink it on the agent page to link this one.", incoming.MessageID)
		return
	}
	if incoming.Group && !incoming.ToBot && name != "ask" && name == "" {
		// In a group the bot answers what is said to it, not everything.
		return
	}
	self.manager.note(self.channelId, "", "", ptr(time.Now()))
	if name == "ask" {
		text = arguments
		name = ""
	}
	if name != "" {
		self.command(ctx, channel, incoming, chat, name, arguments)
		return
	}
	// A card waits: this is its answer.
	self.mutex.Lock()
	pending := self.pending
	self.mutex.Unlock()
	if pending != nil {
		self.answerCard(ctx, pending, chat, incoming, text)
		return
	}
	self.mutex.Lock()
	if self.turning {
		self.mutex.Unlock()
		_, _ = chat.Send(ctx, "Still working on the last one; /stop ends it.", incoming.MessageID)
		return
	}
	self.turning = true
	self.mutex.Unlock()
	defer func() {
		self.mutex.Lock()
		self.turning = false
		self.mutex.Unlock()
	}()
	self.turn(ctx, channel, incoming, chat, text)
}

func ptr[T any](value T) *T { return &value }

func (self *chatState) channel() (*models.AgentChannel, error) {
	var channel *models.AgentChannel
	err := self.manager.settings.Database.Transaction(func(tx db.Transaction) (err error) {
		channel, err = tx.GetAgentChannel(self.agentId, self.kind)
		return err
	})
	if channel != nil && channel.ID != self.channelId {
		channel = nil
	}
	return channel, err
}

// link makes the chat the one the bot answers, when the code is right.
func (self *chatState) link(ctx context.Context, channel *models.AgentChannel, incoming *Incoming, chat Chat, code string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || code != channel.LinkCode {
		_, _ = chat.Send(ctx, "That is not the code. It is on your agent page, under Chat apps.", incoming.MessageID)
		return
	}
	name := incoming.ChatName
	if name == "" {
		name = incoming.SenderName
	}
	if err := self.manager.settings.Database.Transaction(func(tx db.Transaction) error {
		channel.LinkedID = incoming.ChatID
		channel.LinkedName = name
		channel.LinkCode = ""
		_, err := tx.PutAgentChannel(channel)
		return err
	}); err != nil {
		_, _ = chat.Send(ctx, "The link could not be saved: "+err.Error(), incoming.MessageID)
		return
	}
	log.Noticef("the %s bot of agent %s was linked to %q", channel.Kind, channel.AgentID, name)
	_, _ = chat.Send(ctx, "Linked. Say something, and your agent answers here.\n\n"+helpText(), incoming.MessageID)
}

// command answers a slash command.
func (self *chatState) command(ctx context.Context, channel *models.AgentChannel, incoming *Incoming, chat Chat, name, arguments string) {
	reply := ""
	switch name {
	case "start", "help":
		reply = "Your agent is here. Say something, or send a file or a photo with a word about it.\n\n" + helpText()
	case "link":
		reply = "This chat is linked already."
	case "unlink":
		if err := self.manager.settings.Database.Transaction(func(tx db.Transaction) error {
			channel.LinkedID, channel.LinkedName = "", ""
			channel.LinkCode = newCode()
			_, err := tx.PutAgentChannel(channel)
			return err
		}); err != nil {
			reply = "The link could not be dropped: " + err.Error()
		} else {
			reply = "Unlinked. A new code is on your agent page."
		}
	case "stop":
		found, owner, err := self.person(channel)
		if err != nil {
			reply = err.Error()
			break
		}
		conversation, err := self.mainConversation(found)
		if err != nil {
			reply = err.Error()
			break
		}
		_ = owner
		self.manager.settings.Worker.StopConversation(conversation.ID)
		self.mutex.Lock()
		self.pending = nil
		self.mutex.Unlock()
		reply = "Stopped."
	case "new":
		found, _, err := self.person(channel)
		if err != nil {
			reply = err.Error()
			break
		}
		if err := self.freshMain(found); err != nil {
			reply = "A fresh conversation could not be started: " + err.Error()
		} else {
			reply = "A fresh primary conversation. The old one is kept, named, in the drawer."
		}
	case "status":
		found, owner, err := self.person(channel)
		if err != nil {
			reply = err.Error()
			break
		}
		conversation, err := self.mainConversation(found)
		if err != nil {
			reply = err.Error()
			break
		}
		self.mutex.Lock()
		turning, pending := self.turning, self.pending != nil
		self.mutex.Unlock()
		state := "idle"
		if pending {
			state = "waiting for your answer"
		} else if turning {
			state = "working"
		}
		title := conversation.Title
		if title == "" {
			title = "the primary conversation"
		}
		reply = fmt.Sprintf("Agent: %s, %s's\nConversation: %s\nApp: %s, bot %s\nState: %s", found.Name, owner.Name, title, self.kind, self.bot.Name(), state)
	default:
		reply = "Not a command I know.\n\n" + helpText()
	}
	if reply != "" {
		_, _ = chat.Send(ctx, reply, incoming.MessageID)
	}
}

// person is the agent and its owner.
func (self *chatState) person(channel *models.AgentChannel) (*models.Agent, *models.User, error) {
	var found *models.Agent
	var owner *models.User
	err := self.manager.settings.Database.Transaction(func(tx db.Transaction) (err error) {
		found, err = tx.GetAgent(channel.AgentID)
		if err != nil || found == nil {
			return errors.New("the agent is gone")
		}
		owner, err = tx.GetUser(found.UserID)
		if err != nil || owner == nil {
			return errors.New("the person is gone")
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !found.Active() || owner.Disabled() {
		return nil, nil, errors.New("the agent is off")
	}
	return found, owner, nil
}

// mainConversation is the one continuous conversation, made the first
// time it is needed.
func (self *chatState) mainConversation(found *models.Agent) (*models.AgentConversation, error) {
	var conversation *models.AgentConversation
	err := self.manager.settings.Database.Transaction(func(tx db.Transaction) error {
		conversations, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		if len(conversations) > 0 {
			conversation = conversations[0]
			return nil
		}
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		return err
	})
	return conversation, err
}

// freshMain starts a new primary conversation; the old one becomes a
// named one, due for a title.
func (self *chatState) freshMain(found *models.Agent) error {
	return self.manager.settings.Database.Transaction(func(tx db.Transaction) error {
		conversations, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		for _, previous := range conversations {
			if _, err := tx.UpdateAgentConversation(previous.ID, func(conversation *models.AgentConversation) error {
				conversation.Kind = models.AgentConversationNamed
				conversation.DescribedAt = nil
				return nil
			}); err != nil {
				return err
			}
		}
		_, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		return err
	})
}

// answerCard takes the person's reply to a waiting card.
func (self *chatState) answerCard(ctx context.Context, pending *pendingCard, chat Chat, incoming *Incoming, text string) {
	if pending.question {
		if pending.run.Answer(pending.callId, text) {
			self.clearPending(pending)
			return
		}
		self.clearPending(pending)
		_, _ = chat.Send(ctx, "That question is no longer waiting.", incoming.MessageID)
		return
	}
	approve, understood := yesOrNo(text)
	if !understood {
		_, _ = chat.Send(ctx, "Reply yes or no.", incoming.MessageID)
		return
	}
	if !pending.run.Resolve(pending.callId, approve) {
		_, _ = chat.Send(ctx, "That card is no longer waiting.", incoming.MessageID)
	}
	self.clearPending(pending)
}

func (self *chatState) clearPending(pending *pendingCard) {
	self.mutex.Lock()
	if self.pending == pending {
		self.pending = nil
	}
	self.mutex.Unlock()
}

// yesOrNo reads an answer to a card.
func yesOrNo(text string) (approve, understood bool) {
	switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "/"))) {
	case "yes", "y", "ok", "okay", "sure", "go", "do it", "approve", "confirm", "👍":
		return true, true
	case "no", "n", "nope", "cancel", "stop", "don't", "dont", "deny", "👎":
		return false, true
	}
	return false, false
}

// turn is a message of the person's, answered by the agent.
func (self *chatState) turn(ctx context.Context, channel *models.AgentChannel, incoming *Incoming, chat Chat, text string) {
	found, owner, err := self.person(channel)
	if err != nil {
		_, _ = chat.Send(ctx, err.Error(), incoming.MessageID)
		return
	}
	attachments, err := self.attachments(ctx, found, incoming)
	if err != nil {
		_, _ = chat.Send(ctx, "A file could not be taken: "+err.Error(), incoming.MessageID)
		return
	}
	if text == "" && len(attachments) == 0 {
		return
	}
	if text == "" {
		text = "(a file, with nothing said about it)"
	}
	conversation, err := self.mainConversation(found)
	if err != nil {
		_, _ = chat.Send(ctx, "The conversation could not be found: "+err.Error(), incoming.MessageID)
		return
	}
	operations, err := self.manager.settings.Worker.OperationsFor(ctx, owner)
	if err != nil {
		_, _ = chat.Send(ctx, err.Error(), incoming.MessageID)
		return
	}
	run, err := self.manager.settings.Worker.Ask(&agent.AskSettings{
		Agent: found, Owner: owner, Operations: operations, Conversation: conversation,
		Message: text, Surface: string(self.kind), Attachments: attachments,
	})
	if err != nil {
		_, _ = chat.Send(ctx, "The agent could not start: "+err.Error(), incoming.MessageID)
		return
	}
	_ = chat.Typing(ctx)
	self.follow(ctx, run, chat, incoming)
}

// follow carries the run's events into the chat.
func (self *chatState) follow(ctx context.Context, run *agent.AskRun, chat Chat, incoming *Incoming) {
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	live := &preview{chat: chat, replyTo: incoming.MessageID}
	typing := time.NewTicker(typingEvery)
	defer typing.Stop()
	var artifacts []string
	answered := false
	for {
		select {
		case <-typing.C:
			if !answered {
				_ = chat.Typing(ctx)
			}
		case event, open := <-events:
			if !open {
				live.finish(ctx, "")
				self.sendArtifacts(ctx, chat, artifacts)
				return
			}
			switch event.Kind {
			case agent.EventText:
				live.update(ctx, event.Text)
			case agent.EventMessage:
				answered = true
				live.finish(ctx, event.Text)
				live = &preview{chat: chat}
			case agent.EventToolCall:
				live.reset(ctx)
				_ = chat.Typing(ctx)
			case agent.EventToolResult:
				if id := artifactIn(event.Text); id != "" {
					artifacts = append(artifacts, id)
				}
			case agent.EventConfirmation:
				live.finish(ctx, "")
				self.mutex.Lock()
				self.pending = &pendingCard{run: run, callId: event.CallID}
				self.mutex.Unlock()
				card := event.Note
				if event.Risk != "" {
					card += " (" + event.Risk + ")"
				}
				_, _ = chat.Send(ctx, "The agent needs your word: "+card+"\n\nReply yes or no.", "")
			case agent.EventQuestion:
				live.finish(ctx, "")
				self.mutex.Lock()
				self.pending = &pendingCard{run: run, callId: event.CallID, question: true}
				self.mutex.Unlock()
				question := "The agent asks: " + event.Note
				if strings.TrimSpace(event.Text) != "" {
					question += "\n- " + strings.ReplaceAll(strings.TrimSpace(event.Text), "\n", "\n- ")
				}
				_, _ = chat.Send(ctx, question, "")
			case agent.EventNote:
				if strings.HasPrefix(event.Note, "stopped") {
					_, _ = chat.Send(ctx, "("+event.Note+")", "")
				}
			case agent.EventError:
				live.finish(ctx, "")
				_, _ = chat.Send(ctx, "Something went wrong: "+event.Error, incoming.MessageID)
			case agent.EventDone:
				live.finish(ctx, "")
				self.sendArtifacts(ctx, chat, artifacts)
				self.mutex.Lock()
				self.pending = nil
				self.mutex.Unlock()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// artifactMessage marks an attachment as an artifact of the conversation
// rather than a file somebody handed over, which is what the server will
// serve from a signed address.
const artifactMessage = "artifact"

// artifactIn is the id of an artifact a tool's answer made, or of a file
// it handed the person, if one.
func artifactIn(result string) string {
	var made struct {
		ArtifactID   string `json:"artifact_id"`
		AttachmentID string `json:"attachment_id"`
	}
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "{") {
		return ""
	}
	if json.Unmarshal([]byte(trimmed), &made) != nil {
		return ""
	}
	if made.ArtifactID != "" {
		return made.ArtifactID
	}
	return made.AttachmentID
}

// sendArtifacts sends what the agent made or handed over during the
// turn: a picture, a video or a document as a file, and a page as a link. A chat app shows an
// attached page as a file to download and never runs its script, so a
// chart in it would be blank; the link opens it in the browser, drawn,
// without a sign-in, for thirty days.
func (self *chatState) sendArtifacts(ctx context.Context, chat Chat, ids []string) {
	for _, id := range ids {
		var attachment *models.AgentAttachment
		if err := self.manager.settings.Database.Transaction(func(tx db.Transaction) (err error) {
			attachment, err = tx.GetAgentAttachment(id)
			return err
		}); err != nil || attachment == nil {
			continue
		}
		// Only a page the agent made opens from a link: that is what the
		// address the server signs will serve. A page handed over from
		// somewhere else goes as a file, which is what the apps do with
		// one anyway.
		if strings.HasPrefix(attachment.ContentType, "text/html") && attachment.MessageID == artifactMessage {
			if link := self.manager.artifactLink(attachment.ID); link != "" {
				if _, err := chat.Send(ctx, attachment.Name+"\n"+link, ""); err != nil {
					log.Warningf("cannot send the link to %s to the chat: %s", attachment.Name, err)
				}
				continue
			}
		}
		content, err := self.manager.settings.Storage.GetFile(ctx, attachment.ID)
		if err != nil {
			continue
		}
		if err := chat.SendFile(ctx, attachment.Name, attachment.ContentType, content, attachment.Name); err != nil {
			log.Warningf("cannot send %s to the chat: %s", attachment.Name, err)
		}
	}
}

// artifactLink is the address a chat app opens an artifact at, or ""
// when the server cannot sign one. The host is the dashboard's: the
// name passkeys are bound to when the operator set one, the server's
// own name otherwise.
func (self *Manager) artifactLink(attachmentId string) string {
	configuration := self.settings.Configuration()
	host := strings.TrimSpace(configuration.Passkey.RelyingPartyID)
	if host == "" {
		host = strings.TrimSpace(configuration.Server.Name)
	}
	share := self.settings.Worker.ShareAttachment(attachmentId, time.Now().Add(agent.ShareFor))
	if host == "" || share == "" {
		return ""
	}
	return "https://" + host + "/api/v1/agent/attachments/" + attachmentId + "?share=" + share
}

// attachments takes the files on a message into the turn.
func (self *chatState) attachments(ctx context.Context, found *models.Agent, incoming *Incoming) ([]*models.AgentAttachment, error) {
	var attachments []*models.AgentAttachment
	for _, file := range incoming.Files {
		content, err := file.Fetch(ctx)
		if err != nil {
			return nil, err
		}
		contentType := file.ContentType
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		var created *models.AgentAttachment
		if err := self.manager.settings.Database.Transaction(func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
				AgentID: found.ID, Name: file.Name, ContentType: contentType, Size: int64(len(content)),
				Text: agent.ExtractAttachmentText(file.Name, contentType, content),
			})
			if err != nil {
				return err
			}
			return self.manager.settings.Storage.PutFile(ctx, created.ID, content)
		}); err != nil {
			return nil, err
		}
		attachments = append(attachments, created)
	}
	return attachments, nil
}

// preview is the message the streamed answer is edited into, then
// replaced by the whole answer.
type preview struct {
	chat      Chat
	replyTo   string
	messageId string
	text      string
	lastEdit  time.Time
	shown     string
}

func (self *preview) update(ctx context.Context, delta string) {
	self.text += delta
	if time.Since(self.lastEdit) < previewEvery {
		return
	}
	self.flush(ctx)
}

func (self *preview) flush(ctx context.Context) {
	text := strings.TrimSpace(self.text)
	if text == "" || text == self.shown {
		return
	}
	limit := self.chat.Limit() - 2
	if len(text) > limit {
		text = text[:cut(text, limit)]
	}
	self.lastEdit = time.Now()
	self.shown = text
	if self.messageId == "" {
		id, err := self.chat.Send(ctx, text+" …", self.replyTo)
		if err == nil {
			self.messageId = id
		}
		return
	}
	_ = self.chat.Edit(ctx, self.messageId, text+" …")
}

// reset drops the preview: what came before a tool call was thinking
// aloud, and the answer starts over after it.
func (self *preview) reset(ctx context.Context) {
	if self.messageId != "" {
		_ = self.chat.Delete(ctx, self.messageId)
	}
	self.messageId, self.text, self.shown = "", "", ""
}

// finish puts the whole answer in: the preview edited to the first part
// of it, the rest sent after, or the preview alone when nothing final
// came.
func (self *preview) finish(ctx context.Context, whole string) {
	whole = strings.TrimSpace(whole)
	if whole == "" {
		whole = strings.TrimSpace(self.text)
	}
	if whole == "" {
		if self.messageId != "" {
			_ = self.chat.Delete(ctx, self.messageId)
		}
		self.messageId, self.text, self.shown = "", "", ""
		return
	}
	limit := self.chat.Limit()
	first := whole
	rest := ""
	if len(first) > limit {
		at := cut(first, limit)
		first, rest = whole[:at], whole[at:]
	}
	if self.messageId != "" {
		if err := self.chat.Edit(ctx, self.messageId, first); err != nil {
			_, _ = self.chat.Send(ctx, first, self.replyTo)
		}
	} else {
		_, _ = self.chat.Send(ctx, first, self.replyTo)
	}
	for rest != "" {
		part := rest
		if len(part) > limit {
			part = rest[:cut(rest, limit)]
		}
		_, _ = self.chat.Send(ctx, part, "")
		rest = rest[len(part):]
	}
	self.messageId, self.text, self.shown = "", "", ""
}

// cut is where to end a part of text within limit bytes: at a line
// break when one is near, never inside a character.
func cut(text string, limit int) int {
	if len(text) <= limit {
		return len(text)
	}
	at := limit
	if index := strings.LastIndex(text[:limit], "\n"); index > limit/2 {
		at = index + 1
	} else if index := strings.LastIndex(text[:limit], " "); index > limit/2 {
		at = index + 1
	}
	for at > 0 && at < len(text) && (text[at]&0xC0) == 0x80 {
		at--
	}
	if at == 0 {
		at = limit
	}
	return at
}

// The commands a chat may send.
var commands = []struct{ name, description string }{
	{"new", "start a fresh primary conversation; the old one is kept"},
	{"stop", "stop what the agent is doing"},
	{"status", "which agent, which conversation, what it is doing"},
	{"unlink", "drop this chat's link to the bot"},
	{"help", "these commands"},
}

// Commands are the commands by name, and their order, for an app's menu.
func Commands() (map[string]string, []string) {
	described := map[string]string{}
	var order []string
	for _, command := range commands {
		described[command.name] = command.description
		order = append(order, command.name)
	}
	return described, order
}

func helpText() string {
	var builder strings.Builder
	builder.WriteString("Commands:\n")
	for _, command := range commands {
		fmt.Fprintf(&builder, "/%s — %s\n", command.name, command.description)
	}
	builder.WriteString("In a group, /ask <words> talks to the agent; a reply to the bot does too.")
	return builder.String()
}

// parseCommand reads "/name arguments", with a "@bot" suffix on the name
// allowed, as a chat app sends it.
func parseCommand(text string) (name, arguments string) {
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	parts := strings.SplitN(text[1:], " ", 2)
	name = strings.ToLower(parts[0])
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	if len(parts) > 1 {
		arguments = strings.TrimSpace(parts[1])
	}
	if name == "" {
		return "", ""
	}
	return name, arguments
}

// newCode is a link code drawn anew: six characters from an alphabet
// without look-alikes.
func newCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	code := make([]byte, len(bytes))
	for index, value := range bytes {
		code[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(code)
}
