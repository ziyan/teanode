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
	return json.Unmarshal([]byte(`{"GetAgentFeedItem":`+card+`,"PostAgentFeedItem":{"feedItemId":"card-1"},"DismissAgentFeedItems":["card-1"],"RestoreAgentFeedItems":["card-1"],"WithdrawAgentFeedItem":true,"ResolveAgentFeedItem":{"feedItemId":"card-1","isResolved":true},"UnresolveAgentFeedItem":{"feedItemId":"card-1"},"ListAgentFeed":{"feedItems":[],"unreadCount":0}}`), response)
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

// A goal's own turn withdraws only a card whose source is that goal: the tool
// reads the card first, and sends the goal's conversation so that the server
// checks it again. The person's turn is not held to it.
func TestAGoalTurnWithdrawsOnlyItsOwnCards(t *testing.T) {
	goal := &models.AgentConversation{ID: "goal-conversation-1", Kind: models.AgentConversationGoal, Goal: "Watch the parcels"}
	arguments := []byte(`{"action":"withdraw","feed_item_id":"card-1"}`)

	foreign := &recordedOperations{cardSourceID: "goal-conversation-2"}
	goalTurn := tools.WithRun(context.Background(), &feedRun{operations: foreign, conversation: goal, isHeadless: true})
	if _, err := feedTool(t).Run(goalTurn, &tools.Call{ID: "c1", Arguments: arguments}); err == nil || !strings.Contains(err.Error(), "only a card it posted") {
		t.Fatalf("a goal withdrew another goal's card: %v", err)
	}
	for _, document := range foreign.documents {
		if strings.Contains(document, "WithdrawAgentFeedItem") {
			t.Fatalf("the withdrawal was sent: %+v", foreign.documents)
		}
	}

	own := &recordedOperations{cardSourceID: "goal-conversation-1"}
	goalTurn = tools.WithRun(context.Background(), &feedRun{operations: own, conversation: goal, isHeadless: true})
	if _, err := feedTool(t).Run(goalTurn, &tools.Call{ID: "c1", Arguments: arguments}); err != nil {
		t.Fatal(err)
	}
	last := len(own.documents) - 1
	if !strings.Contains(own.documents[last], "WithdrawAgentFeedItem") || own.variables[last]["runConversationId"] != "goal-conversation-1" {
		t.Fatalf("the withdrawal does not name the goal's conversation: %+v %+v", own.documents, own.variables)
	}

	person := &recordedOperations{cardSourceID: "goal-conversation-2"}
	personTurn := tools.WithRun(context.Background(), &feedRun{operations: person, conversation: goal})
	if _, err := feedTool(t).Run(personTurn, &tools.Call{ID: "c1", Arguments: arguments}); err != nil {
		t.Fatalf("the person in a goal's conversation could not withdraw: %v", err)
	}
	if _, isNamed := person.variables[len(person.variables)-1]["runConversationId"]; isNamed {
		t.Fatalf("the person's withdrawal is held to a goal: %+v", person.variables)
	}
}
