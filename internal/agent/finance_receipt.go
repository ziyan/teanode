package agent

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Annotations and receipts on finance transactions: what the person or
// their agent says about a charge, and the merchant's own record of what
// it bought, line by line. These are the operations the dashboard, the
// command line, the finance tool and the receipt job all go through, so a
// receipt is checked and matched the same way whoever recorded it.

// ErrAnnotationIsThePersons refuses the agent changing what the person
// wrote on a finance transaction.
var ErrAnnotationIsThePersons = errors.New("the person wrote this annotation, and the agent changes or removes it only when the person asks in a conversation they are there for")

// ErrReceiptUnbalanced refuses recording a receipt whose lines do not add
// up to its printed totals, unless the person says to record it anyway.
var ErrReceiptUnbalanced = errors.New("its lines do not add up to its printed totals")

// AnnotateTransaction writes what the person or their agent says about a
// finance transaction, empty to take it away, and answers the finance
// transaction as it is now. The agent overwrites or removes the person's
// only when canReplacePersonAnnotation, which is the person asking for it
// in a conversation they are there for; never in a run with nobody
// present (ErrAnnotationIsThePersons). The person always may. What the
// agent writes is the agent's either way.
func (self *Agent) AnnotateTransaction(ctx context.Context, agentRow *models.Agent, financeTransactionId, annotation string, annotatedBy models.AnnotatedBy, canReplacePersonAnnotation bool) (*models.FinanceTransaction, error) {
	var annotated *models.FinanceTransaction
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		isWritten, err := tx.SetFinanceTransactionAnnotation(agentRow.ID, strings.TrimSpace(financeTransactionId), annotation, annotatedBy, canReplacePersonAnnotation)
		if err != nil {
			return err
		}
		if !isWritten {
			return ErrAnnotationIsThePersons
		}
		annotated, err = tx.GetFinanceTransaction(agentRow.ID, strings.TrimSpace(financeTransactionId))
		return err
	})
	return annotated, err
}

// ReceiptRecording is how a receipt is recorded.
type ReceiptRecording struct {
	// IsUnbalancedAccepted records a receipt whose lines do not add up,
	// marked unbalanced; without it such a receipt is refused. Only the
	// person decides it, or the receipt job, which has nobody to ask and
	// leaves the mark for the person to see.
	IsUnbalancedAccepted bool

	// FinanceTransactionID is the charge the receipt was handed in for, a
	// photo uploaded to it say: matched by hand, as the person's, instead
	// of by the matcher.
	FinanceTransactionID string

	// IsRecordedWhenHandMatchRefused records the receipt, unmatched with
	// its candidates, when the match to FinanceTransactionID is refused
	// (other receipts already explain the charge, another currency, money
	// in), saying why in HandMatchRefusalReason. Without it the whole
	// receipt is refused, which is right for a person who can fix the
	// call, and wrong for the receipt job, which would lose the upload's
	// reading for good.
	IsRecordedWhenHandMatchRefused bool
}

// RecordedReceipt is what recording a receipt did: the receipt as stored,
// with its lines and matches; the charges it could explain when the
// matcher left them for the person; and whether it took the place of the
// receipt read from the same source before.
type RecordedReceipt struct {
	FinanceReceipt         *models.FinanceReceipt
	ReceiptMatchCandidates []finance.ReceiptMatchCandidate
	IsReplaced             bool

	// HandMatchRefusalReason says why the receipt was not matched to the
	// charge the recording named, when IsRecordedWhenHandMatchRefused let
	// it be recorded anyway; "" otherwise.
	HandMatchRefusalReason string

	// DroppedReceiptMatchReasons say, one sentence each, which of the
	// person's matches were taken off because the receipt as read again no
	// longer fits them: a lower total, another currency.
	DroppedReceiptMatchReasons []string
}

// ReceiptPreview is what recording a receipt would do, writing nothing.
type ReceiptPreview struct {
	ReceiptCheckState      models.ReceiptCheckState
	CheckDifferenceAmount  string
	ReceiptMatchCandidates []finance.ReceiptMatchCandidate
	IsReplacing            bool
}

// receiptUnbalancedError is the refusal of an unbalanced receipt, saying by
// how much and what to do.
func receiptUnbalancedError(receipt *models.FinanceReceipt, checkDifferenceAmount string) error {
	return fmt.Errorf("%w: %w: %s; read the receipt again, "+
		"and record it as it is only when the person says to", finance.ErrReceiptRefused, ErrReceiptUnbalanced,
		finance.ReceiptCheckDifferenceWords(checkDifferenceAmount, receipt.CurrencyCode))
}

// receiptSourceIdOf is the id of the source a receipt's kind names.
func receiptSourceIdOf(receipt *models.FinanceReceipt) string {
	switch receipt.ReceiptSourceKind {
	case models.ReceiptSourceKindMail:
		return receipt.MailID
	case models.ReceiptSourceKindGmailMessage:
		return receipt.GmailMessageID
	case models.ReceiptSourceKindAttachment:
		return receipt.AgentAttachmentID
	}
	return ""
}

// PreviewReceipt checks a receipt and finds the charges it could explain,
// writing nothing: what RecordReceipt would do with it.
func (self *Agent) PreviewReceipt(ctx context.Context, agentRow *models.Agent, receipt *models.FinanceReceipt) (*ReceiptPreview, error) {
	receiptCheckState, checkDifferenceAmount, err := finance.CheckReceipt(receipt)
	if err != nil {
		return nil, err
	}
	preview := &ReceiptPreview{ReceiptCheckState: receiptCheckState, CheckDifferenceAmount: checkDifferenceAmount}
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if receipt.ReceiptSourceKind.IsValid() {
			existing, err := tx.FindFinanceReceiptBySource(agentRow.ID, receipt.ReceiptSourceKind, receiptSourceIdOf(receipt))
			if err != nil {
				return err
			}
			preview.IsReplacing = existing != nil
		}
		preview.ReceiptMatchCandidates, err = receiptMatchCandidates(tx, agentRow.ID, receipt)
		return err
	})
	return preview, err
}

// RecordReceipt checks a receipt, stores it in place of any read from the
// same source before, and matches it: to the charge the recording names,
// as the person's; else, when the person has not matched it, to the one
// charge the matcher is sure of. The matcher's earlier matches are taken
// off first; the person's are kept while they still fit the receipt as
// read now, and the ones that do not are taken off and said in
// DroppedReceiptMatchReasons. A receipt that does not balance is refused
// unless the recording accepts it.
func (self *Agent) RecordReceipt(ctx context.Context, agentRow *models.Agent, receipt *models.FinanceReceipt, recording ReceiptRecording) (*RecordedReceipt, error) {
	receiptCheckState, checkDifferenceAmount, err := finance.CheckReceipt(receipt)
	if err != nil {
		return nil, err
	}
	if receiptCheckState == models.ReceiptCheckStateUnbalanced && !recording.IsUnbalancedAccepted {
		return nil, receiptUnbalancedError(receipt, checkDifferenceAmount)
	}
	receipt.AgentID = agentRow.ID
	receipt.ReceiptCheckState, receipt.CheckDifferenceAmount = receiptCheckState, checkDifferenceAmount
	recorded := &RecordedReceipt{ReceiptMatchCandidates: []finance.ReceiptMatchCandidate{}, DroppedReceiptMatchReasons: []string{}}
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.FindFinanceReceiptBySource(agentRow.ID, receipt.ReceiptSourceKind, receiptSourceIdOf(receipt))
		if err != nil {
			return err
		}
		recorded.IsReplaced = existing != nil
		// A dropped match is in the currency the receipt was read in before.
		previousCurrencyCode := receipt.CurrencyCode
		if existing != nil {
			previousCurrencyCode = existing.CurrencyCode
		}
		stored, err := tx.PutFinanceReceipt(receipt)
		if err != nil {
			return err
		}
		for _, match := range stored.ReceiptMatches {
			if match.ReceiptMatchSource != models.ReceiptMatchSourcePerson {
				if _, err := tx.DeleteFinanceReceiptMatch(agentRow.ID, stored.ID, match.FinanceTransactionID, models.ReceiptMatchSourceReceiptMatcher); err != nil {
					return err
				}
			}
		}
		// The person matched the receipt as it read before; one that the
		// new reading cannot explain (a lower total, another currency) is
		// taken off and said, rather than kept where no match could be
		// written now, or the whole reading refused.
		dropped, err := tx.DropUnfittingFinanceReceiptMatches(agentRow.ID, stored.ID)
		if err != nil {
			return err
		}
		for _, droppedMatch := range dropped {
			recorded.DroppedReceiptMatchReasons = append(recorded.DroppedReceiptMatchReasons, fmt.Sprintf(
				"the person's match to charge %s for %s %s was taken off, since %s", droppedMatch.ReceiptMatch.FinanceTransactionID,
				finance.FormatReceiptAmount(droppedMatch.ReceiptMatch.MatchedAmount, previousCurrencyCode), previousCurrencyCode,
				strings.TrimSuffix(droppedMatch.DropReason, ".")))
		}
		// What the candidates are weighed against is what is left of the
		// receipt once the matcher's earlier matches and the person's that
		// no longer fit are gone.
		if stored, err = tx.GetFinanceReceipt(agentRow.ID, stored.ID); err != nil {
			return err
		}
		personMatches := stored.ReceiptMatches
		if strings.TrimSpace(recording.FinanceTransactionID) != "" {
			err := matchReceiptByHand(tx, agentRow.ID, stored, recording.FinanceTransactionID, "")
			switch {
			case err == nil:
				recorded.FinanceReceipt, err = tx.GetFinanceReceipt(agentRow.ID, stored.ID)
				return err
			case !recording.IsRecordedWhenHandMatchRefused || !isReceiptMatchRefusal(err):
				return err
			}
			// Refused before anything was written, so the transaction goes
			// on: the receipt is kept, unmatched, with its candidates.
			recorded.HandMatchRefusalReason = receiptMatchRefusalReason(err)
		}
		if len(personMatches) == 0 {
			candidates, err := receiptMatchCandidates(tx, agentRow.ID, stored)
			if err != nil {
				return err
			}
			isAutomaticallyMatched := false
			for index, candidate := range candidates {
				if !candidate.IsAutomatic || recorded.HandMatchRefusalReason != "" {
					candidates[index].IsAutomatic = false
					continue
				}
				isWritten, err := tx.PutFinanceReceiptMatch(agentRow.ID, &models.FinanceReceiptMatch{
					ReceiptID: stored.ID, FinanceTransactionID: candidate.FinanceTransactionID, MatchedAmount: candidate.MatchedAmount,
					ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: candidate.MatchConfidence,
				})
				switch {
				case err == nil && isWritten:
					isAutomaticallyMatched = true
				case err == nil:
					// The person matched the receipt to the charge in between,
					// and the matcher never replaces that.
					candidates[index].IsAutomatic = false
				case isReceiptMatchRefusal(err):
					// Another receipt was matched to the charge after the
					// candidates were read (two receipt jobs at once): the
					// receipt is kept, left to the person.
					candidates[index].IsAutomatic = false
				default:
					return err
				}
			}
			if !isAutomaticallyMatched {
				recorded.ReceiptMatchCandidates = candidates
			}
		}
		recorded.FinanceReceipt, err = tx.GetFinanceReceipt(agentRow.ID, stored.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return recorded, nil
}

// isReceiptMatchRefusal says a match was refused as untrue of the charge
// or the receipt, or its charge is gone: refused before it wrote anything,
// so the transaction it was tried in can go on.
func isReceiptMatchRefusal(err error) bool {
	return errors.Is(err, db.ErrInvalidArguments) || errors.Is(err, db.ErrNotFound)
}

// receiptMatchRefusalReason says in words why a match was refused.
func receiptMatchRefusalReason(err error) string {
	if errors.Is(err, db.ErrNotFound) {
		return "that charge is no longer there"
	}
	return strings.TrimPrefix(err.Error(), db.ErrInvalidArguments.Error()+": ")
}

// matchReceiptByHand matches a receipt to a charge as the person's, with
// the amount given, or else the less of what other receipts leave
// unexplained of the charge and what the receipt's matches to its other
// charges leave of its total. The database refuses an amount that does
// not fit the charge or the receipt.
func matchReceiptByHand(tx db.Transaction, agentId string, receipt *models.FinanceReceipt, financeTransactionId, matchedAmount string) error {
	financeTransaction, err := tx.GetFinanceTransaction(agentId, strings.TrimSpace(financeTransactionId))
	if err != nil {
		return err
	}
	if financeTransaction == nil {
		return db.ErrNotFound
	}
	if strings.TrimSpace(matchedAmount) == "" {
		matchedAmount = receipt.TotalAmount
		charged, chargedErr := finance.ParseAmount(financeTransaction.Amount)
		total, totalErr := finance.ParseAmount(receipt.TotalAmount)
		if totalErr == nil && total.Sign() <= 0 {
			// A return or a void prints a total of nothing or less, which
			// explains no money out; said as that, not as matches elsewhere.
			return fmt.Errorf("%w: the receipt's total is %s, which explains no charge; give the amount this one explains",
				db.ErrInvalidArguments, receipt.TotalAmount)
		}
		if totalErr == nil {
			for _, match := range receipt.ReceiptMatches {
				if match.FinanceTransactionID == financeTransaction.ID {
					continue
				}
				if elsewhere, err := finance.ParseAmount(match.MatchedAmount); err == nil {
					total.Sub(total, elsewhere)
				}
			}
			if total.Sign() <= 0 {
				return fmt.Errorf("%w: the receipt's matches to other charges already explain all of its total; take one off first, or give the amount this one explains",
					db.ErrInvalidArguments)
			}
			matchedAmount = finance.FormatAmount(total)
		}
		if chargedErr == nil && totalErr == nil && charged.Sign() < 0 {
			unexplained := new(big.Rat).Abs(charged)
			coverageById, err := tx.FinanceReceiptMatchCoverage(agentId, receipt.ID, []string{financeTransaction.ID})
			if err != nil {
				return err
			}
			if coverage := coverageById[financeTransaction.ID]; coverage != nil {
				if other, err := finance.ParseAmount(coverage.OtherMatchedAmount); err == nil {
					unexplained.Sub(unexplained, other)
				}
			}
			if unexplained.Sign() <= 0 {
				return fmt.Errorf("%w: other receipts already explain all of that charge; take one off it first, or give the amount this one explains",
					db.ErrInvalidArguments)
			}
			if unexplained.Cmp(total) < 0 {
				matchedAmount = finance.FormatAmount(unexplained)
			}
		}
	}
	_, err = tx.PutFinanceReceiptMatch(agentId, &models.FinanceReceiptMatch{
		ReceiptID: receipt.ID, FinanceTransactionID: financeTransaction.ID, MatchedAmount: matchedAmount,
		ReceiptMatchSource: models.ReceiptMatchSourcePerson,
	})
	return err
}

// receiptMatchCandidates are the charges a receipt could explain among
// the agent's finance transactions posted around its day of purchase,
// leaving out those its own and other receipts already explain in full,
// and never automatic for one another receipt is matched to.
func receiptMatchCandidates(tx db.Transaction, agentId string, receipt *models.FinanceReceipt) ([]finance.ReceiptMatchCandidate, error) {
	purchasedOn, err := time.Parse(time.DateOnly, strings.TrimSpace(receipt.PurchasedOn))
	if err != nil {
		return []finance.ReceiptMatchCandidate{}, nil
	}
	// Every page of the window, since "the only exact amount" is only
	// true of the whole of it: a busy account can post more charges in
	// those days than one page holds.
	var financeTransactions []*models.FinanceTransaction
	filter := &db.FinanceTransactionFilter{
		From: purchasedOn.AddDate(0, 0, -finance.ReceiptMatchDaysBefore).Format(time.DateOnly),
		To:   purchasedOn.AddDate(0, 0, finance.ReceiptMatchDaysAfter).Format(time.DateOnly),
		// Money out only: a charge is negative.
		MaximumAmount: "0", IsDuplicateExcluded: true, Limit: db.FinanceTransactionLimitMost,
	}
	for {
		page, err := tx.ListFinanceTransactions(agentId, filter)
		if err != nil {
			return nil, err
		}
		financeTransactions = append(financeTransactions, page.FinanceTransactions...)
		if page.NextCursor == "" {
			break
		}
		filter.After = page.NextCursor
	}
	accounts, err := tx.ListFinanceAccounts(agentId, "")
	if err != nil {
		return nil, err
	}
	accountMaskByFinanceAccountId := map[string]string{}
	for _, account := range accounts {
		accountMaskByFinanceAccountId[account.ID] = account.AccountMask
	}
	financeTransactionIds := make([]string, 0, len(financeTransactions))
	for _, financeTransaction := range financeTransactions {
		financeTransactionIds = append(financeTransactionIds, financeTransaction.ID)
	}
	coverageByFinanceTransactionId, err := tx.FinanceReceiptMatchCoverage(agentId, receipt.ID, financeTransactionIds)
	if err != nil {
		return nil, err
	}
	return finance.ProposeReceiptMatches(receipt, financeTransactions, accountMaskByFinanceAccountId, coverageByFinanceTransactionId), nil
}

// matchWaitingReceipts matches the receipts recorded before their charge
// arrived, when a sync or a statement import brings it: each receipt
// matched to nothing whose day of purchase the new charges could be of is
// weighed again by the same rule as when it was recorded, over every
// charge of its window. Only a charge among insertedFinanceTransactionIds
// is matched, so a match the person took off is never put back; anything
// less sure stays with the person. It answers how many it matched.
func matchWaitingReceipts(tx db.Transaction, agentId string, insertedFinanceTransactionIds []string) (int, error) {
	if len(insertedFinanceTransactionIds) == 0 {
		return 0, nil
	}
	waiting, err := tx.ListFinanceReceipts(agentId, &db.FinanceReceiptFilter{IsUnmatched: true, Limit: 1})
	if err != nil || len(waiting.FinanceReceipts) == 0 {
		return 0, err
	}
	isInserted := map[string]bool{}
	var earliestPostedOn, latestPostedOn string
	for _, financeTransactionId := range insertedFinanceTransactionIds {
		financeTransaction, err := tx.GetFinanceTransaction(agentId, financeTransactionId)
		if err != nil {
			return 0, err
		}
		if financeTransaction == nil || financeTransaction.PostedOn == "" {
			continue
		}
		isInserted[financeTransaction.ID] = true
		if earliestPostedOn == "" || financeTransaction.PostedOn < earliestPostedOn {
			earliestPostedOn = financeTransaction.PostedOn
		}
		if latestPostedOn == "" || financeTransaction.PostedOn > latestPostedOn {
			latestPostedOn = financeTransaction.PostedOn
		}
	}
	if len(isInserted) == 0 {
		return 0, nil
	}
	earliest, err := time.Parse(time.DateOnly, earliestPostedOn)
	if err != nil {
		return 0, err
	}
	latest, err := time.Parse(time.DateOnly, latestPostedOn)
	if err != nil {
		return 0, err
	}
	// A charge is a candidate from ReceiptMatchDaysBefore days before the
	// day of purchase to ReceiptMatchDaysAfter after, so these are the
	// days of purchase the new charges can explain.
	filter := &db.FinanceReceiptFilter{
		IsUnmatched: true, Limit: db.FinanceReceiptLimitMost,
		From: earliest.AddDate(0, 0, -finance.ReceiptMatchDaysAfter).Format(time.DateOnly),
		To:   latest.AddDate(0, 0, finance.ReceiptMatchDaysBefore).Format(time.DateOnly),
	}
	var receipts []*models.FinanceReceipt
	for {
		page, err := tx.ListFinanceReceipts(agentId, filter)
		if err != nil {
			return 0, err
		}
		receipts = append(receipts, page.FinanceReceipts...)
		if page.NextCursor == "" {
			break
		}
		filter.After = page.NextCursor
	}
	matchedCount := 0
	for _, receipt := range receipts {
		candidates, err := receiptMatchCandidates(tx, agentId, receipt)
		if err != nil {
			return matchedCount, err
		}
		for _, candidate := range candidates {
			if !candidate.IsAutomatic || !isInserted[candidate.FinanceTransactionID] {
				continue
			}
			isWritten, err := tx.PutFinanceReceiptMatch(agentId, &models.FinanceReceiptMatch{
				ReceiptID: receipt.ID, FinanceTransactionID: candidate.FinanceTransactionID, MatchedAmount: candidate.MatchedAmount,
				ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: candidate.MatchConfidence,
			})
			switch {
			case err == nil && isWritten:
				matchedCount++
			case err == nil, isReceiptMatchRefusal(err):
				// The person matched it in between, or another receipt took
				// the charge: theirs to judge.
			default:
				return matchedCount, err
			}
		}
	}
	return matchedCount, nil
}

// ProposeReceiptMatches is the charges a stored receipt could explain, the
// likeliest first, writing nothing.
func (self *Agent) ProposeReceiptMatches(ctx context.Context, agentRow *models.Agent, receiptId string) ([]finance.ReceiptMatchCandidate, error) {
	var candidates []finance.ReceiptMatchCandidate
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		receipt, err := tx.GetFinanceReceipt(agentRow.ID, strings.TrimSpace(receiptId))
		if err != nil {
			return err
		}
		if receipt == nil {
			return db.ErrNotFound
		}
		candidates, err = receiptMatchCandidates(tx, agentRow.ID, receipt)
		return err
	})
	return candidates, err
}

// MatchReceipt matches a receipt to a finance transaction by hand, as the
// person's, which the matcher never removes: with the amount of the
// charge the receipt explains, or when that is empty, the charge's amount
// or the receipt's total, whichever is less. Matching it again changes
// the amount. One receipt may be matched to several charges (an order
// shipped in parts) and one charge to several receipts.
func (self *Agent) MatchReceipt(ctx context.Context, agentRow *models.Agent, receiptId, financeTransactionId, matchedAmount string) (*models.FinanceReceipt, error) {
	var matched *models.FinanceReceipt
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		receipt, err := tx.GetFinanceReceipt(agentRow.ID, strings.TrimSpace(receiptId))
		if err != nil {
			return err
		}
		if receipt == nil {
			return db.ErrNotFound
		}
		if strings.TrimSpace(matchedAmount) != "" {
			parsedMatchedAmount, err := finance.ParseAmount(matchedAmount)
			if err != nil || parsedMatchedAmount.Cmp(new(big.Rat)) <= 0 {
				return fmt.Errorf("%w: the matched amount %q is not an amount above zero", db.ErrInvalidArguments, matchedAmount)
			}
		}
		if err := matchReceiptByHand(tx, agentRow.ID, receipt, financeTransactionId, matchedAmount); err != nil {
			return err
		}
		matched, err = tx.GetFinanceReceipt(agentRow.ID, receipt.ID)
		return err
	})
	return matched, err
}

// UnmatchReceipt takes a receipt off a finance transaction, whoever matched
// them. db.ErrNotFound when they were not matched.
func (self *Agent) UnmatchReceipt(ctx context.Context, agentRow *models.Agent, receiptId, financeTransactionId string) (*models.FinanceReceipt, error) {
	var unmatched *models.FinanceReceipt
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		isRemoved, err := tx.DeleteFinanceReceiptMatch(agentRow.ID, strings.TrimSpace(receiptId), strings.TrimSpace(financeTransactionId), "")
		if err != nil {
			return err
		}
		if !isRemoved {
			return db.ErrNotFound
		}
		unmatched, err = tx.GetFinanceReceipt(agentRow.ID, strings.TrimSpace(receiptId))
		return err
	})
	return unmatched, err
}

// DeleteReceipt deletes a receipt with its lines and matches, and the
// photo or PDF it was read from unless a conversation holds the same file.
// It answers the receipt as it was.
func (self *Agent) DeleteReceipt(ctx context.Context, agentRow *models.Agent, receiptId string) (*models.FinanceReceipt, error) {
	var deleted *db.FinanceReceiptDeleted
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		deleted, err = tx.DeleteFinanceReceipt(agentRow.ID, strings.TrimSpace(receiptId))
		return err
	}); err != nil {
		return nil, err
	}
	// The bytes after the row is gone and committed: bytes left behind by
	// a failure here are reclaimed by retention, while a row whose bytes
	// went first would be a file nobody can open.
	if deleted.DeletedAgentAttachmentID != "" && self.settings.Storage != nil {
		if err := self.settings.Storage.DeleteFile(ctx, deleted.DeletedAgentAttachmentID); err != nil {
			log.Warningf("cannot remove the bytes of receipt file %q: %s", deleted.DeletedAgentAttachmentID, err)
		}
	}
	return deleted.FinanceReceipt, nil
}
