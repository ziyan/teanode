package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// fileRoutine puts invented routine messages in the inbox, as the sorting
// leaves them: nothing to answer and nothing urgent.
func (self *alertFixture) fileRoutine(t *testing.T, count int) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		for index := 0; index < count; index++ {
			mail, err := tx.CreateMail(&models.Mail{Subject: "Weekly offers", From: "offers@shop.example.com", Sender: "offers@shop.example.com", Kind: models.MailKindIncoming, ReceivedAt: time.Now()}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if _, err := tx.AddItem(self.inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
		}
	})
}

// The weekly look at the mail is due only when the Inbox holds enough routine
// mail, only once a week, and only for an agent whose unasked words go to the
// feed.
func TestTheMailOrganizeLookIsDueOnlyAboveTheThresholdAndOnlyWeekly(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	now := time.Now()
	onboarded := now.Add(-48 * time.Hour)
	reason := fixture.worker.mailOrganizeReason()
	isDue := func() bool {
		var due bool
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			var err error
			fixture.agent, err = tx.GetAgent(fixture.agent.ID)
			if err != nil {
				t.Fatal(err)
			}
			if due, err = reason.isDue(t.Context(), tx, fixture.agent, fixture.owner, time.Hour, now); err != nil {
				t.Fatalf("isDue: %s", err)
			}
		})
		return due
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(fixture.agent.ID, func(found *models.Agent) error {
			found.FeedDestination = models.FeedDestinationFeed
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.MarkAgentSpokeFirst(fixture.agent.ID, nil, &onboarded, nil); err != nil {
			t.Fatal(err)
		}
	})

	fixture.fileRoutine(t, mailOrganizePileLeast-1)
	if isDue() {
		t.Fatalf("%d routine messages are not a pile", mailOrganizePileLeast-1)
	}
	fixture.fileRoutine(t, 1)
	if !isDue() {
		t.Fatalf("%d routine messages are a pile", mailOrganizePileLeast)
	}

	// A look this week keeps the next for the next.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := fixture.worker.Enqueue(tx, models.AgentJobSpeakFirst, fixture.agent.ID, "", SpeakFirstMailOrganize); err != nil {
			t.Fatal(err)
		}
	})
	if isDue() {
		t.Fatal("a second look in the same week")
	}

	// Mail the sorting says needs an answer is not routine.
	fixture = newAlertFixtureWith(t, "", "UTC")
	fixture.fileRoutine(t, mailOrganizePileLeast-1)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		flagged := true
		mail, _ := tx.CreateMail(&models.Mail{Subject: "Can you confirm?", From: "friend@example.net", Sender: "friend@example.net", Kind: models.MailKindIncoming, ReceivedAt: time.Now()}, nil)
		if _, err := tx.AddItem(fixture.inbox.ID, mail.ID, "", models.MailboxItemFlags{Flagged: &flagged}); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		count, err := routineInboxCount(tx, fixture.agent, fixture.owner, now)
		if err != nil || count != mailOrganizePileLeast-1 {
			t.Fatalf("a flagged message is not routine: %d %v", count, err)
		}
	})
}

// What the weekly look is told: read, change nothing, and one decision card
// with the three buttons and a pile's facts.
func TestTheMailOrganizeLookIsToldToChangeNothingAndOfferOneCard(t *testing.T) {
	lines := mailOrganizeLines
	for _, wanted := range []string{"Change nothing", "ONE card", "decision", "tray.full", "mail-organize", "Approve", "Discuss", "Not now", "fact_rows", "Approve: <the title>"} {
		if !strings.Contains(lines, wanted) {
			t.Errorf("the look is not told %q", wanted)
		}
	}
}
