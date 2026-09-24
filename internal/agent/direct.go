package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A tool run with no conversation and no model in it.
//
// The loop in ask.go exists because a model has to be told what it may do,
// asked, and then obeyed. A caller that already knows which tool it wants
// needs none of that: a harness speaking the Model Context Protocol asks
// for mail_search by name, and putting a model in the middle to arrive at
// the same call would cost money, take seconds and answer differently each
// time.
//
// What a tool actually needs is the run it is called in -- who it is for,
// what it may reach, and where its work is filed -- and that is an
// interface, not the loop. This is an implementation of it for a caller
// who is not a conversation.

// Held to the interface here rather than at the first use, so that a
// method added to tools.Run breaks this file and not a call site.
var _ tools.Run = (*directRun)(nil)

// directRun is one tool call outside a conversation.
type directRun struct {
	agent      *Agent
	owner      *models.User
	agentModel *models.Agent
	operations tools.Operations
	surface    string
	offered    []*tools.Tool

	// origin is the address the caller reached this server by, for a link
	// that has to work from wherever the caller is; empty when there is none.
	origin string

	mutex    sync.Mutex
	recalled []string
}

// The run as the tool kit sees it (tools.Run).

func (self *directRun) Owner() *models.User          { return self.owner }
func (self *directRun) Agent() *models.Agent         { return self.agentModel }
func (self *directRun) Operations() tools.Operations { return self.operations }
func (self *directRun) Database() db.Database        { return self.agent.settings.Database }
func (self *directRun) Storage() storage.Storage     { return self.agent.settings.Storage }
func (self *directRun) Surface() string              { return self.surface }
func (self *directRun) Offered() []*tools.Tool       { return self.offered }

func (self *directRun) Configuration() *config.Configuration {
	return self.agent.settings.Configuration()
}

// Conversation is nothing: this call is not part of one. A tool that files
// something against a conversation has to cope with that, and they all do,
// because the night is already a run without one.
func (self *directRun) Conversation() *models.AgentConversation { return nil }

// Headless is false because somebody is there -- a person at a harness,
// who asked for this. ReadOnly is false because the credential decides
// what may be reached, and it has already decided by the time a call gets
// here.
func (self *directRun) Headless() bool { return false }
func (self *directRun) CanAsk() bool   { return true }
func (self *directRun) ReadOnly() bool { return false }

// Loaded says every tool is loaded, because there is no tool_search here
// to load them: a harness lists the catalog once and keeps it.
func (self *directRun) Loaded() map[string]bool {
	loaded := make(map[string]bool, len(self.offered))
	for _, tool := range self.offered {
		loaded[tool.Name] = true
	}
	return loaded
}

func (self *directRun) Load(name string) {}

func (self *directRun) Recall(line string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, existing := range self.recalled {
		if existing == line {
			return
		}
	}
	self.recalled = append(self.recalled, line)
}

func (self *directRun) Recalled() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.recalled...)
}

// Ask has nobody to ask.
//
// In a conversation this puts a card in front of the person and waits. A
// call arriving over the protocol has a person at the other end, but they
// are looking at their harness and this server cannot reach them there.
// Saying so is better than guessing an answer on their behalf, and better
// than hanging until the request times out.
func (self *directRun) Ask(ctx context.Context, callId, question string, choices []string) (string, error) {
	return "", fmt.Errorf("this call cannot put a question to anybody: ask the agent instead, which can")
}

func (self *directRun) Enqueue(tx db.Transaction, kind models.AgentJobKind, mailboxId, subjectId string) error {
	_, err := self.agent.Enqueue(tx, kind, self.agentModel.ID, mailboxId, subjectId)
	return err
}

func (self *directRun) DraftReply(ctx context.Context, request *models.AgentDraftRequest) (*models.AgentDraft, error) {
	return self.agent.DraftReply(ctx, request)
}

func (self *directRun) DiscardDraft(ctx context.Context, tx db.Transaction, itemId string) error {
	return self.agent.discardDraft(ctx, tx, itemId)
}

func (self *directRun) MeaningSearch(ctx context.Context, mailboxId, query string, limit int) ([]string, error) {
	return self.agent.meaningSearch(ctx, self.agentModel, mailboxId, query, limit)
}

// DirectTools is the catalog as this person sees it, for a caller that
// will choose a tool itself rather than have a model choose one.
//
// It is built the way a turn's catalog is built -- what their permissions
// allow, less what the operator switched off, plus the tools of the
// servers they have connected and the skills the operator installed --
// with two left out.
//
// ask_user goes because there is nobody at this end to answer it; see Ask
// above. tool_search goes because it exists to keep a long catalog out of
// a model's request until it is wanted, and a caller that lists tools once
// and keeps the list is better served by the list.
func (self *Agent) DirectTools(ctx context.Context, person *models.Agent, operations tools.Operations) []*tools.Tool {
	configuration := self.settings.Configuration()
	offered := self.catalog.Offered(operations.Permissions(), &configuration.Agent.Tools)
	for _, tool := range self.remoteTools(ctx, person.ID, false) {
		if !listed(configuration.Agent.Tools.Disabled, tool) {
			offered = append(offered, tool)
		}
	}
	for _, tool := range self.SkillTools(ctx) {
		if !listed(configuration.Agent.Tools.Disabled, tool) {
			offered = append(offered, tool)
		}
	}
	kept := make([]*tools.Tool, 0, len(offered))
	for _, tool := range offered {
		switch tool.Name {
		case "ask_user", "tool_search":
			continue
		}
		kept = append(kept, tool)
	}
	return kept
}

// CallDirect runs one tool by name, as this person, outside a
// conversation. The tool decides what it may do; the person's permissions
// decided whether it was offered at all.
func (self *Agent) CallDirect(
	ctx context.Context,
	owner *models.User,
	person *models.Agent,
	operations tools.Operations,
	surface string,
	origin string,
	name string,
	arguments json.RawMessage,
) (*tools.Result, error) {
	offered := self.DirectTools(ctx, person, operations)
	var found *tools.Tool
	for _, tool := range offered {
		if tool.Name == name {
			found = tool
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("there is no tool called %q here", name)
	}
	if found.Run == nil {
		return nil, fmt.Errorf("the tool %q cannot be called directly", name)
	}
	run := &directRun{
		agent: self, owner: owner, agentModel: person,
		operations: operations, surface: surface, offered: offered, origin: origin,
	}
	// Confirmed, because the person confirmed by making the call: a
	// harness asks its own person before it runs a tool, and there is no
	// card this server could show that would reach them. The credential
	// they handed the harness is what says they meant it.
	return found.Run(tools.WithRun(ctx, run), &tools.Call{
		ID: "direct", Arguments: arguments, Confirmed: true,
	})
}

// LatestIn is the last turn of a conversation, running or just finished,
// for a caller that asked, could not wait to the end, and has come back
// for the rest.
//
// A finished run is handed back rather than withheld, because the caller
// coming back a second late would otherwise be told there is nothing to
// wait for and never hear the answer. Subscribing to a finished run
// replays what it said and closes, which is exactly what they want. A run
// is forgotten ten minutes after it ends, and after that there is nothing
// to hand back and the conversation itself is where the answer is.
func (self *Agent) LatestIn(conversationId string) *AskRun {
	if self == nil || conversationId == "" {
		return nil
	}
	self.runsMutex.Lock()
	defer self.runsMutex.Unlock()
	return self.latest[conversationId]
}

// The optional sides of a run, which a tool looks for with a type assertion
// and quietly does without when they are missing. Held to them here so that
// leaving one out breaks the build: every one of these was missing once, and
// through the protocol the computer, the attached tab and the coding tools
// failed outright while the searches quietly lost their sense of meaning.
var (
	_ tools.Computing          = (*directRun)(nil)
	_ tools.Browsing           = (*directRun)(nil)
	_ tools.GraphSearching     = (*directRun)(nil)
	_ tools.KnowledgeSearching = (*directRun)(nil)
	_ tools.Remembering        = (*directRun)(nil)
	_ tools.Linking            = (*directRun)(nil)
)

// SharedLink is a full address that opens one file with no sign-in
// (tools.Linking). A caller over MCP has no session here, and its token, when
// a program was given it by approval, works only at the tools endpoint, so a
// plain path to the file was one it could not fetch.
func (self *directRun) SharedLink(attachmentId string) string {
	if self.origin == "" {
		return ""
	}
	share := self.agent.ShareAttachment(attachmentId, time.Now().Add(ShareToOpenFor))
	if share == "" {
		return ""
	}
	return self.origin + "/api/v1/agent/attachments/" + url.PathEscape(attachmentId) + "?share=" + url.QueryEscape(share)
}

// The person's computers (tools.Computing), the same ones a conversation
// reaches: whoever is at the harness is the person, and asked for this.
func (self *directRun) AttachedComputers() []tools.Computer {
	var computers []tools.Computer
	for _, computer := range self.agent.computersFor(self.agentModel.ID) {
		computers = append(computers, computer)
	}
	return computers
}

func (self *directRun) ComputersAllowed() bool {
	return FeatureAllowed(self.agent.settings.Configuration(), "computer")
}

// ComputersUnattended is false, and never matters here: this run is not
// headless, so the unattended rule is not reached.
func (self *directRun) ComputersUnattended() bool { return false }

// The person's attached tab (tools.Browsing), which lives in their own
// browser and outlasts any one call.
func (self *directRun) AttachedTab() tools.Tab {
	if tab := self.agent.tabFor(self.agentModel.ID); tab != nil {
		return tab
	}
	return nil
}

func (self *directRun) TabsAllowed() bool {
	attach := self.agent.settings.Configuration().Agent.Browser.AttachTabs
	return attach == nil || *attach
}

// BrowserPage is refused: the server's own browser keeps a page for the
// length of a conversation and closes it when the turn ends. A call from
// outside one has no turn to end, so a page opened here would be gone by
// the next call, or left open with nobody to close it. The attached tab
// above is what a harness can drive, and the agent itself, asked through
// teanode_ask, can use its own browser inside a turn.
func (self *directRun) BrowserPage(ctx context.Context) (*browser.Context, error) {
	return nil, fmt.Errorf("the server's own browser is used inside a conversation; drive the attached tab instead, or ask the agent")
}

// meaningOfQuestion is what the words mean, for the searches below. A
// conversation remembers these for the length of its turn; one call asks
// once, so there is nothing to remember them for.
func (self *directRun) meaningOfQuestion(ctx context.Context, kind, words string) *meaning {
	text := cutRunes(strings.TrimSpace(words), graphEmbedCharacters)
	if text == "" {
		return nil
	}
	return self.agent.meaningOf(ctx, self.agentModel.ID, kind, text)
}

// Searching by meaning (tools.GraphSearching, tools.KnowledgeSearching), as a
// conversation does. Without these the memory and knowledge tools fell back
// to matching words alone.
func (self *directRun) SearchGraphByMeaning(ctx context.Context, words string, limit int) ([]*models.AgentNode, []*models.AgentFact) {
	nodes, facts, err := self.agent.nearestInGraphTo(ctx, self.agentModel.ID, self.meaningOfQuestion(ctx, "recall", words), limit)
	if err != nil {
		log.Warningf("cannot rank the graph of %q by meaning: %s", self.owner.Username, err)
	}
	return nodes, facts
}

func (self *directRun) SearchKnowledgeByMeaning(ctx context.Context, sourceIds []string, words string, limit int) ([]*models.AgentChunk, bool) {
	return self.agent.searchChunksByMeaning(ctx, self.agentModel.ID, self.meaningOfQuestion(ctx, "search", words), sourceIds, limit)
}

func (self *directRun) RankChunksByMeaning(ctx context.Context, words string, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if len(chunks) <= 1 {
		return chunks
	}
	return self.agent.rankChunksByMeaning(ctx, self.agentModel.ID, self.meaningOfQuestion(ctx, "search", words), chunks, limit)
}

// Remembering (tools.Remembering), the same as a conversation: without it a
// fact written from outside was filed with no vector and no check for the
// same thing already said on the page.
func (self *directRun) NoteFact(ctx context.Context, fact *models.AgentFact) []*models.AgentFact {
	return self.agent.noteFact(ctx, self.agentModel.ID, fact)
}

func (self *directRun) PreparePage(ctx context.Context, path string, kind models.AgentNodeKind, name string) tools.PreparedPage {
	return self.agent.preparePage(ctx, self.agentModel.ID, path, kind, name)
}

func (self *directRun) NoteNode(ctx context.Context, node *models.AgentNode) {
	self.agent.noteNode(ctx, self.agentModel.ID, node)
}
