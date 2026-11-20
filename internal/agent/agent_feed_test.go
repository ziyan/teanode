package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A post with action choices raises a durable question in the posting turn's
// conversation and does not wait on it; the same coalesce key again is the
// same card.
func TestAPostWithChoicesRaisesADurableQuestionAndCoalesces(t *testing.T) {
	world := newSpeakFirstWorld(t)
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.agent.ID, Kind: models.AgentConversationRun, Title: "A run", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	})
	post := func(title string) (*models.AgentFeedItem, models.FeedChange) {
		item, change, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{
			AgentID: world.agent.ID, InteractionConversationID: conversation.ID,
			Item: &models.AgentFeedItem{
				Title: title, Body: "Somebody signed in from a new laptop.", CoalesceKey: "sign-in",
				Choices: []models.FeedChoice{
					{ChoiceKey: "me", ChoiceLabel: "That's me", ChoiceRole: models.FeedChoiceDismiss},
					{ChoiceKey: "secure", ChoiceLabel: "Secure my account", ChoiceRole: models.FeedChoiceAction},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return item, change
	}
	first, change := post("Was this sign-in you?")
	if change != models.FeedChangePosted || first.FeedItemKind != models.FeedItemDecision || first.InteractionID == "" {
		t.Fatalf("first post: %v %+v", change, first)
	}
	second, change := post("Was this sign-in you, from a new laptop?")
	if change != models.FeedChangeReplaced || second.ID != first.ID {
		t.Fatalf("a post with the same key is the same card: %v %s %s", change, second.ID, first.ID)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		open, err := tx.ListOpenAgentInteractions(conversation.ID)
		if err != nil || len(open) != 1 || open[0].ID != second.InteractionID || open[0].InteractionChoices[1] != "Secure my account" {
			t.Fatalf("one question is open, the new one: %+v %v", open, err)
		}
	})
}

// A page already in an open card of the last month is refused, and the card
// that has it may be replaced by a post that links it again.
func TestAPageAlreadyInAnOpenCardIsRefused(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := func(title, key string) *agent.FeedPost {
		return &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
			Title: title, CoalesceKey: key, FeedItemKind: models.FeedItemNews,
			SourceLinks: []models.FeedSourceLink{{SourceTitle: "Example News", SourceURL: "https://news.example.com/picker"}},
		}}
	}
	if _, _, err := world.worker.PostFeedItem(context.Background(), card("A new picking robot", "picker")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := world.worker.PostFeedItem(context.Background(), card("The same robot again", "")); err == nil || !strings.Contains(err.Error(), "already in an open card") {
		t.Fatalf("the same page twice: %v", err)
	}
	if _, change, err := world.worker.PostFeedItem(context.Background(), card("The picking robot, updated", "picker")); err != nil || change != models.FeedChangeReplaced {
		t.Fatalf("an update of the card that has it: %v %v", change, err)
	}
	tooMany := &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Facts"}}
	for index := 0; index < 9; index++ {
		tooMany.Item.FactRows = append(tooMany.Item.FactRows, models.FeedFactRow{FactLabel: "Row", FactValue: "Value"})
	}
	if _, _, err := world.worker.PostFeedItem(context.Background(), tooMany); err == nil || !strings.Contains(err.Error(), "at most 8 fact rows") {
		t.Fatalf("nine fact rows: %v", err)
	}
}

func (self *speakFirstWorld) post(t *testing.T, item *models.AgentFeedItem) *models.AgentFeedItem {
	t.Helper()
	posted, _, err := self.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: self.agent.ID, Item: item})
	if err != nil {
		t.Fatal(err)
	}
	return posted
}

func (self *speakFirstWorld) choose(t *testing.T, item *models.AgentFeedItem, choiceKey string) (*models.AgentFeedItem, error) {
	t.Helper()
	return self.worker.ChooseFeedItem(context.Background(), &agent.FeedChoiceSettings{
		Agent: self.agent, Owner: self.owner, Operations: self.operations, IsPersonsTap: true, Item: item, ChoiceKey: choiceKey,
	})
}

func (self *speakFirstWorld) mainConversation(t *testing.T) (*models.AgentConversation, []*models.AgentMessage) {
	t.Helper()
	var main *models.AgentConversation
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		mains, err := tx.ListAgentConversations(self.agent.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(mains) == 1 {
			main = mains[0]
			messages, _ = tx.ListAgentMessages(main.ID, nil)
		}
	})
	return main, messages
}

// A dismiss choice settles the card and answers its question with no turn and
// no message; a second answer is refused.
func TestADismissChoiceSettlesWithoutATurn(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "Was this sign-in you?", Choices: []models.FeedChoice{
		{ChoiceKey: "me", ChoiceLabel: "That's me", ChoiceRole: models.FeedChoiceDismiss},
		{ChoiceKey: "secure", ChoiceLabel: "Secure my account", ChoiceRole: models.FeedChoiceAction},
	}})
	settled, err := world.choose(t, card, "me")
	if err != nil || settled.ChosenChoiceKey != "me" || settled.IsOutcomePending {
		t.Fatalf("settled: %+v %v", settled, err)
	}
	if _, messages := world.mainConversation(t); len(messages) != 0 {
		t.Fatalf("a dismiss says nothing in the conversation: %+v", messages)
	}
	if _, err := world.choose(t, card, "secure"); err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("the second answer: %v", err)
	}
	if _, err := world.choose(t, card, "nothing"); err == nil {
		t.Fatal("a button the card does not have was pressed")
	}
}

// An action choice creates one message of the person's in the main
// conversation, worded as their choice and referencing the card, starts a
// turn there, and the card settles with the conversation and the first line of
// the turn's answer as its outcome.
func TestAnActionChoiceSaysItInTheMainConversationWithTheCardAttached(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "Sort newsletters into a Newsletters folder", Body: "Twelve messages would move.", Choices: []models.FeedChoice{
		{ChoiceKey: "approve", ChoiceLabel: "Approve", ChoiceRole: models.FeedChoiceAction},
		{ChoiceKey: "not_now", ChoiceLabel: "Not now", ChoiceRole: models.FeedChoiceDismiss},
	}})
	settled, err := world.choose(t, card, "approve")
	if err != nil || settled.ChosenChoiceKey != "approve" || settled.ChosenConversationID == "" || !settled.IsOutcomePending {
		t.Fatalf("settled with a turn pending: %+v %v", settled, err)
	}
	world.worker.Wait()

	main, messages := world.mainConversation(t)
	if main == nil || main.ID != settled.ChosenConversationID {
		t.Fatalf("the card carried on in the main conversation: %v %s", main, settled.ChosenConversationID)
	}
	var said []*models.AgentMessage
	for _, message := range messages {
		if message.Role == "user" {
			said = append(said, message)
		}
	}
	if len(said) != 1 || !strings.Contains(said[0].Content, "Approve: Sort newsletters into a Newsletters folder") {
		t.Fatalf("one message of the person's, worded as their choice: %+v", said)
	}
	if len(said[0].References) != 1 || said[0].References[0].FeedItemID != card.ID || said[0].References[0].FeedItemTitle != card.Title {
		t.Fatalf("with the card attached: %+v", said[0].References)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			card, _ = tx.GetAgentFeedItem(card.ID)
		})
		if !card.IsOutcomePending || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if card.IsOutcomePending || !strings.HasPrefix(card.OutcomeText, "Hello, I am your new agent.") {
		t.Fatalf("the first line of the turn's answer is the outcome: %+v", card)
	}
}

// A like is written to the agent's memory as a preference, and a "show less
// like this" records the opposite and dismisses the card.
func TestALikeIsWrittenToMemory(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "Humanoid robots in warehouses", FeedItemKind: models.FeedItemNews})
	liked, err := world.worker.LikeFeedItem(context.Background(), world.agent.ID, card, true)
	if err != nil || !liked.IsLiked {
		t.Fatalf("like: %+v %v", liked, err)
	}
	if _, err := world.worker.ShowLessLikeFeedItem(context.Background(), world.agent.ID, card); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "self/feed")
		if err != nil || node == nil {
			t.Fatalf("the preferences page: %v %v", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 50)
		if err != nil || len(facts) != 2 || facts[0].Kind != models.FactPreference {
			t.Fatalf("two preferences: %+v %v", facts, err)
		}
		gone, _ := tx.GetAgentFeedItem(card.ID)
		if gone.DismissedAt == nil {
			t.Fatal("show less dismisses the card")
		}
	})
}

// Three news cards from one goal list as one entry with the stack's count, a
// decision from the same goal stands alone, and a run of a schedule stacks
// per run with its summary on top.
func TestCardsFromOneGoalStackAndADecisionStandsAlone(t *testing.T) {
	world := newSpeakFirstWorld(t)
	for _, title := range []string{"One", "Two", "Three"} {
		world.post(t, &models.AgentFeedItem{Title: title, FeedItemKind: models.FeedItemNews, FeedSourceKind: models.FeedSourceGoal, FeedSourceID: "goal-1"})
	}
	decision := world.post(t, &models.AgentFeedItem{Title: "Archive these?", FeedSourceKind: models.FeedSourceGoal, FeedSourceID: "goal-1", Choices: []models.FeedChoice{
		{ChoiceKey: "yes", ChoiceLabel: "Archive", ChoiceRole: models.FeedChoiceAction},
	}})
	if decision.FeedStackKey != "" {
		t.Fatalf("a decision never stacks: %q", decision.FeedStackKey)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		items, _, err := tx.ListAgentFeedItems(world.owner.ID, nil, 10, false, time.Now())
		if err != nil || len(items) != 2 {
			t.Fatalf("one entry for the stack and one for the decision: %d %v", len(items), err)
		}
		stack, _, err := tx.ListAgentFeedStackItems(world.owner.ID, "goal:goal-1", 0, 10, false, time.Now())
		if err != nil || len(stack) != 3 || stack[0].Title != "Three" || stack[2].Title != "One" {
			t.Fatalf("the stack's cards, newest first: %+v %v", stack, err)
		}
		counts, err := tx.CountAgentFeedStacks(world.owner.ID, []string{"goal:goal-1"}, time.Now())
		if err != nil || counts["goal:goal-1"].ItemCount != 3 || counts["goal:goal-1"].UnreadCount != 3 {
			t.Fatalf("counts: %+v %v", counts, err)
		}
	})
}

func TestARunOfAScheduleStacksPerRunWithTheSummaryOnTop(t *testing.T) {
	world := newSpeakFirstWorld(t)
	zero, one, two := 0, 1, 2
	runCard := func(runId, title string, position *int, stackTitle string) {
		world.post(t, &models.AgentFeedItem{
			Title: title, FeedItemKind: models.FeedItemReport, FeedSourceKind: models.FeedSourceSchedule, FeedSourceID: "schedule-1",
			RunConversationID: runId, StackPosition: position, FeedStackTitle: stackTitle,
		})
	}
	runCard("run-a", "Section: weather", &one, "")
	runCard("run-a", "Section: headlines", &two, "Morning brief, Wed Nov 19")
	runCard("run-a", "Summary", &zero, "")
	runCard("run-b", "Yesterday's summary", &zero, "Morning brief, Tue Nov 18")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		items, _, err := tx.ListAgentFeedItems(world.owner.ID, nil, 10, false, time.Now())
		if err != nil || len(items) != 2 {
			t.Fatalf("two runs are two stacks: %d %v", len(items), err)
		}
		titles := []string{items[0].Title, items[1].Title}
		if !contains(titles, "Summary") || !contains(titles, "Yesterday's summary") {
			t.Fatalf("each stack is topped by its summary: %v", titles)
		}
		stack, _, err := tx.ListAgentFeedStackItems(world.owner.ID, "schedule:schedule-1:run-a", 0, 10, false, time.Now())
		if err != nil || len(stack) != 3 || stack[0].Title != "Summary" || stack[1].Title != "Section: weather" || stack[2].Title != "Section: headlines" {
			t.Fatalf("by position: %+v %v", stack, err)
		}
		counts, _ := tx.CountAgentFeedStacks(world.owner.ID, []string{"schedule:schedule-1:run-a"}, time.Now())
		if counts["schedule:schedule-1:run-a"].StackTitle != "Morning brief, Wed Nov 19" {
			t.Fatalf("the stack's title is the first any card gave: %+v", counts)
		}
	})
}

// A schedule's run that hands its work to a subagent still owns what the
// subagent posts: the card is credited to the schedule and stacks with the run.
func TestACardPostedByASubagentIsCreditedToTheScheduleBehindIt(t *testing.T) {
	world := newSpeakFirstWorld(t)
	var scheduleRun, subagentRun *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		scheduleRun, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationRun, JobKind: string(models.AgentJobSchedule), SubjectID: "schedule-1", LastAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		subagentRun, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationRun, JobKind: string(models.AgentJobSubagent), SubjectID: scheduleRun.ID, LastAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	posted := world.post(t, &models.AgentFeedItem{Title: "Weather", FeedItemKind: models.FeedItemReport, RunConversationID: subagentRun.ID})
	if posted.FeedSourceKind != models.FeedSourceSchedule || posted.FeedSourceID != "schedule-1" {
		t.Fatalf("credited to the schedule: %s %s", posted.FeedSourceKind, posted.FeedSourceID)
	}
	if posted.RunConversationID != scheduleRun.ID || posted.FeedStackKey != "schedule:schedule-1:"+scheduleRun.ID {
		t.Fatalf("stacked with the schedule's run: %s %s", posted.RunConversationID, posted.FeedStackKey)
	}
}

func contains(list []string, wanted string) bool {
	for _, each := range list {
		if each == wanted {
			return true
		}
	}
	return false
}

// A layout's content is checked as it is posted, and a malformed one is
// refused with the field named.
func TestLayoutContentIsCheckedAndRoundTrips(t *testing.T) {
	world := newSpeakFirstWorld(t)
	valid := map[models.FeedCardLayout]string{
		models.FeedLayoutList:    `{"listRows":[{"rowTitle":"A new picking robot","rowMeta":"Example News","rowUrl":"https://news.example.com/picker"}]}`,
		models.FeedLayoutWeather: `{"placeName":"Springfield","currentTemperature":12.5,"temperatureUnit":"C","conditionText":"Cloudy","hourlyForecast":[{"forecastHour":"2026-11-19T09:00","forecastTemperature":13,"precipitationChancePercent":20}]}`,
		models.FeedLayoutAgenda:  `{"agendaEntries":[{"startsAt":"2026-11-19T09:00","endsAt":"2026-11-19T09:30","agendaTitle":"Stand-up","agendaEntryKind":"meeting"}]}`,
	}
	for layout, content := range valid {
		posted := world.post(t, &models.AgentFeedItem{Title: "A " + string(layout), Body: "Plain words", FeedCardLayout: layout, LayoutContent: content, CoalesceKey: string(layout)})
		var back *models.AgentFeedItem
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) { back, _ = tx.GetAgentFeedItem(posted.ID) })
		if back.FeedCardLayout != layout || !strings.Contains(back.LayoutContent, firstFieldOf(layout)) {
			t.Fatalf("%s did not round-trip: %+v", layout, back)
		}
	}
	refused := []struct {
		layout  models.FeedCardLayout
		content string
		field   string
	}{
		{models.FeedLayoutList, `{"listRows":[{"rowTitle":"Story","rowUrl":"ftp://news.example.com/picker"}]}`, "listRows[0].rowUrl"},
		{models.FeedLayoutList, `{"listRows":[{"rowDetail":"No title"}]}`, "listRows[0].rowTitle"},
		{models.FeedLayoutList, `{"rows":[]}`, "rows"},
		{models.FeedLayoutWeather, `{"placeName":"Springfield","temperatureUnit":"C","conditionText":"Cloudy"}`, "currentTemperature"},
		{models.FeedLayoutWeather, `{"placeName":"Springfield","currentTemperature":1,"temperatureUnit":"C","conditionText":"Cloudy","hourlyForecast":[{"forecastHour":"noon","forecastTemperature":1}]}`, "hourlyForecast[0].forecastHour"},
		{models.FeedLayoutAgenda, `{"agendaEntries":[{"startsAt":"2026-11-19T09:00","agendaTitle":"Stand-up","agendaEntryKind":"party"}]}`, "agendaEntries[0].agendaEntryKind"},
		{models.FeedLayoutText, `{"listRows":[]}`, "layout_content"},
	}
	for _, each := range refused {
		_, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Bad", FeedCardLayout: each.layout, LayoutContent: each.content}})
		if err == nil || !strings.Contains(err.Error(), each.field) {
			t.Errorf("%s %s: expected a refusal naming %s, got %v", each.layout, each.content, each.field, err)
		}
	}
	tooManyRows := `{"listRows":[` + strings.Repeat(`{"rowTitle":"Story"},`, 10) + `{"rowTitle":"Story"}]}`
	if _, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{Title: "Bad", FeedCardLayout: models.FeedLayoutList, LayoutContent: tooManyRows}}); err == nil || !strings.Contains(err.Error(), "listRows") {
		t.Errorf("eleven rows: %v", err)
	}
}

func firstFieldOf(layout models.FeedCardLayout) string {
	return map[models.FeedCardLayout]string{models.FeedLayoutList: "listRows", models.FeedLayoutWeather: "placeName", models.FeedLayoutAgenda: "agendaEntries"}[layout]
}

// When a session reports its question answered, its open card settles as
// answered on the computer.
func TestACodingSessionCardSettlesWhenTheQuestionIsAnswered(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{
		Title: "api-pane on laptop asks", FeedSourceKind: models.FeedSourceCodingSession, FeedSourceID: "laptop/7", CoalesceKey: "laptop/7/abc123",
		Choices: []models.FeedChoice{{ChoiceKey: "option:1", ChoiceLabel: "Yes", ChoiceRole: models.FeedChoiceAction}, {ChoiceKey: "option:2", ChoiceLabel: "No", ChoiceRole: models.FeedChoiceAction}},
	})
	if !strings.HasPrefix(card.FeedStackKey, "coding:") || !strings.HasPrefix(card.FeedStackTitle, "Coding sessions, ") {
		t.Fatalf("a coding session's question goes in the day's stack: %q %q", card.FeedStackKey, card.FeedStackTitle)
	}
	other := world.post(t, &models.AgentFeedItem{
		Title: "web-pane on desktop asks", FeedSourceKind: models.FeedSourceCodingSession, FeedSourceID: "desktop/3", CoalesceKey: "desktop/3/def456",
		Choices: []models.FeedChoice{{ChoiceKey: "open", ChoiceLabel: "Open session", ChoiceRole: models.FeedChoiceOpen}},
	})
	if other.FeedStackKey != card.FeedStackKey {
		t.Fatalf("another session's question of the same day is in the same stack: %q and %q", other.FeedStackKey, card.FeedStackKey)
	}
	if err := world.worker.SettleFeedItemsOfSession(context.Background(), world.agent.ID, "laptop", "7", ""); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		settled, _ := tx.GetAgentFeedItem(card.ID)
		if settled.ChosenChoiceKey != "answered" || settled.OutcomeText != "Answered on the computer" {
			t.Fatalf("settled: %+v", settled)
		}
	})
}

// With the feed as the destination, a memory check the person asked for runs
// in a conversation of its own, told to post a card, and writes nothing in the
// main conversation.
func TestAMemoryCheckRunsInARunConversationWhenTheFeedIsTheDestination(t *testing.T) {
	world := newSpeakFirstWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(world.agent.ID, func(found *models.Agent) error {
			found.FeedDestination = models.FeedDestinationFeed
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if err := tx.MarkAgentSpokeFirst(world.agent.ID, nil, &now, nil); err != nil {
			t.Fatal(err)
		}
		found, err := tx.GetAgent(world.agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := world.worker.SpeakFirstNow(tx, found, agent.SpeakFirstMemoryCheck); err != nil {
			t.Fatal(err)
		}
	})
	world.tick(t)

	if _, messages := world.mainConversation(t); len(messages) != 0 {
		t.Fatalf("nothing in the main conversation: %+v", messages)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		runs, err := tx.ListAgentConversations(world.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			if run.JobKind != string(models.AgentJobSpeakFirst) || run.SubjectID != agent.SpeakFirstMemoryCheck {
				continue
			}
			messages, _ := tx.ListAgentMessages(run.ID, nil)
			if len(messages) == 0 || !strings.Contains(messages[0].Content, "post it as ONE card with the feed tool") {
				t.Fatalf("the run is told to post a card: %+v", messages)
			}
			return
		}
		t.Fatalf("no run conversation for the check: %+v", runs)
	})
}
