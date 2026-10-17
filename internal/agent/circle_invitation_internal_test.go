package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An invitation is a note for a person whose agent is on, and nothing for one
// whose agent is off or who has none, so no conversation is made for them.
func TestAnInvitationIsNotedOnlyForAnAgentThatIsOn(t *testing.T) {
	fixture := newPhoneFixture(t)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		agentId, err := NoteCircleInvitation(tx, fixture.owner.ID, "Maria", "Home")
		if err != nil || agentId != fixture.agent.ID {
			t.Fatalf("NoteCircleInvitation: %q %v", agentId, err)
		}
		main, err := tx.MainAgentConversation(fixture.agent.ID, "drawer")
		if err != nil {
			t.Fatal(err)
		}
		messages, err := tx.ListAgentMessages(main.ID, nil)
		if err != nil || len(messages) != 1 || messages[0].Name != string(models.NoteCircleInvited) {
			t.Fatalf("the main conversation holds %+v, %v", messages, err)
		}
		if said := models.NoteText(models.NoteCircleInvited, messages[0].Content); said != "Maria invited you to Home" {
			t.Errorf("the note says %q", said)
		}

		stranger, err := tx.CreateUser(&models.User{Username: "no-agent"})
		if err != nil {
			t.Fatal(err)
		}
		if agentId, err := NoteCircleInvitation(tx, stranger.ID, "Maria", "Home"); err != nil || agentId != "" {
			t.Errorf("a person with no agent: %q %v", agentId, err)
		}

		if _, err := tx.UpdateAgent(fixture.agent.ID, func(agent *models.Agent) error {
			agent.Enabled = false
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if agentId, err := NoteCircleInvitation(tx, fixture.owner.ID, "Maria", "Home"); err != nil || agentId != "" {
			t.Errorf("a person whose agent is off: %q %v", agentId, err)
		}
	})
}

// The phone is told who invited the person and to what, as a notification of
// its own kind that a phone can choose not to take.
func TestAPhoneIsToldOfAnInvitation(t *testing.T) {
	fixture := newPhoneFixture(t)
	fixture.worker.NotifyCircleInvitation(fixture.agent.ID, "Maria", "Home")
	fixture.worker.waitGroup.Wait()

	sent := fixture.relay.to("sealed:a-registration")
	if len(sent) != 1 {
		t.Fatalf("the phone was sent %d notifications, not one", len(sent))
	}
	content := fixture.read(t, sent[0])
	if content.NotificationKind != models.NotificationCircle || content.Title != "Invitation" || content.Body != "Maria invited you to Home" {
		t.Errorf("the phone was told %+v", content)
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.UpdateAgentPhoneSettings(fixture.agent.ID, fixture.phone.ID, "A test phone", []string{models.NotificationApproval}); err != nil {
			t.Fatalf("UpdateAgentPhoneSettings: %s", err)
		}
	})
	fixture.worker.NotifyCircleInvitation(fixture.agent.ID, "Maria", "Home")
	fixture.worker.waitGroup.Wait()
	if len(fixture.relay.to("sealed:a-registration")) != 1 {
		t.Error("a phone that chose only approvals was told of an invitation")
	}
}
