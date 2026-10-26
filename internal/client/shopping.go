package client

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/models"
)

// Shopping lists: the same operations as the dashboard's Shopping page and
// the iPhone app, for `teanode shopping` and the agent's shopping tool. The
// live subscription (ShoppingEvents) is left out: a command or a tool call
// reads once.

const (
	shoppingListSelection = `{ id userId listName createdAt updatedAt ownerUsername ownerName isOwner isReminded isShared neededCount inCartCount notNeededCount }`

	shoppingItemSelection = `{ id shoppingListId listName itemDescription itemDetails storeName storeKey isStoreBrand aisleName aisleRank itemRank neededQuantity quantityUnit unitPriceAmount currencyCode shoppingItemState cartedByUserId cartedByName cartedAt outOfStockAt outOfStockByUserId outOfStockByName outOfStockStoreName lastBoughtAt pictureFileId picturePageUrl picturePath pictureSource pictureSearchedAt createdByUserId createdAt updatedAt }`

	shoppingTripSelection = `{ id userId storeName storeKey startedAt finishedAt shoppingListIds shopperUsername shopperName }`

	shoppingShareSelection = `{ id shoppingListId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName createdByUserId createdAt listName ownerUserId ownerUsername ownerName }`

	shoppingPriceSelection = `{ id shoppingItemId storeName storeKey unitPriceAmount currencyCode quantity quantityUnit observedOn priceSource financeReceiptId createdByUserId createdAt }`

	shoppingReceiptMatchSelection = `{ id financeReceiptId financeReceiptLineId shoppingItemId receiptMatchState matchedQuantity previousShoppingItemState previousNeededQuantity shoppingItemPriceId decidedByUserId createdAt decidedAt }`

	shoppingStoreSelection = `{ storeName storeKey neededCount isReminded }`

	shoppingImportItemSelection = `{ itemDescription itemDetails neededQuantity quantityUnit aisleName storeName isStoreBrand mergedShoppingItemId mergedShoppingItemState mergedNeededQuantity }`
)

const (
	DocumentListShoppingLists = `query { ListShoppingLists ` + shoppingListSelection + ` }`

	DocumentGetShoppingList = `query ($shoppingListId: String!, $shoppingItemStates: [String!], $offset: Int, $pageSize: Int) { GetShoppingList(shoppingListId: $shoppingListId, shoppingItemStates: $shoppingItemStates, offset: $offset, pageSize: $pageSize) { totalCount shoppingList ` + shoppingListSelection + ` shoppingItems ` + shoppingItemSelection + ` shoppingTrips ` + shoppingTripSelection + ` } }`

	DocumentGetShoppingItem = `query ($shoppingItemId: String!) { GetShoppingItem(shoppingItemId: $shoppingItemId) ` + shoppingItemSelection + ` }`

	DocumentListShoppingListShares = `query ($shoppingListId: String!) { ListShoppingListShares(shoppingListId: $shoppingListId) ` + shoppingShareSelection + ` }`

	DocumentListShoppingStores = `query { ListShoppingStores ` + shoppingStoreSelection + ` }`

	DocumentListShoppingStoreItems = `query ($storeName: String!, $shoppingListIds: [String!], $isAnyStoreIncluded: Boolean) { ListShoppingStoreItems(storeName: $storeName, shoppingListIds: $shoppingListIds, isAnyStoreIncluded: $isAnyStoreIncluded) { storeName isReminded shoppingItems ` + shoppingItemSelection + ` shoppingLists { shoppingListId listName neededCount isIncluded } shoppingTrips ` + shoppingTripSelection + ` } }`

	DocumentListOpenShoppingTrips = `query { ListOpenShoppingTrips ` + shoppingTripSelection + ` }`

	DocumentListShoppingItemPrices = `query ($shoppingItemId: String!) { ListShoppingItemPrices(shoppingItemId: $shoppingItemId) ` + shoppingPriceSelection + ` }`

	DocumentListShoppingReceiptMatches = `query ($financeReceiptId: String!) { ListShoppingReceiptMatches(financeReceiptId: $financeReceiptId) { financeReceiptId merchantName purchasedOn totalAmount currencyCode shoppingReceiptLines { financeReceiptLineId lineNumber description quantity lineAmount shoppingReceiptMatches { shoppingReceiptMatch ` + shoppingReceiptMatchSelection + ` shoppingItem ` + shoppingItemSelection + ` } } stillNeededItems ` + shoppingItemSelection + ` } }`

	DocumentPreviewShoppingImport = `query ($shoppingListId: String, $importText: String, $agentAttachmentIds: [String!]) { PreviewShoppingImport(shoppingListId: $shoppingListId, importText: $importText, agentAttachmentIds: $agentAttachmentIds) { shoppingItems ` + shoppingImportItemSelection + ` } }`

	DocumentCreateShoppingList = `mutation ($listName: String!) { CreateShoppingList(listName: $listName) ` + shoppingListSelection + ` }`

	DocumentRenameShoppingList = `mutation ($shoppingListId: String!, $listName: String!) { RenameShoppingList(shoppingListId: $shoppingListId, listName: $listName) ` + shoppingListSelection + ` }`

	DocumentDeleteShoppingList = `mutation ($shoppingListId: String!) { DeleteShoppingList(shoppingListId: $shoppingListId) }`

	DocumentShareShoppingList = `mutation ($shoppingListId: String!, $granteeUserId: String, $granteeCircleId: String) { ShareShoppingList(shoppingListId: $shoppingListId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) ` + shoppingShareSelection + ` }`

	DocumentUnshareShoppingList = `mutation ($shoppingListShareId: String!) { UnshareShoppingList(shoppingListShareId: $shoppingListShareId) }`

	DocumentSetShoppingListReminder = `mutation ($shoppingListId: String!, $isReminded: Boolean!) { SetShoppingListReminder(shoppingListId: $shoppingListId, isReminded: $isReminded) }`

	DocumentSetShoppingStoreReminder = `mutation ($storeName: String!, $isReminded: Boolean!) { SetShoppingStoreReminder(storeName: $storeName, isReminded: $isReminded) }`

	DocumentAddShoppingItems = `mutation ($shoppingListId: String!, $shoppingItems: [ShoppingItemEntryInput!]!) { AddShoppingItems(shoppingListId: $shoppingListId, shoppingItems: $shoppingItems) { isMerged shoppingItem ` + shoppingItemSelection + ` } }`

	DocumentUpdateShoppingItem = `mutation ($shoppingItemId: String!, $itemDescription: String, $itemDetails: String, $storeName: String, $isStoreBrand: Boolean, $aisleName: String, $neededQuantity: String, $quantityUnit: String) { UpdateShoppingItem(shoppingItemId: $shoppingItemId, itemDescription: $itemDescription, itemDetails: $itemDetails, storeName: $storeName, isStoreBrand: $isStoreBrand, aisleName: $aisleName, neededQuantity: $neededQuantity, quantityUnit: $quantityUnit) ` + shoppingItemSelection + ` }`

	DocumentRemoveShoppingItems = `mutation ($shoppingItemIds: [String!]!) { RemoveShoppingItems(shoppingItemIds: $shoppingItemIds) }`

	DocumentCheckShoppingItem = `mutation ($shoppingItemId: String!, $isChecked: Boolean!) { CheckShoppingItem(shoppingItemId: $shoppingItemId, isChecked: $isChecked) ` + shoppingItemSelection + ` }`

	DocumentSetShoppingItemNeed = `mutation ($shoppingItemId: String!, $isNeeded: Boolean!, $neededQuantity: String) { SetShoppingItemNeed(shoppingItemId: $shoppingItemId, isNeeded: $isNeeded, neededQuantity: $neededQuantity) ` + shoppingItemSelection + ` }`

	DocumentSetShoppingItemOutOfStock = `mutation ($shoppingItemId: String!, $isOutOfStock: Boolean!, $storeName: String) { SetShoppingItemOutOfStock(shoppingItemId: $shoppingItemId, isOutOfStock: $isOutOfStock, storeName: $storeName) ` + shoppingItemSelection + ` }`

	DocumentRecordShoppingItemPrice = `mutation ($shoppingItemId: String!, $unitPriceAmount: String!, $storeName: String, $observedOn: String, $currencyCode: String) { RecordShoppingItemPrice(shoppingItemId: $shoppingItemId, unitPriceAmount: $unitPriceAmount, storeName: $storeName, observedOn: $observedOn, currencyCode: $currencyCode) ` + shoppingPriceSelection + ` }`

	DocumentFindShoppingItemPicture = `mutation ($shoppingItemId: String!) { FindShoppingItemPicture(shoppingItemId: $shoppingItemId) }`

	DocumentSetShoppingItemPicture = `mutation ($shoppingItemId: String!, $agentAttachmentId: String) { SetShoppingItemPicture(shoppingItemId: $shoppingItemId, agentAttachmentId: $agentAttachmentId) ` + shoppingItemSelection + ` }`

	DocumentStartShopping = `mutation ($storeName: String, $shoppingListIds: [String!]!) { StartShopping(storeName: $storeName, shoppingListIds: $shoppingListIds) ` + shoppingTripSelection + ` }`

	DocumentFinishShopping = `mutation ($shoppingTripId: String!) { FinishShopping(shoppingTripId: $shoppingTripId) ` + shoppingTripSelection + ` }`

	DocumentMatchShoppingReceipt = `mutation ($financeReceiptId: String!) { MatchShoppingReceipt(financeReceiptId: $financeReceiptId) }`

	DocumentDecideShoppingReceiptMatch = `mutation ($shoppingReceiptMatchId: String!, $isApplied: Boolean!) { DecideShoppingReceiptMatch(shoppingReceiptMatchId: $shoppingReceiptMatchId, isApplied: $isApplied) ` + shoppingReceiptMatchSelection + ` }`

	DocumentMatchShoppingReceiptLine = `mutation ($shoppingItemId: String!, $financeReceiptLineId: String!) { MatchShoppingReceiptLine(shoppingItemId: $shoppingItemId, financeReceiptLineId: $financeReceiptLineId) ` + shoppingReceiptMatchSelection + ` }`

	DocumentUndoShoppingReceipt = `mutation ($financeReceiptId: String!) { UndoShoppingReceipt(financeReceiptId: $financeReceiptId) }`

	DocumentAddShoppingItemFromReceiptLine = `mutation ($financeReceiptLineId: String!, $shoppingListId: String!) { AddShoppingItemFromReceiptLine(financeReceiptLineId: $financeReceiptLineId, shoppingListId: $shoppingListId) ` + shoppingItemSelection + ` }`
)

// ShoppingListPageView is one list with a page of its items and who is
// shopping it now.
type ShoppingListPageView struct {
	ShoppingList  *models.ShoppingList   `json:"shoppingList"`
	ShoppingItems []*models.ShoppingItem `json:"shoppingItems"`
	ShoppingTrips []*models.ShoppingTrip `json:"shoppingTrips"`

	// TotalCount is how many of the list's items are in the states asked for.
	TotalCount int `json:"totalCount"`
}

// ShoppingStoreListView is a list on the store screen: how many of its items
// are needed at the store, and whether they are in the screen's items.
type ShoppingStoreListView struct {
	ShoppingListID string `json:"shoppingListId"`
	ListName       string `json:"listName"`
	NeededCount    int    `json:"neededCount"`
	IsIncluded     bool   `json:"isIncluded"`
}

// ShoppingStoreView is the store screen: the items to get at one store across
// lists.
type ShoppingStoreView struct {
	StoreName     string                   `json:"storeName"`
	IsReminded    bool                     `json:"isReminded"`
	ShoppingItems []*models.ShoppingItem   `json:"shoppingItems"`
	ShoppingLists []*ShoppingStoreListView `json:"shoppingLists"`
	ShoppingTrips []*models.ShoppingTrip   `json:"shoppingTrips"`
}

// ShoppingItemAddedView is one item as added, and whether it was merged into
// an item already on the list.
type ShoppingItemAddedView struct {
	ShoppingItem *models.ShoppingItem `json:"shoppingItem"`
	IsMerged     bool                 `json:"isMerged"`
}

// ShoppingImportItemView is one item read out of a pasted list or a picture
// of one, and the item already on the list that it would merge into.
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

// ShoppingImportView is a list as the model read it; nothing is added.
type ShoppingImportView struct {
	ShoppingItems []*ShoppingImportItemView `json:"shoppingItems"`
}

// ShoppingReceiptMatchView is a match with its item.
type ShoppingReceiptMatchView struct {
	ShoppingReceiptMatch *models.ShoppingReceiptMatch `json:"shoppingReceiptMatch"`
	ShoppingItem         *models.ShoppingItem         `json:"shoppingItem"`
}

// ShoppingReceiptLineView is one item line of a receipt and its matches.
type ShoppingReceiptLineView struct {
	FinanceReceiptLineID   string                      `json:"financeReceiptLineId"`
	LineNumber             int                         `json:"lineNumber"`
	Description            string                      `json:"description"`
	Quantity               string                      `json:"quantity"`
	LineAmount             string                      `json:"lineAmount"`
	ShoppingReceiptMatches []*ShoppingReceiptMatchView `json:"shoppingReceiptMatches"`
}

// ShoppingReceiptView is what a receipt did to the lists: each line with its
// matches, and what is still needed at that store.
type ShoppingReceiptView struct {
	FinanceReceiptID     string                     `json:"financeReceiptId"`
	MerchantName         string                     `json:"merchantName"`
	PurchasedOn          string                     `json:"purchasedOn"`
	TotalAmount          string                     `json:"totalAmount"`
	CurrencyCode         string                     `json:"currencyCode"`
	ShoppingReceiptLines []*ShoppingReceiptLineView `json:"shoppingReceiptLines"`
	StillNeededItems     []*models.ShoppingItem     `json:"stillNeededItems"`
}

// ShoppingListQuery is a page of one list's items. No states is every state;
// a zero PageSize is the server's 200.
type ShoppingListQuery struct {
	ShoppingListID     string
	ShoppingItemStates []string
	Offset             int
	PageSize           int
}

// ShoppingStoreQuery is the store screen. No lists is every list with items
// there; IsAnyStoreIncluded nil is the server's default, which is true.
type ShoppingStoreQuery struct {
	StoreName          string
	ShoppingListIDs    []string
	IsAnyStoreIncluded *bool
}

// ShoppingImportQuery is a list to read: pasted text, pictures the caller
// uploaded as their agent's attachments, or both. A list says what each item
// would merge into.
type ShoppingImportQuery struct {
	ShoppingListID     string
	ImportText         string
	AgentAttachmentIDs []string
}

// ShoppingItemEntry is one item to add.
type ShoppingItemEntry struct {
	ItemDescription string `json:"itemDescription"`
	ItemDetails     string `json:"itemDetails,omitempty"`
	StoreName       string `json:"storeName,omitempty"`
	IsStoreBrand    bool   `json:"isStoreBrand,omitempty"`
	AisleName       string `json:"aisleName,omitempty"`
	NeededQuantity  string `json:"neededQuantity,omitempty"`
	QuantityUnit    string `json:"quantityUnit,omitempty"`
	UnitPriceAmount string `json:"unitPriceAmount,omitempty"`
	CurrencyCode    string `json:"currencyCode,omitempty"`
}

// variables is the entry as an input object, in the form a variable takes.
func (self ShoppingItemEntry) variables() map[string]any {
	entry := map[string]any{"itemDescription": self.ItemDescription}
	for name, value := range map[string]string{
		"itemDetails": self.ItemDetails, "storeName": self.StoreName, "aisleName": self.AisleName, "neededQuantity": self.NeededQuantity,
		"quantityUnit": self.QuantityUnit, "unitPriceAmount": self.UnitPriceAmount, "currencyCode": self.CurrencyCode,
	} {
		if value != "" {
			entry[name] = value
		}
	}
	if self.IsStoreBrand {
		entry["isStoreBrand"] = true
	}
	return entry
}

// ShoppingPriceEntry is a price somebody saw. Empty fields are the server's
// defaults: the item's store, today, and the item's currency.
type ShoppingPriceEntry struct {
	ShoppingItemID  string
	UnitPriceAmount string
	StoreName       string
	ObservedOn      string
	CurrencyCode    string
}

// ListShoppingLists are the lists the caller reaches, their own first, each
// with its counts.
func ListShoppingLists(ctx context.Context, executor Executor) ([]*models.ShoppingList, error) {
	var result struct {
		ListShoppingLists []*models.ShoppingList `json:"ListShoppingLists"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingLists, nil, &result); err != nil {
		return nil, err
	}
	return result.ListShoppingLists, nil
}

// GetShoppingList is one list with a page of its items.
func GetShoppingList(ctx context.Context, executor Executor, query ShoppingListQuery) (*ShoppingListPageView, error) {
	variables := map[string]any{"shoppingListId": query.ShoppingListID}
	if len(query.ShoppingItemStates) > 0 {
		variables["shoppingItemStates"] = query.ShoppingItemStates
	}
	if query.Offset > 0 {
		variables["offset"] = query.Offset
	}
	if query.PageSize > 0 {
		variables["pageSize"] = query.PageSize
	}
	var result struct {
		GetShoppingList *ShoppingListPageView `json:"GetShoppingList"`
	}
	if err := executor.Execute(ctx, DocumentGetShoppingList, variables, &result); err != nil {
		return nil, err
	}
	if result.GetShoppingList == nil {
		return nil, fmt.Errorf("%w: no shopping list %s", ErrNotFound, query.ShoppingListID)
	}
	return result.GetShoppingList, nil
}

// GetShoppingItem is one item on a list the caller reaches.
func GetShoppingItem(ctx context.Context, executor Executor, shoppingItemId string) (*models.ShoppingItem, error) {
	var result struct {
		GetShoppingItem *models.ShoppingItem `json:"GetShoppingItem"`
	}
	if err := executor.Execute(ctx, DocumentGetShoppingItem, map[string]any{"shoppingItemId": shoppingItemId}, &result); err != nil {
		return nil, err
	}
	if result.GetShoppingItem == nil {
		return nil, fmt.Errorf("%w: no shopping item %s", ErrNotFound, shoppingItemId)
	}
	return result.GetShoppingItem, nil
}

// ListShoppingListShares are the shares of one of the caller's own lists.
func ListShoppingListShares(ctx context.Context, executor Executor, shoppingListId string) ([]*models.ShoppingListShare, error) {
	var result struct {
		ListShoppingListShares []*models.ShoppingListShare `json:"ListShoppingListShares"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingListShares, map[string]any{"shoppingListId": shoppingListId}, &result); err != nil {
		return nil, err
	}
	return result.ListShoppingListShares, nil
}

// ListShoppingStores are the stores the items of the caller's lists are for,
// with how many are needed at each.
func ListShoppingStores(ctx context.Context, executor Executor) ([]*models.ShoppingStore, error) {
	var result struct {
		ListShoppingStores []*models.ShoppingStore `json:"ListShoppingStores"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingStores, nil, &result); err != nil {
		return nil, err
	}
	return result.ListShoppingStores, nil
}

// ListShoppingStoreItems is the store screen: what to get at one store across
// the lists the caller reaches.
func ListShoppingStoreItems(ctx context.Context, executor Executor, query ShoppingStoreQuery) (*ShoppingStoreView, error) {
	variables := map[string]any{"storeName": query.StoreName}
	if len(query.ShoppingListIDs) > 0 {
		variables["shoppingListIds"] = query.ShoppingListIDs
	}
	if query.IsAnyStoreIncluded != nil {
		variables["isAnyStoreIncluded"] = *query.IsAnyStoreIncluded
	}
	var result struct {
		ListShoppingStoreItems *ShoppingStoreView `json:"ListShoppingStoreItems"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingStoreItems, variables, &result); err != nil {
		return nil, err
	}
	if result.ListShoppingStoreItems == nil {
		return &ShoppingStoreView{StoreName: query.StoreName}, nil
	}
	return result.ListShoppingStoreItems, nil
}

// ListOpenShoppingTrips are the trips open now on the lists the caller
// reaches.
func ListOpenShoppingTrips(ctx context.Context, executor Executor) ([]*models.ShoppingTrip, error) {
	var result struct {
		ListOpenShoppingTrips []*models.ShoppingTrip `json:"ListOpenShoppingTrips"`
	}
	if err := executor.Execute(ctx, DocumentListOpenShoppingTrips, nil, &result); err != nil {
		return nil, err
	}
	return result.ListOpenShoppingTrips, nil
}

// ListShoppingItemPrices are an item's prices, newest first.
func ListShoppingItemPrices(ctx context.Context, executor Executor, shoppingItemId string) ([]*models.ShoppingItemPrice, error) {
	var result struct {
		ListShoppingItemPrices []*models.ShoppingItemPrice `json:"ListShoppingItemPrices"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingItemPrices, map[string]any{"shoppingItemId": shoppingItemId}, &result); err != nil {
		return nil, err
	}
	return result.ListShoppingItemPrices, nil
}

// ListShoppingReceiptMatches is what one of the caller's receipts did to
// their lists.
func ListShoppingReceiptMatches(ctx context.Context, executor Executor, financeReceiptId string) (*ShoppingReceiptView, error) {
	var result struct {
		ListShoppingReceiptMatches *ShoppingReceiptView `json:"ListShoppingReceiptMatches"`
	}
	if err := executor.Execute(ctx, DocumentListShoppingReceiptMatches, map[string]any{"financeReceiptId": financeReceiptId}, &result); err != nil {
		return nil, err
	}
	if result.ListShoppingReceiptMatches == nil {
		return nil, fmt.Errorf("%w: no receipt %s", ErrNotFound, financeReceiptId)
	}
	return result.ListShoppingReceiptMatches, nil
}

// PreviewShoppingImport reads a pasted list or pictures of one into the items
// that would be added, and what each would merge into. Nothing is added.
func PreviewShoppingImport(ctx context.Context, executor Executor, query ShoppingImportQuery) (*ShoppingImportView, error) {
	variables := map[string]any{}
	if query.ShoppingListID != "" {
		variables["shoppingListId"] = query.ShoppingListID
	}
	if query.ImportText != "" {
		variables["importText"] = query.ImportText
	}
	if len(query.AgentAttachmentIDs) > 0 {
		variables["agentAttachmentIds"] = query.AgentAttachmentIDs
	}
	var result struct {
		PreviewShoppingImport *ShoppingImportView `json:"PreviewShoppingImport"`
	}
	if err := executor.Execute(ctx, DocumentPreviewShoppingImport, variables, &result); err != nil {
		return nil, err
	}
	if result.PreviewShoppingImport == nil {
		return &ShoppingImportView{}, nil
	}
	return result.PreviewShoppingImport, nil
}

// CreateShoppingList makes a list owned by the caller.
func CreateShoppingList(ctx context.Context, executor Executor, listName string) (*models.ShoppingList, error) {
	var result struct {
		CreateShoppingList *models.ShoppingList `json:"CreateShoppingList"`
	}
	if err := executor.Execute(ctx, DocumentCreateShoppingList, map[string]any{"listName": listName}, &result); err != nil {
		return nil, err
	}
	return result.CreateShoppingList, nil
}

// RenameShoppingList gives a list another name.
func RenameShoppingList(ctx context.Context, executor Executor, shoppingListId, listName string) (*models.ShoppingList, error) {
	var result struct {
		RenameShoppingList *models.ShoppingList `json:"RenameShoppingList"`
	}
	if err := executor.Execute(ctx, DocumentRenameShoppingList, map[string]any{"shoppingListId": shoppingListId, "listName": listName}, &result); err != nil {
		return nil, err
	}
	return result.RenameShoppingList, nil
}

// DeleteShoppingList deletes one of the caller's own lists with its items and
// prices. It cannot be undone.
func DeleteShoppingList(ctx context.Context, executor Executor, shoppingListId string) error {
	return executor.Execute(ctx, DocumentDeleteShoppingList, map[string]any{"shoppingListId": shoppingListId}, nil)
}

// ShareShoppingList gives a person or a circle one of the caller's own lists.
// Give one of granteeUserId and granteeCircleId.
func ShareShoppingList(ctx context.Context, executor Executor, shoppingListId, granteeUserId, granteeCircleId string) (*models.ShoppingListShare, error) {
	variables := map[string]any{"shoppingListId": shoppingListId}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		ShareShoppingList *models.ShoppingListShare `json:"ShareShoppingList"`
	}
	if err := executor.Execute(ctx, DocumentShareShoppingList, variables, &result); err != nil {
		return nil, err
	}
	return result.ShareShoppingList, nil
}

// UnshareShoppingList takes one share of a list back.
func UnshareShoppingList(ctx context.Context, executor Executor, shoppingListShareId string) error {
	return executor.Execute(ctx, DocumentUnshareShoppingList, map[string]any{"shoppingListShareId": shoppingListShareId}, nil)
}

// SetShoppingListReminder says whether the caller is reminded of a list on
// arriving at a store.
func SetShoppingListReminder(ctx context.Context, executor Executor, shoppingListId string, isReminded bool) error {
	return executor.Execute(ctx, DocumentSetShoppingListReminder, map[string]any{"shoppingListId": shoppingListId, "isReminded": isReminded}, nil)
}

// SetShoppingStoreReminder says whether the caller is reminded on arriving at
// a store.
func SetShoppingStoreReminder(ctx context.Context, executor Executor, storeName string, isReminded bool) error {
	return executor.Execute(ctx, DocumentSetShoppingStoreReminder, map[string]any{"storeName": storeName, "isReminded": isReminded}, nil)
}

// AddShoppingItems adds items to a list. An item already on the list is
// merged into, and a new one gets a picture searched for in the background.
func AddShoppingItems(ctx context.Context, executor Executor, shoppingListId string, shoppingItems []ShoppingItemEntry) ([]*ShoppingItemAddedView, error) {
	entries := make([]any, 0, len(shoppingItems))
	for _, entry := range shoppingItems {
		entries = append(entries, entry.variables())
	}
	var result struct {
		AddShoppingItems []*ShoppingItemAddedView `json:"AddShoppingItems"`
	}
	if err := executor.Execute(ctx, DocumentAddShoppingItems, map[string]any{"shoppingListId": shoppingListId, "shoppingItems": entries}, &result); err != nil {
		return nil, err
	}
	return result.AddShoppingItems, nil
}

// UpdateShoppingItem changes the fields of an item that the change sets and
// leaves the others.
func UpdateShoppingItem(ctx context.Context, executor Executor, shoppingItemId string, change models.ShoppingItemChange) (*models.ShoppingItem, error) {
	variables := map[string]any{"shoppingItemId": shoppingItemId}
	for name, value := range map[string]*string{
		"itemDescription": change.ItemDescription, "itemDetails": change.ItemDetails, "storeName": change.StoreName,
		"aisleName": change.AisleName, "neededQuantity": change.NeededQuantity, "quantityUnit": change.QuantityUnit,
	} {
		if value != nil {
			variables[name] = *value
		}
	}
	if change.IsStoreBrand != nil {
		variables["isStoreBrand"] = *change.IsStoreBrand
	}
	var result struct {
		UpdateShoppingItem *models.ShoppingItem `json:"UpdateShoppingItem"`
	}
	if err := executor.Execute(ctx, DocumentUpdateShoppingItem, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateShoppingItem, nil
}

// RemoveShoppingItems takes items off their lists for good and says how many
// it removed.
func RemoveShoppingItems(ctx context.Context, executor Executor, shoppingItemIds []string) (int, error) {
	var result struct {
		RemoveShoppingItems int `json:"RemoveShoppingItems"`
	}
	if err := executor.Execute(ctx, DocumentRemoveShoppingItems, map[string]any{"shoppingItemIds": shoppingItemIds}, &result); err != nil {
		return 0, err
	}
	return result.RemoveShoppingItems, nil
}

// CheckShoppingItem puts an item in the cart, or back to needed.
func CheckShoppingItem(ctx context.Context, executor Executor, shoppingItemId string, isChecked bool) (*models.ShoppingItem, error) {
	var result struct {
		CheckShoppingItem *models.ShoppingItem `json:"CheckShoppingItem"`
	}
	if err := executor.Execute(ctx, DocumentCheckShoppingItem, map[string]any{"shoppingItemId": shoppingItemId, "isChecked": isChecked}, &result); err != nil {
		return nil, err
	}
	return result.CheckShoppingItem, nil
}

// SetShoppingItemNeed makes an item needed again with a count (1 when
// neededQuantity is empty), or not needed (bought, if it was in the cart).
func SetShoppingItemNeed(ctx context.Context, executor Executor, shoppingItemId string, isNeeded bool, neededQuantity string) (*models.ShoppingItem, error) {
	variables := map[string]any{"shoppingItemId": shoppingItemId, "isNeeded": isNeeded}
	if neededQuantity != "" {
		variables["neededQuantity"] = neededQuantity
	}
	var result struct {
		SetShoppingItemNeed *models.ShoppingItem `json:"SetShoppingItemNeed"`
	}
	if err := executor.Execute(ctx, DocumentSetShoppingItemNeed, variables, &result); err != nil {
		return nil, err
	}
	return result.SetShoppingItemNeed, nil
}

// SetShoppingItemOutOfStock marks an item out of stock at a store (the item's
// own when storeName is empty), or in stock again.
func SetShoppingItemOutOfStock(ctx context.Context, executor Executor, shoppingItemId string, isOutOfStock bool, storeName string) (*models.ShoppingItem, error) {
	variables := map[string]any{"shoppingItemId": shoppingItemId, "isOutOfStock": isOutOfStock}
	if storeName != "" {
		variables["storeName"] = storeName
	}
	var result struct {
		SetShoppingItemOutOfStock *models.ShoppingItem `json:"SetShoppingItemOutOfStock"`
	}
	if err := executor.Execute(ctx, DocumentSetShoppingItemOutOfStock, variables, &result); err != nil {
		return nil, err
	}
	return result.SetShoppingItemOutOfStock, nil
}

// RecordShoppingItemPrice remembers a price somebody saw for an item.
func RecordShoppingItemPrice(ctx context.Context, executor Executor, entry ShoppingPriceEntry) (*models.ShoppingItemPrice, error) {
	variables := map[string]any{"shoppingItemId": entry.ShoppingItemID, "unitPriceAmount": entry.UnitPriceAmount}
	for name, value := range map[string]string{"storeName": entry.StoreName, "observedOn": entry.ObservedOn, "currencyCode": entry.CurrencyCode} {
		if value != "" {
			variables[name] = value
		}
	}
	var result struct {
		RecordShoppingItemPrice *models.ShoppingItemPrice `json:"RecordShoppingItemPrice"`
	}
	if err := executor.Execute(ctx, DocumentRecordShoppingItemPrice, variables, &result); err != nil {
		return nil, err
	}
	return result.RecordShoppingItemPrice, nil
}

// FindShoppingItemPicture queues the search for another picture of an item.
func FindShoppingItemPicture(ctx context.Context, executor Executor, shoppingItemId string) error {
	return executor.Execute(ctx, DocumentFindShoppingItemPicture, map[string]any{"shoppingItemId": shoppingItemId}, nil)
}

// SetShoppingItemPicture gives an item a picture uploaded as the caller's
// agent attachment, or takes its picture away when the attachment is empty.
func SetShoppingItemPicture(ctx context.Context, executor Executor, shoppingItemId, agentAttachmentId string) (*models.ShoppingItem, error) {
	variables := map[string]any{"shoppingItemId": shoppingItemId}
	if agentAttachmentId != "" {
		variables["agentAttachmentId"] = agentAttachmentId
	}
	var result struct {
		SetShoppingItemPicture *models.ShoppingItem `json:"SetShoppingItemPicture"`
	}
	if err := executor.Execute(ctx, DocumentSetShoppingItemPicture, variables, &result); err != nil {
		return nil, err
	}
	return result.SetShoppingItemPicture, nil
}

// StartShopping starts the caller shopping a store (any, when storeName is
// empty) for some lists, and tells the others the lists reach.
func StartShopping(ctx context.Context, executor Executor, storeName string, shoppingListIds []string) (*models.ShoppingTrip, error) {
	variables := map[string]any{"shoppingListIds": shoppingListIds}
	if storeName != "" {
		variables["storeName"] = storeName
	}
	var result struct {
		StartShopping *models.ShoppingTrip `json:"StartShopping"`
	}
	if err := executor.Execute(ctx, DocumentStartShopping, variables, &result); err != nil {
		return nil, err
	}
	return result.StartShopping, nil
}

// FinishShopping ends a trip; what the caller put in the cart is bought.
func FinishShopping(ctx context.Context, executor Executor, shoppingTripId string) (*models.ShoppingTrip, error) {
	var result struct {
		FinishShopping *models.ShoppingTrip `json:"FinishShopping"`
	}
	if err := executor.Execute(ctx, DocumentFinishShopping, map[string]any{"shoppingTripId": shoppingTripId}, &result); err != nil {
		return nil, err
	}
	return result.FinishShopping, nil
}

// MatchShoppingReceipt queues matching one of the caller's receipts to their
// lists again.
func MatchShoppingReceipt(ctx context.Context, executor Executor, financeReceiptId string) error {
	return executor.Execute(ctx, DocumentMatchShoppingReceipt, map[string]any{"financeReceiptId": financeReceiptId}, nil)
}

// DecideShoppingReceiptMatch applies a proposed match, or dismisses one,
// undoing it when it was applied.
func DecideShoppingReceiptMatch(ctx context.Context, executor Executor, shoppingReceiptMatchId string, isApplied bool) (*models.ShoppingReceiptMatch, error) {
	var result struct {
		DecideShoppingReceiptMatch *models.ShoppingReceiptMatch `json:"DecideShoppingReceiptMatch"`
	}
	if err := executor.Execute(ctx, DocumentDecideShoppingReceiptMatch, map[string]any{"shoppingReceiptMatchId": shoppingReceiptMatchId, "isApplied": isApplied}, &result); err != nil {
		return nil, err
	}
	return result.DecideShoppingReceiptMatch, nil
}

// MatchShoppingReceiptLine ties a receipt line to an item by hand.
func MatchShoppingReceiptLine(ctx context.Context, executor Executor, shoppingItemId, financeReceiptLineId string) (*models.ShoppingReceiptMatch, error) {
	var result struct {
		MatchShoppingReceiptLine *models.ShoppingReceiptMatch `json:"MatchShoppingReceiptLine"`
	}
	if err := executor.Execute(ctx, DocumentMatchShoppingReceiptLine, map[string]any{"shoppingItemId": shoppingItemId, "financeReceiptLineId": financeReceiptLineId}, &result); err != nil {
		return nil, err
	}
	return result.MatchShoppingReceiptLine, nil
}

// UndoShoppingReceipt puts every item a receipt changed back as it was.
func UndoShoppingReceipt(ctx context.Context, executor Executor, financeReceiptId string) error {
	return executor.Execute(ctx, DocumentUndoShoppingReceipt, map[string]any{"financeReceiptId": financeReceiptId}, nil)
}

// AddShoppingItemFromReceiptLine puts a receipt line on a list as an item.
func AddShoppingItemFromReceiptLine(ctx context.Context, executor Executor, financeReceiptLineId, shoppingListId string) (*models.ShoppingItem, error) {
	var result struct {
		AddShoppingItemFromReceiptLine *models.ShoppingItem `json:"AddShoppingItemFromReceiptLine"`
	}
	if err := executor.Execute(ctx, DocumentAddShoppingItemFromReceiptLine, map[string]any{"financeReceiptLineId": financeReceiptLineId, "shoppingListId": shoppingListId}, &result); err != nil {
		return nil, err
	}
	return result.AddShoppingItemFromReceiptLine, nil
}
