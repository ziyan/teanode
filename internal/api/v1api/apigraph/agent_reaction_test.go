package apigraph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/graphapi"
)

// The person reacts to a message of their conversation through the schema,
// reads the reaction back with the messages, and whoever has the
// conversation open is told. Nobody else reacts to it or sees it.
func TestThePersonReactsThroughTheSchemaAndOnlyThePerson(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person, stranger *models.User
	var conversation *models.AgentConversation
	var answer, toolAnswer *models.AgentMessage
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "reacting", Name: "Example Person"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mine, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if stranger, err = tx.CreateUser(&models.User{Username: "stranger", Name: "Example Stranger"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: stranger.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: mine.ID, Kind: models.AgentConversationNamed, Title: "a chat", LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: "is the boiler booked?"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		if answer, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: "Booked for Thursday."}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		if toolAnswer, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "tool", Content: "{}"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := agentpackage.New(&agentpackage.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	graphApi := graphapi.New()
	var query Query = resolver
	var mutation Mutation = resolver
	if err := graphApi.Register(&query, &mutation, nil); err != nil {
		t.Fatalf("Register: %s", err)
	}
	schema, err := graphApi.Build()
	if err != nil {
		t.Fatalf("Build: %s", err)
	}
	allowed := func(user *models.User) *api.Principal {
		return &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	}
	// send runs a document as somebody, committed, and returns its data
	// and its errors.
	send := func(user *models.User, document string) (map[string]any, []string) {
		t.Helper()
		var result *graphql.Result
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), allowed(user)), tx)
			result = graphql.Do(graphql.Params{Schema: schema, RequestString: document, Context: ctx})
		})
		var messages []string
		for _, failure := range result.Errors {
			messages = append(messages, failure.Message)
		}
		data, _ := result.Data.(map[string]any)
		return data, messages
	}
	react := func(user *models.User, messageId, reactionEmoji string) []string {
		t.Helper()
		_, errors := send(user, `mutation { ReactToAgentMessage(messageId: "`+messageId+`", emoji: "`+reactionEmoji+`") }`)
		return errors
	}
	reactionsShown := func() []any {
		t.Helper()
		data, errors := send(person, `{ ReadAgentConversation(conversationId: "`+conversation.ID+`") { messages { id role reactions { emoji reactorKind reactedAt } } } }`)
		if len(errors) > 0 {
			t.Fatalf("ReadAgentConversation: %v", errors)
		}
		messages := data["ReadAgentConversation"].(map[string]any)["messages"].([]any)
		for _, each := range messages {
			message := each.(map[string]any)
			if message["id"] == answer.ID {
				reactions, _ := message["reactions"].([]any)
				return reactions
			}
		}
		t.Fatal("the answer is not in the conversation")
		return nil
	}
	feed, unsubscribe := worker.SubscribeConversation(conversation.ID)
	defer unsubscribe()
	nextEvent := func() agentpackage.Event {
		t.Helper()
		select {
		case event := <-feed:
			return event
		case <-time.After(5 * time.Second):
			t.Fatal("no event within five seconds")
			return agentpackage.Event{}
		}
	}

	if shown := reactionsShown(); len(shown) != 0 {
		t.Fatalf("none to begin with, and a list rather than null: %v", shown)
	}
	if errors := react(person, answer.ID, "👍"); len(errors) > 0 {
		t.Fatalf("ReactToAgentMessage: %v", errors)
	}
	shown := reactionsShown()
	if len(shown) != 1 {
		t.Fatalf("one reaction: %v", shown)
	}
	if reaction := shown[0].(map[string]any); reaction["emoji"] != "👍" || reaction["reactorKind"] != "person" || reaction["reactedAt"] == "" {
		t.Fatalf("the person's thumbs up: %v", reaction)
	}
	if event := nextEvent(); event.Kind != agentpackage.EventReacted || event.MessageID != answer.ID || event.Emoji != "👍" || event.ReactorKind != "person" || event.ConversationID != conversation.ID {
		t.Fatalf("the reacted event: %+v", event)
	}
	if errors := react(person, answer.ID, "❤️"); len(errors) > 0 {
		t.Fatalf("replacing: %v", errors)
	}
	if shown := reactionsShown(); len(shown) != 1 || shown[0].(map[string]any)["emoji"] != "❤️" {
		t.Fatalf("replaced: %v", shown)
	}
	nextEvent()

	// What is not an emoji, or not a message somebody said, is refused,
	// and nobody is told of it.
	for name, arguments := range map[string][2]string{
		"a word":          {answer.ID, "yes"},
		"three emoji":     {answer.ID, "👍👍👍"},
		"a tool's answer": {toolAnswer.ID, "👍"},
		"no message":      {"nothing", "👍"},
	} {
		if errors := react(person, arguments[0], arguments[1]); len(errors) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}

	// Somebody else can neither react nor read the reaction.
	if errors := react(stranger, answer.ID, "😢"); len(errors) == 0 || !strings.Contains(strings.Join(errors, " "), "not found") {
		t.Errorf("a stranger reacted: %v", errors)
	}
	if _, errors := send(stranger, `{ ReadAgentConversation(conversationId: "`+conversation.ID+`") { messages { reactions { emoji } } } }`); len(errors) == 0 {
		t.Error("a stranger read the reactions")
	}
	if shown := reactionsShown(); len(shown) != 1 || shown[0].(map[string]any)["emoji"] != "❤️" {
		t.Fatalf("the stranger changed nothing: %v", shown)
	}

	// An empty emoji takes it away, and says so.
	if errors := react(person, answer.ID, ""); len(errors) > 0 {
		t.Fatalf("taking it away: %v", errors)
	}
	if shown := reactionsShown(); len(shown) != 0 {
		t.Fatalf("taken away: %v", shown)
	}
	if event := nextEvent(); event.Kind != agentpackage.EventReacted || event.Emoji != "" || event.MessageID != answer.ID {
		t.Fatalf("the event that takes it away: %+v", event)
	}
	select {
	case event := <-feed:
		t.Fatalf("nothing else was told: %+v", event)
	case <-time.After(200 * time.Millisecond):
	}
}
