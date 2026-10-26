package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// defaultPageSize is how many rows an action that lists answers when no
	// page_size is given. An item row carries the person's own words, so a
	// page of fifty is already a good part of a result.
	defaultPageSize = 50
	maximumPageSize = 200

	// maximumItemsAdded and maximumItemsNamed are the most items one call adds
	// and the most one call names to check off, change or remove.
	maximumItemsAdded = 200
	maximumItemsNamed = 100

	// pageReserveCharacters is what a page leaves free of the result budget
	// for the fields round the rows.
	pageReserveCharacters = 1500
)

// numberText is a count or a price that the model may write as 2 or as "2".
type numberText string

// UnmarshalJSON accepts a JSON string or a JSON number.
func (self *numberText) UnmarshalJSON(encoded []byte) error {
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		*self = numberText(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(encoded, &number); err != nil {
		return errors.New("a count or a price is a number such as 2, 1.5 or \"3.49\"")
	}
	*self = numberText(number.String())
	return nil
}

// itemArguments is one item of add_items.
type itemArguments struct {
	ItemDescription string     `json:"item_description"`
	ItemDetails     string     `json:"item_details"`
	NeededQuantity  numberText `json:"needed_quantity"`
	QuantityUnit    string     `json:"quantity_unit"`
	AisleName       string     `json:"aisle_name"`
	StoreName       string     `json:"store_name"`
	IsStoreBrand    bool       `json:"is_store_brand"`
	UnitPriceAmount numberText `json:"unit_price_amount"`
	CurrencyCode    string     `json:"currency_code"`
}

type arguments struct {
	ListID             string          `json:"list_id"`
	ListIDs            []string        `json:"list_ids"`
	ItemID             string          `json:"item_id"`
	ItemIDs            []string        `json:"item_ids"`
	ShoppingItemStates []string        `json:"shopping_item_states"`
	Offset             int             `json:"offset"`
	PageSize           int             `json:"page_size"`
	ListName           string          `json:"list_name"`
	With               string          `json:"with"`
	ShareID            string          `json:"share_id"`
	IsReminded         *bool           `json:"is_reminded"`
	StoreName          *string         `json:"store_name"`
	IsAnyStoreIncluded *bool           `json:"is_any_store_included"`
	Items              []itemArguments `json:"items"`
	ItemDescription    *string         `json:"item_description"`
	ItemDetails        *string         `json:"item_details"`
	IsStoreBrand       *bool           `json:"is_store_brand"`
	AisleName          *string         `json:"aisle_name"`
	NeededQuantity     *numberText     `json:"needed_quantity"`
	QuantityUnit       *string         `json:"quantity_unit"`
	IsOutOfStock       *bool           `json:"is_out_of_stock"`
	UnitPriceAmount    numberText      `json:"unit_price_amount"`
	ObservedOn         string          `json:"observed_on"`
	CurrencyCode       string          `json:"currency_code"`
	TripID             string          `json:"trip_id"`
	ReceiptID          string          `json:"receipt_id"`
	ReceiptLineID      string          `json:"receipt_line_id"`
	MatchID            string          `json:"match_id"`
	IsApplied          *bool           `json:"is_applied"`
	ImportText         string          `json:"import_text"`
	AttachmentIDs      []string        `json:"attachment_ids"`
}

// storeNameText is the store given, trimmed, or empty when none was.
func (self *arguments) storeNameText() string {
	if self.StoreName == nil {
		return ""
	}
	return strings.TrimSpace(*self.StoreName)
}

// scope is what every action needs: the run's operations and the person's zone.
type scope struct {
	run        tools.Run
	operations tools.Operations
	location   *time.Location
}

// runWith decodes a call's arguments and gives the action its scope.
func runWith(action func(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		asked, err := tools.DecodeArguments[arguments](call)
		if err != nil {
			return nil, err
		}
		run, err := tools.RunFrom(ctx)
		if err != nil {
			return nil, err
		}
		return action(ctx, &scope{run: run, operations: run.Operations(), location: tools.Location(run.Owner())}, &asked)
	}
}

// dataResult is an answer that carries the person's words and the words of
// the people a list is shared with, which never instruct the model.
func dataResult(answer map[string]any) (*tools.Result, error) {
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

// noteResult is dataResult with a line for the drawer saying what was done.
func noteResult(note string, answer map[string]any) (*tools.Result, error) {
	result, err := dataResult(answer)
	if err != nil {
		return nil, err
	}
	result.Note = note
	return result, nil
}

// dayText is a moment as the day it falls on in the person's zone.
func (self *scope) dayText(moment *time.Time) string {
	if moment == nil {
		return ""
	}
	return moment.In(self.location).Format(time.DateOnly)
}

// listRow is a list as the model is told of it.
func listRow(list *models.ShoppingList) map[string]any {
	row := map[string]any{
		"list_id": list.ID, "list_name": list.ListName, "needed_count": list.NeededCount, "in_cart_count": list.InCartCount,
		"not_needed_count": list.NotNeededCount, "is_owner": list.IsOwner, "is_shared": list.IsShared, "is_reminded": list.IsReminded,
	}
	if !list.IsOwner {
		row["owner_name"] = list.OwnerName
		if list.OwnerName == "" {
			row["owner_name"] = list.OwnerUsername
		}
	}
	return row
}

// itemRow is an item as the model is told of it. Fields that are empty are
// left out, so that a row is what is known about the item and nothing more.
func (self *scope) itemRow(item *models.ShoppingItem, isListShown bool) map[string]any {
	row := map[string]any{
		"item_id": item.ID, "item_description": item.ItemDescription, "needed_quantity": item.NeededQuantity,
		"shopping_item_state": string(item.ShoppingItemState), "list_id": item.ShoppingListID,
	}
	for name, value := range map[string]string{
		"item_details": item.ItemDetails, "quantity_unit": item.QuantityUnit, "aisle_name": item.AisleName, "store_name": item.StoreName,
		"unit_price_amount": item.UnitPriceAmount, "carted_by_name": item.CartedByName,
	} {
		if value != "" {
			row[name] = value
		}
	}
	if item.UnitPriceAmount != "" && item.CurrencyCode != "" {
		row["currency_code"] = item.CurrencyCode
	}
	if isListShown {
		row["list_name"] = item.ListName
	}
	if item.IsStoreBrand {
		row["is_store_brand"] = true
	}
	if item.OutOfStockAt != nil {
		row["is_out_of_stock"] = true
		if item.OutOfStockStoreName != "" {
			row["out_of_stock_store_name"] = item.OutOfStockStoreName
		}
		if item.OutOfStockByName != "" {
			row["out_of_stock_by_name"] = item.OutOfStockByName
		}
	}
	if day := self.dayText(item.LastBoughtAt); day != "" {
		row["last_bought_on"] = day
	}
	if item.PictureFileID != "" {
		row["has_picture"] = true
	}
	return row
}

func (self *scope) itemRows(items []*models.ShoppingItem, isListShown bool) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item != nil {
			rows = append(rows, self.itemRow(item, isListShown))
		}
	}
	return rows
}

// tripRow is somebody shopping a store, for the others to know.
func (self *scope) tripRow(trip *models.ShoppingTrip) map[string]any {
	row := map[string]any{
		"trip_id": trip.ID, "shopper_name": trip.ShopperName, "started_at": trip.StartedAt.In(self.location).Format("2006-01-02 15:04"),
		"list_ids": trip.ShoppingListIDs,
	}
	if trip.ShopperName == "" {
		row["shopper_name"] = trip.ShopperUsername
	}
	if trip.StoreName != "" {
		row["store_name"] = trip.StoreName
	}
	return row
}

func (self *scope) tripRows(trips []*models.ShoppingTrip) []map[string]any {
	rows := make([]map[string]any, 0, len(trips))
	for _, trip := range trips {
		if trip != nil {
			rows = append(rows, self.tripRow(trip))
		}
	}
	return rows
}

// pageBounds reads offset and page_size.
func pageBounds(asked *arguments) (offset, pageSize int, err error) {
	pageSize = asked.PageSize
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if asked.Offset < 0 || pageSize < 1 || pageSize > maximumPageSize {
		return 0, 0, fmt.Errorf("offset is 0 or more and page_size is 1 to %d", maximumPageSize)
	}
	return asked.Offset, pageSize, nil
}

// pageOf is the rows of one page of a list the tool holds whole: from offset,
// at most pageSize, and fewer when they would not fit the result the run keeps.
// The answer says whether there is more and where to read on, so that nothing
// is ever cut without the model knowing.
func (self *scope) pageOf(rows []map[string]any, asked *arguments, answer map[string]any) ([]map[string]any, error) {
	offset, pageSize, err := pageBounds(asked)
	if err != nil {
		return nil, err
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	end := min(offset+pageSize, len(rows))
	budget := tools.ResultCharactersOf(self.run) - pageReserveCharacters
	used, fitted := 0, offset
	for fitted < end {
		encoded, err := json.Marshal(rows[fitted])
		if err != nil {
			return nil, err
		}
		if used += len(encoded) + 1; used > budget && fitted > offset {
			break
		}
		fitted++
	}
	pageNote(answer, offset, fitted-offset, len(rows))
	return rows[offset:fitted], nil
}

// pageNote says how many rows there are in all, and whether and where there
// is more.
func pageNote(answer map[string]any, offset, shown, totalCount int) {
	answer["total_count"] = totalCount
	answer["has_more"] = offset+shown < totalCount
	if offset+shown < totalCount {
		answer["next_offset"] = offset + shown
	}
}

// itemIDsOf is the items a call names, by item_id or item_ids, once each.
func itemIDsOf(asked *arguments, action string) ([]string, error) {
	seen := map[string]bool{}
	itemIds := []string{}
	for _, itemId := range append([]string{asked.ItemID}, asked.ItemIDs...) {
		if itemId = strings.TrimSpace(itemId); itemId != "" && !seen[itemId] {
			seen[itemId] = true
			itemIds = append(itemIds, itemId)
		}
	}
	if len(itemIds) == 0 {
		return nil, fmt.Errorf("%s needs item_id or item_ids; list and store give them", action)
	}
	if len(itemIds) > maximumItemsNamed {
		return nil, fmt.Errorf("%s takes at most %d items at a time", action, maximumItemsNamed)
	}
	return itemIds, nil
}

func requiredText(value, name, action, where string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s needs %s; %s", action, name, where)
	}
	return value, nil
}

func listIDOf(asked *arguments, action string) (string, error) {
	return requiredText(asked.ListID, "list_id", action, "lists gives them")
}

func itemIDOf(asked *arguments, action string) (string, error) {
	return requiredText(asked.ItemID, "item_id", action, "list and store give them")
}

func receiptIDOf(asked *arguments, action string) (string, error) {
	return requiredText(asked.ReceiptID, "receipt_id", action, "the person's Finance receipts have them")
}

func runLists(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	lists, err := client.ListShoppingLists(ctx, here.operations)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(lists))
	for _, list := range lists {
		rows = append(rows, listRow(list))
	}
	answer := map[string]any{}
	page, err := here.pageOf(rows, asked, answer)
	if err != nil {
		return nil, err
	}
	answer["lists"] = page
	if len(rows) == 0 {
		answer["note"] = "the person has no shopping lists yet; create_list makes one"
	}
	return dataResult(answer)
}

func runList(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "list")
	if err != nil {
		return nil, err
	}
	offset, pageSize, err := pageBounds(asked)
	if err != nil {
		return nil, err
	}
	page, err := client.GetShoppingList(ctx, here.operations, client.ShoppingListQuery{
		ShoppingListID: listId, ShoppingItemStates: asked.ShoppingItemStates, Offset: offset, PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}
	rows := here.itemRows(page.ShoppingItems, false)
	answer := map[string]any{"list": listRow(page.ShoppingList), "items": rows, "shopping_trips": here.tripRows(page.ShoppingTrips)}
	pageNote(answer, offset, len(rows), page.TotalCount)
	if page.TotalCount == 0 {
		answer["note"] = "no items in these states"
	}
	return dataResult(answer)
}

func runItem(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "item")
	if err != nil {
		return nil, err
	}
	item, err := client.GetShoppingItem(ctx, here.operations, itemId)
	if err != nil {
		return nil, err
	}
	row := here.itemRow(item, true)
	if item.PicturePageURL != "" {
		row["picture_page_url"] = item.PicturePageURL
	}
	return dataResult(map[string]any{"item": row})
}

func runShares(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "shares")
	if err != nil {
		return nil, err
	}
	shares, err := client.ListShoppingListShares(ctx, here.operations, listId)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(shares))
	for _, share := range shares {
		row := map[string]any{"share_id": share.ID, "since": share.CreatedAt.In(here.location).Format(time.DateOnly)}
		if share.GranteeCircleID != "" {
			row["circle_name"] = share.GranteeCircleName
		} else {
			row["username"], row["person_name"] = share.GranteeUsername, share.GranteeName
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"shares": rows}
	if len(rows) == 0 {
		answer["note"] = "the list is not shared with anybody"
	}
	return dataResult(answer)
}

func runStores(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	stores, err := client.ListShoppingStores(ctx, here.operations)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(stores))
	for _, store := range stores {
		rows = append(rows, map[string]any{"store_name": store.StoreName, "needed_count": store.NeededCount, "is_reminded": store.IsReminded})
	}
	answer := map[string]any{"stores": rows}
	if len(rows) == 0 {
		answer["note"] = "none of the person's items is for a particular store yet"
	}
	return dataResult(answer)
}

func runStore(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	storeName, err := requiredText(asked.storeNameText(), "store_name", "store", "stores gives the ones the person's items are for")
	if err != nil {
		return nil, err
	}
	view, err := client.ListShoppingStoreItems(ctx, here.operations, client.ShoppingStoreQuery{
		StoreName: storeName, ShoppingListIDs: asked.ListIDs, IsAnyStoreIncluded: asked.IsAnyStoreIncluded,
	})
	if err != nil {
		return nil, err
	}
	lists := make([]map[string]any, 0, len(view.ShoppingLists))
	for _, list := range view.ShoppingLists {
		lists = append(lists, map[string]any{"list_id": list.ShoppingListID, "list_name": list.ListName, "needed_count": list.NeededCount, "is_included": list.IsIncluded})
	}
	answer := map[string]any{
		"store_name": view.StoreName, "is_reminded": view.IsReminded, "lists": lists, "shopping_trips": here.tripRows(view.ShoppingTrips),
	}
	page, err := here.pageOf(here.itemRows(view.ShoppingItems, len(view.ShoppingLists) > 1), asked, answer)
	if err != nil {
		return nil, err
	}
	answer["items"] = page
	if len(view.ShoppingItems) == 0 {
		answer["note"] = "nothing is needed at this store"
	}
	return dataResult(answer)
}

func runTrips(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	trips, err := client.ListOpenShoppingTrips(ctx, here.operations)
	if err != nil {
		return nil, err
	}
	answer := map[string]any{"shopping_trips": here.tripRows(trips)}
	if len(trips) == 0 {
		answer["note"] = "nobody is shopping now"
	}
	return dataResult(answer)
}

func runPrices(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "prices")
	if err != nil {
		return nil, err
	}
	prices, err := client.ListShoppingItemPrices(ctx, here.operations, itemId)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(prices))
	for _, price := range prices {
		row := map[string]any{
			"observed_on": price.ObservedOn, "unit_price_amount": price.UnitPriceAmount, "currency_code": price.CurrencyCode,
			"price_source": string(price.PriceSource),
		}
		for name, value := range map[string]string{"store_name": price.StoreName, "quantity": price.Quantity, "quantity_unit": price.QuantityUnit} {
			if value != "" {
				row[name] = value
			}
		}
		rows = append(rows, row)
	}
	answer := map[string]any{}
	page, err := here.pageOf(rows, asked, answer)
	if err != nil {
		return nil, err
	}
	answer["prices"] = page
	if len(rows) == 0 {
		answer["note"] = "no prices are remembered for this item"
	}
	return dataResult(answer)
}

func runReceipt(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	receiptId, err := receiptIDOf(asked, "receipt")
	if err != nil {
		return nil, err
	}
	offset, pageSize, err := pageBounds(asked)
	if err != nil {
		return nil, err
	}
	view, err := client.ListShoppingReceiptMatches(ctx, here.operations, receiptId)
	if err != nil {
		return nil, err
	}
	lines := make([]map[string]any, 0, len(view.ShoppingReceiptLines))
	for _, line := range view.ShoppingReceiptLines {
		matches := make([]map[string]any, 0, len(line.ShoppingReceiptMatches))
		for _, match := range line.ShoppingReceiptMatches {
			if match.ShoppingReceiptMatch == nil || match.ShoppingItem == nil {
				continue
			}
			matches = append(matches, map[string]any{
				"match_id": match.ShoppingReceiptMatch.ID, "receipt_match_state": string(match.ShoppingReceiptMatch.ReceiptMatchState),
				"item_id": match.ShoppingItem.ID, "item_description": match.ShoppingItem.ItemDescription, "list_name": match.ShoppingItem.ListName,
				"matched_quantity": match.ShoppingReceiptMatch.MatchedQuantity,
			})
		}
		lines = append(lines, map[string]any{
			"receipt_line_id": line.FinanceReceiptLineID, "line_number": line.LineNumber, "description": line.Description,
			"quantity": line.Quantity, "line_amount": line.LineAmount, "matches": matches,
		})
	}
	stillNeeded := here.itemRows(view.StillNeededItems, true)

	// Lines and the items still needed are paged together by the same offset,
	// so that reading on never skips a row of either.
	answer := map[string]any{
		"finance_receipt_id": view.FinanceReceiptID, "merchant_name": view.MerchantName, "purchased_on": view.PurchasedOn,
		"total_amount": view.TotalAmount, "currency_code": view.CurrencyCode,
	}
	linePage, stillNeededPage := slicePage(lines, offset, pageSize), slicePage(stillNeeded, offset, pageSize)
	answer["receipt_lines"], answer["still_needed_items"] = linePage, stillNeededPage
	shown := max(len(linePage), len(stillNeededPage))
	pageNote(answer, offset, shown, max(len(lines), len(stillNeeded)))
	if len(lines) == 0 {
		answer["note"] = "the receipt has no item lines, or it has not been read yet"
	}
	return dataResult(answer)
}

// slicePage is the rows from offset, at most pageSize of them.
func slicePage(rows []map[string]any, offset, pageSize int) []map[string]any {
	if offset >= len(rows) {
		return []map[string]any{}
	}
	return rows[offset:min(offset+pageSize, len(rows))]
}

func runImportPreview(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	if strings.TrimSpace(asked.ImportText) == "" && len(asked.AttachmentIDs) == 0 {
		return nil, errors.New("import_preview needs import_text or attachment_ids; you can also read a list yourself and call add_items")
	}
	preview, err := client.PreviewShoppingImport(ctx, here.operations, client.ShoppingImportQuery{
		ShoppingListID: strings.TrimSpace(asked.ListID), ImportText: asked.ImportText, AgentAttachmentIDs: asked.AttachmentIDs,
	})
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(preview.ShoppingItems))
	for _, item := range preview.ShoppingItems {
		row := map[string]any{"item_description": item.ItemDescription, "needed_quantity": item.NeededQuantity}
		for name, value := range map[string]string{
			"item_details": item.ItemDetails, "quantity_unit": item.QuantityUnit, "aisle_name": item.AisleName, "store_name": item.StoreName,
		} {
			if value != "" {
				row[name] = value
			}
		}
		if item.IsStoreBrand {
			row["is_store_brand"] = true
		}
		if item.MergedShoppingItemID != "" {
			row["merges_into_item_id"], row["merges_into_shopping_item_state"] = item.MergedShoppingItemID, item.MergedShoppingItemState
			row["merges_into_needed_quantity"] = item.MergedNeededQuantity
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"items": rows, "note": "nothing was added; add_items adds them"}
	return dataResult(answer)
}

func runCreateList(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listName, err := requiredText(asked.ListName, "list_name", "create_list", "name the list, such as Groceries")
	if err != nil {
		return nil, err
	}
	list, err := client.CreateShoppingList(ctx, here.operations, listName)
	if err != nil {
		return nil, err
	}
	return noteResult("made the shopping list "+list.ListName, map[string]any{"list": listRow(list)})
}

func runRenameList(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "rename_list")
	if err != nil {
		return nil, err
	}
	listName, err := requiredText(asked.ListName, "list_name", "rename_list", "give the new name")
	if err != nil {
		return nil, err
	}
	list, err := client.RenameShoppingList(ctx, here.operations, listId, listName)
	if err != nil {
		return nil, err
	}
	return noteResult("renamed the shopping list to "+list.ListName, map[string]any{"list": listRow(list)})
}

func runDeleteList(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "delete_list")
	if err != nil {
		return nil, err
	}
	if err := client.DeleteShoppingList(ctx, here.operations, listId); err != nil {
		return nil, err
	}
	return tools.TextResult("deleted the list %s with its items and prices", listId), nil
}

func runShare(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "share")
	if err != nil {
		return nil, err
	}
	grantee, err := tools.ResolveGrantee(ctx, here.operations, asked.With)
	if err != nil {
		return nil, err
	}
	share, err := client.ShareShoppingList(ctx, here.operations, listId, grantee.UserID, grantee.CircleID)
	if err != nil {
		return nil, err
	}
	return noteResult("shared a shopping list with "+grantee.Description, map[string]any{
		"share_id": share.ID, "list_name": share.ListName, "with": grantee.Description,
		"note": "they can add, change and check off items, start shopping and upload receipts; only the owner can share it further or delete it",
	})
}

func runUnshare(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	shareId, err := requiredText(asked.ShareID, "share_id", "unshare", "shares gives them")
	if err != nil {
		return nil, err
	}
	if err := client.UnshareShoppingList(ctx, here.operations, shareId); err != nil {
		return nil, err
	}
	return tools.TextResult("stopped sharing the list; share %s is gone", shareId), nil
}

// isRemindedOf is the reminder switch a call asks for: on unless it says off.
func isRemindedOf(asked *arguments) bool {
	return asked.IsReminded == nil || *asked.IsReminded
}

func runRemind(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "remind")
	if err != nil {
		return nil, err
	}
	isReminded := isRemindedOf(asked)
	if err := client.SetShoppingListReminder(ctx, here.operations, listId, isReminded); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"list_id": listId, "is_reminded": isReminded})
}

func runStoreRemind(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	storeName, err := requiredText(asked.storeNameText(), "store_name", "store_remind", "stores gives the ones the person's items are for")
	if err != nil {
		return nil, err
	}
	isReminded := isRemindedOf(asked)
	if err := client.SetShoppingStoreReminder(ctx, here.operations, storeName, isReminded); err != nil {
		return nil, err
	}
	return dataResult(map[string]any{"store_name": storeName, "is_reminded": isReminded})
}

func runAddItems(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listId, err := listIDOf(asked, "add_items")
	if err != nil {
		return nil, err
	}
	if len(asked.Items) == 0 || len(asked.Items) > maximumItemsAdded {
		return nil, fmt.Errorf("add_items takes 1 to %d items; add the rest in another call", maximumItemsAdded)
	}
	entries := make([]client.ShoppingItemEntry, 0, len(asked.Items))
	for index, item := range asked.Items {
		if strings.TrimSpace(item.ItemDescription) == "" {
			return nil, fmt.Errorf("item %d of items has no item_description", index+1)
		}
		entries = append(entries, client.ShoppingItemEntry{
			ItemDescription: strings.TrimSpace(item.ItemDescription), ItemDetails: strings.TrimSpace(item.ItemDetails), StoreName: strings.TrimSpace(item.StoreName),
			IsStoreBrand: item.IsStoreBrand, AisleName: strings.TrimSpace(item.AisleName), NeededQuantity: string(item.NeededQuantity),
			QuantityUnit: strings.TrimSpace(item.QuantityUnit), UnitPriceAmount: string(item.UnitPriceAmount), CurrencyCode: strings.TrimSpace(item.CurrencyCode),
		})
	}
	added, err := client.AddShoppingItems(ctx, here.operations, listId, entries)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(added))
	mergedCount := 0
	for _, each := range added {
		if each.ShoppingItem == nil {
			continue
		}
		row := here.itemRow(each.ShoppingItem, false)
		if each.IsMerged {
			row["is_merged"] = true
			mergedCount++
		}
		rows = append(rows, row)
	}
	return noteResult(fmt.Sprintf("added %d items to a shopping list", len(rows)), map[string]any{
		"added_count": len(rows), "merged_count": mergedCount, "items": rows,
		"note": "an item that is_merged was already on the list and its count and details were combined; pictures are searched for in the background",
	})
}

func runUpdateItem(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "update_item")
	if err != nil {
		return nil, err
	}
	change := models.ShoppingItemChange{
		ItemDescription: trimmed(asked.ItemDescription), ItemDetails: trimmed(asked.ItemDetails), StoreName: trimmed(asked.StoreName),
		IsStoreBrand: asked.IsStoreBrand, AisleName: trimmed(asked.AisleName), QuantityUnit: trimmed(asked.QuantityUnit),
	}
	if asked.NeededQuantity != nil {
		quantity := string(*asked.NeededQuantity)
		change.NeededQuantity = &quantity
	}
	if change == (models.ShoppingItemChange{}) {
		return nil, errors.New("update_item needs at least one of item_description, item_details, store_name, is_store_brand, aisle_name, needed_quantity and quantity_unit")
	}
	item, err := client.UpdateShoppingItem(ctx, here.operations, itemId, change)
	if err != nil {
		return nil, err
	}
	return dataResult(map[string]any{"item": here.itemRow(item, true)})
}

// trimmed is a text argument with its spaces taken off, or nil when it was
// not given.
func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	return &text
}

func runRemoveItems(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemIds, err := itemIDsOf(asked, "remove_items")
	if err != nil {
		return nil, err
	}
	removedCount, err := client.RemoveShoppingItems(ctx, here.operations, itemIds)
	if err != nil {
		return nil, err
	}
	return tools.TextResult("removed %d of the %d items named from their lists", removedCount, len(itemIds)), nil
}

// changeEach does one change to every item named and answers the items as they
// are after it. It stops at the first the server refuses and says which ones
// were already changed, so that the model does not repeat them.
func changeEach(ctx context.Context, here *scope, asked *arguments, action string, change func(itemId string) (*models.ShoppingItem, error)) (*tools.Result, error) {
	itemIds, err := itemIDsOf(asked, action)
	if err != nil {
		return nil, err
	}
	items := make([]*models.ShoppingItem, 0, len(itemIds))
	for _, itemId := range itemIds {
		item, err := change(itemId)
		if err != nil {
			if len(items) > 0 {
				return nil, fmt.Errorf("%w (already done: %s)", err, strings.Join(itemIdsOfItems(items), ", "))
			}
			return nil, err
		}
		items = append(items, item)
	}
	return dataResult(map[string]any{"items": here.itemRows(items, true)})
}

func itemIdsOfItems(items []*models.ShoppingItem) []string {
	itemIds := make([]string, 0, len(items))
	for _, item := range items {
		itemIds = append(itemIds, item.ID)
	}
	return itemIds
}

func runCheck(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	return changeEach(ctx, here, asked, "check", func(itemId string) (*models.ShoppingItem, error) {
		return client.CheckShoppingItem(ctx, here.operations, itemId, true)
	})
}

func runUncheck(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	return changeEach(ctx, here, asked, "uncheck", func(itemId string) (*models.ShoppingItem, error) {
		return client.CheckShoppingItem(ctx, here.operations, itemId, false)
	})
}

func runNeed(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	neededQuantity := ""
	if asked.NeededQuantity != nil {
		neededQuantity = string(*asked.NeededQuantity)
	}
	return changeEach(ctx, here, asked, "need", func(itemId string) (*models.ShoppingItem, error) {
		return client.SetShoppingItemNeed(ctx, here.operations, itemId, true, neededQuantity)
	})
}

func runNotNeeded(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	return changeEach(ctx, here, asked, "not_needed", func(itemId string) (*models.ShoppingItem, error) {
		return client.SetShoppingItemNeed(ctx, here.operations, itemId, false, "")
	})
}

func runOutOfStock(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "out_of_stock")
	if err != nil {
		return nil, err
	}
	isOutOfStock := asked.IsOutOfStock == nil || *asked.IsOutOfStock
	item, err := client.SetShoppingItemOutOfStock(ctx, here.operations, itemId, isOutOfStock, asked.storeNameText())
	if err != nil {
		return nil, err
	}
	return dataResult(map[string]any{"item": here.itemRow(item, true)})
}

func runPrice(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "price")
	if err != nil {
		return nil, err
	}
	unitPriceAmount, err := requiredText(string(asked.UnitPriceAmount), "unit_price_amount", "price", "give the price of one, such as 3.49")
	if err != nil {
		return nil, err
	}
	price, err := client.RecordShoppingItemPrice(ctx, here.operations, client.ShoppingPriceEntry{
		ShoppingItemID: itemId, UnitPriceAmount: unitPriceAmount, StoreName: asked.storeNameText(),
		ObservedOn: strings.TrimSpace(asked.ObservedOn), CurrencyCode: strings.TrimSpace(asked.CurrencyCode),
	})
	if err != nil {
		return nil, err
	}
	return dataResult(map[string]any{
		"unit_price_amount": price.UnitPriceAmount, "currency_code": price.CurrencyCode, "store_name": price.StoreName, "observed_on": price.ObservedOn,
	})
}

func runFindPicture(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "find_picture")
	if err != nil {
		return nil, err
	}
	if err := client.FindShoppingItemPicture(ctx, here.operations, itemId); err != nil {
		return nil, err
	}
	return tools.TextResult("searching for another picture of item %s; it arrives in a minute or so", itemId), nil
}

func runStartShopping(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	listIds := make([]string, 0, len(asked.ListIDs))
	for _, listId := range asked.ListIDs {
		if listId = strings.TrimSpace(listId); listId != "" {
			listIds = append(listIds, listId)
		}
	}
	if len(listIds) == 0 {
		return nil, errors.New("start_shopping needs list_ids; lists gives them")
	}
	trip, err := client.StartShopping(ctx, here.operations, asked.storeNameText(), listIds)
	if err != nil {
		return nil, err
	}
	return noteResult("started shopping", map[string]any{
		"shopping_trip": here.tripRow(trip), "note": "the others the lists reach were told; finish_shopping with this trip_id buys what is in the cart",
	})
}

func runFinishShopping(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	tripId, err := requiredText(asked.TripID, "trip_id", "finish_shopping", "start_shopping and trips give them")
	if err != nil {
		return nil, err
	}
	if _, err := client.FinishShopping(ctx, here.operations, tripId); err != nil {
		return nil, err
	}
	return tools.TextResult("finished shopping; what was in the cart is bought"), nil
}

func runMatchReceipt(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	receiptId, err := receiptIDOf(asked, "match_receipt")
	if err != nil {
		return nil, err
	}
	if err := client.MatchShoppingReceipt(ctx, here.operations, receiptId); err != nil {
		return nil, err
	}
	return tools.TextResult("queued; receipt shows what it did to the lists in a minute or so"), nil
}

func runDecideMatch(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	matchId, err := requiredText(asked.MatchID, "match_id", "decide_match", "receipt gives them")
	if err != nil {
		return nil, err
	}
	if asked.IsApplied == nil {
		return nil, errors.New("decide_match needs is_applied: true applies the match, false dismisses it")
	}
	match, err := client.DecideShoppingReceiptMatch(ctx, here.operations, matchId, *asked.IsApplied)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"match_id": match.ID, "receipt_match_state": string(match.ReceiptMatchState)})
}

func runMatchLine(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	itemId, err := itemIDOf(asked, "match_line")
	if err != nil {
		return nil, err
	}
	receiptLineId, err := requiredText(asked.ReceiptLineID, "receipt_line_id", "match_line", "receipt gives them")
	if err != nil {
		return nil, err
	}
	match, err := client.MatchShoppingReceiptLine(ctx, here.operations, itemId, receiptLineId)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"match_id": match.ID, "receipt_match_state": string(match.ReceiptMatchState)})
}

func runUndoReceipt(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	receiptId, err := receiptIDOf(asked, "undo_receipt")
	if err != nil {
		return nil, err
	}
	if err := client.UndoShoppingReceipt(ctx, here.operations, receiptId); err != nil {
		return nil, err
	}
	return tools.TextResult("put everything the receipt changed back as it was"), nil
}

func runAddFromReceipt(ctx context.Context, here *scope, asked *arguments) (*tools.Result, error) {
	receiptLineId, err := requiredText(asked.ReceiptLineID, "receipt_line_id", "add_from_receipt", "receipt gives them")
	if err != nil {
		return nil, err
	}
	listId, err := listIDOf(asked, "add_from_receipt")
	if err != nil {
		return nil, err
	}
	item, err := client.AddShoppingItemFromReceiptLine(ctx, here.operations, receiptLineId, listId)
	if err != nil {
		return nil, err
	}
	return dataResult(map[string]any{"item": here.itemRow(item, true)})
}

// The cards of the actions that ask. A card says what will happen in the
// words the person would use, so each looks up the name of what it is about
// and falls back to a sentence without it.

type listCardArguments struct {
	ListID string `json:"list_id"`
}

func deleteListCard(listCardArguments) string {
	return "Delete a shopping list with its items and prices, for everybody it is shared with"
}

func deleteListCardIn(ctx context.Context, raw json.RawMessage) string {
	var asked listCardArguments
	run, err := tools.RunFrom(ctx)
	if err != nil || json.Unmarshal(raw, &asked) != nil || strings.TrimSpace(asked.ListID) == "" {
		return ""
	}
	list := listNamed(ctx, run, asked.ListID)
	if list == nil {
		return ""
	}
	return fmt.Sprintf("Delete the shopping list %s with its %d items and their prices, for everybody it is shared with", tools.Named(list.ListName, "a list"), list.NeededCount+list.InCartCount+list.NotNeededCount)
}

// listNamed is the list with this id among the ones the person reaches, or nil.
func listNamed(ctx context.Context, run tools.Run, listId string) *models.ShoppingList {
	lists, err := client.ListShoppingLists(ctx, run.Operations())
	if err != nil {
		return nil
	}
	for _, list := range lists {
		if list.ID == strings.TrimSpace(listId) {
			return list
		}
	}
	return nil
}

type shareCardArguments struct {
	ListID string `json:"list_id"`
	With   string `json:"with"`
}

func shareCard(asked shareCardArguments) string {
	return "Share a shopping list with " + tools.Named(asked.With, "someone") + ": they can add, change and check off its items"
}

func shareCardIn(ctx context.Context, raw json.RawMessage) string {
	var asked shareCardArguments
	run, err := tools.RunFrom(ctx)
	if err != nil || json.Unmarshal(raw, &asked) != nil || strings.TrimSpace(asked.With) == "" {
		return ""
	}
	title := "a shopping list"
	if list := listNamed(ctx, run, asked.ListID); list != nil {
		title = "the shopping list " + tools.Named(list.ListName, "")
	}
	return "Share " + title + " with " + tools.GranteeWords(ctx, asked.With) + ": they can add, change and check off its items, but not share it further or delete it"
}

type removeItemsCardArguments struct {
	ItemID  string   `json:"item_id"`
	ItemIDs []string `json:"item_ids"`
}

func (self removeItemsCardArguments) itemIds() []string {
	return append([]string{self.ItemID}, self.ItemIDs...)
}

func removeItemsCard(asked removeItemsCardArguments) string {
	itemCount := 0
	for _, itemId := range asked.itemIds() {
		if strings.TrimSpace(itemId) != "" {
			itemCount++
		}
	}
	switch itemCount {
	case 0:
		return "Remove items from their shopping lists for good"
	case 1:
		return "Remove an item from its shopping list for good"
	}
	return fmt.Sprintf("Remove %d items from their shopping lists for good", itemCount)
}

func removeItemsCardIn(ctx context.Context, raw json.RawMessage) string {
	var asked removeItemsCardArguments
	run, err := tools.RunFrom(ctx)
	if err != nil || json.Unmarshal(raw, &asked) != nil {
		return ""
	}
	names := []string{}
	itemCount := 0
	for _, itemId := range asked.itemIds() {
		if itemId = strings.TrimSpace(itemId); itemId == "" {
			continue
		}
		itemCount++
		if len(names) < 3 {
			if item, err := client.GetShoppingItem(ctx, run.Operations(), itemId); err == nil {
				names = append(names, item.ItemDescription)
			}
		}
	}
	if len(names) == 0 || itemCount == 0 {
		return ""
	}
	return fmt.Sprintf("Remove %s from their shopping lists for good", tools.Some(quotedAll(names), 3)+moreItems(itemCount, len(names)))
}

func quotedAll(names []string) []string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, tools.Named(name, ""))
	}
	return quoted
}

// moreItems is the count of items a card does not name.
func moreItems(itemCount, namedCount int) string {
	if itemCount <= namedCount {
		return ""
	}
	return fmt.Sprintf(" and %d more", itemCount-namedCount)
}
