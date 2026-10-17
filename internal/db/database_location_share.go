package db

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// LocationShareOperation is who may see whose location, and how much: a
// person's location shared with another person or with a circle at a level,
// as a calendar is.
//
// "How much of this person's location may that person see" has one answer,
// GetLocationShareLevel, and every door asks it. A caller that compares the
// two user ids itself is wrong for a person who left the circle that made the
// share possible.
type LocationShareOperation interface {
	// ShareLocation gives a person or a circle a level of access to the
	// sharer's location, and answers the share as stored. Sharing again with
	// the same grantee changes the level. ErrInvalidArguments for a level
	// nobody can be given, a grantee that is both or neither, or the sharer
	// themselves. ErrNotFound for a person the sharer shares no circle with
	// and for a circle the sharer is not in, which is all the caller says.
	ShareLocation(sharerUserId string, share *models.LocationShare) (*models.LocationShare, error)

	// UnshareLocation takes one of the sharer's shares back. ErrNotFound
	// unless it is the sharer's.
	UnshareLocation(sharerUserId, shareId string) error

	// ListLocationShares are the shares the person has made, oldest first,
	// with who each is for.
	ListLocationShares(sharerUserId string) ([]*models.LocationShare, error)

	// GetLocationShareLevel is how much of the owner's location the viewer
	// may see: LocationShareLevelOwner for their own, the highest of the
	// shares reaching them for another person's, and the empty level for
	// anything else. A share to a person reaches them only while the two
	// still share a circle, and a share to a circle only while both are in it.
	GetLocationShareLevel(viewerUserId, ownerUserId string) (models.LocationShareLevel, error)

	// ListLocationSharers are the people whose location reaches the viewer,
	// once each at the highest level, by name. Not the viewer.
	ListLocationSharers(viewerUserId string) ([]*LocationSharer, error)

	// ListLocationGrantees are the people the owner's location reaches now,
	// once each, who have an agent row their phones hang from. Not the owner,
	// and not a person whose account is disabled or whose roles no longer give
	// location:use: they cannot read the location, so they are not told of it
	// either.
	ListLocationGrantees(ownerUserId string) ([]*LocationGrantee, error)
}

// LocationSharer is a person whose location reaches the viewer.
type LocationSharer struct {
	UserID   string
	Username string
	// Name is the name they gave, or their username.
	Name string
	// ShareLevel is the highest level any share gives the viewer.
	ShareLevel models.LocationShareLevel
	// User holds the fields the picture's address and color are made from.
	User *models.User
}

// LocationGrantee is a person the owner's location reaches, and the agent row
// their phones hang from.
type LocationGrantee struct {
	UserID  string
	AgentID string

	// SharedAt is when the earliest share that reaches them was made. What the
	// owner did before it is not theirs to be told.
	SharedAt time.Time
}

type locationShareModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	UserID          string    `gorm:"column:user_id"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	ShareLevel      string    `gorm:"column:share_level"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	CreatedByUserID *string   `gorm:"column:created_by_user_id"`
}

func (locationShareModel) TableName() string { return "location_share" }

func (self *locationShareModel) toModel() *models.LocationShare {
	share := &models.LocationShare{
		ID:         self.ID,
		UserID:     self.UserID,
		ShareLevel: models.LocationShareLevel(self.ShareLevel),
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

func (self *transaction) ShareLocation(sharerUserId string, share *models.LocationShare) (*models.LocationShare, error) {
	if share == nil || sharerUserId == "" || !share.ShareLevel.IsGrantable() ||
		(share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	if share.GranteeUserID != "" {
		if share.GranteeUserID == sharerUserId {
			return nil, ErrInvalidArguments
		}
		var userCount int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&userCount).Error; err != nil {
			return nil, err
		}
		if userCount == 0 {
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
		var memberRows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&memberRows).Error; err != nil {
			return nil, err
		}
		if len(memberRows) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &locationShareModel{
		ID:              newID(),
		UserID:          sharerUserId,
		GranteeUserID:   nullableText(share.GranteeUserID),
		GranteeCircleID: nullableText(share.GranteeCircleID),
		ShareLevel:      string(share.ShareLevel),
		CreatedAt:       time.Now(),
		CreatedByUserID: &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"share_level"})}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "user_id"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "user_id"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	query := self.tx.Where("\"user_id\" = ?", sharerUserId)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	var kept locationShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

func (self *transaction) UnshareLocation(sharerUserId, shareId string) error {
	if sharerUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"user_id\" = ?", shareId, sharerUserId).Delete(&locationShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) ListLocationShares(sharerUserId string) ([]*models.LocationShare, error) {
	var rows []locationShareModel
	if err := self.tx.Where("\"user_id\" = ?", sharerUserId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
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
	shares := make([]*models.LocationShare, 0, len(rows))
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

// reachingLocationSharesQuery selects the shares that reach one viewer, with
// whose location each is. The joins on circle_member are what make a share
// stop reaching someone the moment the circle it depended on is gone: a share
// to a person needs the two to share a circle, and a share to a circle needs
// both the viewer and the owner to be in it. The join on the owner's account
// is what stops a disabled account's location reaching anybody, and the two
// conditions on the viewer stop it reaching a viewer whose account is disabled
// or whose roles do not give location:use (@permission).
var reachingLocationSharesQuery = `
SELECT share.user_id AS owner_user_id, share.share_level AS share_level
FROM "location_share" share
JOIN "user" owner_user ON owner_user.id = share.user_id AND owner_user.disabled_at IS NULL
WHERE share.user_id <> @viewer
  AND (@owner = '' OR share.user_id = @owner)
  AND EXISTS (SELECT 1 FROM "user" viewer_user WHERE viewer_user.id = @viewer AND viewer_user.disabled_at IS NULL)
  AND ` + userHasPermissionSQL("@viewer") + `
  AND (
    (share.grantee_user_id = @viewer AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @viewer AND theirs.user_id = share.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @viewer)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = share.user_id))
  )`

// userHasPermissionSQL is a condition that is true when the user, an SQL
// expression, has the permission @permission through a group's role.
func userHasPermissionSQL(userExpression string) string {
	return `EXISTS (SELECT 1 FROM "user_group" JOIN "group_role" ON group_role.group_id = user_group.group_id
		JOIN "role_permission" ON role_permission.role_id = group_role.role_id
		WHERE user_group.user_id = ` + userExpression + ` AND role_permission.permission_key = @permission)`
}

type reachingLocationShareRow struct {
	OwnerUserID string `gorm:"column:owner_user_id"`
	ShareLevel  string `gorm:"column:share_level"`
}

// reachingLocationLevels is the highest level each owner shares their
// location with the viewer at, for one owner or, with an empty identifier,
// for all of them.
func (self *transaction) reachingLocationLevels(viewerUserId, ownerUserId string) (map[string]models.LocationShareLevel, error) {
	var rows []reachingLocationShareRow
	if err := self.tx.Raw(reachingLocationSharesQuery,
		sql.Named("viewer", viewerUserId), sql.Named("owner", ownerUserId),
		sql.Named("permission", string(models.PermissionLocationUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	levels := make(map[string]models.LocationShareLevel, len(rows))
	for _, row := range rows {
		level := models.LocationShareLevel(row.ShareLevel)
		if level.Rank() > levels[row.OwnerUserID].Rank() {
			levels[row.OwnerUserID] = level
		}
	}
	return levels, nil
}

func (self *transaction) GetLocationShareLevel(viewerUserId, ownerUserId string) (models.LocationShareLevel, error) {
	if viewerUserId == "" || ownerUserId == "" {
		return "", nil
	}
	if viewerUserId == ownerUserId {
		return models.LocationShareLevelOwner, nil
	}
	levels, err := self.reachingLocationLevels(viewerUserId, ownerUserId)
	if err != nil {
		return "", err
	}
	return levels[ownerUserId], nil
}

func (self *transaction) ListLocationSharers(viewerUserId string) ([]*LocationSharer, error) {
	if viewerUserId == "" {
		return []*LocationSharer{}, nil
	}
	levels, err := self.reachingLocationLevels(viewerUserId, "")
	if err != nil {
		return nil, err
	}
	ownerIds := make([]string, 0, len(levels))
	for ownerId := range levels {
		ownerIds = append(ownerIds, ownerId)
	}
	users, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	sharers := make([]*LocationSharer, 0, len(levels))
	for ownerId, level := range levels {
		user := users[ownerId]
		if user == nil {
			continue
		}
		sharers = append(sharers, &LocationSharer{
			UserID: ownerId, Username: user.Username, Name: user.DisplayName(), ShareLevel: level, User: user,
		})
	}
	sort.Slice(sharers, func(first, second int) bool {
		firstName, secondName := strings.ToLower(sharers[first].Name), strings.ToLower(sharers[second].Name)
		if firstName != secondName {
			return firstName < secondName
		}
		return sharers[first].UserID < sharers[second].UserID
	})
	return sharers, nil
}

func (self *transaction) ListLocationGrantees(ownerUserId string) ([]*LocationGrantee, error) {
	if ownerUserId == "" {
		return []*LocationGrantee{}, nil
	}
	// The same conditions as reachingLocationSharesQuery, read from the
	// owner's side.
	var rows []struct {
		UserID   string    `gorm:"column:user_id"`
		AgentID  string    `gorm:"column:agent_id"`
		SharedAt time.Time `gorm:"column:shared_at"`
	}
	if err := self.tx.Raw(`
SELECT grantee.user_id AS user_id, grantee_agent.id AS agent_id, min(grantee.shared_at) AS shared_at FROM (
  SELECT share.grantee_user_id AS user_id, share.created_at AS shared_at
  FROM "location_share" share
  WHERE share.user_id = @owner AND share.grantee_user_id IS NOT NULL
    AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @owner AND theirs.user_id = share.grantee_user_id)
  UNION ALL
  SELECT member.user_id AS user_id, share.created_at AS shared_at
  FROM "location_share" share
  JOIN "circle_member" member ON member.circle_id = share.grantee_circle_id
  WHERE share.user_id = @owner AND share.grantee_circle_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @owner)
) grantee
JOIN "user" owner_user ON owner_user.id = @owner AND owner_user.disabled_at IS NULL
JOIN "user" grantee_user ON grantee_user.id = grantee.user_id AND grantee_user.disabled_at IS NULL
JOIN "agent" grantee_agent ON grantee_agent.user_id = grantee.user_id
WHERE grantee.user_id <> @owner AND `+userHasPermissionSQL("grantee.user_id")+`
GROUP BY grantee.user_id, grantee_agent.id
ORDER BY grantee.user_id ASC`, sql.Named("owner", ownerUserId),
		sql.Named("permission", string(models.PermissionLocationUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	grantees := make([]*LocationGrantee, 0, len(rows))
	for _, row := range rows {
		grantees = append(grantees, &LocationGrantee{UserID: row.UserID, AgentID: row.AgentID, SharedAt: row.SharedAt.In(time.Local)})
	}
	return grantees, nil
}

// endLocationSharesOutsideCircles deletes the location shares that only a
// circle made possible once the person no longer has it, in both directions;
// the circle code calls it (endSharesOutsideCircles) in the transaction that
// takes a person out of a circle. The reasoning is endCalendarSharesOutsideCircles's:
// a direct share would reach two people again, unchosen, the day they met in
// another circle, and a share to a circle the sharer has left is meaningless.
// A share to a circle that somebody else left stops reaching that person by
// the join in reachingLocationSharesQuery and needs no deleting.
func endLocationSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "location_share" WHERE "id" IN (
		SELECT share.id FROM "location_share" share
		WHERE share.grantee_user_id IS NOT NULL
		  AND (share.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = share.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "location_share" share
		WHERE share.grantee_circle_id IS NOT NULL
		  AND share.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}
