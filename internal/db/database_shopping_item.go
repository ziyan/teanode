package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ShoppingItemOperation is the items on shopping lists, their prices, and
// the stores they are for. Every operation that takes an item asks whether
// the person reaches the item's list (GetShoppingListAccess).
type ShoppingItemOperation interface {
	// ListShoppingItems are a page of a list's items in the given states (all
	// of them when none is given), by aisle and then by their order, with how
	// many there are in all.
	ListShoppingItems(userId, shoppingListId string, shoppingItemStates []models.ShoppingItemState, offset, limit int) ([]*models.ShoppingItem, int64, error)

	// GetShoppingItem is one item on a list the person reaches.
	GetShoppingItem(userId, shoppingItemId string) (*models.ShoppingItem, error)

	// GetShoppingItemByID is one item whoever asks, for the server's own jobs.
	// ErrNotFound when there is none.
	GetShoppingItemByID(shoppingItemId string) (*models.ShoppingItem, error)

	// AddShoppingItems puts items on a list the person reaches. An item that
	// is already on it, by its description and store compared without case,
	// is needed again rather than added twice: its count needed becomes the
	// larger of the two and new details are added to its own. It answers the
	// items as stored, in the order given, and for each whether it was
	// merged into one already there.
	AddShoppingItems(userId, shoppingListId string, items []*models.ShoppingItem) ([]*models.ShoppingItem, []bool, error)

	// FindShoppingItemToMerge is the item on the list that an item of this
	// description and store would be merged into, or nil.
	FindShoppingItemToMerge(userId, shoppingListId, itemDescription, storeName string) (*models.ShoppingItem, error)

	// UpdateShoppingItem changes what the change sets on an item.
	UpdateShoppingItem(userId, shoppingItemId string, change *models.ShoppingItemChange) (*models.ShoppingItem, error)

	// RemoveShoppingItems takes items off the lists the person reaches and
	// answers the lists they were on. ErrNotFound when one of them is not an
	// item the person reaches; then nothing is removed.
	RemoveShoppingItems(userId string, shoppingItemIds []string) ([]string, error)

	// CheckShoppingItem puts an item in the cart (checked) or back to needed
	// (unchecked). Checking clears out of stock. Putting something in the
	// cart is shopping: a person with no open trip starts one at storeName
	// (the item's store when empty), and an open trip that does not name the
	// item's list gains it. The trip it started, if any, is answered, for the
	// others to be told.
	CheckShoppingItem(userId, shoppingItemId string, isChecked bool, storeName string) (*models.ShoppingItem, *models.ShoppingTrip, error)

	// SetShoppingItemNeed makes an item needed with a count (Need again), or,
	// with isNeeded false, not needed: bought when it was in the cart.
	SetShoppingItemNeed(userId, shoppingItemId string, isNeeded bool, neededQuantity string) (*models.ShoppingItem, error)

	// SetShoppingItemOutOfStock marks an item out of stock at a store, or
	// takes the mark off.
	SetShoppingItemOutOfStock(userId, shoppingItemId string, isOutOfStock bool, storeName string) (*models.ShoppingItem, error)

	// SetShoppingItemPicture gives an item a picture kept in the file store,
	// and answers the file of the picture it had, for the caller to remove.
	// The server's own; the caller has checked the person may.
	// A picture found on the web is written only while the item still has
	// the picture it had when the search began (expectedPictureFileId) and
	// that picture is not one a person uploaded; otherwise
	// ErrShoppingPictureChanged and nothing is written.
	SetShoppingItemPicture(shoppingItemId, pictureFileId, picturePageUrl string, pictureSource models.ShoppingPictureSource, expectedPictureFileId string) (string, error)

	// MarkShoppingItemPictureSearched records that a search for the item's
	// picture was made now, found or not.
	MarkShoppingItemPictureSearched(shoppingItemId string) error

	// RecordShoppingItemPrice keeps a price seen for an item, and makes it the
	// item's price when it is the newest.
	RecordShoppingItemPrice(userId string, price *models.ShoppingItemPrice) (*models.ShoppingItemPrice, error)

	// ListShoppingItemPrices are an item's prices, newest first.
	ListShoppingItemPrices(userId, shoppingItemId string) ([]*models.ShoppingItemPrice, error)

	// ListShoppingStores are the stores the items of the person's lists are
	// for, by name, with how many are needed at each and whether the person
	// is reminded there.
	ListShoppingStores(userId string) ([]*models.ShoppingStore, error)

	// ListShoppingStoreItems are the needed and in-cart items for a store on
	// the lists the person reaches (those named, or all), and, when asked,
	// the items for any store, by aisle; each says which list it is on.
	ListShoppingStoreItems(userId, storeName string, shoppingListIds []string, isAnyStoreIncluded bool) ([]*models.ShoppingItem, error)

	// ListShoppingItemsNeededAt are the needed items at a store for the
	// arrival reminder: on the lists the person reaches and is reminded of,
	// for that store, and for any store when asked.
	ListShoppingItemsNeededAt(userId, storeKey string, isAnyStoreIncluded bool) ([]*models.ShoppingItem, error)
}

// ErrShoppingPictureChanged is a picture found on the web arriving after the
// item's picture changed, or a person uploaded one: the newer stays.
var ErrShoppingPictureChanged = errors.New("db: the item's picture changed meanwhile")

// The longest of each of an item's words, so that a pasted page does not
// become one item.
const (
	shoppingItemDescriptionLengthMost = 500
	shoppingItemDetailsLengthMost     = 2000
	shoppingItemShortLengthMost       = 200
	shoppingItemUnitLengthMost        = 32
	shoppingItemsAddedAtOnceMost      = 500
	shoppingPageSizeMost              = 500
)

type shoppingItemModel struct {
	ID                  string     `gorm:"column:id;primaryKey"`
	ShoppingListID      string     `gorm:"column:shopping_list_id"`
	ItemDescription     string     `gorm:"column:item_description"`
	ItemDetails         string     `gorm:"column:item_details"`
	StoreName           string     `gorm:"column:store_name"`
	StoreKey            string     `gorm:"column:store_key"`
	IsStoreBrand        bool       `gorm:"column:is_store_brand"`
	AisleName           string     `gorm:"column:aisle_name"`
	AisleRank           int        `gorm:"column:aisle_rank"`
	ItemRank            int        `gorm:"column:item_rank"`
	NeededQuantity      string     `gorm:"column:needed_quantity"`
	QuantityUnit        string     `gorm:"column:quantity_unit"`
	UnitPriceAmount     *string    `gorm:"column:unit_price_amount"`
	CurrencyCode        string     `gorm:"column:currency_code"`
	ShoppingItemState   string     `gorm:"column:shopping_item_state"`
	CartedByUserID      *string    `gorm:"column:carted_by_user_id"`
	CartedAt            *time.Time `gorm:"column:carted_at"`
	OutOfStockAt        *time.Time `gorm:"column:out_of_stock_at"`
	OutOfStockByUserID  *string    `gorm:"column:out_of_stock_by_user_id"`
	OutOfStockStoreName string     `gorm:"column:out_of_stock_store_name"`
	LastBoughtAt        *time.Time `gorm:"column:last_bought_at"`
	PictureFileID       string     `gorm:"column:picture_file_id"`
	PicturePageURL      string     `gorm:"column:picture_page_url"`
	PictureSource       string     `gorm:"column:picture_source"`
	PictureSearchedAt   *time.Time `gorm:"column:picture_searched_at"`
	CreatedByUserID     *string    `gorm:"column:created_by_user_id"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (shoppingItemModel) TableName() string { return "shopping_item" }

func utcOrNil(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	utc := at.UTC()
	return &utc
}

func (self *shoppingItemModel) toModel() *models.ShoppingItem {
	return &models.ShoppingItem{
		ID: self.ID, ShoppingListID: self.ShoppingListID, ItemDescription: self.ItemDescription, ItemDetails: self.ItemDetails,
		StoreName: self.StoreName, StoreKey: self.StoreKey, IsStoreBrand: self.IsStoreBrand, AisleName: self.AisleName,
		AisleRank: self.AisleRank, ItemRank: self.ItemRank, NeededQuantity: trimDecimal(self.NeededQuantity), QuantityUnit: self.QuantityUnit,
		UnitPriceAmount: trimDecimal(optionalString(self.UnitPriceAmount)), CurrencyCode: self.CurrencyCode,
		ShoppingItemState: models.ShoppingItemState(self.ShoppingItemState), CartedByUserID: optionalString(self.CartedByUserID),
		CartedAt: utcOrNil(self.CartedAt), OutOfStockAt: utcOrNil(self.OutOfStockAt), OutOfStockByUserID: optionalString(self.OutOfStockByUserID),
		OutOfStockStoreName: self.OutOfStockStoreName, LastBoughtAt: utcOrNil(self.LastBoughtAt),
		PictureFileID: self.PictureFileID, PicturePageURL: self.PicturePageURL, PictureSource: models.ShoppingPictureSource(self.PictureSource),
		PictureSearchedAt: utcOrNil(self.PictureSearchedAt), CreatedByUserID: optionalString(self.CreatedByUserID),
		CreatedAt: self.CreatedAt.UTC(), UpdatedAt: self.UpdatedAt.UTC(),
	}
}

type shoppingItemPriceModel struct {
	ID               string    `gorm:"column:id;primaryKey"`
	ShoppingItemID   string    `gorm:"column:shopping_item_id"`
	StoreName        string    `gorm:"column:store_name"`
	StoreKey         string    `gorm:"column:store_key"`
	UnitPriceAmount  string    `gorm:"column:unit_price_amount"`
	CurrencyCode     string    `gorm:"column:currency_code"`
	Quantity         *string   `gorm:"column:quantity"`
	QuantityUnit     string    `gorm:"column:quantity_unit"`
	ObservedOn       time.Time `gorm:"column:observed_on"`
	PriceSource      string    `gorm:"column:price_source"`
	FinanceReceiptID *string   `gorm:"column:finance_receipt_id"`
	CreatedByUserID  *string   `gorm:"column:created_by_user_id"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (shoppingItemPriceModel) TableName() string { return "shopping_item_price" }

func (self *shoppingItemPriceModel) toModel() *models.ShoppingItemPrice {
	return &models.ShoppingItemPrice{
		ID: self.ID, ShoppingItemID: self.ShoppingItemID, StoreName: self.StoreName, StoreKey: self.StoreKey,
		UnitPriceAmount: trimDecimal(self.UnitPriceAmount), CurrencyCode: self.CurrencyCode, Quantity: trimDecimal(optionalString(self.Quantity)),
		QuantityUnit: self.QuantityUnit, ObservedOn: self.ObservedOn.Format(time.DateOnly), PriceSource: models.ShoppingPriceSource(self.PriceSource),
		FinanceReceiptID: optionalString(self.FinanceReceiptID), CreatedByUserID: optionalString(self.CreatedByUserID), CreatedAt: self.CreatedAt.UTC(),
	}
}

// shoppingItemOrder is the order items are shown in: by aisle as the aisles
// were given, then as the items were.
const shoppingItemOrder = "\"aisle_rank\" ASC, \"item_rank\" ASC, \"created_at\" ASC, \"id\" ASC"

// shoppingItemsOf reads the items a query selects, with the names a reader is
// shown: the list each is on, and who put it in the cart or found it out of
// stock.
func (self *transaction) shoppingItemsOf(query *gorm.DB) ([]*models.ShoppingItem, error) {
	var rows []shoppingItemModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*models.ShoppingItem, 0, len(rows))
	listIds, userIds := []string{}, []string{}
	for index := range rows {
		item := rows[index].toModel()
		items = append(items, item)
		listIds = append(listIds, item.ShoppingListID)
		userIds = append(userIds, item.CartedByUserID, item.OutOfStockByUserID)
	}
	listNames := map[string]string{}
	if listIds = uniqueStrings(listIds); len(listIds) > 0 {
		var lists []shoppingListModel
		if err := self.tx.Where("\"id\" IN ?", listIds).Find(&lists).Error; err != nil {
			return nil, err
		}
		for _, list := range lists {
			listNames[list.ID] = list.ListName
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		item.ListName = listNames[item.ShoppingListID]
		if user := users[item.CartedByUserID]; user != nil {
			item.CartedByName = user.DisplayName()
		}
		if user := users[item.OutOfStockByUserID]; user != nil {
			item.OutOfStockByName = user.DisplayName()
		}
	}
	return items, nil
}

func (self *transaction) ListShoppingItems(userId, shoppingListId string, shoppingItemStates []models.ShoppingItemState, offset, limit int) ([]*models.ShoppingItem, int64, error) {
	if offset < 0 || limit < 1 || limit > shoppingPageSizeMost {
		return nil, 0, ErrInvalidArguments
	}
	if _, err := self.GetShoppingListAccess(userId, shoppingListId); err != nil {
		return nil, 0, err
	}
	query := self.tx.Model(&shoppingItemModel{}).Where("\"shopping_list_id\" = ?", shoppingListId)
	if len(shoppingItemStates) > 0 {
		states := make([]string, 0, len(shoppingItemStates))
		for _, state := range shoppingItemStates {
			if !state.IsValid() {
				return nil, 0, ErrInvalidArguments
			}
			states = append(states, string(state))
		}
		query = query.Where("\"shopping_item_state\" IN ?", states)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items, err := self.shoppingItemsOf(query.Order(shoppingItemOrder).Offset(offset).Limit(limit))
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (self *transaction) GetShoppingItemByID(shoppingItemId string) (*models.ShoppingItem, error) {
	if shoppingItemId == "" {
		return nil, ErrNotFound
	}
	items, err := self.shoppingItemsOf(self.tx.Where("\"id\" = ?", shoppingItemId))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

func (self *transaction) GetShoppingItem(userId, shoppingItemId string) (*models.ShoppingItem, error) {
	item, err := self.GetShoppingItemByID(shoppingItemId)
	if err != nil {
		return nil, err
	}
	if _, err := self.GetShoppingListAccess(userId, item.ShoppingListID); err != nil {
		return nil, err
	}
	return item, nil
}

// lockShoppingItem is an item on a list the person reaches, its row held for
// the rest of the transaction.
func (self *transaction) lockShoppingItem(userId, shoppingItemId string) (*shoppingItemModel, error) {
	if shoppingItemId == "" {
		return nil, ErrNotFound
	}
	// The list's row is held before the item's, the order every change to a
	// list takes them in (AddShoppingItems, a trip ending), so two changes to
	// one list wait for each other rather than deadlock.
	var listIds []string
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", shoppingItemId).Pluck("shopping_list_id", &listIds).Error; err != nil {
		return nil, err
	}
	if len(listIds) == 0 {
		return nil, ErrNotFound
	}
	if err := self.lockShoppingLists(listIds); err != nil {
		return nil, err
	}
	var row shoppingItemModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", shoppingItemId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	if userId != "" {
		if _, err := self.GetShoppingListAccess(userId, row.ShoppingListID); err != nil {
			return nil, err
		}
	}
	return &row, nil
}

// lockShoppingLists holds the rows of lists for the rest of the transaction,
// in the order of their identifiers.
func (self *transaction) lockShoppingLists(shoppingListIds []string) error {
	shoppingListIds = uniqueStrings(shoppingListIds)
	if len(shoppingListIds) == 0 {
		return nil
	}
	return self.tx.Exec(`SELECT 1 FROM "shopping_list" WHERE "id" IN ? ORDER BY "id" FOR UPDATE`, shoppingListIds).Error
}

// parseRat reads a decimal written as text.
func parseRat(text string) (*big.Rat, bool) {
	return new(big.Rat).SetString(strings.TrimSpace(text))
}

// cleanShoppingItemText is one of an item's words as kept: trimmed, and
// refused when longer than its column.
func cleanShoppingItemText(name, text string, lengthMost int) (string, error) {
	text = strings.TrimSpace(text)
	if len([]rune(text)) > lengthMost {
		return "", fmt.Errorf("%w: an item's %s is at most %d characters", ErrInvalidArguments, name, lengthMost)
	}
	return text, nil
}

// cleanShoppingItem checks and trims what an item is given before it is kept.
func cleanShoppingItem(item *models.ShoppingItem) error {
	var err error
	if item.ItemDescription, err = cleanShoppingItemText("description", item.ItemDescription, shoppingItemDescriptionLengthMost); err != nil {
		return err
	}
	if item.ItemDescription == "" {
		return fmt.Errorf("%w: an item needs a description", ErrInvalidArguments)
	}
	if item.ItemDetails, err = cleanShoppingItemText("details", item.ItemDetails, shoppingItemDetailsLengthMost); err != nil {
		return err
	}
	if item.StoreName, err = cleanShoppingItemText("store", item.StoreName, shoppingItemShortLengthMost); err != nil {
		return err
	}
	item.StoreKey = models.ShoppingStoreKey(item.StoreName)
	if item.StoreKey == "" {
		item.StoreName = ""
	}
	if item.AisleName, err = cleanShoppingItemText("aisle", item.AisleName, shoppingItemShortLengthMost); err != nil {
		return err
	}
	if item.QuantityUnit, err = cleanShoppingItemText("unit", item.QuantityUnit, shoppingItemUnitLengthMost); err != nil {
		return err
	}
	if item.NeededQuantity, err = canonicalShoppingQuantity(item.NeededQuantity); err != nil {
		return err
	}
	if item.UnitPriceAmount, err = canonicalShoppingAmount(item.UnitPriceAmount); err != nil {
		return err
	}
	item.CurrencyCode = strings.ToUpper(strings.TrimSpace(item.CurrencyCode))
	if len(item.CurrencyCode) > 3 {
		return fmt.Errorf("%w: a currency is its three-letter code", ErrInvalidArguments)
	}
	if item.UnitPriceAmount != "" && item.CurrencyCode == "" {
		item.CurrencyCode = "USD"
	}
	return nil
}

// shoppingItemMergeQuery selects the item of a list that one of this
// description and store is merged into.
func (self *transaction) shoppingItemToMerge(shoppingListId, itemDescription, storeKey string) (*shoppingItemModel, error) {
	var row shoppingItemModel
	isFound, err := findOneRow(self.tx.Where("\"shopping_list_id\" = ? AND lower(\"item_description\") = lower(?) AND \"store_key\" = ?",
		shoppingListId, strings.TrimSpace(itemDescription), storeKey).Order("\"created_at\" ASC"), &row)
	if err != nil || !isFound {
		return nil, err
	}
	return &row, nil
}

func (self *transaction) FindShoppingItemToMerge(userId, shoppingListId, itemDescription, storeName string) (*models.ShoppingItem, error) {
	if _, err := self.GetShoppingListAccess(userId, shoppingListId); err != nil {
		return nil, err
	}
	row, err := self.shoppingItemToMerge(shoppingListId, itemDescription, models.ShoppingStoreKey(storeName))
	if err != nil || row == nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) AddShoppingItems(userId, shoppingListId string, items []*models.ShoppingItem) ([]*models.ShoppingItem, []bool, error) {
	if len(items) == 0 || len(items) > shoppingItemsAddedAtOnceMost {
		return nil, nil, fmt.Errorf("%w: add 1 to %d items at a time", ErrInvalidArguments, shoppingItemsAddedAtOnceMost)
	}
	if _, err := self.GetShoppingListAccess(userId, shoppingListId); err != nil {
		return nil, nil, err
	}
	// The list's row is held so that two imports at once take turns, and the
	// second merges into what the first added.
	if err := self.lockShoppingLists([]string{shoppingListId}); err != nil {
		return nil, nil, err
	}
	var count int64
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"shopping_list_id\" = ?", shoppingListId).Count(&count).Error; err != nil {
		return nil, nil, err
	}
	// New aisles go after the ones the list has, in the order given; an aisle
	// the list has keeps its place.
	var aisles []struct {
		AisleName string `gorm:"column:aisle_name"`
		AisleRank int    `gorm:"column:aisle_rank"`
	}
	if err := self.tx.Raw(`SELECT lower("aisle_name") AS aisle_name, min("aisle_rank") AS aisle_rank FROM "shopping_item"
		WHERE "shopping_list_id" = ? GROUP BY lower("aisle_name")`, shoppingListId).Scan(&aisles).Error; err != nil {
		return nil, nil, err
	}
	aisleRanks := map[string]int{}
	nextAisleRank := 0
	for _, aisle := range aisles {
		aisleRanks[aisle.AisleName] = aisle.AisleRank
		nextAisleRank = max(nextAisleRank, aisle.AisleRank+1)
	}
	var itemRankMost sql.NullInt64
	if err := self.tx.Raw(`SELECT max("item_rank") FROM "shopping_item" WHERE "shopping_list_id" = ?`, shoppingListId).Scan(&itemRankMost).Error; err != nil {
		return nil, nil, err
	}
	nextItemRank := int(itemRankMost.Int64) + 1
	now := time.Now()
	storedIds := make([]string, 0, len(items))
	isMerged := make([]bool, 0, len(items))
	for _, given := range items {
		item := *given
		if err := cleanShoppingItem(&item); err != nil {
			return nil, nil, err
		}
		existing, err := self.shoppingItemToMerge(shoppingListId, item.ItemDescription, item.StoreKey)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			updates := map[string]any{"updated_at": now}
			switch models.ShoppingItemState(existing.ShoppingItemState) {
			case models.ShoppingItemStateNeeded:
				if larger, err := largerQuantity(existing.NeededQuantity, item.NeededQuantity); err == nil {
					updates["needed_quantity"] = larger
				}
			case models.ShoppingItemStateNotNeeded:
				updates["shopping_item_state"] = string(models.ShoppingItemStateNeeded)
				updates["needed_quantity"] = item.NeededQuantity
				updates["out_of_stock_at"], updates["out_of_stock_by_user_id"], updates["out_of_stock_store_name"] = nil, nil, ""
			}
			if item.ItemDetails != "" && !strings.Contains(strings.ToLower(existing.ItemDetails), strings.ToLower(item.ItemDetails)) {
				details := item.ItemDetails
				if existing.ItemDetails != "" {
					details = existing.ItemDetails + "; " + item.ItemDetails
				}
				if len([]rune(details)) <= shoppingItemDetailsLengthMost {
					updates["item_details"] = details
				}
			}
			if existing.AisleName == "" && item.AisleName != "" {
				updates["aisle_name"] = item.AisleName
			}
			if existing.QuantityUnit == "" && item.QuantityUnit != "" {
				updates["quantity_unit"] = item.QuantityUnit
			}
			if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", existing.ID).Updates(updates).Error; err != nil {
				return nil, nil, err
			}
			storedIds = append(storedIds, existing.ID)
			isMerged = append(isMerged, true)
			continue
		}
		if count >= shoppingItemsPerListMost {
			return nil, nil, limitReached("a list holds at most %d items; remove some you no longer buy first", shoppingItemsPerListMost)
		}
		count++
		aisleKey := strings.ToLower(item.AisleName)
		aisleRank, isKnown := aisleRanks[aisleKey]
		if !isKnown {
			aisleRank = nextAisleRank
			aisleRanks[aisleKey] = aisleRank
			nextAisleRank++
		}
		row := &shoppingItemModel{
			ID: newID(), ShoppingListID: shoppingListId, ItemDescription: item.ItemDescription, ItemDetails: item.ItemDetails,
			StoreName: item.StoreName, StoreKey: item.StoreKey, IsStoreBrand: item.IsStoreBrand && item.StoreKey != "",
			AisleName: item.AisleName, AisleRank: aisleRank, ItemRank: nextItemRank, NeededQuantity: item.NeededQuantity,
			QuantityUnit: item.QuantityUnit, UnitPriceAmount: nullableText(item.UnitPriceAmount), CurrencyCode: item.CurrencyCode,
			ShoppingItemState: string(models.ShoppingItemStateNeeded), CreatedByUserID: &userId, CreatedAt: now, UpdatedAt: now,
		}
		nextItemRank++
		if err := self.tx.Create(row).Error; err != nil {
			return nil, nil, err
		}
		// A price given with the item is a price seen, kept in its history.
		if item.UnitPriceAmount != "" {
			if _, err := self.recordShoppingItemPrice(row, &models.ShoppingItemPrice{
				UnitPriceAmount: item.UnitPriceAmount, CurrencyCode: item.CurrencyCode, PriceSource: models.ShoppingPriceSourcePerson, CreatedByUserID: userId,
			}); err != nil {
				return nil, nil, err
			}
		}
		storedIds = append(storedIds, row.ID)
		isMerged = append(isMerged, false)
	}
	if err := self.touchShoppingList(shoppingListId); err != nil {
		return nil, nil, err
	}
	stored, err := self.shoppingItemsOf(self.tx.Where("\"id\" IN ?", uniqueStrings(storedIds)))
	if err != nil {
		return nil, nil, err
	}
	byId := map[string]*models.ShoppingItem{}
	for _, item := range stored {
		byId[item.ID] = item
	}
	ordered := make([]*models.ShoppingItem, 0, len(storedIds))
	for _, id := range storedIds {
		ordered = append(ordered, byId[id])
	}
	return ordered, isMerged, nil
}

// largerQuantity is the larger of two counts.
func largerQuantity(left, right string) (string, error) {
	leftValue, isLeftParsed := parseRat(left)
	rightValue, isRightParsed := parseRat(right)
	if !isLeftParsed || !isRightParsed {
		return "", ErrInvalidArguments
	}
	if leftValue.Cmp(rightValue) >= 0 {
		return trimDecimal(leftValue.FloatString(3)), nil
	}
	return trimDecimal(rightValue.FloatString(3)), nil
}

func (self *transaction) UpdateShoppingItem(userId, shoppingItemId string, change *models.ShoppingItemChange) (*models.ShoppingItem, error) {
	if change == nil {
		return nil, ErrInvalidArguments
	}
	row, err := self.lockShoppingItem(userId, shoppingItemId)
	if err != nil {
		return nil, err
	}
	item := row.toModel()
	if change.ItemDescription != nil {
		item.ItemDescription = *change.ItemDescription
	}
	if change.ItemDetails != nil {
		item.ItemDetails = *change.ItemDetails
	}
	if change.StoreName != nil {
		item.StoreName = *change.StoreName
	}
	if change.IsStoreBrand != nil {
		item.IsStoreBrand = *change.IsStoreBrand
	}
	if change.AisleName != nil {
		item.AisleName = *change.AisleName
	}
	if change.NeededQuantity != nil {
		item.NeededQuantity = *change.NeededQuantity
	}
	if change.QuantityUnit != nil {
		item.QuantityUnit = *change.QuantityUnit
	}
	if err := cleanShoppingItem(item); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"item_description": item.ItemDescription, "item_details": item.ItemDetails, "store_name": item.StoreName, "store_key": item.StoreKey,
		"is_store_brand": item.IsStoreBrand && item.StoreKey != "", "aisle_name": item.AisleName, "needed_quantity": item.NeededQuantity,
		"quantity_unit": item.QuantityUnit, "updated_at": time.Now(),
	}
	// An aisle the list has keeps its place; a new one goes last.
	if !strings.EqualFold(item.AisleName, row.AisleName) {
		var aisleRank sql.NullInt64
		if err := self.tx.Raw(`SELECT min("aisle_rank") FROM "shopping_item" WHERE "shopping_list_id" = ? AND lower("aisle_name") = lower(?) AND "id" <> ?`,
			row.ShoppingListID, item.AisleName, row.ID).Scan(&aisleRank).Error; err != nil {
			return nil, err
		}
		if !aisleRank.Valid {
			if err := self.tx.Raw(`SELECT coalesce(max("aisle_rank"), -1) + 1 FROM "shopping_item" WHERE "shopping_list_id" = ?`, row.ShoppingListID).Scan(&aisleRank).Error; err != nil {
				return nil, err
			}
		}
		updates["aisle_rank"] = aisleRank.Int64
	}
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := self.touchShoppingList(row.ShoppingListID); err != nil {
		return nil, err
	}
	return self.GetShoppingItemByID(row.ID)
}

func (self *transaction) RemoveShoppingItems(userId string, shoppingItemIds []string) ([]string, error) {
	shoppingItemIds = uniqueStrings(shoppingItemIds)
	if len(shoppingItemIds) == 0 || len(shoppingItemIds) > shoppingItemsAddedAtOnceMost {
		return nil, ErrInvalidArguments
	}
	var rows []shoppingItemModel
	if err := self.tx.Where("\"id\" IN ?", shoppingItemIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != len(shoppingItemIds) {
		return nil, ErrNotFound
	}
	listIds := []string{}
	for _, row := range rows {
		listIds = append(listIds, row.ShoppingListID)
	}
	listIds = uniqueStrings(listIds)
	for _, listId := range listIds {
		if _, err := self.GetShoppingListAccess(userId, listId); err != nil {
			return nil, err
		}
	}
	if err := self.lockShoppingLists(listIds); err != nil {
		return nil, err
	}
	if err := self.tx.Where("\"id\" IN ?", shoppingItemIds).Delete(&shoppingItemModel{}).Error; err != nil {
		return nil, err
	}
	for _, listId := range listIds {
		if err := self.touchShoppingList(listId); err != nil {
			return nil, err
		}
	}
	return listIds, nil
}

// changeShoppingItem writes updates to an item the person reaches and
// answers it as stored.
func (self *transaction) changeShoppingItem(row *shoppingItemModel, updates map[string]any) (*models.ShoppingItem, error) {
	updates["updated_at"] = time.Now()
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := self.touchShoppingList(row.ShoppingListID); err != nil {
		return nil, err
	}
	return self.GetShoppingItemByID(row.ID)
}

// boughtShoppingItemUpdates are what an item becomes once bought: not needed,
// out of the cart, bought at that moment, and no longer out of stock.
func boughtShoppingItemUpdates(boughtAt time.Time) map[string]any {
	return map[string]any{
		"shopping_item_state": string(models.ShoppingItemStateNotNeeded), "carted_by_user_id": nil, "carted_at": nil,
		"last_bought_at": boughtAt, "out_of_stock_at": nil, "out_of_stock_by_user_id": nil, "out_of_stock_store_name": "",
	}
}

func (self *transaction) CheckShoppingItem(userId, shoppingItemId string, isChecked bool, storeName string) (*models.ShoppingItem, *models.ShoppingTrip, error) {
	var startedTrip *models.ShoppingTrip
	if isChecked {
		// The trip is joined before the item is held: ending a stale trip
		// holds every list its shopper reaches, and lists are held before
		// items.
		item, err := self.GetShoppingItem(userId, shoppingItemId)
		if err != nil {
			return nil, nil, err
		}
		if item.ShoppingItemState == models.ShoppingItemStateNeeded {
			if strings.TrimSpace(storeName) == "" {
				storeName = item.StoreName
			}
			if startedTrip, err = self.joinShoppingTrip(userId, storeName, item.ShoppingListID); err != nil {
				return nil, nil, err
			}
		}
	}
	row, err := self.lockShoppingItem(userId, shoppingItemId)
	if err != nil {
		return nil, nil, err
	}
	if isChecked {
		switch models.ShoppingItemState(row.ShoppingItemState) {
		case models.ShoppingItemStateNotNeeded:
			return nil, nil, fmt.Errorf("%w: the item is not needed; need it again first", ErrInvalidArguments)
		case models.ShoppingItemStateInCart:
			// Already in somebody's cart: it stays in theirs, so their Done
			// buys it.
			item, err := self.GetShoppingItemByID(row.ID)
			return item, startedTrip, err
		}
		item, err := self.changeShoppingItem(row, map[string]any{
			"shopping_item_state": string(models.ShoppingItemStateInCart), "carted_by_user_id": userId, "carted_at": time.Now(),
			"out_of_stock_at": nil, "out_of_stock_by_user_id": nil, "out_of_stock_store_name": "",
		})
		return item, startedTrip, err
	}
	item, err := self.changeShoppingItem(row, map[string]any{
		"shopping_item_state": string(models.ShoppingItemStateNeeded), "carted_by_user_id": nil, "carted_at": nil,
	})
	return item, nil, err
}

func (self *transaction) SetShoppingItemNeed(userId, shoppingItemId string, isNeeded bool, neededQuantity string) (*models.ShoppingItem, error) {
	row, err := self.lockShoppingItem(userId, shoppingItemId)
	if err != nil {
		return nil, err
	}
	if !isNeeded {
		if models.ShoppingItemState(row.ShoppingItemState) == models.ShoppingItemStateInCart {
			return self.changeShoppingItem(row, boughtShoppingItemUpdates(time.Now()))
		}
		return self.changeShoppingItem(row, map[string]any{
			"shopping_item_state": string(models.ShoppingItemStateNotNeeded), "carted_by_user_id": nil, "carted_at": nil,
			"out_of_stock_at": nil, "out_of_stock_by_user_id": nil, "out_of_stock_store_name": "",
		})
	}
	quantity, err := canonicalShoppingQuantity(neededQuantity)
	if err != nil {
		return nil, err
	}
	return self.changeShoppingItem(row, map[string]any{
		"shopping_item_state": string(models.ShoppingItemStateNeeded), "needed_quantity": quantity, "carted_by_user_id": nil, "carted_at": nil,
		"out_of_stock_at": nil, "out_of_stock_by_user_id": nil, "out_of_stock_store_name": "",
	})
}

func (self *transaction) SetShoppingItemOutOfStock(userId, shoppingItemId string, isOutOfStock bool, storeName string) (*models.ShoppingItem, error) {
	row, err := self.lockShoppingItem(userId, shoppingItemId)
	if err != nil {
		return nil, err
	}
	if !isOutOfStock {
		return self.changeShoppingItem(row, map[string]any{"out_of_stock_at": nil, "out_of_stock_by_user_id": nil, "out_of_stock_store_name": ""})
	}
	storeName, err = cleanShoppingItemText("store", storeName, shoppingItemShortLengthMost)
	if err != nil {
		return nil, err
	}
	if storeName == "" {
		storeName = row.StoreName
	}
	updates := map[string]any{"out_of_stock_at": time.Now(), "out_of_stock_by_user_id": userId, "out_of_stock_store_name": storeName}
	// Out of stock is about an item still to buy: one in the cart goes back
	// to needed; a needed or not needed one keeps its state.
	if models.ShoppingItemState(row.ShoppingItemState) == models.ShoppingItemStateInCart {
		updates["shopping_item_state"] = string(models.ShoppingItemStateNeeded)
		updates["carted_by_user_id"], updates["carted_at"] = nil, nil
	}
	return self.changeShoppingItem(row, updates)
}

func (self *transaction) SetShoppingItemPicture(shoppingItemId, pictureFileId, picturePageUrl string, pictureSource models.ShoppingPictureSource, expectedPictureFileId string) (string, error) {
	row, err := self.lockShoppingItem("", shoppingItemId)
	if err != nil {
		return "", err
	}
	if pictureSource == models.ShoppingPictureSourceWeb &&
		(row.PictureFileID != expectedPictureFileId || row.PictureSource == string(models.ShoppingPictureSourceUpload)) {
		return "", ErrShoppingPictureChanged
	}
	if pictureFileId == "" {
		pictureSource, picturePageUrl = "", ""
	}
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(map[string]any{
		"picture_file_id": pictureFileId, "picture_page_url": picturePageUrl, "picture_source": string(pictureSource),
		"picture_searched_at": time.Now(),
	}).Error; err != nil {
		return "", err
	}
	if err := self.touchShoppingList(row.ShoppingListID); err != nil {
		return "", err
	}
	return row.PictureFileID, nil
}

func (self *transaction) MarkShoppingItemPictureSearched(shoppingItemId string) error {
	return self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", shoppingItemId).Update("picture_searched_at", time.Now()).Error
}

// recordShoppingItemPrice keeps a price for an item already held, and makes
// it the item's price when no newer one is kept.
func (self *transaction) recordShoppingItemPrice(row *shoppingItemModel, price *models.ShoppingItemPrice) (*models.ShoppingItemPrice, error) {
	amount, err := canonicalShoppingAmount(price.UnitPriceAmount)
	if err != nil {
		return nil, err
	}
	if amount == "" {
		return nil, fmt.Errorf("%w: give the price", ErrInvalidArguments)
	}
	storeName, err := cleanShoppingItemText("store", price.StoreName, shoppingItemShortLengthMost)
	if err != nil {
		return nil, err
	}
	if storeName == "" {
		storeName = row.StoreName
	}
	storeKey := models.ShoppingStoreKey(storeName)
	if storeKey == "" {
		storeName = ""
	}
	observedOn := time.Now()
	if strings.TrimSpace(price.ObservedOn) != "" {
		if observedOn, err = time.Parse(time.DateOnly, strings.TrimSpace(price.ObservedOn)); err != nil {
			return nil, fmt.Errorf("%w: a day is YYYY-MM-DD", ErrInvalidArguments)
		}
	}
	currencyCode := strings.ToUpper(strings.TrimSpace(price.CurrencyCode))
	if currencyCode == "" {
		currencyCode = "USD"
	}
	if len(currencyCode) != 3 {
		return nil, fmt.Errorf("%w: a currency is its three-letter code", ErrInvalidArguments)
	}
	var quantity *string
	if strings.TrimSpace(price.Quantity) != "" {
		canonical, err := canonicalShoppingQuantity(price.Quantity)
		if err != nil {
			return nil, err
		}
		quantity = &canonical
	}
	source := price.PriceSource
	if source == "" {
		source = models.ShoppingPriceSourcePerson
	}
	model := &shoppingItemPriceModel{
		ID: newID(), ShoppingItemID: row.ID, StoreName: storeName, StoreKey: storeKey, UnitPriceAmount: amount, CurrencyCode: currencyCode,
		Quantity: quantity, QuantityUnit: strings.TrimSpace(price.QuantityUnit), ObservedOn: observedOn, PriceSource: string(source),
		FinanceReceiptID: nullableText(price.FinanceReceiptID), CreatedByUserID: nullableText(price.CreatedByUserID), CreatedAt: time.Now(),
	}
	if len([]rune(model.QuantityUnit)) > shoppingItemUnitLengthMost {
		model.QuantityUnit = ""
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	var newerCount int64
	if err := self.tx.Model(&shoppingItemPriceModel{}).Where("\"shopping_item_id\" = ? AND \"observed_on\" > ?", row.ID, observedOn.Format(time.DateOnly)).
		Count(&newerCount).Error; err != nil {
		return nil, err
	}
	if newerCount == 0 {
		if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(map[string]any{
			"unit_price_amount": amount, "currency_code": currencyCode, "updated_at": time.Now(),
		}).Error; err != nil {
			return nil, err
		}
	}
	return model.toModel(), nil
}

func (self *transaction) RecordShoppingItemPrice(userId string, price *models.ShoppingItemPrice) (*models.ShoppingItemPrice, error) {
	if price == nil {
		return nil, ErrInvalidArguments
	}
	row, err := self.lockShoppingItem(userId, price.ShoppingItemID)
	if err != nil {
		return nil, err
	}
	price.CreatedByUserID = userId
	stored, err := self.recordShoppingItemPrice(row, price)
	if err != nil {
		return nil, err
	}
	if err := self.touchShoppingList(row.ShoppingListID); err != nil {
		return nil, err
	}
	return stored, nil
}

func (self *transaction) ListShoppingItemPrices(userId, shoppingItemId string) ([]*models.ShoppingItemPrice, error) {
	if _, err := self.GetShoppingItem(userId, shoppingItemId); err != nil {
		return nil, err
	}
	var rows []shoppingItemPriceModel
	if err := self.tx.Where("\"shopping_item_id\" = ?", shoppingItemId).
		Order("\"observed_on\" DESC, \"created_at\" DESC").Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	prices := make([]*models.ShoppingItemPrice, 0, len(rows))
	for index := range rows {
		prices = append(prices, rows[index].toModel())
	}
	return prices, nil
}

func (self *transaction) ListShoppingStores(userId string) ([]*models.ShoppingStore, error) {
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	stores := []*models.ShoppingStore{}
	if len(listIds) == 0 {
		return stores, nil
	}
	var rows []struct {
		StoreKey    string `gorm:"column:store_key"`
		StoreName   string `gorm:"column:store_name"`
		NeededCount int    `gorm:"column:needed_count"`
	}
	// The name shown is the one most items give, so a store typed two ways
	// is one row.
	if err := self.tx.Raw(`SELECT "store_key", mode() WITHIN GROUP (ORDER BY "store_name") AS store_name,
		count(*) FILTER (WHERE "shopping_item_state" = 'needed') AS needed_count
		FROM "shopping_item" WHERE "shopping_list_id" IN ? AND "store_key" <> ''
		GROUP BY "store_key" ORDER BY lower(mode() WITHIN GROUP (ORDER BY "store_name"))`, listIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	muted, err := self.ListShoppingStoreMuteKeys(userId)
	if err != nil {
		return nil, err
	}
	isMuted := map[string]bool{}
	for _, key := range muted {
		isMuted[key] = true
	}
	for _, row := range rows {
		stores = append(stores, &models.ShoppingStore{StoreName: row.StoreName, StoreKey: row.StoreKey, NeededCount: row.NeededCount, IsReminded: !isMuted[row.StoreKey]})
	}
	return stores, nil
}

func (self *transaction) ListShoppingStoreItems(userId, storeName string, shoppingListIds []string, isAnyStoreIncluded bool) ([]*models.ShoppingItem, error) {
	reachable, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	listIds := reachable
	if len(shoppingListIds) > 0 {
		isReachable := map[string]bool{}
		for _, id := range reachable {
			isReachable[id] = true
		}
		listIds = []string{}
		for _, id := range uniqueStrings(shoppingListIds) {
			if !isReachable[id] {
				return nil, ErrNotFound
			}
			listIds = append(listIds, id)
		}
	}
	if len(listIds) == 0 {
		return []*models.ShoppingItem{}, nil
	}
	storeKey := models.ShoppingStoreKey(storeName)
	query := self.tx.Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" IN ?", listIds,
		[]string{string(models.ShoppingItemStateNeeded), string(models.ShoppingItemStateInCart)})
	switch {
	case storeKey != "" && isAnyStoreIncluded:
		query = query.Where("\"store_key\" IN ?", []string{storeKey, ""})
	case storeKey != "":
		query = query.Where("\"store_key\" = ?", storeKey)
	case !isAnyStoreIncluded:
		return []*models.ShoppingItem{}, nil
	default:
		query = query.Where("\"store_key\" = ''")
	}
	// The store's own items first, by aisle; items for any store after them.
	return self.shoppingItemsOf(query.Order("(\"store_key\" = '') ASC, " + shoppingItemOrder).Limit(shoppingPageSizeMost * 2))
}

func (self *transaction) ListShoppingItemsNeededAt(userId, storeKey string, isAnyStoreIncluded bool) ([]*models.ShoppingItem, error) {
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	if len(listIds) == 0 || storeKey == "" {
		return []*models.ShoppingItem{}, nil
	}
	keys := []string{storeKey}
	if isAnyStoreIncluded {
		keys = append(keys, "")
	}
	return self.shoppingItemsOf(self.tx.Where(`"shopping_list_id" IN ? AND "shopping_item_state" = 'needed' AND "store_key" IN ?
		AND "shopping_list_id" NOT IN (SELECT "shopping_list_id" FROM "shopping_list_reminder_mute" WHERE "user_id" = ?)`,
		listIds, keys, userId).Order("(\"store_key\" = '') ASC, " + shoppingItemOrder).Limit(200))
}
