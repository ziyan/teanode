package db_test

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// inventedGroceryReceipt is a receipt read from a stored message: three
// items, a promotion on the second, a fee and a tax, adding up.
func inventedGroceryReceipt(agentId, mailId string) *models.FinanceReceipt {
	return &models.FinanceReceipt{
		AgentID: agentId, ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: mailId,
		MerchantName: "Corner Grocer", MerchantReceiptNumber: "R-1001", PurchasedOn: "2026-09-10", CurrencyCode: "usd",
		SubtotalAmount: "40.17", TotalAmount: "42.17", PaymentAccountMask: "0001",
		ReceiptCheckState: models.ReceiptCheckStateBalanced, CheckDifferenceAmount: "0",
		ReceiptLines: []*models.FinanceReceiptLine{
			{LineNumber: 1, ReceiptLineKind: models.ReceiptLineKindItem, Description: "OAT FLAKES", LineAmount: "12.50", TaxClassCode: "F"},
			{LineNumber: 2, ReceiptLineKind: models.ReceiptLineKindItem, Description: "PEARS", Quantity: "1.5", QuantityUnit: "lb", UnitPriceAmount: "3.00", LineAmount: "4.50", TaxClassCode: "F"},
			{LineNumber: 3, ReceiptLineKind: models.ReceiptLineKindDiscount, Description: "Promotion", LineAmount: "-1.00", TaxClassCode: "F", DiscountedLineNumber: 2},
			{LineNumber: 4, ReceiptLineKind: models.ReceiptLineKindItem, Description: "DISH SOAP", LineAmount: "23.97", TaxClassCode: "T"},
			{LineNumber: 5, ReceiptLineKind: models.ReceiptLineKindFee, Description: "BAG FEE", LineAmount: "0.20"},
			{LineNumber: 6, ReceiptLineKind: models.ReceiptLineKindTax, Description: "Sales Tax", LineAmount: "2.00", TaxClassCode: "T"},
		},
	}
}

// listReceipts is one page of receipts, as ListFinanceReceipts gives it,
// its error passed through.
func listReceipts(t *testing.T, tx db.Transaction, agentId string, filter *db.FinanceReceiptFilter) ([]*models.FinanceReceipt, error) {
	t.Helper()
	page, err := tx.ListFinanceReceipts(agentId, filter)
	if err != nil {
		return nil, err
	}
	return page.FinanceReceipts, nil
}

func putReceipt(t *testing.T, tx db.Transaction, receipt *models.FinanceReceipt) *models.FinanceReceipt {
	t.Helper()
	stored, err := tx.PutFinanceReceipt(receipt)
	if err != nil {
		t.Fatalf("PutFinanceReceipt: %s", err)
	}
	return stored
}

// The agent writes an annotation where the person has written none, and
// over the person's only when the person asked; the person always may,
// and empty takes it away. Nobody reaches another agent's transaction.
func TestFinanceTransactionAnnotationKeepsThePersonsWords(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	stranger := createFinanceFixture(t, database, "sam")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"]
		isWritten, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "  weekly shop  ", models.AnnotatedByAgent, false)
		if err != nil || !isWritten {
			t.Fatalf("the agent writes where nobody has: %v %v", isWritten, err)
		}
		isWritten, err = tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "party supplies", models.AnnotatedByPerson, false)
		if err != nil || !isWritten {
			t.Fatalf("the person writes over the agent: %v %v", isWritten, err)
		}
		isWritten, err = tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "groceries", models.AnnotatedByAgent, false)
		if err != nil || isWritten {
			t.Fatalf("the agent never writes over the person: %v %v", isWritten, err)
		}
		isWritten, err = tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "", models.AnnotatedByAgent, false)
		if err != nil || isWritten {
			t.Fatalf("nor takes the person's away: %v %v", isWritten, err)
		}
		read, err := tx.GetFinanceTransaction(fixture.agentId, grocer.ID)
		if err != nil {
			t.Fatalf("GetFinanceTransaction: %s", err)
		}
		if read.Annotation != "party supplies" || read.AnnotatedBy != models.AnnotatedByPerson {
			t.Fatalf("the person's annotation stands: %+v", read)
		}
		isWritten, err = tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "party supplies and snacks", models.AnnotatedByAgent, true)
		if err != nil || !isWritten {
			t.Fatalf("the agent replaces the person's when the person asked: %v %v", isWritten, err)
		}
		read, _ = tx.GetFinanceTransaction(fixture.agentId, grocer.ID)
		if read.Annotation != "party supplies and snacks" || read.AnnotatedBy != models.AnnotatedByAgent {
			t.Fatalf("what the agent wrote is the agent's: %+v", read)
		}
		if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "", models.AnnotatedByPerson, false); err != nil {
			t.Fatalf("the person clears it: %s", err)
		}
		read, _ = tx.GetFinanceTransaction(fixture.agentId, grocer.ID)
		if read.Annotation != "" || read.AnnotatedBy != "" {
			t.Fatalf("cleared, it has no author either: %+v", read)
		}
		if _, err := tx.SetFinanceTransactionAnnotation(stranger.agentId, grocer.ID, "mine now", models.AnnotatedByPerson, false); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent's transaction is not found: %v", err)
		}
		if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "a note", "someone", false); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("an unknown author is refused: %v", err)
		}
	})
}

// A receipt is stored line by line as given, a discount pointing at its
// item; reading the same source again replaces the lines under the same
// id and keeps the matches. Lines with an unknown kind, a discount of a
// line the receipt does not have, or a repeated number are refused.
func TestPutFinanceReceiptReplacesTheSameSource(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-receipt-one"))
		if first.CurrencyCode != "USD" || first.TotalAmount != "42.1700" || first.SubtotalAmount != "40.1700" || len(first.ReceiptLines) != 6 {
			t.Fatalf("the receipt is stored as given, its amounts canonical: %+v", first)
		}
		promotion := first.ReceiptLines[2]
		if promotion.DiscountedLineNumber != 2 || promotion.DiscountedLineID != first.ReceiptLines[1].ID || promotion.LineAmount != "-1.0000" {
			t.Fatalf("the discount points at its item: %+v", promotion)
		}
		if first.ReceiptLines[1].Quantity != "1.50000000" || first.ReceiptLines[1].QuantityUnit != "lb" {
			t.Fatalf("a weighed item keeps its quantity: %+v", first.ReceiptLines[1])
		}
		grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"]
		if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
			ReceiptID: first.ID, FinanceTransactionID: grocer.ID, MatchedAmount: "42.17", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
		}); err != nil {
			t.Fatalf("PutFinanceReceiptMatch: %s", err)
		}

		again := inventedGroceryReceipt(fixture.agentId, "mail-receipt-one")
		again.ReceiptLines = again.ReceiptLines[:2]
		again.ReceiptCheckState, again.CheckDifferenceAmount = models.ReceiptCheckStateUnbalanced, "-23.17"
		second := putReceipt(t, tx, again)
		if second.ID != first.ID || len(second.ReceiptLines) != 2 || second.ReceiptCheckState != models.ReceiptCheckStateUnbalanced {
			t.Fatalf("the same source is replaced, not added: %+v", second)
		}
		if len(second.ReceiptMatches) != 1 || second.ReceiptMatches[0].FinanceTransactionID != grocer.ID {
			t.Fatalf("its matches are kept: %+v", second.ReceiptMatches)
		}
		listed, err := listReceipts(t, tx, fixture.agentId, nil)
		if err != nil || len(listed) != 1 {
			t.Fatalf("one receipt for one source: %d %v", len(listed), err)
		}

		for name, broken := range map[string]func(*models.FinanceReceipt){
			"an unknown kind":           func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[0].ReceiptLineKind = "coupon" },
			"a missing discounted line": func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[2].DiscountedLineNumber = 9 },
			"a repeated number":         func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[1].LineNumber = 1 },
			"no source id":              func(receipt *models.FinanceReceipt) { receipt.MailID = "" },
			"a word for the mask":       func(receipt *models.FinanceReceipt) { receipt.PaymentAccountMask = "VISA" },
		} {
			receipt := inventedGroceryReceipt(fixture.agentId, "mail-receipt-two")
			broken(receipt)
			if _, err := tx.PutFinanceReceipt(receipt); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s is refused: %v", name, err)
			}
		}
	})
}

// The table itself refuses a receipt whose kind does not name the one
// source column that is set.
func TestFinanceReceiptSourceIsCheckedByTheTable(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	rawExec := database.(interface{ RawExec(string) error }).RawExec
	for name, source := range map[string]struct{ columns, values string }{
		"two sources":                  {`"mail_id", "gmail_message_id"`, `'mail', 'mail-one', 'gmail-one'`},
		"a kind naming a column unset": {`"gmail_message_id"`, `'mail', 'gmail-one'`},
		"an unknown kind":              {`"mail_id"`, `'fax', 'mail-one'`},
	} {
		statement := `INSERT INTO "agent_finance_receipt" ("receipt_source_kind", ` + source.columns + `, "id", "agent_id",
				"currency_code", "total_amount", "receipt_check_state", "created_at", "modified_at")
			VALUES (` + source.values + `, 'receipt-broken', '` + fixture.agentId + `', 'USD', 1, 'balanced', now(), now())`
		if err := rawExec(statement); err == nil {
			t.Errorf("a receipt with %s is refused by the table", name)
		}
	}
	if err := rawExec(`INSERT INTO "agent_finance_receipt" ("receipt_source_kind", "mail_id", "id", "agent_id",
			"currency_code", "total_amount", "receipt_check_state", "created_at", "modified_at")
		VALUES ('mail', 'mail-one', 'receipt-whole', '` + fixture.agentId + `', 'USD', 1, 'balanced', now(), now())`); err != nil {
		t.Fatalf("a receipt with its one source is taken: %s", err)
	}
}

// The migration reverses, taking the receipts and the annotations with
// it, and applies again on top, after which receipts are written as
// before.
func TestFinanceReceiptsMigrationReversesAndReapplies(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"]
		if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, grocer.ID, "party supplies", models.AnnotatedByPerson, false); err != nil {
			t.Fatalf("SetFinanceTransactionAnnotation: %s", err)
		}
		putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-receipt-one"))
	})
	for _, migration := range migrations.Migrations() {
		if migration.ID != "0154_finance_receipts" {
			continue
		}
		dbtest.Exec(t, database, migration.ReverseSQL)
		if tables := dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM information_schema.tables WHERE table_name LIKE 'agent_finance_receipt%'`); tables != "0" {
			t.Fatalf("the reverse drops the receipt tables: %s left", tables)
		}
		if columns := dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM information_schema.columns
			WHERE table_name = 'agent_finance_transaction' AND column_name IN ('annotation', 'annotated_by')`); columns != "0" {
			t.Fatalf("the reverse drops the annotation columns: %s left", columns)
		}
		dbtest.Exec(t, database, migration.SQL)
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"]
			if grocer.Annotation != "" {
				t.Fatalf("the annotation went with the reverse: %+v", grocer)
			}
			stored := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-receipt-one"))
			if len(stored.ReceiptLines) != 6 {
				t.Fatalf("receipts are written again after applying it again: %+v", stored)
			}
		})
		return
	}
	t.Fatal("there is no migration 0154_finance_receipts")
}

// The receipt matcher never replaces or removes the person's match;
// transactions say how many receipts they have; listings find a
// transaction's receipts and the unmatched ones; deleting the transaction
// or the receipt takes the match with it.
func TestFinanceReceiptMatchesKeepThePersonsAndCascade(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	stranger := createFinanceFixture(t, database, "sam")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		byProvider := financeTransactionsByProviderId(t, tx, fixture.agentId)
		grocer, diner := byProvider["transaction-grocer"], byProvider["transaction-diner"]
		receipt := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-receipt-one"))
		unmatched := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-receipt-two"))

		isWritten, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
			ReceiptID: receipt.ID, FinanceTransactionID: grocer.ID, MatchedAmount: "42.17", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
		})
		if err != nil || !isWritten {
			t.Fatalf("the person matches: %v %v", isWritten, err)
		}
		isWritten, err = tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
			ReceiptID: receipt.ID, FinanceTransactionID: grocer.ID, MatchedAmount: "1.00", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: "0.9",
		})
		if err != nil || isWritten {
			t.Fatalf("the matcher does not replace the person's match: %v %v", isWritten, err)
		}
		isRemoved, err := tx.DeleteFinanceReceiptMatch(fixture.agentId, receipt.ID, grocer.ID, models.ReceiptMatchSourceReceiptMatcher)
		if err != nil || isRemoved {
			t.Fatalf("nor removes it: %v %v", isRemoved, err)
		}
		if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
			ReceiptID: receipt.ID, FinanceTransactionID: diner.ID, MatchedAmount: "0", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a match explains more than nothing: %v", err)
		}
		if _, err := tx.PutFinanceReceiptMatch(stranger.agentId, &models.FinanceReceiptMatch{
			ReceiptID: receipt.ID, FinanceTransactionID: grocer.ID, MatchedAmount: "1", ReceiptMatchSource: models.ReceiptMatchSourcePerson,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent cannot match this receipt: %v", err)
		}

		read, err := tx.GetFinanceTransaction(fixture.agentId, grocer.ID)
		if err != nil || read.ReceiptCount != 1 {
			t.Fatalf("the transaction counts its receipt: %+v %v", read, err)
		}
		page, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, listed := range page.FinanceTransactions {
			if wanted := map[bool]int{true: 1, false: 0}[listed.ID == grocer.ID]; listed.ReceiptCount != wanted {
				t.Fatalf("a page counts receipts too: %+v", listed)
			}
		}
		ofGrocer, err := listReceipts(t, tx, fixture.agentId, &db.FinanceReceiptFilter{FinanceTransactionID: grocer.ID})
		if err != nil || len(ofGrocer) != 1 || ofGrocer[0].ID != receipt.ID {
			t.Fatalf("the transaction's receipts: %+v %v", ofGrocer, err)
		}
		withoutMatch, err := listReceipts(t, tx, fixture.agentId, &db.FinanceReceiptFilter{IsUnmatched: true, From: "2026-09-01", To: "2026-09-30"})
		if err != nil || len(withoutMatch) != 1 || withoutMatch[0].ID != unmatched.ID {
			t.Fatalf("the unmatched receipts: %+v %v", withoutMatch, err)
		}
		if strangers, err := listReceipts(t, tx, stranger.agentId, nil); err != nil || len(strangers) != 0 {
			t.Fatalf("another agent sees none: %+v %v", strangers, err)
		}
		isRemoved, err = tx.DeleteFinanceReceiptMatch(fixture.agentId, receipt.ID, grocer.ID, "")
		if err != nil || !isRemoved {
			t.Fatalf("the person unmatches: %v %v", isRemoved, err)
		}
		if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
			ReceiptID: unmatched.ID, FinanceTransactionID: diner.ID, MatchedAmount: "18.40", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher,
		}); err != nil {
			t.Fatalf("PutFinanceReceiptMatch: %s", err)
		}
	})
	// The card's account goes, with its transactions and their matches;
	// the receipt stays, unmatched.
	deletedReceiptId := ""
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(fixture.agentId, "")
		if err != nil {
			t.Fatalf("ListFinanceAccounts: %s", err)
		}
		for _, account := range accounts {
			if account.ProviderAccountID == "account-card" {
				if _, err := tx.DeleteFinanceAccount(fixture.agentId, account.ID); err != nil {
					t.Fatalf("DeleteFinanceAccount: %s", err)
				}
			}
		}
		receipts, err := listReceipts(t, tx, fixture.agentId, &db.FinanceReceiptFilter{IsUnmatched: true})
		if err != nil || len(receipts) != 2 {
			t.Fatalf("a deleted transaction takes its matches, not the receipts: %d %v", len(receipts), err)
		}
		deleted, err := tx.DeleteFinanceReceipt(fixture.agentId, receipts[0].ID)
		if err != nil || deleted.FinanceReceipt.ID != receipts[0].ID {
			t.Fatalf("DeleteFinanceReceipt: %+v %v", deleted, err)
		}
		if _, err := tx.DeleteFinanceReceipt(fixture.agentId, receipts[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a receipt deleted is not found: %v", err)
		}
		deletedReceiptId = receipts[0].ID
	})
	if lineCount := dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM "agent_finance_receipt_line" WHERE "receipt_id" = '`+deletedReceiptId+`'`); lineCount != "0" {
		t.Fatalf("the lines go with the receipt: %s left", lineCount)
	}
}

// The sweep of uploads never sent leaves the photo a receipt was read
// from, and deleting the receipt deletes the photo unless a message holds
// it.
func TestReceiptPhotoIsNotAnOrphan(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	strangers := createFinanceFixture(t, database, "sam")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		createAttachment := func(name string) *models.AgentAttachment {
			attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agentId, Name: name, ContentType: "image/jpeg", Size: 10})
			if err != nil {
				t.Fatalf("CreateAgentAttachment: %s", err)
			}
			return attachment
		}
		photo, held, stray := createAttachment("receipt.jpg"), createAttachment("held.jpg"), createAttachment("stray.jpg")
		if err := tx.ClaimAgentAttachments([]string{held.ID}, "conversation-one", "message-one"); err != nil {
			t.Fatalf("ClaimAgentAttachments: %s", err)
		}
		fromPhoto := inventedGroceryReceipt(fixture.agentId, "")
		fromPhoto.ReceiptSourceKind, fromPhoto.MailID, fromPhoto.AgentAttachmentID = models.ReceiptSourceKindAttachment, "", photo.ID
		photoReceipt := putReceipt(t, tx, fromPhoto)
		fromHeld := inventedGroceryReceipt(fixture.agentId, "")
		fromHeld.ReceiptSourceKind, fromHeld.MailID, fromHeld.AgentAttachmentID = models.ReceiptSourceKindAttachment, "", held.ID
		heldReceipt := putReceipt(t, tx, fromHeld)

		orphans, err := tx.ListOrphanAgentAttachments(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("ListOrphanAgentAttachments: %s", err)
		}
		if len(orphans) != 1 || orphans[0].ID != stray.ID {
			t.Fatalf("only the stray upload is an orphan: %+v", orphans)
		}
		deleted, err := tx.DeleteFinanceReceipt(fixture.agentId, photoReceipt.ID)
		if err != nil || deleted.DeletedAgentAttachmentID != photo.ID {
			t.Fatalf("the photo goes with its receipt: %+v %v", deleted, err)
		}
		if gone, _ := tx.GetAgentAttachment(photo.ID); gone != nil {
			t.Fatalf("its row is gone: %+v", gone)
		}
		deleted, err = tx.DeleteFinanceReceipt(fixture.agentId, heldReceipt.ID)
		if err != nil || deleted.DeletedAgentAttachmentID != "" {
			t.Fatalf("a file a message holds stays: %+v %v", deleted, err)
		}
		if kept, _ := tx.GetAgentAttachment(held.ID); kept == nil {
			t.Fatal("the held file is kept")
		}
		fromOther := inventedGroceryReceipt(strangers.agentId, "")
		fromOther.ReceiptSourceKind, fromOther.MailID, fromOther.AgentAttachmentID = models.ReceiptSourceKindAttachment, "", stray.ID
		if _, err := tx.PutFinanceReceipt(fromOther); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent's upload is not found: %v", err)
		}
	})
}

// isSameDecimal is true when two decimal strings hold the same number,
// whatever their scale.
func isSameDecimal(first, second string) bool {
	firstValue, isFirstValid := new(big.Rat).SetString(first)
	secondValue, isSecondValid := new(big.Rat).SetString(second)
	return isFirstValid && isSecondValid && firstValue.Cmp(secondValue) == 0
}

// receiptMatchesOf is every receipt match of one finance transaction.
func receiptMatchesOf(t *testing.T, tx db.Transaction, agentId, financeTransactionId string) []*models.FinanceReceiptMatch {
	t.Helper()
	receipts, err := listReceipts(t, tx, agentId, &db.FinanceReceiptFilter{FinanceTransactionID: financeTransactionId})
	if err != nil {
		t.Fatalf("ListFinanceReceipts: %s", err)
	}
	var matches []*models.FinanceReceiptMatch
	for _, receipt := range receipts {
		for _, match := range receipt.ReceiptMatches {
			if match.FinanceTransactionID == financeTransactionId {
				matches = append(matches, match)
			}
		}
	}
	return matches
}

// When a posted charge takes the place of the pending one it was, the
// annotation and the matched receipts move to it in the same sync: under
// Plaid to the posted charge that names the pending one, under SimpleFIN
// to the posted charge of the same amount a few days later. A receipt
// matcher's whole-amount match follows a charge that posts for more; the
// person's match keeps the amount they gave; a posted charge with its own
// annotation keeps it.
func TestPendingAnnotationAndReceiptsFollowThePostedTransaction(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "finance-pending-receipts")
	accounts := sampleFinanceSync().Accounts
	pending := func(providerTransactionId, postedOn, amount, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: postedOn,
			Amount: amount, CurrencyCode: "USD", Description: description, IsPending: true}
	}
	posted := func(providerTransactionId, pendingProviderTransactionId, amount, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: "2026-09-12",
			Amount: amount, CurrencyCode: "USD", Description: description, PendingProviderTransactionID: pendingProviderTransactionId}
	}
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		pending("pending-cafe", "2026-09-10", "-12.00", "CAFE"),
		pending("pending-diner", "2026-09-10", "-40.00", "DINER"),
		pending("pending-hardware", "2026-09-10", "-30.00", "HARDWARE"),
		pending("pending-florist", "2026-09-10", "-55.00", "FLORIST"),
		pending("simple-pending-bakery", "2026-09-11", "-8.50", "BAKERY"),
	}}, "2026-09-11")

	receiptIdByProviderTransactionId := map[string]string{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for providerTransactionId, annotatedBy := range map[string]models.AnnotatedBy{
			"pending-cafe": models.AnnotatedByPerson, "pending-diner": models.AnnotatedByAgent,
			"pending-florist": models.AnnotatedByPerson, "simple-pending-bakery": models.AnnotatedByPerson,
		} {
			if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, found[providerTransactionId].ID, "note on "+providerTransactionId, annotatedBy, false); err != nil {
				t.Fatalf("SetFinanceTransactionAnnotation: %s", err)
			}
		}
		for providerTransactionId, match := range map[string]models.FinanceReceiptMatch{
			"pending-cafe":          {MatchedAmount: "12.00", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: "0.8"},
			"pending-diner":         {MatchedAmount: "40.00", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: "0.95"},
			"pending-hardware":      {MatchedAmount: "30.00", ReceiptMatchSource: models.ReceiptMatchSourcePerson},
			"simple-pending-bakery": {MatchedAmount: "8.50", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: "0.7"},
		} {
			receipt := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-for-"+providerTransactionId))
			receiptIdByProviderTransactionId[providerTransactionId] = receipt.ID
			match.ReceiptID, match.FinanceTransactionID = receipt.ID, found[providerTransactionId].ID
			if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &match); err != nil {
				t.Fatalf("PutFinanceReceiptMatch: %s", err)
			}
		}
	})

	// The posted florist charge arrives first while its pending one is
	// still listed, and is given its own annotation.
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		posted("posted-florist", "", "-55.00", "FLORIST"),
	}}, "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		florist := financeTransactionsByProviderId(t, tx, fixture.agentId)["posted-florist"]
		if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, florist.ID, "the posted one's own note", models.AnnotatedByPerson, false); err != nil {
			t.Fatalf("SetFinanceTransactionAnnotation: %s", err)
		}
	})

	// Plaid: the cafe posts for the same amount, the diner with a tip, the
	// hardware store for a little more.
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		posted("posted-cafe", "pending-cafe", "-12.00", "CAFE"),
		posted("posted-diner", "pending-diner", "-46.00", "DINER"),
		posted("posted-hardware", "pending-hardware", "-32.00", "HARDWARE"),
		posted("posted-florist", "pending-florist", "-55.00", "FLORIST"),
	}, RemovedProviderTransactionIDs: []string{"pending-cafe", "pending-diner", "pending-hardware", "pending-florist"}}, "2026-09-12")

	// SimpleFIN: the pending bakery charge is not sent again, and the
	// posted one arrives under a new id.
	replacedFrom := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	applied := applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, PendingReplacedFrom: &replacedFrom, Added: []finance.Transaction{
		posted("posted-cafe", "pending-cafe", "-12.00", "CAFE"),
		posted("posted-diner", "pending-diner", "-46.00", "DINER"),
		posted("posted-hardware", "pending-hardware", "-32.00", "HARDWARE"),
		posted("posted-florist", "pending-florist", "-55.00", "FLORIST"),
		{ProviderTransactionID: "simple-posted-bakery", ProviderAccountID: "account-checking", PostedOn: "2026-09-13", Amount: "-8.50",
			CurrencyCode: "USD", Description: "BAKERY"},
	}}, "2026-09-13")
	if applied.ReplacedPendingTransactionCount != 1 {
		t.Fatalf("the pending bakery charge is replaced: %+v", applied)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for _, providerTransactionId := range []string{"pending-cafe", "pending-diner", "pending-hardware", "pending-florist", "simple-pending-bakery"} {
			if found[providerTransactionId] != nil {
				t.Errorf("%s is gone once posted", providerTransactionId)
			}
		}
		for postedProviderTransactionId, want := range map[string]struct {
			annotation  string
			annotatedBy models.AnnotatedBy
		}{
			"posted-cafe":          {"note on pending-cafe", models.AnnotatedByPerson},
			"posted-diner":         {"note on pending-diner", models.AnnotatedByAgent},
			"posted-hardware":      {"", ""},
			"posted-florist":       {"the posted one's own note", models.AnnotatedByPerson},
			"simple-posted-bakery": {"note on simple-pending-bakery", models.AnnotatedByPerson},
		} {
			got := found[postedProviderTransactionId]
			if got == nil || got.Annotation != want.annotation || got.AnnotatedBy != want.annotatedBy {
				t.Errorf("%s carries the annotation %q by %q: %+v", postedProviderTransactionId, want.annotation, want.annotatedBy, got)
			}
		}
		for postedProviderTransactionId, want := range map[string]struct {
			pendingProviderTransactionId string
			matchedAmount                string
			receiptMatchSource           models.ReceiptMatchSource
			matchConfidence              string
		}{
			"posted-cafe": {"pending-cafe", "12", models.ReceiptMatchSourceReceiptMatcher, "0.8"},
			// The diner posts for 46.00 with a tip, more than the receipt's
			// 42.17: the matcher's whole-charge match rises to the posted
			// amount only as far as the receipt's total.
			"posted-diner":         {"pending-diner", "42.17", models.ReceiptMatchSourceReceiptMatcher, "0.95"},
			"posted-hardware":      {"pending-hardware", "30", models.ReceiptMatchSourcePerson, ""},
			"simple-posted-bakery": {"simple-pending-bakery", "8.5", models.ReceiptMatchSourceReceiptMatcher, "0.7"},
		} {
			matches := receiptMatchesOf(t, tx, fixture.agentId, found[postedProviderTransactionId].ID)
			if len(matches) != 1 {
				t.Errorf("%s carries the one receipt match: %+v", postedProviderTransactionId, matches)
				continue
			}
			match := matches[0]
			isConfidenceKept := (want.matchConfidence == "" && match.MatchConfidence == "") || isSameDecimal(match.MatchConfidence, want.matchConfidence)
			if match.ReceiptID != receiptIdByProviderTransactionId[want.pendingProviderTransactionId] || !isSameDecimal(match.MatchedAmount, want.matchedAmount) ||
				match.ReceiptMatchSource != want.receiptMatchSource || !isConfidenceKept {
				t.Errorf("%s carries the match %+v: %+v", postedProviderTransactionId, want, match)
			}
		}
		if matches := receiptMatchesOf(t, tx, fixture.agentId, found["posted-florist"].ID); len(matches) != 0 {
			t.Errorf("a pending charge with no receipt hands none on: %+v", matches)
		}
	})
}

// When the person writes an annotation while the agent's write of one is
// waiting on the same row, the agent's guarded statement changes nothing
// once the person's commits: it answers not written, and the audit log
// keeps the person's write alone.
func TestAnnotationTheAgentLosesToThePersonIsNotAudited(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	var grocerId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		grocerId = financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"].ID
	})
	auditCount := func() string {
		return dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM "audit_event" WHERE "resource_id" = '`+grocerId+`'`)
	}
	isPersonWritten, isPersonCommitting := make(chan struct{}), make(chan struct{})
	personDone := make(chan error, 1)
	go func() {
		personDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.SetFinanceTransactionAnnotation(fixture.agentId, grocerId, "party supplies", models.AnnotatedByPerson, false); err != nil {
				return err
			}
			close(isPersonWritten)
			<-isPersonCommitting
			return nil
		})
	}()
	<-isPersonWritten
	type agentOutcome struct {
		isWritten bool
		err       error
	}
	agentDone := make(chan agentOutcome, 1)
	go func() {
		outcome := agentOutcome{}
		outcome.err = database.Transaction(func(tx db.Transaction) error {
			var err error
			outcome.isWritten, err = tx.SetFinanceTransactionAnnotation(fixture.agentId, grocerId, "groceries", models.AnnotatedByAgent, false)
			return err
		})
		agentDone <- outcome
	}()
	for attempt := 0; dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock'`) == "0"; attempt++ {
		if attempt > 1000 {
			t.Fatal("the agent's write never waited on the person's")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(isPersonCommitting)
	if err := <-personDone; err != nil {
		t.Fatalf("the person's write: %s", err)
	}
	outcome := <-agentDone
	if outcome.err != nil || outcome.isWritten {
		t.Fatalf("the agent's write over the person's is not written: %+v", outcome)
	}
	if count := auditCount(); count != "1" {
		t.Fatalf("only the person's write is audited: %s events", count)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if read, _ := tx.GetFinanceTransaction(fixture.agentId, grocerId); read.Annotation != "party supplies" || read.AnnotatedBy != models.AnnotatedByPerson {
			t.Fatalf("the person's annotation stands: %+v", read)
		}
	})
}

// Receipts page by cursor or by offset, the newest purchase first and the
// receipts that print no day after every dated one, so every receipt is
// reached; isUndated lists only those, and cannot go with a range.
func TestFinanceReceiptsPageThroughTheUndated(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	type stored struct {
		id, purchasedOn string
	}
	var receipts []stored
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for index, purchasedOn := range []string{"2026-09-10", "2026-09-12", "", "2026-09-12", ""} {
			receipt := inventedGroceryReceipt(fixture.agentId, "mail-page-"+string(rune('a'+index)))
			receipt.PurchasedOn = purchasedOn
			receipts = append(receipts, stored{putReceipt(t, tx, receipt).ID, purchasedOn})
		}
	})
	sort.Slice(receipts, func(left, right int) bool {
		if (receipts[left].purchasedOn == "") != (receipts[right].purchasedOn == "") {
			return receipts[right].purchasedOn == ""
		}
		if receipts[left].purchasedOn != receipts[right].purchasedOn {
			return receipts[left].purchasedOn > receipts[right].purchasedOn
		}
		return receipts[left].id > receipts[right].id
	})
	wantIds := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		wantIds = append(wantIds, receipt.id)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var byCursor, byOffset []string
		after := ""
		for pageCount := 0; pageCount < 5; pageCount++ {
			page, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{Limit: 2, After: after, ShouldCountTotal: true})
			if err != nil {
				t.Fatalf("ListFinanceReceipts after %q: %s", after, err)
			}
			if page.TotalCount != 5 {
				t.Fatalf("every page counts all five: %d", page.TotalCount)
			}
			for _, receipt := range page.FinanceReceipts {
				byCursor = append(byCursor, receipt.ID)
			}
			if after = page.NextCursor; after == "" {
				break
			}
		}
		for offset := 0; offset < 5; offset += 2 {
			page, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{Limit: 2, Offset: offset})
			if err != nil {
				t.Fatalf("ListFinanceReceipts at %d: %s", offset, err)
			}
			for _, receipt := range page.FinanceReceipts {
				byOffset = append(byOffset, receipt.ID)
			}
		}
		for name, gotIds := range map[string][]string{"by cursor": byCursor, "by offset": byOffset} {
			if len(gotIds) != len(wantIds) {
				t.Fatalf("%s reaches every receipt: %v, want %v", name, gotIds, wantIds)
			}
			for index := range wantIds {
				if gotIds[index] != wantIds[index] {
					t.Fatalf("%s pages newest first, undated last: %v, want %v", name, gotIds, wantIds)
				}
			}
		}
		undated, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{IsUndated: true, ShouldCountTotal: true})
		if err != nil || undated.TotalCount != 2 || len(undated.FinanceReceipts) != 2 || undated.FinanceReceipts[0].PurchasedOn != "" {
			t.Fatalf("isUndated lists the two that print no day: %+v %v", undated, err)
		}
		dated, err := listReceipts(t, tx, fixture.agentId, &db.FinanceReceiptFilter{From: "2026-09-01", To: "2026-09-30"})
		if err != nil || len(dated) != 3 {
			t.Fatalf("a range leaves the undated out: %d %v", len(dated), err)
		}
		if _, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{IsUndated: true, From: "2026-09-01"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("isUndated with a range is refused: %v", err)
		}
		if _, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{After: "yesterday"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a cursor that is not one is refused: %v", err)
		}
	})
}

// A match must fit its charge: the receipt's currency, money out, no finer
// than the currency counts, and, with every other receipt's match on it,
// no more than the charge took. Matching a receipt again replaces its own
// amount rather than adding to it.
func TestFinanceReceiptMatchFitsTheCharge(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		byProvider := financeTransactionsByProviderId(t, tx, fixture.agentId)
		grocer, salary := byProvider["transaction-grocer"], byProvider["transaction-salary"]
		match := func(receiptId, financeTransactionId, matchedAmount string) error {
			_, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
				ReceiptID: receiptId, FinanceTransactionID: financeTransactionId, MatchedAmount: matchedAmount, ReceiptMatchSource: models.ReceiptMatchSourcePerson,
			})
			return err
		}
		inEuros := inventedGroceryReceipt(fixture.agentId, "mail-in-euros")
		inEuros.CurrencyCode = "EUR"
		euroReceipt := putReceipt(t, tx, inEuros)
		orderEmail := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-order"))
		shippingEmail := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-shipped"))
		for name, refused := range map[string]error{
			"another currency":  match(euroReceipt.ID, grocer.ID, "10.00"),
			"money in":          match(orderEmail.ID, salary.ID, "10.00"),
			"finer than a cent": match(orderEmail.ID, grocer.ID, "10.001"),
			"more than it took": match(orderEmail.ID, grocer.ID, "42.18"),
			"another receipt's room": func() error {
				_ = match(orderEmail.ID, grocer.ID, "30.00")
				return match(shippingEmail.ID, grocer.ID, "20.00")
			}(),
		} {
			if !errors.Is(refused, db.ErrInvalidArguments) {
				t.Errorf("a match to %s is refused: %v", name, refused)
			}
		}
		if err := match(shippingEmail.ID, grocer.ID, "12.17"); err != nil {
			t.Fatalf("what is left of the charge is matched: %s", err)
		}
		if err := match(orderEmail.ID, grocer.ID, "30.00"); err != nil {
			t.Fatalf("matching a receipt again replaces its own amount: %s", err)
		}
		coverage, err := tx.FinanceReceiptMatchCoverage(fixture.agentId, shippingEmail.ID, []string{grocer.ID, salary.ID})
		if err != nil {
			t.Fatalf("FinanceReceiptMatchCoverage: %s", err)
		}
		ofGrocer := coverage[grocer.ID]
		if len(coverage) != 1 || ofGrocer == nil || !isSameDecimal(ofGrocer.MatchedAmount, "42.17") ||
			!isSameDecimal(ofGrocer.OtherMatchedAmount, "30") || !ofGrocer.HasOtherReceipt {
			t.Fatalf("the grocer charge is explained by both receipts, 30 of it by the other: %+v", coverage)
		}
	})
}

// One receipt explains at most its total across all its charges: with
// part of it matched to one charge, a match to another beyond what is
// left is refused, what is left is matched, and matching that charge
// again for a cent more is refused while its own amount again is not.
func TestFinanceReceiptMatchesFitTheReceiptsTotal(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		byProvider := financeTransactionsByProviderId(t, tx, fixture.agentId)
		grocer, diner := byProvider["transaction-grocer"], byProvider["transaction-diner"]
		match := func(receiptId, financeTransactionId, matchedAmount string) error {
			_, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
				ReceiptID: receiptId, FinanceTransactionID: financeTransactionId, MatchedAmount: matchedAmount, ReceiptMatchSource: models.ReceiptMatchSourcePerson,
			})
			return err
		}
		receipt := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-two-charges"))
		if err := match(receipt.ID, diner.ID, "18.40"); err != nil {
			t.Fatalf("the diner charge is matched: %s", err)
		}
		if err := match(receipt.ID, grocer.ID, "30.00"); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("18.40 and 30.00 is more than the receipt's 42.17, refused: %v", err)
		}
		if err := match(receipt.ID, grocer.ID, "23.77"); err != nil {
			t.Fatalf("what is left of the receipt is matched: %s", err)
		}
		if err := match(receipt.ID, grocer.ID, "23.78"); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("matching the grocer again for a cent more than the receipt leaves is refused: %v", err)
		}
		if err := match(receipt.ID, grocer.ID, "23.77"); err != nil {
			t.Fatalf("matching the grocer again for its own amount fits: %s", err)
		}
	})
}

// A pending charge that posts for less keeps its carried receipt matches
// within what it took: the person's first, then the matcher's, which is
// cut to what is left, or taken off when nothing is.
func TestCarriedReceiptMatchesFitALowerPostedAmount(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "finance-carried-cap")
	accounts := sampleFinanceSync().Accounts
	pending := func(providerTransactionId string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: "2026-09-10",
			Amount: "-40.00", CurrencyCode: "USD", Description: "OUTFITTER", IsPending: true}
	}
	posted := func(providerTransactionId, pendingProviderTransactionId, amount string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: "2026-09-12",
			Amount: amount, CurrencyCode: "USD", Description: "OUTFITTER", PendingProviderTransactionID: pendingProviderTransactionId}
	}
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		pending("pending-lower"), pending("pending-lowest"),
	}}, "2026-09-11")
	receiptIds := map[string]string{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for _, providerTransactionId := range []string{"pending-lower", "pending-lowest"} {
			for matchIndex, match := range []models.FinanceReceiptMatch{
				{MatchedAmount: "25.00", ReceiptMatchSource: models.ReceiptMatchSourcePerson},
				{MatchedAmount: "15.00", ReceiptMatchSource: models.ReceiptMatchSourceReceiptMatcher, MatchConfidence: "0.85"},
			} {
				receipt := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, fmt.Sprintf("mail-%s-%d", providerTransactionId, matchIndex)))
				receiptIds[providerTransactionId+"/"+string(match.ReceiptMatchSource)] = receipt.ID
				match.ReceiptID, match.FinanceTransactionID = receipt.ID, found[providerTransactionId].ID
				if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &match); err != nil {
					t.Fatalf("PutFinanceReceiptMatch: %s", err)
				}
			}
		}
	})
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		posted("posted-lower", "pending-lower", "-30.00"), posted("posted-lowest", "pending-lowest", "-20.00"),
	}, RemovedProviderTransactionIDs: []string{"pending-lower", "pending-lowest"}}, "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for postedProviderTransactionId, want := range map[string]map[string]string{
			"posted-lower":  {receiptIds["pending-lower/person"]: "25", receiptIds["pending-lower/receipt_matcher"]: "5"},
			"posted-lowest": {receiptIds["pending-lowest/person"]: "20"},
		} {
			matches := receiptMatchesOf(t, tx, fixture.agentId, found[postedProviderTransactionId].ID)
			if len(matches) != len(want) {
				t.Errorf("%s keeps %d matches: %+v", postedProviderTransactionId, len(want), matches)
				continue
			}
			for _, match := range matches {
				if !isSameDecimal(match.MatchedAmount, want[match.ReceiptID]) {
					t.Errorf("%s: the match of %s explains %s, not %s", postedProviderTransactionId, match.ReceiptID, want[match.ReceiptID], match.MatchedAmount)
				}
			}
		}
	})
}

// An upload waiting to be read as a finance transaction's receipt is kept
// by the sweep while that finance transaction exists, and swept once it
// is gone.
func TestReceiptUploadWaitingToBeReadIsNotAnOrphan(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	var upload *models.AgentAttachment
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if upload, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agentId, Name: "receipt.jpg", ContentType: "image/jpeg", Size: 10}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
		grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["transaction-grocer"]
		if err := tx.SetAgentAttachmentFinanceTransaction(fixture.agentId, upload.ID, grocer.ID); err != nil {
			t.Fatalf("SetAgentAttachmentFinanceTransaction: %s", err)
		}
		if orphans, err := tx.ListOrphanAgentAttachments(time.Now().Add(time.Hour)); err != nil || len(orphans) != 0 {
			t.Fatalf("the upload waiting to be read is kept: %+v %v", orphans, err)
		}
		accounts, err := tx.ListFinanceAccounts(fixture.agentId, "")
		if err != nil {
			t.Fatalf("ListFinanceAccounts: %s", err)
		}
		for _, account := range accounts {
			if account.ProviderAccountID == "account-checking" {
				if _, err := tx.DeleteFinanceAccount(fixture.agentId, account.ID); err != nil {
					t.Fatalf("DeleteFinanceAccount: %s", err)
				}
			}
		}
		if orphans, err := tx.ListOrphanAgentAttachments(time.Now().Add(time.Hour)); err != nil || len(orphans) != 1 || orphans[0].ID != upload.ID {
			t.Fatalf("with its finance transaction gone, it is swept: %+v %v", orphans, err)
		}
	})
}

// Deleting a conversation leaves a receipt's photo to the receipt: it is
// taken out of the conversation, and deleting the receipt later deletes
// it, as with any upload no message holds.
func TestDetachReceiptAttachmentsLeavesThePhotoToItsReceipt(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		createAttachment := func(name string) *models.AgentAttachment {
			attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agentId, Name: name, ContentType: "image/jpeg", Size: 10})
			if err != nil {
				t.Fatalf("CreateAgentAttachment: %s", err)
			}
			return attachment
		}
		photo, other := createAttachment("receipt.jpg"), createAttachment("view.jpg")
		if err := tx.ClaimAgentAttachments([]string{photo.ID, other.ID}, "conversation-one", "message-one"); err != nil {
			t.Fatalf("ClaimAgentAttachments: %s", err)
		}
		fromPhoto := inventedGroceryReceipt(fixture.agentId, "")
		fromPhoto.ReceiptSourceKind, fromPhoto.MailID, fromPhoto.AgentAttachmentID = models.ReceiptSourceKindAttachment, "", photo.ID
		receipt := putReceipt(t, tx, fromPhoto)
		if err := tx.DetachReceiptAttachments(fixture.agentId, "conversation-one"); err != nil {
			t.Fatalf("DetachReceiptAttachments: %s", err)
		}
		inConversation, err := tx.ListAgentAttachments(fixture.agentId, "conversation-one")
		if err != nil || len(inConversation) != 1 || inConversation[0].ID != other.ID {
			t.Fatalf("only the file no receipt holds is left in the conversation: %+v %v", inConversation, err)
		}
		if detached, _ := tx.GetAgentAttachment(photo.ID); detached == nil || detached.ConversationID != "" || detached.MessageID != "" {
			t.Fatalf("the photo names no conversation or message: %+v", detached)
		}
		deleted, err := tx.DeleteFinanceReceipt(fixture.agentId, receipt.ID)
		if err != nil || deleted.DeletedAgentAttachmentID != photo.ID {
			t.Fatalf("deleting the receipt deletes its photo: %+v %v", deleted, err)
		}
		if err := tx.DetachReceiptAttachments(fixture.agentId, ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("detaching needs a conversation: %v", err)
		}
	})
}

// A receipt read again with a lower total, or in another currency, keeps
// the matches it still explains and drops the rest, saying why: the
// oldest match is kept first, and a later one that no longer fits beside
// it goes.
func TestDropUnfittingFinanceReceiptMatchesKeepsWhatStillFits(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		byProvider := financeTransactionsByProviderId(t, tx, fixture.agentId)
		grocer, diner := byProvider["transaction-grocer"], byProvider["transaction-diner"]
		receipt := putReceipt(t, tx, inventedGroceryReceipt(fixture.agentId, "mail-read-again"))
		for _, match := range []struct{ financeTransactionId, matchedAmount string }{{diner.ID, "18.40"}, {grocer.ID, "23.77"}} {
			if _, err := tx.PutFinanceReceiptMatch(fixture.agentId, &models.FinanceReceiptMatch{
				ReceiptID: receipt.ID, FinanceTransactionID: match.financeTransactionId, MatchedAmount: match.matchedAmount,
				ReceiptMatchSource: models.ReceiptMatchSourcePerson,
			}); err != nil {
				t.Fatalf("PutFinanceReceiptMatch: %s", err)
			}
		}
		dropped, err := tx.DropUnfittingFinanceReceiptMatches(fixture.agentId, receipt.ID)
		if err != nil || len(dropped) != 0 {
			t.Fatalf("matches that fit are all kept: %+v %v", dropped, err)
		}

		lower := inventedGroceryReceipt(fixture.agentId, "mail-read-again")
		lower.TotalAmount = "25.00"
		putReceipt(t, tx, lower)
		dropped, err = tx.DropUnfittingFinanceReceiptMatches(fixture.agentId, receipt.ID)
		if err != nil || len(dropped) != 1 || dropped[0].ReceiptMatch.FinanceTransactionID != grocer.ID || dropped[0].DropReason == "" {
			t.Fatalf("the later match no longer fits the lower total beside the earlier one: %+v %v", dropped, err)
		}
		stored, err := tx.GetFinanceReceipt(fixture.agentId, receipt.ID)
		if err != nil || len(stored.ReceiptMatches) != 1 || stored.ReceiptMatches[0].FinanceTransactionID != diner.ID ||
			!isSameDecimal(stored.ReceiptMatches[0].MatchedAmount, "18.40") {
			t.Fatalf("the earlier match stays as it was: %+v %v", stored, err)
		}

		inEuros := inventedGroceryReceipt(fixture.agentId, "mail-read-again")
		inEuros.CurrencyCode = "EUR"
		putReceipt(t, tx, inEuros)
		dropped, err = tx.DropUnfittingFinanceReceiptMatches(fixture.agentId, receipt.ID)
		if err != nil || len(dropped) != 1 || dropped[0].ReceiptMatch.FinanceTransactionID != diner.ID {
			t.Fatalf("a match in another currency is dropped: %+v %v", dropped, err)
		}
		if stored, _ := tx.GetFinanceReceipt(fixture.agentId, receipt.ID); len(stored.ReceiptMatches) != 0 {
			t.Fatalf("nothing is left matched: %+v", stored.ReceiptMatches)
		}
	})
}

// An amount or a quantity larger than its column holds is refused as an
// argument, not left to fail the statement.
func TestPutFinanceReceiptRefusesWhatItsColumnsCannotHold(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for name, broken := range map[string]func(*models.FinanceReceipt){
			"a total of fifteen digits": func(receipt *models.FinanceReceipt) { receipt.TotalAmount = "1000000000000000" },
			"a negative line amount":    func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[2].LineAmount = "-1000000000000000.00" },
			"a subtotal rounding up":    func(receipt *models.FinanceReceipt) { receipt.SubtotalAmount = "999999999999999.99999" },
			"a unit price":              func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[1].UnitPriceAmount = "5000000000000000" },
			"a quantity":                func(receipt *models.FinanceReceipt) { receipt.ReceiptLines[1].Quantity = "10000000000000000" },
			"a check difference": func(receipt *models.FinanceReceipt) {
				receipt.ReceiptCheckState, receipt.CheckDifferenceAmount = models.ReceiptCheckStateUnbalanced, "2000000000000000"
			},
		} {
			receipt := inventedGroceryReceipt(fixture.agentId, "mail-too-large")
			broken(receipt)
			if _, err := tx.PutFinanceReceipt(receipt); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s too large is refused: %v", name, err)
			}
		}
		largest := inventedGroceryReceipt(fixture.agentId, "mail-largest")
		largest.TotalAmount, largest.ReceiptLines[1].Quantity = "999999999999999.9999", "9999999999999999.99999999"
		if stored, err := tx.PutFinanceReceipt(largest); err != nil || stored.TotalAmount != "999999999999999.9999" {
			t.Fatalf("the largest the columns hold is kept: %+v %v", stored, err)
		}
	})
}

// Two first reads of one source finishing together write one receipt:
// the second waits for the first and replaces it, rather than failing on
// the unique index.
func TestPutFinanceReceiptFirstReadsOfOneSourceWriteOneReceipt(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	isFirstWritten, isFirstCommitting := make(chan struct{}), make(chan struct{})
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() {
		firstDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.PutFinanceReceipt(inventedGroceryReceipt(fixture.agentId, "mail-read-twice")); err != nil {
				return err
			}
			close(isFirstWritten)
			<-isFirstCommitting
			return nil
		})
	}()
	<-isFirstWritten
	go func() {
		secondDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.PutFinanceReceipt(inventedGroceryReceipt(fixture.agentId, "mail-read-twice"))
			return err
		})
	}()
	for attempt := 0; dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock'`) == "0"; attempt++ {
		if attempt > 1000 {
			t.Fatal("the second read never waited on the first")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(isFirstCommitting)
	if err := <-firstDone; err != nil {
		t.Fatalf("the first read: %s", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("the second read replaces the first: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if listed, err := listReceipts(t, tx, fixture.agentId, nil); err != nil || len(listed) != 1 {
			t.Fatalf("one receipt for one source: %d %v", len(listed), err)
		}
	})
}

// Text finds a receipt by its merchant, its receipt number or a word of
// any line, in any case, counted the same; a percent sign is a percent
// sign, not a wildcard; another agent's receipts are never found.
func TestFinanceReceiptsFindByText(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "sparrow")
	stranger := createFinanceFixture(t, database, "wren")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		grocery := inventedGroceryReceipt(fixture.agentId, "mail-text-grocery")
		grocery.MerchantReceiptNumber = "R-4471"
		putReceipt(t, tx, grocery)
		hardware := inventedGroceryReceipt(fixture.agentId, "mail-text-hardware")
		hardware.MerchantName = "Hilltop Hardware"
		hardware.ReceiptLines[3].Description = "WOOD GLUE"
		putReceipt(t, tx, hardware)
		putReceipt(t, tx, inventedGroceryReceipt(stranger.agentId, "mail-text-stranger"))
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := func(text string) []string {
			t.Helper()
			page, err := tx.ListFinanceReceipts(fixture.agentId, &db.FinanceReceiptFilter{Text: text, ShouldCountTotal: true})
			if err != nil {
				t.Fatalf("ListFinanceReceipts %q: %s", text, err)
			}
			if page.TotalCount != len(page.FinanceReceipts) {
				t.Fatalf("%q counts %d but lists %d", text, page.TotalCount, len(page.FinanceReceipts))
			}
			names := []string{}
			for _, receipt := range page.FinanceReceipts {
				names = append(names, receipt.MerchantName)
			}
			sort.Strings(names)
			return names
		}
		for text, want := range map[string]string{
			"hilltop":   "Hilltop Hardware",
			"r-4471":    "Corner Grocer",
			"wood glue": "Hilltop Hardware",
			"dish soap": "Corner Grocer",
			"bag fee":   "Corner Grocer,Hilltop Hardware",
			"100%":      "",
		} {
			if got := strings.Join(found(text), ","); got != want {
				t.Fatalf("%q finds %q, want %q", text, got, want)
			}
		}
	})
}
