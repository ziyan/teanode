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

	// cardSourceID is the goal the card that GetAgentFeedItem answers with
	// came from.
	cardSourceID string
}

func (self *recordedOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

func (self *recordedOperations) Execute(_ context.Context, document string, variables map[string]any, response any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
	card := `{"feedItemId":"card-1","feedSourceKind":"goal","feedSourceId":"` + self.cardSourceID + `"}`
	return json.Unmarshal([]byte(`{"GetAgentFeedItem":`+card+`,"PostAgentFeedItem":{"feedItemId":"card-1"},"DismissAgentFeedItems":["card-1"],"RestoreAgentFeedItems":["card-1"],"WithdrawAgentFeedItem":true,"ResolveAgentFeedItem":{"feedItemId":"card-1","isResolved":true},"UnresolveAgentFeedItem":{"feedItemId":"card-1"},"ListAgentFeed":{"feedItems":[],"unreadCount":0},"ListAgentFeedStack":{"feedItems":[],"unreadCount":0},"ChooseAgentFeedItem":{"feedItemId":"card-1"},"LikeAgentFeedItem":{"feedItemId":"card-1"},"ShowLessLikeAgentFeedItem":{"feedItemId":"card-1"},"MarkAgentFeedRead":1}`), response)
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

// The dismissed cards are listed on their own.
func TestTheFeedToolListsTheDismissed(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"list","dismissed_only":true}`)}); err != nil {
		t.Fatal(err)
	}
	if operations.variables[0]["isDismissedOnly"] != true {
		t.Fatalf("the dismissed cards were not asked for: %+v", operations.variables[0])
	}
}

// A card is resolved or unresolved through the operations the apps call; a
// goal's own turn names its conversation, which holds it to the cards it
// posted, and the person's conversation names none.
func TestTheFeedToolResolvesAndUnresolves(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	for _, arguments := range []string{
		`{"action":"resolve","feed_item_id":"card-1","resolution_note":"The parcel arrived"}`,
		`{"action":"resolve","feed_item_id":"card-2"}`,
		`{"action":"unresolve","feed_item_id":"card-1"}`,
	} {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
	}
	if !strings.Contains(operations.documents[0], "ResolveAgentFeedItem") || !strings.Contains(operations.documents[2], "UnresolveAgentFeedItem") {
		t.Fatalf("documents: %+v", operations.documents)
	}
	if operations.variables[0]["feedItemId"] != "card-1" || operations.variables[0]["resolutionNote"] != "The parcel arrived" || operations.variables[0]["runConversationId"] != nil {
		t.Fatalf("a resolve: %+v", operations.variables[0])
	}
	if _, isNoteSent := operations.variables[1]["resolutionNote"]; isNoteSent {
		t.Fatalf("a resolve with no note sent one: %+v", operations.variables[1])
	}
	goal := &models.AgentConversation{ID: "goal-conversation-1", Kind: models.AgentConversationGoal, Goal: "Watch the parcels"}
	goalTurn := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal, isHeadless: true})
	for _, arguments := range []string{`{"action":"resolve","feed_item_id":"card-1"}`, `{"action":"unresolve","feed_item_id":"card-1"}`} {
		if _, err := feedTool(t).Run(goalTurn, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
	}
	if operations.variables[3]["runConversationId"] != "goal-conversation-1" || operations.variables[4]["runConversationId"] != "goal-conversation-1" {
		t.Fatalf("a goal's own turn did not name its conversation: %+v %+v", operations.variables[3], operations.variables[4])
	}
	personInGoal := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal})
	if _, err := feedTool(t).Run(personInGoal, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"resolve","feed_item_id":"card-1"}`)}); err != nil || operations.variables[5]["runConversationId"] != nil {
		t.Fatalf("the person in a goal's conversation: %v %+v", err, operations.variables[5])
	}
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"resolve"}`)}); err == nil {
		t.Fatal("a resolve without a card was accepted")
	}
}

// Charts go through as the model wrote them, and a camera still is the media
// entry's camera_still, taken by the attachment the camera tool's answer gave.
func TestTheFeedToolPostsChartsAndCameraStills(t *testing.T) {
	operations := &recordedOperations{}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations})
	arguments := `{"action":"post","title":"A parcel was left","chart_blocks":[{"chartKind":"series","chartTitle":"Balance","provenance":"Read from the bank at 09:40 EST","xLabels":["Mon","Tue"],"series":[{"seriesName":"This week","values":[1,null]}]}],` +
		`"media":[{"media_caption":"By the door","camera_still":{"attachment_id":"attachment-1","camera_name":"Front door","captured_at":"2026-11-19T14:02:11-05:00","still_reason":"Where the parcel was left","live_url":"https://nvr.example.com/live"}}]}`
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
		t.Fatal(err)
	}
	variables := operations.variables[0]
	charts, _ := variables["chartBlocks"].([]any)
	if len(charts) != 1 || charts[0].(map[string]any)["provenance"] != "Read from the bank at 09:40 EST" {
		t.Fatalf("charts: %+v", variables["chartBlocks"])
	}
	media, _ := variables["media"].([]map[string]any)
	if len(media) != 1 || media[0]["mediaCaption"] != "By the door" {
		t.Fatalf("media: %+v", variables["media"])
	}
	still, _ := media[0]["cameraStill"].(map[string]any)
	if still["attachmentId"] != "attachment-1" || still["cameraName"] != "Front door" || still["capturedAt"] != "2026-11-19T14:02:11-05:00" || still["stillReason"] != "Where the parcel was left" || still["liveUrl"] != "https://nvr.example.com/live" {
		t.Fatalf("the still: %+v", still)
	}
	if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"post","title":"A chart","chart_blocks":{"chartKind":"bars"}}`)}); err == nil || !strings.Contains(err.Error(), "chart_blocks is a list of charts") {
		t.Fatalf("one chart that is not in a list: %v", err)
	}
}

// The rules of a good card are in the tool's description.
func TestTheFeedToolDescriptionSaysTheRulesOfACard(t *testing.T) {
	description := feedTool(t).Description
	for _, expected := range []string{
		"what happened, why it matters and one or two next steps", "Numbers come only from this turn's tool results", "provenance", "One card per subject", "`list` first",
		"never rewrite a card into a different subject", "no ids or tool names", "title that says what the body says", "Silence is the default",
		"`resolve` that card", "instead of posting a card that says all is clear", "materially worse", "not finished (today, this hour) is never drawn as measured",
		"keeps every field you leave out", "postNotes",
	} {
		if !strings.Contains(description, expected) {
			t.Errorf("the description does not say %q", expected)
		}
	}
	if strings.ContainsAny(description, "\u2014\u2013") {
		t.Error("the description has a dash")
	}
}

// What the person's buttons do, the tool does when they ask in words: each
// action sends the operation the apps send, a choice names this turn's
// conversation so that no second turn starts, and the pages go on from the
// cursor the page before gave.
func TestTheFeedToolDoesWhatThePersonsButtonsDo(t *testing.T) {
	operations := &recordedOperations{}
	main := &models.AgentConversation{ID: "main-conversation-1", Kind: models.AgentConversationMain}
	ctx := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: main})
	calls := []struct {
		arguments string
		operation string
		variables map[string]any
	}{
		{`{"action":"list","after":"cursor-2"}`, "ListAgentFeed(", map[string]any{"after": "cursor-2"}},
		{`{"action":"stack","stack_key":"brief-1"}`, "ListAgentFeedStack(", map[string]any{"feedStackKey": "brief-1"}},
		{`{"action":"show","feed_item_id":"card-1"}`, "GetAgentFeedItem(", map[string]any{"feedItemId": "card-1"}},
		{`{"action":"choose","feed_item_id":"card-1","choice_key":"unsubscribe"}`, "ChooseAgentFeedItem(", map[string]any{"feedItemId": "card-1", "choiceKey": "unsubscribe", "runConversationId": "main-conversation-1"}},
		{`{"action":"like","feed_item_id":"card-1"}`, "LikeAgentFeedItem(", map[string]any{"feedItemId": "card-1", "isLiked": true}},
		{`{"action":"unlike","feed_item_id":"card-1"}`, "LikeAgentFeedItem(", map[string]any{"feedItemId": "card-1", "isLiked": false}},
		{`{"action":"less","feed_item_id":"card-1"}`, "ShowLessLikeAgentFeedItem(", map[string]any{"feedItemId": "card-1"}},
		{`{"action":"read","feed_item_ids":["card-1","card-2"]}`, "MarkAgentFeedRead(", nil},
	}
	for index, call := range calls {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(call.arguments)}); err != nil {
			t.Fatalf("%s: %v", call.arguments, err)
		}
		if !strings.Contains(operations.documents[index], call.operation) {
			t.Fatalf("%s sent %s", call.arguments, operations.documents[index])
		}
		for name, value := range call.variables {
			if operations.variables[index][name] != value {
				t.Fatalf("%s: %s is %v, want %v", call.arguments, name, operations.variables[index][name], value)
			}
		}
	}
	if ids, _ := operations.variables[len(calls)-1]["feedItemIds"].([]string); len(ids) != 2 {
		t.Fatalf("read did not name both cards: %+v", operations.variables[len(calls)-1])
	}
	for _, arguments := range []string{`{"action":"choose","feed_item_id":"card-1"}`, `{"action":"stack"}`, `{"action":"read"}`, `{"action":"like"}`} {
		if _, err := feedTool(t).Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err == nil {
			t.Fatalf("%s ran without what it needs", arguments)
		}
	}
}

// A goal's own turn presses the person's buttons as a conversation with them
// does, when its instructions say to: nothing here is held back for it.
func TestAGoalTurnPressesThePersonsButtons(t *testing.T) {
	operations := &recordedOperations{}
	goal := &models.AgentConversation{ID: "goal-conversation-1", Kind: models.AgentConversationGoal, Goal: "Watch the parcels"}
	goalTurn := tools.WithRun(context.Background(), &feedRun{operations: operations, conversation: goal, isHeadless: true})
	for _, arguments := range []string{
		`{"action":"choose","feed_item_id":"card-1","choice_key":"yes"}`,
		`{"action":"like","feed_item_id":"card-1"}`,
		`{"action":"unlike","feed_item_id":"card-1"}`,
		`{"action":"less","feed_item_id":"card-1"}`,
		`{"action":"read","feed_item_ids":["card-1"]}`,
		`{"action":"restore","feed_item_ids":["card-1"]}`,
		`{"action":"withdraw","feed_item_id":"card-1"}`,
	} {
		if _, err := feedTool(t).Run(goalTurn, &tools.Call{ID: "c1", Arguments: []byte(arguments)}); err != nil {
			t.Fatalf("a goal's own turn could not run %s: %v", arguments, err)
		}
	}
	if len(operations.documents) != 7 {
		t.Fatalf("the goal's own turn sent %d documents, not 7", len(operations.documents))
	}
}
