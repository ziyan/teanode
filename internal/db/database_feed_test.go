package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// feedTestAgent makes a person and their agent and returns the ids.
func feedTestAgent(t *testing.T, tx db.Transaction, username string) (userId, agentId string) {
	t.Helper()
	owner, err := tx.CreateUser(&models.User{Username: username})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return owner.ID, agent.ID
}

func newTestCard(userId, agentId, title string) *models.AgentFeedItem {
	return &models.AgentFeedItem{
		AgentID: agentId, UserID: userId, FeedItemKind: models.FeedItemNews, FeedSourceKind: models.FeedSourceAgent,
		Title: title, Body: "A line about " + title, FeedUrgency: models.FeedUrgencyToday,
	}
}

// A post with the key of an open card replaces it: one card, moved to the
// top and unread again, and a like on it is kept.
func TestPostingWithTheSameCoalesceKeyReplacesTheCard(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-replace")
		start := time.Now()
		weather := newTestCard(userId, agentId, "Cloudy in Springfield")
		weather.CoalesceKey = "weather"
		first, change, err := tx.UpsertAgentFeedItem(weather, start)
		if err != nil || change != models.FeedChangePosted {
			t.Fatalf("first post: %v %v", change, err)
		}
		other, _, err := tx.UpsertAgentFeedItem(newTestCard(userId, agentId, "A second card"), start.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.MarkAgentFeedItemsRead(userId, []string{first.ID}, start.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := tx.SetAgentFeedItemLiked(first.ID, true, start.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}

		sunny := newTestCard(userId, agentId, "Sunny in Springfield")
		sunny.CoalesceKey = "weather"
		replaced, change, err := tx.UpsertAgentFeedItem(sunny, start.Add(2*time.Minute))
		if err != nil || change != models.FeedChangeReplaced {
			t.Fatalf("second post: %v %v", change, err)
		}
		if replaced.ID != first.ID || replaced.Title != "Sunny in Springfield" || replaced.ReadAt != nil || !replaced.IsLiked {
			t.Fatalf("the card was not replaced in place: %+v", replaced)
		}
		items, next, err := tx.ListAgentFeedItems(userId, nil, 10, false, start.Add(3*time.Minute))
		if err != nil || next != nil || len(items) != 2 {
			t.Fatalf("list: %d %v %v", len(items), next, err)
		}
		if items[0].ID != first.ID || items[1].ID != other.ID {
			t.Fatalf("the replaced card is not on top: %s %s", items[0].Title, items[1].Title)
		}

		// A dismissed card frees its key.
		if dismissed, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{first.ID}}, start.Add(4*time.Minute)); err != nil || len(dismissed) != 1 {
			t.Fatalf("dismiss: %v %v", dismissed, err)
		}
		again := newTestCard(userId, agentId, "Rain in Springfield")
		again.CoalesceKey = "weather"
		fresh, change, err := tx.UpsertAgentFeedItem(again, start.Add(5*time.Minute))
		if err != nil || change != models.FeedChangePosted || fresh.ID == first.ID {
			t.Fatalf("a dismissed card is not replaced: %v %v", change, err)
		}
	})
}

// The list pages newest first with a cursor, counts what is unread, leaves
// out the dismissed and the expired, and keeps one person's cards from
// another's.
func TestTheFeedPagesNewestFirstAndCountsUnread(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-pages")
		otherUserId, otherAgentId := feedTestAgent(t, tx, "feed-pages-other")
		start := time.Now()
		ids := []string{}
		for index := 0; index < 5; index++ {
			card, _, err := tx.UpsertAgentFeedItem(newTestCard(userId, agentId, fmt.Sprintf("Card %d", index)), start.Add(time.Duration(index)*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, card.ID)
		}
		if _, _, err := tx.UpsertAgentFeedItem(newTestCard(otherUserId, otherAgentId, "Somebody else's"), start); err != nil {
			t.Fatal(err)
		}
		now := start.Add(10 * time.Minute)

		first, next, err := tx.ListAgentFeedItems(userId, nil, 2, false, now)
		if err != nil || len(first) != 2 || next == nil || first[0].ID != ids[4] || first[1].ID != ids[3] {
			t.Fatalf("first page: %v %v %v", first, next, err)
		}
		cursor, err := db.DecodeFeedCursor(db.EncodeFeedCursor(next))
		if err != nil {
			t.Fatal(err)
		}
		second, next, err := tx.ListAgentFeedItems(userId, cursor, 2, false, now)
		if err != nil || len(second) != 2 || next == nil || second[0].ID != ids[2] || second[1].ID != ids[1] {
			t.Fatalf("second page: %v %v %v", second, next, err)
		}
		third, next, err := tx.ListAgentFeedItems(userId, next, 2, false, now)
		if err != nil || len(third) != 1 || next != nil || third[0].ID != ids[0] {
			t.Fatalf("third page: %v %v %v", third, next, err)
		}

		if unread, err := tx.CountUnreadAgentFeedItems(userId, now); err != nil || unread != 5 {
			t.Fatalf("unread: %d %v", unread, err)
		}
		if readCount, err := tx.MarkAgentFeedItemsRead(userId, []string{ids[0], ids[1]}, now); err != nil || readCount != 2 {
			t.Fatalf("read: %d %v", readCount, err)
		}
		if readCount, _ := tx.MarkAgentFeedItemsRead(userId, []string{ids[0]}, now); readCount != 0 {
			t.Fatalf("a card is newly read once: %d", readCount)
		}
		if _, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{ids[4]}}, now); err != nil {
			t.Fatal(err)
		}
		if unread, _ := tx.CountUnreadAgentFeedItems(userId, now); unread != 2 {
			t.Fatalf("unread after reading two and dismissing one: %d", unread)
		}
		visible, _, _ := tx.ListAgentFeedItems(userId, nil, 10, false, now)
		withDismissed, _, _ := tx.ListAgentFeedItems(userId, nil, 10, true, now)
		if len(visible) != 4 || len(withDismissed) != 5 {
			t.Fatalf("dismissed cards: %d %d", len(visible), len(withDismissed))
		}
		// Thirty days on, the news has dropped out of the list.
		later := now.AddDate(0, 0, models.FeedExpiryDays+1)
		if expired, _, _ := tx.ListAgentFeedItems(userId, nil, 10, true, later); len(expired) != 0 {
			t.Fatalf("expired cards are listed: %d", len(expired))
		}
		if deleted, err := tx.ExpireAgentFeedItems(later); err != nil || deleted != 6 {
			t.Fatalf("expire: %d %v", deleted, err)
		}
	})
}

// A decision is settled once, never expires while it waits, and keeps what
// was chosen; the outcome line arrives later.
func TestADecisionIsSettledOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-settle")
		start := time.Now()
		decision := newTestCard(userId, agentId, "Was this sign-in you?")
		decision.FeedItemKind = models.FeedItemDecision
		decision.Choices = []models.FeedChoice{
			{ChoiceKey: "me", ChoiceLabel: "That's me", ChoiceRole: models.FeedChoiceDismiss},
			{ChoiceKey: "secure", ChoiceLabel: "Secure my account", ChoiceRole: models.FeedChoiceAction},
		}
		card, _, err := tx.UpsertAgentFeedItem(decision, start)
		if err != nil {
			t.Fatal(err)
		}
		if card.ExpiresAt != nil {
			t.Fatalf("an unanswered decision expires: %v", card.ExpiresAt)
		}
		isSettled, err := tx.SettleAgentFeedItem(card.ID, &models.FeedSettlement{ChoiceKey: "secure", ConversationID: "main-1", IsOutcomePending: true}, start.Add(time.Minute))
		if err != nil || !isSettled {
			t.Fatalf("settle: %v %v", isSettled, err)
		}
		if isSettled, _ := tx.SettleAgentFeedItem(card.ID, &models.FeedSettlement{ChoiceKey: "me"}, start.Add(2*time.Minute)); isSettled {
			t.Fatal("a second choice settled the card again")
		}
		if err := tx.SetAgentFeedItemOutcome(card.ID, "Signed out the other session.", start.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		settled, err := tx.GetAgentFeedItem(card.ID)
		if err != nil || settled == nil {
			t.Fatal(err)
		}
		if settled.ChosenChoiceKey != "secure" || settled.ChosenConversationID != "main-1" || settled.IsOutcomePending ||
			settled.OutcomeText != "Signed out the other session." || settled.ReadAt == nil || settled.ExpiresAt == nil {
			t.Fatalf("settled card: %+v", settled)
		}
	})
}

// A page already linked by an open card of the last thirty days is found,
// and one only a dismissed card linked is not.
func TestASourceURLIsFoundInAnOpenCard(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-source")
		start := time.Now()
		news := newTestCard(userId, agentId, "A new warehouse robot")
		news.SourceLinks = []models.FeedSourceLink{{SourceTitle: "The article", SourceURL: "https://news.example.com/robot"}}
		card, _, err := tx.UpsertAgentFeedItem(news, start)
		if err != nil {
			t.Fatal(err)
		}
		since := start.AddDate(0, 0, -models.FeedSourceURLDays)
		if found, err := tx.FindOpenAgentFeedItemBySourceURL(agentId, "https://news.example.com/robot", since); err != nil || found == nil || found.ID != card.ID {
			t.Fatalf("the open card: %v %v", found, err)
		}
		if found, _ := tx.FindOpenAgentFeedItemBySourceURL(agentId, "https://news.example.com/other", since); found != nil {
			t.Fatal("another page was found")
		}
		if _, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{card.ID}}, start); err != nil {
			t.Fatal(err)
		}
		if found, _ := tx.FindOpenAgentFeedItemBySourceURL(agentId, "https://news.example.com/robot", since); found != nil {
			t.Fatal("a dismissed card was found")
		}
	})
}

func TestTheFeedCursorOfNothingIsNoCursor(t *testing.T) {
	if cursor, err := db.DecodeFeedCursor(""); cursor != nil || err != nil {
		t.Fatalf("empty: %v %v", cursor, err)
	}
	if _, err := db.DecodeFeedCursor("not a cursor"); err == nil {
		t.Fatal("a cursor that is not one was read")
	}
}

// A dismissal takes the named cards, a stack's, or every read card but a
// decision still waiting; one already closed is left out, another person's
// card is never touched, and a restore brings back what was dismissed except
// what a choice settled.
func TestDismissingCardsByIdsStackAndReadAndRestoringThem(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-dismiss")
		otherUserId, otherAgentId := feedTestAgent(t, tx, "feed-dismiss-other")
		start := time.Now()
		post := func(title, stackKey string) *models.AgentFeedItem {
			card := newTestCard(userId, agentId, title)
			card.FeedStackKey = stackKey
			stored, _, err := tx.UpsertAgentFeedItem(card, start)
			if err != nil {
				t.Fatal(err)
			}
			return stored
		}
		first, second, third := post("First", ""), post("Second", ""), post("Third", "")
		stackedOne, stackedTwo := post("Stacked one", "morning-brief"), post("Stacked two", "morning-brief")
		decisionCard := newTestCard(userId, agentId, "Was this sign-in you?")
		decisionCard.FeedItemKind = models.FeedItemDecision
		decisionCard.Choices = []models.FeedChoice{{ChoiceKey: "secure", ChoiceLabel: "Secure my account", ChoiceRole: models.FeedChoiceAction}}
		decision, _, err := tx.UpsertAgentFeedItem(decisionCard, start)
		if err != nil {
			t.Fatal(err)
		}
		// The agent may put choices on any kind of card: an alert that waits
		// for its answer is kept by every read card as a decision is.
		alertCard := newTestCard(userId, agentId, "A new device signed in")
		alertCard.FeedItemKind = models.FeedItemAlert
		alertCard.Choices = []models.FeedChoice{{ChoiceKey: "me", ChoiceLabel: "That was me", ChoiceRole: models.FeedChoiceDismiss}}
		waitingAlert, _, err := tx.UpsertAgentFeedItem(alertCard, start)
		if err != nil {
			t.Fatal(err)
		}
		strangers, _, err := tx.UpsertAgentFeedItem(newTestCard(otherUserId, otherAgentId, "Somebody else's"), start)
		if err != nil {
			t.Fatal(err)
		}
		now := start.Add(time.Minute)
		idsOf := func(items []*models.AgentFeedItem) map[string]bool {
			ids := map[string]bool{}
			for _, item := range items {
				ids[item.ID] = true
			}
			return ids
		}

		// By ids: the other person's card is not touched, and a card closed
		// before is left out the second time.
		dismissed, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{first.ID, strangers.ID}}, now)
		if ids := idsOf(dismissed); err != nil || len(dismissed) != 1 || !ids[first.ID] || dismissed[0].DismissedAt == nil || dismissed[0].ReadAt == nil {
			t.Fatalf("by ids: %+v %v", dismissed, err)
		}
		if again, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{first.ID}}, now); err != nil || len(again) != 0 {
			t.Fatalf("a dismissed card is dismissed again: %+v %v", again, err)
		}
		if untouched, _ := tx.GetAgentFeedItem(strangers.ID); untouched.DismissedAt != nil {
			t.Fatal("another person's card was dismissed")
		}

		// By stack: both cards of the stack and nothing else.
		dismissed, err = tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedStackKey: "morning-brief"}, now)
		if ids := idsOf(dismissed); err != nil || len(dismissed) != 2 || !ids[stackedOne.ID] || !ids[stackedTwo.ID] {
			t.Fatalf("by stack: %+v %v", dismissed, err)
		}

		// Every read card: the unread card stays, so does the decision that
		// waits, though it is read.
		if _, err := tx.MarkAgentFeedItemsRead(userId, []string{second.ID, decision.ID, waitingAlert.ID}, now); err != nil {
			t.Fatal(err)
		}
		dismissed, err = tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{IsEveryReadCard: true}, now)
		if ids := idsOf(dismissed); err != nil || len(dismissed) != 1 || !ids[second.ID] {
			t.Fatalf("every read card: %+v %v", dismissed, err)
		}
		if remaining, _ := tx.GetAgentFeedItem(third.ID); remaining.DismissedAt != nil {
			t.Fatal("an unread card was dismissed with the read ones")
		}
		if remaining, _ := tx.GetAgentFeedItem(decision.ID); remaining.DismissedAt != nil {
			t.Fatal("a decision still waiting was dismissed with the read ones")
		}
		if remaining, _ := tx.GetAgentFeedItem(waitingAlert.ID); remaining.DismissedAt != nil {
			t.Fatal("an alert still waiting for its answer was dismissed with the read ones")
		}

		// Restore: a dismissed card comes back, one answered with an action
		// comes back with its answer, and an open one and another person's
		// are left out.
		if _, err := tx.SettleAgentFeedItem(decision.ID, &models.FeedSettlement{ChoiceKey: "secure"}, now); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{decision.ID}}, now); err != nil {
			t.Fatal(err)
		}
		restored, err := tx.RestoreAgentFeedItems(userId, []string{first.ID, decision.ID, third.ID, strangers.ID}, now.Add(time.Minute))
		if ids := idsOf(restored); err != nil || len(restored) != 2 || !ids[first.ID] || !ids[decision.ID] {
			t.Fatalf("restore: %+v %v", restored, err)
		}
		if settled, _ := tx.GetAgentFeedItem(decision.ID); settled.DismissedAt != nil || settled.ChosenChoiceKey != "secure" {
			t.Fatalf("a card answered with an action comes back with its answer: %+v", settled)
		}
		if brought, _ := tx.GetAgentFeedItem(first.ID); brought.DismissedAt != nil || brought.ReadAt == nil {
			t.Fatal("the restored card is still dismissed, or unread again")
		}
		if visible, _, _ := tx.ListAgentFeedItems(userId, nil, 10, false, now); len(visible) != 4 {
			t.Fatalf("the restored two, the unread and the waiting alert card are in the list: %d", len(visible))
		}

		// A card past its expiry is out of the feed: it is not dismissed,
		// and a dismissed one is not brought back.
		afterExpiry := start.Add(time.Duration(models.FeedExpiryDays+1) * 24 * time.Hour)
		if expired, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{third.ID}}, afterExpiry); err != nil || len(expired) != 0 {
			t.Fatalf("an expired card was dismissed: %+v %v", expired, err)
		}
		if expired, err := tx.RestoreAgentFeedItems(userId, []string{stackedOne.ID}, afterExpiry); err != nil || len(expired) != 0 {
			t.Fatalf("an expired card was restored: %+v %v", expired, err)
		}
	})
}

// The dismissed cards are listed on their own, the most recently dismissed
// first, every card an entry even inside a stack, and a page at a time.
func TestTheDismissedCardsAreListedMostRecentlyDismissedFirst(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-dismissed-list")
		otherUserId, otherAgentId := feedTestAgent(t, tx, "feed-dismissed-list-other")
		start := time.Now()
		post := func(userId, agentId, title, stackKey string) *models.AgentFeedItem {
			card := newTestCard(userId, agentId, title)
			card.FeedStackKey = stackKey
			stored, _, err := tx.UpsertAgentFeedItem(card, start)
			if err != nil {
				t.Fatal(err)
			}
			return stored
		}
		alone, stackedOne, stackedTwo := post(userId, agentId, "Alone", ""), post(userId, agentId, "Stacked one", "garden"), post(userId, agentId, "Stacked two", "garden")
		post(userId, agentId, "Still open", "")
		strangers := post(otherUserId, otherAgentId, "Somebody else's", "")
		for index, card := range []*models.AgentFeedItem{stackedOne, alone, stackedTwo} {
			if _, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{card.ID}}, start.Add(time.Duration(index+1)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.DismissAgentFeedItems(otherUserId, &db.FeedDismissSelection{FeedItemIDs: []string{strangers.ID}}, start.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		now := start.Add(time.Hour)
		page, next, err := tx.ListDismissedAgentFeedItems(userId, nil, 2, now)
		if err != nil || len(page) != 2 || page[0].ID != stackedTwo.ID || page[1].ID != alone.ID || next == nil {
			t.Fatalf("first page: %+v %+v %v", page, next, err)
		}
		rest, next, err := tx.ListDismissedAgentFeedItems(userId, next, 2, now)
		if err != nil || len(rest) != 1 || rest[0].ID != stackedOne.ID || next != nil {
			t.Fatalf("second page: %+v %+v %v", rest, next, err)
		}
		// The cursor goes through its text, as a client hands it back.
		decoded, err := db.DecodeFeedCursor(db.EncodeFeedCursor(&models.FeedCursor{OrderedAt: *page[1].DismissedAt, ID: page[1].ID}))
		if err != nil {
			t.Fatal(err)
		}
		if again, _, _ := tx.ListDismissedAgentFeedItems(userId, decoded, 2, now); len(again) != 1 || again[0].ID != stackedOne.ID {
			t.Fatalf("a page from a decoded cursor: %+v", again)
		}
		// Expired cards are not among them.
		afterExpiry := start.Add(time.Duration(models.FeedExpiryDays+1) * 24 * time.Hour)
		if expired, _, _ := tx.ListDismissedAgentFeedItems(userId, nil, 10, afterExpiry); len(expired) != 0 {
			t.Fatalf("expired cards are listed: %d", len(expired))
		}
	})
}

// A card answered "not now" comes back waiting for its answer, a card whose
// subject an open card holds stays where it is, of two dismissed cards on
// one subject the one dismissed last comes back, and a card answered a day
// ago is swept to the dismissed cards.
func TestRestoringRespectsAnswersAndSubjectsAndAnsweredCardsAreSwept(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId, agentId := feedTestAgent(t, tx, "feed-restore-rules")
		start := time.Now()
		postKeyed := func(title, coalesceKey string, choices []models.FeedChoice) *models.AgentFeedItem {
			card := newTestCard(userId, agentId, title)
			card.CoalesceKey, card.Choices = coalesceKey, choices
			if len(choices) > 0 {
				card.FeedItemKind = models.FeedItemDecision
			}
			stored, _, err := tx.UpsertAgentFeedItem(card, start)
			if err != nil {
				t.Fatal(err)
			}
			return stored
		}
		dismiss := func(card *models.AgentFeedItem, at time.Time) {
			if _, err := tx.DismissAgentFeedItems(userId, &db.FeedDismissSelection{FeedItemIDs: []string{card.ID}}, at); err != nil {
				t.Fatal(err)
			}
		}
		notNow := postKeyed("Try the new route?", "", []models.FeedChoice{
			{ChoiceKey: "do_it", ChoiceLabel: "Do it", ChoiceRole: models.FeedChoiceAction},
			{ChoiceKey: "not_now", ChoiceLabel: "Not now", ChoiceRole: models.FeedChoiceDismiss},
		})
		if isSettled, err := tx.SettleAgentFeedItem(notNow.ID, &models.FeedSettlement{ChoiceKey: "not_now"}, start); err != nil || !isSettled {
			t.Fatal(isSettled, err)
		}
		dismiss(notNow, start.Add(time.Minute))

		// A subject an open card holds: the old card stays dismissed.
		oldWeather := postKeyed("Rain this afternoon", "weather:today", nil)
		dismiss(oldWeather, start.Add(time.Minute))
		postKeyed("Sun this afternoon", "weather:today", nil)

		// Two dismissed cards on one subject: the later dismissal wins.
		earlierParcel := postKeyed("Parcel on its way", "parcel:one", nil)
		dismiss(earlierParcel, start.Add(time.Minute))
		laterParcel := postKeyed("Parcel out for delivery", "parcel:one", nil)
		dismiss(laterParcel, start.Add(2*time.Minute))

		now := start.Add(time.Hour)
		restored, err := tx.RestoreAgentFeedItems(userId, []string{notNow.ID, oldWeather.ID, earlierParcel.ID, laterParcel.ID}, now)
		if err != nil {
			t.Fatal(err)
		}
		restoredIds := map[string]bool{}
		for _, item := range restored {
			restoredIds[item.ID] = true
		}
		if len(restored) != 2 || !restoredIds[notNow.ID] || !restoredIds[laterParcel.ID] {
			t.Fatalf("restored: %+v", restored)
		}
		if waiting, _ := tx.GetAgentFeedItem(notNow.ID); waiting.ChosenChoiceKey != "" || waiting.ChosenAt != nil {
			t.Fatalf("a card answered not now is waiting again: %+v", waiting)
		}
		if still, _ := tx.GetAgentFeedItem(oldWeather.ID); still.DismissedAt == nil {
			t.Fatal("a card whose subject an open card holds was restored")
		}

		// The sweep: answered more than a day ago leaves the feed, answered
		// an hour ago stays.
		answeredLong := postKeyed("Book the table?", "", []models.FeedChoice{{ChoiceKey: "book", ChoiceLabel: "Book it", ChoiceRole: models.FeedChoiceAction}})
		answeredRecently := postKeyed("Order more tea?", "", []models.FeedChoice{{ChoiceKey: "order", ChoiceLabel: "Order it", ChoiceRole: models.FeedChoiceAction}})
		if _, err := tx.SettleAgentFeedItem(answeredLong.ID, &models.FeedSettlement{ChoiceKey: "book"}, now.Add(-25*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SettleAgentFeedItem(answeredRecently.ID, &models.FeedSettlement{ChoiceKey: "order"}, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		swept, err := tx.DismissAgentFeedItemsAnsweredBefore(now.Add(-models.FeedAnsweredLifetime), now)
		if err != nil || len(swept) != 1 || swept[0].ID != answeredLong.ID || swept[0].DismissedAt == nil {
			t.Fatalf("swept: %+v %v", swept, err)
		}

		// A withdrawn card is gone, not dismissed.
		if err := tx.DeleteAgentFeedItem(answeredRecently.ID); err != nil {
			t.Fatal(err)
		}
		if gone, err := tx.GetAgentFeedItem(answeredRecently.ID); err != nil || gone != nil {
			t.Fatalf("a deleted card is still there: %+v %v", gone, err)
		}
	})
}
