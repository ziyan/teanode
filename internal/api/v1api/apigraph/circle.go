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
	// chat or a share can be offered to. Their memberRole is empty, because a
	// role belongs to one circle. Needs circle:use.
	ListCirclePeople(ctx context.Context) ([]*CircleMember, error)
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

func describeCircleMember(member *models.CircleMember) *CircleMember {
	described := &CircleMember{
		UserID:           member.UserID,
		MemberRole:       string(member.MemberRole),
		JoinedAt:         member.JoinedAt,
		AvatarColorIndex: avatar.InitialColorIndex(member.UserID),
	}
	if member.User != nil {
		described.Username = member.User.Username
		described.Name = member.User.Name
		described.AvatarURL = avatarURL(member.User)
	}
	return described
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
func describeCircle(circle *models.Circle, callerUserId string, pending []*models.CircleInvitation) *Circle {
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
		described.Members = append(described.Members, describeCircleMember(member))
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
	return describeCircle(circle, callerUserId, pending), nil
}

// translateCircleError turns what a membership change says into what the API
// says. The refusal that keeps a circle's admin gets words of its own; it is
// about a circle the caller is in, so it reveals nothing.
func translateCircleError(err error) error {
	if errors.Is(err, db.ErrCircleNeedsAdmin) {
		return fmt.Errorf("%w: a circle needs an admin; make somebody else an admin first, or delete the circle", api.ErrInvalidArguments)
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
	described := make([]*CircleMember, 0, len(people))
	for _, person := range people {
		described = append(described, describeCircleMember(person))
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
	return describeCircle(created, user.ID, nil), nil
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
	if err := self.transaction(ctx).DeleteCircle(arguments.CircleID); err != nil {
		return false, translateError(err)
	}
	log.Noticef("%s deleted the circle %q", member.User.Username, circle.Name)
	return true, nil
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
	memberRole := models.MemberRole(strings.TrimSpace(arguments.MemberRole))
	if !memberRole.IsValid() {
		return false, fmt.Errorf("%w: the member role is admin or member", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	username := strings.TrimSpace(arguments.Username)
	if username == "" {
		return false, fmt.Errorf("%w: say which username to invite", api.ErrInvalidArguments)
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
	if worker := self.agentWorker(); worker != nil {
		tx.AfterCommit(func() { worker.NotifyCircleInvitation(agentId, inviterName, circle.Name) })
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
	invitationAnswer := models.InvitationAnswerDeclined
	if arguments.IsAccepted {
		invitationAnswer = models.InvitationAnswerAccepted
	}
	joined, err := tx.AnswerCircleInvitation(invitation.ID, invitationAnswer, time.Now())
	if err != nil {
		return nil, translateError(err)
	}
	if joined == nil {
		return nil, nil
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
	remaining, err := self.transaction(ctx).RemoveCircleMember(arguments.CircleID, arguments.UserID)
	if err != nil {
		return nil, translateCircleError(err)
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
	if _, err := self.transaction(ctx).RemoveCircleMember(arguments.CircleID, member.UserID); err != nil {
		return false, translateCircleError(err)
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
	principal, err := self.requirePermission(ctx, models.PermissionUserManage)
	if err != nil {
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
	added, err := tx.AddCircleMember(arguments.CircleID, user.ID, memberRole, principal.UserID())
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s put the account %q in the circle %q", operatorName(ctx), user.Username, added.Name)
	return self.circleFor(ctx, added, principal.UserID())
}
