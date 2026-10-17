package agent

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// An invitation to a circle reaches the invited person in two places besides
// the Circles page: as a note in the main conversation of their agent, when
// it is on, which is where they would look for what happened while they were
// away, and as a notification on their phones.
//
// The note is written in the transaction that stores the invitation, so there
// is never one without the other. The notification is sent after it commits.

// NoteCircleInvitation writes the note into the invited person's main
// conversation and says whose agent it was written for, or "" when they have
// none that is on. The note is not a turn: nothing is asked of the model, and
// the phone is told by NotifyCircleInvitation instead of by the loop that
// sends what the agent says on its own.
func NoteCircleInvitation(tx db.Transaction, invitedUserId, invitedByName, circleName string) (string, error) {
	person, err := tx.GetAgentByUser(invitedUserId)
	if err != nil || !person.Active() {
		return "", err
	}
	main, err := tx.MainAgentConversation(person.ID, "drawer")
	if err != nil {
		return "", err
	}
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(main.ID, models.NoteCircleInvited, models.CircleInvitedDetail(invitedByName, circleName))); err != nil {
		return "", err
	}
	return person.ID, nil
}

// NotifyCircleInvitation tells the phones of an agent's person about an
// invitation, in the background. The title and body are the same in every
// language: the notification is plain text sent to a phone that decrypts it,
// and the phone has no catalog to word it from.
func (self *Agent) NotifyCircleInvitation(agentId, invitedByName, circleName string) {
	self.notifyPhones(agentId, models.NotificationCircle, &push.Content{
		NotificationKind: models.NotificationCircle,
		Title:            "Invitation",
		Body:             phoneBody(invitedByName + " invited you to " + circleName),
		ThreadID:         "circle-invitations",
	})
}
