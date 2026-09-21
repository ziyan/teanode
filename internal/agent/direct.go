package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ziyan/teanode/internal/agent/tools"
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
		operations: operations, surface: surface, offered: offered,
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
