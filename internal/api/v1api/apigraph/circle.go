package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Circles: named sets of people on this server who know each other. A person
// sees other people only through the circles they share, and everything this
// server shares between people is shared with a circle or with one person in
// one. docs/subsystems/circles.md says what a circle is and who sees whom.
//
// Every operation here needs circle:use, and a circle the caller is not in
// is not found, as is every other refusal about a row the caller may not
// know exists. The one exception is a member who is not an admin: they can
// see the circle, so they are told in words that only an admin can do that.

// CircleQuery reads the caller's circles.
type CircleQuery interface {
	// The circles the caller is in, by name, each with its members. The
	// invitations still waiting for an answer are filled in for the circles
	// the caller is an admin of. Needs circle:use.
	ListCircles(ctx context.Context) ([]*Circle, error)

	// One circle the caller is in. Needs circle:use.
	ReadCircle(ctx context.Context, arguments ReadCircleArguments) (*Circle, error)

	// The invitations to the caller that can still be answered, the newest
	// first. Needs circle:use.
	ListCircleInvitations(ctx context.Context) ([]*CircleInvitation, error)

	// Everybody the caller shares any circle with, once each: the people a
	// chat or a share can be offered to, with what each is to the caller.
	// Their memberRole is empty, because a role belongs to one circle. Needs
	// circle:use.
	ListCirclePeople(ctx context.Context) ([]*CircleMember, error)

	// Every word a relationship can be said with, in the order they are
	// offered: wife, father, aunt_or_uncle. Needs circle:use.
	ListRelationshipTerms(ctx context.Context) ([]*RelationshipTerm, error)
}

// CircleMutation changes circles and who is in them.
type CircleMutation interface {
	// Make a circle, with the caller as its first admin. Needs circle:use.
	CreateCircle(ctx context.Context, arguments CreateCircleArguments) (*Circle, error)

	// Change a circle's name. Needs the caller to be an admin of it.
	RenameCircle(ctx context.Context, arguments RenameCircleArguments) (*Circle, error)

	// Delete a circle, with its members and invitations. Needs the caller to
	// be an admin of it.
	DeleteCircle(ctx context.Context, arguments DeleteCircleArguments) (bool, error)

	// Invite somebody to a circle by their exact username. The answer is
	// true whether or not that username is an account, and whether or not
	// the account is already in the circle or can use circles: asking is not
	// a way to list the accounts on this server, and an invitation to
	// nobody is never stored. Needs the caller to be an admin of it.
	InviteToCircle(ctx context.Context, arguments InviteToCircleArguments) (bool, error)

	// Accept or decline an invitation to the caller. Accepting returns the
	// circle; declining returns null.
	AnswerCircleInvitation(ctx context.Context, arguments AnswerCircleInvitationArguments) (*Circle, error)

	// Take back an invitation nobody has answered. Needs the caller to be an
	// admin of its circle.
	WithdrawCircleInvitation(ctx context.Context, arguments WithdrawCircleInvitationArguments) (bool, error)

	// Make a member an admin, or an admin a member. A circle always keeps an
	// admin. Needs the caller to be an admin of it.
	SetCircleMemberRole(ctx context.Context, arguments SetCircleMemberRoleArguments) (*Circle, error)

	// Take a member out of a circle. Needs the caller to be an admin of it;
	// an admin who removes themselves leaves.
	RemoveCircleMember(ctx context.Context, arguments RemoveCircleMemberArguments) (*Circle, error)

	// Leave a circle. The last member leaving deletes it; the last admin
	// cannot leave while others remain.
	LeaveCircle(ctx context.Context, arguments LeaveCircleArguments) (bool, error)

	// Put an account in a circle directly, without an invitation, for an
	// account the operator made for somebody who will not set it up
	// themselves. Needs user:manage.
	AddCircleMember(ctx context.Context, arguments AddCircleMemberArguments) (*Circle, error)

	// Say what somebody the caller shares a circle with is to them: "father",
	// "wife". The other side is filled in from it (the caller is their son),
	// and they are told. It is one fact between the two: either may change or
	// remove it, for both. Returns the person as the caller now sees them.
	// Needs circle:use and a circle in common.
	SetPersonRelationship(ctx context.Context, arguments SetPersonRelationshipArguments) (*CircleMember, error)

	// Take away what the caller and somebody are to each other, whichever of
	// them said it. The other person is told. True when there was one. Needs
	// circle:use and a circle in common.
	RemovePersonRelationship(ctx context.Context, arguments RemovePersonRelationshipArguments) (bool, error)
}

// Circle is a named set of people on this server who know each other.
type Circle struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// What the circle is for: family, friends or colleagues
	CircleKind string `json:"circleKind"`

	CreatedAt time.Time `json:"createdAt"`

	// The caller's own role in it: admin or member. Empty for an operator
	// looking at a circle they are not in.
	MemberRole string `json:"memberRole"`

	Members []*CircleMember `json:"members"`

	// The invitations still waiting for an answer. Empty unless the caller
	// is an admin of the circle.
	PendingInvitations []*CircleInvitation `json:"pendingInvitations"`
}

// CircleMember is one person in a circle.
type CircleMember struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`

	// What they call themselves; empty when they have not said
	Name string `json:"name"`

	// Address of their picture, or null when they have none
	AvatarURL *string `json:"avatarUrl"`

	// Which of the twelve avatar colors their initial is drawn on when they
	// have no picture
	AvatarColorIndex int `json:"avatarColorIndex"`

	// admin or member; empty in the list of everybody the caller shares a
	// circle with, where it would mean nothing
	MemberRole string `json:"memberRole"`

	// When they joined the circle; in the list of everybody the caller
	// shares a circle with, the earliest they joined any of them
	JoinedAt time.Time `json:"joinedAt"`

	// What this person is to the caller, such as parent or spouse, or empty
	// when neither of them has said. Only the two of them see it.
	RelationshipKind string `json:"relationshipKind"`

	// The gender of the word to show for it: masculine, feminine or neutral.
	// It follows how this person said they are described, or the word the
	// caller chose for them.
	RelationshipTermGender string `json:"relationshipTermGender"`

	// That word in English, such as father or wife; empty when there is no
	// relationship
	RelationshipWord string `json:"relationshipWord"`

	// The caller's own contact that they linked to this person, or empty.
	// Private to the caller: the person it names is never told, and sees no
	// such field about the caller. Present only while they share a circle,
	// which every member of this type does.
	LinkedContactID string `json:"linkedContactId"`
}

// CircleInvitation asks one person to join one circle.
type CircleInvitation struct {
	ID         string `json:"id"`
	CircleID   string `json:"circleId"`
	CircleName string `json:"circleName"`

	// family, friends or colleagues
	CircleKind string `json:"circleKind"`

	// Who asked: their name, or their username when they have none
	InvitedByName string `json:"invitedByName"`

	// Who was asked
	InvitedUsername string `json:"invitedUsername"`

	// The role they join with: admin or member
	MemberRole string `json:"memberRole"`

	CreatedAt time.Time `json:"createdAt"`

	// After this it can no longer be accepted
	ExpiresAt time.Time `json:"expiresAt"`
}

// describeCircleMember is one person as the caller sees them, with what they
// are to the caller when relationships has it: the caller's relationships by
// the other person's account id.
func describeCircleMember(member *models.CircleMember, callerUserId string, relationships map[string]*models.PersonRelationship, linkedContactIds map[string]string) *CircleMember {
	described := &CircleMember{
		UserID:           member.UserID,
		MemberRole:       string(member.MemberRole),
		JoinedAt:         member.JoinedAt,
		AvatarColorIndex: avatar.InitialColorIndex(member.UserID),
		LinkedContactID:  linkedContactIds[member.UserID],
	}
	var pronoun models.Pronoun
	if member.User != nil {
		described.Username = member.User.Username
		described.Name = member.User.Name
		described.AvatarURL = avatarUrl(member.User)
		pronoun = member.User.Pronoun
	}
	if relationship := relationships[member.UserID]; relationship != nil && member.UserID != callerUserId {
		relationshipKind := relationship.KindFor(callerUserId)
		termGender := relationship.TermGenderFor(callerUserId, pronoun)
		described.RelationshipKind = string(relationshipKind)
		described.RelationshipTermGender = string(termGender)
		described.RelationshipWord = relationshipKind.Word(termGender)
	}
	return described
}

// relationshipsOf is a person's relationships, by the other person's account
// id.
func (self *graph) relationshipsOf(ctx context.Context, userId string) (map[string]*models.PersonRelationship, error) {
	relationships, err := self.transaction(ctx).ListPersonRelationships(userId)
	if err != nil {
		return nil, err
	}
	byUserId := make(map[string]*models.PersonRelationship, len(relationships))
	for _, relationship := range relationships {
		byUserId[relationship.Other(userId)] = relationship
	}
	return byUserId, nil
}

// linkedContactIdsOf is the contact id of the caller's own contact linked to
// each account, by the account's id: the first address book that has one.
func (self *graph) linkedContactIdsOf(ctx context.Context, userId string) (map[string]string, error) {
	links, err := self.transaction(ctx).ListContactAccountLinks(userId)
	if err != nil {
		return nil, err
	}
	byUserId := make(map[string]string, len(links))
	for _, link := range links {
		if _, found := byUserId[link.AccountUserID]; !found {
			byUserId[link.AccountUserID] = link.ID
		}
	}
	return byUserId, nil
}

func describeCircleInvitation(invitation *models.CircleInvitation) *CircleInvitation {
	invitedByName := invitation.InvitedByName
	if invitedByName == "" {
		invitedByName = invitation.InvitedByUsername
	}
	return &CircleInvitation{
		ID:              invitation.ID,
		CircleID:        invitation.CircleID,
		CircleName:      invitation.CircleName,
		CircleKind:      string(invitation.CircleKind),
		InvitedByName:   invitedByName,
		InvitedUsername: invitation.InvitedUsername,
		MemberRole:      string(invitation.MemberRole),
		CreatedAt:       invitation.CreatedAt,
		ExpiresAt:       invitation.ExpiresAt,
	}
}

// describeCircle is a circle as the caller sees it. The waiting invitations
// are the circle's own and are shown only to its admins, so they are passed
// in by the caller who has already decided that.
func describeCircle(circle *models.Circle, callerUserId string, pending []*models.CircleInvitation, relationships map[string]*models.PersonRelationship, linkedContactIds map[string]string) *Circle {
	described := &Circle{
		ID:                 circle.ID,
		Name:               circle.Name,
		CircleKind:         string(circle.CircleKind),
		CreatedAt:          circle.CreatedAt,
		Members:            make([]*CircleMember, 0, len(circle.Members)),
		PendingInvitations: make([]*CircleInvitation, 0, len(pending)),
	}
	if member := circle.Member(callerUserId); member != nil {
		described.MemberRole = string(member.MemberRole)
	}
	for _, member := range circle.Members {
		described.Members = append(described.Members, describeCircleMember(member, callerUserId, relationships, linkedContactIds))
	}
	if described.MemberRole == string(models.MemberRoleAdmin) {
		for _, invitation := range pending {
			described.PendingInvitations = append(described.PendingInvitations, describeCircleInvitation(invitation))
		}
	}
	return described
}

// circleFor describes a circle the caller is in, reading its waiting
// invitations when they are an admin.
func (self *graph) circleFor(ctx context.Context, circle *models.Circle, callerUserId string) (*Circle, error) {
	var pending []*models.CircleInvitation
	if member := circle.Member(callerUserId); member != nil && member.MemberRole == models.MemberRoleAdmin {
		var err error
		if pending, err = self.transaction(ctx).ListCircleInvitationsOf(circle.ID, time.Now()); err != nil {
			return nil, err
		}
	}
	relationships, err := self.relationshipsOf(ctx, callerUserId)
	if err != nil {
		return nil, err
	}
	linkedContactIds, err := self.linkedContactIdsOf(ctx, callerUserId)
	if err != nil {
		return nil, err
	}
	return describeCircle(circle, callerUserId, pending, relationships, linkedContactIds), nil
}

// translateCircleError turns what a membership change says into what the API
// says. The refusal that keeps a circle's admin gets words of its own; it is
// about a circle the caller is in, so it reveals nothing.
func translateCircleError(err error) error {
	if errors.Is(err, db.ErrCircleNeedsAdmin) {
		return fmt.Errorf("%w: a circle needs an admin; make somebody else an admin first, or delete the circle", api.ErrInvalidArguments)
	}
	if errors.Is(err, db.ErrPasswordsBusy) {
		// Ending the shares of Passwords that only the circle made possible
		// waited for a lock and gave up; the member is still in.
		return passwordsFailure(err)
	}
	return translateError(err)
}

func (self *graph) ListCircles(ctx context.Context) ([]*Circle, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	circles, err := self.transaction(ctx).ListCircles(user.ID)
	if err != nil {
		return nil, err
	}
	described := make([]*Circle, 0, len(circles))
	for _, circle := range circles {
		each, err := self.circleFor(ctx, circle, user.ID)
		if err != nil {
			return nil, err
		}
		described = append(described, each)
	}
	return described, nil
}

type ReadCircleArguments struct {
	CircleID string `json:"circleId"`
}

func (self *graph) ReadCircle(ctx context.Context, arguments ReadCircleArguments) (*Circle, error) {
	circle, member, err := self.requireCircleMember(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	return self.circleFor(ctx, circle, member.UserID)
}

func (self *graph) ListCircleInvitations(ctx context.Context) ([]*CircleInvitation, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	invitations, err := self.transaction(ctx).ListCircleInvitationsTo(user.ID, time.Now())
	if err != nil {
		return nil, err
	}
	described := make([]*CircleInvitation, 0, len(invitations))
	for _, invitation := range invitations {
		described = append(described, describeCircleInvitation(invitation))
	}
	return described, nil
}

func (self *graph) ListCirclePeople(ctx context.Context) ([]*CircleMember, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	people, err := self.transaction(ctx).ListCirclePeople(user.ID)
	if err != nil {
		return nil, err
	}
	relationships, err := self.relationshipsOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	linkedContactIds, err := self.linkedContactIdsOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	described := make([]*CircleMember, 0, len(people))
	for _, person := range people {
		described = append(described, describeCircleMember(person, user.ID, relationships, linkedContactIds))
	}
	return described, nil
}

type CreateCircleArguments struct {
	// What to call it, at most 100 characters
	Name string `json:"name"`

	// What it is for: family, friends or colleagues
	CircleKind string `json:"circleKind"`
}

func (self *graph) CreateCircle(ctx context.Context, arguments CreateCircleArguments) (*Circle, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	created, err := self.transaction(ctx).CreateCircle(&models.Circle{
		Name:       arguments.Name,
		CircleKind: models.CircleKind(strings.TrimSpace(arguments.CircleKind)),
	}, user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s made the circle %q", user.Username, created.Name)
	// A new circle has only its maker in it, who has no relationship with
	// themselves.
	return describeCircle(created, user.ID, nil, nil, nil), nil
}

type RenameCircleArguments struct {
	CircleID string `json:"circleId"`

	// What to call it from now on, at most 100 characters
	Name string `json:"name"`
}

func (self *graph) RenameCircle(ctx context.Context, arguments RenameCircleArguments) (*Circle, error) {
	_, member, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	renamed, err := self.transaction(ctx).RenameCircle(arguments.CircleID, arguments.Name)
	if err != nil {
		return nil, translateError(err)
	}
	return self.circleFor(ctx, renamed, member.UserID)
}

type DeleteCircleArguments struct {
	CircleID string `json:"circleId"`
}

func (self *graph) DeleteCircle(ctx context.Context, arguments DeleteCircleArguments) (bool, error) {
	circle, member, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	// Everybody in the circle leaves its chat, and the group chats that were
	// held together only by it: told who was in them, after.
	snapshot, err := self.snapshotChatsOf(tx, circleMemberIds(circle)...)
	if err != nil {
		return false, err
	}
	if err := tx.DeleteCircle(arguments.CircleID); err != nil {
		return false, translateCircleError(err)
	}
	if err := self.publishMembersChangedSince(ctx, snapshot); err != nil {
		return false, err
	}
	log.Noticef("%s deleted the circle %q", member.User.Username, circle.Name)
	return true, nil
}

// requireTapForHealthSharedCircle refuses a call that is not a person's own
// tap when Health is shared with the circle: whoever joins a circle reads what
// it was shared, so bringing somebody in is sharing that Health with them,
// which SetHealthShare leaves to a tap as well.
func (self *graph) requireTapForHealthSharedCircle(ctx context.Context, circleId string) error {
	if api.ContextPrincipal(ctx).IsPersonsTap() {
		return nil
	}
	isShared, err := self.transaction(ctx).CircleHasHealthShare(circleId)
	if err != nil {
		return err
	}
	if isShared {
		return fmt.Errorf("%w: %s", api.ErrPermissionDenied, healthOnATap)
	}
	return nil
}

type InviteToCircleArguments struct {
	CircleID string `json:"circleId"`

	// The exact username of the account to invite
	Username string `json:"username"`

	// What they join as: admin or member
	MemberRole string `json:"memberRole"`
}

func (self *graph) InviteToCircle(ctx context.Context, arguments InviteToCircleArguments) (bool, error) {
	circle, member, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return false, err
	}
	// Before the username is looked at, so that the refusal says nothing
	// about whether it is an account.
	if err := self.requireTapForHealthSharedCircle(ctx, circle.ID); err != nil {
		return false, err
	}
	memberRole := models.MemberRole(strings.TrimSpace(arguments.MemberRole))
	if !memberRole.IsValid() {
		return false, fmt.Errorf("%w: the member role is admin or member", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	username := strings.TrimSpace(arguments.Username)
	if username == "" {
		return false, fmt.Errorf("%w: say which username to invite", api.ErrInvalidArguments)
	}
	// Counted before anything about the person is looked at, for every call
	// however it ends, so that the bound cannot be used to tell a username
	// that exists from one that does not, and deleting a circle does not give
	// the allowance back.
	if err := tx.RecordCircleInvitationSend(member.UserID, time.Now()); err != nil {
		return false, translateError(err)
	}
	invited, err := tx.GetUserByUsername(username)
	if err != nil {
		return false, err
	}
	// Nobody there, or nobody who could take part: the same answer as for
	// an invitation sent, and nothing stored.
	if invited == nil || invited.Disabled() {
		return true, nil
	}
	permissions, err := tx.EffectivePermissions(invited.ID)
	if err != nil {
		return false, err
	}
	if !permissions.Has(models.PermissionCircleUse) {
		return true, nil
	}

	now := time.Now()
	invitation, isNew, err := tx.CreateCircleInvitation(&models.CircleInvitation{
		CircleID:        circle.ID,
		InvitedUserID:   invited.ID,
		InvitedByUserID: member.UserID,
		MemberRole:      memberRole,
		CreatedAt:       now,
		ExpiresAt:       now.Add(models.CircleInvitationLifetime),
	})
	if err != nil {
		return false, translateError(err)
	}
	// A renewed invitation, or none because the person is in already, is not
	// announced a second time.
	if invitation == nil || !isNew {
		return true, nil
	}
	inviterName := member.User.DisplayName()
	agentId, err := agent.NoteCircleInvitation(tx, invited.ID, inviterName, circle.Name)
	if err != nil {
		return false, err
	}
	// Through the notifier chats use, so a person whose agent is off, or a
	// server with no worker, is told as well.
	if notifier := self.phoneNotifier(); notifier != nil && agentId != "" {
		tx.AfterCommit(func() { notifier.NotifyCircleInvitation(agentId, inviterName, circle.Name) })
	}
	log.Noticef("%s invited an account to the circle %q", member.User.Username, circle.Name)
	return true, nil
}

type AnswerCircleInvitationArguments struct {
	InvitationID string `json:"invitationId"`

	// true to join, false to decline
	IsAccepted bool `json:"isAccepted"`
}

func (self *graph) AnswerCircleInvitation(ctx context.Context, arguments AnswerCircleInvitationArguments) (*Circle, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	invitation, err := tx.GetCircleInvitation(arguments.InvitationID)
	if err != nil {
		return nil, err
	}
	// Somebody else's invitation is as good as none.
	if invitation == nil || invitation.InvitedUserID != user.ID {
		return nil, api.ErrNotFound
	}
	// It is the caller's own, so they may be told why it cannot be answered.
	if invitation.AnsweredAt != nil {
		return nil, fmt.Errorf("%w: that invitation was already answered", api.ErrInvalidArguments)
	}
	if !invitation.IsOpen(time.Now()) {
		return nil, fmt.Errorf("%w: that invitation has expired", api.ErrInvalidArguments)
	}
	invitationAnswer := models.InvitationAnswerDeclined
	if arguments.IsAccepted {
		// The invitation may have been made while nothing was shared with the
		// circle: whoever joins reads what it is shared now, so joining is
		// asked of a person's own tap as bringing somebody in is.
		if err := self.requireTapForHealthSharedCircle(ctx, invitation.CircleID); err != nil {
			return nil, err
		}
		invitationAnswer = models.InvitationAnswerAccepted
	}
	joined, err := tx.AnswerCircleInvitation(invitation.ID, invitationAnswer, time.Now())
	if err != nil {
		return nil, translateError(err)
	}
	if joined == nil {
		return nil, nil
	}
	if err := self.publishCircleChatMembersChanged(ctx, joined.ID); err != nil {
		return nil, err
	}
	log.Noticef("%s joined the circle %q", user.Username, joined.Name)
	return self.circleFor(ctx, joined, user.ID)
}

type WithdrawCircleInvitationArguments struct {
	InvitationID string `json:"invitationId"`
}

func (self *graph) WithdrawCircleInvitation(ctx context.Context, arguments WithdrawCircleInvitationArguments) (bool, error) {
	if _, err := self.requireCirclePerson(ctx); err != nil {
		return false, err
	}
	invitation, err := self.transaction(ctx).GetCircleInvitation(arguments.InvitationID)
	if err != nil {
		return false, err
	}
	if invitation == nil {
		return false, api.ErrNotFound
	}
	if _, _, err := self.requireCircleAdmin(ctx, invitation.CircleID); err != nil {
		return false, err
	}
	if _, err := self.transaction(ctx).AnswerCircleInvitation(invitation.ID, models.InvitationAnswerWithdrawn, time.Now()); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

type SetCircleMemberRoleArguments struct {
	CircleID string `json:"circleId"`

	// ID of the account whose role to change
	UserID string `json:"userId"`

	// admin or member
	MemberRole string `json:"memberRole"`
}

func (self *graph) SetCircleMemberRole(ctx context.Context, arguments SetCircleMemberRoleArguments) (*Circle, error) {
	_, member, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	memberRole := models.MemberRole(strings.TrimSpace(arguments.MemberRole))
	if !memberRole.IsValid() {
		return nil, fmt.Errorf("%w: the member role is admin or member", api.ErrInvalidArguments)
	}
	changed, err := self.transaction(ctx).SetCircleMemberRole(arguments.CircleID, arguments.UserID, memberRole)
	if err != nil {
		return nil, translateCircleError(err)
	}
	if err := self.publishCircleChatMembersChanged(ctx, arguments.CircleID); err != nil {
		return nil, err
	}
	return self.circleFor(ctx, changed, member.UserID)
}

type RemoveCircleMemberArguments struct {
	CircleID string `json:"circleId"`

	// ID of the account to take out
	UserID string `json:"userId"`
}

// RemoveCircleMember answers with the circle as the caller now sees it, or
// null when they took themselves out of it.
func (self *graph) RemoveCircleMember(ctx context.Context, arguments RemoveCircleMemberArguments) (*Circle, error) {
	_, member, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	snapshot, err := self.snapshotChatsOf(tx, arguments.UserID)
	if err != nil {
		return nil, err
	}
	remaining, err := tx.RemoveCircleMember(arguments.CircleID, arguments.UserID)
	if err != nil {
		return nil, translateCircleError(err)
	}
	if err := self.publishMembersChangedSince(ctx, snapshot); err != nil {
		return nil, err
	}
	if remaining == nil || remaining.Member(member.UserID) == nil {
		return nil, nil
	}
	return self.circleFor(ctx, remaining, member.UserID)
}

type LeaveCircleArguments struct {
	CircleID string `json:"circleId"`
}

func (self *graph) LeaveCircle(ctx context.Context, arguments LeaveCircleArguments) (bool, error) {
	_, member, err := self.requireCircleMember(ctx, arguments.CircleID)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	snapshot, err := self.snapshotChatsOf(tx, member.UserID)
	if err != nil {
		return false, err
	}
	if _, err := tx.RemoveCircleMember(arguments.CircleID, member.UserID); err != nil {
		return false, translateCircleError(err)
	}
	if err := self.publishMembersChangedSince(ctx, snapshot); err != nil {
		return false, err
	}
	return true, nil
}

type AddCircleMemberArguments struct {
	CircleID string `json:"circleId"`

	// The exact username of the account to put in
	Username string `json:"username"`

	// What they join as: admin or member
	MemberRole string `json:"memberRole"`
}

func (self *graph) AddCircleMember(ctx context.Context, arguments AddCircleMemberArguments) (*Circle, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	// Said plainly: "not found" here read as if the circle were missing, to
	// an admin of that very circle.
	if !principal.Permissions.Has(models.PermissionUserManage) {
		return nil, fmt.Errorf("%w: putting an account in a circle needs the %s permission", api.ErrPermissionDenied, models.PermissionUserManage)
	}
	if err := self.requireTapForHealthSharedCircle(ctx, arguments.CircleID); err != nil {
		return nil, err
	}
	memberRole := models.MemberRole(strings.TrimSpace(arguments.MemberRole))
	if !memberRole.IsValid() {
		return nil, fmt.Errorf("%w: the member role is admin or member", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	user, err := tx.GetUserByUsername(strings.TrimSpace(arguments.Username))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, api.ErrNotFound
	}
	before, err := tx.GetCircle(arguments.CircleID)
	if err != nil {
		return nil, err
	}
	isAlreadyMember := before != nil && before.Member(user.ID) != nil
	added, err := tx.AddCircleMember(arguments.CircleID, user.ID, memberRole, principal.UserID())
	if err != nil {
		return nil, translateError(err)
	}
	if !isAlreadyMember {
		if err := self.tellCircleAdministratorsOfMember(ctx, added, user); err != nil {
			return nil, err
		}
	}
	if err := self.publishCircleChatMembersChanged(ctx, added.ID); err != nil {
		return nil, err
	}
	log.Noticef("%s put the account %q in the circle %q", operatorName(ctx), user.Username, added.Name)
	return self.circleFor(ctx, added, principal.UserID())
}

// tellCircleAdministratorsOfMember tells the administrators of a circle that an
// operator put an account in it, which no invitation announced: a note in
// their agent's main conversation, written in this transaction, and a
// notification on their phones once it commits. The account put in is not told
// about itself.
func (self *graph) tellCircleAdministratorsOfMember(ctx context.Context, circle *models.Circle, added *models.User) error {
	tx := self.transaction(ctx)
	addedName := added.DisplayName()
	for _, member := range circle.Members {
		if member.MemberRole != models.MemberRoleAdmin || member.UserID == added.ID {
			continue
		}
		agentId, err := agent.NoteCircleMemberAdded(tx, member.UserID, addedName, circle.Name)
		if err != nil {
			return err
		}
		if notifier := self.phoneNotifier(); notifier != nil && agentId != "" {
			circleName := circle.Name
			tx.AfterCommit(func() { notifier.NotifyCircleMemberAdded(agentId, addedName, circleName) })
		}
	}
	return nil
}

// circleMemberIds are the accounts in a circle.
func circleMemberIds(circle *models.Circle) []string {
	userIds := make([]string, 0, len(circle.Members))
	for _, member := range circle.Members {
		userIds = append(userIds, member.UserID)
	}
	return userIds
}
