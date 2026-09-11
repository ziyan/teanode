package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ziyan/teanode/internal/agent/tools"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
	"github.com/ziyan/teanode/internal/version"
)

// Ask is the person talking to their agent: a conversation kept in the
// database, a prompt built fresh each round from layers that change at
// different rates, and rounds of model → tool calls → results until the
// model answers in words. The loop is the only place the agent reaches a
// write, a destructive or an outward tool, and the last two never run
// without the person's word.

// AskSettings is one turn of a conversation.
type AskSettings struct {
	Agent      *models.Agent
	Owner      *models.User
	Operations Operations

	// Conversation is where the turn goes; the caller made or found it.
	Conversation *models.AgentConversation

	// Message is what the person said.
	Message string

	// Viewing is what they have open, when the drawer knows.
	Viewing *Viewing

	// Attachments are the files that came with the message, uploaded
	// already; References the threads the person pointed at.
	Attachments []*models.AgentAttachment
	References  []models.AgentReference

	// Surface is where the answer goes: drawer, phone, cli, api or mail.
	Surface string

	// ReadOnly leaves out every tool that changes anything, for a
	// read-only credential.
	ReadOnly bool

	// Allow, when set, is the only tools offered by name; Headless adds
	// the remote tools marked for runs with nobody present. MaxRounds
	// overrides the limit; UsageKind names the usage rows.
	Allow     map[string]bool
	Headless  bool
	MaxRounds int
	UsageKind string
}

// Viewing is what the person has open in the dashboard.
type Viewing struct {
	ItemID      string `json:"itemId,omitempty" graphapi:"nullable"`
	ThreadID    string `json:"threadId,omitempty" graphapi:"nullable"`
	Subject     string `json:"subject,omitempty" graphapi:"nullable"`
	MailboxID   string `json:"mailboxId,omitempty" graphapi:"nullable"`
	MailboxName string `json:"mailboxName,omitempty" graphapi:"nullable"`
	FolderID    string `json:"folderId,omitempty" graphapi:"nullable"`
	FolderName  string `json:"folderName,omitempty" graphapi:"nullable"`
	Page        string `json:"page,omitempty" graphapi:"nullable"`

	// ListKey and ListName are the mailing list open on the subscriptions
	// page, which the subscription tool acts on by key.
	ListKey  string `json:"listKey,omitempty" graphapi:"nullable"`
	ListName string `json:"listName,omitempty" graphapi:"nullable"`
}

// EventKind is what an event of a run carries.
type EventKind string

// The events a run emits, in the order a drawer shows them.
const (
	EventText         EventKind = "text"         // a piece of the answer
	EventMessage      EventKind = "message"      // the whole answer, once it is
	EventToolCall     EventKind = "tool_call"    // the model asked for a tool
	EventToolResult   EventKind = "tool_result"  // the tool answered
	EventConfirmation EventKind = "confirmation" // waiting for the person
	EventQuestion     EventKind = "question"     // the agent asked something
	EventNote         EventKind = "note"         // compaction and the like
	EventDone         EventKind = "done"
	EventError        EventKind = "error"
)

// Event is one thing that happened in a run.
type Event struct {
	Kind           EventKind `json:"kind"`
	RunID          string    `json:"runId"`
	ConversationID string    `json:"conversationId"`
	Sequence       int       `json:"sequence"`
	Text           string    `json:"text,omitempty"`
	Tool           string    `json:"tool,omitempty"`
	CallID         string    `json:"callId,omitempty"`
	Arguments      string    `json:"arguments,omitempty"`
	Risk           string    `json:"risk,omitempty"`
	Note           string    `json:"note,omitempty"`
	Error          string    `json:"error,omitempty"`
	At             time.Time `json:"at"`
}

// AskRun is one turn in flight.
type AskRun struct {
	ID       string
	settings *AskSettings
	agent    *Agent

	ctx    context.Context
	cancel context.CancelFunc

	mutex          sync.Mutex
	events         []Event
	subscribers    map[int]chan Event
	nextSubscriber int
	finished       bool

	// confirmations are the calls waiting for the person, by call id.
	confirmations map[string]chan bool

	// questions are the ask_user cards waiting for an answer, by call id.
	questions map[string]chan string

	// recalled is what memory searches found this turn, for the overlay.
	recalled []string

	// browser is the turn's headless browser, once it opened one.
	browser *browserRunner

	// loaded are the deferred tools loaded through tool_search this turn.
	loaded  map[string]bool
	offered []*Tool

	// previous is the turn of the same conversation this one waits for;
	// done closes when this one is over.
	previous *AskRun
	done     chan struct{}

	usage llm.Usage
}

// The bounds of a turn.
const (
	// askEventBacklog is how many events a late subscriber is replayed.
	askEventBacklog = 500

	// confirmationWait is how long a run waits for the person's word
	// before giving up the turn.
	confirmationWait = 10 * time.Minute

	// askHistoryTokens is the most history a round carries before the
	// older turns are compacted. A third of a small model's window, which
	// leaves room for the prompt, the tools and the answer.
	askHistoryTokens = 30000

	// askTailMessages is how many recent messages stay verbatim through a
	// compaction.
	askTailMessages = 12

	// askResultCharacters bounds a tool's answer in the history.
	askResultCharacters = 24000
)

// Ask starts a turn. It returns at once; the run streams what happens.
func (self *Agent) Ask(settings *AskSettings) (*AskRun, error) {
	if self == nil {
		return nil, ErrUnavailable
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "ask") || self.settings.Registry == nil {
		return nil, ErrUnavailable
	}
	if settings == nil || settings.Agent == nil || settings.Owner == nil || settings.Operations == nil || settings.Conversation == nil {
		return nil, fmt.Errorf("agent: a turn needs the agent, the person, the operations and the conversation")
	}
	if strings.TrimSpace(settings.Message) == "" {
		return nil, fmt.Errorf("agent: nothing was said")
	}
	run := &AskRun{
		ID:            security.NewULID(),
		settings:      settings,
		agent:         self,
		subscribers:   map[int]chan Event{},
		confirmations: map[string]chan bool{},
		loaded:        map[string]bool{},
		done:          make(chan struct{}),
	}
	run.ctx, run.cancel = context.WithCancel(self.ctx)
	self.runsMutex.Lock()
	if self.runs == nil {
		self.runs = map[string]*AskRun{}
		self.latest = map[string]*AskRun{}
	}
	self.runs[run.ID] = run
	// One turn at a time per conversation: a second one waits for the
	// one before it, so the history the model reads is never two turns
	// interleaved.
	if previous := self.latest[settings.Conversation.ID]; previous != nil && !previous.isFinished() {
		run.previous = previous
	}
	self.latest[settings.Conversation.ID] = run
	self.runsMutex.Unlock()
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		run.loop()
	}()
	return run, nil
}

// StopConversation ends every run of a conversation, for the conversation
// being deleted under them.
func (self *Agent) StopConversation(conversationId string) {
	self.runsMutex.Lock()
	var running []*AskRun
	for _, run := range self.runs {
		if run.settings.Conversation.ID == conversationId {
			running = append(running, run)
		}
	}
	self.runsMutex.Unlock()
	for _, run := range running {
		run.Stop()
	}
}

// FindRun is a run in flight or recently finished, or nil.
func (self *Agent) FindRun(runId string) *AskRun {
	self.runsMutex.Lock()
	defer self.runsMutex.Unlock()
	return self.runs[runId]
}

// forgetRun drops a finished run after a while, so a late drawer can still
// replay it but the map does not grow forever.
func (self *Agent) forgetRun(runId string) {
	timer := time.NewTimer(10 * time.Minute)
	select {
	case <-timer.C:
	case <-self.ctx.Done():
		timer.Stop()
	}
	self.runsMutex.Lock()
	if run := self.runs[runId]; run != nil {
		conversationId := run.settings.Conversation.ID
		if latest := self.latest[conversationId]; latest != nil && latest.ID == runId {
			delete(self.latest, conversationId)
		}
	}
	delete(self.runs, runId)
	self.runsMutex.Unlock()
}

// isFinished says whether the run is over.
func (self *AskRun) isFinished() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.finished
}

// Queued says whether the run is still waiting for the turn before it.
func (self *AskRun) Queued() bool {
	previous := self.previous
	return previous != nil && !previous.isFinished() && !self.isFinished()
}

// Subscribe replays what happened so far and then follows the run. The
// channel closes when the run is over.
func (self *AskRun) Subscribe() (<-chan Event, func()) {
	channel := make(chan Event, askEventBacklog+16)
	self.mutex.Lock()
	for _, event := range self.events {
		channel <- event
	}
	if self.finished {
		self.mutex.Unlock()
		close(channel)
		return channel, func() {}
	}
	id := self.nextSubscriber
	self.nextSubscriber++
	self.subscribers[id] = channel
	self.mutex.Unlock()
	return channel, func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		if _, ok := self.subscribers[id]; ok {
			delete(self.subscribers, id)
			close(channel)
		}
	}
}

// Conversation is the conversation the run belongs to.
func (self *AskRun) Conversation() *models.AgentConversation {
	return self.settings.Conversation
}

// Owner is the person.
func (self *AskRun) Owner() *models.User {
	return self.settings.Owner
}

// The run as the tool kit sees it (tools.Run).

func (self *AskRun) Agent() *models.Agent                 { return self.settings.Agent }
func (self *AskRun) Operations() tools.Operations         { return self.settings.Operations }
func (self *AskRun) Database() db.Database                { return self.agent.settings.Database }
func (self *AskRun) Configuration() *config.Configuration { return self.agent.settings.Configuration() }
func (self *AskRun) Surface() string                      { return self.settings.Surface }
func (self *AskRun) Headless() bool                       { return self.settings.Headless }
func (self *AskRun) ReadOnly() bool                       { return self.settings.ReadOnly }
func (self *AskRun) Offered() []*tools.Tool               { return self.offered }
func (self *AskRun) Loaded() map[string]bool              { return self.loaded }
func (self *AskRun) Load(name string)                     { self.loaded[name] = true }

// Resolve answers a confirmation card. It says whether there was one.
func (self *AskRun) Resolve(callId string, approve bool) bool {
	self.mutex.Lock()
	channel, ok := self.confirmations[callId]
	if ok {
		delete(self.confirmations, callId)
	}
	self.mutex.Unlock()
	if !ok {
		return false
	}
	channel <- approve
	return true
}

// Stop ends the run; the turn stays where it got to.
func (self *AskRun) Stop() {
	self.cancel()
}

func (self *AskRun) emit(event Event) {
	event.RunID = self.ID
	event.ConversationID = self.settings.Conversation.ID
	event.At = time.Now()
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.finished {
		return
	}
	event.Sequence = len(self.events)
	if len(self.events) < askEventBacklog {
		self.events = append(self.events, event)
	}
	for _, channel := range self.subscribers {
		select {
		case channel <- event:
		default:
			// A drawer that stopped reading is not a reason to stall the
			// run; it will miss this one.
		}
	}
}

func (self *AskRun) finish() {
	// The browser goes first, so that whoever is watching the events sees
	// the turn end with its context already discarded.
	self.mutex.Lock()
	runner := self.browser
	self.mutex.Unlock()
	if runner != nil {
		runner.close()
	}
	self.mutex.Lock()
	self.finished = true
	for id, channel := range self.subscribers {
		delete(self.subscribers, id)
		close(channel)
	}
	for id, channel := range self.confirmations {
		delete(self.confirmations, id)
		close(channel)
	}
	for id, channel := range self.questions {
		delete(self.questions, id)
		close(channel)
	}
	self.mutex.Unlock()
	close(self.done)
	self.cancel()
	go self.agent.forgetRun(self.ID)
}

// loop is the turn: history, then rounds until the model answers.
func (self *AskRun) loop() {
	defer self.finish()
	if previous := self.previous; previous != nil && !previous.isFinished() {
		self.emit(Event{Kind: EventNote, Note: "queued behind the turn before it"})
		select {
		case <-previous.done:
		case <-self.ctx.Done():
			self.emit(Event{Kind: EventNote, Note: "stopped"})
			self.emit(Event{Kind: EventDone})
			return
		}
	}
	if err := self.turn(); err != nil {
		if errors.Is(err, context.Canceled) {
			// Said in the transcript too, so that the words cut short
			// read as cut short after a reload.
			_ = self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
				_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: self.settings.Conversation.ID, Role: models.AgentMessageNote, Content: "stopped"})
				return err
			})
			self.emit(Event{Kind: EventNote, Note: "stopped"})
		} else {
			log.Warningf("the agent of %q failed a turn: %s", self.settings.Owner.Username, err)
			self.emit(Event{Kind: EventError, Error: err.Error()})
		}
	}
	self.emit(Event{Kind: EventDone})
}

func (self *AskRun) turn() error {
	ctx := self.ctx
	settings := self.settings
	configuration := self.agent.settings.Configuration()
	registry := self.agent.settings.Registry
	provider, model, err := self.chooseModel(configuration, registry)
	if err != nil {
		return err
	}
	modelName := registry.Configuration().Models.ForWork(config.AgentWorkAsk)
	if settings.Agent.AskModel != "" {
		modelName = settings.Agent.AskModel
	}

	// The person's turn, kept before anything is asked.
	var history []llm.ChatMessage
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, settings.Agent, settings.Owner, time.Now()); err != nil {
			return err
		}
		if history, err = self.loadHistory(tx); err != nil {
			return err
		}
		stored := &models.AgentMessage{ConversationID: settings.Conversation.ID, Role: string(llm.RoleUser), Content: settings.Message, References: settings.References}
		attachmentIds := make([]string, 0, len(settings.Attachments))
		for _, attachment := range settings.Attachments {
			stored.Attachments = append(stored.Attachments, *attachment)
			attachmentIds = append(attachmentIds, attachment.ID)
		}
		saved, err := tx.AppendAgentMessage(stored)
		if err != nil {
			return err
		}
		if err := tx.ClaimAgentAttachments(attachmentIds, settings.Conversation.ID, saved.ID); err != nil {
			return err
		}
		_, err = tx.UpdateAgentConversation(settings.Conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.LastAt = time.Now()
			if conversation.Surface == "" {
				conversation.Surface = settings.Surface
			}
			return nil
		})
		return err
	}); err != nil {
		return err
	}
	history = append(history, userTurn(ctx, self.agent.settings.Storage, settings.Message, settings.Attachments, settings.References))

	// The catalog as this person sees it, and what the connected servers
	// offer them.
	self.offered = self.agent.catalog.Offered(settings.Operations.Permissions(), &configuration.Agent.Tools)
	for _, tool := range self.agent.remoteTools(ctx, settings.Agent.ID) {
		if !listed(configuration.Agent.Tools.Disabled, tool) {
			self.offered = append(self.offered, tool)
		}
	}
	if !configuration.Agent.Browser.Enabled || !FeatureAllowed(configuration, "browser") {
		withoutBrowser := self.offered[:0:0]
		for _, tool := range self.offered {
			if tool.Family != FamilyBrowser {
				withoutBrowser = append(withoutBrowser, tool)
			}
		}
		self.offered = withoutBrowser
	}
	if settings.ReadOnly || settings.Allow != nil {
		kept := self.offered[:0:0]
		for _, tool := range self.offered {
			if settings.ReadOnly && tool.Risk != RiskRead && tool.RiskOf == nil {
				// A tool whose calls differ in risk stays: each call is
				// judged when it is made.
				continue
			}
			if settings.Allow != nil && !settings.Allow[tool.Name] && (!settings.Headless || !tool.Headless) {
				continue
			}
			kept = append(kept, tool)
		}
		self.offered = kept
	}
	usageKind := settings.UsageKind
	if usageKind == "" {
		usageKind = "ask"
	}

	maximumRounds := configuration.Agent.Limits.MaxRoundsPerAsk
	if maximumRounds <= 0 {
		maximumRounds = 40
	}
	if settings.MaxRounds > 0 {
		maximumRounds = settings.MaxRounds
	}
	for round := 0; round < maximumRounds; round++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if round > 0 {
			// A turn of many rounds is measured as it goes, not only when
			// it starts: the budget is a cap, not a suggestion.
			var deferral *Deferral
			if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
				err := RequireBudget(tx, configuration, settings.Agent, settings.Owner, time.Now())
				if errors.As(err, &deferral) {
					return nil
				}
				return err
			}); err != nil {
				return err
			}
			if deferral != nil {
				self.emit(Event{Kind: EventNote, Note: "stopped: " + deferral.Reason})
				return nil
			}
		}
		if llm.EstimateTokens(renderHistory(history)) > askHistoryTokens {
			compacted, err := self.compact(ctx, provider, model, modelName, history)
			if err != nil {
				log.Warningf("cannot compact the conversation %q: %s", settings.Conversation.ID, err)
			} else {
				history = compacted
			}
		}
		compact := llm.EstimateTokens(renderHistory(history)) > askHistoryTokens/2
		sent, deferred := Split(self.offered, self.loaded, compact)
		system, err := self.systemPrompt(ctx, configuration, sent, deferred, compact)
		if err != nil {
			return err
		}
		messages := make([]llm.ChatMessage, 0, len(history)+2)
		messages = append(messages, llm.ChatMessage{Role: llm.RoleSystem, Content: system, CacheBreakpoint: true})
		messages = append(messages, history...)
		if overlays := self.overlays(ctx, configuration); overlays != "" {
			messages = append(messages, llm.ChatMessage{Role: llm.RoleSystem, Content: overlays})
		}
		definitions := make([]llm.ToolDefinition, 0, len(sent))
		for _, tool := range sent {
			definitions = append(definitions, tool.Definition())
		}

		response, err := self.chat(ctx, provider, &llm.ChatRequest{Model: model, Messages: messages, Tools: definitions, MaxTokens: 4000})
		if response != nil {
			self.usage = self.usage.Add(response.Usage)
			RecordUsage(self.agent.settings.Database, settings.Agent.ID, "", modelName, usageKind, response.Usage)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) && response != nil && strings.TrimSpace(response.Message.Content) != "" {
				// Stopped mid-sentence: the words that had come stay in
				// the conversation, so what was seen is what is kept.
				_ = self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
					_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: settings.Conversation.ID, Role: string(llm.RoleAssistant), Content: response.Message.Content})
					return err
				})
			}
			return fmt.Errorf("asking the model: %w", err)
		}
		answer := response.Message
		answer.Role = llm.RoleAssistant
		history = append(history, answer)
		if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.AppendAgentMessage(&models.AgentMessage{
				ConversationID: settings.Conversation.ID,
				Role:           string(llm.RoleAssistant),
				Content:        answer.Content,
				ToolCalls:      toolCallsOf(answer.ToolCalls),
				Usage: &models.AgentUsageNote{Model: modelName, Kind: usageKind, PromptTokens: response.Usage.PromptTokens, CompletionTokens: response.Usage.CompletionTokens,
					CacheReadTokens: response.Usage.CacheReadTokens, CacheWriteTokens: response.Usage.CacheWriteTokens},
			})
			return err
		}); err != nil {
			return err
		}
		if strings.TrimSpace(answer.Content) != "" {
			self.emit(Event{Kind: EventMessage, Text: answer.Content})
		}
		if len(answer.ToolCalls) == 0 {
			// A new named conversation is titled now, so the picker has a
			// name for it; everything after that waits for it to go quiet.
			if settings.Conversation.Kind == models.AgentConversationNamed && strings.TrimSpace(settings.Conversation.Title) == "" {
				self.agent.waitGroup.Add(1)
				go func() {
					defer self.agent.waitGroup.Done()
					titled, err := self.agent.describeConversation(self.agent.ctx, settings.Conversation)
					if err != nil {
						log.Warningf("cannot title conversation %q: %s", settings.Conversation.ID, err)
					}
					if titled != "" {
						self.emit(Event{Kind: EventNote, Note: "titled: " + titled})
					}
				}()
			}
			return nil
		}
		for _, toolCall := range answer.ToolCalls {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result := self.runTool(ctx, configuration, sent, deferred, toolCall)
			history = append(history, llm.ChatMessage{Role: llm.RoleTool, ToolCallID: toolCall.ID, Name: toolCall.Name, Content: result})
			if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
				_, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: settings.Conversation.ID, Role: string(llm.RoleTool), ToolCallID: toolCall.ID, Name: toolCall.Name, Content: result})
				return err
			}); err != nil {
				return err
			}
		}
	}
	self.emit(Event{Kind: EventNote, Note: "stopped after the most rounds a turn may take"})
	return nil
}

// chooseModel is the provider and model for this person's Ask: their own
// choice when the operator offers choices, else the one for ask work.
func (self *AskRun) chooseModel(configuration *config.Configuration, registry *llm.Registry) (llm.Provider, string, error) {
	if chosen := strings.TrimSpace(self.settings.Agent.AskModel); chosen != "" {
		for _, choice := range configuration.Agent.Models.Choices {
			if choice == chosen {
				return registry.ForModel(chosen)
			}
		}
	}
	return registry.ForWork(config.AgentWorkAsk)
}

// chat streams when the provider can, so the drawer sees the words as they
// come, and falls back to one call when it cannot.
func (self *AskRun) chat(ctx context.Context, provider llm.Provider, request *llm.ChatRequest) (*llm.ChatResponse, error) {
	timeout := self.agent.settings.Configuration().Agent.Limits.RequestTimeout.Duration()
	callContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	events, err := provider.ChatStream(callContext, request)
	if err != nil {
		return provider.Chat(callContext, request)
	}
	var response *llm.ChatResponse
	var text strings.Builder
	var toolCalls []llm.ToolCall
	for event := range events {
		switch event.Kind {
		case llm.StreamText:
			text.WriteString(event.Text)
			self.emit(Event{Kind: EventText, Text: event.Text})
		case llm.StreamToolCall:
			if event.ToolCall != nil {
				toolCalls = append(toolCalls, *event.ToolCall)
			}
		case llm.StreamDone:
			response = event.Response
		case llm.StreamError:
			// What had streamed by then, for the caller that wants to
			// keep it.
			partial := &llm.ChatResponse{Message: llm.ChatMessage{Role: llm.RoleAssistant, Content: text.String(), ToolCalls: toolCalls}}
			return partial, event.Err
		}
	}
	if response == nil {
		return nil, fmt.Errorf("the stream ended without a response")
	}
	if response.Message.Content == "" {
		response.Message.Content = text.String()
	}
	if len(response.Message.ToolCalls) == 0 {
		response.Message.ToolCalls = toolCalls
	}
	return response, nil
}

// runTool runs one call: an unknown tool answers so, a tool that needs the
// person's word waits for it, and what a tool answers is bounded and, when
// it came from outside, marked as data.
func (self *AskRun) runTool(ctx context.Context, configuration *config.Configuration, sent, deferred []*Tool, toolCall llm.ToolCall) string {
	self.emit(Event{Kind: EventToolCall, Tool: toolCall.Name, CallID: toolCall.ID, Arguments: toolCall.Arguments})
	var tool *Tool
	for _, candidate := range sent {
		if candidate.Name == toolCall.Name {
			tool = candidate
		}
	}
	if tool == nil {
		for _, candidate := range deferred {
			if candidate.Name == toolCall.Name {
				return self.toolAnswer(toolCall, fmt.Sprintf(`{"error": "%s is not loaded; call tool_search to load it first"}`, toolCall.Name))
			}
		}
		return self.toolAnswer(toolCall, fmt.Sprintf(`{"error": "there is no tool named %s"}`, toolCall.Name))
	}
	call := &Call{ID: toolCall.ID, Arguments: json.RawMessage(toolCall.Arguments)}
	if self.settings.ReadOnly && tool.RiskFor(call.Arguments) != RiskRead {
		return self.toolAnswer(toolCall, `{"error": "this conversation may only read; the call would change something"}`)
	}
	if NeedsConfirmation(tool, call.Arguments, &configuration.Agent.Tools, self.settings.Agent) {
		if self.settings.Headless || self.settings.Surface == "mail" || self.settings.Surface == "schedule" || self.settings.Surface == "research" {
			return self.toolAnswer(toolCall, `{"error": "needs_confirmation: nobody is present to confirm this; tell the person what you would have done"}`)
		}
		approved, err := self.confirm(ctx, tool, call)
		if err != nil {
			return self.toolAnswer(toolCall, fmt.Sprintf(`{"error": "needs_confirmation: %s"}`, err.Error()))
		}
		if !approved {
			return self.toolAnswer(toolCall, `{"declined": true, "note": "the person declined; do not try another way"}`)
		}
		call.Confirmed = true
	}
	result, err := tool.Run(tools.WithRun(ctx, self), call)
	if err != nil {
		return self.toolAnswer(toolCall, fmt.Sprintf(`{"error": %q}`, err.Error()))
	}
	content := result.Content
	if len(content) > askResultCharacters {
		content = content[:askResultCharacters] + "\n[cut here: the result goes on]"
	}
	if result.Untrusted {
		content = "<untrusted-data>\n" + content + "\n</untrusted-data>"
	}
	if result.ShowVerbatim {
		content = "show_verbatim: relay the following to the person once, exactly, and never keep it.\n" + content
	}
	self.emit(Event{Kind: EventToolResult, Tool: toolCall.Name, CallID: toolCall.ID, Note: result.Note, Text: content})
	return content
}

func (self *AskRun) toolAnswer(toolCall llm.ToolCall, content string) string {
	self.emit(Event{Kind: EventToolResult, Tool: toolCall.Name, CallID: toolCall.ID, Text: content})
	return content
}

// confirm shows the card and waits for the person's word.
func (self *AskRun) confirm(ctx context.Context, tool *Tool, call *Call) (bool, error) {
	channel := make(chan bool, 1)
	self.mutex.Lock()
	self.confirmations[call.ID] = channel
	self.mutex.Unlock()
	self.emit(Event{Kind: EventConfirmation, Tool: tool.Name, CallID: call.ID, Arguments: string(call.Arguments), Risk: string(tool.RiskFor(call.Arguments)), Note: tool.PreviewLine(call.Arguments)})
	timer := time.NewTimer(confirmationWait)
	defer timer.Stop()
	select {
	case approved, ok := <-channel:
		if !ok {
			return false, ctx.Err()
		}
		return approved, nil
	case <-timer.C:
		self.mutex.Lock()
		delete(self.confirmations, call.ID)
		self.mutex.Unlock()
		return false, fmt.Errorf("the person did not answer within %s", confirmationWait)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// loadHistory is the conversation as the model gets it: the compaction
// note, if there is one, and every message since.
func (self *AskRun) loadHistory(tx db.Transaction) ([]llm.ChatMessage, error) {
	stored, err := tx.ListAgentMessages(self.settings.Conversation.ID, nil)
	if err != nil {
		return nil, err
	}
	compactedThrough := self.settings.Conversation.CompactedThrough
	var history []llm.ChatMessage
	skipping := compactedThrough != ""
	for _, message := range stored {
		if skipping {
			if message.ID == compactedThrough {
				skipping = false
			}
			if message.Role != roleCompaction {
				continue
			}
		}
		switch message.Role {
		case string(llm.RoleUser):
			history = append(history, llm.ChatMessage{Role: llm.RoleUser, Content: historyTurn(message)})
		case string(llm.RoleAssistant):
			history = append(history, llm.ChatMessage{Role: llm.RoleAssistant, Content: message.Content, ToolCalls: toolCallsFrom(message.ToolCalls)})
		case string(llm.RoleTool):
			history = append(history, llm.ChatMessage{Role: llm.RoleTool, ToolCallID: message.ToolCallID, Name: message.Name, Content: message.Content})
		case roleCompaction:
			if message.ID == compactedThrough || compactedThrough == "" {
				history = append(history, llm.ChatMessage{Role: llm.RoleUser, Content: "Note on the earlier conversation, which was compacted:\n\n" + message.Content})
			}
		}
	}
	return repairHistory(history), nil
}

// roleCompaction is the role of the note that stands in for the compacted
// turns.
const roleCompaction = "compaction"

// repairHistory drops a tool message whose call is not in the history and
// an assistant call without its answer, which a provider refuses.
func repairHistory(history []llm.ChatMessage) []llm.ChatMessage {
	answered := map[string]bool{}
	for _, message := range history {
		if message.Role == llm.RoleTool {
			answered[message.ToolCallID] = true
		}
	}
	repaired := make([]llm.ChatMessage, 0, len(history))
	asked := map[string]bool{}
	for _, message := range history {
		switch message.Role {
		case llm.RoleAssistant:
			kept := message.ToolCalls[:0:0]
			for _, toolCall := range message.ToolCalls {
				if answered[toolCall.ID] {
					kept = append(kept, toolCall)
					asked[toolCall.ID] = true
				}
			}
			message.ToolCalls = kept
			if message.Content == "" && len(kept) == 0 {
				continue
			}
		case llm.RoleTool:
			if !asked[message.ToolCallID] {
				continue
			}
		}
		repaired = append(repaired, message)
	}
	return repaired
}

func toolCallsOf(calls []llm.ToolCall) []models.AgentToolCall {
	if len(calls) == 0 {
		return nil
	}
	stored := make([]models.AgentToolCall, 0, len(calls))
	for _, call := range calls {
		stored = append(stored, models.AgentToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return stored
}

func toolCallsFrom(stored []models.AgentToolCall) []llm.ToolCall {
	if len(stored) == 0 {
		return nil
	}
	calls := make([]llm.ToolCall, 0, len(stored))
	for _, call := range stored {
		calls = append(calls, llm.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return calls
}

// renderHistory is the history as text, for measuring and for compaction.
func renderHistory(history []llm.ChatMessage) string {
	var builder strings.Builder
	for _, message := range history {
		builder.WriteString(string(message.Role))
		if message.Name != "" {
			builder.WriteString(" (" + message.Name + ")")
		}
		builder.WriteString(": ")
		builder.WriteString(message.Content)
		for _, call := range message.ToolCalls {
			builder.WriteString("\n  → " + call.Name + " " + call.Arguments)
		}
		builder.WriteString("\n\n")
	}
	return builder.String()
}

// compact asks the model for a note that stands in for the older turns,
// keeps the recent tail, and records where the verbatim history resumes.
func (self *AskRun) compact(ctx context.Context, provider llm.Provider, model, modelName string, history []llm.ChatMessage) ([]llm.ChatMessage, error) {
	if len(history) <= askTailMessages+1 {
		return history, nil
	}
	cut := len(history) - askTailMessages
	// Never cut between a call and its answer.
	for cut > 0 && history[cut].Role == llm.RoleTool {
		cut--
	}
	if cut <= 0 {
		return history, nil
	}
	older, tail := history[:cut], history[cut:]
	prompt, err := render("compact.txt", map[string]any{"Conversation": renderHistory(older)})
	if err != nil {
		return nil, err
	}
	registry := self.agent.settings.Registry
	compactProvider, compactModel, err := registry.ForWork(config.AgentWorkCompact)
	if err != nil {
		compactProvider, compactModel = provider, model
	}
	callContext, cancel := context.WithTimeout(ctx, self.agent.settings.Configuration().Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := compactProvider.Chat(callContext, &llm.ChatRequest{Model: compactModel, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 800})
	if response != nil {
		RecordUsage(self.agent.settings.Database, self.settings.Agent.ID, "", modelName, "compact", response.Usage)
	}
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(response.Message.Content)
	if note == "" {
		return history, nil
	}
	var stored *models.AgentMessage
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		stored, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: self.settings.Conversation.ID, Role: roleCompaction, Content: note})
		if err != nil {
			return err
		}
		_, err = tx.UpdateAgentConversation(self.settings.Conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.CompactedThrough = stored.ID
			return nil
		})
		return err
	}); err != nil {
		return nil, err
	}
	self.settings.Conversation.CompactedThrough = stored.ID
	self.emit(Event{Kind: EventNote, Note: "the earlier conversation was compacted into a note"})
	compacted := []llm.ChatMessage{{Role: llm.RoleUser, Content: "Note on the earlier conversation, which was compacted:\n\n" + note}}
	return append(compacted, tail...), nil
}

// systemPrompt is layers 0 to 4: identity and conduct, the operator's
// instructions, the situation, the person's own words and memories, the
// guidance for this round's tools and the deferred catalog.
func (self *AskRun) systemPrompt(ctx context.Context, configuration *config.Configuration, sent, deferred []*Tool, short bool) (string, error) {
	settings := self.settings
	guidance := make([]string, 0, len(sent))
	seen := map[string]bool{}
	for _, tool := range sent {
		if tool.Guidance != "" && !seen[tool.Guidance] {
			seen[tool.Guidance] = true
			guidance = append(guidance, strings.TrimSpace(tool.Guidance))
		}
	}
	deferredLines := make([]string, 0, len(deferred))
	for _, tool := range deferred {
		deferredLines = append(deferredLines, tool.Name+" — "+firstSentence(tool.Description))
	}
	return render("ask.txt", map[string]any{
		"AgentName":         settings.Agent.DisplayName(),
		"PersonName":        personName(settings.Owner),
		"ServerName":        configuration.Server.Name,
		"Language":          languageName(Language(settings.Agent, settings.Owner)),
		"Short":             short,
		"HouseInstructions": strings.TrimSpace(configuration.Agent.Instructions),
		"Situation":         self.situation(ctx, configuration),
		"Instructions":      strings.TrimSpace(settings.Agent.Instructions),
		"Memories":          self.memories(ctx),
		"Guidance":          guidance,
		"Deferred":          deferredLines,
	})
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.Index(text, ". "); index > 0 {
		return text[:index+1]
	}
	return text
}

// situation is layer 2: computed each round, never stored.
func (self *AskRun) situation(ctx context.Context, configuration *config.Configuration) string {
	settings := self.settings
	owner := settings.Owner
	var lines []string
	name := personName(owner)
	lines = append(lines, fmt.Sprintf("The person is %s (username %s).", name, owner.Username))
	lines = append(lines, "What they may do: "+permissionWords(settings.Operations.Permissions())+".")
	granted, others := self.sources(ctx)
	if len(granted) > 0 {
		lines = append(lines, "Mailboxes you may reach:")
		lines = append(lines, granted...)
	} else {
		lines = append(lines, "They have not given you any mailbox yet.")
	}
	if len(others) > 0 {
		lines = append(lines, "Mailboxes they have not given you: "+strings.Join(others, ", ")+".")
	}
	// The zone, not the time: the time changes every minute, and a system
	// prompt that changes every minute is one no provider can cache. The
	// <now> overlay carries the time, after the history.
	lines = append(lines, fmt.Sprintf("Their time zone is %s. Their language is %s.", Location(owner).String(), languageName(Language(settings.Agent, owner))))
	lines = append(lines, fmt.Sprintf("This server is %s, running TeaNode %s.", configuration.Server.Name, version.Version()))
	if configuration.Agent.Models.Embedding != "" && FeatureAllowed(configuration, "search") {
		lines = append(lines, "Search by meaning exists: mail_search with a query finds messages that say the same thing in other words.")
	} else {
		lines = append(lines, "Search is by keyword only.")
	}
	if settings.Surface != "" {
		lines = append(lines, "You are talking through the "+settings.Surface+".")
	}
	return strings.Join(lines, "\n")
}

// permissionWords says what the person may do, in words.
func permissionWords(permissions *models.EffectivePermissions) string {
	if permissions == nil {
		return "nothing beyond their own mail"
	}
	var words []string
	for _, permission := range models.Permissions() {
		if !permissions.HasAnywhere(permission) {
			continue
		}
		domains, all := permissions.DomainsWith(permission)
		switch {
		case all || len(domains) == 0:
			words = append(words, string(permission))
		default:
			words = append(words, fmt.Sprintf("%s (over %d domain(s))", permission, len(domains)))
		}
	}
	if len(words) == 0 {
		return "nothing beyond their own mail"
	}
	sort.Strings(words)
	return strings.Join(words, ", ")
}

// sources lists the mailboxes, granted and not, as the situation says them.
func (self *AskRun) sources(ctx context.Context) (granted []string, others []string) {
	views, err := listMailboxes(ctx, self.settings.Operations)
	if err != nil {
		log.Warningf("cannot list the mailboxes of %q for the prompt: %s", self.settings.Owner.Username, err)
		return nil, nil
	}
	for _, view := range views {
		mailbox := view.Mailbox
		if mailbox.Agent == nil || !mailbox.Agent.Granted {
			others = append(others, fmt.Sprintf("%q", mailbox.Name))
			continue
		}
		addresses := make([]string, 0, len(mailbox.Addresses))
		for _, address := range mailbox.Addresses {
			addresses = append(addresses, address.Address)
		}
		folders := make([]string, 0, len(view.Folders))
		for _, folder := range view.Folders {
			folders = append(folders, folder.Name)
		}
		features := []string{}
		source := mailbox.Agent
		if source.Triage != nil && source.Triage.Enabled {
			features = append(features, "sorting")
		}
		if source.Summaries != nil && source.Summaries.Enabled {
			features = append(features, "summaries")
		}
		if source.DraftReplies {
			features = append(features, "drafts")
		}
		if source.AutoReply != nil && source.AutoReply.Enabled {
			features = append(features, "answering for them")
		}
		if source.Search {
			features = append(features, "search by meaning")
		}
		line := fmt.Sprintf("- %q (id %s): addresses %s; folders %s", mailbox.Name, mailbox.ID, strings.Join(addresses, ", "), strings.Join(folders, ", "))
		if len(features) > 0 {
			line += "; on: " + strings.Join(features, ", ")
		}
		granted = append(granted, line)
	}
	return granted, others
}

// overlays is what is true now, appended after the history each round.
func (self *AskRun) overlays(ctx context.Context, configuration *config.Configuration) string {
	settings := self.settings
	var blocks []string
	if viewing := settings.Viewing; viewing != nil {
		var parts []string
		if viewing.ItemID != "" {
			parts = append(parts, "item_id "+viewing.ItemID)
		}
		if viewing.ThreadID != "" {
			parts = append(parts, "thread_id "+viewing.ThreadID)
		}
		if viewing.Subject != "" {
			parts = append(parts, fmt.Sprintf("subject %q", viewing.Subject))
		}
		if viewing.MailboxName != "" {
			parts = append(parts, fmt.Sprintf("mailbox %q", viewing.MailboxName))
		} else if viewing.MailboxID != "" {
			parts = append(parts, "mailbox_id "+viewing.MailboxID)
		}
		if viewing.FolderName != "" {
			parts = append(parts, fmt.Sprintf("folder %q", viewing.FolderName))
		}
		if viewing.ListName != "" || viewing.ListKey != "" {
			parts = append(parts, fmt.Sprintf("the mailing list %q (key %s; the subscription tool acts on it by key)", viewing.ListName, viewing.ListKey))
		}
		if viewing.Page != "" {
			parts = append(parts, "page "+viewing.Page)
		}
		if len(parts) > 0 {
			blocks = append(blocks, "<viewing>\nThe person has open: "+strings.Join(parts, ", ")+". \"This\" means it.\n</viewing>")
		}
	}
	switch settings.Surface {
	case "phone":
		blocks = append(blocks, "<surface>\nA phone: keep it short, no tables.\n</surface>")
	case "cli", "api":
		blocks = append(blocks, "<surface>\nA terminal: plain text, no markdown tables wider than eighty columns, no suggestions of what to click.\n</surface>")
	case "mail":
		blocks = append(blocks, "<surface>\nThe answer goes out as a mail message: plain paragraphs, and the first line is its subject.\n</surface>")
	}
	for _, tool := range self.offered {
		if tool.Overlay != nil {
			if block := strings.TrimSpace(tool.Overlay(tools.WithRun(ctx, self))); block != "" {
				blocks = append(blocks, block)
			}
		}
	}
	_ = self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
		budget, err := CheckBudget(tx, configuration, settings.Agent, settings.Owner, time.Now())
		if err == nil && budget != nil && budget.Limit > 0 && budget.Limit-budget.Used < budget.Limit/5 {
			blocks = append(blocks, fmt.Sprintf("<budget>\n%d tokens remain of today's %d. Avoid long reads.\n</budget>", max(budget.Limit-budget.Used, 0), budget.Limit))
		}
		return nil
	})
	blocks = append(blocks, "<now>\n"+time.Now().In(Location(settings.Owner)).Format("Monday, 2 January 2006 15:04 MST")+"\n</now>")
	return strings.Join(blocks, "\n")
}
