package apigraph

import (
	"context"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type feedWorld struct {
	resolver *graph
	person   *models.User
	stranger *models.User
	agentId  string
	within   func(user *models.User, function func(ctx context.Context))
}

func newFeedWorld(t *testing.T) *feedWorld {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	world := &feedWorld{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.person, err = tx.CreateUser(&models.User{Username: "reader", Name: "Example Reader"}); err != nil {
			t.Fatal(err)
		}
		created, err := tx.CreateAgent(&models.Agent{UserID: world.person.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		world.agentId = created.ID
		if world.stranger, err = tx.CreateUser(&models.User{Username: "stranger", Name: "Example Stranger"}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: world.stranger.ID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := agentpackage.New(&agentpackage.Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	world.resolver = &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	world.within = func(user *models.User, function func(ctx context.Context)) {
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			function(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx))
		})
	}
	return world
}

// A goal's news cards are posted through the API, stack as one entry with the
// stack's count, and name the goal by its title; a decision from the same
// goal stands alone; the stack lists its three newest first; a stranger sees
// none of it.
func TestAGoalsNewsCardsStackAndNameTheGoal(t *testing.T) {
	world := newFeedWorld(t)
	var goalId string
	world.within(world.person, func(ctx context.Context) {
		goal, err := world.resolver.StartAgentGoal(ctx, StartAgentGoalArguments{GoalTitle: "Warehouse robot news", GoalDescription: "Keep a feed of news about warehouse robots."})
		if err != nil {
			t.Fatal(err)
		}
		goalId = goal.ConversationID
	})
	post := func(ctx context.Context, title string, extra func(*PostAgentFeedItemArguments)) *AgentFeedItemView {
		arguments := PostAgentFeedItemArguments{
			Title: title, FeedItemKind: "news", FeedSourceKind: "goal", FeedSourceID: goalId, RunConversationID: goalId, PictureSymbol: "newspaper",
			SourceLinks: []models.FeedSourceLink{{SourceTitle: "Example News", SourceURL: "https://news.example.com/" + title}},
		}
		if extra != nil {
			extra(&arguments)
		}
		view, err := world.resolver.PostAgentFeedItem(ctx, arguments)
		if err != nil {
			t.Fatalf("PostAgentFeedItem %s: %s", title, err)
		}
		return view
	}
	world.within(world.person, func(ctx context.Context) {
		for _, title := range []string{"one", "two", "three"} {
			post(ctx, title, nil)
		}
		decision := post(ctx, "decide", func(arguments *PostAgentFeedItemArguments) {
			arguments.FeedItemKind = "decision"
			arguments.SourceLinks = nil
			arguments.Choices = []models.FeedChoice{{ChoiceKey: "go", ChoiceLabel: "Go", ChoiceRole: models.FeedChoiceAction}}
		})
		if decision.FeedStackKey != "" || decision.FeedSourceTitle != "Warehouse robot news" {
			t.Fatalf("a decision stands alone and names its goal: %+v", decision)
		}
		if _, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: "again", SourceLinks: []models.FeedSourceLink{{SourceURL: "https://news.example.com/one"}}}); err == nil {
			t.Fatal("a page already in an open card was posted again")
		}

		page, err := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{})
		if err != nil || len(page.FeedItems) != 2 || page.UnreadCount != 4 || page.NextCursor != "" {
			t.Fatalf("one entry for the stack, one for the decision, four cards unread: %+v %v", page, err)
		}
		var top *AgentFeedItemView
		for _, view := range page.FeedItems {
			if view.FeedStackKey != "" {
				top = view
			}
		}
		if top == nil || top.Title != "three" || top.StackItemCount != 3 || top.StackUnreadCount != 3 || top.FeedSourceTitle != "Warehouse robot news" || top.FeedCardLayout != "text" {
			t.Fatalf("the stack's top card: %+v", top)
		}
		stack, err := world.resolver.ListAgentFeedStack(ctx, ListAgentFeedStackArguments{FeedStackKey: top.FeedStackKey})
		if err != nil || len(stack.FeedItems) != 3 || stack.FeedItems[0].Title != "three" || stack.FeedItems[1].Title != "two" || stack.FeedItems[2].Title != "one" {
			t.Fatalf("the stack, newest first: %+v %v", stack, err)
		}
		if readCount, err := world.resolver.MarkAgentFeedRead(ctx, MarkAgentFeedReadArguments{FeedItemIDs: []string{stack.FeedItems[0].FeedItemID, stack.FeedItems[1].FeedItemID}}); err != nil || readCount != 2 {
			t.Fatalf("read: %d %v", readCount, err)
		}
		after, _ := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{})
		if after.UnreadCount != 2 {
			t.Fatalf("unread after reading two: %d", after.UnreadCount)
		}
		dismissed, err := world.resolver.DismissAgentFeedItems(ctx, DismissAgentFeedItemsArguments{FeedItemIDs: []string{decision.FeedItemID}})
		if err != nil || len(dismissed) != 1 || dismissed[0] != decision.FeedItemID {
			t.Fatalf("dismiss: %+v %v", dismissed, err)
		}
		discussed, err := world.resolver.DiscussAgentFeedItem(ctx, AgentFeedItemArguments{FeedItemID: top.FeedItemID})
		if err != nil || discussed == "" {
			t.Fatalf("discuss answers the main conversation: %q %v", discussed, err)
		}
	})
	world.within(world.stranger, func(ctx context.Context) {
		page, err := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{})
		if err != nil || len(page.FeedItems) != 0 || page.UnreadCount != 0 {
			t.Fatalf("a stranger's feed is empty: %+v %v", page, err)
		}
		if _, err := world.resolver.GetAgentFeedItem(ctx, AgentFeedItemArguments{FeedItemID: goalId}); err == nil {
			t.Fatal("a stranger read a card")
		}
	})
}

// A post that changes a card by id keeps it where it stands, and a
// malformed layout is refused as an invalid argument.
func TestAPostCanChangeACardAndRefusesABadLayout(t *testing.T) {
	world := newFeedWorld(t)
	world.within(world.person, func(ctx context.Context) {
		posted, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: "Weather", CoalesceKey: "weather"})
		if err != nil {
			t.Fatal(err)
		}
		changed, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{FeedItemID: posted.FeedItemID, Body: "Sunny"})
		if err != nil || changed.FeedItemID != posted.FeedItemID || changed.Body != "Sunny" || changed.Title != "Weather" {
			t.Fatalf("changed in place: %+v %v", changed, err)
		}
		if _, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: "Bad", CardLayout: "weather", LayoutContent: `{"placeName":"Springfield"}`}); err == nil {
			t.Fatal("a weather card with no temperature")
		}
		good, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: "Good", CardLayout: "weather", LayoutContent: `{"placeName":"Springfield","currentTemperature":12,"temperatureUnit":"C","conditionText":"Cloudy"}`})
		if err != nil || good.FeedCardLayout != "weather" || good.LayoutContent == "" {
			t.Fatalf("a weather card: %+v %v", good, err)
		}
	})
}

// Dismiss takes exactly one of ids, a stack or every read card, refuses
// another person's id as a whole, keeps a decision that waits when every read
// card goes, and each card is announced; restore brings back what the person
// dismissed and not what a choice settled.
func TestDismissingAndRestoringFeedCards(t *testing.T) {
	world := newFeedWorld(t)
	var strangerCardId string
	world.within(world.stranger, func(ctx context.Context) {
		card, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: "Somebody else's"})
		if err != nil {
			t.Fatal(err)
		}
		strangerCardId = card.FeedItemID
	})
	world.within(world.person, func(ctx context.Context) {
		post := func(title, stackKey string) *AgentFeedItemView {
			view, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: title, StackKey: stackKey})
			if err != nil {
				t.Fatalf("post %s: %s", title, err)
			}
			return view
		}
		single, read := post("Single", ""), post("Read", "")
		stackedOne, stackedTwo := post("Stacked one", "evening-brief"), post("Stacked two", "evening-brief")
		decision, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{
			Title: "Decide", FeedItemKind: "decision", Choices: []models.FeedChoice{{ChoiceKey: "go", ChoiceLabel: "Go", ChoiceRole: models.FeedChoiceAction}},
		})
		if err != nil {
			t.Fatal(err)
		}
		isTrue := true
		stackKey := "evening-brief"

		for name, arguments := range map[string]DismissAgentFeedItemsArguments{
			"none":      {},
			"two":       {FeedItemIDs: []string{single.FeedItemID}, FeedStackKey: &stackKey},
			"false":     {IsEveryReadCard: new(bool)},
			"too many":  {FeedItemIDs: make([]string, feedChangeMost+1)},
			"a strange": {FeedItemIDs: []string{single.FeedItemID, strangerCardId}},
		} {
			if dismissed, err := world.resolver.DismissAgentFeedItems(ctx, arguments); err == nil {
				t.Fatalf("%s was accepted: %v", name, dismissed)
			}
		}
		if still, _ := world.resolver.GetAgentFeedItem(ctx, AgentFeedItemArguments{FeedItemID: single.FeedItemID}); still.IsDismissed {
			t.Fatal("a refused dismissal still closed a card")
		}

		dismissed, err := world.resolver.DismissAgentFeedItems(ctx, DismissAgentFeedItemsArguments{FeedStackKey: &stackKey})
		if err != nil || len(dismissed) != 2 {
			t.Fatalf("the stack: %v %v", dismissed, err)
		}
		if _, err := world.resolver.MarkAgentFeedRead(ctx, MarkAgentFeedReadArguments{FeedItemIDs: []string{read.FeedItemID, decision.FeedItemID}}); err != nil {
			t.Fatal(err)
		}
		dismissed, err = world.resolver.DismissAgentFeedItems(ctx, DismissAgentFeedItemsArguments{IsEveryReadCard: &isTrue})
		if err != nil || len(dismissed) != 1 || dismissed[0] != read.FeedItemID {
			t.Fatalf("every read card keeps the waiting decision and the unread card: %v %v", dismissed, err)
		}
		page, err := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{})
		if err != nil || len(page.FeedItems) != 2 {
			t.Fatalf("the unread card and the decision stay: %+v %v", page, err)
		}

		restored, err := world.resolver.RestoreAgentFeedItems(ctx, RestoreAgentFeedItemsArguments{FeedItemIDs: []string{stackedOne.FeedItemID, stackedTwo.FeedItemID, single.FeedItemID}})
		if err != nil || len(restored) != 2 {
			t.Fatalf("restore brings back the two dismissed: %v %v", restored, err)
		}
		if _, err := world.resolver.RestoreAgentFeedItems(ctx, RestoreAgentFeedItemsArguments{FeedItemIDs: []string{strangerCardId}}); err == nil {
			t.Fatal("another person's card was restored")
		}
		if page, _ := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{}); len(page.FeedItems) != 3 {
			t.Fatalf("after the restore: %d", len(page.FeedItems))
		}
	})
	world.within(world.stranger, func(ctx context.Context) {
		if page, _ := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{}); len(page.FeedItems) != 1 {
			t.Fatalf("the stranger's own card is untouched: %+v", page)
		}
	})
}

// The dismissed cards are a list of their own, a stack's cards each an
// entry, and a withdrawn card is gone rather than dismissed.
func TestTheDismissedCardsAndAWithdrawnCard(t *testing.T) {
	world := newFeedWorld(t)
	world.within(world.person, func(ctx context.Context) {
		post := func(title, stackKey string) *AgentFeedItemView {
			view, err := world.resolver.PostAgentFeedItem(ctx, PostAgentFeedItemArguments{Title: title, StackKey: stackKey})
			if err != nil {
				t.Fatalf("post %s: %s", title, err)
			}
			return view
		}
		stackedOne, stackedTwo, open := post("Stacked one", "evening-brief"), post("Stacked two", "evening-brief"), post("Still open", "")
		withdrawn := post("No longer true", "")
		stackKey := "evening-brief"
		if _, err := world.resolver.DismissAgentFeedItems(ctx, DismissAgentFeedItemsArguments{FeedStackKey: &stackKey}); err != nil {
			t.Fatal(err)
		}
		isTrue := true
		page, err := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{IsDismissedOnly: &isTrue})
		if err != nil || len(page.FeedItems) != 2 {
			t.Fatalf("the dismissed cards: %+v %v", page, err)
		}
		for _, entry := range page.FeedItems {
			if entry.FeedItemID != stackedOne.FeedItemID && entry.FeedItemID != stackedTwo.FeedItemID {
				t.Fatalf("a card that is not dismissed is listed: %s", entry.Title)
			}
			if !entry.IsDismissed || entry.DismissedAt == nil || entry.StackItemCount != 1 || entry.FeedStackKey != stackKey {
				t.Fatalf("each dismissed card is an entry of its own, with its stack named: %+v", entry)
			}
		}

		if isWithdrawn, err := world.resolver.WithdrawAgentFeedItem(ctx, AgentFeedItemArguments{FeedItemID: withdrawn.FeedItemID}); err != nil || !isWithdrawn {
			t.Fatalf("withdraw: %v %v", isWithdrawn, err)
		}
		if _, err := world.resolver.GetAgentFeedItem(ctx, AgentFeedItemArguments{FeedItemID: withdrawn.FeedItemID}); err == nil {
			t.Fatal("a withdrawn card can still be read")
		}
		if page, _ := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{IsDismissedOnly: &isTrue}); len(page.FeedItems) != 2 {
			t.Fatalf("a withdrawn card is among the dismissed: %d", len(page.FeedItems))
		}
		if page, _ := world.resolver.ListAgentFeed(ctx, ListAgentFeedArguments{}); len(page.FeedItems) != 1 || page.FeedItems[0].FeedItemID != open.FeedItemID {
			t.Fatalf("the feed keeps the open card alone: %+v", page.FeedItems)
		}
	})
}
