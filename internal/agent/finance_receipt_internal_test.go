package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// inventedOrderReceipt is an invented online order of 30.00, read from a
// stored message: two items and a tax that add up.
func inventedOrderReceipt(mailId string) *models.FinanceReceipt {
	return &models.FinanceReceipt{
		ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: mailId, MerchantName: "Example Outfitters", PurchasedOn: "2026-09-10",
		CurrencyCode: "USD", SubtotalAmount: "28.00", TotalAmount: "30.00",
		ReceiptLines: []*models.FinanceReceiptLine{
			{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "WOOL SOCKS", LineAmount: "12.00"},
			{LineNumber: 2, ReceiptLineKind: models.ReceiptLineKindItem, Description: "RAIN HAT", LineAmount: "16.00"},
			{LineNumber: 3, ReceiptLineKind: models.ReceiptLineKindTax, Description: "Tax", LineAmount: "2.00"},
		},
	}
}

// One order charged as two shipments has no exact charge: it is left
// unmatched with both as candidates, and the person matches each by hand
// with the amount it explains. Reading the message again matches nothing
// by itself and keeps the person's matches; a receipt the matcher matched
// is matched again when read again.
func TestRecordReceiptLeavesSplitShipmentsToThePersonAndKeepsTheirMatches(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("shipment-one", "2026-09-11", "-18.00", "EXAMPLE OUTFITTERS 1 OF 2", "Example Outfitters", ""),
			inventedTransaction("shipment-two", "2026-09-14", "-12.00", "EXAMPLE OUTFITTERS 2 OF 2", "Example Outfitters", ""),
			inventedTransaction("single-order", "2026-09-12", "-45.50", "LAKESIDE BOOKS", "Lakeside Books", ""),
		},
	})
	byDescription := fixture.transactions(t)
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-order-one"), ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	candidates := recorded.ReceiptMatchCandidates
	if len(recorded.FinanceReceipt.ReceiptMatches) != 0 || len(candidates) != 3 ||
		candidates[0].FinanceTransactionID != byDescription["EXAMPLE OUTFITTERS 1 OF 2"].ID || candidates[1].FinanceTransactionID != byDescription["EXAMPLE OUTFITTERS 2 OF 2"].ID {
		t.Fatalf("a split shipment is left unmatched, both shipments the likeliest candidates: %+v", recorded)
	}
	receiptId := recorded.FinanceReceipt.ID
	for description, matchedAmount := range map[string]string{"EXAMPLE OUTFITTERS 1 OF 2": "18.00", "EXAMPLE OUTFITTERS 2 OF 2": "12.00"} {
		if _, err := fixture.worker.MatchReceipt(t.Context(), fixture.agent, receiptId, byDescription[description].ID, matchedAmount); err != nil {
			t.Fatalf("MatchReceipt: %s", err)
		}
	}
	again, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-order-one"), ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt again: %s", err)
	}
	if !again.IsReplaced || again.FinanceReceipt.ID != receiptId || len(again.FinanceReceipt.ReceiptMatches) != 2 {
		t.Fatalf("reading it again replaces it and keeps the person's matches: %+v", again)
	}
	for _, match := range again.FinanceReceipt.ReceiptMatches {
		if match.ReceiptMatchSource != models.ReceiptMatchSourcePerson {
			t.Fatalf("the matches are the person's: %+v", match)
		}
	}

	bookReceipt := inventedOrderReceipt("mail-books-one")
	bookReceipt.MerchantName, bookReceipt.SubtotalAmount, bookReceipt.TotalAmount = "Lakeside Books", "", "45.50"
	bookReceipt.ReceiptLines = bookReceipt.ReceiptLines[:1]
	bookReceipt.ReceiptLines[0].LineAmount = "45.50"
	books, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, bookReceipt, ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	if len(books.FinanceReceipt.ReceiptMatches) != 1 || books.FinanceReceipt.ReceiptMatches[0].ReceiptMatchSource != models.ReceiptMatchSourceReceiptMatcher {
		t.Fatalf("the one exact charge is matched by the matcher: %+v", books.FinanceReceipt.ReceiptMatches)
	}
	booksAgain, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, bookReceipt, ReceiptRecording{})
	if err != nil || len(booksAgain.FinanceReceipt.ReceiptMatches) != 1 {
		t.Fatalf("read again, it is matched once again, not twice: %+v %v", booksAgain, err)
	}
}

// A receipt handed in for a charge is matched to it by hand; one that does
// not add up is refused unless the recording accepts it, and then kept
// marked unbalanced.
func TestRecordReceiptForAChargeAndUnbalanced(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("shipment-one", "2026-09-30", "-18.00", "SOMEWHERE ELSE", "", "")},
	})
	charge := fixture.transactions(t)["SOMEWHERE ELSE"]
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-order-one"), ReceiptRecording{FinanceTransactionID: charge.ID})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	if matches := recorded.FinanceReceipt.ReceiptMatches; len(matches) != 1 || matches[0].FinanceTransactionID != charge.ID ||
		matches[0].ReceiptMatchSource != models.ReceiptMatchSourcePerson || matches[0].MatchedAmount != "18.0000" {
		t.Fatalf("the charge it was handed in for is matched by hand, for what it charged: %+v", matches)
	}

	misread := inventedOrderReceipt("mail-order-two")
	misread.ReceiptLines[1].LineAmount = "61.00"
	if _, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, misread, ReceiptRecording{}); !errors.Is(err, ErrReceiptUnbalanced) || !errors.Is(err, finance.ErrReceiptRefused) {
		t.Fatalf("a receipt that does not add up is refused: %v", err)
	}
	misread = inventedOrderReceipt("mail-order-two")
	misread.ReceiptLines[1].LineAmount = "61.00"
	kept, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, misread, ReceiptRecording{IsUnbalancedAccepted: true})
	if err != nil || kept.FinanceReceipt.ReceiptCheckState != models.ReceiptCheckStateUnbalanced || kept.FinanceReceipt.CheckDifferenceAmount != "45.0000" {
		t.Fatalf("accepted, it is kept marked: %+v %v", kept, err)
	}
}

// A receipt read again for less than the person matched it to is not
// kept matched beyond its new total: the person's match is taken off and
// the recording says so, and the receipt is left to be matched again.
func TestRecordReceiptDropsThePersonsMatchTheNewReadingCannotExplain(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("shipment-one", "2026-09-11", "-18.00", "SOMEWHERE ELSE", "", "")},
	})
	charge := fixture.transactions(t)["SOMEWHERE ELSE"]
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-order-one"), ReceiptRecording{FinanceTransactionID: charge.ID})
	if err != nil || len(recorded.FinanceReceipt.ReceiptMatches) != 1 {
		t.Fatalf("the receipt is matched to the charge by hand: %+v %v", recorded, err)
	}
	misread := inventedOrderReceipt("mail-order-one")
	misread.SubtotalAmount, misread.TotalAmount = "", "10.00"
	misread.ReceiptLines = misread.ReceiptLines[:1]
	misread.ReceiptLines[0].LineAmount = "10.00"
	again, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, misread, ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt again: %s", err)
	}
	if len(again.FinanceReceipt.ReceiptMatches) != 0 || len(again.DroppedReceiptMatchReasons) != 1 ||
		!strings.Contains(again.DroppedReceiptMatchReasons[0], charge.ID) {
		t.Fatalf("the match of 18.00 to a receipt of 10.00 is taken off and said: %+v", again)
	}
	if len(again.ReceiptMatchCandidates) != 1 || again.ReceiptMatchCandidates[0].FinanceTransactionID != charge.ID {
		t.Fatalf("the charge is a candidate again, for the person: %+v", again.ReceiptMatchCandidates)
	}
}

// lakesideBookReceipt is an invented one-line receipt of 45.50 from a
// stored message.
func lakesideBookReceipt(mailId string) *models.FinanceReceipt {
	receipt := inventedOrderReceipt(mailId)
	receipt.MerchantName, receipt.SubtotalAmount, receipt.TotalAmount = "Lakeside Books", "", "45.50"
	receipt.ReceiptLines = receipt.ReceiptLines[:1]
	receipt.ReceiptLines[0].LineAmount = "45.50"
	return receipt
}

// A receipt handed in for a charge it cannot explain is refused whole,
// unless the recording says to keep it, as the receipt job's does: then it
// is kept unmatched, with its candidates and the reason.
func TestRecordReceiptKeepsItUnmatchedWhenTheHandMatchIsRefused(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("book-charge", "2026-09-11", "-45.50", "LAKESIDE BOOKS", "Lakeside Books", "")},
	})
	charge := fixture.transactions(t)["LAKESIDE BOOKS"]
	if first, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, lakesideBookReceipt("mail-books-email"), ReceiptRecording{}); err != nil ||
		len(first.FinanceReceipt.ReceiptMatches) != 1 {
		t.Fatalf("the email explains the charge: %+v %v", first, err)
	}
	if _, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, lakesideBookReceipt("mail-books-copy"),
		ReceiptRecording{FinanceTransactionID: charge.ID}); !errors.Is(err, db.ErrInvalidArguments) {
		t.Fatalf("without the flag the whole receipt is refused: %v", err)
	}
	kept, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, lakesideBookReceipt("mail-books-copy"),
		ReceiptRecording{FinanceTransactionID: charge.ID, IsRecordedWhenHandMatchRefused: true})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	if len(kept.FinanceReceipt.ReceiptMatches) != 0 || !strings.Contains(kept.HandMatchRefusalReason, "other receipts already explain all of that charge") ||
		strings.HasPrefix(kept.HandMatchRefusalReason, "db:") {
		t.Fatalf("it is kept unmatched, saying why in words: %+v", kept)
	}
}

// Two readings of one purchase at once: the other's match to the charge
// commits while this one's automatic match waits on the charge's row.
// This one's match is then refused, and the receipt is still recorded,
// unmatched, its candidate no longer automatic.
func TestRecordReceiptKeepsTheReceiptWhenAnotherMatchWinsTheCharge(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("book-charge", "2026-09-11", "-45.50", "LAKESIDE BOOKS", "Lakeside Books", "")},
	})
	charge := fixture.transactions(t)["LAKESIDE BOOKS"]
	undated := lakesideBookReceipt("mail-books-photo")
	undated.PurchasedOn = ""
	other, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, undated, ReceiptRecording{})
	if err != nil || len(other.FinanceReceipt.ReceiptMatches) != 0 {
		t.Fatalf("an undated receipt is recorded unmatched: %+v %v", other, err)
	}
	isOtherMatched, isOtherCommitting := make(chan struct{}), make(chan struct{})
	otherDone := make(chan error, 1)
	go func() {
		otherDone <- fixture.database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.PutFinanceReceiptMatch(fixture.agent.ID, &models.FinanceReceiptMatch{
				ReceiptID: other.FinanceReceipt.ID, FinanceTransactionID: charge.ID, MatchedAmount: "45.50", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
			}); err != nil {
				return err
			}
			close(isOtherMatched)
			<-isOtherCommitting
			return nil
		})
	}()
	<-isOtherMatched
	type recordOutcome struct {
		recorded *RecordedReceipt
		err      error
	}
	recordDone := make(chan recordOutcome, 1)
	go func() {
		recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, lakesideBookReceipt("mail-books-email"), ReceiptRecording{})
		recordDone <- recordOutcome{recorded, err}
	}()
	for attempt := 0; dbtest.QueryString(t, fixture.database, `SELECT COUNT(*)::text FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock'`) == "0"; attempt++ {
		if attempt > 1000 {
			t.Fatal("the automatic match never waited on the other match")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(isOtherCommitting)
	if err := <-otherDone; err != nil {
		t.Fatalf("the other match: %s", err)
	}
	outcome := <-recordDone
	if outcome.err != nil {
		t.Fatalf("a refused automatic match does not stop the recording: %s", outcome.err)
	}
	recorded := outcome.recorded
	if len(recorded.FinanceReceipt.ReceiptMatches) != 0 || len(recorded.ReceiptMatchCandidates) != 1 || recorded.ReceiptMatchCandidates[0].IsAutomatic {
		t.Fatalf("the receipt is kept unmatched, its candidate left to the person: %+v", recorded)
	}
}

// One receipt explains at most its total across all its charges: matched
// in part to one charge, a match by hand to another with no amount takes
// what is left of it, and an amount beyond that is refused.
func TestMatchReceiptKeepsWithinTheReceiptsTotal(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("shipment-one", "2026-09-11", "-25.00", "EXAMPLE OUTFITTERS 1 OF 2", "Example Outfitters", ""),
			inventedTransaction("shipment-two", "2026-09-14", "-25.00", "EXAMPLE OUTFITTERS 2 OF 2", "Example Outfitters", ""),
		},
	})
	byDescription := fixture.transactions(t)
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-order-one"), ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	receiptId := recorded.FinanceReceipt.ID
	if _, err := fixture.worker.MatchReceipt(t.Context(), fixture.agent, receiptId, byDescription["EXAMPLE OUTFITTERS 1 OF 2"].ID, ""); err != nil {
		t.Fatalf("MatchReceipt: %s", err)
	}
	if _, err := fixture.worker.MatchReceipt(t.Context(), fixture.agent, receiptId, byDescription["EXAMPLE OUTFITTERS 2 OF 2"].ID, "6.00"); !errors.Is(err, db.ErrInvalidArguments) {
		t.Fatalf("a match beyond what is left of the receipt is refused: %v", err)
	}
	matched, err := fixture.worker.MatchReceipt(t.Context(), fixture.agent, receiptId, byDescription["EXAMPLE OUTFITTERS 2 OF 2"].ID, "")
	if err != nil {
		t.Fatalf("MatchReceipt: %s", err)
	}
	amountByCharge := map[string]string{}
	for _, match := range matched.ReceiptMatches {
		amountByCharge[match.FinanceTransactionID] = match.MatchedAmount
	}
	if amountByCharge[byDescription["EXAMPLE OUTFITTERS 1 OF 2"].ID] != "25.0000" || amountByCharge[byDescription["EXAMPLE OUTFITTERS 2 OF 2"].ID] != "5.0000" {
		t.Fatalf("the second charge takes what is left of the 30.00: %+v", amountByCharge)
	}
}

// A receipt whose total is nothing, as a void prints, explains no charge,
// and a match by hand with no amount says that rather than blaming
// matches to other charges it does not have.
func TestMatchReceiptRefusesATotalOfNothingPlainly(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added:    []finance.Transaction{inventedTransaction("socks", "2026-09-11", "-12.00", "EXAMPLE OUTFITTERS", "Example Outfitters", "")},
	})
	byDescription := fixture.transactions(t)
	voided := inventedOrderReceipt("mail-void")
	voided.SubtotalAmount, voided.TotalAmount = "0.00", "0.00"
	voided.ReceiptLines = []*models.FinanceReceiptLine{
		{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "WOOL SOCKS", LineAmount: "12.00"},
		{LineNumber: 2, ReceiptLineKind: models.ReceiptLineKindDiscount, Description: "Voided", LineAmount: "-12.00", DiscountedLineNumber: 1},
	}
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, voided, ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	_, err = fixture.worker.MatchReceipt(t.Context(), fixture.agent, recorded.FinanceReceipt.ID, byDescription["EXAMPLE OUTFITTERS"].ID, "")
	if !errors.Is(err, db.ErrInvalidArguments) || !strings.Contains(err.Error(), "explains no charge") {
		t.Fatalf("a total of nothing is refused as such: %v", err)
	}
}

// A tool called directly, as an MCP client calls one, runs as a run that
// can ask and is not headless: the person at the harness made the call.
// The finance tool reads that as the person being there, so an MCP client
// may replace the person's annotation; this pins that choice.
func TestDirectRunCountsAsThePersonPresent(t *testing.T) {
	run := &directRun{surface: "mcp"}
	if !run.CanAsk() || run.Headless() {
		t.Fatalf("a direct run can ask and is not headless: canAsk %v, headless %v", run.CanAsk(), run.Headless())
	}
}

// Deleting a receipt read from an upload removes the upload's bytes too.
func TestDeleteReceiptRemovesItsUpload(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	fixture.worker.settings.Storage = store
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "receipt.jpg", ContentType: "image/jpeg", Size: 4}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := store.PutFile(t.Context(), attachment.ID, []byte("jpeg")); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	fromPhoto := inventedOrderReceipt("")
	fromPhoto.ReceiptSourceKind, fromPhoto.MailID, fromPhoto.AgentAttachmentID = models.ReceiptSourceKindAttachment, "", attachment.ID
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, fromPhoto, ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	if _, err := fixture.worker.DeleteReceipt(t.Context(), fixture.agent, recorded.FinanceReceipt.ID); err != nil {
		t.Fatalf("DeleteReceipt: %s", err)
	}
	if _, err := store.GetFile(t.Context(), attachment.ID); err == nil {
		t.Fatal("the upload's bytes go with the receipt")
	}
}

// A receipt recorded before its charge arrives waits, unmatched, and the
// sync that brings the charge matches it by the same rule as recording
// does. A match the person takes off is not put back by a later sync, and
// the posted charge a pending one became is not a new charge to match.
func TestSyncMatchesAReceiptRecordedBeforeItsCharge(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	syncAndFollow := func(added ...finance.Transaction) *db.FinanceSyncApplied {
		t.Helper()
		var applied *db.FinanceSyncApplied
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			var err error
			if applied, err = tx.ApplyFinanceSync(fixture.agent.ID, fixture.source.ID, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: added}, time.Now().UTC().Format(time.DateOnly)); err != nil {
				t.Fatalf("ApplyFinanceSync: %s", err)
			}
		})
		if err := fixture.worker.afterFinanceSync(t.Context(), fixture.run(), fixture.source, applied, time.Now().UTC().Format(time.DateOnly), false, false); err != nil {
			t.Fatalf("afterFinanceSync: %s", err)
		}
		return applied
	}
	receiptMatches := func(receiptId string) []*models.FinanceReceiptMatch {
		t.Helper()
		var found *models.FinanceReceipt
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			var err error
			if found, err = tx.GetFinanceReceipt(fixture.agent.ID, receiptId); err != nil || found == nil {
				t.Fatalf("GetFinanceReceipt: %v %v", found, err)
			}
		})
		return found.ReceiptMatches
	}

	syncAndFollow(inventedTransaction("unrelated", "2026-09-09", "-12.00", "LAKESIDE BOOKS", "Lakeside Books", ""))
	recorded, err := fixture.worker.RecordReceipt(t.Context(), fixture.agent, inventedOrderReceipt("mail-before-charge"), ReceiptRecording{})
	if err != nil {
		t.Fatalf("RecordReceipt: %s", err)
	}
	receiptId := recorded.FinanceReceipt.ID
	if len(recorded.FinanceReceipt.ReceiptMatches) != 0 {
		t.Fatalf("with no charge of its merchant yet, the receipt waits: %+v", recorded.FinanceReceipt.ReceiptMatches)
	}

	// The pending charge arrives; then the posted one it became.
	applied := syncAndFollow(inventedTransaction("order-pending", "2026-09-11", "-30.00", "EXAMPLE OUTFITTERS", "Example Outfitters", ""))
	if len(applied.InsertedFinanceTransactionIDs) != 1 {
		t.Fatalf("the sync names the charge it brought: %+v", applied.InsertedFinanceTransactionIDs)
	}
	matches := receiptMatches(receiptId)
	if len(matches) != 1 || matches[0].FinanceTransactionID != applied.InsertedFinanceTransactionIDs[0] ||
		matches[0].ReceiptMatchSource != models.ReceiptMatchSourceReceiptMatcher || matches[0].MatchedAmount != "30.0000" {
		t.Fatalf("the waiting receipt is matched to the charge that arrived: %+v", matches)
	}
	if _, err := fixture.worker.UnmatchReceipt(t.Context(), fixture.agent, receiptId, matches[0].FinanceTransactionID); err != nil {
		t.Fatalf("UnmatchReceipt: %s", err)
	}
	posted := inventedTransaction("order-posted", "2026-09-12", "-30.00", "EXAMPLE OUTFITTERS", "Example Outfitters", "")
	posted.PendingProviderTransactionID = "order-pending"
	applied = syncAndFollow(posted, inventedTransaction("another", "2026-09-12", "-4.00", "CORNER CAFE", "Corner Cafe", ""))
	if len(applied.InsertedFinanceTransactionIDs) != 1 {
		t.Fatalf("the posted one a pending charge became is not new: %+v", applied.InsertedFinanceTransactionIDs)
	}
	if matches := receiptMatches(receiptId); len(matches) != 0 {
		t.Fatalf("the match the person took off is not put back: %+v", matches)
	}
}
