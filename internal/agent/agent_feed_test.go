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

// An action chosen by a turn answering for the person, who asked it to in
// words, is recorded with that turn's conversation and starts no turn of its
// own: the turn that chose carries it out.
func TestAnActionAnsweredInATurnStartsNoTurnOfItsOwn(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "Unsubscribe from three shops?", Choices: []models.FeedChoice{
		{ChoiceKey: "unsubscribe", ChoiceLabel: "Unsubscribe", ChoiceRole: models.FeedChoiceAction},
		{ChoiceKey: "not_now", ChoiceLabel: "Not now", ChoiceRole: models.FeedChoiceDismiss},
	}})
	settled, err := world.worker.ChooseFeedItem(context.Background(), &agent.FeedChoiceSettings{
		Agent: world.agent, Owner: world.owner, Operations: world.operations, Item: card, ChoiceKey: "unsubscribe",
		AnsweringConversationID: "answering-conversation-1",
	})
	if err != nil || settled.ChosenChoiceKey != "unsubscribe" || settled.ChosenConversationID != "answering-conversation-1" || settled.IsOutcomePending {
		t.Fatalf("recorded, with no outcome to wait for: %+v %v", settled, err)
	}
	world.worker.Wait()
	if _, messages := world.mainConversation(t); len(messages) != 0 {
		t.Fatalf("a second turn started in the main conversation: %+v", messages)
	}
	if _, err := world.choose(t, card, "not_now"); err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("the card was answered twice: %v", err)
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

// A dismissed card can still be answered, liked and shown less like, and
// stays dismissed through all of it: putting it back is the person's restore.
func TestADismissedCardCanStillBeAnsweredAndLiked(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "A new device signed in", Choices: []models.FeedChoice{
		{ChoiceKey: "me", ChoiceLabel: "That was me", ChoiceRole: models.FeedChoiceDismiss},
	}})
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.DismissAgentFeedItems(world.owner.ID, &db.FeedDismissSelection{FeedItemIDs: []string{card.ID}}, time.Now()); err != nil {
			t.Fatal(err)
		}
		card, _ = tx.GetAgentFeedItem(card.ID)
	})
	answered, err := world.choose(t, card, "me")
	if err != nil || answered.ChosenChoiceKey != "me" || answered.DismissedAt == nil {
		t.Fatalf("a dismissed card answered stays dismissed: %+v %v", answered, err)
	}
	liked, err := world.worker.LikeFeedItem(context.Background(), world.agent.ID, answered, true)
	if err != nil || !liked.IsLiked || liked.DismissedAt == nil {
		t.Fatalf("a dismissed card is liked: %+v %v", liked, err)
	}
	less, err := world.worker.ShowLessLikeFeedItem(context.Background(), world.agent.ID, liked)
	if err != nil || less.DismissedAt == nil || !less.DismissedAt.Equal(*liked.DismissedAt) {
		t.Fatalf("show less on a dismissed card only remembers: %+v %v", less, err)
	}
}

// Three news cards from one goal list as one entry with the stack's count, and
// a decision from the same goal stands alone.
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

// Each turn of a goal on set times is a stack of its own, topped by its
// summary, so this morning's brief is not piled on yesterday's; a turn that
// names no stack title is titled for the goal.
func TestEachTurnOfAGoalOnSetTimesIsAStackWithTheSummaryOnTop(t *testing.T) {
	world := newSpeakFirstWorld(t)
	var goal *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		goal, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationGoal, LastAt: time.Now(),
			Goal: "Tell me about the morning", GoalTitle: "Morning brief", GoalState: models.GoalWorking, GoalTimes: []string{"0 8 * * *"},
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	// A turn is the goal's job while it runs.
	turn := func() *models.AgentJob {
		var running *models.AgentJob
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			queued, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: world.agent.ID, Kind: models.AgentJobGoal, SubjectID: goal.ID})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := tx.ClaimAgentJobs("instance-a", 50, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range claimed {
				if job.ID == queued.ID {
					running = job
				}
			}
		})
		if running == nil {
			t.Fatal("the goal's turn is not running")
		}
		return running
	}
	finish := func(job *models.AgentJob) {
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			if _, err := tx.FinishAgentJob(job.ID, job.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobDone, FinishedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		})
	}
	zero, one, two := 0, 1, 2
	card := func(title string, position *int, stackTitle string) {
		world.post(t, &models.AgentFeedItem{
			Title: title, FeedItemKind: models.FeedItemReport, FeedSourceKind: models.FeedSourceGoal, FeedSourceID: goal.ID,
			RunConversationID: goal.ID, StackPosition: position, FeedStackTitle: stackTitle,
		})
	}
	yesterday := turn()
	card("Yesterday's summary", &zero, "")
	finish(yesterday)
	today := turn()
	card("Section: weather", &one, "")
	card("Section: headlines", &two, "Morning brief, Wed Nov 19")
	card("Summary", &zero, "")
	finish(today)
	todayKey := "goal:" + goal.ID + ":" + today.ID
	yesterdayKey := "goal:" + goal.ID + ":" + yesterday.ID
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		items, _, err := tx.ListAgentFeedItems(world.owner.ID, nil, 10, false, time.Now())
		if err != nil || len(items) != 2 {
			t.Fatalf("two turns are two stacks: %d %v", len(items), err)
		}
		titles := []string{items[0].Title, items[1].Title}
		if !contains(titles, "Summary") || !contains(titles, "Yesterday's summary") {
			t.Fatalf("each stack is topped by its summary: %v", titles)
		}
		stack, _, err := tx.ListAgentFeedStackItems(world.owner.ID, todayKey, 0, 10, false, time.Now())
		if err != nil || len(stack) != 3 || stack[0].Title != "Summary" || stack[1].Title != "Section: weather" || stack[2].Title != "Section: headlines" {
			t.Fatalf("by position: %+v %v", stack, err)
		}
		counts, _ := tx.CountAgentFeedStacks(world.owner.ID, []string{todayKey, yesterdayKey}, time.Now())
		if counts[todayKey].StackTitle != "Morning brief" || counts[yesterdayKey].StackTitle != "Morning brief" {
			t.Fatalf("a turn's stack is titled for the goal when its first card names no title: %+v %+v", counts[todayKey], counts[yesterdayKey])
		}
	})
}

// A goal's turn that hands its work to a subagent still owns what the
// subagent posts: the card is credited to the goal.
func TestACardPostedByASubagentIsCreditedToTheGoalBehindIt(t *testing.T) {
	world := newSpeakFirstWorld(t)
	var goal, subagentRun *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		goal, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationGoal, LastAt: time.Now(),
			Goal: "Watch the weather", GoalTitle: "Weather", GoalState: models.GoalWorking,
		})
		if err != nil {
			t.Fatal(err)
		}
		subagentRun, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.agent.ID, Kind: models.AgentConversationRun, JobKind: string(models.AgentJobSubagent), SubjectID: goal.ID, LastAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	posted := world.post(t, &models.AgentFeedItem{Title: "Rain at noon", FeedItemKind: models.FeedItemReport, RunConversationID: subagentRun.ID})
	if posted.FeedSourceKind != models.FeedSourceGoal || posted.FeedSourceID != goal.ID {
		t.Fatalf("credited to the goal: %s %s", posted.FeedSourceKind, posted.FeedSourceID)
	}
	if posted.FeedStackKey != "goal:"+goal.ID {
		t.Fatalf("stacked with the goal's cards: %s", posted.FeedStackKey)
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
		Title: "api-pane on laptop asks", FeedSourceKind: models.FeedSourceCodingSession, FeedSourceID: "laptop/7", CoalesceKey: "laptop/7/question-one",
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

// The same preference is one fact: showing less like a card twice, or liking
// it twice, files each sentence once on the preferences page.
func TestAPreferenceAlreadyOnThePageIsNotFiledAgain(t *testing.T) {
	world := newSpeakFirstWorld(t)
	card := world.post(t, &models.AgentFeedItem{Title: "Humanoid robots in warehouses", FeedItemKind: models.FeedItemNews})
	for range 2 {
		if _, err := world.worker.ShowLessLikeFeedItem(context.Background(), world.agent.ID, card); err != nil {
			t.Fatal(err)
		}
		if _, err := world.worker.LikeFeedItem(context.Background(), world.agent.ID, card, true); err != nil {
			t.Fatal(err)
		}
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "self/feed")
		if err != nil || node == nil {
			t.Fatalf("the preferences page: %v %v", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, true, 50)
		if err != nil || len(facts) != 2 {
			t.Fatalf("one fact for each sentence: %+v %v", facts, err)
		}
	})
}

// The sweep that moves answered cards to the dismissed ones tells the open
// feeds of each card, with the person's unread count read once, and leaves a
// card restored since the cutoff where it is.
func TestTheSweepAnnouncesEachDismissedCard(t *testing.T) {
	world := newSpeakFirstWorld(t)
	choices := []models.FeedChoice{{ChoiceKey: "go", ChoiceLabel: "Go", ChoiceRole: models.FeedChoiceAction}}
	first := world.post(t, &models.AgentFeedItem{Title: "Book the table?", Choices: choices})
	second := world.post(t, &models.AgentFeedItem{Title: "Order more tea?", Choices: choices})
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, card := range []*models.AgentFeedItem{first, second} {
			if _, err := tx.SettleAgentFeedItem(card.ID, &models.FeedSettlement{ChoiceKey: "go"}, time.Now().Add(-48*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err := world.worker.SweepAnsweredFeedItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, card := range []*models.AgentFeedItem{first, second} {
			if stored, err := tx.GetAgentFeedItem(card.ID); err != nil || stored.DismissedAt == nil {
				t.Fatalf("the card was not swept: %+v %v", stored, err)
			}
		}
		restored, err := tx.RestoreAgentFeedItems(first.UserID, []string{first.ID}, time.Now())
		if err != nil || len(restored) != 1 {
			t.Fatalf("restore: %+v %v", restored, err)
		}
	})
	if err := world.worker.SweepAnsweredFeedItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if stored, _ := tx.GetAgentFeedItem(first.ID); stored.DismissedAt != nil {
			t.Fatal("a restored card was swept again")
		}
	})
}
