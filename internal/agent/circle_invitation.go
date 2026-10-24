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
// The phones hang from the person's agent row whether or not the agent is on,
// and the notification goes through the phone notifier chats use, which needs
// no worker, so an invitation reaches a person whose agent is off or whose
// server has no model just the same. Only the note needs the agent on.
//
// The note is written in the transaction that stores the invitation, so there
// is never one without the other. The notification is sent after it commits.

// NoteCircleInvitation writes the note into the invited person's main
// conversation when their agent is on, and says whose agent row holds their
// phones, or "" when they have none. The note is not a turn: nothing is asked
// of the model, and the phone is told by NotifyCircleInvitation instead of by
// the loop that sends what the agent says on its own.
func NoteCircleInvitation(tx db.Transaction, invitedUserId, invitedByName, circleName string) (string, error) {
	person, err := tx.GetAgentByUser(invitedUserId)
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
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(main.ID, models.NoteCircleInvited, models.CircleInvitedDetail(invitedByName, circleName))); err != nil {
		return "", err
	}
	return person.ID, nil
}

// The words of the notifications about circles.
var (
	phraseInvitation        = push.Phrase{English: "Invitation", Chinese: "邀请", Japanese: "招待"}
	phraseInvitedYou        = push.Phrase{English: "%s invited you to %s", Chinese: "%s 邀请你加入「%s」", Japanese: "%s さんから「%s」に招待されました"}
	phraseCircles           = push.Phrase{English: "Circles", Chinese: "圈子", Japanese: "サークル"}
	phraseCircleMemberAdded = push.Phrase{
		English:  "An operator of this server put %s in %s, which you administer.",
		Chinese:  "此服务器的运维者把 %s 加入了你管理的「%s」。",
		Japanese: "このサーバーの運用者が %s さんを、あなたが管理する「%s」に追加しました。",
	}
)

// NotifyCircleInvitation tells the phones of an agent's person about an
// invitation, in the background, in their language.
func (self *PhoneNotifier) NotifyCircleInvitation(agentId, invitedByName, circleName string) {
	self.NotifyPhones(agentId, models.NotificationCircle, &push.Content{
		NotificationKind: models.NotificationCircle,
		Title:            phraseInvitation.English,
		Body:             PhoneBody(phraseInvitedYou.In(push.LanguageEnglish, invitedByName, circleName)),
		ThreadID:         "circle-invitations",
		Wording: func(content *push.Content, language string) {
			content.Title = phraseInvitation.In(language)
			content.Body = PhoneBody(phraseInvitedYou.In(language, invitedByName, circleName))
		},
	})
}

// When an operator puts an account in a circle, with no invitation for the
// account to answer, the circle's administrators are told the way an
// invitation is told: a note in the main conversation of their agent when it
// is on, written in the same transaction, and a notification on their phones
// after it commits.

// NoteCircleMemberAdded writes the note into an administrator's main
// conversation when their agent is on, and says whose agent row holds their
// phones, or "" when they have none.
func NoteCircleMemberAdded(tx db.Transaction, administratorUserId, addedName, circleName string) (string, error) {
	person, err := tx.GetAgentByUser(administratorUserId)
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
	if _, err := tx.AppendAgentMessage(models.NewAgentNote(main.ID, models.NoteCircleMemberAdded, models.CircleMemberAddedDetail(addedName, circleName))); err != nil {
		return "", err
	}
	return person.ID, nil
}

// NotifyCircleMemberAdded tells the phones of an agent's person that an
// account was put in a circle they administer, in the background, in their
// language.
func (self *PhoneNotifier) NotifyCircleMemberAdded(agentId, addedName, circleName string) {
	self.NotifyPhones(agentId, models.NotificationCircle, &push.Content{
		NotificationKind: models.NotificationCircle,
		Title:            phraseCircles.English,
		Body:             PhoneBody(phraseCircleMemberAdded.In(push.LanguageEnglish, addedName, circleName)),
		ThreadID:         "circle-members",
		Wording: func(content *push.Content, language string) {
			content.Title = phraseCircles.In(language)
			content.Body = PhoneBody(phraseCircleMemberAdded.In(language, addedName, circleName))
		},
	})
}
