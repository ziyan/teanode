package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Ask: the person talking to their agent. A turn is started here, runs in
// the agent package, and streams back over the websocket as a
// subscription; a confirmation card is answered here; a conversation is
// listed, started, renamed and archived here. What the agent does for the
// person goes through the same API the person uses, executed as them by
// agentOperations, so the agent can do exactly what they can do.

// AgentAskQuery is the conversations and their turns.
type AgentAskQuery interface {
	// The caller's conversations with their agent: the main one first, then
	// the named ones by last use; archived ones only when asked. Needs
	// agent:use.
	ListAgentConversations(ctx context.Context, arguments ListAgentConversationsArguments) ([]*models.AgentConversation, error)

	// One conversation and its messages, oldest first; the main
	// conversation when no id is given (made if it does not exist yet).
	// Needs agent:use.
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
	ListAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) ([]*models.AgentConversation, error)
}

// AgentAskMutation is a turn, its confirmations, and the conversations.
type AgentAskMutation interface {
	// Say something to the agent. Starts a turn in the conversation given,
	// or in the main one, and returns the run to follow. Needs agent:use.
	AskAgent(ctx context.Context, arguments AskAgentArguments) (*AgentTurnView, error)

	// Answer a confirmation card: approve or decline what the agent asked
	// to do. Needs agent:use.
	ResolveAgentConfirmation(ctx context.Context, arguments ResolveAgentConfirmationArguments) (bool, error)

	// Stop a turn where it is. Needs agent:use.
	StopAgentRun(ctx context.Context, arguments StopAgentRunArguments) (bool, error)

	// Start a named conversation, kept apart from the main one. Needs
	// agent:use.
	StartAgentConversation(ctx context.Context, arguments StartAgentConversationArguments) (*models.AgentConversation, error)

	// Rename a conversation, or archive and unarchive it. The main
	// conversation is never archived. Needs agent:use.
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
}

// ListAgentConversationsArguments say whether archived ones are wanted.
type ListAgentConversationsArguments struct {
	Archived bool `json:"archived" graphapi:"nullable"`

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

// AgentConversationView is a conversation, its messages and its task list.
type AgentConversationView struct {
	Conversation *models.AgentConversation `json:"conversation"`
	Messages     []*models.AgentMessage    `json:"messages"`
	Total        int                       `json:"total"`
	Todos        []*models.AgentTodo       `json:"todos"`
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
}

// AskAgentArguments are one turn.
type AskAgentArguments struct {
	ConversationID string         `json:"conversationId" graphapi:"nullable"`
	Message        string         `json:"message"`
	Viewing        *agent.Viewing `json:"viewing" graphapi:"nullable"`
	Surface        string         `json:"surface" graphapi:"nullable"`

	// ReadOnly leaves out every tool that changes anything: what a
	// read-only profile of the command line asks for.
	ReadOnly bool `json:"readOnly" graphapi:"nullable"`

	// AttachmentIDs are files uploaded for this turn; References the
	// threads the person pointed at.
	AttachmentIDs []string                `json:"attachmentIds" graphapi:"nullable"`
	References    []models.AgentReference `json:"references" graphapi:"nullable"`
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

// StopAgentRunArguments name the run.
type StopAgentRunArguments struct {
	RunID string `json:"runId"`
}

// StartAgentConversationArguments may name it; the model does otherwise.
type StartAgentConversationArguments struct {
	Title string `json:"title" graphapi:"nullable"`
}

// UpdateAgentConversationArguments rename or archive.
type UpdateAgentConversationArguments struct {
	ConversationID string `json:"conversationId"`
	Title          string `json:"title" graphapi:"nullable"`
	Archived       *bool  `json:"archived" graphapi:"nullable"`
}

// agentOperations is the API as the person, for the agent's tools. Every
// call runs in its own transaction with the person's permissions resolved
// afresh, and the audit trail names the person with the agent as the
// actor.
type agentOperations struct {
	graph       *graph
	user        *models.User
	permissions *models.EffectivePermissions
}

func (self *agentOperations) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	ctx = api.ContextWithAuthenticatedUsername(ctx, self.user.Username)
	ctx = db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorAgent, UserID: self.user.ID})
	var outcome *graphql.Result
	if err := self.graph.database.TransactionContext(ctx, func(tx db.Transaction) error {
		ctx := api.ContextWithTransaction(ctx, tx)
		principal, err := self.graph.resolvePrincipal(tx, self.user.Username, self.user)
		if err != nil {
			return err
		}
		if principal == nil {
			return api.ErrNotLoggedIn
		}
		ctx = api.ContextWithPrincipal(ctx, principal)
		outcome = graphql.Do(graphql.Params{
			Schema:         self.graph.schema,
			RequestString:  document,
			VariableValues: variables,
			Context:        ctx,
		})
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
	conversations, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(conversations) > 0 {
		return conversations[0], nil
	}
	return tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, Title: "", LastAt: time.Now()})
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

// ListAgentRunsArguments bound the listing.
type ListAgentRunsArguments struct {
	First int `json:"first" graphapi:"nullable"`
}

func (self *graph) ListAgentRuns(ctx context.Context, arguments ListAgentRunsArguments) ([]*models.AgentConversation, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return []*models.AgentConversation{}, nil
		}
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return self.transaction(ctx).ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationRun}, &db.Options{Limit: uint64(limit)})
}

func (self *graph) ListAgentConversations(ctx context.Context, arguments ListAgentConversationsArguments) ([]*models.AgentConversation, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return []*models.AgentConversation{}, nil
		}
		return nil, err
	}
	tx := self.transaction(ctx)
	if query := strings.TrimSpace(arguments.Query); query != "" {
		return tx.SearchAgentConversations(found.ID, query, 50)
	}
	main, err := self.mainConversation(tx, found)
	if err != nil {
		return nil, err
	}
	named, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationNamed}, &db.Options{Limit: 200})
	if err != nil {
		return nil, err
	}
	conversations := []*models.AgentConversation{main}
	for _, conversation := range named {
		if (conversation.ArchivedAt != nil) == arguments.Archived {
			conversations = append(conversations, conversation)
		}
	}
	return conversations, nil
}

func (self *graph) ReadAgentConversation(ctx context.Context, arguments ReadAgentConversationArguments) (*AgentConversationView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, true)
	if err != nil {
		return nil, err
	}
	messages, err := tx.ListAgentMessages(conversation.ID, nil)
	if err != nil {
		return nil, err
	}
	total := len(messages)
	// The newest page by default: a drawer opens on the end of a
	// conversation, not its beginning.
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	start := len(messages) - limit - arguments.Offset
	end := len(messages) - arguments.Offset
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
	return &AgentConversationView{Conversation: conversation, Messages: messages[start:end], Total: total, Todos: todos}, nil
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
	tools := worker.Catalog().Offered(principal.Permissions, &configuration.Agent.Tools)
	views := make([]*AgentToolView, 0, len(tools))
	for _, tool := range tools {
		views = append(views, &AgentToolView{Name: tool.Name, Family: string(tool.Family), Risk: string(tool.Risk), Description: tool.Description, Confirms: agent.NeedsConfirmation(tool, nil, &configuration.Agent.Tools, found), Core: tool.Core})
	}
	return views, nil
}

func (self *graph) AskAgent(ctx context.Context, arguments AskAgentArguments) (*AgentTurnView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	// A run's transcript can be talked into: the person reading what the
	// agent did on its own — sorted a message, wrote a reply — asks about
	// it right there, with the message and the decision as the history.
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, true)
	if err != nil {
		return nil, err
	}
	surface := strings.TrimSpace(arguments.Surface)
	if surface == "" {
		surface = "drawer"
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
		if attachment.AgentID != found.ID || (attachment.MessageID != "" && attachment.ConversationID != conversation.ID) {
			return nil, api.ErrNotFound
		}
	}
	run, err := worker.Ask(&agent.AskSettings{
		Agent:        found,
		Owner:        principal.User,
		Operations:   &agentOperations{graph: self, user: principal.User, permissions: principal.Permissions},
		Conversation: conversation,
		Message:      arguments.Message,
		Viewing:      arguments.Viewing,
		Surface:      surface,
		ReadOnly:     arguments.ReadOnly,
		Attachments:  attachments,
		References:   arguments.References,
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
	run := worker.FindRun(arguments.RunID)
	if run == nil || run.Conversation().AgentID != found.ID {
		return false, api.ErrNotFound
	}
	log.Noticef("%s %s what their agent asked to do", operatorName(ctx), map[bool]string{true: "approved", false: "declined"}[arguments.Approve])
	return run.Resolve(arguments.CallID, arguments.Approve), nil
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
	run := worker.FindRun(arguments.RunID)
	if run == nil || run.Conversation().AgentID != found.ID {
		return false, api.ErrNotFound
	}
	run.Stop()
	return true, nil
}

func (self *graph) StartAgentConversation(ctx context.Context, arguments StartAgentConversationArguments) (*models.AgentConversation, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationNamed, Title: strings.TrimSpace(arguments.Title), LastAt: time.Now()})
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
	if worker := self.agentWorker(); worker != nil {
		worker.StopConversation(conversation.ID)
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
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, false)
	if err != nil {
		return nil, err
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
	return updated, nil
}

// AgentRunEvents follows a run over the websocket.
func (self *graph) AgentRunEvents(ctx context.Context, arguments ReadAgentRunArguments) (<-chan *agent.Event, error) {
	// A subscription resolves in a goroutine of its own, after the
	// transaction the socket opened for it has been committed, so the
	// lookup needs a transaction of its own.
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
