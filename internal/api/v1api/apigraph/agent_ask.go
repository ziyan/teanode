package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/travel/rebuild"
)

// Ask: the person talking to their agent. A turn is started here, runs in
// the agent package, and streams back over the websocket as a
// subscription; a confirmation card is answered here; a conversation is
// listed, started, renamed and archived here. What the agent does for the
// person goes through the same API the person uses, executed as them by
// agentOperations, so the agent can do exactly what they can do.

// AgentAskQuery is the conversations and their turns.
type AgentAskQuery interface {
	// The caller's conversations: the main one with their agent first, then
	// the named ones and their chats with other people by last message;
	// archived ones only when asked. A chat comes with the people in it, how
	// many messages the caller has not read, and whether they silenced it.
	// Needs agent:use, or circle:use for the chats alone: a person whose
	// agent is off is listed their chats and nothing else.
	ListAgentConversations(ctx context.Context, arguments ListAgentConversationsArguments) ([]*models.AgentConversation, error)

	// One conversation and its messages, oldest first; the main
	// conversation when no id is given (made if it does not exist yet). A
	// chat's messages are what the people and their agents said, from when
	// the caller joined until they left if they did, with the caller's own
	// agent's rows among them by time. Needs agent:use, or circle:use for a
	// chat.
	ReadAgentConversation(ctx context.Context, arguments ReadAgentConversationArguments) (*AgentConversationView, error)

	// What happened in a turn so far, from a sequence number on; waits up
	// to the given seconds for more when nothing is new, for a client with
	// no websocket. Needs agent:use.
	ReadAgentRun(ctx context.Context, arguments ReadAgentRunArguments) (*AgentRunView, error)

	// The catalog as the caller sees it: family, name, risk class, whether
	// it asks first. Needs agent:use.
	ListAgentTools(ctx context.Context) ([]*AgentToolView, error)

	// The transcripts of the agent's runs — sorting, summaries, replies —
	// newest first: what it did while nobody was there. Needs agent:use.
	ListAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) (*AgentRunPage, error)
	// ListAllAgentRuns is every person's runs, for an operator with
	// agent:act: what every agent on the server did, openable.
	ListAllAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) (*AgentRunPage, error)
}

// AgentAskMutation is a turn, its confirmations, and the conversations.
type AgentAskMutation interface {
	// Say something to the agent. Starts a turn in the conversation given,
	// or in the main one, and returns the run to follow. In a chat it says
	// the words to the people instead, starts no turn and returns no run.
	// Needs agent:use, or circle:use for a chat.
	AskAgent(ctx context.Context, arguments AskAgentArguments) (*AgentTurnView, error)

	// Answer a confirmation card: approve or decline what the agent asked
	// to do. Needs agent:use.
	ResolveAgentConfirmation(ctx context.Context, arguments ResolveAgentConfirmationArguments) (bool, error)

	// Say where the browser is, answering a turn that asked with the
	// location tool, or why it cannot. Sent by the dashboard the person is
	// chatting in. Needs agent:use.
	AnswerAgentLocation(ctx context.Context, arguments AnswerAgentLocationArguments) (bool, error)

	// Stop a turn where it is. Needs agent:use.
	StopAgentRun(ctx context.Context, arguments StopAgentRunArguments) (bool, error)

	// React to a message of a conversation with an emoji, as the person:
	// one or two emoji, replacing the person's reaction on it; an empty
	// emoji takes it away. Whoever has the conversation open is told with
	// a "reacted" event. In a chat, to any message there, one reaction for
	// each person. Needs agent:use, or circle:use for a chat.
	ReactToAgentMessage(ctx context.Context, arguments ReactToAgentMessageArguments) (bool, error)

	// Have a running turn push its state (working, waiting, done, failed)
	// to a Live Activity on the person's phone until it ends: the sealed
	// registration the relay made of the activity's push token. Needs
	// agent:use.
	RegisterAgentLiveActivity(ctx context.Context, arguments RegisterAgentLiveActivityArguments) (bool, error)

	// Start a named conversation, kept apart from the main one, with a
	// goal on it if one is given. Needs agent:use.
	StartAgentConversation(ctx context.Context, arguments StartAgentConversationArguments) (*models.AgentConversation, error)

	// Rename a conversation, archive and unarchive it, or set the goal it
	// works toward — an empty goal clears it and stops the turn it was
	// taking. The main conversation is never archived. Needs agent:use.
	UpdateAgentConversation(ctx context.Context, arguments UpdateAgentConversationArguments) (*models.AgentConversation, error)
	DeleteAgentConversation(ctx context.Context, arguments DeleteAgentConversationArguments) (bool, error)

	// Make a conversation the main one — the one the drawer opens to and
	// the agent's runs deliver into — or, with no conversation named, start
	// a fresh main one. The main conversation until now becomes a named
	// one, keeping everything said in it. Needs agent:use.
	SetAgentMainConversation(ctx context.Context, arguments SetAgentMainConversationArguments) (*models.AgentConversation, error)
}

// AgentSubscription follows a turn as it happens.
type AgentSubscription interface {
	// Every event of a run, from the start, then live until it is done.
	// Needs agent:use.
	AgentRunEvents(ctx context.Context, arguments ReadAgentRunArguments) (<-chan *agent.Event, error)

	// Every event of every turn of a conversation, wherever the turn was
	// started — this drawer, a phone, a terminal, a chat app, another
	// instance: the turns in flight are replayed, then it is live until
	// the subscription ends. A turn begins with an "asked" event carrying
	// what was said. A chat's feed carries what the people do in it: said,
	// edited, deleted, reacted, read and typing. Needs agent:use, or
	// circle:use for a chat.
	AgentConversationEvents(ctx context.Context, arguments ReadAgentConversationEventsArguments) (<-chan *agent.Event, error)
}

// ReadAgentConversationEventsArguments name the conversation to follow.
type ReadAgentConversationEventsArguments struct {
	ConversationID string `json:"conversationId"`
}

// ListAgentConversationsArguments say whether archived ones are wanted.
type ListAgentConversationsArguments struct {
	Archived bool `json:"archived" graphapi:"nullable"`

	// Kind narrows the list to one kind: main, named or chat. Empty is all
	// of them.
	Kind string `json:"kind" graphapi:"nullable"`

	// Query finds conversations by words in the title or in what was said;
	// with it, Archived is ignored and every match is listed.
	Query string `json:"query" graphapi:"nullable"`
}

// SetAgentMainConversationArguments name the conversation to make the
// main one; none for a fresh one.
type SetAgentMainConversationArguments struct {
	ConversationID string `json:"conversationId" graphapi:"nullable"`
}

// DeleteAgentConversationArguments name the conversation to delete.
type DeleteAgentConversationArguments struct {
	ConversationID string `json:"conversationId"`
}

// ReadAgentConversationArguments name the conversation and a page of
// messages.
type ReadAgentConversationArguments struct {
	ConversationID string `json:"conversationId" graphapi:"nullable"`
	First          int    `json:"first" graphapi:"nullable"`
	Offset         int    `json:"offset" graphapi:"nullable"`
}

// ReactToAgentMessageArguments name the message and the emoji; an empty
// emoji takes the person's reaction away.
type ReactToAgentMessageArguments struct {
	MessageID string `json:"messageId"`
	Emoji     string `json:"emoji"`
}

// AgentConversationView is a conversation, its messages and its task list.
type AgentConversationView struct {
	Conversation *models.AgentConversation `json:"conversation"`
	Messages     []*models.AgentMessage    `json:"messages"`
	Total        int                       `json:"total"`
	Todos        []*models.AgentTodo       `json:"todos"`
	// ActingAs is the person whose agent this is, when it is not the
	// caller's own: an operator reading it, and speaking into it, does so
	// as that person, and the drawer says so.
	ActingAs string `json:"actingAs,omitempty"`
}

// ReadAgentRunArguments name a run and where to read from.
type ReadAgentRunArguments struct {
	RunID string `json:"runId"`
	After int    `json:"after" graphapi:"nullable"`
	Wait  int    `json:"wait" graphapi:"nullable"`
}

// AgentRunView is what happened in a run, from a point on.
type AgentRunView struct {
	RunID  string         `json:"runId"`
	Events []*agent.Event `json:"events"`
	Done   bool           `json:"done"`
}

// AgentToolView is one tool as the caller sees it.
type AgentToolView struct {
	Name        string `json:"name"`
	Family      string `json:"family"`
	Risk        string `json:"risk"`
	Description string `json:"description"`
	Confirms    bool   `json:"confirms"`
	Core        bool   `json:"core"`

	// Actions are the verbs a tool takes, for the tools that are one thing
	// with several: one line of policy covers all of them, and a page that
	// does not say which is a page that cannot be reasoned about.
	Actions []string `json:"actions"`
}

// AskAgentArguments are one turn.
type AskAgentArguments struct {
	ConversationID string         `json:"conversationId" graphapi:"nullable"`
	Message        string         `json:"message"`
	Viewing        *agent.Viewing `json:"viewing" graphapi:"nullable"`
	Surface        string         `json:"surface" graphapi:"nullable"`

	// InterruptedAnswer is how much of a spoken answer the person heard
	// before they talked over it.
	InterruptedAnswer *agent.InterruptedAnswer `json:"interruptedAnswer" graphapi:"nullable"`

	// ReadOnly leaves out every tool that changes anything: what a
	// read-only profile of the command line asks for.
	ReadOnly bool `json:"readOnly" graphapi:"nullable"`

	// AttachmentIDs are files uploaded for this turn; References the
	// threads the person pointed at.
	AttachmentIDs []string                `json:"attachmentIds" graphapi:"nullable"`
	References    []models.AgentReference `json:"references" graphapi:"nullable"`

	// Effort is how hard the model thinks before it answers: low, medium
	// or high. Empty leaves it to the agent.
	Effort string `json:"effort" graphapi:"nullable"`

	// DrawerID names the open drawer that sent the turn, for the browser a
	// location call asks (agent.AskSettings.DrawerID).
	DrawerID string `json:"drawerId" graphapi:"nullable"`

	// AddressedAgentIDs are, in a chat, the agents the message is for: those
	// the composer's @ chose, and the agent of a message it replies to. An
	// agent's name in the words addresses nothing. Elsewhere it is ignored.
	AddressedAgentIDs []string `json:"addressedAgentIds" graphapi:"nullable"`

	// ShouldAddressOwnAgent addresses the caller's own agent in a chat, for a
	// client that does not know its id, such as the command line's
	// --ask-agent.
	ShouldAddressOwnAgent bool `json:"shouldAddressOwnAgent" graphapi:"nullable"`

	// MentionedUserIDs are, in a chat, the members the message mentions by
	// name, as the composer's @ chose them, and IsAllMentioned says it
	// mentions everybody. A member told only of mentions is told of it.
	// Mentioning a person is not addressing their agent: that is
	// AddressedAgentIDs. Elsewhere both are ignored.
	MentionedUserIDs []string `json:"mentionedUserIds" graphapi:"nullable"`
	IsAllMentioned   bool     `json:"isAllMentioned" graphapi:"nullable"`
}

// AgentTurnView is the run to follow.
type AgentTurnView struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId"`
}

// ResolveAgentConfirmationArguments answer one card.
type ResolveAgentConfirmationArguments struct {
	RunID   string `json:"runId"`
	CallID  string `json:"callId"`
	Approve bool   `json:"approve"`
}

// drawerIdLength bounds the drawer id a turn carries: the dashboard makes
// one of 36 characters.
const drawerIdLength = 64

// AnswerAgentLocationArguments are the browser's answer to one location
// call: where it is, as its geolocation measured it, or ErrorMessage.
type AnswerAgentLocationArguments struct {
	RunID            string   `json:"runId"`
	CallID           string   `json:"callId"`
	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
	AccuracyMeters   *float64 `json:"accuracyMeters" graphapi:"nullable"`
	// MeasuredAt is when the browser measured it, RFC 3339; empty is now.
	MeasuredAt string `json:"measuredAt" graphapi:"nullable"`
	// ErrorMessage says why the browser cannot say: the person did not
	// allow it, or it could not find out.
	ErrorMessage string `json:"errorMessage" graphapi:"nullable"`
}

// StopAgentRunArguments name the run.
type StopAgentRunArguments struct {
	RunID string `json:"runId"`
}

// RegisterAgentLiveActivityArguments name the turn and the activity.
type RegisterAgentLiveActivityArguments struct {
	RunID string `json:"runId"`

	// The registration the relay sealed from the activity's push token
	PushRegistration string `json:"pushRegistration"`
}

// StartAgentConversationArguments may name it; the model does otherwise.
// A goal is started with StartAgentGoal, in a conversation of its own.
type StartAgentConversationArguments struct {
	Title string `json:"title" graphapi:"nullable"`
}

// UpdateAgentConversationArguments rename or archive. A goal is marked
// done or dropped with SetAgentGoalState.
type UpdateAgentConversationArguments struct {
	ConversationID string `json:"conversationId"`
	Title          string `json:"title" graphapi:"nullable"`
	Archived       *bool  `json:"archived" graphapi:"nullable"`
}

// asJSONValues is what a map of variables looks like once it has been through
// JSON: objects as maps, numbers as float64, and nothing that only a Go value
// could be.
func asJSONValues(variables map[string]any) (map[string]any, error) {
	if len(variables) == 0 {
		return variables, nil
	}
	written, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("those arguments cannot be sent: %w", err)
	}
	var asked map[string]any
	if err := json.Unmarshal(written, &asked); err != nil {
		return nil, fmt.Errorf("those arguments cannot be sent: %w", err)
	}
	return asked, nil
}

// agentOperations is the API as the person, for the agent's tools. Every
// call runs in its own transaction with the person's permissions resolved
// afresh, and the audit trail names the person with the agent as the
// actor.
type agentOperations struct {
	graph       *graph
	user        *models.User
	permissions *models.EffectivePermissions

	// permissionLimit, when set, is what every call is held to besides
	// the person's own permissions: work that must never do more than the
	// turn that started it. See NarrowedTo.
	permissionLimit *models.EffectivePermissions

	// impersonator is the operator signed in as the person when the turn
	// was asked for, named beside the agent on every audit row it writes.
	impersonator *models.User
}

// NarrowedTo is these operations held to the limit as well: each call is
// made with the person's permissions as they are at the call, less
// whatever the limit does not allow.
func (self *agentOperations) NarrowedTo(limit *models.EffectivePermissions) agent.Operations {
	narrowed := *self
	narrowed.permissions = self.permissions.Within(limit)
	narrowed.permissionLimit = limit
	if self.permissionLimit != nil {
		narrowed.permissionLimit = self.permissionLimit.Within(limit)
	}
	return &narrowed
}

func (self *agentOperations) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	// As the values a request would have arrived with. A tool calls this
	// with whatever Go values it has to hand -- a slice of structs for a
	// list of rules, say -- and the query engine checks an input object by
	// asserting it is a map, so a struct is "not an object" to it and the
	// whole call is refused. Over HTTP the same variables have been through
	// JSON and are maps already, so this is the one door where it matters:
	// every rule the agent tried to write failed on it, with an error about
	// element numbers that named neither the tool nor the reason.
	variables, err := asJSONValues(variables)
	if err != nil {
		return err
	}
	prepared, rejected := self.graph.prepareGraphRequest(&graphRequest{Query: document, Variables: variables})
	if rejected != nil {
		return errors.New(rejected.Errors[0].Message)
	}
	ctx = api.ContextWithAuthenticatedUsername(ctx, self.user.Username)
	auditPrincipal := db.AuditPrincipal{ActorKind: models.AuditActorAgent, UserID: self.user.ID}
	if self.impersonator != nil {
		auditPrincipal.ImpersonatorUserID = self.impersonator.ID
	}
	ctx = db.ContextWithAuditPrincipal(ctx, auditPrincipal)
	var outcome *graphql.Result
	if operation, err := selectGraphOperation(prepared.AST, prepared.OperationName); err == nil && hasOnlyTransactionFreeMutations(operation) {
		// A mutation that does slow work reads and writes in short
		// transactions of its own, as it does over HTTP; the agent's limits
		// on what it may do are applied there.
		prepared.Context = context.WithValue(ctx, queryExecutionKey{}, &queryExecution{
			Username: self.user.Username, User: self.user, Impersonator: self.impersonator, PermissionLimit: self.permissionLimit,
		})
		if outcome = graphql.Execute(prepared); len(outcome.Errors) > 0 {
			return errors.New(outcome.Errors[0].Message)
		}
	} else if err := self.graph.database.TransactionContext(ctx, func(tx db.Transaction) error {
		ctx := api.ContextWithTransaction(ctx, tx)
		principal, err := self.graph.resolvePrincipal(tx, self.user.Username, self.user)
		if err != nil {
			return err
		}
		if principal == nil {
			return api.ErrNotLoggedIn
		}
		if self.permissionLimit != nil {
			principal.Permissions = principal.Permissions.Within(self.permissionLimit)
		}
		ctx = api.ContextWithPrincipal(ctx, principal)
		prepared.Context = ctx
		outcome = graphql.Execute(prepared)
		if len(outcome.Errors) > 0 {
			// A failed operation must not leave half of itself behind.
			return errors.New(outcome.Errors[0].Message)
		}
		return nil
	}); err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	encoded, err := json.Marshal(outcome.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func (self *agentOperations) Permissions() *models.EffectivePermissions {
	return self.permissions
}

// requireAgentPerson is the caller, their agent and the operations bound to
// them, for the Ask operations.
func (self *graph) requireAgentPerson(ctx context.Context) (*api.Principal, *models.Agent, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, nil, err
	}
	if principal.User == nil {
		return nil, nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, nil, err
	}
	if found == nil || !found.Active() {
		return nil, nil, agent.ErrUnavailable
	}
	return principal, found, nil
}

// mainConversation is the one continuous conversation, made the first
// time it is needed.
func (self *graph) mainConversation(tx db.Transaction, found *models.Agent) (*models.AgentConversation, error) {
	return tx.MainAgentConversation(found.ID, "")
}

// ownConversation is one of the caller's conversations; a run's transcript
// only when reading, never to talk into.
func (self *graph) ownConversation(tx db.Transaction, found *models.Agent, conversationId string, readingRuns bool) (*models.AgentConversation, error) {
	if strings.TrimSpace(conversationId) == "" {
		return self.mainConversation(tx, found)
	}
	conversation, err := tx.GetAgentConversation(conversationId)
	if err != nil {
		return nil, err
	}
	if conversation == nil || conversation.AgentID != found.ID || (conversation.Kind == models.AgentConversationRun && !readingRuns) {
		return nil, api.ErrNotFound
	}
	return conversation, nil
}

// conversationFor is the conversation the caller may read or speak into:
// their own agent's, or -- with agent:act -- any agent's, handed back with
// that agent and its person so the caller acts as them. The agent and
// person are nil for the caller's own.
func (self *graph) conversationFor(tx db.Transaction, principal *api.Principal, found *models.Agent, conversationId string, readingRuns bool) (*models.AgentConversation, *models.Agent, *models.User, error) {
	conversation, err := self.ownConversation(tx, found, conversationId, readingRuns)
	if err == nil || !errors.Is(err, api.ErrNotFound) || !principal.Permissions.Has(models.PermissionAgentAct) {
		return conversation, nil, nil, err
	}
	conversation, err = tx.GetAgentConversation(conversationId)
	if err != nil || conversation == nil {
		return nil, nil, nil, api.ErrNotFound
	}
	other, err := tx.GetAgent(conversation.AgentID)
	if err != nil || other == nil {
		return nil, nil, nil, api.ErrNotFound
	}
	owner, err := tx.GetUser(other.UserID)
	if err != nil || owner == nil {
		return nil, nil, nil, api.ErrNotFound
	}
	return conversation, other, owner, nil
}

// ListAgentRunsArguments bound the listing.
type ListAgentRunsArguments struct {
	First  int `json:"first" graphapi:"nullable"`
	Offset int `json:"offset" graphapi:"nullable"`
	// AgentID, for ListAllAgentRuns, narrows the operator's listing to one
	// person's agent; empty is everybody's.
	AgentID string `json:"agentId" graphapi:"nullable"`
	// JobID narrows the listing to the runs one job made: a dream's, by
	// the job on its record. Kinds narrows it to some kinds of run, and
	// Query to titles carrying the words.
	JobID string   `json:"jobId" graphapi:"nullable"`
	Kinds []string `json:"kinds" graphapi:"nullable"`
	Query string   `json:"query" graphapi:"nullable"`
}

// AgentRunPage is a page of runs and how many there are in all, each with
// what it cost.
type AgentRunPage struct {
	Runs  []*AgentRunSummary `json:"runs"`
	Total int64              `json:"total"`
}

// AgentRunSummary is one run as a list shows it: the conversation's
// fields a list needs, and what every call in it cost added up.
type AgentRunSummary struct {
	ID        string                `json:"id"`
	AgentID   string                `json:"agentId"`
	Kind      string                `json:"kind"`
	Title     string                `json:"title"`
	Summary   string                `json:"summary,omitempty"`
	JobID     string                `json:"jobId,omitempty"`
	JobKind   string                `json:"jobKind,omitempty"`
	SubjectID string                `json:"subjectId,omitempty"`
	Surface   string                `json:"surface,omitempty"`
	LastAt    time.Time             `json:"lastAt"`
	Usage     models.AgentUsageNote `json:"usage"`
}

// runSummaries is the runs with their usage attached.
func runSummaries(tx db.Transaction, runs []*models.AgentConversation) ([]*AgentRunSummary, error) {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	totals, err := tx.SumAgentRunUsage(ids)
	if err != nil {
		return nil, err
	}
	summaries := make([]*AgentRunSummary, 0, len(runs))
	for _, run := range runs {
		summaries = append(summaries, &AgentRunSummary{
			ID: run.ID, AgentID: run.AgentID, Kind: string(run.Kind), Title: run.Title, Summary: run.Summary,
			JobID: run.JobID, JobKind: run.JobKind, SubjectID: run.SubjectID, Surface: run.Surface, LastAt: run.LastAt,
			Usage: totals[run.ID],
		})
	}
	return summaries, nil
}

func (self *graph) ListAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) (*AgentRunPage, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return &AgentRunPage{Runs: []*AgentRunSummary{}}, nil
		}
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := arguments.Offset
	if offset < 0 {
		offset = 0
	}
	filter := &db.AgentRunFilter{JobID: arguments.JobID, Kinds: arguments.Kinds, Query: arguments.Query}
	tx := self.transaction(ctx)
	runs, err := tx.ListAgentRuns(found.ID, filter, &db.Options{Limit: uint64(limit), Offset: uint64(offset)})
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAgentRuns(found.ID, filter)
	if err != nil {
		return nil, err
	}
	summaries, err := runSummaries(tx, runs)
	if err != nil {
		return nil, err
	}
	return &AgentRunPage{Runs: summaries, Total: total}, nil
}

func (self *graph) ListAgentConversations(ctx context.Context, arguments ListAgentConversationsArguments) ([]*models.AgentConversation, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return []*models.AgentConversation{}, nil
		}
		return nil, err
	}
	kind := models.AgentConversationKind(strings.TrimSpace(arguments.Kind))
	switch kind {
	case "", models.AgentConversationMain, models.AgentConversationNamed, models.AgentConversationChat:
	default:
		return nil, fmt.Errorf("%w: the kind is main, named or chat", api.ErrInvalidArguments)
	}
	// What the caller may have: the conversations with their agent while it
	// is on and they may use it, and their chats while they may use circles.
	// A person whose agent is off is listed their chats and nothing else.
	hasAgent := canUseAgentConversations(principal, found)
	hasChats := principal.Permissions.Has(models.PermissionCircleUse)
	tx := self.transaction(ctx)

	chats := []*models.AgentConversation{}
	if hasChats && (kind == "" || kind == models.AgentConversationChat) {
		if err := tx.SyncChatsOf(principal.User.ID); err != nil {
			return nil, err
		}
		if chats, err = tx.ListChatConversations(found.ID); err != nil {
			return nil, err
		}
		if err := describeChatConversations(tx, principal.User.ID, chats); err != nil {
			return nil, err
		}
	}
	if query := strings.TrimSpace(arguments.Query); query != "" {
		matched := []*models.AgentConversation{}
		if hasAgent && (kind == "" || kind != models.AgentConversationChat) {
			if matched, err = tx.SearchAgentConversations(found.ID, query, 50, principal.IsPersonsTap()); err != nil {
				return nil, err
			}
			if kind != "" {
				matched = slices.DeleteFunc(matched, func(conversation *models.AgentConversation) bool { return conversation.Kind != kind })
			}
		}
		for _, chat := range chats {
			if chat.HasChatWords(query) {
				matched = append(matched, chat)
			}
		}
		sortByLastMessage(matched)
		return matched, nil
	}
	conversations := []*models.AgentConversation{}
	if hasAgent && (kind == "" || kind == models.AgentConversationMain) {
		main, err := self.mainConversation(tx, found)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, main)
	}
	rest := []*models.AgentConversation{}
	if hasAgent && (kind == "" || kind == models.AgentConversationNamed) {
		named, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationNamed}, &db.Options{Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, conversation := range named {
			if (conversation.ArchivedAt != nil) == arguments.Archived {
				rest = append(rest, conversation)
			}
		}
	}
	// A chat put away is listed with the archived, like a named conversation,
	// and a chat that is not is listed with the live ones.
	for _, chat := range chats {
		if (chat.ArchivedAt != nil) == arguments.Archived {
			rest = append(rest, chat)
		}
	}
	sortByLastMessage(rest)
	return append(conversations, rest...), nil
}

// canUseAgentConversations says whether the caller has conversations with
// their agent: they may use it and it is on. A person who can only chat has
// their chats and nothing else.
func canUseAgentConversations(principal *api.Principal, found *models.Agent) bool {
	return principal.Permissions.Has(models.PermissionAgentUse) && found.Active()
}

// sortByLastMessage puts the conversation with the newest message first.
func sortByLastMessage(conversations []*models.AgentConversation) {
	sort.SliceStable(conversations, func(left, right int) bool {
		return conversations[left].LastAt.After(conversations[right].LastAt)
	})
}

func (self *graph) ReadAgentConversation(ctx context.Context, arguments ReadAgentConversationArguments) (*AgentConversationView, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	// The main conversation is made on demand, which is for an agent that is
	// on; a chat is named by its id.
	if strings.TrimSpace(arguments.ConversationID) == "" {
		if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationMain}); err != nil {
			return nil, err
		}
	}
	conversation, other, owner, err := self.conversationFor(tx, principal, found, arguments.ConversationID, true)
	if err != nil {
		return nil, err
	}
	// A chat is between its people: an operator with agent:act reads none of
	// them but their own.
	if conversation.IsChat() && other != nil {
		return nil, api.ErrNotFound
	}
	if err := requireConversationUse(principal, found, conversation); err != nil {
		return nil, err
	}
	actingAs := ""
	if other != nil {
		actingAs = owner.Username
		log.Noticef("%s read a conversation of %s's agent", operatorName(ctx), owner.Username)
	}
	// The newest page by default: a drawer opens on the end of a
	// conversation, not its beginning.
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := max(arguments.Offset, 0)
	if conversation.IsChat() {
		return self.readChatConversation(ctx, principal.User, conversation, limit, offset)
	}
	messages, err := tx.ListAgentMessages(conversation.ID, nil)
	if err != nil {
		return nil, err
	}
	total := len(messages)
	start := len(messages) - limit - offset
	end := len(messages) - offset
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	todos, err := tx.ListAgentTodos(conversation.ID)
	if err != nil {
		return nil, err
	}
	page := messages[start:end]
	speaker, speakingAgent := principal.User, found
	if other != nil {
		speaker, speakingAgent = owner, other
	}
	describeOwnMessages(page, speaker, speakingAgent)
	return &AgentConversationView{Conversation: conversation, Messages: page, Total: total, Todos: todos, ActingAs: actingAs}, nil
}

// describeOwnMessages gives the messages of a conversation with the agent the
// author a chat's have, so a client draws every conversation one way: what the
// person said is theirs and the rest is their agent's, and all of it is the
// reader's.
func describeOwnMessages(messages []*models.AgentMessage, owner *models.User, speakingAgent *models.Agent) {
	for _, message := range messages {
		message.IsMine = true
		message.AuthorAvatarColorIndex = avatar.InitialColorIndex(owner.ID)
		if message.Role == "user" {
			message.AuthorKind = models.ChatAuthorPerson
			message.AuthorUserID = owner.ID
			message.AuthorName = owner.DisplayName()
			message.AuthorAvatarURL = avatarUrl(owner)
		} else {
			message.AuthorKind = models.ChatAuthorAgent
			message.AuthorAgentID = speakingAgent.ID
			message.AuthorName = speakingAgent.DisplayName()
		}
		for index := range message.Reactions {
			if message.Reactions[index].ReactorKind == models.ReactorAgent {
				message.Reactions[index].ReactorName = speakingAgent.DisplayName()
			} else {
				message.Reactions[index].ReactorUserID = owner.ID
				message.Reactions[index].ReactorName = owner.DisplayName()
			}
		}
	}
}

// readChatConversation is a page of a chat as one member reads it.
func (self *graph) readChatConversation(ctx context.Context, user *models.User, conversation *models.AgentConversation, limit, offset int) (*AgentConversationView, error) {
	tx := self.transaction(ctx)
	messages, total, err := tx.ListChatConversationMessages(conversation, limit, offset)
	if err != nil {
		return nil, err
	}
	authorIds := []string{}
	for _, message := range messages {
		if message.AuthorUserID != "" {
			authorIds = append(authorIds, message.AuthorUserID)
		}
	}
	users, err := tx.GetUsersByID(authorIds)
	if err != nil {
		return nil, err
	}
	describeChatAuthors(messages, users, user.ID)
	// A message of the member's own agent in the conversation is drawn as in
	// any other, and as the reader's.
	own := []*models.AgentMessage{}
	for _, message := range messages {
		if message.AuthorKind == "" {
			own = append(own, message)
		}
	}
	if len(own) > 0 {
		found, err := tx.GetAgent(conversation.AgentID)
		if err != nil {
			return nil, err
		}
		describeOwnMessages(own, user, found)
		for _, message := range own {
			message.IsAgentWork = true
		}
	}
	view, err := self.chatConversationOf(ctx, user.ID, conversation)
	if err != nil {
		return nil, err
	}
	return &AgentConversationView{Conversation: view, Messages: messages, Total: total, Todos: []*models.AgentTodo{}}, nil
}

func (self *graph) ListAllAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) (*AgentRunPage, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAct); err != nil {
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := max(arguments.Offset, 0)
	filter := &db.AgentRunFilter{JobID: arguments.JobID, Kinds: arguments.Kinds, Query: arguments.Query}
	tx := self.transaction(ctx)
	runs, err := tx.ListAgentRuns(arguments.AgentID, filter, &db.Options{Limit: uint64(limit), Offset: uint64(offset)})
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAgentRuns(arguments.AgentID, filter)
	if err != nil {
		return nil, err
	}
	summaries, err := runSummaries(tx, runs)
	if err != nil {
		return nil, err
	}
	return &AgentRunPage{Runs: summaries, Total: total}, nil
}

func (self *graph) ReadAgentRun(ctx context.Context, arguments ReadAgentRunArguments) (*AgentRunView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if self.settings.Agent == nil {
		return nil, agent.ErrUnavailable
	}
	run := self.agentWorker().FindRun(arguments.RunID)
	if run == nil || run.Conversation().AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	events, unsubscribe := run.Subscribe()
	defer unsubscribe()
	wait := time.Duration(arguments.Wait) * time.Second
	if wait > 25*time.Second {
		wait = 25 * time.Second
	}
	deadline := time.After(wait)
	view := &AgentRunView{RunID: run.ID, Events: []*agent.Event{}}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				view.Done = true
				return view, nil
			}
			if event.Sequence < arguments.After {
				continue
			}
			copied := event
			view.Events = append(view.Events, &copied)
			if event.Kind == agent.EventDone {
				view.Done = true
				return view, nil
			}
			// Drain what is already there without waiting.
			wait = 0
			deadline = time.After(0)
		case <-deadline:
			if len(view.Events) > 0 || wait == 0 {
				return view, nil
			}
		case <-ctx.Done():
			return view, nil
		}
	}
}

func (self *graph) ListAgentTools(ctx context.Context) ([]*AgentToolView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return []*AgentToolView{}, nil
	}
	configuration := self.config.Current()
	tools := worker.Catalog().Offered(principal.Permissions, &configuration.Agent.Tools, &configuration.Services)
	// The skills an operator installed bring tools that are not compiled
	// in, and an operator who cannot see them here cannot tell what a
	// skill added or write a policy about it.
	tools = append(tools, worker.SkillTools(ctx)...)
	views := make([]*AgentToolView, 0, len(tools))
	for _, tool := range tools {
		// The lead sentence, not the whole description: a merged tool
		// describes every one of its actions, which is right in a model's
		// request and is a wall of text in a list of policies. The actions
		// are said beside it instead.
		views = append(views, &AgentToolView{
			Name: tool.Name, Family: string(tool.Family), Risk: string(tool.Risk),
			Description: leadSentence(tool.Description),
			Confirms:    agent.NeedsConfirmation(tool, nil, &configuration.Agent.Tools, found),
			Core:        tool.Core, Actions: agent.ActionsOf(tool),
		})
	}
	return views, nil
}

// leadSentence is the first line of a description, which for a merged tool is
// what it is rather than what each of its actions does.
func leadSentence(description string) string {
	if index := strings.Index(description, "\n"); index > 0 {
		return strings.TrimSpace(description[:index])
	}
	return strings.TrimSpace(description)
}

func (self *graph) AskAgent(ctx context.Context, arguments AskAgentArguments) (*AgentTurnView, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	// A turn in the main conversation is for an agent that is on; a chat is
	// named by its id.
	if strings.TrimSpace(arguments.ConversationID) == "" {
		if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationMain}); err != nil {
			return nil, err
		}
	}
	// A chat is between its people and starts no turn: what the caller says
	// goes to them. An operator with agent:act speaks into none but their own.
	if chat, err := tx.GetAgentConversation(strings.TrimSpace(arguments.ConversationID)); err != nil {
		return nil, err
	} else if chat != nil && chat.IsChat() {
		if chat.AgentID != found.ID {
			return nil, api.ErrNotFound
		}
		if err := requireConversationUse(principal, found, chat); err != nil {
			return nil, err
		}
		session, err := chatSessionOf(tx, principal.User, found, chat)
		if err != nil {
			return nil, err
		}
		return self.sayInChat(ctx, session, arguments)
	}
	if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationNamed}); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	// A run's transcript can be talked into: the person reading what the
	// agent did on its own — sorted a message, wrote a reply — asks about
	// it right there, with the message and the decision as the history.
	//
	// An operator with agent:act may speak into another person's
	// conversation, and does so as that person: their agent, their
	// permissions, their tools. It is said in the log every time.
	conversation, other, owner, err := self.conversationFor(tx, principal, found, arguments.ConversationID, true)
	if err != nil {
		return nil, err
	}
	asking, person := found, principal.User
	var operations agent.Operations = &agentOperations{graph: self, user: principal.User, permissions: principal.Permissions, impersonator: principal.Impersonator}
	if other != nil {
		asking, person = other, owner
		if operations, err = worker.OperationsFor(ctx, owner); err != nil {
			return nil, err
		}
		log.Noticef("%s spoke to %s's agent as them", operatorName(ctx), owner.Username)
	}
	surface := strings.TrimSpace(arguments.Surface)
	if surface == "" {
		surface = "drawer"
	}
	drawerId := strings.TrimSpace(arguments.DrawerID)
	if len(drawerId) > drawerIdLength {
		return nil, fmt.Errorf("%w: a drawer id is at most %d characters", api.ErrInvalidArguments, drawerIdLength)
	}
	if err := checkEffort(arguments.Effort); err != nil {
		return nil, err
	}
	// The files, which must be this agent's own and not yet another
	// turn's.
	attachments, err := tx.GetAgentAttachments(arguments.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	if len(attachments) != len(arguments.AttachmentIDs) {
		return nil, fmt.Errorf("%w: an attachment is missing", api.ErrInvalidArguments)
	}
	for _, attachment := range attachments {
		if attachment.AgentID != asking.ID || (attachment.MessageID != "" && attachment.ConversationID != conversation.ID) {
			return nil, api.ErrNotFound
		}
	}
	// A finance transaction pointed at must be this agent's own, refused
	// like a file that is not. The agent reads it again when it keeps the
	// turn, and drops one that is not its own, for every other caller.
	for _, reference := range arguments.References {
		if reference.FinanceTransactionID == "" {
			continue
		}
		financeTransaction, err := tx.GetFinanceTransaction(asking.ID, reference.FinanceTransactionID)
		if err != nil {
			return nil, err
		}
		if financeTransaction == nil {
			return nil, api.ErrNotFound
		}
	}
	// A recording pointed at must be the person's own and not deleted,
	// refused the same way.
	for _, reference := range arguments.References {
		if reference.RecordingID == "" {
			continue
		}
		if _, err := tx.GetRecording(asking.UserID, reference.RecordingID); err != nil {
			if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
				return nil, api.ErrNotFound
			}
			return nil, err
		}
	}
	// A photo pointed at must be one the person may read and not deleted,
	// refused the same way.
	for _, reference := range arguments.References {
		if reference.PhotoID == "" {
			continue
		}
		if _, err := tx.GetPhotoAccess(asking.UserID, reference.PhotoID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, api.ErrNotFound
			}
			return nil, err
		}
	}
	// A message replied to must be one of this conversation's, refused
	// the same way. The agent reads it again when it keeps the turn.
	for _, reference := range arguments.References {
		if strings.TrimSpace(reference.AgentMessageID) == "" {
			continue
		}
		replied, err := tx.GetAgentConversationMessage(conversation.ID, strings.TrimSpace(reference.AgentMessageID))
		if err != nil {
			return nil, err
		}
		if replied == nil {
			return nil, api.ErrNotFound
		}
	}
	// Where the dashboard was reached, for a link to a file the person asks
	// to open on another device.
	origin := ""
	if request := api.ContextRequest(ctx); request != nil {
		origin = self.originOf(request)
	}
	run, err := worker.Ask(&agent.AskSettings{
		Agent:        asking,
		Owner:        person,
		Operations:   operations,
		Conversation: conversation,
		Message:      arguments.Message,
		Viewing:      arguments.Viewing,
		Surface:      surface,
		DrawerID:     drawerId,

		// The surface is only what the client says. Whether the person
		// tapped is judged from how the request signed in, and an operator
		// who speaks into another person's conversation, or is signed in as
		// them, is never the person (agent.AskRun.IsStartedByPerson).
		IsPersonsTap:             principal.IsPersonsTap() && other == nil,
		IsOperatorActingAsPerson: other != nil || principal.IsImpersonation(),

		InterruptedAnswer: arguments.InterruptedAnswer,
		ReadOnly:          arguments.ReadOnly,
		Attachments:       attachments,
		References:        arguments.References,
		Effort:            arguments.Effort,
		Origin:            origin,
	})
	if err != nil {
		return nil, translateError(err)
	}
	return &AgentTurnView{RunID: run.ID, ConversationID: conversation.ID}, nil
}

func (self *graph) ResolveAgentConfirmation(ctx context.Context, arguments ResolveAgentConfirmationArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	log.Noticef("%s %s what their agent asked to do", operatorName(ctx), map[bool]string{true: "approved", false: "declined"}[arguments.Approve])
	answer := models.InteractionDeclined
	if arguments.Approve {
		answer = models.InteractionApproved
	}
	return self.answerInteraction(ctx, found, worker, arguments.CallID, answer, agent.RunCommand{RunID: arguments.RunID, Action: agent.CommandResolve, CallID: arguments.CallID, Approve: arguments.Approve})
}

// commandAgentRun gives a run the person's word: applied here when the
// run is here; forwarded to the instance running it when the run is one
// this instance has heard of through the feed. Either way the run must
// be this agent's.
func (self *graph) commandAgentRun(ctx context.Context, found *models.Agent, worker *agent.Agent, command agent.RunCommand) (bool, error) {
	if run := worker.FindRun(command.RunID); run != nil {
		if run.Conversation().AgentID != found.ID {
			return false, api.ErrNotFound
		}
		return worker.Apply(run, command), nil
	}
	conversationId, ok := worker.ForeignRun(command.RunID)
	if !ok {
		return false, api.ErrNotFound
	}
	conversation, err := self.transaction(ctx).GetAgentConversation(conversationId)
	if err != nil {
		return false, err
	}
	if conversation == nil || conversation.AgentID != found.ID {
		return false, api.ErrNotFound
	}
	if err := worker.Forward(command); err != nil {
		return false, err
	}
	return true, nil
}

func (self *graph) AnswerAgentLocation(ctx context.Context, arguments AnswerAgentLocationArguments) (bool, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	// Where it is, or why it cannot say: an answer with neither would be
	// read as a place at latitude and longitude zero.
	answer := agent.LocationAnswer{MeasuredAt: time.Now(), ErrorMessage: strings.TrimSpace(arguments.ErrorMessage)}
	switch {
	case arguments.LatitudeDegrees != nil && arguments.LongitudeDegrees != nil:
		answer.LatitudeDegrees, answer.LongitudeDegrees = *arguments.LatitudeDegrees, *arguments.LongitudeDegrees
		if arguments.AccuracyMeters != nil {
			answer.AccuracyMeters = *arguments.AccuracyMeters
		}
	case answer.ErrorMessage == "":
		return false, fmt.Errorf("%w: give latitudeDegrees and longitudeDegrees, or errorMessage", api.ErrInvalidArguments)
	}
	if measuredAt := strings.TrimSpace(arguments.MeasuredAt); measuredAt != "" {
		if answer.MeasuredAt, err = time.Parse(time.RFC3339, measuredAt); err != nil {
			return false, fmt.Errorf("%w: measuredAt is an RFC 3339 time", api.ErrInvalidArguments)
		}
	}
	said, err := json.Marshal(answer)
	if err != nil {
		return false, err
	}
	isAnswered, err := self.commandAgentRun(ctx, found, worker, agent.RunCommand{RunID: arguments.RunID, Action: agent.CommandLocate, CallID: arguments.CallID, Answer: string(said)})
	if err == nil && isAnswered && answer.ErrorMessage == "" {
		self.keepBrowserLocation(ctx, principal, &answer)
	}
	return isAnswered, err
}

// keepBrowserLocation stores where the browser said it was as a position of
// the person's, so that the Location page and the agent's history count it
// too, when they may record where they go. Best effort: the person asked
// where they are and has been answered, so a failure here is logged and goes
// no further. It is its own transaction for the same reason, since a failed
// statement would spoil the one the answer is in.
func (self *graph) keepBrowserLocation(ctx context.Context, principal *api.Principal, answer *agent.LocationAnswer) {
	if !principal.Permissions.Has(models.PermissionLocationUse) {
		return
	}
	newPointCount := 0
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Behind the person's location lock, like a phone's report, so a
		// pause that began while it waited is in effect for it.
		if err := tx.LockLocation(principal.User.ID); err != nil {
			return err
		}
		now := time.Now()
		measuredAt := answer.MeasuredAt
		if measuredAt.IsZero() || measuredAt.After(now) {
			// A browser's clock a little ahead of this one's is the same moment.
			measuredAt = now
		}
		// Not kept while the person has paused, like a phone's position.
		pauses, err := readLocationPauseSet(tx, principal.User.ID, measuredAt, measuredAt, now)
		if err != nil {
			return err
		}
		kept, _ := pauses.keptPoints([]*models.LocationPoint{{
			LatitudeDegrees: answer.LatitudeDegrees, LongitudeDegrees: answer.LongitudeDegrees, AccuracyMeters: answer.AccuracyMeters,
			MeasuredAt: measuredAt, LocationSource: models.LocationSourceBrowser,
		}})
		if newPointCount, err = tx.InsertLocationPoints(principal.User.ID, kept); err != nil || newPointCount == 0 {
			return err
		}
		// The trip it falls in, if any, takes it in, as after a phone's report.
		return rebuild.Around(tx, principal.User.ID, measuredAt, measuredAt)
	}); err != nil {
		log.Warningf("cannot keep the location the browser gave: %s", err)
		return
	}
	// Told once the position is stored, as a phone's report is, so that the
	// maps of the people it is shared with move now and not at their next read.
	if newPointCount > 0 {
		if err := self.database.NotifyLocationEvent(principal.User.ID); err != nil {
			log.Warningf("cannot tell the listeners that a location changed: %s", err)
		}
	}
}

func (self *graph) StopAgentRun(ctx context.Context, arguments StopAgentRunArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	return self.commandAgentRun(ctx, found, worker, agent.RunCommand{RunID: arguments.RunID, Action: agent.CommandStop})
}

// ReactToAgentMessage is the person's reaction. The message is found by
// its id and then held to the same rule as reading its conversation: the
// caller's own, or another person's for an operator with agent:act, who
// reacts as that person and is logged doing so. A message of anybody else
// is not found, as their conversation is not.
func (self *graph) ReactToAgentMessage(ctx context.Context, arguments ReactToAgentMessageArguments) (bool, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return false, err
	}
	reactionEmoji := strings.TrimSpace(arguments.Emoji)
	if reactionEmoji != "" {
		if err := models.ValidateReactionEmoji(reactionEmoji); err != nil {
			return false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
	}
	tx := self.transaction(ctx)
	message, err := tx.GetAgentMessage(strings.TrimSpace(arguments.MessageID))
	if err != nil {
		return false, err
	}
	if message == nil {
		return self.reactToChatMessage(ctx, principal, found, strings.TrimSpace(arguments.MessageID), reactionEmoji)
	}
	conversation, other, owner, err := self.conversationFor(tx, principal, found, message.ConversationID, true)
	if err != nil {
		return false, err
	}
	// A chat is between its people: an operator who may act as somebody does
	// not read it, ask in it or react in it as them.
	if conversation.IsChat() && other != nil {
		return false, api.ErrNotFound
	}
	if err := requireConversationUse(principal, found, conversation); err != nil {
		return false, err
	}
	// Taking a reaction away is always allowed; putting one on is only for
	// what the agent said.
	if reactionEmoji != "" && !message.CanBeReactedToBy(models.ReactorPerson) {
		return false, fmt.Errorf("%w: you can react to what the agent said, not to your own messages", api.ErrInvalidArguments)
	}
	reacting := found
	if other != nil {
		reacting = other
		log.Noticef("%s reacted to a message of %s's agent as them", operatorName(ctx), owner.Username)
	}
	isChanged, err := tx.SetAgentMessageReaction(reacting.ID, conversation.ID, message.ID, models.ReactorPerson, reactionEmoji)
	if err != nil {
		return false, translateError(err)
	}
	if worker := self.agentWorker(); worker != nil && isChanged {
		tx.AfterCommit(func() { worker.PublishReaction(conversation.ID, message.ID, reactionEmoji, models.ReactorPerson) })
	}
	return true, nil
}

// reactToChatMessage is a reaction on a message of a chat the caller is in:
// one reaction for each person, on anything said while they were there, and
// told to everybody in the chat. A message that is not a chat's is not found.
func (self *graph) reactToChatMessage(ctx context.Context, principal *api.Principal, found *models.Agent, messageId, reactionEmoji string) (bool, error) {
	if !principal.Permissions.Has(models.PermissionCircleUse) {
		return false, api.ErrNotFound
	}
	tx := self.transaction(ctx)
	message, err := tx.GetChatMessage(messageId)
	if err != nil {
		return false, err
	}
	if message == nil {
		return false, api.ErrNotFound
	}
	chat, err := tx.GetChat(message.ChatID)
	if err != nil {
		return false, err
	}
	var member *models.ChatMember
	if chat != nil {
		member = chat.Member(principal.User.ID)
	}
	if member == nil || !isReadableByMember(message, member) {
		return false, api.ErrNotFound
	}
	// A reaction is said to the others like a message: not by someone who
	// left, nor between two who no longer share a circle.
	if err := self.requireChatWritable(tx, &chatSession{User: principal.User, Agent: found, Chat: chat, Member: member}); err != nil {
		return false, err
	}
	reactorName := principal.User.DisplayName()
	isChanged, err := tx.SetChatMessageReaction(message.ID, models.ReactorPerson, principal.User.ID, reactorName, reactionEmoji)
	if err != nil {
		return false, translateError(err)
	}
	if isChanged {
		self.publishChatMessageEvents(ctx, message, func(*models.ChatMember) agent.Event {
			return agent.Event{
				Kind: agent.EventReacted, MessageID: message.ID, Emoji: reactionEmoji, ReactorKind: string(models.ReactorPerson),
				ReactorUserID: principal.User.ID, ReactorName: reactorName,
			}
		})
	}
	return true, nil
}

func (self *graph) RegisterAgentLiveActivity(ctx context.Context, arguments RegisterAgentLiveActivityArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	pushRegistration := strings.TrimSpace(arguments.PushRegistration)
	if pushRegistration == "" {
		return false, fmt.Errorf("%w: a Live Activity needs its registration", api.ErrInvalidArguments)
	}
	if run := worker.FindRun(arguments.RunID); run != nil {
		if run.Conversation().AgentID != found.ID {
			return false, api.ErrNotFound
		}
		if err := run.RegisterLiveActivity(pushRegistration); err != nil {
			return false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		return true, nil
	}
	// The turn runs on another instance, which follows the activity when
	// the word reaches it; a refusal there is not heard here.
	return self.commandAgentRun(ctx, found, worker, agent.RunCommand{RunID: arguments.RunID, Action: agent.CommandLiveActivity, PushRegistration: pushRegistration})
}

func (self *graph) StartAgentConversation(ctx context.Context, arguments StartAgentConversationArguments) (*models.AgentConversation, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	starting := &models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationNamed, Title: strings.TrimSpace(arguments.Title), LastAt: time.Now()}
	conversation, err := tx.CreateAgentConversation(starting)
	if err != nil {
		return nil, translateError(err)
	}
	return conversation, nil
}

// DeleteAgentConversation removes a named conversation with everything
// in it — the messages and the files that came with them. The main
// conversation is never deleted; it is compacted instead.
func (self *graph) DeleteAgentConversation(ctx context.Context, arguments DeleteAgentConversationArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, true)
	if err != nil {
		return false, err
	}
	if conversation.Kind == models.AgentConversationMain {
		return false, fmt.Errorf("%w: the main conversation is never deleted", api.ErrInvalidArguments)
	}
	if conversation.IsChat() {
		return false, fmt.Errorf("%w: a chat is left, not deleted", api.ErrInvalidArguments)
	}
	if worker := self.agentWorker(); worker != nil {
		worker.StopConversation(conversation.ID)
	}
	// A photo a receipt was read from stays with the receipt, which would
	// go with it otherwise; deleting the receipt deletes it.
	if err := tx.DetachReceiptAttachments(found.ID, conversation.ID); err != nil {
		return false, err
	}
	attachments, err := tx.ListAgentAttachments(found.ID, conversation.ID)
	if err != nil {
		return false, err
	}
	for _, attachment := range attachments {
		if err := tx.DeleteAgentAttachment(attachment.ID); err != nil {
			return false, err
		}
		if err := self.storage.DeleteFile(ctx, attachment.ID); err != nil {
			log.Warningf("cannot remove the bytes of attachment %q: %s", attachment.ID, err)
		}
	}
	if err := tx.DeleteAgentConversation(conversation.ID); err != nil {
		return false, err
	}
	log.Noticef("%s deleted a conversation with their agent", operatorName(ctx))
	return true, nil
}

func (self *graph) SetAgentMainConversation(ctx context.Context, arguments SetAgentMainConversationArguments) (*models.AgentConversation, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	previous, err := self.mainConversation(tx, found)
	if err != nil {
		return nil, err
	}
	var chosen *models.AgentConversation
	if id := strings.TrimSpace(arguments.ConversationID); id != "" {
		chosen, err = self.ownConversation(tx, found, id, false)
		if err != nil {
			return nil, err
		}
		if chosen.ID == previous.ID {
			return previous, nil
		}
		if chosen.Kind != models.AgentConversationNamed {
			return nil, fmt.Errorf("%w: only a named conversation can become the main one", api.ErrInvalidArguments)
		}
	}
	// The old main becomes a named conversation. Untitled, so the describer
	// names it from what was said; never archived, so it stays in the list.
	if _, err := tx.UpdateAgentConversation(previous.ID, func(conversation *models.AgentConversation) error {
		conversation.Kind = models.AgentConversationNamed
		// Described as the main one, which takes no title; due again so
		// that it gets one.
		conversation.DescribedAt = nil
		return nil
	}); err != nil {
		return nil, translateError(err)
	}
	if chosen == nil {
		return tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
	}
	updated, err := tx.UpdateAgentConversation(chosen.ID, func(conversation *models.AgentConversation) error {
		conversation.Kind = models.AgentConversationMain
		conversation.ArchivedAt = nil
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

func (self *graph) UpdateAgentConversation(ctx context.Context, arguments UpdateAgentConversationArguments) (*models.AgentConversation, error) {
	principal, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	// Asked before the main conversation is looked up, which makes it: with
	// the agent off there is none to change.
	if strings.TrimSpace(arguments.ConversationID) == "" {
		if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationMain}); err != nil {
			return nil, err
		}
	}
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, false)
	if err != nil {
		return nil, err
	}
	// A chat is put away like any other conversation, by anyone who may use
	// circles whether or not their agent is on. It is their own row of it, so
	// it leaves nobody else's list; its name is the people's, and has its own
	// operation.
	if err := requireConversationUse(principal, found, conversation); err != nil {
		return nil, err
	}
	if conversation.IsChat() && strings.TrimSpace(arguments.Title) != "" {
		return nil, fmt.Errorf("%w: a chat is renamed with RenameChat", api.ErrInvalidArguments)
	}
	updated, err := tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
		if title := strings.TrimSpace(arguments.Title); title != "" {
			// Named by the person: the model stops renaming it.
			conversation.Title = title
			conversation.TitledBy = "person"
		}
		if arguments.Archived != nil {
			if conversation.Kind == models.AgentConversationMain {
				return fmt.Errorf("%w: the main conversation is never archived", api.ErrInvalidArguments)
			}
			if *arguments.Archived {
				now := time.Now()
				conversation.ArchivedAt = &now
			} else {
				conversation.ArchivedAt = nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	if updated.IsChat() {
		return self.chatConversationOf(ctx, principal.User.ID, updated)
	}
	return updated, nil
}

// AgentRunEvents follows a run over the websocket.
func (self *graph) AgentRunEvents(ctx context.Context, arguments ReadAgentRunArguments) (<-chan *agent.Event, error) {
	// The socket resolves the principal before starting the stream. This
	// lookup has its own short transaction; the stream holds no SQL connection.
	var found *models.Agent
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		_, found, err = self.requireAgentPerson(api.ContextWithTransaction(ctx, tx))
		return err
	}); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	run := worker.FindRun(arguments.RunID)
	if run == nil || run.Conversation().AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	events, unsubscribe := run.Subscribe()
	channel := make(chan *agent.Event)
	go func() {
		defer close(channel)
		defer unsubscribe()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Sequence < arguments.After {
					continue
				}
				copied := event
				select {
				case channel <- &copied:
				case <-ctx.Done():
					return
				}
				if event.Kind == agent.EventDone {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return channel, nil
}

func (self *graph) AgentConversationEvents(ctx context.Context, arguments ReadAgentConversationEventsArguments) (<-chan *agent.Event, error) {
	// The lookup needs a transaction of its own, as AgentRunEvents does.
	var conversation *models.AgentConversation
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		principal, found, err := self.requireConversationPerson(api.ContextWithTransaction(ctx, tx))
		if err != nil {
			return err
		}
		if strings.TrimSpace(arguments.ConversationID) == "" {
			if err := requireConversationUse(principal, found, &models.AgentConversation{Kind: models.AgentConversationMain}); err != nil {
				return err
			}
		}
		var other *models.Agent
		conversation, other, _, err = self.conversationFor(tx, principal, found, arguments.ConversationID, true)
		if err != nil {
			return err
		}
		// Nobody follows a chat but its people.
		if conversation.IsChat() && other != nil {
			return api.ErrNotFound
		}
		return requireConversationUse(principal, found, conversation)
	}); err != nil {
		return nil, err
	}
	workerEvents, hubEvents, unsubscribe, err := self.followConversation(conversation)
	if err != nil {
		return nil, err
	}
	channel := make(chan *agent.Event)
	go func() {
		defer close(channel)
		defer unsubscribe()
		// An event of the worker's feed replayed and then published again is
		// told by its sequence: that feed delivers each of a run's events
		// once. One of no run -- the conversation's title, written after its
		// first turn ended -- is never replayed, and is delivered as it comes.
		//
		// The chat hub's events are not held to it. A chat turn tells the
		// room its words and that it is thinking with the run's identifier on
		// every one and no sequence, so counting them would drop every one
		// after the first: the words stopped streaming and the typing line
		// came once.
		delivered := map[string]int{}
		send := func(event agent.Event) bool {
			copied := event
			select {
			case channel <- &copied:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for workerEvents != nil || hubEvents != nil {
			select {
			case event, ok := <-workerEvents:
				if !ok {
					workerEvents = nil
					continue
				}
				if event.RunID != "" {
					if last, seen := delivered[event.RunID]; seen && event.Sequence <= last {
						continue
					}
					delivered[event.RunID] = event.Sequence
				}
				if !send(event) {
					return
				}
			case event, ok := <-hubEvents:
				if !ok {
					hubEvents = nil
					continue
				}
				if !send(event) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return channel, nil
}

// followConversation is the feeds of a conversation and the function that ends
// them. A chat's events are the chat hub's, which exists whether or not the
// server has an agent: its people do not need one to hear each other. The
// agent's own turns in a chat conversation are on the worker's feed, followed
// as well when there is a worker. Any other conversation is the worker's
// alone, and refused as unavailable without one. The two are returned apart
// because only the worker's feed replays events (see AgentConversationEvents);
// a feed that is not followed is nil.
func (self *graph) followConversation(conversation *models.AgentConversation) (workerEvents, hubEvents <-chan agent.Event, unsubscribe func(), err error) {
	worker := self.agentWorker()
	if !conversation.IsChat() {
		if worker == nil {
			return nil, nil, nil, agent.ErrUnavailable
		}
		events, unsubscribeWorker := worker.SubscribeConversation(conversation.ID)
		return events, nil, unsubscribeWorker, nil
	}
	hub := self.chatHub()
	if hub == nil {
		return nil, nil, nil, agent.ErrUnavailable
	}
	hubEvents, unsubscribeHub := hub.Subscribe(conversation.ID)
	if worker == nil {
		return nil, hubEvents, unsubscribeHub, nil
	}
	workerEvents, unsubscribeWorker := worker.SubscribeConversation(conversation.ID)
	return workerEvents, hubEvents, func() {
		unsubscribeHub()
		unsubscribeWorker()
	}, nil
}

// agentWorker is the worker as the API reaches it, or nil when agents are
// off.
func (self *graph) agentWorker() *agent.Agent {
	if self.settings == nil || self.settings.Agent == nil {
		return nil
	}
	worker, ok := self.settings.Agent.(*agent.Agent)
	if !ok {
		return nil
	}
	return worker
}
