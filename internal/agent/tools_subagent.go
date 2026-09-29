package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Handing a piece of work to a run of its own.
//
// Some work is a job rather than a question: read these forty messages and
// say which mention the invoice, go through that repository and find where
// the timeout is set. Done in the turn itself, each one fills the turn's
// context with material that mattered for one step and is then in the way
// of every step after it -- and a turn that runs out of room is compacted,
// which is where the thread of what the person actually asked gets lost.
//
// A subagent is that work done somewhere else: its own rounds, its own
// context, and one answer back. What it costs is that nobody reads the
// middle of it as it happens, which is why its transcript is a run of its
// own that can be read afterwards.
const (
	// subagentRounds is how many rounds one may take. Fewer than a turn's
	// forty: this is one piece of work, and a subagent still going after
	// twenty rounds has not understood it and will not on the twenty-first.
	subagentRounds = 20

	// subagentWait is how long the turn waits for it. A turn blocked on a
	// subagent is a person watching nothing happen, so this is minutes,
	// not the hour a background job may take.
	subagentWait = 10 * time.Minute
)

// subagentTool is the tool as the catalog holds it. It is built here
// rather than registered in an init like the others, because carrying it
// out starts another run, and only the agent can do that.
func (self *Agent) subagentTool() *Tool {
	return &Tool{
		Name:   "subagent",
		Family: FamilyGeneral,
		// Read, though what it does may write: the tools it uses carry
		// their own risks and ask for themselves, in this person's own
		// conversation. A card in front of the spawning, on top of the
		// cards the work itself raises, is a card that says nothing.
		Risk: tools.RiskRead,
		Description: "Hand a piece of work to a run of its own and get back what it found. It has the tools you have and answers in one message; you do not see the middle of it. Use it for work that would fill this conversation with material that matters for one step and is in the way afterwards — reading through many messages to answer one question, going through a repository, checking a list of things one at a time. Say what to do and what to answer with, in as much detail as you would give somebody else, because it cannot ask you anything: it has this conversation's question and nothing else of what you know. " +
			"Set background to start it and not wait: you get an id at once and are woken with its answer when it finishes. Choose that for work of more than a few minutes, or when you have other things to do meanwhile; a subagent in the background cannot put anything to the person for their approval, so work that needs their word is better waited for.",
		Parameters: tools.Object(map[string]any{
			"prompt":     tools.StringProperty("what to do and what to answer with, in full: it knows nothing of this conversation"),
			"title":      tools.StringProperty("a few words naming the work, for the record of the run"),
			"background": tools.BooleanProperty("true to start it and not wait; you are woken with its answer when it finishes. false, the default: wait for its answer in this turn"),
		}, "prompt"),
		Guidance: "subagent: hands one piece of work to a run of its own, so that the working does not crowd out this conversation.",
		Run:      self.runSubagent,
	}
}

func (self *Agent) runSubagent(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		Prompt     string `json:"prompt"`
		Title      string `json:"title"`
		Background bool   `json:"background"`
	}](call)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(arguments.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("say what the subagent is to do")
	}
	// The run this is being called from. It is the parent in every sense:
	// its person, its permissions, its tools, and the place its
	// confirmations are answered.
	parent, ok := tools.MustRun(ctx).(*AskRun)
	if !ok {
		return nil, fmt.Errorf("a subagent can only be started from a turn")
	}
	if parent.settings.subagentDepth > 0 {
		// Belt and braces; the tool is not offered inside one.
		return nil, fmt.Errorf("a subagent cannot start another")
	}
	title := strings.TrimSpace(arguments.Title)
	if title == "" {
		title = cut(prompt, 60)
	}
	if arguments.Background {
		return self.startBackgroundSubagent(ctx, parent, prompt, title)
	}

	conversation, err := self.createSubagentRun(ctx, parent.settings.Agent.ID, title)
	if err != nil {
		return nil, err
	}

	// The tools the parent has, less this one. That is what "the same as
	// the parent" means, and it is also what keeps the depth at one.
	allowed := map[string]bool{}
	for _, tool := range parent.offered {
		if tool.Name != "subagent" {
			allowed[tool.Name] = true
		}
	}

	turn, err := self.Ask(&AskSettings{
		Agent:        parent.settings.Agent,
		Owner:        parent.settings.Owner,
		Operations:   parent.settings.Operations,
		Conversation: conversation,
		Message:      prompt,
		Surface:      "subagent",
		ReadOnly:     parent.settings.ReadOnly,
		// Held to reading wherever the parent is. A night that may only
		// look in the graph and could hand the writing to a run of its
		// own would not be held to anything.
		ReadOnlyTools: parent.settings.ReadOnlyTools,
		Allow:         allowed,
		MaxRounds:     subagentRounds,
		UsageKind:     "subagent",
		// Not headless: somebody is present, at the other end of the turn
		// that started this. Which is what makes the next line work.
		confirmVia:    parent,
		subagentDepth: parent.settings.subagentDepth + 1,
	})
	if err != nil {
		return nil, err
	}
	answer, calls, err := followSubagent(ctx, turn, conversation.ID, subagentWait)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"answer": answer,
		// Where the working is, so that a person who wants to see what it
		// actually did can be pointed at it.
		"run":        conversation.ID,
		"tool_calls": calls,
	})
	if err != nil {
		return nil, err
	}
	result.Note = title
	return result, nil
}

// createSubagentRun makes the conversation a subagent works in: one of
// its own, of the kind runs use, so the working is kept and readable
// afterwards without being in the middle of what the person is reading
// now.
func (self *Agent) createSubagentRun(ctx context.Context, agentId, title string) (*models.AgentConversation, error) {
	var conversation *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: agentId,
			Kind:    models.AgentConversationRun,
			Title:   "Subagent: " + title,
			JobKind: string(models.AgentJobSubagent),
			Surface: "subagent",
			LastAt:  time.Now(),
		})
		return err
	}); err != nil {
		return nil, err
	}
	return conversation, nil
}

// followSubagent waits for a subagent's turn to end, at most wait, and
// says what it answered and how many tools it called. A turn still going
// when the wait is up, or when ctx ends, is stopped.
func followSubagent(ctx context.Context, turn *AskRun, conversationId string, wait time.Duration) (string, int, error) {
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	answer, failure, calls := "", "", 0
	for waiting := true; waiting; {
		select {
		case event, open := <-events:
			if !open {
				waiting = false
				break
			}
			switch event.Kind {
			case EventMessage:
				answer = event.Text
			case EventToolCall:
				calls++
			case EventError:
				failure = event.Error
			}
		case <-timer.C:
			turn.Stop()
			failure = fmt.Sprintf("it was still going after %s and was stopped", wait)
			waiting = false
		case <-ctx.Done():
			turn.Stop()
			return "", calls, ctx.Err()
		}
	}
	if failure != "" {
		return "", calls, fmt.Errorf("the subagent did not finish: %s", failure)
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", calls, fmt.Errorf("the subagent ended without answering; its working is in the run %s", conversationId)
	}
	return answer, calls, nil
}

// startBackgroundSubagent starts a subagent that the turn does not wait
// for. Its tools are the parent's less the ones that start or manage work
// of this kind, fixed now and kept with the work, so that a run after a
// restart has the same.
func (self *Agent) startBackgroundSubagent(ctx context.Context, parent *AskRun, prompt, title string) (*Result, error) {
	request := models.AgentBackgroundWorkRequest{Prompt: prompt, IsReadOnly: parent.settings.ReadOnly, AllowedToolNames: []string{}}
	for _, tool := range parent.offered {
		switch tool.Name {
		case "subagent", "survey", "background_work":
			continue
		}
		request.AllowedToolNames = append(request.AllowedToolNames, tool.Name)
	}
	for name, isReadOnly := range parent.settings.ReadOnlyTools {
		if isReadOnly {
			request.ReadOnlyToolNames = append(request.ReadOnlyToolNames, name)
		}
	}
	sort.Strings(request.AllowedToolNames)
	sort.Strings(request.ReadOnlyToolNames)
	work, err := self.startBackgroundWork(ctx, parent, &models.AgentBackgroundWork{
		AgentID: parent.settings.Agent.ID, WorkKind: models.BackgroundWorkSubagent, Title: title, WorkRequest: request,
	})
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"backgroundWorkId": work.ID,
		"note":             "The subagent has started in the background. Tell the person it has started; you will be woken with its answer when it finishes. background_work reads or stops it.",
	})
	if err != nil {
		return nil, err
	}
	result.Note = title
	return result, nil
}
