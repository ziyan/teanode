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
		if isDismissed, err := tx.DismissAgentFeedItem(first.ID, start.Add(4*time.Minute)); err != nil || !isDismissed {
			t.Fatalf("dismiss: %v %v", isDismissed, err)
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
		if _, err := tx.DismissAgentFeedItem(ids[4], now); err != nil {
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
		if _, err := tx.DismissAgentFeedItem(card.ID, start); err != nil {
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
