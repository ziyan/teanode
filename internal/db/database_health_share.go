package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// HealthShareOperation is who may see whose Health, and how much: a person's
// Health shared with another person or with a circle by Health category, as a
// location is shared at a level.
//
// What a share gives is the day summaries of the kinds in its categories and
// the list of those kinds, never the readings, the routes or the
// characteristics. "Which categories of this person's Health may that person
// see" has one answer, GetSharedHealthCategories, and every door asks it. A
// caller that compares the two user ids itself is wrong for a person who left
// the circle that made the share possible.
type HealthShareOperation interface {
	// SetHealthShare gives a person or a circle the categories of the sharer's
	// Health, and answers the share as stored. Sharing again with the same
	// grantee replaces the categories. An empty list takes the share back and
	// answers nil, also when there was none. ErrInvalidArguments for a
	// category nobody knows, a grantee that is both or neither, or the sharer
	// themselves. ErrNotFound for a person the sharer shares no circle with
	// and for a circle the sharer is not in, which is all the caller says.
	SetHealthShare(sharerUserId string, share *models.HealthShare) (*models.HealthShare, error)

	// ListHealthShares are the shares the person has made, oldest first, with
	// who each is for.
	ListHealthShares(sharerUserId string) ([]*models.HealthShare, error)

	// ListHealthSharesReaching are the shares that reach the viewer now,
	// oldest first, with whose Health each is. A share to a person reaches
	// them only while the two still share a circle, and a share to a circle
	// only while both are in it. Not the viewer's own.
	ListHealthSharesReaching(viewerUserId string) ([]*models.HealthShare, error)

	// GetSharedHealthCategories are the categories of the owner's Health that
	// the shares reaching the viewer give together, in the order of the Health
	// app, and none for anything else. The owner is not asked about: a person
	// reads their own Health without a share.
	GetSharedHealthCategories(viewerUserId, ownerUserId string) ([]string, error)
}

type healthShareModel struct {
	ID               string         `gorm:"column:id;primaryKey"`
	UserID           string         `gorm:"column:user_id"`
	GranteeUserID    *string        `gorm:"column:grantee_user_id"`
	GranteeCircleID  *string        `gorm:"column:grantee_circle_id"`
	HealthCategories pq.StringArray `gorm:"column:health_categories;type:text[]"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	CreatedByUserID  *string        `gorm:"column:created_by_user_id"`
}

func (healthShareModel) TableName() string { return "health_share" }

func (self *healthShareModel) toModel() *models.HealthShare {
	share := &models.HealthShare{
		ID:               self.ID,
		UserID:           self.UserID,
		HealthCategories: append([]string{}, self.HealthCategories...),
		CreatedAt:        self.CreatedAt.In(time.Local),
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

// orderedHealthCategories is the categories in the order of the Health app,
// each once, or false when one of the texts is not a category.
func orderedHealthCategories(texts []string) ([]string, bool) {
	wanted := map[string]bool{}
	for _, text := range texts {
		if !health.IsKnownCategory(text) {
			return nil, false
		}
		wanted[text] = true
	}
	ordered := make([]string, 0, len(wanted))
	for _, category := range health.Categories() {
		if wanted[string(category)] {
			ordered = append(ordered, string(category))
		}
	}
	return ordered, true
}

func (self *transaction) SetHealthShare(sharerUserId string, share *models.HealthShare) (*models.HealthShare, error) {
	if share == nil || sharerUserId == "" || (share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	if share.GranteeUserID == sharerUserId {
		return nil, ErrInvalidArguments
	}
	categories, isValid := orderedHealthCategories(share.HealthCategories)
	if !isValid {
		return nil, fmt.Errorf("%w: Health is shared by category, and one of those given is not a category", ErrInvalidArguments)
	}

	query := self.tx.Where("\"user_id\" = ?", sharerUserId)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	if len(categories) == 0 {
		if err := query.Delete(&healthShareModel{}).Error; err != nil {
			return nil, err
		}
		return nil, nil
	}

	if share.GranteeUserID != "" {
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

	row := &healthShareModel{
		ID:               newID(),
		UserID:           sharerUserId,
		GranteeUserID:    nullableText(share.GranteeUserID),
		GranteeCircleID:  nullableText(share.GranteeCircleID),
		HealthCategories: categories,
		CreatedAt:        time.Now(),
		CreatedByUserID:  &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"health_categories"})}
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
	var kept healthShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

// nameHealthShares fills in the names the listings show: whose Health each
// share is, and who it is for.
func nameHealthShares(tx *gorm.DB, shares []*models.HealthShare) error {
	userIds, circleIds := []string{}, []string{}
	for _, share := range shares {
		userIds = append(userIds, share.UserID)
		if share.GranteeUserID != "" {
			userIds = append(userIds, share.GranteeUserID)
		}
		if share.GranteeCircleID != "" {
			circleIds = append(circleIds, share.GranteeCircleID)
		}
	}
	users, err := loadUsersById(tx, userIds)
	if err != nil {
		return err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for _, share := range shares {
		if user := users[share.UserID]; user != nil {
			share.OwnerUsername, share.OwnerName = user.Username, user.DisplayName()
		}
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUsername, share.GranteeName = user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
	}
	return nil
}

func (self *transaction) ListHealthShares(sharerUserId string) ([]*models.HealthShare, error) {
	var rows []healthShareModel
	if err := self.tx.Where("\"user_id\" = ?", sharerUserId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.HealthShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameHealthShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

// reachingHealthSharesQuery selects the shares that reach one viewer, with
// whose Health each is. It is reachingLocationSharesQuery for Health: the
// joins on circle_member are what make a share stop reaching someone the
// moment the circle it depended on is gone, the join on the owner's account
// stops a disabled account's Health reaching anybody, and the owner and the
// viewer both need health:use (@permission), so that an operator who takes it
// from a role has switched Health off for those people and what they stored is
// no longer read by others.
var reachingHealthSharesQuery = `
SELECT share.id AS id
FROM "health_share" share
JOIN "user" owner_user ON owner_user.id = share.user_id AND owner_user.disabled_at IS NULL
WHERE share.user_id <> @viewer
  AND ` + userHasPermissionSQL("share.user_id") + `
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

// reachingHealthShares are the shares that reach the viewer, for one owner or,
// with an empty identifier, for all of them.
func (self *transaction) reachingHealthShares(viewerUserId, ownerUserId string) ([]*models.HealthShare, error) {
	var shareIds []string
	if err := self.tx.Raw(reachingHealthSharesQuery,
		sql.Named("viewer", viewerUserId), sql.Named("owner", ownerUserId),
		sql.Named("permission", string(models.PermissionHealthUse))).Scan(&shareIds).Error; err != nil {
		return nil, err
	}
	if len(shareIds) == 0 {
		return []*models.HealthShare{}, nil
	}
	var rows []healthShareModel
	if err := self.tx.Where("\"id\" IN ?", shareIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.HealthShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	return shares, nil
}

func (self *transaction) ListHealthSharesReaching(viewerUserId string) ([]*models.HealthShare, error) {
	if viewerUserId == "" {
		return []*models.HealthShare{}, nil
	}
	shares, err := self.reachingHealthShares(viewerUserId, "")
	if err != nil {
		return nil, err
	}
	if err := nameHealthShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func (self *transaction) GetSharedHealthCategories(viewerUserId, ownerUserId string) ([]string, error) {
	if viewerUserId == "" || ownerUserId == "" || viewerUserId == ownerUserId {
		return []string{}, nil
	}
	shares, err := self.reachingHealthShares(viewerUserId, ownerUserId)
	if err != nil {
		return nil, err
	}
	var categories []string
	for _, share := range shares {
		categories = append(categories, share.HealthCategories...)
	}
	ordered, _ := orderedHealthCategories(categories)
	return ordered, nil
}

// endHealthSharesOutsideCircles deletes the Health shares that only a circle
// made possible once the person no longer has it, in both directions; the
// circle code calls it (endSharesOutsideCircles) in the transaction that takes
// a person out of a circle. The reasoning is that of
// endLocationSharesOutsideCircles: a direct share would reach two people again,
// unchosen, the day they met in another circle, and a share to a circle the
// sharer has left is meaningless. A share to a circle that somebody else left
// stops reaching that person by the join in reachingHealthSharesQuery and needs
// no deleting.
func endHealthSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "health_share" WHERE "id" IN (
		SELECT share.id FROM "health_share" share
		WHERE share.grantee_user_id IS NOT NULL
		  AND (share.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = share.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "health_share" share
		WHERE share.grantee_circle_id IS NOT NULL
		  AND share.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}
