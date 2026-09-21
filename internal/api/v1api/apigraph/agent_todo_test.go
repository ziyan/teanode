package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The task list a conversation keeps, changed by the person whose
// conversation it is.
//
// It was written by the agent's own `todo` tool and by nothing else: the
// drawer and `conversation show` printed it, and somebody who had done
// one of the things on it could not say so. The tool ticks an item off by
// an identifier of its own and checks the conversation it is running in
// before it writes, and these mutations have to make the same check --
// an item is addressed by its identifier alone, so without it one
// person's identifier would tick off an item on somebody else's list.
func TestATodoIsChangedOnlyByThePersonWhoseConversationItIs(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner, stranger *models.User
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "todo-owner", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		ownersAgent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: ownersAgent.ID, Kind: models.AgentConversationNamed, Title: "The move",
		}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if stranger, err = tx.CreateUser(&models.User{Username: "todo-stranger", Name: "Carol Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: stranger.ID, Enabled: true, Name: "Coco"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})

	asPerson := func(person *models.User) *api.Principal {
		return &api.Principal{
			User: person,
			Permissions: models.NewEffectivePermissions([]models.Grant{
				{Permission: models.PermissionAgentUse},
			}),
		}
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asPerson(owner)), tx)

		if _, err := resolver.AddAgentTodo(ctx, AddAgentTodoArguments{
			ConversationID: conversation.ID, Text: "   ",
		}); err == nil {
			t.Errorf("an item with no words is refused")
		} else if !errors.Is(err, api.ErrInvalidArguments) {
			t.Errorf("and refused as a bad argument: %s", err)
		}

		added, err := resolver.AddAgentTodo(ctx, AddAgentTodoArguments{
			ConversationID: conversation.ID, Text: "  book the van  ",
		})
		if err != nil {
			t.Fatalf("AddAgentTodo: %s", err)
		}
		if added.Text != "book the van" {
			t.Errorf("the item reads %q, want it without the spaces around it", added.Text)
		}
		if added.DoneAt != nil {
			t.Errorf("a new item is open, and this one is done at %s", added.DoneAt)
		}

		done := true
		marked, err := resolver.SetAgentTodo(ctx, SetAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID, Done: &done,
		})
		if err != nil {
			t.Fatalf("SetAgentTodo(done): %s", err)
		}
		if marked.DoneAt == nil {
			t.Errorf("the item is done")
		}
		if marked.Text != added.Text {
			t.Errorf("and keeps its words: %q", marked.Text)
		}

		// Nothing given leaves the item as it stands: that is what makes
		// rewriting the words of a finished item safe.
		left, err := resolver.SetAgentTodo(ctx, SetAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID, Text: "book the van and the ramp",
		})
		if err != nil {
			t.Fatalf("SetAgentTodo(text): %s", err)
		}
		if left.DoneAt == nil {
			t.Errorf("an item whose words changed is still done")
		}
		if left.Text != "book the van and the ramp" {
			t.Errorf("the item reads %q", left.Text)
		}

		open := false
		reopened, err := resolver.SetAgentTodo(ctx, SetAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID, Done: &open,
		})
		if err != nil {
			t.Fatalf("SetAgentTodo(reopen): %s", err)
		}
		if reopened.DoneAt != nil {
			t.Errorf("the item is open again, and is done at %s", reopened.DoneAt)
		}

		// The tool reads the list on its next round, so what the person
		// changed has to be on it and nowhere else.
		todos, err := tx.ListAgentTodos(conversation.ID)
		if err != nil {
			t.Fatalf("ListAgentTodos: %s", err)
		}
		if len(todos) != 1 || todos[0].ID != added.ID || todos[0].Text != "book the van and the ramp" {
			t.Errorf("the conversation's list is %+v", todos)
		}

		strangersContext := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asPerson(stranger)), tx)
		if _, err := resolver.AddAgentTodo(strangersContext, AddAgentTodoArguments{
			ConversationID: conversation.ID, Text: "not mine to add",
		}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("somebody else cannot add to the list, and is told it is not there: %v", err)
		}
		if _, err := resolver.SetAgentTodo(strangersContext, SetAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID, Done: &done,
		}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("nor tick one off: %v", err)
		}
		if _, err := resolver.RemoveAgentTodo(strangersContext, RemoveAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID,
		}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("nor take one off: %v", err)
		}
		after, err := tx.ListAgentTodos(conversation.ID)
		if err != nil {
			t.Fatalf("ListAgentTodos: %s", err)
		}
		if len(after) != 1 {
			t.Errorf("and nothing of theirs moved: %+v", after)
		}

		removed, err := resolver.RemoveAgentTodo(ctx, RemoveAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID,
		})
		if err != nil || !removed {
			t.Fatalf("RemoveAgentTodo: %v %s", removed, err)
		}
		// An identifier that names no item of theirs is refused rather
		// than answered yes: the delete is scoped to the conversation and
		// would otherwise remove nothing and say it had.
		if _, err := resolver.RemoveAgentTodo(ctx, RemoveAgentTodoArguments{
			ConversationID: conversation.ID, TodoID: added.ID,
		}); !errors.Is(err, api.ErrNotFound) {
			t.Errorf("removing it twice is refused: %v", err)
		}
	})
}
