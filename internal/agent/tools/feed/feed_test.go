package feed_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/feed"
	"github.com/ziyan/teanode/internal/models"
)

// recordedOperations keeps the documents the tool sends.
type recordedOperations struct {
	documents []string
	variables []map[string]any
}

func (self *recordedOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

func (self *recordedOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
	return json.Unmarshal([]byte(`{"PostAgentFeedItem":{"feedItemId":"card-1"},"DismissAgentFeedItems":["card-1"],"RestoreAgentFeedItems":["card-1"],"WithdrawAgentFeedItem":true,"ListAgentFeed":{"feedItems":[],"unreadCount":0}}`), response)
}

type feedRun struct {
	tools.Run
	operations   *recordedOperations
	conversation *models.AgentConversation
	isHeadless   bool
}

func (self *feedRun) Headless() bool { return self.isHeadless }

func (self *feedRun) Agent() *models.Agent         { return &models.Agent{ID: "agent-1"} }
func (self *feedRun) Owner() *models.User          { return &models.User{ID: "owner-1"} }
func (self *feedRun) Operations() tools.Operations { return self.operations }
func (self *feedRun) Conversation() *models.AgentConversation {
	return self.conversation
}

func feedTool(t *testing.T) *tools.Tool {
	t.Helper()
	for _, each := range tools.Build().All() {
		if each.Name == "feed" {
			return each
		}
	}
	t.Fatal("the feed tool is not registered")
	return nil
}

// A goal's turn that posts a news card says which goal made it, by the
// goal's conversation, and which conversation posted, so that the card can
// name the goal and the info sheet can link to it.
func TestAGoalTurnPostsACardThatNamesTheGoal(t *testing.T) {
	operations := &recordedOperations{}
	goal := &models.AgentConversation{ID: "goal-conversation-1", Kind: models.AgentConversationGoal, Goal: "Keep a feed of warehouse robot news"}
	arguments := `{"action":"post","title":"A new picking robot","feed_item_kind":"news","picture_symbol":"newspaper","coalesce_key":"robot-picker",` +
		`"source_links":[{"source_title":"Example News","source_url":"https://news.example.com/picker"}],"fact_rows":[{"fact_label":"Price","fact_value":"unknown"}]}`
	if _, err := feedTool(t).Run(tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal}), &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
		t.Fatal(err)
	}
	if len(operations.variables) != 1 || !strings.Contains(operations.documents[0], "PostAgentFeedItem") {
		t.Fatalf("one post: %+v", operations.documents)
	}
	variables := operations.variables[0]
	if variables["feedSourceKind"] != "goal" || variables["feedSourceId"] != "goal-conversation-1" || variables["runConversationId"] != "goal-conversation-1" {
		t.Fatalf("the card does not say which goal made it: %+v", variables)
	}
	if variables["feedItemKind"] != "news" || variables["coalesceKey"] != "robot-picker" || variables["pictureSymbol"] != "newspaper" {
		t.Fatalf("the card's own fields were lost: %+v", variables)
	}
	links, _ := variables["sourceLinks"].([]map[string]string)
	if len(links) != 1 || links[0]["sourceUrl"] != "https://news.example.com/picker" {
		t.Fatalf("source links: %+v", variables["sourceLinks"])
	}
}

// A card posted from the person's own conversation belongs to the agent.
func TestACardPostedInTheMainConversationNamesNoSource(t *testing.T) {
	operations := &recordedOperations{}
	main := &models.AgentConversation{ID: "main-1", Kind: models.AgentConversationMain}
	if _, err := feedTool(t).Run(tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: main}), &tools.Call{ID: "c1", Arguments: []byte(`{"action":"post","title":"A test card"}`)}); err != nil {
		t.Fatal(err)
	}
	if variables := operations.variables[0]; variables["feedSourceKind"] != nil {
		t.Fatalf("a post from the main conversation names no source: %+v", variables)
	}
}

// Dismiss takes exactly one of ids, a stack or every read card, and restore
// takes ids; each goes through the operation the apps call.
func TestTheFeedToolDismissesAndRestores(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	for _, arguments := range []string{
		`{"action":"dismiss","feed_item_ids":["card-1","card-2"]}`,
		`{"action":"dismiss","stack_key":"morning-brief"}`,
		`{"action":"dismiss","every_read":true}`,
		`{"action":"restore","feed_item_ids":["card-1"]}`,
	} {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
	}
	if len(operations.documents) != 4 || !strings.Contains(operations.documents[0], "DismissAgentFeedItems") || !strings.Contains(operations.documents[3], "RestoreAgentFeedItems") {
		t.Fatalf("documents: %+v", operations.documents)
	}
	if ids, _ := operations.variables[0]["feedItemIds"].([]string); len(ids) != 2 {
		t.Fatalf("ids: %+v", operations.variables[0])
	}
	if operations.variables[1]["feedStackKey"] != "morning-brief" || operations.variables[2]["isEveryReadCard"] != true {
		t.Fatalf("variables: %+v", operations.variables)
	}
	for _, arguments := range []string{
		`{"action":"dismiss"}`,
		`{"action":"dismiss","every_read":true,"stack_key":"morning-brief"}`,
		`{"action":"restore"}`,
	} {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err == nil {
			t.Fatalf("%s was accepted", arguments)
		}
	}
}

func TestTheFeedToolUpdatesWithdrawsAndLists(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	for _, arguments := range []string{`{"action":"update","feed_item_id":"card-1","body":"Now sunny"}`, `{"action":"withdraw","feed_item_id":"card-1"}`, `{"action":"list"}`} {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
	}
	if len(operations.documents) != 3 || operations.variables[0]["feedItemId"] != "card-1" || !strings.Contains(operations.documents[1], "WithdrawAgentFeedItem") || !strings.Contains(operations.documents[2], "ListAgentFeed") {
		t.Fatalf("documents: %+v", operations.documents)
	}
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"update","body":"x"}`)}); err == nil {
		t.Fatal("an update without a card was accepted")
	}
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"post"}`)}); err == nil {
		t.Fatal("a post without a title was accepted")
	}
}

// The dismissed cards are listed on their own, and a goal's own turn cannot
// bring back what the person dismissed, though the person's conversation can.
func TestTheFeedToolListsTheDismissedAndAGoalTurnCannotRestore(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"list","dismissed_only":true}`)}); err != nil {
		t.Fatal(err)
	}
	if operations.variables[0]["isDismissedOnly"] != true {
		t.Fatalf("the dismissed cards were not asked for: %+v", operations.variables[0])
	}
	goal := &models.AgentConversation{ID: "goal-conversation-1", Kind: models.AgentConversationGoal, Goal: "Watch the parcels"}
	goalTurn := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal, isHeadless: true})
	if _, err := feedTool(t).Run(goalTurn, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"restore","feed_item_ids":["card-1"]}`)}); err == nil || !strings.Contains(err.Error(), "cannot restore") {
		t.Fatalf("a goal's own turn restored a card: %v", err)
	}
	personInGoal := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal})
	if _, err := feedTool(t).Run(personInGoal, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"restore","feed_item_ids":["card-1"]}`)}); err != nil {
		t.Fatalf("the person in a goal's conversation could not restore: %v", err)
	}
}
