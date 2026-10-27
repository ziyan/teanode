package db

import (
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ShoppingOperation is shopping lists: a person's named lists of things to
// buy, shared with a person or a circle, checked off at the store and by a
// receipt, and remembered with their prices.
//
// "May this person read and change this list" has one answer,
// GetShoppingListAccess, and every operation that takes a list or an item
// asks it. A list reaches its owner and the people its shares reach: a share
// to a person while the two share a circle, a share to a circle while both
// the reader and the owner are in it, and only while the owner is enabled
// and has shopping:use. Everybody it reaches may do everything to it except
// share it further and delete it, which are the owner's.
type ShoppingOperation interface {
	// GetShoppingListAccess is how the person reaches the list. ErrNotFound
	// when there is no such list or it does not reach them.
	GetShoppingListAccess(userId, shoppingListId string) (*models.ShoppingListAccess, error)

	// CreateShoppingList makes a list of the person's.
	CreateShoppingList(userId, listName string) (*models.ShoppingList, error)

	// RenameShoppingList renames a list the person reaches.
	RenameShoppingList(userId, shoppingListId, listName string) (*models.ShoppingList, error)

	// DeleteShoppingList deletes one of the person's own lists with its
	// items, prices and shares. ErrNotFound unless it is theirs.
	DeleteShoppingList(userId, shoppingListId string) error

	// ListShoppingLists are the lists the person reaches, their own first,
	// then by name, each with its counts.
	ListShoppingLists(userId string) ([]*models.ShoppingList, error)

	// GetShoppingList is one list the person reaches, with its counts.
	GetShoppingList(userId, shoppingListId string) (*models.ShoppingList, error)

	// ShareShoppingList gives a person or a circle one of the sharer's own
	// lists. ErrNotFound unless the list is the sharer's and, for a person,
	// the two share a circle, or, for a circle, the sharer is in it.
	// ErrInvalidArguments for a grantee that is not exactly one, or the
	// sharer themselves. Sharing again with the same grantee answers the
	// share there is.
	ShareShoppingList(sharerUserId string, share *models.ShoppingListShare) (*models.ShoppingListShare, error)

	// ListShoppingListShares are a list's shares, oldest first, with who each
	// is for. ErrNotFound unless the list is the person's.
	ListShoppingListShares(userId, shoppingListId string) ([]*models.ShoppingListShare, error)

	// ListShoppingListSharesBy are the shares the person has made of their
	// own lists, oldest first.
	ListShoppingListSharesBy(ownerUserId string) ([]*models.ShoppingListShare, error)

	// ListShoppingListSharesReaching are the shares that reach the person
	// now, oldest first: not their own lists.
	ListShoppingListSharesReaching(userId string) ([]*models.ShoppingListShare, error)

	// DeleteShoppingListShare takes one of the owner's shares back.
	// ErrNotFound when there is none, or it is of somebody else's list.
	DeleteShoppingListShare(ownerUserId, shoppingListShareId string) error

	// DeclineShoppingListShare ends a share made to the person by name.
	// ErrNotFound for any other.
	DeclineShoppingListShare(granteeUserId, shoppingListShareId string) error

	// ListShoppingListReaderUserIDs are everybody the list reaches now, its
	// owner included, once each.
	ListShoppingListReaderUserIDs(shoppingListId string) ([]string, error)

	// SetShoppingListReminder says whether the person is reminded of the
	// list when they arrive at a store. On until they turn it off.
	SetShoppingListReminder(userId, shoppingListId string, isReminded bool) error

	// ListShoppingListReminderMuters are the people who muted the list.
	ListShoppingListReminderMuters(shoppingListId string) ([]string, error)

	// SetShoppingStoreReminder says whether the person is reminded at a
	// store at all.
	SetShoppingStoreReminder(userId, storeName string, isReminded bool) error

	// ListShoppingStoreMuteKeys are the store keys the person asked never to
	// be reminded at.
	ListShoppingStoreMuteKeys(userId string) ([]string, error)

	ShoppingItemOperation
	ShoppingTripOperation
	ShoppingReceiptOperation
}

// The bounds on what one person keeps, so that a script cannot fill the
// database through a list.
const (
	shoppingListsMost          = 200
	shoppingItemsPerListMost   = 2000
	shoppingListNameLengthMost = 200
)

type shoppingListModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	ListName  string    `gorm:"column:list_name"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (shoppingListModel) TableName() string { return "shopping_list" }

func (self *shoppingListModel) toModel() *models.ShoppingList {
	return &models.ShoppingList{ID: self.ID, UserID: self.UserID, ListName: self.ListName, CreatedAt: self.CreatedAt.UTC(), UpdatedAt: self.UpdatedAt.UTC()}
}

type shoppingListShareModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	ShoppingListID  string    `gorm:"column:shopping_list_id"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	CreatedByUserID *string   `gorm:"column:created_by_user_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (shoppingListShareModel) TableName() string { return "shopping_list_share" }

func (self *shoppingListShareModel) toModel() *models.ShoppingListShare {
	share := &models.ShoppingListShare{ID: self.ID, ShoppingListID: self.ShoppingListID, CreatedAt: self.CreatedAt.UTC()}
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

type shoppingListReminderMuteModel struct {
	ShoppingListID string    `gorm:"column:shopping_list_id;primaryKey"`
	UserID         string    `gorm:"column:user_id;primaryKey"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (shoppingListReminderMuteModel) TableName() string { return "shopping_list_reminder_mute" }

type shoppingStoreMuteModel struct {
	UserID    string    `gorm:"column:user_id;primaryKey"`
	StoreKey  string    `gorm:"column:store_key;primaryKey"`
	StoreName string    `gorm:"column:store_name"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (shoppingStoreMuteModel) TableName() string { return "shopping_store_mute" }

// shoppingListReachQuery selects the lists one person reaches, and how: the
// lists they own, and the shares that reach them. With a list named it is
// that list only. This is the one statement that says who reaches a list; the
// joins on circle_member are what make a share stop reaching somebody the
// moment the circle it depended on is gone, and the joins on the accounts what
// make a disabled owner's lists, or those of an owner or a reader without
// shopping:use, reach nobody, the owner included: such a person is reminded
// of nothing and no receipt of theirs checks anything off.
var shoppingListReachQuery = `
SELECT list.id AS shopping_list_id, list.user_id AS owner_user_id, true AS is_owner
FROM "shopping_list" list
JOIN "user" owner_user ON owner_user.id = list.user_id AND owner_user.disabled_at IS NULL
WHERE list.user_id = @reader AND (@list = '' OR list.id = @list)
  AND ` + userHasPermissionSQL("list.user_id") + `
UNION ALL
SELECT list.id, list.user_id, false
FROM "shopping_list_share" share
JOIN "shopping_list" list ON list.id = share.shopping_list_id
JOIN "user" owner_user ON owner_user.id = list.user_id AND owner_user.disabled_at IS NULL
WHERE list.user_id <> @reader AND (@list = '' OR list.id = @list)
  AND ` + userHasPermissionSQL("list.user_id") + `
  AND EXISTS (SELECT 1 FROM "user" reader_user WHERE reader_user.id = @reader AND reader_user.disabled_at IS NULL)
  AND ` + userHasPermissionSQL("@reader") + `
  AND (
    (share.grantee_user_id = @reader AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @reader AND theirs.user_id = list.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @reader)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = list.user_id))
  )`

type shoppingListReachRow struct {
	ShoppingListID string `gorm:"column:shopping_list_id"`
	OwnerUserID    string `gorm:"column:owner_user_id"`
	IsOwner        bool   `gorm:"column:is_owner"`
}

// shoppingListReach is, for each list the person reaches (or the one they
// name), how they reach it.
func (self *transaction) shoppingListReach(userId, shoppingListId string) (map[string]*models.ShoppingListAccess, error) {
	reach := map[string]*models.ShoppingListAccess{}
	if userId == "" {
		return reach, nil
	}
	var rows []shoppingListReachRow
	if err := self.tx.Raw(shoppingListReachQuery, sql.Named("reader", userId), sql.Named("list", shoppingListId),
		sql.Named("permission", string(models.PermissionShoppingUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		found := reach[row.ShoppingListID]
		if found == nil {
			found = &models.ShoppingListAccess{ShoppingListID: row.ShoppingListID, OwnerUserID: row.OwnerUserID}
			reach[row.ShoppingListID] = found
		}
		found.IsOwner = found.IsOwner || row.IsOwner
	}
	return reach, nil
}

// reachableShoppingListIds are the identifiers of every list the person
// reaches.
func (self *transaction) reachableShoppingListIds(userId string) ([]string, error) {
	reach, err := self.shoppingListReach(userId, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(reach))
	for id := range reach {
		ids = append(ids, id)
	}
	return ids, nil
}

func (self *transaction) GetShoppingListAccess(userId, shoppingListId string) (*models.ShoppingListAccess, error) {
	if userId == "" || shoppingListId == "" {
		return nil, ErrNotFound
	}
	reach, err := self.shoppingListReach(userId, shoppingListId)
	if err != nil {
		return nil, err
	}
	found := reach[shoppingListId]
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

// cleanShoppingListName is a list's name as kept: trimmed, not empty, and not
// longer than the column.
func cleanShoppingListName(listName string) (string, error) {
	listName = strings.TrimSpace(listName)
	if listName == "" || len([]rune(listName)) > shoppingListNameLengthMost {
		return "", fmt.Errorf("%w: a list's name is 1 to %d characters", ErrInvalidArguments, shoppingListNameLengthMost)
	}
	return listName, nil
}

func (self *transaction) CreateShoppingList(userId, listName string) (*models.ShoppingList, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	listName, err := cleanShoppingListName(listName)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := self.tx.Model(&shoppingListModel{}).Where("\"user_id\" = ?", userId).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= shoppingListsMost {
		return nil, limitReached("you have %d shopping lists, the most one person keeps; delete one you no longer use first", shoppingListsMost)
	}
	now := time.Now()
	row := &shoppingListModel{ID: newID(), UserID: userId, ListName: listName, CreatedAt: now, UpdatedAt: now}
	if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	return self.GetShoppingList(userId, row.ID)
}

func (self *transaction) RenameShoppingList(userId, shoppingListId, listName string) (*models.ShoppingList, error) {
	if _, err := self.GetShoppingListAccess(userId, shoppingListId); err != nil {
		return nil, err
	}
	listName, err := cleanShoppingListName(listName)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Model(&shoppingListModel{}).Where("\"id\" = ?", shoppingListId).
		Updates(map[string]any{"list_name": listName, "updated_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	return self.GetShoppingList(userId, shoppingListId)
}

func (self *transaction) DeleteShoppingList(userId, shoppingListId string) error {
	if userId == "" || shoppingListId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"user_id\" = ?", shoppingListId, userId).Delete(&shoppingListModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// touchShoppingList marks a list changed, for the order of a listing and for
// a client that asks what changed since.
func (self *transaction) touchShoppingList(shoppingListId string) error {
	return self.tx.Model(&shoppingListModel{}).Where("\"id\" = ?", shoppingListId).Update("updated_at", time.Now()).Error
}

func (self *transaction) ListShoppingLists(userId string) ([]*models.ShoppingList, error) {
	reach, err := self.shoppingListReach(userId, "")
	if err != nil {
		return nil, err
	}
	return self.describeShoppingLists(userId, reach)
}

func (self *transaction) GetShoppingList(userId, shoppingListId string) (*models.ShoppingList, error) {
	access, err := self.GetShoppingListAccess(userId, shoppingListId)
	if err != nil {
		return nil, err
	}
	lists, err := self.describeShoppingLists(userId, map[string]*models.ShoppingListAccess{shoppingListId: access})
	if err != nil {
		return nil, err
	}
	if len(lists) == 0 {
		return nil, ErrNotFound
	}
	return lists[0], nil
}

// describeShoppingLists reads the lists the reach names with what a listing
// shows of each: whose it is, its counts, whether it is shared and whether
// the reader is reminded of it. The reader's own come first, then by name.
func (self *transaction) describeShoppingLists(userId string, reach map[string]*models.ShoppingListAccess) ([]*models.ShoppingList, error) {
	lists := []*models.ShoppingList{}
	if len(reach) == 0 {
		return lists, nil
	}
	ids := make([]string, 0, len(reach))
	for id := range reach {
		ids = append(ids, id)
	}
	var rows []shoppingListModel
	if err := self.tx.Where("\"id\" IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	var counts []struct {
		ShoppingListID    string `gorm:"column:shopping_list_id"`
		ShoppingItemState string `gorm:"column:shopping_item_state"`
		ItemCount         int    `gorm:"column:item_count"`
	}
	if err := self.tx.Raw(`SELECT "shopping_list_id", "shopping_item_state", count(*) AS item_count FROM "shopping_item"
		WHERE "shopping_list_id" IN ? GROUP BY "shopping_list_id", "shopping_item_state"`, ids).Scan(&counts).Error; err != nil {
		return nil, err
	}
	var sharedIds []string
	if err := self.tx.Model(&shoppingListShareModel{}).Where("\"shopping_list_id\" IN ?", ids).Distinct().Pluck("shopping_list_id", &sharedIds).Error; err != nil {
		return nil, err
	}
	var mutedIds []string
	if err := self.tx.Model(&shoppingListReminderMuteModel{}).Where("\"user_id\" = ? AND \"shopping_list_id\" IN ?", userId, ids).
		Pluck("shopping_list_id", &mutedIds).Error; err != nil {
		return nil, err
	}
	ownerIds := make([]string, 0, len(rows))
	for _, row := range rows {
		ownerIds = append(ownerIds, row.UserID)
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	isShared := map[string]bool{}
	for _, id := range sharedIds {
		isShared[id] = true
	}
	isMuted := map[string]bool{}
	for _, id := range mutedIds {
		isMuted[id] = true
	}
	byId := map[string]*models.ShoppingList{}
	for index := range rows {
		list := rows[index].toModel()
		access := reach[list.ID]
		list.IsOwner = access != nil && access.IsOwner
		list.IsShared = isShared[list.ID]
		list.IsReminded = !isMuted[list.ID]
		if owner := owners[list.UserID]; owner != nil {
			list.OwnerUsername, list.OwnerName = owner.Username, owner.DisplayName()
		}
		byId[list.ID] = list
		lists = append(lists, list)
	}
	for _, count := range counts {
		list := byId[count.ShoppingListID]
		if list == nil {
			continue
		}
		switch models.ShoppingItemState(count.ShoppingItemState) {
		case models.ShoppingItemStateNeeded:
			list.NeededCount = count.ItemCount
		case models.ShoppingItemStateInCart:
			list.InCartCount = count.ItemCount
		case models.ShoppingItemStateNotNeeded:
			list.NotNeededCount = count.ItemCount
		}
	}
	sortShoppingLists(lists)
	return lists, nil
}

// sortShoppingLists puts the reader's own lists first, then everything by
// name and, for two of one name, by when it was made.
func sortShoppingLists(lists []*models.ShoppingList) {
	less := func(left, right *models.ShoppingList) bool {
		if left.IsOwner != right.IsOwner {
			return left.IsOwner
		}
		leftName, rightName := strings.ToLower(left.ListName), strings.ToLower(right.ListName)
		if leftName != rightName {
			return leftName < rightName
		}
		return left.CreatedAt.Before(right.CreatedAt)
	}
	for index := 1; index < len(lists); index++ {
		for at := index; at > 0 && less(lists[at], lists[at-1]); at-- {
			lists[at], lists[at-1] = lists[at-1], lists[at]
		}
	}
}

func (self *transaction) ShareShoppingList(sharerUserId string, share *models.ShoppingListShare) (*models.ShoppingListShare, error) {
	if share == nil || sharerUserId == "" || (share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	if share.GranteeUserID == sharerUserId {
		return nil, ErrInvalidArguments
	}
	var list shoppingListModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("\"id\" = ? AND \"user_id\" = ?", share.ShoppingListID, sharerUserId), &list)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	if share.GranteeUserID != "" {
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
		if err := lockCircleMembership(self.tx); err != nil {
			return nil, err
		}
		var members []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&members).Error; err != nil {
			return nil, err
		}
		if len(members) == 0 {
			return nil, ErrNotFound
		}
	}
	row := &shoppingListShareModel{
		ID: newID(), ShoppingListID: list.ID, GranteeUserID: nullableText(share.GranteeUserID), GranteeCircleID: nullableText(share.GranteeCircleID),
		CreatedByUserID: &sharerUserId, CreatedAt: time.Now(),
	}
	conflict := clause.OnConflict{DoNothing: true}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "shopping_list_id"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "shopping_list_id"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	query := self.tx.Where("\"shopping_list_id\" = ?", list.ID)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	shares, err := self.shoppingListSharesOf(query)
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return nil, ErrNotFound
	}
	return shares[0], nil
}

// nameShoppingListShares fills in the names the listings show: the list's
// name and whose it is, and who each share is for.
func nameShoppingListShares(tx *gorm.DB, shares []*models.ShoppingListShare) error {
	listIds, userIds, circleIds := []string{}, []string{}, []string{}
	for _, share := range shares {
		listIds = append(listIds, share.ShoppingListID)
		userIds = append(userIds, share.GranteeUserID)
		circleIds = append(circleIds, share.GranteeCircleID)
	}
	lists := map[string]*shoppingListModel{}
	if listIds = uniqueStrings(listIds); len(listIds) > 0 {
		var rows []shoppingListModel
		if err := tx.Where("\"id\" IN ?", listIds).Find(&rows).Error; err != nil {
			return err
		}
		for index := range rows {
			lists[rows[index].ID] = &rows[index]
			userIds = append(userIds, rows[index].UserID)
		}
	}
	users, err := loadUsersById(tx, userIds)
	if err != nil {
		return err
	}
	circleNames := map[string]string{}
	if circleIds = uniqueStrings(circleIds); len(circleIds) > 0 {
		var circles []circleModel
		if err := tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for _, share := range shares {
		if list := lists[share.ShoppingListID]; list != nil {
			share.ListName, share.OwnerUserID = list.ListName, list.UserID
		}
		if owner := users[share.OwnerUserID]; owner != nil {
			share.OwnerUsername, share.OwnerName = owner.Username, owner.DisplayName()
		}
		if grantee := users[share.GranteeUserID]; grantee != nil {
			share.GranteeUsername, share.GranteeName = grantee.Username, grantee.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
	}
	return nil
}

func (self *transaction) shoppingListSharesOf(query *gorm.DB) ([]*models.ShoppingListShare, error) {
	var rows []shoppingListShareModel
	if err := query.Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.ShoppingListShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameShoppingListShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func (self *transaction) ListShoppingListShares(userId, shoppingListId string) ([]*models.ShoppingListShare, error) {
	access, err := self.GetShoppingListAccess(userId, shoppingListId)
	if err != nil {
		return nil, err
	}
	if !access.IsOwner {
		return nil, ErrNotFound
	}
	return self.shoppingListSharesOf(self.tx.Where("\"shopping_list_id\" = ?", shoppingListId))
}

func (self *transaction) ListShoppingListSharesBy(ownerUserId string) ([]*models.ShoppingListShare, error) {
	if ownerUserId == "" {
		return []*models.ShoppingListShare{}, nil
	}
	return self.shoppingListSharesOf(self.tx.Where(
		"\"shopping_list_id\" IN (SELECT \"id\" FROM \"shopping_list\" WHERE \"user_id\" = ?)", ownerUserId))
}

func (self *transaction) ListShoppingListSharesReaching(userId string) ([]*models.ShoppingListShare, error) {
	reach, err := self.shoppingListReach(userId, "")
	if err != nil {
		return nil, err
	}
	sharedIds := []string{}
	for id, access := range reach {
		if !access.IsOwner {
			sharedIds = append(sharedIds, id)
		}
	}
	if len(sharedIds) == 0 {
		return []*models.ShoppingListShare{}, nil
	}
	// The shares of those lists that name the person or a circle they are in:
	// the reach says the list reaches them, and these are the ways it does.
	return self.shoppingListSharesOf(self.tx.Where(`"shopping_list_id" IN ? AND ("grantee_user_id" = ? OR "grantee_circle_id" IN
		(SELECT "circle_id" FROM "circle_member" WHERE "user_id" = ?))`, sharedIds, userId, userId))
}

func (self *transaction) DeleteShoppingListShare(ownerUserId, shoppingListShareId string) error {
	if ownerUserId == "" || shoppingListShareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"shopping_list_id\" IN (SELECT \"id\" FROM \"shopping_list\" WHERE \"user_id\" = ?)", shoppingListShareId, ownerUserId).
		Delete(&shoppingListShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) DeclineShoppingListShare(granteeUserId, shoppingListShareId string) error {
	if granteeUserId == "" || shoppingListShareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"grantee_user_id\" = ?", shoppingListShareId, granteeUserId).Delete(&shoppingListShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) ListShoppingListReaderUserIDs(shoppingListId string) ([]string, error) {
	var userIds []string
	if err := self.tx.Raw(`
		SELECT list.user_id FROM "shopping_list" list
		JOIN "user" owner_user ON owner_user.id = list.user_id AND owner_user.disabled_at IS NULL
		WHERE list.id = @list AND `+userHasPermissionSQL("list.user_id")+`
		UNION
		SELECT share.grantee_user_id FROM "shopping_list_share" share
		JOIN "shopping_list" list ON list.id = share.shopping_list_id
		JOIN "user" owner_user ON owner_user.id = list.user_id AND owner_user.disabled_at IS NULL
		JOIN "user" reader_user ON reader_user.id = share.grantee_user_id AND reader_user.disabled_at IS NULL
		WHERE share.shopping_list_id = @list
		  AND `+userHasPermissionSQL("list.user_id")+` AND `+userHasPermissionSQL("share.grantee_user_id")+`
		  AND EXISTS (SELECT 1 FROM "circle_member" mine JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		      WHERE mine.user_id = share.grantee_user_id AND theirs.user_id = list.user_id)
		UNION
		SELECT member.user_id FROM "shopping_list_share" share
		JOIN "shopping_list" list ON list.id = share.shopping_list_id
		JOIN "user" owner_user ON owner_user.id = list.user_id AND owner_user.disabled_at IS NULL
		JOIN "circle_member" member ON member.circle_id = share.grantee_circle_id
		JOIN "user" reader_user ON reader_user.id = member.user_id AND reader_user.disabled_at IS NULL
		WHERE share.shopping_list_id = @list
		  AND `+userHasPermissionSQL("list.user_id")+` AND `+userHasPermissionSQL("member.user_id")+`
		  AND EXISTS (SELECT 1 FROM "circle_member" owner_member WHERE owner_member.circle_id = share.grantee_circle_id AND owner_member.user_id = list.user_id)`,
		sql.Named("list", shoppingListId), sql.Named("permission", string(models.PermissionShoppingUse))).Scan(&userIds).Error; err != nil {
		return nil, err
	}
	return uniqueStrings(userIds), nil
}

func (self *transaction) SetShoppingListReminder(userId, shoppingListId string, isReminded bool) error {
	if _, err := self.GetShoppingListAccess(userId, shoppingListId); err != nil {
		return err
	}
	if isReminded {
		return self.tx.Where("\"shopping_list_id\" = ? AND \"user_id\" = ?", shoppingListId, userId).Delete(&shoppingListReminderMuteModel{}).Error
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&shoppingListReminderMuteModel{ShoppingListID: shoppingListId, UserID: userId, CreatedAt: time.Now()}).Error
}

func (self *transaction) ListShoppingListReminderMuters(shoppingListId string) ([]string, error) {
	var userIds []string
	if err := self.tx.Model(&shoppingListReminderMuteModel{}).Where("\"shopping_list_id\" = ?", shoppingListId).Pluck("user_id", &userIds).Error; err != nil {
		return nil, err
	}
	return userIds, nil
}

func (self *transaction) SetShoppingStoreReminder(userId, storeName string, isReminded bool) error {
	storeName = strings.TrimSpace(storeName)
	storeKey := models.ShoppingStoreKey(storeName)
	if userId == "" || storeKey == "" || len([]rune(storeName)) > shoppingListNameLengthMost {
		return fmt.Errorf("%w: name the store", ErrInvalidArguments)
	}
	if isReminded {
		return self.tx.Where("\"user_id\" = ? AND \"store_key\" = ?", userId, storeKey).Delete(&shoppingStoreMuteModel{}).Error
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&shoppingStoreMuteModel{UserID: userId, StoreKey: storeKey, StoreName: storeName, CreatedAt: time.Now()}).Error
}

func (self *transaction) ListShoppingStoreMuteKeys(userId string) ([]string, error) {
	var keys []string
	if err := self.tx.Model(&shoppingStoreMuteModel{}).Where("\"user_id\" = ?", userId).Order("\"store_key\"").Pluck("store_key", &keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// endShoppingSharesOutsideCircles deletes the shares that only a circle made
// possible once the person no longer has it, in both directions; the circle
// code calls it (endSharesOutsideCircles) in the transaction that takes a
// person out of a circle. The reasoning is that of
// endRecordingSharesOutsideCircles.
func endShoppingSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "shopping_list_share" WHERE "id" IN (
		SELECT share.id FROM "shopping_list_share" share
		JOIN "shopping_list" list ON list.id = share.shopping_list_id
		WHERE share.grantee_user_id IS NOT NULL
		  AND (list.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = list.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "shopping_list_share" share
		JOIN "shopping_list" list ON list.id = share.shopping_list_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND list.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}

// canonicalShoppingQuantity is a count needed as kept: a decimal above zero
// with at most three places, written without trailing zeros. Empty is one.
func canonicalShoppingQuantity(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "1", nil
	}
	value, isParsed := new(big.Rat).SetString(text)
	if !isParsed {
		return "", fmt.Errorf("%w: a count needed is a number above zero, such as 2 or 1.5", ErrInvalidArguments)
	}
	// Checked as it is kept, to three places: 0.0004 is kept as 0, which is
	// not a count.
	rounded, _ := new(big.Rat).SetString(value.FloatString(3))
	if rounded.Sign() <= 0 || rounded.Cmp(big.NewRat(1000000000, 1)) >= 0 {
		return "", fmt.Errorf("%w: a count needed is a number above zero, such as 2 or 1.5", ErrInvalidArguments)
	}
	return trimDecimal(rounded.FloatString(3)), nil
}

// canonicalShoppingAmount is a price as kept: a decimal of at least zero with
// at most eight places, without trailing zeros, or empty for none.
func canonicalShoppingAmount(text string) (string, error) {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "$"))
	if text == "" {
		return "", nil
	}
	value, isParsed := new(big.Rat).SetString(text)
	if !isParsed {
		return "", fmt.Errorf("%w: a price is a number of at least zero, such as 3.49", ErrInvalidArguments)
	}
	rounded, _ := new(big.Rat).SetString(value.FloatString(8))
	if rounded.Sign() < 0 || rounded.Cmp(big.NewRat(10000000000, 1)) >= 0 {
		return "", fmt.Errorf("%w: a price is a number of at least zero, such as 3.49", ErrInvalidArguments)
	}
	return trimDecimal(rounded.FloatString(8)), nil
}

// trimDecimal drops the zeros after a decimal point, and the point when
// nothing is left after it.
func trimDecimal(text string) string {
	if !strings.Contains(text, ".") {
		return text
	}
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}
