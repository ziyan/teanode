package agent

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// When somebody shares something with a person, or widens what they shared,
// the person is told the way an invitation to a circle is told: a note in the
// main conversation of their agent when it is on, written in the transaction
// that made the share, and a notification on their phones of kind circle after
// it commits. The same two tell an owner that somebody joined a circle their
// shares reach. Without them a share only shows the day the person opens the
// screen it appears on.

// NoteSharing writes the note into the person's main conversation when their
// agent is on, and says whose agent row holds their phones, or "" when they
// have none. noteKind is NoteShareReceived or NoteShareReached.
func NoteSharing(tx db.Transaction, toldUserId string, noteKind models.AgentNoteKind, detail string) (string, error) {
	person, err := tx.GetAgentByUser(toldUserId)
	if err != nil || person == nil {
		return "", err
	}
	if !person.Active() {
		return person.ID, nil
	}
	main, err := tx.MainAgentConversation(person.ID, "drawer")
	if err != nil {
		return "", err
	}
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(main.ID, noteKind, detail)); err != nil {
		return "", err
	}
	return person.ID, nil
}

// NotifySharing tells the phones of an agent's person what the note says, in
// the background, in English for the reason NotifyCircleInvitation gives.
func (self *PhoneNotifier) NotifySharing(agentId string, noteKind models.AgentNoteKind, detail string) {
	self.NotifyPhones(agentId, models.NotificationCircle, &push.Content{
		NotificationKind: models.NotificationCircle,
		Title:            "Sharing",
		Body:             PhoneBody(models.NoteText(noteKind, detail)),
		ThreadID:         "circle-sharing",
	})
}
