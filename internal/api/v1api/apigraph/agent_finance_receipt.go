package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Annotations and receipts: what the person or their agent writes on a
// finance transaction, and a merchant's record of a purchase, line by
// line, matched to the charges it explains. Part of the finance area
// (FinanceQuery, FinanceMutation). A receipt is read from a message in the
// person's mailbox (by mailboxItemId), from a Gmail message the agent read
// through the Gmail skill (by gmailMessageId), or from a photo or PDF
// uploaded to the agent's attachments (by agentAttachmentId).

// AnnotateTransactionArguments name a finance transaction and what to
// write on it; empty takes the annotation away. IsAskedByPerson is for
// the agent's finance tool alone, which sets it from the run, never from
// the model: true when the person is there in the conversation the agent
// is writing from, which lets the agent replace an annotation the person
// wrote, as they asked. Anyone else's annotation is the person's own and
// replaces whatever is there.
type AnnotateTransactionArguments struct {
	FinanceTransactionID string `json:"financeTransactionId"`
	Annotation           string `json:"annotation" graphapi:"nullable"`
	IsAskedByPerson      *bool  `json:"isAskedByPerson" graphapi:"nullable"`
}

// ReceiptLine is one printed line of a receipt as read: its number on the
// receipt (its place in the list when left out), its kind (item,
// discount, tax, fee or tip), what it says, the quantity, unit and unit
// price it prints, its amount signed as printed (a discount negative), the
// receipt's tax mark on it, and for a discount the number of the item it
// takes money off.
type ReceiptLine struct {
	LineNumber           *int   `json:"lineNumber" graphapi:"nullable"`
	ReceiptLineKind      string `json:"receiptLineKind"`
	Description          string `json:"description" graphapi:"nullable"`
	Quantity             string `json:"quantity" graphapi:"nullable"`
	QuantityUnit         string `json:"quantityUnit" graphapi:"nullable"`
	UnitPriceAmount      string `json:"unitPriceAmount" graphapi:"nullable"`
	LineAmount           string `json:"lineAmount"`
	TaxClassCode         string `json:"taxClassCode" graphapi:"nullable"`
	DiscountedLineNumber *int   `json:"discountedLineNumber" graphapi:"nullable"`
}

// RecordReceiptArguments are a receipt as read and where it was read
// from: one of mailboxItemId, gmailMessageId and agentAttachmentId.
// PurchasedAt is RFC 3339, when the receipt prints a time.
// FinanceTransactionID is the charge it was handed in for, matched by
// hand; without it the matcher matches the one charge it is sure of.
// IsUnbalancedAccepted records a receipt whose lines do not add up, which
// is refused otherwise; only when the person says so.
type RecordReceiptArguments struct {
	MailboxItemID         string        `json:"mailboxItemId" graphapi:"nullable"`
	GmailMessageID        string        `json:"gmailMessageId" graphapi:"nullable"`
	AgentAttachmentID     string        `json:"agentAttachmentId" graphapi:"nullable"`
	MerchantName          string        `json:"merchantName"`
	MerchantReceiptNumber string        `json:"merchantReceiptNumber" graphapi:"nullable"`
	PurchasedOn           string        `json:"purchasedOn" graphapi:"nullable"`
	PurchasedAt           string        `json:"purchasedAt" graphapi:"nullable"`
	CurrencyCode          string        `json:"currencyCode"`
	SubtotalAmount        string        `json:"subtotalAmount" graphapi:"nullable"`
	TotalAmount           string        `json:"totalAmount"`
	PaymentAccountMask    string        `json:"paymentAccountMask" graphapi:"nullable"`
	ReceiptLines          []ReceiptLine `json:"receiptLines"`
	FinanceTransactionID  string        `json:"financeTransactionId" graphapi:"nullable"`
	IsUnbalancedAccepted  *bool         `json:"isUnbalancedAccepted" graphapi:"nullable"`
}

// ReceiptArguments name a receipt.
type ReceiptArguments struct {
	ReceiptID string `json:"receiptId"`
}

// MatchReceiptArguments name a receipt, a finance transaction, and how
// much of the charge the receipt explains: the charge's amount or the
// receipt's total, whichever is less, when left out.
type MatchReceiptArguments struct {
	ReceiptID            string `json:"receiptId"`
	FinanceTransactionID string `json:"financeTransactionId"`
	MatchedAmount        string `json:"matchedAmount" graphapi:"nullable"`
}

// UnmatchReceiptArguments name a receipt and the finance transaction to
// take it off.
type UnmatchReceiptArguments struct {
	ReceiptID            string `json:"receiptId"`
	FinanceTransactionID string `json:"financeTransactionId"`
}

// FinanceReceiptsArguments narrow a page of receipts. Every field is
// optional.
type FinanceReceiptsArguments struct {
	// FinanceTransactionID keeps the receipts matched to this finance
	// transaction.
	FinanceTransactionID string `json:"financeTransactionId" graphapi:"nullable"`

	// From and To bound the day of purchase, both included, "2006-01-02".
	// A receipt that prints no day is left out when either is given, and
	// listed after every dated one when neither is; IsUndated lists only
	// those, and cannot go with From or To.
	From      string `json:"from" graphapi:"nullable"`
	To        string `json:"to" graphapi:"nullable"`
	IsUndated *bool  `json:"isUndated" graphapi:"nullable"`

	// IsUnmatched keeps the receipts matched to no finance transaction.
	IsUnmatched *bool `json:"isUnmatched" graphapi:"nullable"`

	// Limit is at most 200; zero is 50. After is the nextCursor of the
	// page before; Offset is how many to pass over, for a page by its
	// number, counted from After when both are given.
	Limit  *int   `json:"limit" graphapi:"nullable"`
	After  string `json:"after" graphapi:"nullable"`
	Offset *int   `json:"offset" graphapi:"nullable"`
}

// FinanceReceiptPageView is one page of receipts, the cursor for the
// next, empty on the last, and how many match the filters on every page.
type FinanceReceiptPageView struct {
	FinanceReceipts []*models.FinanceReceipt `json:"financeReceipts"`
	NextCursor      string                   `json:"nextCursor,omitempty" graphapi:"nullable"`
	TotalCount      int                      `json:"totalCount"`
}

// ReceiptMatchCandidateView is a charge a receipt could explain, with the
// finance transaction itself to show: how much of it the receipt would
// explain, what agrees (the exact amount, the card's last digits, a word
// of the merchant), how many days from the purchase it posted, and
// whether the matcher matches it without asking, with its confidence.
type ReceiptMatchCandidateView struct {
	FinanceTransactionID string                     `json:"financeTransactionId"`
	FinanceTransaction   *models.FinanceTransaction `json:"financeTransaction,omitempty" graphapi:"nullable"`
	MatchedAmount        string                     `json:"matchedAmount"`
	IsExactAmount        bool                       `json:"isExactAmount"`
	IsSameAccount        bool                       `json:"isSameAccount"`
	IsMerchantNameShared bool                       `json:"isMerchantNameShared"`
	DayDistanceCount     int                        `json:"dayDistanceCount"`
	IsAutomatic          bool                       `json:"isAutomatic"`
	MatchConfidence      string                     `json:"matchConfidence,omitempty" graphapi:"nullable"`
}

// RecordedReceiptView is what recording a receipt did: the receipt as
// stored, with its lines and matches; what its check found, in a sentence;
// the charges it could explain when the matcher left them for the person;
// whether it took the place of the receipt read from the same source; and
// which of the person's matches that receipt had were taken off, since
// the receipt as read now cannot explain them, one sentence each.
type RecordedReceiptView struct {
	FinanceReceipt             *models.FinanceReceipt       `json:"financeReceipt"`
	ReceiptCheckSummary        string                       `json:"receiptCheckSummary"`
	ReceiptMatchCandidates     []*ReceiptMatchCandidateView `json:"receiptMatchCandidates"`
	IsReplaced                 bool                         `json:"isReplaced"`
	DroppedReceiptMatchReasons []string                     `json:"droppedReceiptMatchReasons"`
}

// ReceiptPreviewView is what recording a receipt would do, writing
// nothing: whether its lines add up and by how much they miss, in a
// sentence too, the charges it could explain (the automatic one marked),
// and whether it would take the place of the receipt read from the same
// source.
type ReceiptPreviewView struct {
	ReceiptCheckState      models.ReceiptCheckState     `json:"receiptCheckState"`
	CheckDifferenceAmount  string                       `json:"checkDifferenceAmount"`
	ReceiptCheckSummary    string                       `json:"receiptCheckSummary"`
	ReceiptMatchCandidates []*ReceiptMatchCandidateView `json:"receiptMatchCandidates"`
	IsReplacing            bool                         `json:"isReplacing"`
}

// ReadReceiptArguments name what to read a receipt out of: a photo or a
// text file uploaded to the agent's attachments, or a message in a mailbox
// the person grants their agent. FinanceTransactionID is the finance
// transaction an upload is the receipt for, matched by hand once read;
// only with an upload, since a message's receipt is matched by the
// matcher, or by hand with MatchReceipt once it is read.
type ReadReceiptArguments struct {
	AgentAttachmentID    string `json:"agentAttachmentId" graphapi:"nullable"`
	MailboxItemID        string `json:"mailboxItemId" graphapi:"nullable"`
	FinanceTransactionID string `json:"financeTransactionId" graphapi:"nullable"`
}

// ReceiptReadingView is the receipt job queued to read it, whose run says
// what it read and matched.
type ReceiptReadingView struct {
	AgentJobID string `json:"agentJobId"`
}

// receiptError is a refusal about a receipt or an annotation as the API
// says it.
func receiptError(err error) error {
	if errors.Is(err, finance.ErrReceiptRefused) || errors.Is(err, agent.ErrAnnotationIsThePersons) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return financeError(err)
}

// annotatedByCaller is who is writing: the agent when it is the agent's
// operations calling, else the person.
func annotatedByCaller(ctx context.Context) models.AnnotatedBy {
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent {
		return models.AnnotatedByAgent
	}
	return models.AnnotatedByPerson
}

func (self *graph) AnnotateTransaction(ctx context.Context, arguments AnnotateTransactionArguments) (*models.FinanceTransaction, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	if _, err := ownFinanceTransaction(self.transaction(ctx), found.ID, arguments.FinanceTransactionID); err != nil {
		return nil, err
	}
	annotatedBy := annotatedByCaller(ctx)
	// Only the agent's own calls carry the flag that matters; the person
	// replaces what they like with or without it.
	canReplacePersonAnnotation := annotatedBy == models.AnnotatedByAgent && arguments.IsAskedByPerson != nil && *arguments.IsAskedByPerson
	annotated, err := worker.AnnotateTransaction(ctx, found, arguments.FinanceTransactionID, arguments.Annotation, annotatedBy, canReplacePersonAnnotation)
	if err != nil {
		return nil, receiptError(err)
	}
	return annotated, nil
}

// receiptOf is the receipt the arguments describe, its source settled: a
// message in a mailbox the caller may read becomes its stored message, an
// upload must be the caller's.
func (self *graph) receiptOf(ctx context.Context, found *models.Agent, arguments RecordReceiptArguments) (*models.FinanceReceipt, error) {
	receipt := &models.FinanceReceipt{
		AgentID: found.ID, MerchantName: arguments.MerchantName, MerchantReceiptNumber: arguments.MerchantReceiptNumber,
		CurrencyCode: arguments.CurrencyCode, SubtotalAmount: arguments.SubtotalAmount, TotalAmount: arguments.TotalAmount,
		PaymentAccountMask: strings.TrimSpace(arguments.PaymentAccountMask),
	}
	var err error
	if receipt.PurchasedOn, err = dayArgument("purchasedOn", arguments.PurchasedOn, ""); err != nil {
		return nil, err
	}
	if purchasedAt := strings.TrimSpace(arguments.PurchasedAt); purchasedAt != "" {
		parsed, err := time.Parse(time.RFC3339, purchasedAt)
		if err != nil {
			return nil, fmt.Errorf("%w: purchasedAt %q is not a time written 2006-01-02T15:04:05-07:00", api.ErrInvalidArguments, purchasedAt)
		}
		receipt.PurchasedAt = &parsed
	}
	mailboxItemId, gmailMessageId, attachmentId := strings.TrimSpace(arguments.MailboxItemID), strings.TrimSpace(arguments.GmailMessageID), strings.TrimSpace(arguments.AgentAttachmentID)
	sourceCount := 0
	for _, sourceId := range []string{mailboxItemId, gmailMessageId, attachmentId} {
		if sourceId != "" {
			sourceCount++
		}
	}
	if sourceCount != 1 {
		return nil, fmt.Errorf("%w: give where the receipt was read from, one of mailboxItemId, gmailMessageId and agentAttachmentId", api.ErrInvalidArguments)
	}
	switch {
	case mailboxItemId != "":
		items, _, err := self.requireItems(ctx, models.PermissionMailRead, []string{mailboxItemId})
		if err != nil {
			return nil, err
		}
		receipt.ReceiptSourceKind, receipt.MailID, receipt.MailboxItemID = models.ReceiptSourceKindMail, items[0].MailID, items[0].ID
	case gmailMessageId != "":
		receipt.ReceiptSourceKind, receipt.GmailMessageID = models.ReceiptSourceKindGmailMessage, gmailMessageId
	default:
		attachment, err := self.transaction(ctx).GetAgentAttachment(attachmentId)
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != found.ID {
			return nil, api.ErrNotFound
		}
		receipt.ReceiptSourceKind, receipt.AgentAttachmentID = models.ReceiptSourceKindAttachment, attachment.ID
	}
	for index, line := range arguments.ReceiptLines {
		lineNumber := index + 1
		if line.LineNumber != nil {
			lineNumber = *line.LineNumber
		}
		discountedLineNumber := 0
		if line.DiscountedLineNumber != nil {
			discountedLineNumber = *line.DiscountedLineNumber
		}
		receipt.ReceiptLines = append(receipt.ReceiptLines, &models.FinanceReceiptLine{
			LineNumber: lineNumber, ReceiptLineKind: models.ReceiptLineKind(strings.ToLower(strings.TrimSpace(line.ReceiptLineKind))),
			Description: line.Description, Quantity: line.Quantity, QuantityUnit: line.QuantityUnit, UnitPriceAmount: line.UnitPriceAmount,
			LineAmount: line.LineAmount, TaxClassCode: line.TaxClassCode, DiscountedLineNumber: discountedLineNumber,
		})
	}
	return receipt, nil
}

// receiptMatchCandidateViews are candidates with their finance
// transactions.
func receiptMatchCandidateViews(tx db.Transaction, agentId string, candidates []finance.ReceiptMatchCandidate) ([]*ReceiptMatchCandidateView, error) {
	views := make([]*ReceiptMatchCandidateView, 0, len(candidates))
	if len(candidates) == 0 {
		return views, nil
	}
	financeTransactionIds := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		financeTransactionIds = append(financeTransactionIds, candidate.FinanceTransactionID)
	}
	financeTransactions, err := tx.GetFinanceTransactions(agentId, financeTransactionIds)
	if err != nil {
		return nil, err
	}
	byId := map[string]*models.FinanceTransaction{}
	for _, financeTransaction := range financeTransactions {
		byId[financeTransaction.ID] = financeTransaction
	}
	for _, candidate := range candidates {
		views = append(views, &ReceiptMatchCandidateView{
			FinanceTransactionID: candidate.FinanceTransactionID, FinanceTransaction: byId[candidate.FinanceTransactionID],
			MatchedAmount: candidate.MatchedAmount, IsExactAmount: candidate.IsExactAmount, IsSameAccount: candidate.IsSameAccount,
			IsMerchantNameShared: candidate.IsMerchantNameShared, DayDistanceCount: candidate.DayDistanceCount,
			IsAutomatic: candidate.IsAutomatic, MatchConfidence: candidate.MatchConfidence,
		})
	}
	return views, nil
}

// placeReceipts fills in where each receipt's message is now in the
// caller's mailboxes, for opening it: the first mailbox item of its stored
// message in a mailbox the caller owns, when they may read mail; empty
// when there is none. One statement for every receipt of a page.
func (self *graph) placeReceipts(ctx context.Context, receipts ...*models.FinanceReceipt) error {
	mailIds := []string{}
	for _, receipt := range receipts {
		if receipt == nil || receipt.MailID == "" {
			continue
		}
		receipt.MailboxItemID = ""
		mailIds = append(mailIds, receipt.MailID)
	}
	if len(mailIds) == 0 {
		return nil
	}
	principal, err := self.requirePermission(ctx, models.PermissionMailRead)
	if err != nil || principal.User == nil {
		return nil
	}
	itemIdByMailId, err := self.transaction(ctx).FirstOwnedItemIDsByMail(principal.User.ID, mailIds)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if receipt != nil && receipt.MailID != "" {
			receipt.MailboxItemID = itemIdByMailId[receipt.MailID]
		}
	}
	return nil
}

func (self *graph) PreviewRecordReceipt(ctx context.Context, arguments RecordReceiptArguments) (*ReceiptPreviewView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	receipt, err := self.receiptOf(ctx, found, arguments)
	if err != nil {
		return nil, err
	}
	preview, err := worker.PreviewReceipt(ctx, found, receipt)
	if err != nil {
		return nil, receiptError(err)
	}
	candidates, err := receiptMatchCandidateViews(self.transaction(ctx), found.ID, preview.ReceiptMatchCandidates)
	if err != nil {
		return nil, err
	}
	return &ReceiptPreviewView{
		ReceiptCheckState: preview.ReceiptCheckState, CheckDifferenceAmount: preview.CheckDifferenceAmount,
		ReceiptCheckSummary:    finance.ReceiptCheckSummary(preview.ReceiptCheckState, preview.CheckDifferenceAmount, receipt.CurrencyCode),
		ReceiptMatchCandidates: candidates, IsReplacing: preview.IsReplacing,
	}, nil
}

func (self *graph) RecordReceipt(ctx context.Context, arguments RecordReceiptArguments) (*RecordedReceiptView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	receipt, err := self.receiptOf(ctx, found, arguments)
	if err != nil {
		return nil, err
	}
	recording := agent.ReceiptRecording{
		IsUnbalancedAccepted: arguments.IsUnbalancedAccepted != nil && *arguments.IsUnbalancedAccepted,
		FinanceTransactionID: strings.TrimSpace(arguments.FinanceTransactionID),
	}
	if recording.FinanceTransactionID != "" {
		if _, err := ownFinanceTransaction(self.transaction(ctx), found.ID, recording.FinanceTransactionID); err != nil {
			return nil, err
		}
	}
	recorded, err := worker.RecordReceipt(ctx, found, receipt, recording)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := self.placeReceipts(ctx, recorded.FinanceReceipt); err != nil {
		return nil, err
	}
	candidates, err := receiptMatchCandidateViews(self.transaction(ctx), found.ID, recorded.ReceiptMatchCandidates)
	if err != nil {
		return nil, err
	}
	stored := recorded.FinanceReceipt
	return &RecordedReceiptView{
		FinanceReceipt:         stored,
		ReceiptCheckSummary:    finance.ReceiptCheckSummary(stored.ReceiptCheckState, finance.FormatReceiptAmount(stored.CheckDifferenceAmount, stored.CurrencyCode), stored.CurrencyCode),
		ReceiptMatchCandidates: candidates, IsReplaced: recorded.IsReplaced,
		DroppedReceiptMatchReasons: recorded.DroppedReceiptMatchReasons,
	}, nil
}

func (self *graph) FinanceReceipt(ctx context.Context, arguments ReceiptArguments) (*models.FinanceReceipt, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	receipt, err := self.transaction(ctx).GetFinanceReceipt(found.ID, strings.TrimSpace(arguments.ReceiptID))
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, api.ErrNotFound
	}
	if err := self.placeReceipts(ctx, receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (self *graph) FinanceReceipts(ctx context.Context, arguments FinanceReceiptsArguments) (*FinanceReceiptPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	filter := &db.FinanceReceiptFilter{
		FinanceTransactionID: strings.TrimSpace(arguments.FinanceTransactionID),
		IsUndated:            arguments.IsUndated != nil && *arguments.IsUndated,
		IsUnmatched:          arguments.IsUnmatched != nil && *arguments.IsUnmatched,
		After:                strings.TrimSpace(arguments.After),
	}
	if filter.From, err = dayArgument("from", arguments.From, ""); err != nil {
		return nil, err
	}
	if filter.To, err = dayArgument("to", arguments.To, ""); err != nil {
		return nil, err
	}
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if filter.Offset, err = offsetArgument(arguments.Offset); err != nil {
		return nil, err
	}
	filter.ShouldCountTotal = true
	page, err := self.transaction(ctx).ListFinanceReceipts(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	receipts := page.FinanceReceipts
	if receipts == nil {
		receipts = []*models.FinanceReceipt{}
	}
	if err := self.placeReceipts(ctx, receipts...); err != nil {
		return nil, err
	}
	return &FinanceReceiptPageView{FinanceReceipts: receipts, NextCursor: page.NextCursor, TotalCount: page.TotalCount}, nil
}

func (self *graph) ProposeReceiptMatches(ctx context.Context, arguments ReceiptArguments) ([]*ReceiptMatchCandidateView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	candidates, err := worker.ProposeReceiptMatches(ctx, found, arguments.ReceiptID)
	if err != nil {
		return nil, receiptError(err)
	}
	return receiptMatchCandidateViews(self.transaction(ctx), found.ID, candidates)
}

func (self *graph) MatchReceipt(ctx context.Context, arguments MatchReceiptArguments) (*models.FinanceReceipt, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	if _, err := ownFinanceTransaction(self.transaction(ctx), found.ID, arguments.FinanceTransactionID); err != nil {
		return nil, err
	}
	matched, err := worker.MatchReceipt(ctx, found, arguments.ReceiptID, arguments.FinanceTransactionID, arguments.MatchedAmount)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := self.placeReceipts(ctx, matched); err != nil {
		return nil, err
	}
	return matched, nil
}

func (self *graph) UnmatchReceipt(ctx context.Context, arguments UnmatchReceiptArguments) (*models.FinanceReceipt, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	unmatched, err := worker.UnmatchReceipt(ctx, found, arguments.ReceiptID, arguments.FinanceTransactionID)
	if err != nil {
		return nil, receiptError(err)
	}
	if err := self.placeReceipts(ctx, unmatched); err != nil {
		return nil, err
	}
	return unmatched, nil
}

func (self *graph) ReadReceipt(ctx context.Context, arguments ReadReceiptArguments) (*ReceiptReadingView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	attachmentId, itemId := strings.TrimSpace(arguments.AgentAttachmentID), strings.TrimSpace(arguments.MailboxItemID)
	if (attachmentId == "") == (itemId == "") {
		return nil, fmt.Errorf("%w: give the uploaded photo (agentAttachmentId) or the message (mailboxItemId) the receipt is in, one of the two", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	financeTransactionId := strings.TrimSpace(arguments.FinanceTransactionID)
	if financeTransactionId != "" && itemId != "" {
		return nil, fmt.Errorf("%w: financeTransactionId goes with an upload; a message's receipt is matched once it is read, by the matcher or with MatchReceipt", api.ErrInvalidArguments)
	}
	if financeTransactionId != "" {
		financeTransaction, err := ownFinanceTransaction(tx, found.ID, financeTransactionId)
		if err != nil {
			return nil, err
		}
		// Refused now rather than read and left unmatched: a receipt
		// explains only money out. The receipt's currency is known only
		// once it is read, so a charge in another currency is the job's
		// to find, and it keeps the receipt unmatched, saying why.
		if chargedAmount, err := finance.ParseAmount(financeTransaction.Amount); err != nil || chargedAmount.Sign() >= 0 {
			return nil, fmt.Errorf("%w: that transaction is money in, not a charge, so no receipt explains it; upload the receipt to the charge it paid for", api.ErrInvalidArguments)
		}
	}
	mailboxId, sourceId := "", attachmentId
	if attachmentId != "" {
		attachment, err := tx.GetAgentAttachment(attachmentId)
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != found.ID {
			return nil, api.ErrNotFound
		}
		// Refused now rather than queued to fail: the server reads a
		// picture and a file with text in it, not a PDF.
		if !agent.IsImageAttachment(attachment.ContentType) && strings.TrimSpace(attachment.Text) == "" {
			return nil, fmt.Errorf("%w: %s has no text the server can read; a PDF's text is not read here, so upload a photo or a screenshot of the receipt", api.ErrInvalidArguments, attachment.Name)
		}
	} else {
		items, mailbox, err := self.requireItems(ctx, models.PermissionMailRead, []string{itemId})
		if err != nil {
			return nil, err
		}
		if mailbox.Agent == nil || !mailbox.Agent.Granted {
			return nil, fmt.Errorf("%w: the message is in a mailbox your agent is not granted; grant it the mailbox first", api.ErrInvalidArguments)
		}
		mailboxId, sourceId = mailbox.ID, items[0].MailID
	}
	job, err := worker.QueueReceiptReading(tx, found.ID, mailboxId, sourceId, financeTransactionId)
	if err != nil {
		return nil, financeError(err)
	}
	return &ReceiptReadingView{AgentJobID: job.ID}, nil
}

func (self *graph) DeleteReceipt(ctx context.Context, arguments ReceiptArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	if _, err := worker.DeleteReceipt(ctx, found, arguments.ReceiptID); err != nil {
		return false, receiptError(err)
	}
	return true, nil
}
