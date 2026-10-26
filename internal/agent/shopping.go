package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools/websearch"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Shopping lists' work for the agent: matching every receipt to the items a
// person's lists need, finding an item's picture on the web, and reading a
// pasted list or a screenshot of one into items. docs/subsystems/shopping.md
// says how they fit.

// QueueShoppingReceiptMatch queues the matching of one of the agent's
// receipts to the shopping lists its owner reaches, when the owner has items
// a receipt from that merchant could have bought. RecordReceipt calls it for
// every receipt, so a receipt is one system: uploaded on a list or in
// Finance, read from a mail, or sent to the agent as a photo.
func (self *Agent) QueueShoppingReceiptMatch(tx db.Transaction, agentRow *models.Agent, receipt *models.FinanceReceipt) error {
	if agentRow == nil || receipt == nil || receipt.ID == "" {
		return nil
	}
	candidates, err := tx.ListShoppingReceiptCandidates(agentRow.UserID, receipt.MerchantName)
	if err != nil || len(candidates) == 0 {
		return err
	}
	_, err = self.Enqueue(tx, models.AgentJobMatchShopping, agentRow.ID, "", receipt.ID)
	return err
}

// QueueShoppingPicture queues the search for an item's picture, on the agent
// of whoever asked for it.
func (self *Agent) QueueShoppingPicture(tx db.Transaction, agentId, shoppingItemId string) error {
	_, err := self.Enqueue(tx, models.AgentJobShoppingPicture, agentId, "", shoppingItemId)
	return err
}

// shoppingMatchAnswer is what the model answers about a receipt's lines.
type shoppingMatchAnswer struct {
	ShoppingMatches []struct {
		LineNumber int    `json:"lineNumber"`
		ItemNumber int    `json:"itemNumber"`
		Quantity   string `json:"quantity"`
		IsSure     bool   `json:"isSure"`
	} `json:"shoppingMatches"`
}

// runMatchShopping is the handler for a receipt's shopping match job.
func (self *Agent) runMatchShopping(ctx context.Context, run *Run) error {
	if !self.canThink(run.Configuration()) || self.settings.Registry == nil {
		return nil
	}
	var receipt *models.FinanceReceipt
	var candidates []*models.ShoppingItem
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if receipt, err = tx.GetFinanceReceipt(run.Agent.ID, run.Job.SubjectID); err != nil || receipt == nil {
			return err
		}
		candidates, err = tx.ListShoppingReceiptCandidates(run.Owner.ID, receipt.MerchantName)
		return err
	}); err != nil {
		return err
	}
	if receipt == nil || len(candidates) == 0 {
		return nil
	}
	lines := []*models.FinanceReceiptLine{}
	var linesText strings.Builder
	for _, line := range receipt.ReceiptLines {
		if line.ReceiptLineKind != models.ReceiptLineKindItem {
			continue
		}
		lines = append(lines, line)
		fmt.Fprintf(&linesText, "line %d: %s", line.LineNumber, line.Description)
		if line.Quantity != "" {
			fmt.Fprintf(&linesText, ", quantity %s %s", line.Quantity, line.QuantityUnit)
		}
		fmt.Fprintf(&linesText, ", amount %s\n", line.LineAmount)
	}
	if len(lines) == 0 {
		return nil
	}
	var itemsText strings.Builder
	for index, item := range candidates {
		fmt.Fprintf(&itemsText, "item %d: %s", index+1, item.ItemDescription)
		if item.ItemDetails != "" {
			fmt.Fprintf(&itemsText, " (%s)", item.ItemDetails)
		}
		store := item.StoreName
		if store == "" {
			store = "any store"
		}
		fmt.Fprintf(&itemsText, ", needed %s %s, from %s, on the list %q\n", item.NeededQuantity, item.QuantityUnit, store, item.ListName)
	}
	prompt, err := render("shopping_match.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Lines":      fenced("merchant: " + receipt.MerchantName + "\n" + linesText.String()),
		"Items":      fenced(itemsText.String()),
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Matching a receipt from "+receipt.MerchantName+" to shopping lists", prompt, nil, noTools, 1,
		models.AgentJobMatchShopping, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[shoppingMatchAnswer](thinking.Text, "shoppingMatches")
	if !answer.IsValid {
		self.retitle(ctx, run, thinking.Conversation, "Could not tell what the shopping match said: "+answer.Problem)
		return nil
	}
	lineIds := map[int]string{}
	for _, line := range lines {
		lineIds[line.LineNumber] = line.ID
	}
	proposals := []*models.ShoppingReceiptMatchProposal{}
	for _, match := range answer.Value.ShoppingMatches {
		lineId := lineIds[match.LineNumber]
		if lineId == "" || match.ItemNumber < 1 || match.ItemNumber > len(candidates) {
			continue
		}
		proposals = append(proposals, &models.ShoppingReceiptMatchProposal{
			FinanceReceiptLineID: lineId, ShoppingItemID: candidates[match.ItemNumber-1].ID,
			MatchedQuantity: strings.TrimSpace(match.Quantity), IsSure: match.IsSure,
		})
	}
	var written []*models.ShoppingReceiptMatch
	var changedListIds []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		written, changedListIds, err = tx.ApplyShoppingReceiptMatches(run.Owner.ID, receipt.ID, proposals)
		return err
	}); err != nil {
		return err
	}
	for _, listId := range changedListIds {
		if err := run.Database().NotifyShoppingEvent(listId); err != nil {
			log.Warningf("cannot announce a change to a shopping list: %s", err)
		}
	}
	appliedCount, proposedCount := 0, 0
	for _, match := range written {
		if match.ReceiptMatchState == models.ReceiptMatchStateApplied {
			appliedCount++
		} else {
			proposedCount++
		}
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("A receipt from %s checked off %d shopping items; %d more wait to be confirmed",
		receipt.MerchantName, appliedCount, proposedCount))
	return nil
}

// shoppingPictureAnswer is the search result the model picked, by number,
// or zero for none.
type shoppingPictureAnswer struct {
	ResultNumber int `json:"resultNumber"`
}

// shoppingPictureSearchCount is how many results are shown to the model, and
// shoppingPictureFetchMost how many pages are read for a picture before
// giving up.
const (
	shoppingPictureSearchCount = 8
	shoppingPictureFetchMost   = 3
)

// shoppingPictureQuery is what is searched for an item: the store's name
// with a store brand, so its own packaging is found, and the description
// alone otherwise.
func shoppingPictureQuery(item *models.ShoppingItem) string {
	if item.IsStoreBrand && item.StoreName != "" {
		return item.StoreName + " " + item.ItemDescription
	}
	if item.StoreName != "" {
		return item.ItemDescription + " " + item.StoreName
	}
	return item.ItemDescription + " grocery"
}

// runShoppingPicture is the handler for an item's picture job.
func (self *Agent) runShoppingPicture(ctx context.Context, run *Run) error {
	var item *models.ShoppingItem
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		item, err = tx.GetShoppingItem(run.Owner.ID, run.Job.SubjectID)
		if errors.Is(err, db.ErrNotFound) {
			item, err = nil, nil
		}
		return err
	}); err != nil || item == nil {
		return err
	}
	markSearched := func() error {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkShoppingItemPictureSearched(item.ID)
		})
	}
	search := run.Configuration().Agent.Search
	if search.Kind == "" || search.APIKey == "" || run.Storage() == nil {
		return markSearched()
	}
	results, err := websearch.Search(ctx, search, shoppingPictureQuery(item), shoppingPictureSearchCount, "")
	if err != nil {
		log.Debugf("cannot search for a shopping item's picture: %s", err)
		return markSearched()
	}
	// The page the item has a picture from now is not offered again: asking
	// again means "find another".
	offered := []websearch.Result{}
	for _, result := range results {
		if result.URL != "" && result.URL != item.PicturePageURL {
			offered = append(offered, result)
		}
	}
	if len(offered) == 0 {
		return markSearched()
	}
	order := make([]int, 0, len(offered))
	if self.canThink(run.Configuration()) && self.settings.Registry != nil {
		var resultsText strings.Builder
		for index, result := range offered {
			fmt.Fprintf(&resultsText, "result %d: %s\n%s\n%s\n\n", index+1, result.Title, result.URL, result.Description)
		}
		prompt, err := render("shopping_picture.txt", map[string]any{
			"Item":    fenced(item.ItemDescription + "\n" + item.ItemDetails + "\nstore: " + item.StoreName),
			"Results": fenced(resultsText.String()),
		})
		if err != nil {
			return err
		}
		thinking, err := self.thinkAbout(ctx, run, "Finding a picture of "+item.ItemDescription, prompt, nil, noTools, 1,
			models.AgentJobShoppingPicture, config.AgentWorkScan)
		if err != nil {
			return err
		}
		answer := readModelAnswer[shoppingPictureAnswer](thinking.Text, "resultNumber")
		if answer.IsValid && answer.Value.ResultNumber >= 1 && answer.Value.ResultNumber <= len(offered) {
			order = append(order, answer.Value.ResultNumber-1)
		} else if answer.IsValid && answer.Value.ResultNumber == 0 {
			return markSearched()
		}
	}
	for index := range offered {
		if len(order) == 0 || order[0] != index {
			order = append(order, index)
		}
	}
	fetcher := linkpreview.NewFetcher()
	for attempt, index := range order {
		if attempt >= shoppingPictureFetchMost {
			break
		}
		page, err := fetcher.FetchPage(ctx, offered[index].URL)
		if err != nil || page.ImageURL == "" {
			continue
		}
		image, err := fetcher.FetchImage(ctx, page.ImageURL)
		if err != nil {
			continue
		}
		pictureFileId := "shopping-picture-" + item.ID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := run.Storage().PutFile(ctx, pictureFileId, image.Content); err != nil {
			return err
		}
		var previousFileId string
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			// Written only if nobody gave the item a picture meanwhile, and
			// only while the item is still on a list its owner reaches.
			if _, err := tx.GetShoppingItem(run.Owner.ID, item.ID); err != nil {
				return err
			}
			previousFileId, err = tx.SetShoppingItemPicture(item.ID, pictureFileId, offered[index].URL, models.ShoppingPictureSourceWeb, item.PictureFileID)
			return err
		}); err != nil {
			_ = run.Storage().DeleteFile(ctx, pictureFileId)
			if errors.Is(err, db.ErrShoppingPictureChanged) || errors.Is(err, db.ErrNotFound) {
				return nil
			}
			return err
		}
		if previousFileId != "" {
			if err := run.Storage().DeleteFile(ctx, previousFileId); err != nil {
				log.Warningf("cannot remove a shopping item's old picture: %s", err)
			}
		}
		if err := run.Database().NotifyShoppingEvent(item.ShoppingListID); err != nil {
			log.Warningf("cannot announce a change to a shopping list: %s", err)
		}
		return nil
	}
	return markSearched()
}

// shoppingImportAnswer is a list read by the model.
type shoppingImportAnswer struct {
	StoreName     string `json:"storeName"`
	ShoppingItems []struct {
		ItemDescription string `json:"itemDescription"`
		ItemDetails     string `json:"itemDetails"`
		NeededQuantity  string `json:"neededQuantity"`
		QuantityUnit    string `json:"quantityUnit"`
		AisleName       string `json:"aisleName"`
		StoreName       string `json:"storeName"`
		IsStoreBrand    bool   `json:"isStoreBrand"`
	} `json:"shoppingItems"`
}

// shoppingImportPictureMost is how many pictures of a list one import reads.
const shoppingImportPictureMost = 4

// ReadShoppingList reads pasted text, the agent's uploaded pictures of a list
// (screenshots, a photo of a handwritten one), or both, into the items a
// person would add, without adding them: the caller shows them first. The
// model's answer says which store the list is for when it names one, and each
// item's count, unit, details, aisle and whether it is the store's own brand.
func (self *Agent) ReadShoppingList(ctx context.Context, agentRow *models.Agent, owner *models.User, importText string, agentAttachmentIds []string) ([]*models.ShoppingItem, error) {
	if self == nil {
		return nil, ErrUnavailable
	}
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || !self.canThink(configuration) {
		return nil, ErrUnavailable
	}
	importText = strings.TrimSpace(importText)
	if len(agentAttachmentIds) > shoppingImportPictureMost {
		return nil, fmt.Errorf("%w: read at most %d pictures of a list at once", db.ErrInvalidArguments, shoppingImportPictureMost)
	}
	if importText == "" && len(agentAttachmentIds) == 0 {
		return nil, fmt.Errorf("%w: paste a list or add a picture of one", db.ErrInvalidArguments)
	}
	if len([]rune(importText)) > 20000 {
		return nil, fmt.Errorf("%w: a list to read is at most 20,000 characters", db.ErrInvalidArguments)
	}
	pictures := []llm.ContentPart{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, agentRow, owner, time.Now()); err != nil {
			return err
		}
		for _, attachmentId := range agentAttachmentIds {
			attachment, err := tx.GetAgentAttachment(attachmentId)
			if err != nil {
				return err
			}
			if attachment == nil || attachment.AgentID != agentRow.ID {
				return db.ErrNotFound
			}
			if !IsImageAttachment(attachment.ContentType) {
				return fmt.Errorf("%w: %s is not a picture", db.ErrInvalidArguments, attachment.Name)
			}
			if attachment.Size > attachmentImageBytes {
				return fmt.Errorf("%w: %s is larger than a model takes", db.ErrInvalidArguments, attachment.Name)
			}
			pictures = append(pictures, llm.ContentPart{Type: "image", MediaType: attachment.ContentType, Data: []byte(attachment.ID)})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for index := range pictures {
		content, err := self.settings.Storage.GetFile(ctx, string(pictures[index].Data))
		if err != nil {
			return nil, fmt.Errorf("the picture of the list cannot be read: %w", err)
		}
		pictures[index].Data = content
	}
	prompt, err := render("shopping_import.txt", map[string]any{
		"PersonName": personName(owner),
		"HasPicture": len(pictures) > 0,
		"ImportText": fenced(importText),
	})
	if err != nil {
		return nil, err
	}
	thinking, err := self.thinkAbout(ctx, self.runFor(agentRow, owner, nil, ""), "Reading a shopping list", prompt, pictures, noTools, 1,
		models.AgentJobShoppingImport, config.AgentWorkScan)
	if err != nil {
		return nil, fmt.Errorf("asking the model: %w", err)
	}
	answer := readModelAnswer[shoppingImportAnswer](thinking.Text, "shoppingItems")
	if !answer.IsValid {
		return nil, fmt.Errorf("could not read the list: %s", answer.Problem)
	}
	items := []*models.ShoppingItem{}
	for _, read := range answer.Value.ShoppingItems {
		description := strings.TrimSpace(read.ItemDescription)
		if description == "" {
			continue
		}
		storeName := strings.TrimSpace(read.StoreName)
		if storeName == "" {
			storeName = strings.TrimSpace(answer.Value.StoreName)
		}
		items = append(items, &models.ShoppingItem{
			ItemDescription: description, ItemDetails: strings.TrimSpace(read.ItemDetails), NeededQuantity: strings.TrimSpace(read.NeededQuantity),
			QuantityUnit: strings.TrimSpace(read.QuantityUnit), AisleName: strings.TrimSpace(read.AisleName), StoreName: storeName,
			IsStoreBrand: read.IsStoreBrand && storeName != "",
		})
	}
	return items, nil
}
