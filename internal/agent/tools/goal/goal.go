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
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
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
				Description: "Goals: what you keep at for the person in the background, between conversations, each in a conversation of its own where your turns on it run out of their sight, until it is met or they drop it. They hear from a goal only when it needs them. " +
					"`start` starts one, with a title of a few words and text saying what it is for and what done looks like; use it when they ask you to keep at, watch for or follow up on something that outlasts this conversation. `list` is every goal and where it stands; `show` one with what happened on it and what it made. " +
					"`tell` passes the person's words to a goal by goal_id: their answer to what it asked, or something it should know. `done`, `drop` and `reopen` are theirs: a goal met, stopped, or taken up again. " +
					"In a goal's own turns: `note` with the status in one line, the minutes until your next turn, and activity when something happened worth their reading later; `wait` with what you need from them, which is said to them in their main conversation; `met` when it is done.",
				Parameters: tools.Object(map[string]any{
					"action":   tools.EnumProperty("what to do", "start", "list", "show", "tell", "done", "drop", "reopen", "note", "wait", "met", "set"),
					"goal_id":  tools.StringProperty("for show, tell, done, drop and reopen: the goal, by the id list gives"),
					"title":    tools.StringProperty("for start: what the goal is called, a few words"),
					"text":     tools.StringProperty("for start: what it is for and what done looks like. For tell: the person's words. For note: the status in one line. For wait: what you need from them, a sentence they can answer. For met: how it ended"),
					"activity": tools.StringProperty("for note: one line on what happened, when something did that the person would want in the goal's log; leave it out when you only looked"),
					"minutes":  tools.IntegerProperty("for note: how long until your next turn on this, from 5 to 1440; 30 by default"),
				}, "action"),
				Guidance: "goal: `start` a goal when the person asks you to keep at something beyond this conversation; `list` first so you do not start one twice. A goal's turns run in its own conversation and end with exactly one call: `note` (status in one line, activity only when something happened), `wait` (what you need from them, said to them once in their main conversation) or `met`. When the person answers a goal that waited for them, pass their words on with `tell` and its goal_id; when they say it is done or to stop, `done` or `drop`.",
				Run:      run,
			},
		}
	})
}

type arguments struct {
	Action   string `json:"action"`
	GoalID   string `json:"goal_id"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	Activity string `json:"activity"`
	Minutes  int    `json:"minutes"`
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
	here := current.Conversation()
	isGoalTurn := here.IsGoal() && current.Headless()
	goalId := strings.TrimSpace(asked.GoalID)

	// Across goals, through the operations the dashboard's Goals tab and
	// the command line call, so the three list, start and close the same.
	switch action {
	case "list":
		return operate(ctx, client.DocumentListAgentGoals, map[string]any{}, "ListAgentGoals", "")
	case "show":
		if goalId == "" {
			return nil, fmt.Errorf("which goal? give goal_id, from list")
		}
		return operate(ctx, client.DocumentGetAgentGoal, map[string]any{"conversationId": goalId}, "GetAgentGoal", "")
	case "start":
		// A goal's own turns do not start more goals: work it needs is
		// background work or a schedule of its own.
		if isGoalTurn {
			return nil, fmt.Errorf("a goal does not start goals; schedule what should happen on a clock, or start background work")
		}
		if text == "" {
			return nil, fmt.Errorf("say what the goal is for and what done looks like, in text")
		}
		variables := map[string]any{"goalTitle": strings.TrimSpace(asked.Title), "goalDescription": text}
		if here != nil {
			variables["originConversationId"] = here.ID
		}
		return operate(ctx, client.DocumentStartAgentGoal, variables, "StartAgentGoal", "the goal is started; its first turn runs at once, in its own conversation")
	case "tell", "done", "drop", "reopen":
		// The person's words and the person's decisions: from a turn
		// they are in, never one of the agent's own.
		if current.Headless() {
			return nil, fmt.Errorf("only the person answers, closes or reopens a goal; in a goal's own turn, say note, wait or met")
		}
		if goalId == "" {
			return nil, fmt.Errorf("which goal? give goal_id, from list")
		}
		if action == "tell" {
			if text == "" {
				return nil, fmt.Errorf("say what the person said, in text")
			}
			return operate(ctx, client.DocumentTellAgentGoal, map[string]any{"conversationId": goalId, "text": text}, "TellAgentGoal", "told; the goal goes on in a minute")
		}
		goalState := map[string]string{"done": "met", "drop": "dropped", "reopen": "working"}[action]
		return operate(ctx, client.DocumentSetAgentGoalState, map[string]any{"conversationId": goalId, "goalState": goalState}, "SetAgentGoalState", "the goal is "+goalState)
	}
	if here == nil {
		return nil, fmt.Errorf("there is no conversation here with a goal")
	}
	if runes := []rune(text); len(runes) > NoteCharacters {
		text = string(runes[:NoteCharacters])
	}
	activity := strings.TrimSpace(asked.Activity)

	var after *models.AgentConversation
	if err := current.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		updated, err := tx.UpdateAgentConversation(here.ID, func(conversation *models.AgentConversation) error {
			switch action {
			case "set":
				// Goals run in the background now, each in a conversation
				// of its own; a goal left on this one from before can only
				// be cleared.
				if text != "" {
					return fmt.Errorf("goals run in the background now; start one with action start")
				}
				if current.Headless() {
					return fmt.Errorf("a goal is the person's to clear; you cannot clear one in a turn of your own")
				}
				conversation.Goal, conversation.GoalState, conversation.GoalNote, conversation.GoalNextAt, conversation.GoalSetAt = "", "", "", nil, nil
				return nil
			case "note", "wait", "met":
				if conversation.Goal == "" {
					return fmt.Errorf("there is no goal on this conversation; start one with action start")
				}
				if conversation.GoalState == models.GoalDropped {
					return fmt.Errorf("the person dropped this goal; it takes no more turns")
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
					// A goal waiting with nobody present is said in the
					// person's main conversation by the worker's next
					// sweep; one waiting in front of them needs no call.
					if current.Headless() {
						conversation.GoalSurfacedAt = nil
					} else {
						now := time.Now()
						conversation.GoalSurfacedAt = &now
					}
				} else {
					conversation.GoalState = models.GoalMet
				}
				return nil
			}
			return fmt.Errorf("%q is not start, list, show, tell, done, drop, reopen, note, wait or met", action)
		})
		if err != nil {
			return err
		}
		after = updated
		if updated.IsGoal() {
			if err := goalActivity(tx, updated, action, text, activity); err != nil {
				return err
			}
		}
		// Set and met are moments of the conversation, so they are
		// written into it; a note or a wait is the chip's and the bar's.
		if kind, detail := models.GoalChangeNote(here, updated); kind != "" {
			if _, err := tx.AppendAgentMessage(models.NewAgentNote(here.ID, kind, detail)); err != nil {
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
	case after.GoalState == models.GoalWaiting && after.IsGoal() && after.GoalSurfacedAt == nil:
		note = "goal: waiting; the person is told in their main conversation"
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

// goalActivity writes the row of a goal's log that a call of the agent's
// own makes: a note only when it says something happened, a wait and a
// met always.
func goalActivity(tx db.Transaction, goal *models.AgentConversation, action, text, activity string) error {
	var activityKind models.AgentGoalActivityKind
	headline, detail := "", ""
	switch action {
	case "note":
		if activity == "" {
			return nil
		}
		activityKind, headline, detail = models.GoalActivityProgress, activity, text
	case "wait":
		activityKind, headline, detail = models.GoalActivityWaiting, "Needs you", text
	case "met":
		activityKind, headline, detail = models.GoalActivityMet, "Done", text
	default:
		return nil
	}
	_, err := tx.AddAgentGoalActivity(&models.AgentGoalActivity{
		AgentID: goal.AgentID, ConversationID: goal.ID, GoalActivityKind: activityKind,
		ActivityHeadline: headline, ActivityDetail: detail,
	})
	return err
}

// operate runs one of the goal operations as the person and answers with
// what it returned.
func operate(ctx context.Context, document string, variables map[string]any, operation, note string) (*tools.Result, error) {
	result, err := operator.Execute(ctx, document, variables)
	if err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(result[operation])
	if err != nil {
		return nil, err
	}
	answer.Note = note
	return answer, nil
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
