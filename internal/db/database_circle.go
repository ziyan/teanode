package db

import (
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// ErrCircleNeedsAdmin is the refusal of a change that would leave a circle
// with members and no admin: the last admin leaving or being made a member.
// The circle has to name another admin first.
var ErrCircleNeedsAdmin = errors.New("db: a circle needs at least one admin")

// circleMembershipLockKey is the key of the PostgreSQL advisory lock every
// change to who is in a circle, and every cleanup of what that change ends,
// takes first (lockCircleMembership). Circle membership changes are rare, and
// a total order between them is what makes the cleanup of one see the other
// change: leaving two circles at once, each person's cleanup used to look for
// the circle the two still shared, which the other's uncommitted leaving was
// about to end, and kept a share or a group chat between two people who then
// had no circle in common. Holding it for the rest of the transaction is what
// lets the second look at the committed result of the first.
const circleMembershipLockKey int64 = 7164951213004801

// lockCircleMembership takes the lock above for the rest of the transaction.
// It is taken before any row lock the change needs, so that every change
// takes its locks in the one order.
func lockCircleMembership(tx *gorm.DB) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(?)`, circleMembershipLockKey).Error
}

// CircleOperation is circles, who is in them and who has been invited.
//
// Every change to who is in a circle takes the circle's row for update first,
// so that two changes made at once take turns and the rules that depend on the
// other members (an admin is always left; a circle whose last member leaves is
// deleted) are decided on the members as they stand.
type CircleOperation interface {
	// CreateCircle makes a circle with its maker as the first admin.
	CreateCircle(circle *models.Circle, creatorUserId string) (*models.Circle, error)

	// GetCircle returns a circle with its members, or nil.
	GetCircle(circleId string) (*models.Circle, error)

	// ListCircles returns the circles a person is in, with their members,
	// by name.
	ListCircles(userId string) ([]*models.Circle, error)

	// RenameCircle changes a circle's name. ErrNotFound when there is none.
	RenameCircle(circleId, name string) (*models.Circle, error)

	// DeleteCircle removes a circle with its members and invitations.
	// ErrNotFound when there is none.
	DeleteCircle(circleId string) error

	// AddCircleMember puts an account in a circle. A person who is already in
	// it is left as they are. ErrNotFound when the circle or the account is
	// not there. A waiting invitation to the same circle is closed as
	// accepted. An account put in is recorded in the audit log, naming the
	// actor of the transaction.
	AddCircleMember(circleId, userId string, memberRole models.MemberRole, invitedByUserId string) (*models.Circle, error)

	// SetCircleMemberRole changes a member's role. ErrNotFound when the
	// person is not in the circle; ErrCircleNeedsAdmin when it would take
	// the last admin's.
	SetCircleMemberRole(circleId, userId string, memberRole models.MemberRole) (*models.Circle, error)

	// RemoveCircleMember takes a person out of a circle, whether they left
	// or were removed. It returns the circle as it is now, or nil when that
	// was its last member and the circle went with them. ErrNotFound when
	// the person is not in the circle; ErrCircleNeedsAdmin when they are
	// its last admin and others remain.
	RemoveCircleMember(circleId, userId string) (*models.Circle, error)

	// SharesCircle says whether two different people are in a circle
	// together.
	SharesCircle(userId, otherUserId string) (bool, error)

	// CircleHasSensitiveShare says whether anybody has shared Health or a
	// finance account with the circle, so that a new member of it would read
	// that. Sharing either with somebody new is the sharer's own tap, and so
	// is bringing somebody into a circle that has such a share.
	CircleHasSensitiveShare(circleId string) (bool, error)

	// CircleUserIDs returns everybody who shares a circle with the person,
	// once each and not the person.
	CircleUserIDs(userId string) ([]string, error)

	// ListCirclePeople returns everybody who shares a circle with the person,
	// once each, with their accounts. JoinedAt is the earliest they joined
	// any of the circles the two share; the role is left empty, because it
	// belongs to one circle.
	ListCirclePeople(userId string) ([]*models.CircleMember, error)

	// RecordCircleInvitationSend counts one call that invites somebody against
	// the person who made it, and refuses it with ErrLimitReached when they
	// have sent models.CircleInvitationsPerHourMost in the hour before sentAt.
	// It is called for every invitation however it ends, before anything about
	// the person invited is looked at, and its rows outlive the circles the
	// invitations were to, so neither whether a username exists nor deleting
	// a circle changes the count.
	RecordCircleInvitationSend(inviterUserId string, sentAt time.Time) error

	// CreateCircleInvitation writes an invitation, and says whether it is a
	// new one. A person already in the circle is not invited again and
	// nothing is written (nil, false). A waiting invitation to the same
	// person is renewed with the new role and a new lifetime rather than
	// doubled (false). ErrNotFound when the circle is not there.
	CreateCircleInvitation(invitation *models.CircleInvitation) (*models.CircleInvitation, bool, error)

	// GetCircleInvitation returns one invitation, answered or not, or nil.
	GetCircleInvitation(invitationId string) (*models.CircleInvitation, error)

	// ListCircleInvitationsTo returns the invitations to a person that can
	// still be answered at this moment, the newest first.
	ListCircleInvitationsTo(userId string, now time.Time) ([]*models.CircleInvitation, error)

	// ListCircleInvitationsOf returns the invitations of a circle that can
	// still be answered at this moment, the newest first.
	ListCircleInvitationsOf(circleId string, now time.Time) ([]*models.CircleInvitation, error)

	// AnswerCircleInvitation ends an invitation that can still be answered.
	// Accepting puts the person in the circle with the role they were
	// invited with and returns it; the other answers return nil. ErrNotFound
	// when it was answered already, has expired, or is not there.
	AnswerCircleInvitation(invitationId string, invitationAnswer models.InvitationAnswer, now time.Time) (*models.Circle, error)
}

type circleModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	ModifiedAt      time.Time `gorm:"column:modified_at"`
	Name            string    `gorm:"column:name"`
	CircleKind      string    `gorm:"column:circle_kind"`
	CreatedByUserID *string   `gorm:"column:created_by_user_id"`
}

func (circleModel) TableName() string { return "circle" }

type circleMemberModel struct {
	CircleID        string    `gorm:"column:circle_id;primaryKey"`
	UserID          string    `gorm:"column:user_id;primaryKey"`
	MemberRole      string    `gorm:"column:member_role"`
	JoinedAt        time.Time `gorm:"column:joined_at"`
	InvitedByUserID *string   `gorm:"column:invited_by_user_id"`
}

func (circleMemberModel) TableName() string { return "circle_member" }

// circleMemberAudit is what the audit log keeps of an account put in a circle.
type circleMemberAudit struct {
	CircleID   string `json:"circleId"`
	UserID     string `json:"userId"`
	Username   string `json:"username"`
	MemberRole string `json:"memberRole"`
}

type circleInvitationModel struct {
	ID               string     `gorm:"column:id;primaryKey"`
	CircleID         string     `gorm:"column:circle_id"`
	InvitedUserID    string     `gorm:"column:invited_user_id"`
	InvitedByUserID  *string    `gorm:"column:invited_by_user_id"`
	MemberRole       string     `gorm:"column:member_role"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ExpiresAt        time.Time  `gorm:"column:expires_at"`
	AnsweredAt       *time.Time `gorm:"column:answered_at"`
	InvitationAnswer *string    `gorm:"column:invitation_answer"`
}

func (circleInvitationModel) TableName() string { return "circle_invitation" }

// circleInvitationRow is an invitation as the listing queries read it, with
// the names a screen shows beside it.
type circleInvitationRow struct {
	ID                string     `gorm:"column:id"`
	CircleID          string     `gorm:"column:circle_id"`
	InvitedUserID     string     `gorm:"column:invited_user_id"`
	InvitedByUserID   *string    `gorm:"column:invited_by_user_id"`
	MemberRole        string     `gorm:"column:member_role"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	ExpiresAt         time.Time  `gorm:"column:expires_at"`
	AnsweredAt        *time.Time `gorm:"column:answered_at"`
	InvitationAnswer  *string    `gorm:"column:invitation_answer"`
	CircleName        string     `gorm:"column:circle_name"`
	CircleKind        string     `gorm:"column:circle_kind"`
	InvitedByUsername *string    `gorm:"column:invited_by_username"`
	InvitedByName     *string    `gorm:"column:invited_by_name"`
	InvitedUsername   string     `gorm:"column:invited_username"`
}

// circleInvitationSelect reads invitations with the circle's and the two
// people's names. The inviter is a left join because that account may be
// gone.
const circleInvitationSelect = `SELECT invitation.*, circle.name AS circle_name, circle.circle_kind AS circle_kind,
	inviter.username AS invited_by_username, inviter.name AS invited_by_name, invited.username AS invited_username
	FROM "circle_invitation" invitation
	JOIN "circle" circle ON circle.id = invitation.circle_id
	JOIN "user" invited ON invited.id = invitation.invited_user_id
	LEFT JOIN "user" inviter ON inviter.id = invitation.invited_by_user_id`

func (self *circleInvitationRow) toModel() *models.CircleInvitation {
	invitation := &models.CircleInvitation{
		ID:              self.ID,
		CircleID:        self.CircleID,
		InvitedUserID:   self.InvitedUserID,
		MemberRole:      models.MemberRole(self.MemberRole),
		CreatedAt:       self.CreatedAt.In(time.Local),
		ExpiresAt:       self.ExpiresAt.In(time.Local),
		CircleName:      self.CircleName,
		CircleKind:      models.CircleKind(self.CircleKind),
		InvitedUsername: self.InvitedUsername,
	}
	if self.InvitedByUserID != nil {
		invitation.InvitedByUserID = *self.InvitedByUserID
	}
	if self.InvitedByUsername != nil {
		invitation.InvitedByUsername = *self.InvitedByUsername
	}
	if self.InvitedByName != nil {
		invitation.InvitedByName = *self.InvitedByName
	}
	if self.AnsweredAt != nil {
		answeredAt := self.AnsweredAt.In(time.Local)
		invitation.AnsweredAt = &answeredAt
	}
	if self.InvitationAnswer != nil {
		invitation.InvitationAnswer = models.InvitationAnswer(*self.InvitationAnswer)
	}
	return invitation
}

func circleFromModel(model *circleModel, members []*models.CircleMember) *models.Circle {
	circle := &models.Circle{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		Name:       model.Name,
		CircleKind: models.CircleKind(model.CircleKind),
		Members:    members,
	}
	if model.CreatedByUserID != nil {
		circle.CreatedByUserID = *model.CreatedByUserID
	}
	if circle.Members == nil {
		circle.Members = []*models.CircleMember{}
	}
	return circle
}

func nullableUserId(userId string) *string {
	if userId == "" {
		return nil
	}
	return &userId
}

// loadCircleMembers reads the members of these circles with their accounts:
// admins first, then in the order they joined.
func loadCircleMembers(tx *gorm.DB, circleIds []string) (map[string][]*models.CircleMember, error) {
	members := map[string][]*models.CircleMember{}
	if len(circleIds) == 0 {
		return members, nil
	}
	var rows []circleMemberModel
	if err := tx.Where("\"circle_id\" IN ?", circleIds).
		Order("(\"member_role\" = 'admin') DESC, \"joined_at\" ASC, \"user_id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	userIds := make([]string, 0, len(rows))
	for _, row := range rows {
		userIds = append(userIds, row.UserID)
	}
	users, err := loadUsersById(tx, userIds)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		member := &models.CircleMember{
			CircleID:   row.CircleID,
			UserID:     row.UserID,
			MemberRole: models.MemberRole(row.MemberRole),
			JoinedAt:   row.JoinedAt.In(time.Local),
			User:       users[row.UserID],
		}
		if row.InvitedByUserID != nil {
			member.InvitedByUserID = *row.InvitedByUserID
		}
		members[row.CircleID] = append(members[row.CircleID], member)
	}
	return members, nil
}

// loadUsersById reads accounts, without their groups, which a screen drawing
// a person has no use for.
func loadUsersById(tx *gorm.DB, userIds []string) (map[string]*models.User, error) {
	users := map[string]*models.User{}
	if len(userIds) == 0 {
		return users, nil
	}
	var rows []userModel
	if err := tx.Where("\"id\" IN ?", uniqueStrings(userIds)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		users[rows[index].ID] = userFromModel(&rows[index], nil)
	}
	return users, nil
}

func (self *transaction) CreateCircle(circle *models.Circle, creatorUserId string) (*models.Circle, error) {
	if err := circle.Validate(); err != nil {
		return nil, err
	}
	if creatorUserId == "" {
		return nil, ErrInvalidArguments
	}
	var madeCount int64
	if err := self.tx.Model(&circleModel{}).Where(`"created_by_user_id" = ?`, creatorUserId).Count(&madeCount).Error; err != nil {
		return nil, err
	}
	if madeCount >= models.CirclesMadeMost {
		return nil, limitReached("you have made %d circles, the most one person has; delete one you no longer use before making another", models.CirclesMadeMost)
	}
	now := time.Now()
	model := &circleModel{
		ID:              newID(),
		CreatedAt:       now,
		ModifiedAt:      now,
		Name:            models.PlainCircleName(circle.Name),
		CircleKind:      string(circle.CircleKind),
		CreatedByUserID: &creatorUserId,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Create(&circleMemberModel{
		CircleID: model.ID, UserID: creatorUserId, MemberRole: string(models.MemberRoleAdmin), JoinedAt: now,
	}).Error; err != nil {
		return nil, err
	}
	// Every circle has a chat of its own, with the members the circle has.
	if _, err := self.ensureCircleChat(model.ID); err != nil {
		return nil, err
	}
	return self.GetCircle(model.ID)
}

func (self *transaction) GetCircle(circleId string) (*models.Circle, error) {
	if circleId == "" {
		return nil, nil
	}
	var model circleModel
	result := self.tx.Where("\"id\" = ?", circleId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	members, err := loadCircleMembers(self.tx, []string{model.ID})
	if err != nil {
		return nil, err
	}
	return circleFromModel(&model, members[model.ID]), nil
}

func (self *transaction) ListCircles(userId string) ([]*models.Circle, error) {
	var rows []circleModel
	if err := self.tx.Where("\"id\" IN (?)", self.tx.Table("circle_member").Select("circle_id").Where("\"user_id\" = ?", userId)).
		Order("lower(\"name\") ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	circleIds := make([]string, 0, len(rows))
	for _, row := range rows {
		circleIds = append(circleIds, row.ID)
	}
	members, err := loadCircleMembers(self.tx, circleIds)
	if err != nil {
		return nil, err
	}
	circles := make([]*models.Circle, 0, len(rows))
	for index := range rows {
		circles = append(circles, circleFromModel(&rows[index], members[rows[index].ID]))
	}
	return circles, nil
}

func (self *transaction) RenameCircle(circleId, name string) (*models.Circle, error) {
	// The circle membership lock comes before any circle row, as in every
	// other change to circles. Taking the row first would let a rename wait for
	// a request that holds the membership lock and waits for this row (a
	// Passwords change after the rename in the same request, against a person
	// leaving the circle).
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	var locked circleModel
	if err := lockRow(self.tx, &locked, circleId); err != nil {
		return nil, err
	}
	circle := &models.Circle{Name: name, CircleKind: models.CircleKind(locked.CircleKind)}
	if err := circle.Validate(); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&circleModel{}).Where("\"id\" = ?", circleId).
		Updates(map[string]any{"name": models.PlainCircleName(circle.Name), "modified_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	// The circle chat is called what the circle is.
	if err := self.tx.Model(&chatModel{}).Where(`"circle_id" = ?`, circleId).
		Updates(map[string]any{"title": models.PlainCircleName(circle.Name), "modified_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	return self.GetCircle(circleId)
}

func (self *transaction) DeleteCircle(circleId string) error {
	if err := lockCircleMembership(self.tx); err != nil {
		return err
	}
	var locked circleModel
	if err := lockRow(self.tx, &locked, circleId); err != nil {
		return err
	}
	var memberIds []string
	if err := self.tx.Model(&circleMemberModel{}).Where("\"circle_id\" = ?", circleId).Pluck("user_id", &memberIds).Error; err != nil {
		return err
	}
	// One unit with the ending of the members' shares, as in RemoveCircleMember.
	return self.withPasswordsSavepoint(func() error {
		return deleteCircleRow(self.tx, circleId, memberIds)
	})
}

// deleteCircleRow deletes a circle whose row the caller has locked, and does
// for each of its members what leaving it does, in the same transaction. They
// leave the circle's own chat first, while it is there, which switches off
// their agents' reading of it (deleting the circle would take the chat with it
// and leave those sources pointing at nothing); and once the circle is gone,
// the group chats in which they now share a circle with none of the others,
// which cannot be told before.
func deleteCircleRow(tx *gorm.DB, circleId string, memberIds []string) error {
	memberIds = append([]string{}, memberIds...)
	sort.Strings(memberIds)
	now := time.Now()
	for _, userId := range memberIds {
		if err := leaveCircleChats(tx, circleId, userId, now); err != nil {
			return err
		}
	}
	if err := tx.Where("\"id\" = ?", circleId).Delete(&circleModel{}).Error; err != nil {
		return err
	}
	for _, userId := range memberIds {
		if err := leaveCircleChats(tx, circleId, userId, now); err != nil {
			return err
		}
	}
	return endSharesOutsideCircles(tx, memberIds...)
}

// endSharesOutsideCircles ends, for each of these people, every direct
// share between them and somebody they no longer share any circle with,
// in the transaction that changed the circles. Shares with a circle need
// nothing: they reach a person only through circle_member. Every kind of
// share that can name a person is ended here, so leaving a circle has one
// meaning whatever was shared, and so is what the two said they are to each
// other, and a contact the owner linked to the other's account.
func endSharesOutsideCircles(tx *gorm.DB, userIds ...string) error {
	for _, userId := range userIds {
		if err := endRelationshipsOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endContactAccountsOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endCalendarSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endRecordingSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endLocationSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endHealthSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endFinanceSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endPasswordSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endPageSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endSkillSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
		if err := endPhotoAlbumSharesOutsideCircles(tx, userId); err != nil {
			return err
		}
	}
	return nil
}

// lockedCircleMembers takes the circle's row for update and returns its
// member rows as they stand, which is the state every membership rule is
// decided on.
func (self *transaction) lockedCircleMembers(circleId string) ([]circleMemberModel, error) {
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	var locked circleModel
	if err := lockRow(self.tx, &locked, circleId); err != nil {
		return nil, err
	}
	var rows []circleMemberModel
	if err := self.tx.Where("\"circle_id\" = ?", circleId).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (self *transaction) touchCircle(circleId string) error {
	return self.tx.Model(&circleModel{}).Where("\"id\" = ?", circleId).Update("modified_at", time.Now()).Error
}

func countCircleAdmins(rows []circleMemberModel) int {
	count := 0
	for _, row := range rows {
		if row.MemberRole == string(models.MemberRoleAdmin) {
			count++
		}
	}
	return count
}

func (self *transaction) AddCircleMember(circleId, userId string, memberRole models.MemberRole, invitedByUserId string) (*models.Circle, error) {
	if !memberRole.IsValid() {
		return nil, ErrInvalidArguments
	}
	rows, err := self.lockedCircleMembers(circleId)
	if err != nil {
		return nil, err
	}
	var usernames []string
	if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", userId).Pluck("username", &usernames).Error; err != nil {
		return nil, err
	}
	if len(usernames) == 0 {
		return nil, ErrNotFound
	}
	isMember := false
	for _, row := range rows {
		if row.UserID == userId {
			isMember = true
		}
	}
	if !isMember {
		member := &circleMemberModel{
			CircleID: circleId, UserID: userId, MemberRole: string(memberRole), JoinedAt: time.Now(),
			InvitedByUserID: nullableUserId(invitedByUserId),
		}
		// The one way into a circle that no invitation precedes, so the log
		// says who was put in, in which circle and by whom.
		if err := self.applyMutation(models.AuditResourceCircleMember, circleId+"/"+userId, models.AuditActionCreate, nil,
			&circleMemberAudit{CircleID: circleId, UserID: userId, Username: usernames[0], MemberRole: string(memberRole)},
			func(tx *gorm.DB) error { return tx.Create(member).Error }); err != nil {
			return nil, err
		}
		if err := self.touchCircle(circleId); err != nil {
			return nil, err
		}
		if err := self.joinCircleChat(circleId, userId, memberRole); err != nil {
			return nil, err
		}
	}
	// Whoever put them in, an invitation still waiting for their answer has
	// been answered.
	if err := self.tx.Model(&circleInvitationModel{}).
		Where("\"circle_id\" = ? AND \"invited_user_id\" = ? AND \"answered_at\" IS NULL", circleId, userId).
		Updates(map[string]any{"answered_at": time.Now(), "invitation_answer": string(models.InvitationAnswerAccepted)}).Error; err != nil {
		return nil, err
	}
	return self.GetCircle(circleId)
}

func (self *transaction) SetCircleMemberRole(circleId, userId string, memberRole models.MemberRole) (*models.Circle, error) {
	if !memberRole.IsValid() {
		return nil, ErrInvalidArguments
	}
	rows, err := self.lockedCircleMembers(circleId)
	if err != nil {
		return nil, err
	}
	var found *circleMemberModel
	for index := range rows {
		if rows[index].UserID == userId {
			found = &rows[index]
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	if found.MemberRole != string(memberRole) {
		if found.MemberRole == string(models.MemberRoleAdmin) && countCircleAdmins(rows) == 1 {
			return nil, ErrCircleNeedsAdmin
		}
		if err := self.tx.Model(&circleMemberModel{}).Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, userId).
			Update("member_role", string(memberRole)).Error; err != nil {
			return nil, err
		}
		if err := self.touchCircle(circleId); err != nil {
			return nil, err
		}
		if err := syncCircleChatRoles(self.tx, circleId); err != nil {
			return nil, err
		}
	}
	return self.GetCircle(circleId)
}

func (self *transaction) RemoveCircleMember(circleId, userId string) (*models.Circle, error) {
	rows, err := self.lockedCircleMembers(circleId)
	if err != nil {
		return nil, err
	}
	var found *circleMemberModel
	for index := range rows {
		if rows[index].UserID == userId {
			found = &rows[index]
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	// The change and the ending of the shares it makes are one unit: were the
	// ending of Passwords shares refused because Passwords is busy, a request
	// that answers with that error would still commit the member's removal.
	var remaining *models.Circle
	err = self.withPasswordsSavepoint(func() (err error) {
		remaining, err = self.removeLockedCircleMember(circleId, userId, rows, found)
		return err
	})
	if err != nil {
		return nil, err
	}
	return remaining, nil
}

func (self *transaction) removeLockedCircleMember(circleId, userId string, rows []circleMemberModel, found *circleMemberModel) (*models.Circle, error) {
	if len(rows) == 1 {
		// Nobody is left to belong to it, and its last member leaves its
		// chats as any other member would.
		return nil, deleteCircleRow(self.tx, circleId, []string{userId})
	}
	if found.MemberRole == string(models.MemberRoleAdmin) && countCircleAdmins(rows) == 1 {
		return nil, ErrCircleNeedsAdmin
	}
	if err := self.tx.Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, userId).Delete(&circleMemberModel{}).Error; err != nil {
		return nil, err
	}
	if err := self.touchCircle(circleId); err != nil {
		return nil, err
	}
	// In the same transaction, the chats that were theirs because of the
	// circle stop being.
	if err := leaveCircleChats(self.tx, circleId, userId, time.Now()); err != nil {
		return nil, err
	}
	if err := endSharesOutsideCircles(self.tx, userId); err != nil {
		return nil, err
	}
	return self.GetCircle(circleId)
}

// releaseCircles takes an account out of every circle it is in, before the
// account is deleted. The cascade would remove the rows, but not keep the
// rules: a circle left with no members would stay for ever, and one whose
// only admin went would have nobody to manage it. So the earliest-joined
// member of such a circle becomes its admin.
func releaseCircles(tx *gorm.DB, userId string) error {
	if err := lockCircleMembership(tx); err != nil {
		return err
	}
	var circleIds []string
	if err := tx.Model(&circleMemberModel{}).Where("\"user_id\" = ?", userId).Order("\"circle_id\" ASC").Pluck("circle_id", &circleIds).Error; err != nil {
		return err
	}
	for _, circleId := range circleIds {
		var locked circleModel
		if err := lockRow(tx, &locked, circleId); err != nil {
			return err
		}
		var rows []circleMemberModel
		if err := tx.Where("\"circle_id\" = ?", circleId).Order("\"joined_at\" ASC, \"user_id\" ASC").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 1 {
			if err := tx.Where("\"id\" = ?", circleId).Delete(&circleModel{}).Error; err != nil {
				return err
			}
			continue
		}
		isOnlyAdmin := false
		for _, row := range rows {
			if row.UserID == userId && row.MemberRole == string(models.MemberRoleAdmin) && countCircleAdmins(rows) == 1 {
				isOnlyAdmin = true
			}
		}
		if err := tx.Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, userId).Delete(&circleMemberModel{}).Error; err != nil {
			return err
		}
		if err := leaveCircleChats(tx, circleId, userId, time.Now()); err != nil {
			return err
		}
		if isOnlyAdmin {
			for _, row := range rows {
				if row.UserID == userId {
					continue
				}
				if err := tx.Model(&circleMemberModel{}).Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, row.UserID).
					Update("member_role", string(models.MemberRoleAdmin)).Error; err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func (self *transaction) CircleHasSensitiveShare(circleId string) (bool, error) {
	var count int64
	if err := self.tx.Model(&healthShareModel{}).Where("\"grantee_circle_id\" = ?", circleId).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := self.tx.Model(&financeAccountShareModel{}).Where("\"grantee_circle_id\" = ?", circleId).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (self *transaction) SharesCircle(userId, otherUserId string) (bool, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return false, nil
	}
	var count int64
	if err := self.tx.Table("circle_member AS mine").
		Joins("JOIN circle_member AS theirs ON theirs.circle_id = mine.circle_id").
		Where("mine.user_id = ? AND theirs.user_id = ?", userId, otherUserId).
		Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (self *transaction) CircleUserIDs(userId string) ([]string, error) {
	userIds := []string{}
	if userId == "" {
		return userIds, nil
	}
	if err := self.tx.Table("circle_member AS mine").
		Joins("JOIN circle_member AS theirs ON theirs.circle_id = mine.circle_id AND theirs.user_id <> mine.user_id").
		Where("mine.user_id = ?", userId).
		Distinct().Order("theirs.user_id ASC").Pluck("theirs.user_id", &userIds).Error; err != nil {
		return nil, err
	}
	return userIds, nil
}

func (self *transaction) ListCirclePeople(userId string) ([]*models.CircleMember, error) {
	var rows []struct {
		UserID   string    `gorm:"column:user_id"`
		JoinedAt time.Time `gorm:"column:joined_at"`
	}
	if userId == "" {
		return []*models.CircleMember{}, nil
	}
	if err := self.tx.Table("circle_member AS mine").
		Select("theirs.user_id AS user_id, MIN(theirs.joined_at) AS joined_at").
		Joins("JOIN circle_member AS theirs ON theirs.circle_id = mine.circle_id AND theirs.user_id <> mine.user_id").
		Where("mine.user_id = ?", userId).
		Group("theirs.user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	userIds := make([]string, 0, len(rows))
	for _, row := range rows {
		userIds = append(userIds, row.UserID)
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	people := make([]*models.CircleMember, 0, len(rows))
	for _, row := range rows {
		if users[row.UserID] == nil {
			continue
		}
		people = append(people, &models.CircleMember{UserID: row.UserID, JoinedAt: row.JoinedAt.In(time.Local), User: users[row.UserID]})
	}
	return people, nil
}

func (self *transaction) RecordCircleInvitationSend(inviterUserId string, sentAt time.Time) error {
	if inviterUserId == "" {
		return ErrInvalidArguments
	}
	// Taken in turns by the person's row, so that two calls at once cannot
	// both find room for the same last one.
	if err := self.tx.Exec(`SELECT 1 FROM "user" WHERE "id" = ? FOR UPDATE`, inviterUserId).Error; err != nil {
		return err
	}
	var sentCount int64
	if err := self.tx.Table("circle_invitation_send").
		Where(`"user_id" = ? AND "sent_at" > ?`, inviterUserId, sentAt.Add(-time.Hour)).
		Count(&sentCount).Error; err != nil {
		return err
	}
	if sentCount >= models.CircleInvitationsPerHourMost {
		return limitReached("you have sent %d invitations in the last hour, the most one person sends; try again later", models.CircleInvitationsPerHourMost)
	}
	if err := self.tx.Exec(`DELETE FROM "circle_invitation_send" WHERE "user_id" = ? AND "sent_at" <= ?`, inviterUserId, sentAt.Add(-24*time.Hour)).Error; err != nil {
		return err
	}
	return self.tx.Exec(`INSERT INTO "circle_invitation_send" ("user_id", "sent_at") VALUES (?, ?)`, inviterUserId, sentAt).Error
}

func (self *transaction) CreateCircleInvitation(invitation *models.CircleInvitation) (*models.CircleInvitation, bool, error) {
	if !invitation.MemberRole.IsValid() || invitation.InvitedUserID == "" {
		return nil, false, ErrInvalidArguments
	}
	rows, err := self.lockedCircleMembers(invitation.CircleID)
	if err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		if row.UserID == invitation.InvitedUserID {
			return nil, false, nil
		}
	}
	var waiting circleInvitationModel
	result := self.tx.Where("\"circle_id\" = ? AND \"invited_user_id\" = ? AND \"answered_at\" IS NULL", invitation.CircleID, invitation.InvitedUserID).
		Limit(1).Find(&waiting)
	if result.Error != nil {
		return nil, false, result.Error
	}
	invitationId := waiting.ID
	isNew := result.RowsAffected == 0
	if isNew {
		invitationId = newID()
		if err := self.tx.Create(&circleInvitationModel{
			ID:              invitationId,
			CircleID:        invitation.CircleID,
			InvitedUserID:   invitation.InvitedUserID,
			InvitedByUserID: nullableUserId(invitation.InvitedByUserID),
			MemberRole:      string(invitation.MemberRole),
			CreatedAt:       invitation.CreatedAt,
			ExpiresAt:       invitation.ExpiresAt,
		}).Error; err != nil {
			return nil, false, err
		}
	} else if err := self.tx.Model(&circleInvitationModel{}).Where("\"id\" = ?", invitationId).Updates(map[string]any{
		"member_role":        string(invitation.MemberRole),
		"invited_by_user_id": nullableUserId(invitation.InvitedByUserID),
		"created_at":         invitation.CreatedAt,
		"expires_at":         invitation.ExpiresAt,
	}).Error; err != nil {
		return nil, false, err
	}
	created, err := self.GetCircleInvitation(invitationId)
	return created, isNew, err
}

func (self *transaction) GetCircleInvitation(invitationId string) (*models.CircleInvitation, error) {
	if invitationId == "" {
		return nil, nil
	}
	var rows []circleInvitationRow
	if err := self.tx.Raw(circleInvitationSelect+` WHERE invitation.id = ?`, invitationId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].toModel(), nil
}

func (self *transaction) listCircleInvitations(condition string, value string, now time.Time) ([]*models.CircleInvitation, error) {
	var rows []circleInvitationRow
	if err := self.tx.Raw(circleInvitationSelect+` WHERE `+condition+` AND invitation.answered_at IS NULL AND invitation.expires_at > ?
		ORDER BY invitation.created_at DESC, invitation.id DESC`, value, now).Scan(&rows).Error; err != nil {
		return nil, err
	}
	invitations := make([]*models.CircleInvitation, 0, len(rows))
	for index := range rows {
		invitations = append(invitations, rows[index].toModel())
	}
	return invitations, nil
}

func (self *transaction) ListCircleInvitationsTo(userId string, now time.Time) ([]*models.CircleInvitation, error) {
	return self.listCircleInvitations("invitation.invited_user_id = ?", userId, now)
}

func (self *transaction) ListCircleInvitationsOf(circleId string, now time.Time) ([]*models.CircleInvitation, error) {
	return self.listCircleInvitations("invitation.circle_id = ?", circleId, now)
}

func (self *transaction) AnswerCircleInvitation(invitationId string, invitationAnswer models.InvitationAnswer, now time.Time) (*models.Circle, error) {
	switch invitationAnswer {
	case models.InvitationAnswerAccepted, models.InvitationAnswerDeclined, models.InvitationAnswerWithdrawn:
	default:
		return nil, ErrInvalidArguments
	}
	// The circle first, as every other change to it, and the invitation as it
	// stands once the circle is held: two answers at once take turns.
	var invitation circleInvitationModel
	if result := self.tx.Where("\"id\" = ?", invitationId).Limit(1).Find(&invitation); result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	rows, err := self.lockedCircleMembers(invitation.CircleID)
	if err != nil {
		return nil, err
	}
	if result := self.tx.Where("\"id\" = ?", invitationId).Limit(1).Find(&invitation); result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected == 0 || invitation.AnsweredAt != nil || !now.Before(invitation.ExpiresAt) {
		return nil, ErrNotFound
	}
	if err := self.tx.Model(&circleInvitationModel{}).Where("\"id\" = ?", invitationId).
		Updates(map[string]any{"answered_at": now, "invitation_answer": string(invitationAnswer)}).Error; err != nil {
		return nil, err
	}
	if invitationAnswer != models.InvitationAnswerAccepted {
		return nil, nil
	}
	for _, row := range rows {
		if row.UserID == invitation.InvitedUserID {
			return self.GetCircle(invitation.CircleID)
		}
	}
	if err := self.tx.Create(&circleMemberModel{
		CircleID: invitation.CircleID, UserID: invitation.InvitedUserID, MemberRole: invitation.MemberRole,
		JoinedAt: now, InvitedByUserID: invitation.InvitedByUserID,
	}).Error; err != nil {
		return nil, err
	}
	if err := self.touchCircle(invitation.CircleID); err != nil {
		return nil, err
	}
	if err := self.joinCircleChat(invitation.CircleID, invitation.InvitedUserID, models.MemberRole(invitation.MemberRole)); err != nil {
		return nil, err
	}
	return self.GetCircle(invitation.CircleID)
}
