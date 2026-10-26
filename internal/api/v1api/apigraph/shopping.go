package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

// Shopping lists: a person's named lists of things to buy, shared with a
// person or a circle, checked off live at the store, by a receipt, and
// remembered with their prices. docs/subsystems/shopping.md says how they
// are used. Who reaches a list is decided in the database
// (GetShoppingListAccess); every operation here needs shopping:use, and the
// dashboard, the iPhone app, the command line and the agent's shopping tool
// all use the same ones.

// ShoppingQuery is the reads of shopping lists.
type ShoppingQuery interface {
	// The lists the caller reaches, their own first, each with its counts.
	ListShoppingLists(ctx context.Context) ([]*models.ShoppingList, error)

	// One list with a page of its items, and who is shopping it now.
	GetShoppingList(ctx context.Context, arguments GetShoppingListArguments) (*ShoppingListPageView, error)

	// One item on a list the caller reaches.
	GetShoppingItem(ctx context.Context, arguments ShoppingItemArguments) (*models.ShoppingItem, error)

	// The shares of one of the caller's own lists.
	ListShoppingListShares(ctx context.Context, arguments ShoppingListArguments) ([]*models.ShoppingListShare, error)

	// The stores the items of the caller's lists are for, with how many are
	// needed at each.
	ListShoppingStores(ctx context.Context) ([]*models.ShoppingStore, error)

	// The store screen: the needed and in-cart items for a store across the
	// lists the caller reaches (or those named), and the items for any store
	// when asked, by aisle; the lists that have items there, and who is
	// shopping now.
	ListShoppingStoreItems(ctx context.Context, arguments ListShoppingStoreItemsArguments) (*ShoppingStoreView, error)

	// The trips open now on the lists the caller reaches.
	ListOpenShoppingTrips(ctx context.Context) ([]*models.ShoppingTrip, error)

	// An item's prices, newest first.
	ListShoppingItemPrices(ctx context.Context, arguments ShoppingItemArguments) ([]*models.ShoppingItemPrice, error)

	// What one of the caller's receipts did to their lists: each item line
	// with its matches, and what is still needed on the lists it touched.
	ListShoppingReceiptMatches(ctx context.Context, arguments ShoppingReceiptArguments) (*ShoppingReceiptView, error)

	// Reads pasted text or uploaded pictures of a list into the items that
	// would be added, and what each would merge into on the list named;
	// nothing is added. Needs agent:use and a model.
	PreviewShoppingImport(ctx context.Context, arguments PreviewShoppingImportArguments) (*ShoppingImportView, error)
}

// ShoppingMutation is the changes to shopping lists.
type ShoppingMutation interface {
	CreateShoppingList(ctx context.Context, arguments CreateShoppingListArguments) (*models.ShoppingList, error)
	RenameShoppingList(ctx context.Context, arguments RenameShoppingListArguments) (*models.ShoppingList, error)

	// Deletes one of the caller's own lists with its items and prices.
	DeleteShoppingList(ctx context.Context, arguments ShoppingListArguments) (bool, error)

	// Gives a person or a circle one of the caller's own lists.
	ShareShoppingList(ctx context.Context, arguments ShareShoppingListArguments) (*models.ShoppingListShare, error)
	UnshareShoppingList(ctx context.Context, arguments UnshareShoppingListArguments) (bool, error)

	// Whether the caller is reminded of a list, or at a store, on arriving
	// at a store.
	SetShoppingListReminder(ctx context.Context, arguments SetShoppingListReminderArguments) (bool, error)
	SetShoppingStoreReminder(ctx context.Context, arguments SetShoppingStoreReminderArguments) (bool, error)

	// Adds items, merging one already on the list into it, and queues the
	// search for a picture of each new one.
	AddShoppingItems(ctx context.Context, arguments AddShoppingItemsArguments) ([]*ShoppingItemAddedView, error)
	UpdateShoppingItem(ctx context.Context, arguments UpdateShoppingItemArguments) (*models.ShoppingItem, error)
	RemoveShoppingItems(ctx context.Context, arguments RemoveShoppingItemsArguments) (int, error)

	// In the cart, or back to needed.
	CheckShoppingItem(ctx context.Context, arguments CheckShoppingItemArguments) (*models.ShoppingItem, error)

	// Needed again with a count, or not needed (bought, if it was in the cart).
	SetShoppingItemNeed(ctx context.Context, arguments SetShoppingItemNeedArguments) (*models.ShoppingItem, error)
	SetShoppingItemOutOfStock(ctx context.Context, arguments SetShoppingItemOutOfStockArguments) (*models.ShoppingItem, error)
	RecordShoppingItemPrice(ctx context.Context, arguments RecordShoppingItemPriceArguments) (*models.ShoppingItemPrice, error)

	// Queues the search for another picture of an item.
	FindShoppingItemPicture(ctx context.Context, arguments ShoppingItemArguments) (bool, error)

	// Gives an item a picture the caller uploaded, or none.
	SetShoppingItemPicture(ctx context.Context, arguments SetShoppingItemPictureArguments) (*models.ShoppingItem, error)

	// Starts the caller shopping a store for lists, telling the others the
	// lists reach; Finish buys what the caller put in the cart.
	StartShopping(ctx context.Context, arguments StartShoppingArguments) (*models.ShoppingTrip, error)
	FinishShopping(ctx context.Context, arguments FinishShoppingArguments) (*models.ShoppingTrip, error)

	// Queues the matching of one of the caller's receipts to their lists
	// again, for a receipt read before the lists had its items.
	MatchShoppingReceipt(ctx context.Context, arguments ShoppingReceiptArguments) (bool, error)
	DecideShoppingReceiptMatch(ctx context.Context, arguments DecideShoppingReceiptMatchArguments) (*models.ShoppingReceiptMatch, error)
	MatchShoppingReceiptLine(ctx context.Context, arguments MatchShoppingReceiptLineArguments) (*models.ShoppingReceiptMatch, error)
	UndoShoppingReceipt(ctx context.Context, arguments ShoppingReceiptArguments) (bool, error)
	AddShoppingItemFromReceiptLine(ctx context.Context, arguments AddShoppingItemFromReceiptLineArguments) (*models.ShoppingItem, error)
}

// ShoppingSubscription follows shopping lists as they change.
type ShoppingSubscription interface {
	// Says which list changed, each time a list the caller reaches changes
	// (or one they could reach until now); the client reads it again.
	ShoppingEvents(ctx context.Context) (<-chan *ShoppingEventView, error)
}

// ShoppingListPageView is one list with a page of its items.
type ShoppingListPageView struct {
	ShoppingList  *models.ShoppingList   `json:"shoppingList"`
	ShoppingItems []*models.ShoppingItem `json:"shoppingItems"`

	// How many of the list's items are in the states asked for.
	TotalCount int `json:"totalCount"`

	// Who is shopping this list now.
	ShoppingTrips []*models.ShoppingTrip `json:"shoppingTrips"`
}

// ShoppingStoreView is the store screen.
type ShoppingStoreView struct {
	StoreName     string                 `json:"storeName"`
	IsReminded    bool                   `json:"isReminded"`
	ShoppingItems []*models.ShoppingItem `json:"shoppingItems"`

	// Every list the caller reaches that has items here, and whether its
	// items are on the screen.
	ShoppingLists []*ShoppingStoreListView `json:"shoppingLists"`
	ShoppingTrips []*models.ShoppingTrip   `json:"shoppingTrips"`
}

// ShoppingStoreListView is a list on the store screen's buttons.
type ShoppingStoreListView struct {
	ShoppingListID string `json:"shoppingListId"`
	ListName       string `json:"listName"`
	NeededCount    int    `json:"neededCount"`
	IsIncluded     bool   `json:"isIncluded"`
}

// ShoppingItemAddedView is one item as added, and whether it was merged into
// one already on the list.
type ShoppingItemAddedView struct {
	ShoppingItem *models.ShoppingItem `json:"shoppingItem"`
	IsMerged     bool                 `json:"isMerged"`
}

// ShoppingImportView is a list as the model read it.
type ShoppingImportView struct {
	ShoppingItems []*ShoppingImportItemView `json:"shoppingItems"`
}

// ShoppingImportItemView is one item read, and the item already on the list
// it would merge into, when there is one.
type ShoppingImportItemView struct {
	ItemDescription string `json:"itemDescription"`
	ItemDetails     string `json:"itemDetails"`
	NeededQuantity  string `json:"neededQuantity"`
	QuantityUnit    string `json:"quantityUnit"`
	AisleName       string `json:"aisleName"`
	StoreName       string `json:"storeName"`
	IsStoreBrand    bool   `json:"isStoreBrand"`

	MergedShoppingItemID    string `json:"mergedShoppingItemId"`
	MergedShoppingItemState string `json:"mergedShoppingItemState"`
	MergedNeededQuantity    string `json:"mergedNeededQuantity"`
}

// ShoppingReceiptView is what one receipt did to the caller's lists.
type ShoppingReceiptView struct {
	FinanceReceiptID string `json:"financeReceiptId"`
	MerchantName     string `json:"merchantName"`
	PurchasedOn      string `json:"purchasedOn"`
	TotalAmount      string `json:"totalAmount"`
	CurrencyCode     string `json:"currencyCode"`

	ShoppingReceiptLines []*ShoppingReceiptLineView `json:"shoppingReceiptLines"`

	// The items still needed on the lists the receipt touched, for that
	// store or any store: what was not on the receipt.
	StillNeededItems []*models.ShoppingItem `json:"stillNeededItems"`
}

// ShoppingReceiptLineView is one item line of a receipt and its matches.
type ShoppingReceiptLineView struct {
	FinanceReceiptLineID string `json:"financeReceiptLineId"`
	LineNumber           int    `json:"lineNumber"`
	Description          string `json:"description"`
	Quantity             string `json:"quantity"`
	LineAmount           string `json:"lineAmount"`

	ShoppingReceiptMatches []*ShoppingReceiptMatchView `json:"shoppingReceiptMatches"`
}

// ShoppingReceiptMatchView is a match with its item.
type ShoppingReceiptMatchView struct {
	ShoppingReceiptMatch *models.ShoppingReceiptMatch `json:"shoppingReceiptMatch"`
	ShoppingItem         *models.ShoppingItem         `json:"shoppingItem"`
}

// ShoppingEventView names the list that changed.
type ShoppingEventView struct {
	ShoppingListID string `json:"shoppingListId"`
}

type ShoppingListArguments struct {
	ShoppingListID string `json:"shoppingListId"`
}

type ShoppingItemArguments struct {
	ShoppingItemID string `json:"shoppingItemId"`
}

type ShoppingReceiptArguments struct {
	FinanceReceiptID string `json:"financeReceiptId"`
}

type GetShoppingListArguments struct {
	ShoppingListID string `json:"shoppingListId"`

	// needed, in_cart, not_needed; every state when none is given
	ShoppingItemStates []string `json:"shoppingItemStates" graphapi:"nullable"`

	// Skip this many items; 0 by default
	Offset int `json:"offset" graphapi:"nullable"`

	// How many items, 200 by default and 500 at most
	PageSize int `json:"pageSize" graphapi:"nullable"`
}

type ListShoppingStoreItemsArguments struct {
	StoreName string `json:"storeName"`

	// The lists to include; every list with items there when none is given
	ShoppingListIDs []string `json:"shoppingListIds" graphapi:"nullable"`

	// Also the items for any store; true by default
	IsAnyStoreIncluded *bool `json:"isAnyStoreIncluded" graphapi:"nullable"`
}

type PreviewShoppingImportArguments struct {
	// The list the items would go on, to say what each would merge into
	ShoppingListID string `json:"shoppingListId" graphapi:"nullable"`

	// Pasted text of a list
	ImportText string `json:"importText" graphapi:"nullable"`

	// Pictures of a list uploaded as the caller's agent's attachments
	AgentAttachmentIDs []string `json:"agentAttachmentIds" graphapi:"nullable"`
}

type CreateShoppingListArguments struct {
	ListName string `json:"listName"`
}

type RenameShoppingListArguments struct {
	ShoppingListID string `json:"shoppingListId"`
	ListName       string `json:"listName"`
}

type ShareShoppingListArguments struct {
	ShoppingListID string `json:"shoppingListId"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`
}

type UnshareShoppingListArguments struct {
	ShoppingListShareID string `json:"shoppingListShareId"`
}

type SetShoppingListReminderArguments struct {
	ShoppingListID string `json:"shoppingListId"`
	IsReminded     bool   `json:"isReminded"`
}

type SetShoppingStoreReminderArguments struct {
	StoreName  string `json:"storeName"`
	IsReminded bool   `json:"isReminded"`
}

// ShoppingItemEntry is one item to add.
type ShoppingItemEntry struct {
	ItemDescription string `json:"itemDescription"`
	ItemDetails     string `json:"itemDetails" graphapi:"nullable"`

	// The store to get it from; leave out for any store
	StoreName    string `json:"storeName" graphapi:"nullable"`
	IsStoreBrand bool   `json:"isStoreBrand" graphapi:"nullable"`
	AisleName    string `json:"aisleName" graphapi:"nullable"`

	// How many, a number above zero; 1 by default
	NeededQuantity string `json:"neededQuantity" graphapi:"nullable"`
	QuantityUnit   string `json:"quantityUnit" graphapi:"nullable"`

	// The price of one, when known
	UnitPriceAmount string `json:"unitPriceAmount" graphapi:"nullable"`
	CurrencyCode    string `json:"currencyCode" graphapi:"nullable"`
}

type AddShoppingItemsArguments struct {
	ShoppingListID string              `json:"shoppingListId"`
	ShoppingItems  []ShoppingItemEntry `json:"shoppingItems"`
}

type UpdateShoppingItemArguments struct {
	ShoppingItemID  string  `json:"shoppingItemId"`
	ItemDescription *string `json:"itemDescription" graphapi:"nullable"`
	ItemDetails     *string `json:"itemDetails" graphapi:"nullable"`

	// The store to get it from; empty for any store
	StoreName      *string `json:"storeName" graphapi:"nullable"`
	IsStoreBrand   *bool   `json:"isStoreBrand" graphapi:"nullable"`
	AisleName      *string `json:"aisleName" graphapi:"nullable"`
	NeededQuantity *string `json:"neededQuantity" graphapi:"nullable"`
	QuantityUnit   *string `json:"quantityUnit" graphapi:"nullable"`
}

type RemoveShoppingItemsArguments struct {
	ShoppingItemIDs []string `json:"shoppingItemIds"`
}

type CheckShoppingItemArguments struct {
	ShoppingItemID string `json:"shoppingItemId"`
	IsChecked      bool   `json:"isChecked"`
}

type SetShoppingItemNeedArguments struct {
	ShoppingItemID string `json:"shoppingItemId"`
	IsNeeded       bool   `json:"isNeeded"`

	// How many are needed, when needed; 1 by default
	NeededQuantity string `json:"neededQuantity" graphapi:"nullable"`
}

type SetShoppingItemOutOfStockArguments struct {
	ShoppingItemID string `json:"shoppingItemId"`
	IsOutOfStock   bool   `json:"isOutOfStock"`

	// The store that had none; the item's store by default
	StoreName string `json:"storeName" graphapi:"nullable"`
}

type RecordShoppingItemPriceArguments struct {
	ShoppingItemID  string `json:"shoppingItemId"`
	UnitPriceAmount string `json:"unitPriceAmount"`

	// The store it was seen at; the item's store by default
	StoreName string `json:"storeName" graphapi:"nullable"`

	// The day it was seen, YYYY-MM-DD; today by default
	ObservedOn   string `json:"observedOn" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

type SetShoppingItemPictureArguments struct {
	ShoppingItemID string `json:"shoppingItemId"`

	// A picture uploaded as the caller's agent's attachment; leave out to
	// take the picture away
	AgentAttachmentID string `json:"agentAttachmentId" graphapi:"nullable"`
}

type StartShoppingArguments struct {
	// The store, or empty for none in particular
	StoreName       string   `json:"storeName" graphapi:"nullable"`
	ShoppingListIDs []string `json:"shoppingListIds"`
}

type FinishShoppingArguments struct {
	ShoppingTripID string `json:"shoppingTripId"`
}

type DecideShoppingReceiptMatchArguments struct {
	ShoppingReceiptMatchID string `json:"shoppingReceiptMatchId"`

	// true applies it; false dismisses it, undoing it when it was applied
	IsApplied bool `json:"isApplied"`
}

type MatchShoppingReceiptLineArguments struct {
	FinanceReceiptLineID string `json:"financeReceiptLineId"`
	ShoppingItemID       string `json:"shoppingItemId"`
}

type AddShoppingItemFromReceiptLineArguments struct {
	FinanceReceiptLineID string `json:"financeReceiptLineId"`
	ShoppingListID       string `json:"shoppingListId"`
}

// shoppingPerson is the signed-in person who may keep shopping lists.
func (self *graph) shoppingPerson(ctx context.Context) (*models.User, error) {
	principal, err := self.requirePermission(ctx, models.PermissionShoppingUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal.User, nil
}

// announceShopping tells the instances, once the change is committed, that
// these lists changed, and that they no longer reach endedFor.
func (self *graph) announceShopping(tx db.Transaction, shoppingListIds []string, endedForUserIds ...string) {
	tx.AfterCommit(func() {
		for _, listId := range shoppingListIds {
			if listId == "" {
				continue
			}
			if err := self.database.NotifyShoppingEvent(listId, endedForUserIds...); err != nil {
				log.Warningf("cannot announce a change to a shopping list: %s", err)
			}
		}
	})
}

// withPicturePaths fills in where each item's picture is served from.
func withPicturePaths(items ...*models.ShoppingItem) []*models.ShoppingItem {
	for _, item := range items {
		if item != nil && item.PictureFileID != "" {
			item.PicturePath = api.ShoppingPicturePath(item.ID, item.PictureFileID)
		}
	}
	return items
}

// withPicturePath is withPicturePaths for one item.
func withPicturePath(item *models.ShoppingItem) *models.ShoppingItem {
	withPicturePaths(item)
	return item
}

func (self *graph) ListShoppingLists(ctx context.Context) ([]*models.ShoppingList, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	lists, err := self.transaction(ctx).ListShoppingLists(user.ID)
	return lists, translateError(err)
}

func (self *graph) GetShoppingList(ctx context.Context, arguments GetShoppingListArguments) (*ShoppingListPageView, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	list, err := tx.GetShoppingList(user.ID, arguments.ShoppingListID)
	if err != nil {
		return nil, translateError(err)
	}
	states := []models.ShoppingItemState{}
	for _, state := range arguments.ShoppingItemStates {
		parsed := models.ShoppingItemState(strings.TrimSpace(state))
		if !parsed.IsValid() {
			return nil, fmt.Errorf("%w: an item's state is needed, in_cart or not_needed", api.ErrInvalidArguments)
		}
		states = append(states, parsed)
	}
	pageSize := arguments.PageSize
	if pageSize == 0 {
		pageSize = 200
	}
	items, total, err := tx.ListShoppingItems(user.ID, list.ID, states, arguments.Offset, pageSize)
	if err != nil {
		return nil, translateError(err)
	}
	trips, err := tx.ListOpenShoppingTrips(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	onList := []*models.ShoppingTrip{}
	for _, trip := range trips {
		for _, listId := range trip.ShoppingListIDs {
			if listId == list.ID {
				onList = append(onList, trip)
				break
			}
		}
	}
	return &ShoppingListPageView{ShoppingList: list, ShoppingItems: withPicturePaths(items...), TotalCount: int(total), ShoppingTrips: onList}, nil
}

func (self *graph) GetShoppingItem(ctx context.Context, arguments ShoppingItemArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	item, err := self.transaction(ctx).GetShoppingItem(user.ID, arguments.ShoppingItemID)
	if err != nil {
		return nil, translateError(err)
	}
	return withPicturePath(item), nil
}

func (self *graph) ListShoppingListShares(ctx context.Context, arguments ShoppingListArguments) ([]*models.ShoppingListShare, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	shares, err := self.transaction(ctx).ListShoppingListShares(user.ID, arguments.ShoppingListID)
	return shares, translateError(err)
}

func (self *graph) ListShoppingStores(ctx context.Context) ([]*models.ShoppingStore, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := self.transaction(ctx).ListShoppingStores(user.ID)
	return stores, translateError(err)
}

func (self *graph) ListShoppingStoreItems(ctx context.Context, arguments ListShoppingStoreItemsArguments) (*ShoppingStoreView, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	isAnyStoreIncluded := arguments.IsAnyStoreIncluded == nil || *arguments.IsAnyStoreIncluded
	storeName := strings.TrimSpace(arguments.StoreName)
	// Every list with items here, for the buttons, whichever are included.
	everything, err := tx.ListShoppingStoreItems(user.ID, storeName, nil, isAnyStoreIncluded)
	if err != nil {
		return nil, translateError(err)
	}
	isIncluded := map[string]bool{}
	for _, id := range arguments.ShoppingListIDs {
		isIncluded[id] = true
	}
	lists := []*ShoppingStoreListView{}
	byList := map[string]*ShoppingStoreListView{}
	for _, item := range everything {
		list := byList[item.ShoppingListID]
		if list == nil {
			list = &ShoppingStoreListView{ShoppingListID: item.ShoppingListID, ListName: item.ListName,
				IsIncluded: len(arguments.ShoppingListIDs) == 0 || isIncluded[item.ShoppingListID]}
			byList[item.ShoppingListID] = list
			lists = append(lists, list)
		}
		if item.ShoppingItemState == models.ShoppingItemStateNeeded {
			list.NeededCount++
		}
	}
	// A list named that has nothing here is still a button, switched on.
	for _, id := range arguments.ShoppingListIDs {
		if byList[id] == nil {
			found, err := tx.GetShoppingList(user.ID, id)
			if err != nil {
				return nil, translateError(err)
			}
			list := &ShoppingStoreListView{ShoppingListID: found.ID, ListName: found.ListName, IsIncluded: true}
			byList[id] = list
			lists = append(lists, list)
		}
	}
	items := []*models.ShoppingItem{}
	for _, item := range everything {
		if byList[item.ShoppingListID].IsIncluded {
			items = append(items, item)
		}
	}
	trips, err := tx.ListOpenShoppingTrips(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	muted, err := tx.ListShoppingStoreMuteKeys(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	isReminded := true
	for _, key := range muted {
		if key == models.ShoppingStoreKey(storeName) {
			isReminded = false
		}
	}
	return &ShoppingStoreView{StoreName: storeName, IsReminded: isReminded, ShoppingItems: withPicturePaths(items...), ShoppingLists: lists, ShoppingTrips: trips}, nil
}

func (self *graph) ListOpenShoppingTrips(ctx context.Context) ([]*models.ShoppingTrip, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	trips, err := self.transaction(ctx).ListOpenShoppingTrips(user.ID)
	return trips, translateError(err)
}

func (self *graph) ListShoppingItemPrices(ctx context.Context, arguments ShoppingItemArguments) ([]*models.ShoppingItemPrice, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	prices, err := self.transaction(ctx).ListShoppingItemPrices(user.ID, arguments.ShoppingItemID)
	return prices, translateError(err)
}

func (self *graph) ListShoppingReceiptMatches(ctx context.Context, arguments ShoppingReceiptArguments) (*ShoppingReceiptView, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	owner, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, api.ErrNotFound
	}
	receipt, err := tx.GetFinanceReceipt(owner.ID, arguments.FinanceReceiptID)
	if err != nil {
		return nil, translateError(err)
	}
	if receipt == nil {
		return nil, api.ErrNotFound
	}
	matches, err := tx.ListShoppingReceiptMatches(user.ID, receipt.ID)
	if err != nil {
		return nil, translateError(err)
	}
	view := &ShoppingReceiptView{
		FinanceReceiptID: receipt.ID, MerchantName: receipt.MerchantName, PurchasedOn: receipt.PurchasedOn, TotalAmount: receipt.TotalAmount,
		CurrencyCode: receipt.CurrencyCode, ShoppingReceiptLines: []*ShoppingReceiptLineView{}, StillNeededItems: []*models.ShoppingItem{},
	}
	byLine := map[string][]*models.ShoppingReceiptMatch{}
	touchedListIds := map[string]bool{}
	items := map[string]*models.ShoppingItem{}
	for _, match := range matches {
		byLine[match.FinanceReceiptLineID] = append(byLine[match.FinanceReceiptLineID], match)
		item, err := tx.GetShoppingItem(user.ID, match.ShoppingItemID)
		if err != nil {
			return nil, translateError(err)
		}
		items[item.ID] = withPicturePath(item)
		if match.ReceiptMatchState == models.ReceiptMatchStateApplied {
			touchedListIds[item.ShoppingListID] = true
		}
	}
	for _, line := range receipt.ReceiptLines {
		if line.ReceiptLineKind != models.ReceiptLineKindItem {
			continue
		}
		lineView := &ShoppingReceiptLineView{
			FinanceReceiptLineID: line.ID, LineNumber: line.LineNumber, Description: line.Description, Quantity: line.Quantity,
			LineAmount: line.LineAmount, ShoppingReceiptMatches: []*ShoppingReceiptMatchView{},
		}
		for _, match := range byLine[line.ID] {
			lineView.ShoppingReceiptMatches = append(lineView.ShoppingReceiptMatches, &ShoppingReceiptMatchView{ShoppingReceiptMatch: match, ShoppingItem: items[match.ShoppingItemID]})
		}
		view.ShoppingReceiptLines = append(view.ShoppingReceiptLines, lineView)
	}
	if len(touchedListIds) > 0 {
		candidates, err := tx.ListShoppingReceiptCandidates(user.ID, receipt.MerchantName)
		if err != nil {
			return nil, translateError(err)
		}
		for _, item := range candidates {
			if touchedListIds[item.ShoppingListID] && item.ShoppingItemState == models.ShoppingItemStateNeeded {
				view.StillNeededItems = append(view.StillNeededItems, withPicturePath(item))
			}
		}
	}
	return view, nil
}

func (self *graph) PreviewShoppingImport(ctx context.Context, arguments PreviewShoppingImportArguments) (*ShoppingImportView, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, fmt.Errorf("%w: reading a list needs the agent, which this server does not run", api.ErrInvalidArguments)
	}
	var owner *models.Agent
	shoppingListId := strings.TrimSpace(arguments.ShoppingListID)
	// Read outside the request's transaction: the model is asked in between,
	// and a transaction held open across a model's answer holds a connection.
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if shoppingListId != "" {
			if _, err := tx.GetShoppingListAccess(user.ID, shoppingListId); err != nil {
				return err
			}
		}
		owner, err = tx.GetAgentByUser(user.ID)
		return err
	}); err != nil {
		return nil, translateError(err)
	}
	if owner == nil {
		return nil, fmt.Errorf("%w: reading a list needs your agent; set it up first", api.ErrInvalidArguments)
	}
	read, err := worker.ReadShoppingList(ctx, owner, user, arguments.ImportText, arguments.AgentAttachmentIDs)
	if errors.Is(err, agent.ErrUnavailable) {
		return nil, fmt.Errorf("%w: no model is set up to read a list", api.ErrInvalidArguments)
	}
	if err != nil {
		return nil, translateError(err)
	}
	view := &ShoppingImportView{ShoppingItems: []*ShoppingImportItemView{}}
	for _, item := range read {
		itemView := &ShoppingImportItemView{
			ItemDescription: item.ItemDescription, ItemDetails: item.ItemDetails, NeededQuantity: item.NeededQuantity, QuantityUnit: item.QuantityUnit,
			AisleName: item.AisleName, StoreName: item.StoreName, IsStoreBrand: item.IsStoreBrand,
		}
		if itemView.NeededQuantity == "" {
			itemView.NeededQuantity = "1"
		}
		if shoppingListId != "" {
			merged, err := self.transaction(ctx).FindShoppingItemToMerge(user.ID, shoppingListId, item.ItemDescription, item.StoreName)
			if err != nil {
				return nil, translateError(err)
			}
			if merged != nil {
				itemView.MergedShoppingItemID, itemView.MergedShoppingItemState, itemView.MergedNeededQuantity = merged.ID, string(merged.ShoppingItemState), merged.NeededQuantity
			}
		}
		view.ShoppingItems = append(view.ShoppingItems, itemView)
	}
	return view, nil
}

func (self *graph) CreateShoppingList(ctx context.Context, arguments CreateShoppingListArguments) (*models.ShoppingList, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	list, err := tx.CreateShoppingList(user.ID, arguments.ListName)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{list.ID})
	return list, nil
}

func (self *graph) RenameShoppingList(ctx context.Context, arguments RenameShoppingListArguments) (*models.ShoppingList, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	list, err := tx.RenameShoppingList(user.ID, arguments.ShoppingListID, arguments.ListName)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{list.ID})
	return list, nil
}

// removeShoppingPicturesAfterCommit deletes the files of pictures whose items
// are gone, once their removal is committed.
func (self *graph) removeShoppingPicturesAfterCommit(tx db.Transaction, items []*models.ShoppingItem) {
	pictureFileIds := []string{}
	for _, item := range items {
		if item.PictureFileID != "" {
			pictureFileIds = append(pictureFileIds, item.PictureFileID)
		}
	}
	if len(pictureFileIds) == 0 || self.storage == nil {
		return
	}
	store := self.storage
	tx.AfterCommit(func() {
		for _, pictureFileId := range pictureFileIds {
			if err := store.DeleteFile(context.Background(), pictureFileId); err != nil {
				log.Warningf("cannot remove a shopping item's picture: %s", err)
			}
		}
	})
}

func (self *graph) DeleteShoppingList(ctx context.Context, arguments ShoppingListArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	access, err := tx.GetShoppingListAccess(user.ID, arguments.ShoppingListID)
	if err != nil {
		return false, translateError(err)
	}
	if !access.IsOwner {
		return false, api.ErrNotFound
	}
	readers, err := tx.ListShoppingListReaderUserIDs(arguments.ShoppingListID)
	if err != nil {
		return false, translateError(err)
	}
	// Its items' pictures go with it, read before the rows do.
	items := []*models.ShoppingItem{}
	for offset := 0; ; offset += 500 {
		page, total, err := tx.ListShoppingItems(user.ID, arguments.ShoppingListID, nil, offset, 500)
		if err != nil {
			return false, translateError(err)
		}
		items = append(items, page...)
		if len(page) == 0 || int64(offset+len(page)) >= total {
			break
		}
	}
	if err := tx.DeleteShoppingList(user.ID, arguments.ShoppingListID); err != nil {
		return false, translateError(err)
	}
	self.removeShoppingPicturesAfterCommit(tx, items)
	self.announceShopping(tx, []string{arguments.ShoppingListID}, readers...)
	log.Noticef("%s deleted a shopping list", user.Username)
	return true, nil
}

func (self *graph) ShareShoppingList(ctx context.Context, arguments ShareShoppingListArguments) (*models.ShoppingListShare, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	earlier, err := tx.ListShoppingListShares(user.ID, arguments.ShoppingListID)
	if err != nil {
		return nil, translateError(err)
	}
	share, err := tx.ShareShoppingList(user.ID, &models.ShoppingListShare{
		ShoppingListID: arguments.ShoppingListID, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in, a list that is not theirs: all answer as nothing
		// found, because which of them it was is not the caller's to learn.
		return nil, translateError(err)
	}
	isNew := true
	for _, before := range earlier {
		if before.ID == share.ID {
			isNew = false
		}
	}
	if isNew {
		if err := self.tellAboutShare(ctx, user, models.ShareReceivedNote{
			SharerName: user.DisplayName(), ShareKind: models.ShareKindShopping, ShareSubject: share.ListName, ShareSubjectID: share.ShoppingListID,
		}, granteeUserId, granteeCircleId); err != nil {
			return nil, err
		}
	}
	self.announceShopping(tx, []string{share.ShoppingListID})
	log.Noticef("%s shared a shopping list", user.Username)
	return share, nil
}

func (self *graph) UnshareShoppingList(ctx context.Context, arguments UnshareShoppingListArguments) (bool, error) {
	// Like every kind's way to stop a share, it needs no permission of the
	// kind, so a person whose role lost shopping:use can still take back what
	// they shared (requireShareOwner).
	principal, err := self.requireShareOwner(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	shares, err := tx.ListShoppingListSharesBy(principal.User.ID)
	if err != nil {
		return false, translateError(err)
	}
	listId := ""
	for _, share := range shares {
		if share.ID == arguments.ShoppingListShareID {
			listId = share.ShoppingListID
		}
	}
	before := []string{}
	if listId != "" {
		if before, err = tx.ListShoppingListReaderUserIDs(listId); err != nil {
			return false, translateError(err)
		}
	}
	if err := tx.DeleteShoppingListShare(principal.User.ID, arguments.ShoppingListShareID); err != nil {
		return false, translateError(err)
	}
	after, err := tx.ListShoppingListReaderUserIDs(listId)
	if err != nil {
		return false, translateError(err)
	}
	isStill := map[string]bool{}
	for _, id := range after {
		isStill[id] = true
	}
	ended := []string{}
	for _, id := range before {
		if !isStill[id] {
			ended = append(ended, id)
		}
	}
	self.announceShopping(tx, []string{listId}, ended...)
	return true, nil
}

func (self *graph) SetShoppingListReminder(ctx context.Context, arguments SetShoppingListReminderArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).SetShoppingListReminder(user.ID, arguments.ShoppingListID, arguments.IsReminded); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

func (self *graph) SetShoppingStoreReminder(ctx context.Context, arguments SetShoppingStoreReminderArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).SetShoppingStoreReminder(user.ID, arguments.StoreName, arguments.IsReminded); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

// shoppingPicturesQueuedMost is how many picture searches one call to add
// items queues: an import of a long list gets pictures for its first items,
// and the rest can be asked for one at a time.
const shoppingPicturesQueuedMost = 60

func (self *graph) AddShoppingItems(ctx context.Context, arguments AddShoppingItemsArguments) ([]*ShoppingItemAddedView, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	given := make([]*models.ShoppingItem, 0, len(arguments.ShoppingItems))
	for _, input := range arguments.ShoppingItems {
		given = append(given, &models.ShoppingItem{
			ItemDescription: input.ItemDescription, ItemDetails: input.ItemDetails, StoreName: input.StoreName, IsStoreBrand: input.IsStoreBrand,
			AisleName: input.AisleName, NeededQuantity: input.NeededQuantity, QuantityUnit: input.QuantityUnit,
			UnitPriceAmount: input.UnitPriceAmount, CurrencyCode: input.CurrencyCode,
		})
	}
	tx := self.transaction(ctx)
	stored, isMerged, err := tx.AddShoppingItems(user.ID, arguments.ShoppingListID, given)
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]*ShoppingItemAddedView, 0, len(stored))
	queuedCount := 0
	worker := self.agentWorker()
	var owner *models.Agent
	if worker != nil {
		if owner, err = tx.GetAgentByUser(user.ID); err != nil {
			return nil, err
		}
	}
	for index, item := range stored {
		views = append(views, &ShoppingItemAddedView{ShoppingItem: withPicturePath(item), IsMerged: isMerged[index]})
		if worker != nil && owner != nil && owner.Enabled && item.PictureFileID == "" && item.PictureSearchedAt == nil && queuedCount < shoppingPicturesQueuedMost {
			if err := worker.QueueShoppingPicture(tx, owner.ID, item.ID); err != nil {
				return nil, err
			}
			queuedCount++
		}
	}
	self.announceShopping(tx, []string{arguments.ShoppingListID})
	return views, nil
}

func (self *graph) UpdateShoppingItem(ctx context.Context, arguments UpdateShoppingItemArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.UpdateShoppingItem(user.ID, arguments.ShoppingItemID, &models.ShoppingItemChange{
		ItemDescription: arguments.ItemDescription, ItemDetails: arguments.ItemDetails, StoreName: arguments.StoreName,
		IsStoreBrand: arguments.IsStoreBrand, AisleName: arguments.AisleName, NeededQuantity: arguments.NeededQuantity, QuantityUnit: arguments.QuantityUnit,
	})
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{item.ShoppingListID})
	return withPicturePath(item), nil
}

func (self *graph) RemoveShoppingItems(ctx context.Context, arguments RemoveShoppingItemsArguments) (int, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return 0, err
	}
	tx := self.transaction(ctx)
	removed := []*models.ShoppingItem{}
	for _, shoppingItemId := range arguments.ShoppingItemIDs {
		item, err := tx.GetShoppingItem(user.ID, shoppingItemId)
		if err != nil {
			return 0, translateError(err)
		}
		removed = append(removed, item)
	}
	listIds, err := tx.RemoveShoppingItems(user.ID, arguments.ShoppingItemIDs)
	if err != nil {
		return 0, translateError(err)
	}
	self.removeShoppingPicturesAfterCommit(tx, removed)
	self.announceShopping(tx, listIds)
	return len(arguments.ShoppingItemIDs), nil
}

// changedShoppingItem announces an item's change and answers it.
func (self *graph) changedShoppingItem(tx db.Transaction, item *models.ShoppingItem, err error) (*models.ShoppingItem, error) {
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{item.ShoppingListID})
	return withPicturePath(item), nil
}

func (self *graph) CheckShoppingItem(ctx context.Context, arguments CheckShoppingItemArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.CheckShoppingItem(user.ID, arguments.ShoppingItemID, arguments.IsChecked)
	return self.changedShoppingItem(tx, item, err)
}

func (self *graph) SetShoppingItemNeed(ctx context.Context, arguments SetShoppingItemNeedArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.SetShoppingItemNeed(user.ID, arguments.ShoppingItemID, arguments.IsNeeded, arguments.NeededQuantity)
	return self.changedShoppingItem(tx, item, err)
}

func (self *graph) SetShoppingItemOutOfStock(ctx context.Context, arguments SetShoppingItemOutOfStockArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.SetShoppingItemOutOfStock(user.ID, arguments.ShoppingItemID, arguments.IsOutOfStock, arguments.StoreName)
	return self.changedShoppingItem(tx, item, err)
}

func (self *graph) RecordShoppingItemPrice(ctx context.Context, arguments RecordShoppingItemPriceArguments) (*models.ShoppingItemPrice, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	price, err := tx.RecordShoppingItemPrice(user.ID, &models.ShoppingItemPrice{
		ShoppingItemID: arguments.ShoppingItemID, UnitPriceAmount: arguments.UnitPriceAmount, StoreName: arguments.StoreName,
		ObservedOn: arguments.ObservedOn, CurrencyCode: arguments.CurrencyCode, PriceSource: models.ShoppingPriceSourcePerson,
	})
	if err != nil {
		return nil, translateError(err)
	}
	item, err := tx.GetShoppingItemByID(price.ShoppingItemID)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{item.ShoppingListID})
	return price, nil
}

func (self *graph) FindShoppingItemPicture(ctx context.Context, arguments ShoppingItemArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, fmt.Errorf("%w: finding a picture needs the agent, which this server does not run", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	item, err := tx.GetShoppingItem(user.ID, arguments.ShoppingItemID)
	if err != nil {
		return false, translateError(err)
	}
	owner, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return false, err
	}
	if owner == nil || !owner.Enabled {
		return false, fmt.Errorf("%w: finding a picture needs your agent; turn it on first", api.ErrInvalidArguments)
	}
	if err := worker.QueueShoppingPicture(tx, owner.ID, item.ID); err != nil {
		return false, err
	}
	return true, nil
}

func (self *graph) SetShoppingItemPicture(ctx context.Context, arguments SetShoppingItemPictureArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.GetShoppingItem(user.ID, arguments.ShoppingItemID)
	if err != nil {
		return nil, translateError(err)
	}
	pictureFileId := ""
	if attachmentId := strings.TrimSpace(arguments.AgentAttachmentID); attachmentId != "" {
		owner, err := tx.GetAgentByUser(user.ID)
		if err != nil {
			return nil, err
		}
		attachment, err := tx.GetAgentAttachment(attachmentId)
		if err != nil {
			return nil, err
		}
		if owner == nil || attachment == nil || attachment.AgentID != owner.ID {
			return nil, api.ErrNotFound
		}
		if !agent.IsImageAttachment(attachment.ContentType) {
			return nil, fmt.Errorf("%w: %s is not a picture", api.ErrInvalidArguments, attachment.Name)
		}
		content, err := self.storage.GetFile(ctx, attachment.ID)
		if err != nil {
			return nil, err
		}
		picture, err := linkpreview.KeepImage(ctx, content)
		if err != nil {
			return nil, fmt.Errorf("%w: %s cannot be read as a picture", api.ErrInvalidArguments, attachment.Name)
		}
		pictureFileId = fmt.Sprintf("shopping-picture-%s-%s", item.ID, attachment.ID)
		if err := self.storage.PutFile(ctx, pictureFileId, picture.Content); err != nil {
			return nil, err
		}
	}
	source := models.ShoppingPictureSourceUpload
	previousFileId, err := tx.SetShoppingItemPicture(item.ID, pictureFileId, "", source)
	if err != nil {
		return nil, translateError(err)
	}
	if previousFileId != "" && previousFileId != pictureFileId {
		storage := self.storage
		tx.AfterCommit(func() {
			if err := storage.DeleteFile(context.Background(), previousFileId); err != nil {
				log.Warningf("cannot remove a shopping item's old picture: %s", err)
			}
		})
	}
	changed, err := tx.GetShoppingItemByID(item.ID)
	return self.changedShoppingItem(tx, changed, err)
}

func (self *graph) StartShopping(ctx context.Context, arguments StartShoppingArguments) (*models.ShoppingTrip, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	trip, boughtListIds, err := tx.StartShoppingTrip(user.ID, arguments.StoreName, arguments.ShoppingListIDs)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, append(append([]string{}, trip.ShoppingListIDs...), boughtListIds...))
	if notifier := self.phoneNotifier(); notifier != nil {
		detached := context.WithoutCancel(ctx)
		tx.AfterCommit(func() { notifier.NotifyShoppingStarted(detached, user, trip) })
	}
	return trip, nil
}

func (self *graph) FinishShopping(ctx context.Context, arguments FinishShoppingArguments) (*models.ShoppingTrip, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	trip, err := tx.FinishShoppingTrip(user.ID, arguments.ShoppingTripID)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, trip.ShoppingListIDs)
	return trip, nil
}

func (self *graph) MatchShoppingReceipt(ctx context.Context, arguments ShoppingReceiptArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, fmt.Errorf("%w: matching a receipt needs the agent, which this server does not run", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	owner, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return false, err
	}
	if owner == nil {
		return false, api.ErrNotFound
	}
	receipt, err := tx.GetFinanceReceipt(owner.ID, arguments.FinanceReceiptID)
	if err != nil {
		return false, translateError(err)
	}
	if receipt == nil {
		return false, api.ErrNotFound
	}
	if err := worker.QueueShoppingReceiptMatch(tx, owner, receipt); err != nil {
		return false, err
	}
	return true, nil
}

func (self *graph) DecideShoppingReceiptMatch(ctx context.Context, arguments DecideShoppingReceiptMatchArguments) (*models.ShoppingReceiptMatch, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	match, listId, err := tx.DecideShoppingReceiptMatch(user.ID, arguments.ShoppingReceiptMatchID, arguments.IsApplied)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{listId})
	return match, nil
}

func (self *graph) MatchShoppingReceiptLine(ctx context.Context, arguments MatchShoppingReceiptLineArguments) (*models.ShoppingReceiptMatch, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	match, listId, err := tx.MatchShoppingReceiptLine(user.ID, arguments.FinanceReceiptLineID, arguments.ShoppingItemID)
	if err != nil {
		return nil, translateError(err)
	}
	self.announceShopping(tx, []string{listId})
	return match, nil
}

func (self *graph) UndoShoppingReceipt(ctx context.Context, arguments ShoppingReceiptArguments) (bool, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	listIds, err := tx.UndoShoppingReceipt(user.ID, arguments.FinanceReceiptID)
	if err != nil {
		return false, translateError(err)
	}
	self.announceShopping(tx, listIds)
	return true, nil
}

func (self *graph) AddShoppingItemFromReceiptLine(ctx context.Context, arguments AddShoppingItemFromReceiptLineArguments) (*models.ShoppingItem, error) {
	user, err := self.shoppingPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	item, err := tx.AddShoppingItemFromReceiptLine(user.ID, arguments.FinanceReceiptLineID, arguments.ShoppingListID)
	return self.changedShoppingItem(tx, item, err)
}
