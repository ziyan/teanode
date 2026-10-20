package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A preview is saved per address, and saving the same address again replaces
// the row and says which picture file the old row named, so that the caller
// removes only the file nobody names any more.
func TestSavingALinkPreviewReplacesTheRowAndNamesTheOldPicture(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		replaced, err := tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair", Description: "A chair.", SiteName: "Example Shop",
			ImageFileID: "link-preview-first", ImageSizeBytes: 9000, ImageWidthPixels: 600, ImageHeightPixels: 400, FetchedAt: now,
		})
		if err != nil || replaced != "" {
			t.Fatalf("the first save replaced %q: %v", replaced, err)
		}

		replaced, err = tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair, blue", ImageFileID: "link-preview-second", FetchedAt: now,
		})
		if err != nil || replaced != "link-preview-first" {
			t.Fatalf("the second save replaced %q (%v), want the first picture", replaced, err)
		}

		replaced, err = tx.SaveLinkPreview(&db.LinkPreview{
			URL: "https://example.com/garden-chair", Title: "Garden Chair, blue", ImageFileID: "link-preview-second", FetchedAt: now,
		})
		if err != nil || replaced != "" {
			t.Fatalf("saving with the same picture replaced %q: %v", replaced, err)
		}

		previews, err := tx.ListLinkPreviews([]string{"https://example.com/garden-chair", "https://example.com/never-fetched"})
		if err != nil {
			t.Fatalf("ListLinkPreviews: %s", err)
		}
		if len(previews) != 1 {
			t.Fatalf("ListLinkPreviews returned %d rows, want 1", len(previews))
		}
		found := previews["https://example.com/garden-chair"]
		if found == nil || found.Title != "Garden Chair, blue" || found.Description != "" || found.ImageWidthPixels != 0 {
			t.Errorf("the row is %+v: the second save did not replace the first", found)
		}
	})
}

func TestALinkPreviewIsFoundByItsPictureFile(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, preview := range []*db.LinkPreview{
			{URL: "https://example.com/with-picture", ImageFileID: "link-preview-one", FetchedAt: time.Now()},
			{URL: "https://example.com/without-picture", FetchedAt: time.Now()},
			{URL: "https://example.com/also-without-picture", FetchedAt: time.Now()},
		} {
			if _, err := tx.SaveLinkPreview(preview); err != nil {
				t.Fatalf("SaveLinkPreview: %s", err)
			}
		}
		found, err := tx.GetLinkPreviewByImageFile("link-preview-one")
		if err != nil || found == nil || found.URL != "https://example.com/with-picture" {
			t.Errorf("GetLinkPreviewByImageFile = %+v, %v", found, err)
		}
		if found, err := tx.GetLinkPreviewByImageFile("link-preview-missing"); err != nil || found != nil {
			t.Errorf("GetLinkPreviewByImageFile of a name nobody has = %+v, %v", found, err)
		}
		// Rows without a picture do not collide on the empty name, and the
		// empty name finds none of them.
		if found, err := tx.GetLinkPreviewByImageFile(""); err != nil || found != nil {
			t.Errorf("GetLinkPreviewByImageFile of the empty name = %+v, %v", found, err)
		}
	})
}

func TestOldLinkPreviewsAreListedOldestFirstAndDeleted(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		for address, age := range map[string]time.Duration{
			"https://example.com/old":   90 * 24 * time.Hour,
			"https://example.com/older": 120 * 24 * time.Hour,
			"https://example.com/fresh": time.Hour,
		} {
			if _, err := tx.SaveLinkPreview(&db.LinkPreview{URL: address, FetchedAt: now.Add(-age)}); err != nil {
				t.Fatalf("SaveLinkPreview: %s", err)
			}
		}
		old, err := tx.ListLinkPreviewsFetchedBefore(now.Add(-30*24*time.Hour), 10)
		if err != nil {
			t.Fatalf("ListLinkPreviewsFetchedBefore: %s", err)
		}
		if len(old) != 2 || old[0].URL != "https://example.com/older" || old[1].URL != "https://example.com/old" {
			t.Fatalf("ListLinkPreviewsFetchedBefore = %+v, want older then old", old)
		}
		limited, err := tx.ListLinkPreviewsFetchedBefore(now.Add(-30*24*time.Hour), 1)
		if err != nil || len(limited) != 1 {
			t.Errorf("a limit of 1 returned %d rows: %v", len(limited), err)
		}

		if err := tx.DeleteLinkPreviews([]string{old[0].URL, old[1].URL}); err != nil {
			t.Fatalf("DeleteLinkPreviews: %s", err)
		}
		left, err := tx.ListLinkPreviews([]string{"https://example.com/old", "https://example.com/older", "https://example.com/fresh"})
		if err != nil || len(left) != 1 || left["https://example.com/fresh"] == nil {
			t.Errorf("after the delete %d rows are left (%v), want only the fresh one", len(left), err)
		}
	})
}

// A link may be fetched for a card only if it is written in a message of the
// conversation that the agent did not write: a person's, or a tool's answer.
// A reply of the agent's alone is not enough, since it can be made to carry
// what the agent read.
func TestALinkIsAllowedOnlyWhereSomebodyOtherThanTheAgentWroteIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "link-owner")
		agent, err := tx.EnsureAgent(owner.ID)
		if err != nil {
			t.Fatalf("EnsureAgent: %s", err)
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		other, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		for _, message := range []*models.AgentMessage{
			{ConversationID: conversation.ID, Role: "assistant", Content: "Look: https://example.com/collect?data=secret"},
			{ConversationID: conversation.ID, Role: "user", Content: "Is https://example.com/shop/chair?color=blue good?"},
			{ConversationID: conversation.ID, Role: "tool", Name: "web_search", ToolCallID: "call_1", Content: `[{"url":"https://example.org/guide?id=7"}]`},
			{ConversationID: other.ID, Role: "user", Content: "https://example.com/collect?data=secret"},
		} {
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
		check := func(conversation *models.AgentConversation, want bool, texts ...string) {
			t.Helper()
			got, err := tx.HasLinkNotWrittenByAgent(conversation, texts...)
			if err != nil || got != want {
				t.Errorf("HasLinkNotWrittenByAgent(%v) = %v, %v; want %v", texts, got, err, want)
			}
		}
		check(conversation, false, "https://example.com/collect?data=secret")
		check(conversation, true, "https://example.com/shop/chair?color=blue")
		check(conversation, true, "https://example.org/guide?id=7")
		// The cleaned form is tried too, and either is enough.
		check(conversation, true, "https://example.com/shop/chair?color=blue&gclid=1", "https://example.com/shop/chair?color=blue")
		// A prefix of what was written is not what was written: a page the
		// agent read may plant the start of a link and the agent add the rest.
		check(conversation, false, "https://example.com/shop/chair?color=blue&data=secret")
		check(conversation, false, "https://example.org/guide?id=7&data=secret")
		check(conversation, false, "")
		// Another conversation's messages are not this one's.
		check(conversation, false, "https://example.com/collect?data=secret")
		check(other, true, "https://example.com/collect?data=secret")
	})
}

func TestALinkInAChatIsAllowedOnlyWhereAPersonWroteIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "link-chat-alice")
		bob := newCircleTestUser(t, tx, "link-chat-bob")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}
		agent, err := tx.GetAgentByUser(bob.ID)
		if err != nil || agent == nil {
			t.Fatalf("GetAgentByUser: %v %v", agent, err)
		}
		if _, err := tx.AppendChatMessage(&models.ChatMessage{
			ChatID: chat.ID, AuthorUserID: bob.ID, AuthorName: "Agent", AuthorKind: models.ChatAuthorAgent, AuthorAgentID: agent.ID,
			Body: "See https://example.com/collect?data=secret",
		}); err != nil {
			t.Fatalf("AppendChatMessage: %s", err)
		}
		say(t, tx, chat.ID, alice, "I like https://example.com/shop/chair?color=blue")
		deleted := say(t, tx, chat.ID, alice, "https://example.com/deleted?x=1")
		if _, err := tx.DeleteChatMessage(deleted.ID); err != nil {
			t.Fatalf("DeleteChatMessage: %s", err)
		}

		conversation := conversationOf(t, tx, alice, chat.ID)
		for link, want := range map[string]bool{
			"https://example.com/collect?data=secret":   false,
			"https://example.com/shop/chair?color=blue": true,
			"https://example.com/deleted?x=1":           false,
		} {
			got, err := tx.HasLinkNotWrittenByAgent(conversation, link)
			if err != nil || got != want {
				t.Errorf("HasLinkNotWrittenByAgent(%q) = %v, %v; want %v", link, got, err, want)
			}
		}
	})
}
