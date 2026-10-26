package db

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ShoppingReceiptOperation ties the lines of a Finance receipt to the items
// of the shopping lists its owner reaches. A receipt is one system: however
// it arrived, its lines check items off and remember their price at the
// store, and what was applied can be undone.
//
// The receipt is the person's own (its agent is theirs); the items are on
// lists they reach. A match is never written to an item they do not reach.
type ShoppingReceiptOperation interface {
	// ListShoppingReceiptCandidates are the items a receipt from this
	// merchant could have bought: needed or in the cart, on the lists the
	// person reaches, for a store whose name the merchant's contains (or the
	// other way round), or for any store.
	ListShoppingReceiptCandidates(userId, merchantName string) ([]*models.ShoppingItem, error)

	// ApplyShoppingReceiptMatches writes what the matcher found for one of
	// the person's receipts: a sure tie checks its item off and writes its
	// price; an unsure one is proposed. A line or an item that already has a
	// match on this receipt is skipped, so running the matcher again changes
	// nothing that was decided. It answers the matches written and the lists
	// whose items changed.
	ApplyShoppingReceiptMatches(userId, financeReceiptId string, proposals []*models.ShoppingReceiptMatchProposal) ([]*models.ShoppingReceiptMatch, []string, error)

	// ListShoppingReceiptMatches are a receipt's matches to items the person
	// reaches, in the order of the receipt's lines.
	ListShoppingReceiptMatches(userId, financeReceiptId string) ([]*models.ShoppingReceiptMatch, error)

	// DecideShoppingReceiptMatch applies a proposed match, or dismisses one,
	// undoing it when it was applied. It answers the match and its item's list.
	DecideShoppingReceiptMatch(userId, shoppingReceiptMatchId string, isApplied bool) (*models.ShoppingReceiptMatch, string, error)

	// MatchShoppingReceiptLine ties a line of one of the person's receipts to
	// an item they reach, applied, as a person decides it.
	MatchShoppingReceiptLine(userId, financeReceiptLineId, shoppingItemId string) (*models.ShoppingReceiptMatch, string, error)

	// UndoShoppingReceipt undoes every applied match of one of the person's
	// receipts, and answers the lists whose items changed.
	UndoShoppingReceipt(userId, financeReceiptId string) ([]string, error)

	// AddShoppingItemFromReceiptLine puts what a line bought on a list the
	// person reaches, as bought, with the line's price.
	AddShoppingItemFromReceiptLine(userId, financeReceiptLineId, shoppingListId string) (*models.ShoppingItem, error)
}

type shoppingReceiptMatchModel struct {
	ID                          string     `gorm:"column:id;primaryKey"`
	FinanceReceiptID            string     `gorm:"column:finance_receipt_id"`
	ReceiptLineNumber           int        `gorm:"column:receipt_line_number"`
	ShoppingItemID              string     `gorm:"column:shopping_item_id"`
	ReceiptMatchState           string     `gorm:"column:receipt_match_state"`
	MatchedQuantity             *string    `gorm:"column:matched_quantity"`
	PreviousShoppingItemState   string     `gorm:"column:previous_shopping_item_state"`
	PreviousNeededQuantity      *string    `gorm:"column:previous_needed_quantity"`
	PreviousLastBoughtAt        *time.Time `gorm:"column:previous_last_bought_at"`
	PreviousOutOfStockAt        *time.Time `gorm:"column:previous_out_of_stock_at"`
	PreviousOutOfStockByUserID  *string    `gorm:"column:previous_out_of_stock_by_user_id"`
	PreviousOutOfStockStoreName string     `gorm:"column:previous_out_of_stock_store_name"`
	ShoppingItemPriceID         *string    `gorm:"column:shopping_item_price_id"`
	DecidedByUserID             *string    `gorm:"column:decided_by_user_id"`
	CreatedAt                   time.Time  `gorm:"column:created_at"`
	DecidedAt                   *time.Time `gorm:"column:decided_at"`
}

func (shoppingReceiptMatchModel) TableName() string { return "shopping_receipt_match" }

func (self *shoppingReceiptMatchModel) toModel() *models.ShoppingReceiptMatch {
	return &models.ShoppingReceiptMatch{
		ID: self.ID, FinanceReceiptID: self.FinanceReceiptID, ReceiptLineNumber: self.ReceiptLineNumber, ShoppingItemID: self.ShoppingItemID,
		ReceiptMatchState: models.ReceiptMatchState(self.ReceiptMatchState), MatchedQuantity: trimDecimal(optionalString(self.MatchedQuantity)),
		PreviousShoppingItemState: models.ShoppingItemState(self.PreviousShoppingItemState), PreviousNeededQuantity: trimDecimal(optionalString(self.PreviousNeededQuantity)),
		ShoppingItemPriceID: optionalString(self.ShoppingItemPriceID), DecidedByUserID: optionalString(self.DecidedByUserID),
		CreatedAt: self.CreatedAt.UTC(), DecidedAt: utcOrNil(self.DecidedAt),
	}
}

// matchesWithLineIds fills in, for each match, the identifier of the line of
// that number the receipt has now: a receipt read again has new line rows.
func (self *transaction) matchesWithLineIds(rows []shoppingReceiptMatchModel) ([]*models.ShoppingReceiptMatch, error) {
	matches := make([]*models.ShoppingReceiptMatch, 0, len(rows))
	receiptIds := []string{}
	for index := range rows {
		matches = append(matches, rows[index].toModel())
		receiptIds = append(receiptIds, rows[index].FinanceReceiptID)
	}
	if receiptIds = uniqueStrings(receiptIds); len(receiptIds) == 0 {
		return matches, nil
	}
	var lines []agentFinanceReceiptLineModel
	if err := self.tx.Where("\"receipt_id\" IN ?", receiptIds).Find(&lines).Error; err != nil {
		return nil, err
	}
	lineIds := map[string]string{}
	for _, line := range lines {
		lineIds[fmt.Sprintf("%s %d", line.ReceiptID, line.LineNumber)] = line.ID
	}
	for _, match := range matches {
		match.FinanceReceiptLineID = lineIds[fmt.Sprintf("%s %d", match.FinanceReceiptID, match.ReceiptLineNumber)]
	}
	return matches, nil
}

// ownShoppingReceipt is one of the person's receipts: one their own agent
// keeps. ErrNotFound for any other.
func (self *transaction) ownShoppingReceipt(userId, financeReceiptId string) (*agentFinanceReceiptModel, error) {
	if userId == "" || financeReceiptId == "" {
		return nil, ErrNotFound
	}
	var row agentFinanceReceiptModel
	isFound, err := findOneRow(self.tx.Where(`"id" = ? AND "agent_id" IN (SELECT "id" FROM "agent" WHERE "user_id" = ?)`, financeReceiptId, userId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &row, nil
}

// ownShoppingReceiptLine is an item line of one of the person's receipts,
// with its receipt.
func (self *transaction) ownShoppingReceiptLine(userId, financeReceiptLineId string) (*agentFinanceReceiptLineModel, *agentFinanceReceiptModel, error) {
	if userId == "" || financeReceiptLineId == "" {
		return nil, nil, ErrNotFound
	}
	var line agentFinanceReceiptLineModel
	isFound, err := findOneRow(self.tx.Where(`"id" = ? AND "agent_id" IN (SELECT "id" FROM "agent" WHERE "user_id" = ?)`, financeReceiptLineId, userId), &line)
	if err != nil {
		return nil, nil, err
	}
	if !isFound {
		return nil, nil, ErrNotFound
	}
	if line.ReceiptLineKind != string(models.ReceiptLineKindItem) {
		return nil, nil, fmt.Errorf("%w: only an item line buys something", ErrInvalidArguments)
	}
	receipt, err := self.ownShoppingReceipt(userId, line.ReceiptID)
	if err != nil {
		return nil, nil, err
	}
	return &line, receipt, nil
}

// receiptLineOfNumber is the item line of a receipt with that number now.
func (self *transaction) receiptLineOfNumber(financeReceiptId string, lineNumber int) (*agentFinanceReceiptLineModel, error) {
	var line agentFinanceReceiptLineModel
	isFound, err := findOneRow(self.tx.Where("\"receipt_id\" = ? AND \"line_number\" = ? AND \"receipt_line_kind\" = ?",
		financeReceiptId, lineNumber, string(models.ReceiptLineKindItem)), &line)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &line, nil
}

// isShoppingStoreOfMerchant says an item's store is the merchant's: one of
// the two compared names holds the other ("examplemarket" in "examplemarket552").
func isShoppingStoreOfMerchant(storeKey, merchantKey string) bool {
	if storeKey == "" || merchantKey == "" {
		return false
	}
	return strings.Contains(merchantKey, storeKey) || strings.Contains(storeKey, merchantKey)
}

func (self *transaction) ListShoppingReceiptCandidates(userId, merchantName string) ([]*models.ShoppingItem, error) {
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	if len(listIds) == 0 {
		return []*models.ShoppingItem{}, nil
	}
	// The store is compared here rather than in the query, by the loose rule
	// above; every item still to buy is read first, a list's worth at most.
	items, err := self.shoppingItemsOf(self.tx.Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" IN ?", listIds,
		[]string{string(models.ShoppingItemStateNeeded), string(models.ShoppingItemStateInCart)}).
		Order("\"updated_at\" DESC").Limit(shoppingItemsPerListMost))
	if err != nil {
		return nil, err
	}
	merchantKey := models.ShoppingStoreKey(merchantName)
	candidates := []*models.ShoppingItem{}
	for _, item := range items {
		if item.StoreKey == "" || isShoppingStoreOfMerchant(item.StoreKey, merchantKey) {
			candidates = append(candidates, item)
		}
	}
	return candidates, nil
}

// receiptLineUnitPrice is what one of a line's things cost: its printed unit
// price, or its amount over its quantity (a count or a weight), or its
// amount; empty when none of those is above zero (a free or a discount line).
func receiptLineUnitPrice(line *agentFinanceReceiptLineModel) string {
	if line.UnitPriceAmount != nil {
		if value, isParsed := parseRat(*line.UnitPriceAmount); isParsed && value.Sign() > 0 {
			return trimDecimal(value.FloatString(8))
		}
	}
	amount, isParsed := parseRat(line.LineAmount)
	if !isParsed || amount.Sign() <= 0 {
		return ""
	}
	if line.Quantity != nil {
		if quantity, isParsed := parseRat(*line.Quantity); isParsed && quantity.Sign() > 0 {
			return trimDecimal(new(big.Rat).Quo(amount, quantity).FloatString(8))
		}
	}
	return trimDecimal(amount.FloatString(8))
}

// applyShoppingReceiptMatch checks an item off for a receipt line: its price
// at the store is kept, and it is bought, or still needed for what the line
// did not buy of it. Everything it changes about the item goes on the match
// first, so undoing puts back exactly that.
func (self *transaction) applyShoppingReceiptMatch(userId string, receipt *agentFinanceReceiptModel, line *agentFinanceReceiptLineModel,
	row *shoppingItemModel, matchedQuantity string, match *shoppingReceiptMatchModel) error {
	now := time.Now()
	boughtAt := now
	if receipt.PurchasedAt != nil {
		boughtAt = *receipt.PurchasedAt
	} else if receipt.PurchasedOn != nil {
		boughtAt = *receipt.PurchasedOn
	}
	previousQuantity := row.NeededQuantity
	match.PreviousShoppingItemState, match.PreviousNeededQuantity = row.ShoppingItemState, &previousQuantity
	match.PreviousLastBoughtAt, match.PreviousOutOfStockAt = row.LastBoughtAt, row.OutOfStockAt
	match.PreviousOutOfStockByUserID, match.PreviousOutOfStockStoreName = row.OutOfStockByUserID, row.OutOfStockStoreName
	if matchedQuantity == "" && line.Quantity != nil && line.QuantityUnit == "" {
		matchedQuantity = *line.Quantity
	}
	if strings.TrimSpace(matchedQuantity) != "" {
		if canonical, err := canonicalShoppingQuantity(matchedQuantity); err == nil {
			match.MatchedQuantity = &canonical
		}
	}
	if unitPrice := receiptLineUnitPrice(line); unitPrice != "" {
		storeName := receipt.MerchantName
		if isShoppingStoreOfMerchant(row.StoreKey, models.ShoppingStoreKey(receipt.MerchantName)) {
			storeName = row.StoreName
		}
		observedOn := ""
		if receipt.PurchasedOn != nil {
			observedOn = receipt.PurchasedOn.Format(time.DateOnly)
		}
		quantity := optionalString(match.MatchedQuantity)
		if quantity == "" && line.Quantity != nil {
			quantity = *line.Quantity
		}
		price, err := self.recordShoppingItemPrice(row, &models.ShoppingItemPrice{
			StoreName: storeName, UnitPriceAmount: unitPrice, CurrencyCode: receipt.CurrencyCode, Quantity: quantity,
			QuantityUnit: line.QuantityUnit, ObservedOn: observedOn, PriceSource: models.ShoppingPriceSourceReceipt,
			FinanceReceiptID: receipt.ID, CreatedByUserID: userId,
		})
		if err != nil {
			return err
		}
		match.ShoppingItemPriceID = &price.ID
	}
	// Bought fewer than needed: what is left is still needed.
	updates := withUpdatedAt(boughtShoppingItemUpdates(boughtAt), now)
	if match.MatchedQuantity != nil && models.ShoppingItemState(row.ShoppingItemState) == models.ShoppingItemStateNeeded {
		needed, isNeededParsed := parseRat(row.NeededQuantity)
		bought, isBoughtParsed := parseRat(*match.MatchedQuantity)
		if isNeededParsed && isBoughtParsed && bought.Cmp(needed) < 0 {
			left := new(big.Rat).Sub(needed, bought)
			updates = map[string]any{"needed_quantity": trimDecimal(left.FloatString(3)), "last_bought_at": boughtAt, "updated_at": now}
		}
	}
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(updates).Error; err != nil {
		return err
	}
	decidedAt := time.Now()
	match.ReceiptMatchState, match.DecidedAt = string(models.ReceiptMatchStateApplied), &decidedAt
	return self.touchShoppingList(row.ShoppingListID)
}

func (self *transaction) ApplyShoppingReceiptMatches(userId, financeReceiptId string, proposals []*models.ShoppingReceiptMatchProposal) ([]*models.ShoppingReceiptMatch, []string, error) {
	receipt, err := self.ownShoppingReceipt(userId, financeReceiptId)
	if err != nil {
		return nil, nil, err
	}
	var existing []shoppingReceiptMatchModel
	if err := self.tx.Where("\"finance_receipt_id\" = ?", receipt.ID).Find(&existing).Error; err != nil {
		return nil, nil, err
	}
	isLineTaken, isItemTaken, isDismissed := map[int]bool{}, map[string]bool{}, map[string]bool{}
	for _, match := range existing {
		if match.ReceiptMatchState == string(models.ReceiptMatchStateDismissed) {
			isDismissed[fmt.Sprintf("%d %s", match.ReceiptLineNumber, match.ShoppingItemID)] = true
			continue
		}
		isLineTaken[match.ReceiptLineNumber] = true
		isItemTaken[match.ShoppingItemID] = true
	}
	written := []*shoppingReceiptMatchModel{}
	changedListIds := []string{}
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		var line agentFinanceReceiptLineModel
		isFound, err := findOneRow(self.tx.Where("\"id\" = ? AND \"receipt_id\" = ? AND \"receipt_line_kind\" = ?",
			proposal.FinanceReceiptLineID, receipt.ID, string(models.ReceiptLineKindItem)), &line)
		if err != nil {
			return nil, nil, err
		}
		if !isFound || isLineTaken[line.LineNumber] || isItemTaken[proposal.ShoppingItemID] ||
			isDismissed[fmt.Sprintf("%d %s", line.LineNumber, proposal.ShoppingItemID)] {
			continue
		}
		row, err := self.lockShoppingItem(userId, proposal.ShoppingItemID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if models.ShoppingItemState(row.ShoppingItemState) == models.ShoppingItemStateNotNeeded {
			continue
		}
		match := &shoppingReceiptMatchModel{
			ID: newID(), FinanceReceiptID: receipt.ID, ReceiptLineNumber: line.LineNumber, ShoppingItemID: row.ID,
			ReceiptMatchState: string(models.ReceiptMatchStateProposed), CreatedAt: time.Now(),
		}
		if proposal.IsSure {
			if err := self.applyShoppingReceiptMatch(userId, receipt, &line, row, proposal.MatchedQuantity, match); err != nil {
				return nil, nil, err
			}
		} else if strings.TrimSpace(proposal.MatchedQuantity) != "" {
			if canonical, err := canonicalShoppingQuantity(proposal.MatchedQuantity); err == nil {
				match.MatchedQuantity = &canonical
			}
		}
		if err := self.tx.Create(match).Error; err != nil {
			return nil, nil, err
		}
		isLineTaken[line.LineNumber], isItemTaken[row.ID] = true, true
		written = append(written, match)
		changedListIds = append(changedListIds, row.ShoppingListID)
	}
	rows := make([]shoppingReceiptMatchModel, 0, len(written))
	for _, match := range written {
		rows = append(rows, *match)
	}
	matches, err := self.matchesWithLineIds(rows)
	if err != nil {
		return nil, nil, err
	}
	return matches, uniqueStrings(changedListIds), nil
}

func (self *transaction) ListShoppingReceiptMatches(userId, financeReceiptId string) ([]*models.ShoppingReceiptMatch, error) {
	receipt, err := self.ownShoppingReceipt(userId, financeReceiptId)
	if err != nil {
		return nil, err
	}
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	if len(listIds) == 0 {
		return []*models.ShoppingReceiptMatch{}, nil
	}
	var rows []shoppingReceiptMatchModel
	if err := self.tx.Where(`"finance_receipt_id" = ? AND "shopping_item_id" IN (SELECT "id" FROM "shopping_item" WHERE "shopping_list_id" IN ?)`,
		receipt.ID, listIds).Order("\"receipt_line_number\", \"created_at\"").Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.matchesWithLineIds(rows)
}

// undoShoppingReceiptMatch takes away the price an applied match wrote and,
// unless the item was changed since, puts it back as it was before. It never
// touches an item the person no longer reaches: the caller checks.
func (self *transaction) undoShoppingReceiptMatch(match *shoppingReceiptMatchModel) error {
	row, err := self.lockShoppingItem("", match.ShoppingItemID)
	if err != nil {
		return err
	}
	if match.ShoppingItemPriceID != nil {
		if err := self.tx.Where("\"id\" = ?", *match.ShoppingItemPriceID).Delete(&shoppingItemPriceModel{}).Error; err != nil {
			return err
		}
		if err := self.refreshShoppingItemPrice(row.ID); err != nil {
			return err
		}
	}
	// A change made to the item after the match (needed again, checked off,
	// a new count) is the newer word, and stays.
	isChangedSince := match.DecidedAt != nil && row.UpdatedAt.After(match.DecidedAt.Add(time.Second))
	if !isChangedSince {
		updates := map[string]any{
			"last_bought_at": match.PreviousLastBoughtAt, "out_of_stock_at": match.PreviousOutOfStockAt,
			"out_of_stock_by_user_id": match.PreviousOutOfStockByUserID, "out_of_stock_store_name": match.PreviousOutOfStockStoreName,
			"updated_at": time.Now(),
		}
		switch models.ShoppingItemState(match.PreviousShoppingItemState) {
		case models.ShoppingItemStateNeeded, models.ShoppingItemStateInCart:
			// Back to needed: the cart it was in belongs to a trip that may be over.
			updates["shopping_item_state"] = string(models.ShoppingItemStateNeeded)
			updates["carted_by_user_id"], updates["carted_at"] = nil, nil
		case models.ShoppingItemStateNotNeeded:
			updates["shopping_item_state"] = string(models.ShoppingItemStateNotNeeded)
			updates["carted_by_user_id"], updates["carted_at"] = nil, nil
		}
		if match.PreviousNeededQuantity != nil {
			updates["needed_quantity"] = *match.PreviousNeededQuantity
		}
		if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return self.touchShoppingList(row.ShoppingListID)
}

// refreshShoppingItemPrice makes the newest price kept the item's price.
func (self *transaction) refreshShoppingItemPrice(shoppingItemId string) error {
	var newest shoppingItemPriceModel
	isFound, err := findOneRow(self.tx.Where("\"shopping_item_id\" = ?", shoppingItemId).Order("\"observed_on\" DESC, \"created_at\" DESC"), &newest)
	if err != nil {
		return err
	}
	updates := map[string]any{"unit_price_amount": nil}
	if isFound {
		updates = map[string]any{"unit_price_amount": newest.UnitPriceAmount, "currency_code": newest.CurrencyCode}
	}
	return self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", shoppingItemId).Updates(updates).Error
}

// dismissShoppingReceiptMatch marks a match dismissed by the person, undoing
// it first when it was applied and its item is one they reach; a match on an
// item they no longer reach is only dismissed, its item left alone.
func (self *transaction) dismissShoppingReceiptMatch(userId string, match *shoppingReceiptMatchModel) (string, error) {
	listId := ""
	if item, err := self.GetShoppingItem(userId, match.ShoppingItemID); err == nil {
		listId = item.ShoppingListID
		if match.ReceiptMatchState == string(models.ReceiptMatchStateApplied) {
			if err := self.undoShoppingReceiptMatch(match); err != nil {
				return "", err
			}
		}
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	now := time.Now()
	match.ReceiptMatchState, match.ShoppingItemPriceID, match.DecidedByUserID, match.DecidedAt = string(models.ReceiptMatchStateDismissed), nil, &userId, &now
	return listId, self.tx.Save(match).Error
}

// reachableShoppingReceiptMatch is a match of one of the person's receipts
// to an item they reach, its row held.
func (self *transaction) reachableShoppingReceiptMatch(userId, shoppingReceiptMatchId string) (*shoppingReceiptMatchModel, *agentFinanceReceiptModel, error) {
	var match shoppingReceiptMatchModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", shoppingReceiptMatchId), &match)
	if err != nil {
		return nil, nil, err
	}
	if !isFound {
		return nil, nil, ErrNotFound
	}
	receipt, err := self.ownShoppingReceipt(userId, match.FinanceReceiptID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := self.GetShoppingItem(userId, match.ShoppingItemID); err != nil {
		return nil, nil, err
	}
	return &match, receipt, nil
}

// decidedMatch reads a match back with its line's identifier.
func (self *transaction) decidedMatch(match *shoppingReceiptMatchModel) (*models.ShoppingReceiptMatch, error) {
	matches, err := self.matchesWithLineIds([]shoppingReceiptMatchModel{*match})
	if err != nil {
		return nil, err
	}
	return matches[0], nil
}

func (self *transaction) DecideShoppingReceiptMatch(userId, shoppingReceiptMatchId string, isApplied bool) (*models.ShoppingReceiptMatch, string, error) {
	match, receipt, err := self.reachableShoppingReceiptMatch(userId, shoppingReceiptMatchId)
	if err != nil {
		return nil, "", err
	}
	item, err := self.GetShoppingItemByID(match.ShoppingItemID)
	if err != nil {
		return nil, "", err
	}
	state := models.ReceiptMatchState(match.ReceiptMatchState)
	switch {
	case isApplied && state != models.ReceiptMatchStateApplied:
		line, err := self.receiptLineOfNumber(receipt.ID, match.ReceiptLineNumber)
		if err != nil {
			return nil, "", err
		}
		row, err := self.lockShoppingItem(userId, match.ShoppingItemID)
		if err != nil {
			return nil, "", err
		}
		if err := self.applyShoppingReceiptMatch(userId, receipt, line, row, optionalString(match.MatchedQuantity), match); err != nil {
			return nil, "", err
		}
		match.DecidedByUserID = &userId
		if err := self.tx.Save(match).Error; err != nil {
			return nil, "", err
		}
	case !isApplied:
		if _, err := self.dismissShoppingReceiptMatch(userId, match); err != nil {
			return nil, "", err
		}
	}
	decided, err := self.decidedMatch(match)
	if err != nil {
		return nil, "", err
	}
	return decided, item.ShoppingListID, nil
}

func (self *transaction) MatchShoppingReceiptLine(userId, financeReceiptLineId, shoppingItemId string) (*models.ShoppingReceiptMatch, string, error) {
	line, receipt, err := self.ownShoppingReceiptLine(userId, financeReceiptLineId)
	if err != nil {
		return nil, "", err
	}
	row, err := self.lockShoppingItem(userId, shoppingItemId)
	if err != nil {
		return nil, "", err
	}
	var existing shoppingReceiptMatchModel
	isFound, err := findOneRow(self.tx.Where("\"finance_receipt_id\" = ? AND \"receipt_line_number\" = ? AND \"shopping_item_id\" = ?",
		receipt.ID, line.LineNumber, row.ID), &existing)
	if err != nil {
		return nil, "", err
	}
	if isFound && existing.ReceiptMatchState == string(models.ReceiptMatchStateApplied) {
		decided, err := self.decidedMatch(&existing)
		return decided, row.ShoppingListID, err
	}
	// The line's other match, applied or proposed, gives way to the person's.
	var others []shoppingReceiptMatchModel
	if err := self.tx.Where("\"finance_receipt_id\" = ? AND \"receipt_line_number\" = ? AND \"shopping_item_id\" <> ? AND \"receipt_match_state\" <> ?",
		receipt.ID, line.LineNumber, row.ID, string(models.ReceiptMatchStateDismissed)).Find(&others).Error; err != nil {
		return nil, "", err
	}
	for index := range others {
		if _, err := self.dismissShoppingReceiptMatch(userId, &others[index]); err != nil {
			return nil, "", err
		}
	}
	match := &existing
	if !isFound {
		match = &shoppingReceiptMatchModel{ID: newID(), FinanceReceiptID: receipt.ID, ReceiptLineNumber: line.LineNumber, ShoppingItemID: row.ID, CreatedAt: time.Now()}
	}
	if err := self.applyShoppingReceiptMatch(userId, receipt, line, row, "", match); err != nil {
		return nil, "", err
	}
	match.DecidedByUserID = &userId
	if err := self.tx.Save(match).Error; err != nil {
		return nil, "", err
	}
	decided, err := self.decidedMatch(match)
	return decided, row.ShoppingListID, err
}

func (self *transaction) UndoShoppingReceipt(userId, financeReceiptId string) ([]string, error) {
	receipt, err := self.ownShoppingReceipt(userId, financeReceiptId)
	if err != nil {
		return nil, err
	}
	listIds, err := self.reachableShoppingListIds(userId)
	if err != nil {
		return nil, err
	}
	if len(listIds) == 0 {
		return []string{}, nil
	}
	var matches []shoppingReceiptMatchModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"finance_receipt_id" = ? AND "receipt_match_state" = ? AND "shopping_item_id" IN
		(SELECT "id" FROM "shopping_item" WHERE "shopping_list_id" IN ?)`, receipt.ID, string(models.ReceiptMatchStateApplied), listIds).
		Find(&matches).Error; err != nil {
		return nil, err
	}
	changed := []string{}
	for index := range matches {
		listId, err := self.dismissShoppingReceiptMatch(userId, &matches[index])
		if err != nil {
			return nil, err
		}
		changed = append(changed, listId)
	}
	return uniqueStrings(changed), nil
}

func (self *transaction) AddShoppingItemFromReceiptLine(userId, financeReceiptLineId, shoppingListId string) (*models.ShoppingItem, error) {
	line, receipt, err := self.ownShoppingReceiptLine(userId, financeReceiptLineId)
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(line.Description)
	if len([]rune(description)) > shoppingItemDescriptionLengthMost {
		description = string([]rune(description)[:shoppingItemDescriptionLengthMost])
	}
	// What the item was before, when the line merges into one already on the
	// list; a new item was not on the list at all, which undoing leaves as
	// bought rather than putting it on the list to buy.
	previous, err := self.FindShoppingItemToMerge(userId, shoppingListId, description, receipt.MerchantName)
	if err != nil {
		return nil, err
	}
	items, _, err := self.AddShoppingItems(userId, shoppingListId, []*models.ShoppingItem{{ItemDescription: description, StoreName: receipt.MerchantName}})
	if err != nil {
		return nil, err
	}
	row, err := self.lockShoppingItem(userId, items[0].ID)
	if err != nil {
		return nil, err
	}
	match := &shoppingReceiptMatchModel{ID: newID(), FinanceReceiptID: receipt.ID, ReceiptLineNumber: line.LineNumber, ShoppingItemID: row.ID, CreatedAt: time.Now()}
	if err := self.applyShoppingReceiptMatch(userId, receipt, line, row, "", match); err != nil {
		return nil, err
	}
	match.PreviousShoppingItemState = string(models.ShoppingItemStateNotNeeded)
	if previous != nil {
		quantity := previous.NeededQuantity
		match.PreviousShoppingItemState, match.PreviousNeededQuantity = string(previous.ShoppingItemState), &quantity
		match.PreviousLastBoughtAt, match.PreviousOutOfStockAt = previous.LastBoughtAt, previous.OutOfStockAt
		match.PreviousOutOfStockByUserID, match.PreviousOutOfStockStoreName = nullableText(previous.OutOfStockByUserID), previous.OutOfStockStoreName
	}
	match.DecidedByUserID = &userId
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(match).Error; err != nil {
		return nil, err
	}
	return self.GetShoppingItemByID(row.ID)
}
