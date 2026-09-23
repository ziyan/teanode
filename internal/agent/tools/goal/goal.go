// Package goal is the conversation's standing instruction: the sentence
// the agent keeps working toward across turns of its own, and the one tool
// that says where it is with it. A goal turn ends by calling this tool
// once -- with a note and when to look again, with what it needs from the
// person, or with the word that it is done -- and that call is the whole
// of the cadence.
package goal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds on how often the agent may take a turn of its own. Constants
// rather than a setting: a number nobody has asked to change does not need
// a page in the dashboard, and the day somebody does it becomes one.
//
// The floor is what keeps a goal from spinning: five minutes is long
// enough that a model which answers "check again in a minute" to
// everything costs twelve turns an hour rather than sixty. The ceiling is
// a day, because a goal nothing has happened on for longer than that is
// one to ask the person about rather than to work on quietly.
const (
	Soonest         = 5 * time.Minute
	Latest          = 24 * time.Hour
	DefaultInterval = 30 * time.Minute
)

// NoteCharacters is as much of the agent's note as is kept. The note is
// shown beside the conversation and, while the goal waits, in a bar above
// the box the person types in, so it is a sentence or two by design and
// cut here when it is not.
const NoteCharacters = 500

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "goal", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "The goal on this conversation: the one thing you keep working toward across turns of your own until it is met or the person clears it. `set` puts a goal on the conversation, or clears it with empty text, and is only for when the person asks you to keep at something. `note` says where you are and how many minutes until you look again. `wait` says you need them, and stops your own turns until they write. `met` says it is done.",
				Parameters: tools.Object(map[string]any{
					"action":  tools.EnumProperty("what to say about the goal", "set", "note", "wait", "met"),
					"text":    tools.StringProperty("for set: the goal, in the person's words, or empty to clear it. For note, wait and met: a sentence or two on where you are, what you need, or how it ended"),
					"minutes": tools.IntegerProperty("for note: how long until your next turn on this, from 5 to 1440; 30 by default"),
				}, "action"),
				Guidance: "goal: while a goal is set you take turns here on your own, and each ends with exactly one call to this tool: `note` where you are and the minutes until it is worth looking again; `wait` with one line saying what you need from the person, which is shown above the box they type in; `met` when it is done. A sentence or two each. `set` a goal only when the person asks you to keep working at something, never in a check-in of your own.",
				Run:      run,
			},
		}
	})
}

type arguments struct {
	Action  string `json:"action"`
	Text    string `json:"text"`
	Minutes int    `json:"minutes"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(asked.Action))
	text := strings.TrimSpace(asked.Text)
	if runes := []rune(text); len(runes) > NoteCharacters {
		text = string(runes[:NoteCharacters])
	}
	here := current.Conversation()
	if here == nil {
		return nil, fmt.Errorf("there is no conversation to put a goal on")
	}

	var after *models.AgentConversation
	if err := current.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		updated, err := tx.UpdateAgentConversation(here.ID, func(conversation *models.AgentConversation) error {
			switch action {
			case "set":
				// A goal spends the person's budget with nobody watching,
				// so starting one is theirs: through the dashboard, the
				// command line, or this tool when they have just asked
				// for it. A turn of the agent's own cannot.
				if current.Headless() {
					return fmt.Errorf("a goal is the person's to set; you cannot set one in a turn of your own")
				}
				if conversation.Kind == models.AgentConversationRun {
					return fmt.Errorf("a goal goes on a conversation with the person, not on the record of a run")
				}
				if text == "" {
					conversation.Goal, conversation.GoalState, conversation.GoalNote, conversation.GoalNextAt, conversation.GoalSetAt = "", "", "", nil, nil
					return nil
				}
				now := time.Now()
				conversation.Goal, conversation.GoalState, conversation.GoalNote, conversation.GoalNextAt, conversation.GoalSetAt = text, models.GoalWorking, "", &now, &now
				return nil
			case "note", "wait", "met":
				if conversation.Goal == "" {
					return fmt.Errorf("there is no goal on this conversation; only the person sets one")
				}
				conversation.GoalNote = text
				if action == "note" {
					next := time.Now().Add(interval(asked.Minutes))
					conversation.GoalState, conversation.GoalNextAt = models.GoalWorking, &next
					return nil
				}
				// Waiting and met both stop the turns: nothing is
				// scheduled, and it is the person who starts them again --
				// by writing, which resumes a goal that waits, or by
				// setting a goal again on one that is met.
				conversation.GoalNextAt = nil
				if action == "wait" {
					conversation.GoalState = models.GoalWaiting
				} else {
					conversation.GoalState = models.GoalMet
				}
				return nil
			}
			return fmt.Errorf("%q is not set, note, wait or met", action)
		})
		if err != nil {
			return err
		}
		after = updated
		// Set and met are moments of the conversation, so they are
		// written into it; a note or a wait is the chip's and the bar's.
		if note := models.GoalChangeNote(here, updated); note != "" {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: here.ID, Role: models.AgentMessageNote, Content: note}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	answer := map[string]any{"goal": after.Goal, "state": string(after.GoalState), "note": after.GoalNote}
	note := ""
	switch {
	case after.Goal == "":
		note = "the goal is cleared"
	case after.GoalNextAt != nil:
		answer["next_turn_at"] = after.GoalNextAt.Format(time.RFC3339)
		note = fmt.Sprintf("goal: %s, next turn %s", after.GoalState, after.GoalNextAt.Format("15:04"))
	default:
		note = "goal: " + string(after.GoalState)
	}
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = note
	return result, nil
}

// interval is how long until the next turn: what the model asked for, as
// the bounds allow. A model that gives no number, or one it made up out of
// nothing, gets the default rather than a refusal -- the turn is over by
// the time this is called, and a refusal would leave the goal with no next
// time at all.
func interval(minutes int) time.Duration {
	if minutes <= 0 {
		return DefaultInterval
	}
	asked := time.Duration(minutes) * time.Minute
	if asked < Soonest {
		return Soonest
	}
	if asked > Latest {
		return Latest
	}
	return asked
}
