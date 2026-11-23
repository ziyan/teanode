// Package goal is the conversation's standing instruction: the sentence
// the agent keeps working toward across turns of its own, and the one tool
// that says where it is with it. A goal turn ends by calling this tool
// once -- with a note and when to look again, with what it needs from the
// person, or with the word that it is done -- and that call is the whole
// of the cadence, unless the goal has times, which are.
package goal

import (
	"context"
	"fmt"
	"regexp"
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
				Description: "Goals: what you keep at for the person in the background, between conversations, each in a conversation of its own where your turns on it run out of their sight, until it is met or they drop it. They hear from a goal only when it needs them, or in their feed. " +
					"`start` starts one, with a title of a few words and text saying what it is for and what done looks like; use it when they ask you to keep at, watch for or follow up on something that outlasts this conversation, and for anything at a time they name. `list` is every goal and where it stands; `show` one with what happened on it and what it made. " +
					"A goal paces itself, unless it is given times or on_mail: times are cron lines in the person's zone (\"30 7 * * 1-5\" is weekdays at 7:30) or single moments (\"@at 2026-09-12 09:00\", \"@in 2h\"), and on_mail runs it whenever a message passes every test. Its turns are then at those times, and the text says what to do at each: \"every weekday at 8, tell me what needs me\" is a goal with times [\"0 8 * * 1-5\"]; a reminder in two hours is a goal with times [\"@in 2h\"]. " +
					"`tell` passes the person's words to a goal by goal_id: their answer to what it asked, or something it should know. `set` changes a goal's title, text, times, on_mail or is_notified_when_posted; `run` gives it a turn now. `done`, `drop`, `pause` and `resume` are theirs: a goal met, stopped, switched off for now, or taken up again. " +
					"In a goal's own turns: `note` with status (where it stands, one line), the minutes until your next turn (not used for a goal with times), and activity when something happened worth their reading later; `wait` with status saying what you need from them, which is said to them in their main conversation; `met` with text saying how it ended, as soon as it is done.",
				Parameters: tools.Object(map[string]any{
					"action":  tools.EnumProperty("what to do", "start", "list", "show", "tell", "set", "run", "done", "drop", "pause", "resume", "note", "wait", "met"),
					"goal_id": tools.StringProperty("for show, tell, set, run, done, drop, pause and resume: the goal, by the id list gives"),
					"title":   tools.StringProperty("for start and set: what the goal is called, a few words"),
					"text":    tools.StringProperty("for start and set: what it is for and what done looks like, and for a goal with times what to do at each. For tell: the person's words. For met: how it ended and what came of it; when the person was not there it is what they are told in their main conversation"),
					"status":  tools.StringProperty("for note: where the goal stands, in one line. For wait: what you need from the person, a sentence they can answer"),
					"times":   tools.ArrayProperty("for start and set: when its turns are, in the person's zone; each five cron fields (minute hour day month weekday, such as 0 8 * * 1-5), or once: @at 2026-09-12 09:00, or @in 5m, @in 2h, @in 1 day. For set, an empty list takes them away", tools.StringProperty("a cron line or a moment")),
					"on_mail": tools.ArrayProperty("for start and set: also take a turn whenever a message arrives, in a mailbox you were given, that passes every one of these tests, as a mailbox rule's are; for example from contains news@school.example. For set, an empty list takes them away", tools.Object(map[string]any{
						"field":    tools.EnumProperty("what to test", models.GoalMailFields...),
						"header":   tools.StringProperty("for header: the header's name"),
						"operator": tools.EnumProperty("how to compare", "contains", "equals", "matches"),
						"value":    tools.StringProperty("what to compare with"),
					})),
					"is_notified_when_posted": tools.BooleanProperty("for start and set, a goal with times: send one quiet notification, \"<title> is ready\", when the first card of a turn is posted; for a daily brief"),
					"activity":                tools.StringProperty("for note: one line on what happened, when something did that the person would want in the goal's log; leave it out when you only looked"),
					"minutes":                 tools.IntegerProperty("for note: how long until your next turn on this, from 5 to 1440; 30 by default"),
				}, "action"),
				Preview: tools.PreviewOf(func(call struct {
					Action string   `json:"action"`
					Title  string   `json:"title"`
					Times  []string `json:"times"`
				}) string {
					named := tools.Named(call.Title, "a goal")
					when := ""
					if len(call.Times) > 0 {
						when = ", at " + strings.Join(call.Times, ", ")
					}
					switch call.Action {
					case "start":
						return "Start " + named + when
					case "set":
						return "Change the goal " + named + when
					case "run":
						return "Run a goal now"
					}
					return "Change your goals"
				}),
				Guidance: "goal: `list` before you `start` one, so you do not start one twice. Write the title, text, status and activity for the person to read, in plain words: no ids, no tool names, no file paths unless they would use them; a goal or a conversation is named, never numbered. Someone saying what they hope for (\"my goal is to run a marathon\") is not asking you to keep at anything: talk about it, and start a goal only if they ask you to work on it over time.",
				Run:      run,
			},
		}
	})
}

type arguments struct {
	Action   string  `json:"action"`
	GoalID   string  `json:"goal_id"`
	Title    *string `json:"title"`
	Text     string  `json:"text"`
	Status   string  `json:"status"`
	Activity string  `json:"activity"`

	Times                *[]string                      `json:"times"`
	OnMail               *[]models.MailboxRuleCondition `json:"on_mail"`
	IsNotifiedWhenPosted *bool                          `json:"is_notified_when_posted"`
	Minutes              int                            `json:"minutes"`
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
	// A note's and a wait's words are its status; a model that put them in
	// text is read the same.
	if status := strings.TrimSpace(asked.Status); status != "" && (action == "note" || action == "wait") {
		text = status
	}
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
		// background work, or a later turn of its own.
		if isGoalTurn {
			return nil, fmt.Errorf("a goal does not start goals; give the minutes until your next turn with note, or start background work")
		}
		if text == "" {
			return nil, fmt.Errorf("say what the goal is for and what done looks like, in text")
		}
		variables := goalVariables(&asked)
		variables["goalDescription"] = text
		if here != nil {
			variables["originConversationId"] = here.ID
		}
		note := "the goal is started; its first turn runs at once, in its own conversation"
		if asked.Times != nil && len(*asked.Times) > 0 || asked.OnMail != nil && len(*asked.OnMail) > 0 {
			note = "the goal is started; its turns run at its times, in its own conversation"
		}
		return operate(ctx, client.DocumentStartAgentGoal, variables, "StartAgentGoal", note+passwordsNoticeFor(text))
	case "tell", "set", "run", "done", "drop", "pause", "resume":
		// The person's words and the person's decisions: from a turn
		// they are in, never one of the agent's own.
		if current.Headless() {
			return nil, fmt.Errorf("only the person answers, changes, closes or pauses a goal; in a goal's own turn, say note, wait or met")
		}
		if goalId == "" && here.IsGoal() {
			goalId = here.ID
		}
		if goalId == "" {
			return nil, fmt.Errorf("which goal? give goal_id, from list")
		}
		switch action {
		case "set":
			variables := goalVariables(&asked)
			if text != "" {
				variables["goalDescription"] = text
			}
			variables["conversationId"] = goalId
			return operate(ctx, client.DocumentUpdateAgentGoal, variables, "UpdateAgentGoal", "the goal is changed"+passwordsNoticeFor(text))
		case "run":
			return operate(ctx, client.DocumentRunAgentGoal, map[string]any{"conversationId": goalId}, "RunAgentGoal", "the goal takes a turn now, in its own conversation")
		}
		if action == "tell" {
			if text == "" {
				return nil, fmt.Errorf("say what the person said, in text")
			}
			return operate(ctx, client.DocumentTellAgentGoal, map[string]any{"conversationId": goalId, "text": text}, "TellAgentGoal", "told; the goal goes on in a minute")
		}
		goalState := map[string]string{"done": "met", "drop": "dropped", "pause": "paused", "resume": "working"}[action]
		return operate(ctx, client.DocumentSetAgentGoalState, map[string]any{"conversationId": goalId, "goalState": goalState}, "SetAgentGoalState", "the goal is "+goalState)
	}
	// Note, wait and met are a goal's own turn saying where it stands, in
	// the goal's conversation; anywhere else there is no goal to say it of.
	if !here.IsGoal() {
		return nil, fmt.Errorf("%s is said in a goal's own turn; this is not a goal's conversation. To start a goal, use start", action)
	}
	if runes := []rune(text); len(runes) > NoteCharacters {
		text = string(runes[:NoteCharacters])
	}
	activity := strings.TrimSpace(asked.Activity)

	var after *models.AgentConversation
	if err := current.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		updated, err := tx.UpdateAgentConversation(here.ID, func(conversation *models.AgentConversation) error {
			switch action {
			case "note", "wait", "met":
				if conversation.Goal == "" {
					return fmt.Errorf("there is no goal on this conversation; start one with action start")
				}
				if conversation.GoalState == models.GoalDropped {
					return fmt.Errorf("the person dropped this goal; it takes no more turns")
				}
				if conversation.GoalState == models.GoalPaused && current.Headless() {
					return fmt.Errorf("the person paused this goal; only they take it up again")
				}
				// A goal that is done stays done: only the person takes it
				// up again.
				if conversation.GoalState == models.GoalMet && current.Headless() {
					return fmt.Errorf("this goal is done; only the person takes it up again")
				}
				conversation.GoalNote = text
				if action == "note" {
					conversation.GoalState = models.GoalWorking
					// A goal on set times runs at them, and books no turn
					// of its own: the worker moves it on after the turn.
					if !conversation.IsGoalOnSetTimes() {
						next := time.Now().Add(interval(asked.Minutes))
						conversation.GoalNextAt = &next
					}
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
				// A goal waiting or finished with nobody present is said in
				// the person's main conversation by the worker's next
				// sweep; in front of them it needs no call.
				if current.Headless() {
					conversation.GoalSurfacedAt = nil
				} else {
					now := time.Now()
					conversation.GoalSurfacedAt = &now
				}
				return nil
			}
			return fmt.Errorf("%q is not start, list, show, tell, set, run, done, drop, pause, resume, note, wait or met", action)
		})
		if err != nil {
			return err
		}
		after = updated
		return goalActivity(tx, updated, action, text, activity)
	}); err != nil {
		return nil, err
	}

	answer := map[string]any{"goalDescription": after.Goal, "goalState": string(after.GoalState), "goalStatus": after.GoalNote}
	note := ""
	switch {
	case after.GoalNextAt != nil:
		answer["goalNextAt"] = after.GoalNextAt.Format(time.RFC3339)
		note = fmt.Sprintf("goal: %s, next turn %s", after.GoalState, after.GoalNextAt.Format("15:04"))
	case after.GoalState == models.GoalWaiting && after.GoalSurfacedAt == nil:
		note = "goal: waiting; the person is told in their main conversation"
	case after.GoalState == models.GoalMet && after.GoalSurfacedAt == nil:
		note = "goal: met; the person is told in their main conversation"
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

// passwordsNotice is said to the model when it starts or rewords a goal about
// signing in, so that it tells the person. A goal the agent wrote cannot use
// Passwords; one the person saves themselves, on times they chose, can.
const passwordsNotice = "This goal was written by you, the agent, so its turns cannot use Passwords. Tell the person: they can open the goal on the dashboard or in the app (Goals) and save it themselves, with the times it runs at, to let its turns use Passwords."

// signingInWords matches words about signing in, a password, a passkey, a
// login or a one-time code.
var signingInWords = regexp.MustCompile(`(?i)\b(sign(s|ed|ing)?[ -]?in(to)?|log(s|ged|ging)?[ -]?(ins?|into)|passwords?|passkeys?|one-time codes?)\b`)

// passwordsNoticeFor is the notice, after a separator, when the words are
// about signing in, and nothing otherwise.
func passwordsNoticeFor(text string) string {
	if signingInWords.MatchString(text) {
		return ". " + passwordsNotice
	}
	return ""
}

// goalVariables is what start and set send of what the model gave: only what
// it gave, so that set changes nothing else.
func goalVariables(asked *arguments) map[string]any {
	variables := map[string]any{}
	if asked.Title != nil {
		variables["goalTitle"] = strings.TrimSpace(*asked.Title)
	}
	if asked.Times != nil {
		variables["goalTimes"] = *asked.Times
	}
	if asked.OnMail != nil {
		variables["goalMailConditions"] = *asked.OnMail
	}
	if asked.IsNotifiedWhenPosted != nil {
		variables["isGoalNotifiedWhenPosted"] = *asked.IsNotifiedWhenPosted
	}
	return variables
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
