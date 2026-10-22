package db

import (
	"database/sql"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/models"
)

// CalendarShareOperation is who may read and write which calendar: a person's
// calendar shared with another person or with a circle at a level, and the
// calendar a circle keeps for itself.
//
// "May this person read or write this calendar, and at what level" has one
// answer, CalendarAccess, and every door asks it: the dashboard's API, CalDAV,
// the command line and the agent. A caller that compares a calendar's UserID
// with the person instead is wrong for a circle's calendar, which has none.
type CalendarShareOperation interface {
	// CalendarAccess is what the person may do with the calendar:
	// ShareLevelOwner and true for their own, ShareLevelEdit for every member
	// of the circle a circle calendar belongs to, the highest of the shares
	// reaching them for another person's, and the empty level for anything
	// else. A share to a circle reaches a member only while the calendar's
	// owner is still in that circle, and a share to a person only while the
	// two still share a circle.
	CalendarAccess(principalUserId string, found *models.Calendar) (models.ShareLevel, bool, error)

	// ListReachableCalendars are every calendar the person reaches, oldest
	// first within each group: their own, then their circles' calendars, then
	// the ones shared with them at any level.
	ListReachableCalendars(userId string) ([]*models.ReachableCalendar, error)

	// FindCalendar is the person's own calendar of a kind, or nil. Unlike
	// EnsureCalendar it makes nothing.
	FindCalendar(userId string, kind models.CalendarKind) (*models.Calendar, error)

	// CreateCircleCalendar makes a circle's calendar of events. ErrNotFound
	// when there is no such circle, ErrAlreadyExists when it has one: a circle
	// has one.
	CreateCircleCalendar(circleId, name, timezone string) (*models.Calendar, error)

	// GetCircleCalendar is the circle's calendar, or nil.
	GetCircleCalendar(circleId string) (*models.Calendar, error)

	// ShareCalendar gives a person or a circle a level of access to the
	// sharer's own events calendar, or changes the level they have. ErrNotFound
	// unless the calendar is the sharer's and, for a person, the two share a
	// circle, or, for a circle, the sharer is in it. ErrInvalidArguments for a
	// level that cannot be given or a grantee that is not exactly one.
	ShareCalendar(sharerUserId string, share *models.CalendarShare) (*models.CalendarShare, error)

	// GetCalendarShare is one share, or nil.
	GetCalendarShare(shareId string) (*models.CalendarShare, error)

	// ListCalendarShares are a calendar's shares with who they are for, by
	// who they were made for.
	ListCalendarShares(calendarId string) ([]*models.CalendarShare, error)

	// DeleteCalendarShare takes one back. ErrNotFound when there is none.
	DeleteCalendarShare(shareId string) error

	// ReadCalendarObject is GetCalendarObject for a reader at a level: the
	// owner's and an editor's get the event as it is, and anybody else's what
	// calendar.Redact leaves. This and ReadCalendarObjects are the only places
	// a reader's view of an event is made.
	ReadCalendarObject(calendarId, objectId string, shareLevel models.ShareLevel) (*models.CalendarObject, error)

	// ReadCalendarObjects is ListCalendarObjects for a reader at a level.
	ReadCalendarObjects(calendarId string, shareLevel models.ShareLevel) ([]*models.CalendarObject, error)
}

// nullableText is a value for a nullable column: nil when it is empty.
func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type calendarShareModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	CalendarID      string    `gorm:"column:calendar_id"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	ShareLevel      string    `gorm:"column:share_level"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	CreatedByUserID *string   `gorm:"column:created_by_user_id"`
}

func (calendarShareModel) TableName() string { return "calendar_share" }

func (self *calendarShareModel) toModel() *models.CalendarShare {
	share := &models.CalendarShare{
		ID:         self.ID,
		CalendarID: self.CalendarID,
		ShareLevel: models.ShareLevel(self.ShareLevel),
		CreatedAt:  self.CreatedAt.In(time.Local),
	}
	if self.GranteeUserID != nil {
		share.GranteeUserID = *self.GranteeUserID
	}
	if self.GranteeCircleID != nil {
		share.GranteeCircleID = *self.GranteeCircleID
	}
	if self.CreatedByUserID != nil {
		share.CreatedByUserID = *self.CreatedByUserID
	}
	return share
}

// reachingSharesQuery selects the shares that reach one person, with the
// calendar each is for. The joins on circle_member are what make a share stop
// reaching someone the moment the circle it depended on is gone: a share to a
// person needs the two to share a circle, and a share to a circle needs both
// the reader and the calendar's owner to be in it. The join on the owner's
// account is what stops a disabled account's calendar reaching anybody. Only
// the events calendar of a person is ever reached this way; reminders are
// never shared.
const reachingSharesQuery = `
SELECT share.id AS share_id, share.calendar_id AS calendar_id, share.share_level AS share_level,
  share.grantee_user_id AS grantee_user_id, share.grantee_circle_id AS grantee_circle_id, share.created_at AS created_at
FROM "calendar_share" share
JOIN "calendar" shared ON shared.id = share.calendar_id
JOIN "user" owner_user ON owner_user.id = shared.user_id AND owner_user.disabled_at IS NULL
WHERE shared.user_id IS NOT NULL
  AND shared.user_id <> @reader
  AND shared.calendar_kind = 'events'
  AND (@calendar = '' OR share.calendar_id = @calendar)
  AND (
    (share.grantee_user_id = @reader AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @reader AND theirs.user_id = shared.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @reader)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = shared.user_id))
  )`

type reachingShareRow struct {
	ShareID         string    `gorm:"column:share_id"`
	CalendarID      string    `gorm:"column:calendar_id"`
	ShareLevel      string    `gorm:"column:share_level"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

// reachingShares is the highest level each calendar is shared with the person
// at, for one calendar or, with an empty identifier, for all of them.
func (self *transaction) reachingShares(readerUserId, calendarId string) (map[string]models.ShareLevel, error) {
	var rows []reachingShareRow
	if err := self.tx.Raw(reachingSharesQuery,
		sql.Named("reader", readerUserId), sql.Named("calendar", calendarId)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	levels := make(map[string]models.ShareLevel, len(rows))
	for _, row := range rows {
		levels[row.CalendarID] = models.HighestShareLevel(levels[row.CalendarID], models.ShareLevel(row.ShareLevel))
	}
	return levels, nil
}

func (self *transaction) CalendarAccess(principalUserId string, found *models.Calendar) (models.ShareLevel, bool, error) {
	if found == nil || principalUserId == "" {
		return "", false, nil
	}
	if found.UserID != "" && found.UserID == principalUserId {
		return models.ShareLevelOwner, true, nil
	}
	if found.CircleID != "" {
		var count int64
		if err := self.tx.Model(&circleMemberModel{}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", found.CircleID, principalUserId).Count(&count).Error; err != nil {
			return "", false, err
		}
		if count == 0 {
			return "", false, nil
		}
		return models.ShareLevelEdit, false, nil
	}
	levels, err := self.reachingShares(principalUserId, found.ID)
	if err != nil {
		return "", false, err
	}
	return levels[found.ID], false, nil
}

func (self *transaction) FindCalendar(userId string, kind models.CalendarKind) (*models.Calendar, error) {
	return self.calendarOfKind(userId, kind)
}

func (self *transaction) ListReachableCalendars(userId string) ([]*models.ReachableCalendar, error) {
	reachable := []*models.ReachableCalendar{}
	if userId == "" {
		return reachable, nil
	}
	owned, err := self.ListCalendars(userId)
	if err != nil {
		return nil, err
	}
	for _, found := range owned {
		reachable = append(reachable, &models.ReachableCalendar{
			Calendar: found, ShareLevel: models.ShareLevelOwner, OwnerUserID: userId,
		})
	}

	// The circles' own, with the circle's name for a screen that says whose
	// they are.
	var circleCalendars []calendarModel
	if err := self.tx.Raw(`SELECT calendar.* FROM "calendar" calendar
		JOIN "circle_member" member ON member.circle_id = calendar.circle_id AND member.user_id = ?
		ORDER BY calendar.created_at ASC, calendar.id ASC`, userId).Scan(&circleCalendars).Error; err != nil {
		return nil, err
	}
	circleIds := make([]string, 0, len(circleCalendars))
	for _, row := range circleCalendars {
		if row.CircleID != nil {
			circleIds = append(circleIds, *row.CircleID)
		}
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return nil, err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for index := range circleCalendars {
		found := circleCalendars[index].toModel()
		reachable = append(reachable, &models.ReachableCalendar{
			Calendar: found, ShareLevel: models.ShareLevelEdit, CircleName: circleNames[found.CircleID],
		})
	}

	levels, err := self.reachingShares(userId, "")
	if err != nil {
		return nil, err
	}
	if len(levels) == 0 {
		return reachable, nil
	}
	calendarIds := make([]string, 0, len(levels))
	for calendarId := range levels {
		calendarIds = append(calendarIds, calendarId)
	}
	var shared []calendarModel
	if err := self.tx.Where("\"id\" IN ?", calendarIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&shared).Error; err != nil {
		return nil, err
	}
	ownerIds := make([]string, 0, len(shared))
	for _, row := range shared {
		if row.UserID != nil {
			ownerIds = append(ownerIds, *row.UserID)
		}
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	for index := range shared {
		found := shared[index].toModel()
		entry := &models.ReachableCalendar{Calendar: found, ShareLevel: levels[found.ID], OwnerUserID: found.UserID}
		entry.Owner = owners[found.UserID]
		reachable = append(reachable, entry)
	}
	return reachable, nil
}

func (self *transaction) GetCircleCalendar(circleId string) (*models.Calendar, error) {
	if circleId == "" {
		return nil, nil
	}
	var found []calendarModel
	if err := self.tx.Where("\"circle_id\" = ? AND \"calendar_kind\" = ?", circleId, string(models.CalendarEvents)).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) CreateCircleCalendar(circleId, name, timezone string) (*models.Calendar, error) {
	// The circle membership lock comes before the circle's row, as in every
	// change to circles (see circleMembershipLockKey). Taking the row first
	// would let this wait for a request that holds the membership lock and
	// waits for this row: a person leaving the circle against a Passwords
	// change made after this one in the same request.
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	var locked circleModel
	if err := lockRow(self.tx, &locked, circleId); err != nil {
		return nil, err
	}
	existing, err := self.GetCircleCalendar(circleId)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAlreadyExists
	}
	if strings.TrimSpace(name) == "" {
		name = locked.Name
	}
	return self.CreateCalendar(&models.Calendar{
		CircleID: circleId, Name: name, Timezone: timezone, CalendarKind: models.CalendarEvents,
	})
}

// holdMutualCircle holds the circle membership rows two people's sharing
// rests on, so that a transaction taking one of them out of the circle waits
// for this one to finish and then deletes the share made meanwhile. It
// reports whether the two are in a circle together.
func (self *transaction) holdMutualCircle(userId, otherUserId string) (bool, error) {
	// In the order of the circle changes themselves (see
	// circleMembershipLockKey): a share made while somebody leaves the last
	// circle the two had in common is made before the leaving, and cleaned up
	// by it, or after, and refused.
	if err := lockCircleMembership(self.tx); err != nil {
		return false, err
	}
	var rows []struct {
		UserID string `gorm:"column:user_id"`
	}
	if err := self.tx.Raw(`SELECT mine.user_id AS user_id FROM "circle_member" mine
		JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		WHERE mine.user_id = ? AND theirs.user_id = ? LIMIT 1 FOR SHARE OF mine, theirs`, userId, otherUserId).Scan(&rows).Error; err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func (self *transaction) ShareCalendar(sharerUserId string, share *models.CalendarShare) (*models.CalendarShare, error) {
	if share == nil || sharerUserId == "" || !share.ShareLevel.IsGrantable() ||
		(share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	found, err := self.LockCalendar(share.CalendarID)
	if err != nil {
		return nil, err
	}
	// Only a person's own events calendar is shared. A circle's already
	// belongs to everybody in it, and reminders are nobody else's.
	if found == nil || found.UserID != sharerUserId || found.CalendarKind != models.CalendarEvents {
		return nil, ErrNotFound
	}
	if share.GranteeUserID != "" {
		if share.GranteeUserID == sharerUserId {
			return nil, ErrInvalidArguments
		}
		var count int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNotFound
		}
		isMutual, err := self.holdMutualCircle(sharerUserId, share.GranteeUserID)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	} else {
		var rows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &calendarShareModel{
		ID:              newID(),
		CalendarID:      found.ID,
		GranteeUserID:   nullableText(share.GranteeUserID),
		GranteeCircleID: nullableText(share.GranteeCircleID),
		ShareLevel:      string(share.ShareLevel),
		CreatedAt:       time.Now(),
		CreatedByUserID: &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"share_level"})}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "calendar_id"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "calendar_id"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	query := self.tx.Where("\"calendar_id\" = ?", found.ID)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	var kept calendarShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

func (self *transaction) GetCalendarShare(shareId string) (*models.CalendarShare, error) {
	if shareId == "" {
		return nil, nil
	}
	var found []calendarShareModel
	if err := self.tx.Where("\"id\" = ?", shareId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListCalendarShares(calendarId string) ([]*models.CalendarShare, error) {
	var rows []calendarShareModel
	if err := self.tx.Where("\"calendar_id\" = ?", calendarId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	userIds, circleIds := []string{}, []string{}
	for _, row := range rows {
		if row.GranteeUserID != nil {
			userIds = append(userIds, *row.GranteeUserID)
		}
		if row.GranteeCircleID != nil {
			circleIds = append(circleIds, *row.GranteeCircleID)
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return nil, err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	shares := make([]*models.CalendarShare, 0, len(rows))
	for index := range rows {
		share := rows[index].toModel()
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUsername, share.GranteeName = user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
		shares = append(shares, share)
	}
	return shares, nil
}

func (self *transaction) DeleteCalendarShare(shareId string) error {
	result := self.tx.Where("\"id\" = ?", shareId).Delete(&calendarShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// endCalendarSharesOutsideCircles deletes the shares that only a circle
// made possible once the person no longer has it, in both directions; the
// circle code calls it (endSharesOutsideCircles) in the transaction that
// takes a person out of a circle.
//
// Two kinds of share are left over when somebody leaves a circle. A direct
// share between two people made possible by the circle they shared, which
// would reach them again, unchosen, the day they met in another circle. And a
// share the person made to a circle they are no longer in. A share to a circle
// that somebody else left stops reaching that person by the join in
// reachingSharesQuery, and needs no deleting.
func endCalendarSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "calendar_share" WHERE "id" IN (
		SELECT share.id FROM "calendar_share" share
		JOIN "calendar" shared ON shared.id = share.calendar_id
		WHERE share.grantee_user_id IS NOT NULL
		  AND (shared.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = shared.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "calendar_share" share
		JOIN "calendar" shared ON shared.id = share.calendar_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND shared.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}

// redactedFor is an event as a reader at the level may see it. The one
// place the reader's view of an event is made, so that every door that reads
// events for somebody who is not the owner shows the same thing.
func redactedFor(object *models.CalendarObject, shareLevel models.ShareLevel) *models.CalendarObject {
	if object == nil {
		return nil
	}
	redacted := calendar.Redact(*object, shareLevel)
	return &redacted
}

func (self *transaction) ReadCalendarObject(calendarId, objectId string, shareLevel models.ShareLevel) (*models.CalendarObject, error) {
	object, err := self.GetCalendarObject(calendarId, objectId)
	if err != nil {
		return nil, err
	}
	if object != nil && shareLevel == models.ShareLevelEdit {
		isCircleCalendar, err := self.isCircleCalendar(calendarId)
		if err != nil {
			return nil, err
		}
		if isCircleCalendar {
			return object, nil
		}
	}
	return redactedFor(object, shareLevel), nil
}

func (self *transaction) ReadCalendarObjects(calendarId string, shareLevel models.ShareLevel) ([]*models.CalendarObject, error) {
	objects, err := self.ListCalendarObjects(calendarId)
	if err != nil {
		return nil, err
	}
	if shareLevel == models.ShareLevelEdit {
		isCircleCalendar, err := self.isCircleCalendar(calendarId)
		if err != nil {
			return nil, err
		}
		if isCircleCalendar {
			return objects, nil
		}
	}
	for index, object := range objects {
		objects[index] = redactedFor(object, shareLevel)
	}
	return objects, nil
}

// isCircleCalendar reports whether a calendar belongs to a circle. Its events
// are every member's to read and write, so there is no owner to keep a private
// event from: calendar.Redact would otherwise make an event marked private
// busy to every member, the one who wrote it included.
func (self *transaction) isCircleCalendar(calendarId string) (bool, error) {
	var count int64
	if err := self.tx.Model(&calendarModel{}).
		Where("\"id\" = ? AND \"circle_id\" IS NOT NULL", calendarId).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
