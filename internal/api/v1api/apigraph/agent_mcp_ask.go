package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mcp"
	"github.com/ziyan/teanode/internal/models"
)

// Putting a question to the agent, as one tool among its tools.
//
// Everything else a harness gets here is a tool run directly, which is
// what a caller wants when it knows what it is after. This is the other
// half: a question in words, answered by the agent's own loop, with the
// whole of its kit and its memory of the person behind it. A harness that
// would have had to orchestrate six calls asks once.
//
// It is not in the agent's own catalog, only in what this endpoint offers,
// which is what stops the agent asking itself.

// mcpAskName is the tool as a harness sees it.
const mcpAskName = "teanode_ask"

// mcpAskWait is how long one call waits for the answer.
//
// Bounded by what is at the other end rather than by what a turn takes. A
// harness gives a tool call somewhere around a minute before it gives up,
// and a call that is still waiting when it does is work thrown away. So
// this returns before then with what the turn has said so far and the
// conversation it is in, and the caller asks again to hear the rest. A
// turn is not cancelled by this returning; it carries on, and the answer
// is there to be collected.
//
// A variable rather than a constant so that the test for what happens when
// the wait runs out does not have to take a minute to find out.
var mcpAskWait = 55 * time.Second

// mcpAskTool is the tool as the protocol describes it.
func mcpAskTool() mcp.Tool {
	return mcp.Tool{
		Name: mcpAskName,
		Description: "Put a question to this person's own agent, in words, and get its answer. " +
			"It can search their mail, their calendar, their contacts and everything its nightly " +
			"reading has learned about their work and the people in it, and it can use every tool " +
			"listed here. Slower than calling one of those tools, and worth it for anything " +
			"open-ended, anything needing judgement, or anything you would otherwise have to work " +
			"out across several calls. Leave the question out and give a conversation to go on " +
			"waiting for an answer that was not finished.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "what to ask, in words",
				},
				"conversation": map[string]any{
					"type": "string",
					"description": "the conversation to ask in, to carry on an earlier exchange " +
						"or to wait for an answer that was not finished; the main one when left out",
				},
			},
		},
	}
}

// ask puts the question and waits for as much of the answer as it can.
func (self *mcpCatalog) ask(ctx context.Context, arguments json.RawMessage) (string, error) {
	var call struct {
		Question     string `json:"question"`
		Conversation string `json:"conversation"`
	}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &call); err != nil {
			return "", fmt.Errorf("the arguments are not an object: %w", err)
		}
	}
	question := strings.TrimSpace(call.Question)

	var conversation *models.AgentConversation
	if err := self.graph.database.Transaction(func(tx db.Transaction) (err error) {
		conversation, err = self.graph.ownConversation(tx, self.person, call.Conversation, false)
		return err
	}); err != nil || conversation == nil {
		return "", fmt.Errorf("there is no conversation of yours with that name")
	}

	var run *agent.AskRun
	if question == "" {
		// Going on waiting for an answer a previous call left unfinished.
		// A turn that finished in the meantime is handed back too, and
		// replays what it said, so coming back a second late still hears
		// the answer.
		if run = self.worker.LatestIn(conversation.ID); run == nil {
			return "", fmt.Errorf(
				"nothing has been asked in that conversation lately: ask a question rather than waiting for one")
		}
	} else {
		var err error
		run, err = self.worker.Ask(&agent.AskSettings{
			Agent:        self.person,
			Owner:        self.owner,
			Operations:   self.operations,
			Conversation: conversation,
			Message:      question,
			Surface:      mcpSurface,
		})
		if err != nil {
			return "", err
		}
	}
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	return mcpCollect(ctx, events, self.owner.Name, conversation.ID), nil
}

// mcpCollect follows a turn until it answers, or until the wait is up.
//
// What comes back is the agent's words. The things that happened on the
// way -- a tool it reached for, a card it raised -- are said only where
// the caller has to know: a turn that stopped to ask the person something
// cannot be answered from here, and saying so is the difference between a
// harness telling its person where to go and a harness waiting for nothing.
// Written over the events rather than over the run, so that what it does
// with a turn that stops to ask, fails, says nothing or outlasts the wait
// can be tested by handing it those events.
func mcpCollect(ctx context.Context, events <-chan agent.Event, personName, conversationId string) string {
	deadline := time.NewTimer(mcpAskWait)
	defer deadline.Stop()

	var answer, pieces strings.Builder
	waitingFor := ""
	failed := ""
	finished := false

	for !finished {
		select {
		case event, open := <-events:
			if !open {
				finished = true
				break
			}
			switch event.Kind {
			case agent.EventMessage:
				// The whole answer, once there is one. A turn can say more
				// than one thing, so they are kept in order rather than
				// the last one winning.
				if text := strings.TrimSpace(event.Text); text != "" {
					if answer.Len() > 0 {
						answer.WriteString("\n\n")
					}
					answer.WriteString(text)
				}
			case agent.EventText:
				pieces.WriteString(event.Text)
			case agent.EventConfirmation:
				waitingFor = "The agent has stopped to ask " + personName +
					" whether to go ahead with something. Nobody here can answer that; " +
					"it is waiting in their dashboard."
			case agent.EventQuestion:
				waitingFor = "The agent has a question for " + personName +
					", waiting in their dashboard: " + strings.TrimSpace(event.Text)
			case agent.EventError:
				failed = strings.TrimSpace(event.Error)
			case agent.EventDone:
				finished = true
			}
		case <-deadline.C:
			said := strings.TrimSpace(answer.String())
			if said == "" {
				said = strings.TrimSpace(pieces.String())
			}
			unfinished := fmt.Sprintf(
				"The agent is still working on this. Call %s again with conversation %q "+
					"and no question to go on waiting.", mcpAskName, conversationId)
			if said == "" {
				return unfinished
			}
			return said + "\n\n" + unfinished
		case <-ctx.Done():
			return fmt.Sprintf(
				"The connection went while the agent was working. What it says will be in "+
					"conversation %q when it is done.", conversationId)
		}
	}

	said := strings.TrimSpace(answer.String())
	if said == "" {
		said = strings.TrimSpace(pieces.String())
	}
	switch {
	case waitingFor != "" && said != "":
		return said + "\n\n" + waitingFor
	case waitingFor != "":
		return waitingFor
	case failed != "" && said != "":
		return said + "\n\nThe turn ended with an error: " + failed
	case failed != "":
		return "The agent could not answer: " + failed
	case said == "":
		return "The agent finished without saying anything."
	}
	return said
}
