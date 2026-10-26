package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ShoppingTripOperation is somebody shopping a store now. A trip names the
// lists it is for; while it is open, what its shopper checks off on them is in
// the cart, and when it ends what they put in the cart is bought.
type ShoppingTripOperation interface {
	// StartShoppingTrip starts the person shopping a store for lists they
	// reach. It finishes their own trip that is still open, and every trip on
	// these lists that started more than models.ShoppingTripStaleAfter ago,
	// buying what those had in the cart. It answers the trip and the lists
	// whose items were bought by finishing the others.
	StartShoppingTrip(userId, storeName string, shoppingListIds []string) (*models.ShoppingTrip, []string, error)

	// FinishShoppingTrip ends one of the person's trips: what they put in
	// the cart on any list they reach is bought. It answers the trip as
	// finished and the lists whose items were bought. ErrNotFound unless the
	// trip is theirs and open. With no trip named it ends the person's own
	// open trip, however old; with none open, what they have in the cart is
	// bought all the same, recorded as a trip that starts and ends now, and
	// with nothing in the cart either it is ErrInvalidArguments.
	FinishShoppingTrip(userId, shoppingTripId string) (*models.ShoppingTrip, []string, error)

	// ListOpenShoppingTrips are the trips open now on the lists the person
	// reaches, theirs included, newest first, each with who is shopping.
	ListOpenShoppingTrips(userId string) ([]*models.ShoppingTrip, error)
}

type shoppingTripModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	UserID     string     `gorm:"column:user_id"`
	StoreName  string     `gorm:"column:store_name"`
	StoreKey   string     `gorm:"column:store_key"`
	StartedAt  time.Time  `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

func (shoppingTripModel) TableName() string { return "shopping_trip" }

type shoppingTripListModel struct {
	ShoppingTripID string `gorm:"column:shopping_trip_id;primaryKey"`
	ShoppingListID string `gorm:"column:shopping_list_id;primaryKey"`
}

func (shoppingTripListModel) TableName() string { return "shopping_trip_list" }

// shoppingTripsOf reads trips with their lists and who is shopping.
func (self *transaction) shoppingTripsOf(rows []shoppingTripModel) ([]*models.ShoppingTrip, error) {
	trips := make([]*models.ShoppingTrip, 0, len(rows))
	if len(rows) == 0 {
		return trips, nil
	}
	tripIds, userIds := []string{}, []string{}
	for _, row := range rows {
		tripIds = append(tripIds, row.ID)
		userIds = append(userIds, row.UserID)
	}
	var lists []shoppingTripListModel
	if err := self.tx.Where("\"shopping_trip_id\" IN ?", tripIds).Order("\"shopping_list_id\"").Find(&lists).Error; err != nil {
		return nil, err
	}
	listIds := map[string][]string{}
	for _, list := range lists {
		listIds[list.ShoppingTripID] = append(listIds[list.ShoppingTripID], list.ShoppingListID)
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		trip := &models.ShoppingTrip{
			ID: row.ID, UserID: row.UserID, StoreName: row.StoreName, StoreKey: row.StoreKey, StartedAt: row.StartedAt.UTC(),
			FinishedAt: utcOrNil(row.FinishedAt), ShoppingListIDs: listIds[row.ID],
		}
		if trip.ShoppingListIDs == nil {
			trip.ShoppingListIDs = []string{}
		}
		if user := users[row.UserID]; user != nil {
			trip.ShopperUsername, trip.ShopperName = user.Username, user.DisplayName()
		}
		trips = append(trips, trip)
	}
	return trips, nil
}

// finishShoppingTrips ends the trips and buys what each one's shopper put in
// the cart, on every list they still reach (a list switched on at the store
// after the trip began is shopped too), and answers the lists whose items
// were bought.
func (self *transaction) finishShoppingTrips(rows []shoppingTripModel) ([]string, error) {
	now := time.Now()
	// Every list any of these shoppers reaches is held first, once and in one
	// order, so two trips ending at once take turns rather than deadlock.
	reachable := map[string][]string{}
	allListIds := []string{}
	for _, row := range rows {
		if _, isRead := reachable[row.UserID]; isRead {
			continue
		}
		listIds, err := self.reachableShoppingListIds(row.UserID)
		if err != nil {
			return nil, err
		}
		reachable[row.UserID] = listIds
		allListIds = append(allListIds, listIds...)
	}
	if err := self.lockShoppingLists(allListIds); err != nil {
		return nil, err
	}
	changedListIds := []string{}
	for _, row := range rows {
		listIds := reachable[row.UserID]
		if len(listIds) > 0 {
			// The lists whose items this buys, which are the ones to announce.
			var bought []string
			cart := self.tx.Model(&shoppingItemModel{}).
				Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" = 'in_cart' AND \"carted_by_user_id\" = ?", listIds, row.UserID)
			if err := cart.Session(&gorm.Session{}).Distinct().Pluck("shopping_list_id", &bought).Error; err != nil {
				return nil, err
			}
			if len(bought) > 0 {
				if err := self.tx.Model(&shoppingItemModel{}).
					Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" = 'in_cart' AND \"carted_by_user_id\" = ?", bought, row.UserID).
					Updates(withUpdatedAt(boughtShoppingItemUpdates(now), now)).Error; err != nil {
					return nil, err
				}
				changedListIds = append(changedListIds, bought...)
			}
		}
		if err := self.tx.Model(&shoppingTripModel{}).Where("\"id\" = ?", row.ID).Update("finished_at", now).Error; err != nil {
			return nil, err
		}
	}
	changedListIds = uniqueStrings(changedListIds)
	for _, listId := range changedListIds {
		if err := self.touchShoppingList(listId); err != nil {
			return nil, err
		}
	}
	return changedListIds, nil
}

// withUpdatedAt adds the moment of the change to the updates.
func withUpdatedAt(updates map[string]any, at time.Time) map[string]any {
	updates["updated_at"] = at
	return updates
}

func (self *transaction) StartShoppingTrip(userId, storeName string, shoppingListIds []string) (*models.ShoppingTrip, []string, error) {
	shoppingListIds = uniqueStrings(shoppingListIds)
	if userId == "" || len(shoppingListIds) == 0 || len(shoppingListIds) > shoppingListsMost {
		return nil, nil, fmt.Errorf("%w: name the lists to shop for", ErrInvalidArguments)
	}
	storeName = strings.TrimSpace(storeName)
	if len([]rune(storeName)) > shoppingItemShortLengthMost {
		return nil, nil, fmt.Errorf("%w: a store's name is at most %d characters", ErrInvalidArguments, shoppingItemShortLengthMost)
	}
	storeKey := models.ShoppingStoreKey(storeName)
	if storeKey == "" {
		storeName = ""
	}
	for _, listId := range shoppingListIds {
		if _, err := self.GetShoppingListAccess(userId, listId); err != nil {
			return nil, nil, err
		}
	}
	// The person's own open trip, and any trip on these lists that was left
	// open long ago.
	var finishing []shoppingTripModel
	if err := self.tx.Where(`"finished_at" IS NULL AND ("user_id" = ? OR ("started_at" < ? AND "id" IN
		(SELECT "shopping_trip_id" FROM "shopping_trip_list" WHERE "shopping_list_id" IN ?)))`,
		userId, time.Now().Add(-models.ShoppingTripStaleAfter), shoppingListIds).Find(&finishing).Error; err != nil {
		return nil, nil, err
	}
	changedListIds, err := self.finishShoppingTrips(finishing)
	if err != nil {
		return nil, nil, err
	}
	row := shoppingTripModel{ID: newID(), UserID: userId, StoreName: storeName, StoreKey: storeKey, StartedAt: time.Now()}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, nil, err
	}
	for _, listId := range shoppingListIds {
		if err := self.tx.Create(&shoppingTripListModel{ShoppingTripID: row.ID, ShoppingListID: listId}).Error; err != nil {
			return nil, nil, err
		}
	}
	trips, err := self.shoppingTripsOf([]shoppingTripModel{row})
	if err != nil {
		return nil, nil, err
	}
	return trips[0], changedListIds, nil
}

func (self *transaction) FinishShoppingTrip(userId, shoppingTripId string) (*models.ShoppingTrip, []string, error) {
	if shoppingTripId == "" {
		return self.finishOwnShopping(userId)
	}
	var row shoppingTripModel
	isFound, err := findOneRow(self.tx.Where("\"id\" = ? AND \"user_id\" = ? AND \"finished_at\" IS NULL", shoppingTripId, userId), &row)
	if err != nil {
		return nil, nil, err
	}
	if !isFound {
		return nil, nil, ErrNotFound
	}
	boughtListIds, err := self.finishShoppingTrips([]shoppingTripModel{row})
	if err != nil {
		return nil, nil, err
	}
	if err := self.tx.Where("\"id\" = ?", row.ID).Take(&row).Error; err != nil {
		return nil, nil, err
	}
	trips, err := self.shoppingTripsOf([]shoppingTripModel{row})
	if err != nil {
		return nil, nil, err
	}
	return trips[0], boughtListIds, nil
}

// finishOwnShopping is Done with no trip named: the person's newest open
// trip ends, and any older one with it. A person who put things in the cart
// without a trip (one ended under them as stale) still buys them: a trip is
// written for the purchase, for the lists it bought on, so the record of what
// was bought when is the same either way.
func (self *transaction) finishOwnShopping(userId string) (*models.ShoppingTrip, []string, error) {
	var open []shoppingTripModel
	if err := self.tx.Where("\"user_id\" = ? AND \"finished_at\" IS NULL", userId).Order("\"started_at\" DESC").Find(&open).Error; err != nil {
		return nil, nil, err
	}
	if len(open) == 0 {
		listIds, err := self.reachableShoppingListIds(userId)
		if err != nil {
			return nil, nil, err
		}
		var carted []string
		if len(listIds) > 0 {
			if err := self.tx.Model(&shoppingItemModel{}).
				Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" = 'in_cart' AND \"carted_by_user_id\" = ?", listIds, userId).
				Distinct().Pluck("shopping_list_id", &carted).Error; err != nil {
				return nil, nil, err
			}
		}
		if len(carted) == 0 {
			return nil, nil, fmt.Errorf("%w: nothing is in your cart", ErrInvalidArguments)
		}
		row := shoppingTripModel{ID: newID(), UserID: userId, StartedAt: time.Now()}
		if err := self.tx.Create(&row).Error; err != nil {
			return nil, nil, err
		}
		for _, listId := range carted {
			if err := self.tx.Create(&shoppingTripListModel{ShoppingTripID: row.ID, ShoppingListID: listId}).Error; err != nil {
				return nil, nil, err
			}
		}
		open = []shoppingTripModel{row}
	}
	boughtListIds, err := self.finishShoppingTrips(open)
	if err != nil {
		return nil, nil, err
	}
	newest := open[0]
	if err := self.tx.Where("\"id\" = ?", newest.ID).Take(&newest).Error; err != nil {
		return nil, nil, err
	}
	trips, err := self.shoppingTripsOf([]shoppingTripModel{newest})
	if err != nil {
		return nil, nil, err
	}
	return trips[0], boughtListIds, nil
}

// joinShoppingTrip puts the person on a trip for a list as they put one of
// its items in the cart: their open trip gains the list when it does not
// name it already, and a person with none open (or only one left open long
// enough to be stale) starts one, which is answered so the others are told.
func (self *transaction) joinShoppingTrip(userId, storeName, shoppingListId string) (*models.ShoppingTrip, error) {
	var open shoppingTripModel
	isFound, err := findOneRow(self.tx.Where("\"user_id\" = ? AND \"finished_at\" IS NULL AND \"started_at\" >= ?",
		userId, time.Now().Add(-models.ShoppingTripStaleAfter)).Order("\"started_at\" DESC"), &open)
	if err != nil {
		return nil, err
	}
	if isFound {
		return nil, self.tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&shoppingTripListModel{ShoppingTripID: open.ID, ShoppingListID: shoppingListId}).Error
	}
	trip, _, err := self.StartShoppingTrip(userId, storeName, []string{shoppingListId})
	return trip, err
}

func (self *transaction) ListOpenShoppingTrips(userId string) ([]*models.ShoppingTrip, error) {
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	if len(listIds) == 0 {
		return []*models.ShoppingTrip{}, nil
	}
	var rows []shoppingTripModel
	if err := self.tx.Where(`"finished_at" IS NULL AND "started_at" >= ? AND "id" IN
		(SELECT "shopping_trip_id" FROM "shopping_trip_list" WHERE "shopping_list_id" IN ?)`,
		time.Now().Add(-models.ShoppingTripStaleAfter), listIds).Order("\"started_at\" DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	trips, err := self.shoppingTripsOf(rows)
	if err != nil {
		return nil, err
	}
	// A trip names only the lists the reader reaches: another list on it is
	// somebody else's business.
	isReachable := map[string]bool{}
	for _, id := range listIds {
		isReachable[id] = true
	}
	for _, trip := range trips {
		visible := []string{}
		for _, id := range trip.ShoppingListIDs {
			if isReachable[id] {
				visible = append(visible, id)
			}
		}
		trip.ShoppingListIDs = visible
	}
	return trips, nil
}
