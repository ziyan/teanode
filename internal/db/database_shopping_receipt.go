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
	ID                        string     `gorm:"column:id;primaryKey"`
	FinanceReceiptID          string     `gorm:"column:finance_receipt_id"`
	FinanceReceiptLineID      string     `gorm:"column:finance_receipt_line_id"`
	ShoppingItemID            string     `gorm:"column:shopping_item_id"`
	ReceiptMatchState         string     `gorm:"column:receipt_match_state"`
	MatchedQuantity           *string    `gorm:"column:matched_quantity"`
	PreviousShoppingItemState string     `gorm:"column:previous_shopping_item_state"`
	PreviousNeededQuantity    *string    `gorm:"column:previous_needed_quantity"`
	ShoppingItemPriceID       *string    `gorm:"column:shopping_item_price_id"`
	DecidedByUserID           *string    `gorm:"column:decided_by_user_id"`
	CreatedAt                 time.Time  `gorm:"column:created_at"`
	DecidedAt                 *time.Time `gorm:"column:decided_at"`
}

func (shoppingReceiptMatchModel) TableName() string { return "shopping_receipt_match" }

func (self *shoppingReceiptMatchModel) toModel() *models.ShoppingReceiptMatch {
	return &models.ShoppingReceiptMatch{
		ID: self.ID, FinanceReceiptID: self.FinanceReceiptID, FinanceReceiptLineID: self.FinanceReceiptLineID, ShoppingItemID: self.ShoppingItemID,
		ReceiptMatchState: models.ReceiptMatchState(self.ReceiptMatchState), MatchedQuantity: trimDecimal(optionalString(self.MatchedQuantity)),
		PreviousShoppingItemState: models.ShoppingItemState(self.PreviousShoppingItemState), PreviousNeededQuantity: trimDecimal(optionalString(self.PreviousNeededQuantity)),
		ShoppingItemPriceID: optionalString(self.ShoppingItemPriceID), DecidedByUserID: optionalString(self.DecidedByUserID),
		CreatedAt: self.CreatedAt.UTC(), DecidedAt: utcOrNil(self.DecidedAt),
	}
}

// ownShoppingReceipt is one of the person's receipts: one their own agent
// keeps. ErrNotFound for any other.
func (self *transaction) ownShoppingReceipt(userId, financeReceiptId string) (*agentFinanceReceiptModel, error) {
	var row agentFinanceReceiptModel
	isFound, err := findOneRow(self.tx.Where(`"id" = ? AND "agent_id" IN (SELECT "id" FROM "agent" WHERE "user_id" = ?)`, financeReceiptId, userId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound || userId == "" {
		return nil, ErrNotFound
	}
	return &row, nil
}

// ownShoppingReceiptLine is an item line of one of the person's receipts,
// with its receipt.
func (self *transaction) ownShoppingReceiptLine(userId, financeReceiptLineId string) (*agentFinanceReceiptLineModel, *agentFinanceReceiptModel, error) {
	var line agentFinanceReceiptLineModel
	isFound, err := findOneRow(self.tx.Where(`"id" = ? AND "agent_id" IN (SELECT "id" FROM "agent" WHERE "user_id" = ?)`, financeReceiptLineId, userId), &line)
	if err != nil {
		return nil, nil, err
	}
	if !isFound || userId == "" {
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

// isShoppingStoreOfMerchant says an item's store is the merchant's: one of
// the two compared names holds the other ("traderjoes" in "traderjoes552").
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
	items, err := self.shoppingItemsOf(self.tx.Where("\"shopping_list_id\" IN ? AND \"shopping_item_state\" IN ?", listIds,
		[]string{string(models.ShoppingItemStateNeeded), string(models.ShoppingItemStateInCart)}).
		Order("\"updated_at\" DESC").Limit(shoppingPageSizeMost))
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
// price, or its amount over its quantity, or its amount; empty when none of
// those is above zero (a free or a discount line).
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
	if line.Quantity != nil && line.QuantityUnit == "" {
		if quantity, isParsed := parseRat(*line.Quantity); isParsed && quantity.Sign() > 0 {
			return trimDecimal(new(big.Rat).Quo(amount, quantity).FloatString(8))
		}
	}
	return trimDecimal(amount.FloatString(8))
}

// applyShoppingReceiptMatch checks an item off for a receipt line: its price
// at the store is kept, and it is bought, or still needed for what the line
// did not buy of it. What it was before goes on the match, for undoing.
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
	if matchedQuantity == "" && line.Quantity != nil && line.QuantityUnit == "" {
		matchedQuantity = *line.Quantity
	}
	if canonical, err := canonicalShoppingQuantity(matchedQuantity); err == nil && strings.TrimSpace(matchedQuantity) != "" {
		match.MatchedQuantity = &canonical
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
		price, err := self.recordShoppingItemPrice(row, &models.ShoppingItemPrice{
			StoreName: storeName, UnitPriceAmount: unitPrice, CurrencyCode: receipt.CurrencyCode, Quantity: optionalString(match.MatchedQuantity),
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
	match.ReceiptMatchState = string(models.ReceiptMatchStateApplied)
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
	isLineTaken, isItemTaken := map[string]bool{}, map[string]bool{}
	for _, match := range existing {
		if match.ReceiptMatchState != string(models.ReceiptMatchStateDismissed) {
			isLineTaken[match.FinanceReceiptLineID] = true
			isItemTaken[match.ShoppingItemID] = true
		}
	}
	isDismissed := map[string]bool{}
	for _, match := range existing {
		if match.ReceiptMatchState == string(models.ReceiptMatchStateDismissed) {
			isDismissed[match.FinanceReceiptLineID+" "+match.ShoppingItemID] = true
		}
	}
	written := []*models.ShoppingReceiptMatch{}
	changedListIds := []string{}
	for _, proposal := range proposals {
		if proposal == nil || isLineTaken[proposal.FinanceReceiptLineID] || isItemTaken[proposal.ShoppingItemID] ||
			isDismissed[proposal.FinanceReceiptLineID+" "+proposal.ShoppingItemID] {
			continue
		}
		var line agentFinanceReceiptLineModel
		isFound, err := findOneRow(self.tx.Where("\"id\" = ? AND \"receipt_id\" = ? AND \"receipt_line_kind\" = ?",
			proposal.FinanceReceiptLineID, receipt.ID, string(models.ReceiptLineKindItem)), &line)
		if err != nil {
			return nil, nil, err
		}
		if !isFound {
			continue
		}
		row, err := self.lockShoppingItem(userId, proposal.ShoppingItemID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, nil, err
		}
		if models.ShoppingItemState(row.ShoppingItemState) == models.ShoppingItemStateNotNeeded {
			continue
		}
		match := &shoppingReceiptMatchModel{
			ID: newID(), FinanceReceiptID: receipt.ID, FinanceReceiptLineID: line.ID, ShoppingItemID: row.ID,
			ReceiptMatchState: string(models.ReceiptMatchStateProposed), CreatedAt: time.Now(),
		}
		if proposal.IsSure {
			if err := self.applyShoppingReceiptMatch(userId, receipt, &line, row, proposal.MatchedQuantity, match); err != nil {
				return nil, nil, err
			}
			decidedAt := time.Now()
			match.DecidedAt = &decidedAt
			changedListIds = append(changedListIds, row.ShoppingListID)
		} else if canonical, err := canonicalShoppingQuantity(proposal.MatchedQuantity); err == nil && strings.TrimSpace(proposal.MatchedQuantity) != "" {
			match.MatchedQuantity = &canonical
		}
		if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(match).Error; err != nil {
			return nil, nil, err
		}
		isLineTaken[line.ID], isItemTaken[row.ID] = true, true
		written = append(written, match.toModel())
		if !proposal.IsSure {
			changedListIds = append(changedListIds, row.ShoppingListID)
		}
	}
	return written, uniqueStrings(changedListIds), nil
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
	matches := []*models.ShoppingReceiptMatch{}
	if len(listIds) == 0 {
		return matches, nil
	}
	var rows []shoppingReceiptMatchModel
	if err := self.tx.Raw(`SELECT receipt_match.* FROM "shopping_receipt_match" receipt_match
		JOIN "agent_finance_receipt_line" line ON line.id = receipt_match.finance_receipt_line_id
		JOIN "shopping_item" item ON item.id = receipt_match.shopping_item_id
		WHERE receipt_match.finance_receipt_id = ? AND item.shopping_list_id IN ?
		ORDER BY line.line_number, receipt_match.created_at`, receipt.ID, listIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		matches = append(matches, rows[index].toModel())
	}
	return matches, nil
}

// undoShoppingReceiptMatch puts an applied match's item back as it was and
// takes away the price it wrote.
func (self *transaction) undoShoppingReceiptMatch(match *shoppingReceiptMatchModel) error {
	row, err := self.lockShoppingItem("", match.ShoppingItemID)
	if err != nil {
		return err
	}
	updates := map[string]any{"updated_at": time.Now()}
	switch models.ShoppingItemState(match.PreviousShoppingItemState) {
	case models.ShoppingItemStateNeeded, models.ShoppingItemStateInCart:
		// Back to needed: the cart it was in belongs to a trip that may be over.
		updates["shopping_item_state"] = string(models.ShoppingItemStateNeeded)
		updates["carted_by_user_id"], updates["carted_at"] = nil, nil
	}
	if match.PreviousNeededQuantity != nil {
		updates["needed_quantity"] = *match.PreviousNeededQuantity
	}
	if err := self.tx.Model(&shoppingItemModel{}).Where("\"id\" = ?", row.ID).Updates(updates).Error; err != nil {
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

func (self *transaction) DecideShoppingReceiptMatch(userId, shoppingReceiptMatchId string, isApplied bool) (*models.ShoppingReceiptMatch, string, error) {
	match, receipt, err := self.reachableShoppingReceiptMatch(userId, shoppingReceiptMatchId)
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	state := models.ReceiptMatchState(match.ReceiptMatchState)
	switch {
	case isApplied && state != models.ReceiptMatchStateApplied:
		var line agentFinanceReceiptLineModel
		if err := self.tx.Where("\"id\" = ?", match.FinanceReceiptLineID).Take(&line).Error; err != nil {
			return nil, "", err
		}
		row, err := self.lockShoppingItem(userId, match.ShoppingItemID)
		if err != nil {
			return nil, "", err
		}
		if err := self.applyShoppingReceiptMatch(userId, receipt, &line, row, optionalString(match.MatchedQuantity), match); err != nil {
			return nil, "", err
		}
	case !isApplied && state == models.ReceiptMatchStateApplied:
		if err := self.undoShoppingReceiptMatch(match); err != nil {
			return nil, "", err
		}
		match.ReceiptMatchState, match.ShoppingItemPriceID = string(models.ReceiptMatchStateDismissed), nil
	case !isApplied:
		match.ReceiptMatchState = string(models.ReceiptMatchStateDismissed)
	}
	match.DecidedByUserID, match.DecidedAt = &userId, &now
	if err := self.tx.Save(match).Error; err != nil {
		return nil, "", err
	}
	item, err := self.GetShoppingItemByID(match.ShoppingItemID)
	if err != nil {
		return nil, "", err
	}
	return match.toModel(), item.ShoppingListID, nil
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
	isFound, err := findOneRow(self.tx.Where("\"finance_receipt_line_id\" = ? AND \"shopping_item_id\" = ?", line.ID, row.ID), &existing)
	if err != nil {
		return nil, "", err
	}
	if isFound && existing.ReceiptMatchState == string(models.ReceiptMatchStateApplied) {
		return existing.toModel(), row.ShoppingListID, nil
	}
	// The line's other match, applied or proposed, gives way to the person's.
	var others []shoppingReceiptMatchModel
	if err := self.tx.Where("\"finance_receipt_line_id\" = ? AND \"shopping_item_id\" <> ? AND \"receipt_match_state\" <> ?", line.ID, row.ID,
		string(models.ReceiptMatchStateDismissed)).Find(&others).Error; err != nil {
		return nil, "", err
	}
	now := time.Now()
	for index := range others {
		other := &others[index]
		if other.ReceiptMatchState == string(models.ReceiptMatchStateApplied) {
			if err := self.undoShoppingReceiptMatch(other); err != nil {
				return nil, "", err
			}
		}
		other.ReceiptMatchState, other.ShoppingItemPriceID, other.DecidedByUserID, other.DecidedAt = string(models.ReceiptMatchStateDismissed), nil, &userId, &now
		if err := self.tx.Save(other).Error; err != nil {
			return nil, "", err
		}
	}
	match := &existing
	if !isFound {
		match = &shoppingReceiptMatchModel{ID: newID(), FinanceReceiptID: receipt.ID, FinanceReceiptLineID: line.ID, ShoppingItemID: row.ID, CreatedAt: now}
	}
	if err := self.applyShoppingReceiptMatch(userId, receipt, line, row, "", match); err != nil {
		return nil, "", err
	}
	match.DecidedByUserID, match.DecidedAt = &userId, &now
	if err := self.tx.Save(match).Error; err != nil {
		return nil, "", err
	}
	return match.toModel(), row.ShoppingListID, nil
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
	now := time.Now()
	changed := []string{}
	for index := range matches {
		match := &matches[index]
		item, err := self.GetShoppingItemByID(match.ShoppingItemID)
		if err != nil {
			return nil, err
		}
		if err := self.undoShoppingReceiptMatch(match); err != nil {
			return nil, err
		}
		match.ReceiptMatchState, match.ShoppingItemPriceID, match.DecidedByUserID, match.DecidedAt = string(models.ReceiptMatchStateDismissed), nil, &userId, &now
		if err := self.tx.Save(match).Error; err != nil {
			return nil, err
		}
		changed = append(changed, item.ShoppingListID)
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
	items, _, err := self.AddShoppingItems(userId, shoppingListId, []*models.ShoppingItem{{ItemDescription: description, StoreName: receipt.MerchantName}})
	if err != nil {
		return nil, err
	}
	row, err := self.lockShoppingItem(userId, items[0].ID)
	if err != nil {
		return nil, err
	}
	match := &shoppingReceiptMatchModel{ID: newID(), FinanceReceiptID: receipt.ID, FinanceReceiptLineID: line.ID, ShoppingItemID: row.ID, CreatedAt: time.Now()}
	if err := self.applyShoppingReceiptMatch(userId, receipt, line, row, "", match); err != nil {
		return nil, err
	}
	now := time.Now()
	match.DecidedByUserID, match.DecidedAt = &userId, &now
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(match).Error; err != nil {
		return nil, err
	}
	return self.GetShoppingItemByID(row.ID)
}
