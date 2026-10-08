package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// inventedCornerGrocerReceipt is an invented receipt for the seeded
// grocery charge of 42.17: two items, a bag fee and a tax that add up.
func inventedCornerGrocerReceipt() RecordReceiptArguments {
	two := 2
	return RecordReceiptArguments{
		GmailMessageID: "gmail-invented-one", MerchantName: "Corner Grocer", PurchasedOn: "2026-09-11", CurrencyCode: "USD",
		SubtotalAmount: "40.17", TotalAmount: "42.17", PaymentAccountMask: "0001",
		ReceiptLines: []ReceiptLine{
			{ReceiptLineKind: "item", Description: "RICE 5LB", LineAmount: "20.00"},
			{ReceiptLineKind: "item", Description: "OLIVE OIL", LineAmount: "21.07"},
			{ReceiptLineKind: "discount", Description: "Member price", LineAmount: "-1.00", DiscountedLineNumber: &two},
			{ReceiptLineKind: "fee", Description: "BAG", LineAmount: "0.10"},
			{ReceiptLineKind: "tax", Description: "Sales Tax", LineAmount: "2.00"},
		},
	}
}

// asAgent runs one step as the owner's agent acting for them, the way the
// finance tool calls the API.
func (self *financeFixture) asAgent(test *testing.T, run func(ctx context.Context, tx db.Transaction)) {
	test.Helper()
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		run(db.ContextWithAuditPrincipal(ctx, db.AuditPrincipal{ActorKind: models.AuditActorAgent, UserID: self.owner.ID}), tx)
	})
}

// The agent annotates where the person has not, never over the person;
// the transactions page says who wrote it and how many receipts it has.
func TestAnnotateTransactionKeepsThePersonsWords(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	grocer := transactions[1]
	if grocer.Description != "CORNER GROCER 0412" {
		grocer = transactions[0]
	}
	fixture.asAgent(test, func(ctx context.Context, tx db.Transaction) {
		annotated, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "weekly shop"})
		if err != nil || annotated.AnnotatedBy != models.AnnotatedByAgent {
			test.Fatalf("the agent annotates: %+v %v", annotated, err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "party supplies"}); err != nil {
			test.Fatalf("the person annotates: %s", err)
		}
	})
	fixture.asAgent(test, func(ctx context.Context, tx db.Transaction) {
		_, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "groceries"})
		if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "the person wrote") {
			test.Fatalf("the agent is refused over the person's: %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{grocer.ID}})
		if err != nil || len(page.FinanceTransactions) != 1 {
			test.Fatalf("FinanceTransactions: %+v %v", page, err)
		}
		if read := page.FinanceTransactions[0]; read.Annotation != "party supplies" || read.AnnotatedBy != models.AnnotatedByPerson || read.ReceiptCount != 0 {
			test.Fatalf("the page carries the annotation and its author: %+v", read)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "mine"}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's transaction is not found: %v", err)
		}
	})
}

// A receipt is checked, previewed without writing, recorded, matched to
// the one charge of its exact amount, listed by that charge, read again,
// matched and unmatched by hand, and deleted; one that does not add up is
// refused until the person says to record it; another person reaches
// none of it.
func TestRecordReceiptChecksMatchesAndStaysTheCallers(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	grocer, cafe := transactions[0], transactions[1]
	if grocer.Description != "CORNER GROCER 0412" {
		grocer, cafe = cafe, grocer
	}
	var receiptId string
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		preview, err := resolver.PreviewRecordReceipt(ctx, inventedCornerGrocerReceipt())
		if err != nil {
			test.Fatalf("PreviewRecordReceipt: %s", err)
		}
		if preview.ReceiptCheckState != models.ReceiptCheckStateBalanced || len(preview.ReceiptMatchCandidates) != 1 ||
			!preview.ReceiptMatchCandidates[0].IsAutomatic || preview.ReceiptMatchCandidates[0].FinanceTransaction == nil {
			test.Fatalf("the preview balances and proposes the grocery charge: %+v", preview)
		}
		if listed, _ := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{}); len(listed.FinanceReceipts) != 0 {
			test.Fatalf("the preview wrote nothing: %+v", listed)
		}

		misread := inventedCornerGrocerReceipt()
		misread.ReceiptLines[1].LineAmount = "21.70"
		if _, err := resolver.RecordReceipt(ctx, misread); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "0.63 USD") {
			test.Fatalf("a receipt that does not add up is refused, saying by how much: %v", err)
		}

		recorded, err := resolver.RecordReceipt(ctx, inventedCornerGrocerReceipt())
		if err != nil {
			test.Fatalf("RecordReceipt: %s", err)
		}
		stored := recorded.FinanceReceipt
		receiptId = stored.ID
		if len(stored.ReceiptLines) != 5 || stored.ReceiptLines[2].DiscountedLineNumber != 2 || !strings.HasPrefix(recorded.ReceiptCheckSummary, "balanced") {
			test.Fatalf("the receipt is stored line by line: %+v", recorded)
		}
		if len(stored.ReceiptMatches) != 1 || stored.ReceiptMatches[0].FinanceTransactionID != grocer.ID ||
			stored.ReceiptMatches[0].ReceiptMatchSource != models.ReceiptMatchSourceReceiptMatcher || stored.ReceiptMatches[0].MatchedAmount != "42.1700" {
			test.Fatalf("it is matched to the one charge of its amount: %+v", stored.ReceiptMatches)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		ofGrocer, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{FinanceTransactionID: grocer.ID})
		if err != nil || len(ofGrocer.FinanceReceipts) != 1 || ofGrocer.FinanceReceipts[0].ID != receiptId || ofGrocer.TotalCount != 1 {
			test.Fatalf("the charge's receipts: %+v %v", ofGrocer, err)
		}
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceTransactionIDs: []string{grocer.ID}})
		if err != nil || page.FinanceTransactions[0].ReceiptCount != 1 {
			test.Fatalf("the charge counts its receipt: %+v %v", page, err)
		}
		again, err := resolver.RecordReceipt(ctx, inventedCornerGrocerReceipt())
		if err != nil || again.FinanceReceipt.ID != receiptId || !again.IsReplaced || len(again.FinanceReceipt.ReceiptMatches) != 1 {
			test.Fatalf("recording the same message again replaces it: %+v %v", again, err)
		}
		if _, err := resolver.MatchReceipt(ctx, MatchReceiptArguments{ReceiptID: receiptId, FinanceTransactionID: cafe.ID, MatchedAmount: "5.00"}); !errors.Is(err, api.ErrInvalidArguments) ||
			!strings.Contains(err.Error(), "in USD and the charge in EUR") {
			test.Fatalf("a charge in another currency is refused by hand too: %v", err)
		}
		if _, err := resolver.MatchReceipt(ctx, MatchReceiptArguments{ReceiptID: receiptId, FinanceTransactionID: cafe.ID, MatchedAmount: "-5"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("a matched amount below zero is refused: %v", err)
		}
		if candidates, err := resolver.ProposeReceiptMatches(ctx, ReceiptArguments{ReceiptID: receiptId}); err != nil || len(candidates) != 0 {
			test.Fatalf("a charge it explains in full is no candidate: %+v %v", candidates, err)
		}
		unmatched, err := resolver.UnmatchReceipt(ctx, UnmatchReceiptArguments{ReceiptID: receiptId, FinanceTransactionID: grocer.ID})
		if err != nil || len(unmatched.ReceiptMatches) != 0 {
			test.Fatalf("it is unmatched: %+v %v", unmatched, err)
		}
		candidates, err := resolver.ProposeReceiptMatches(ctx, ReceiptArguments{ReceiptID: receiptId})
		if err != nil || len(candidates) != 1 || candidates[0].FinanceTransactionID != grocer.ID {
			test.Fatalf("the charges it could explain, the other currency's left out: %+v %v", candidates, err)
		}
		matched, err := resolver.MatchReceipt(ctx, MatchReceiptArguments{ReceiptID: receiptId, FinanceTransactionID: grocer.ID})
		if err != nil || len(matched.ReceiptMatches) != 1 || matched.ReceiptMatches[0].ReceiptMatchSource != models.ReceiptMatchSourcePerson ||
			matched.ReceiptMatches[0].MatchedAmount != "42.1700" {
			test.Fatalf("matched again by hand, for the whole charge: %+v %v", matched, err)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.FinanceReceipt(ctx, ReceiptArguments{ReceiptID: receiptId}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person cannot read it: %v", err)
		}
		if listed, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{}); err != nil || len(listed.FinanceReceipts) != 0 {
			test.Errorf("another person lists none: %+v %v", listed, err)
		}
		if _, err := resolver.DeleteReceipt(ctx, ReceiptArguments{ReceiptID: receiptId}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person cannot delete it: %v", err)
		}
		if _, err := resolver.UnmatchReceipt(ctx, UnmatchReceiptArguments{ReceiptID: receiptId, FinanceTransactionID: grocer.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("another person cannot unmatch it: %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if isDeleted, err := resolver.DeleteReceipt(ctx, ReceiptArguments{ReceiptID: receiptId}); err != nil || !isDeleted {
			test.Fatalf("DeleteReceipt: %v %v", isDeleted, err)
		}
		accepted := inventedCornerGrocerReceipt()
		accepted.ReceiptLines[1].LineAmount = "21.70"
		isAccepted := true
		accepted.IsUnbalancedAccepted = &isAccepted
		recorded, err := resolver.RecordReceipt(ctx, accepted)
		if err != nil || recorded.FinanceReceipt.ReceiptCheckState != models.ReceiptCheckStateUnbalanced || recorded.FinanceReceipt.CheckDifferenceAmount != "0.6300" {
			test.Fatalf("recorded as it is when the person says so, marked: %+v %v", recorded, err)
		}
		twoSources := inventedCornerGrocerReceipt()
		twoSources.AgentAttachmentID = "attachment-invented"
		if _, err := resolver.RecordReceipt(ctx, twoSources); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("a receipt from two sources is refused: %v", err)
		}
	})
}

// An upload is queued for the receipt job, remembering the charge it was
// uploaded to; a file with no text the server can read is refused, and so
// is another person's upload or a charge named with a message.
func TestReadReceiptQueuesTheReceiptJob(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	var photo, document *models.AgentAttachment
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		var err error
		if photo, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "receipt.jpg", ContentType: "image/jpeg", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if document, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "receipt.pdf", ContentType: "application/pdf", Size: 4}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		reading, err := resolver.ReadReceipt(ctx, ReadReceiptArguments{AgentAttachmentID: photo.ID, FinanceTransactionID: transactions[0].ID})
		if err != nil || reading.AgentJobID == "" {
			test.Fatalf("ReadReceipt: %+v %v", reading, err)
		}
		job, err := tx.GetAgentJob(reading.AgentJobID)
		if err != nil || job == nil || job.Kind != models.AgentJobReadReceipt || job.SubjectID != photo.ID || job.MailboxID != "" {
			test.Fatalf("the receipt job is queued for the upload: %+v %v", job, err)
		}
		uploaded, err := tx.GetAgentAttachment(photo.ID)
		if err != nil || uploaded.FinanceTransactionID != transactions[0].ID {
			test.Fatalf("the upload remembers its charge: %+v %v", uploaded, err)
		}
		if _, err := resolver.ReadReceipt(ctx, ReadReceiptArguments{AgentAttachmentID: document.ID}); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "photo") {
			test.Fatalf("a PDF with no text is refused, saying what to give: %v", err)
		}
		if _, err := resolver.ReadReceipt(ctx, ReadReceiptArguments{MailboxItemID: "item-invented", FinanceTransactionID: transactions[0].ID}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("a charge named with a message is refused: %v", err)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.ReadReceipt(ctx, ReadReceiptArguments{AgentAttachmentID: photo.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's upload is not found: %v", err)
		}
	})
	// Money in is refused before anything is read: no receipt explains it.
	dbtest.Exec(test, fixture.database, `UPDATE "agent_finance_transaction" SET "amount" = 20 WHERE "id" = '`+transactions[1].ID+`'`)
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.ReadReceipt(ctx, ReadReceiptArguments{AgentAttachmentID: photo.ID, FinanceTransactionID: transactions[1].ID}); !errors.Is(err, api.ErrInvalidArguments) ||
			!strings.Contains(err.Error(), "money in") {
			test.Fatalf("an upload to money in is refused, saying why: %v", err)
		}
	})
}

// A second record of the same purchase (its shipping email after its
// order email, say) is not matched to the charge the first already
// explains: it is left for the person, the explained charge no candidate,
// and matching it by hand beyond what the charge took is refused.
func TestSecondReceiptOfOnePurchaseIsNotCountedTwice(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	grocer := transactions[0]
	if grocer.Description != "CORNER GROCER 0412" {
		grocer = transactions[1]
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, err := resolver.RecordReceipt(ctx, inventedCornerGrocerReceipt())
		if err != nil || len(first.FinanceReceipt.ReceiptMatches) != 1 {
			test.Fatalf("the order email is matched: %+v %v", first, err)
		}
		shipped := inventedCornerGrocerReceipt()
		shipped.GmailMessageID = "gmail-invented-two"
		second, err := resolver.RecordReceipt(ctx, shipped)
		if err != nil {
			test.Fatalf("RecordReceipt: %s", err)
		}
		if len(second.FinanceReceipt.ReceiptMatches) != 0 || len(second.ReceiptMatchCandidates) != 0 {
			test.Fatalf("the shipping email is not matched to the explained charge, nor offered it: %+v", second)
		}
		if _, err := resolver.MatchReceipt(ctx, MatchReceiptArguments{ReceiptID: second.FinanceReceipt.ID, FinanceTransactionID: grocer.ID}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("matching it by hand to a charge explained in full is refused: %v", err)
		}
		if _, err := resolver.MatchReceipt(ctx, MatchReceiptArguments{ReceiptID: second.FinanceReceipt.ID, FinanceTransactionID: grocer.ID, MatchedAmount: "1.00"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("even for an amount, when nothing of it is left: %v", err)
		}
	})
}

// The person replaces anything; the agent replaces the person's
// annotation only when the finance tool says the person asked, and what
// it writes is the agent's.
func TestAgentReplacesThePersonsAnnotationWhenAsked(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	grocer := transactions[0]
	isAsked, isNotAsked := true, false
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "party supplies"}); err != nil {
			test.Fatalf("the person annotates: %s", err)
		}
	})
	fixture.asAgent(test, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "snacks", IsAskedByPerson: &isNotAsked}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("not asked, the agent is refused: %v", err)
		}
		annotated, err := resolver.AnnotateTransaction(ctx, AnnotateTransactionArguments{FinanceTransactionID: grocer.ID, Annotation: "party snacks", IsAskedByPerson: &isAsked})
		if err != nil || annotated.Annotation != "party snacks" || annotated.AnnotatedBy != models.AnnotatedByAgent {
			test.Fatalf("asked, the agent replaces it as its own: %+v %v", annotated, err)
		}
	})
}

// Receipts page through the API as transactions do: limit, a cursor and
// how many there are in all.
func TestFinanceReceiptsPagesThroughTheAPI(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	one, isUndated := 1, true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		for index, purchasedOn := range []string{"2026-09-11", ""} {
			receipt := inventedCornerGrocerReceipt()
			receipt.GmailMessageID, receipt.PurchasedOn = fmt.Sprintf("gmail-invented-page-%d", index), purchasedOn
			if _, err := resolver.RecordReceipt(ctx, receipt); err != nil {
				test.Fatalf("RecordReceipt: %s", err)
			}
		}
		first, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{Limit: &one})
		if err != nil || len(first.FinanceReceipts) != 1 || first.TotalCount != 2 || first.NextCursor == "" || first.FinanceReceipts[0].PurchasedOn != "2026-09-11" {
			test.Fatalf("the first page holds the dated one and a cursor: %+v %v", first, err)
		}
		second, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{Limit: &one, After: first.NextCursor})
		if err != nil || len(second.FinanceReceipts) != 1 || second.NextCursor != "" || second.FinanceReceipts[0].PurchasedOn != "" {
			test.Fatalf("the last page holds the undated one: %+v %v", second, err)
		}
		undated, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{IsUndated: &isUndated})
		if err != nil || undated.TotalCount != 1 {
			test.Fatalf("isUndated lists the one that prints no day: %+v %v", undated, err)
		}
		if _, err := resolver.FinanceReceipts(ctx, FinanceReceiptsArguments{IsUndated: &isUndated, From: "2026-09-01"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("isUndated with a range is refused: %v", err)
		}
	})
}

// ReadReceipt refuses a message in a mailbox the person reads but has not
// granted their agent, saying to grant it, and queues one in a mailbox
// they have.
func TestReadReceiptRefusesAMailboxTheAgentIsNotGranted(test *testing.T) {
	fixture, _, mailbox := newStatementFixture(test)
	var keptBack, granted *models.MailboxItem
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		grantedMailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Shared with the agent", Agent: &models.AgentMailbox{Granted: true}})
		if err != nil {
			test.Fatal(err)
		}
		for _, placed := range []struct {
			mailboxId string
			item      **models.MailboxItem
		}{{mailbox.ID, &keptBack}, {grantedMailbox.ID, &granted}} {
			inbox, err := tx.GetFolderByKind(placed.mailboxId, models.MailboxFolderKindInbox)
			if err != nil {
				test.Fatal(err)
			}
			mail, err := tx.CreateMail(&models.Mail{From: "orders@example.net", ReceivedAt: time.Now(), Kind: models.MailKindIncoming}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if *placed.item, err = tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
		}
	})
	fixture.asReader(test, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.ReadReceipt(ctx, ReadReceiptArguments{MailboxItemID: keptBack.ID}); !errors.Is(err, api.ErrInvalidArguments) ||
			!strings.Contains(err.Error(), "not granted") {
			test.Fatalf("a mailbox kept back from the agent is refused, saying so: %v", err)
		}
		reading, err := fixture.resolver.ReadReceipt(ctx, ReadReceiptArguments{MailboxItemID: granted.ID})
		if err != nil || reading.AgentJobID == "" {
			test.Fatalf("a granted mailbox's message is queued: %+v %v", reading, err)
		}
	})
}

// Deleting a conversation leaves the photo a receipt was read from: the
// receipt keeps it, and the conversation's other files go.
func TestDeletingAConversationKeepsAReceiptsPhoto(test *testing.T) {
	fixture, _, _ := newStatementFixture(test)
	var photo, other *models.AgentAttachment
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: fixture.ownerAgent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
		if photo, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "receipt.jpg", ContentType: "image/jpeg", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if other, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "view.jpg", ContentType: "image/jpeg", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if err := tx.ClaimAgentAttachments([]string{photo.ID, other.ID}, conversation.ID, "message-invented"); err != nil {
			test.Fatal(err)
		}
	})
	var receiptId string
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		fromPhoto := inventedCornerGrocerReceipt()
		fromPhoto.GmailMessageID, fromPhoto.AgentAttachmentID = "", photo.ID
		recorded, err := fixture.resolver.RecordReceipt(ctx, fromPhoto)
		if err != nil {
			test.Fatalf("RecordReceipt: %s", err)
		}
		receiptId = recorded.FinanceReceipt.ID
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if isDeleted, err := fixture.resolver.DeleteAgentConversation(ctx, DeleteAgentConversationArguments{ConversationID: conversation.ID}); err != nil || !isDeleted {
			test.Fatalf("DeleteAgentConversation: %v %v", isDeleted, err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		receipt, err := fixture.resolver.FinanceReceipt(ctx, ReceiptArguments{ReceiptID: receiptId})
		if err != nil || receipt.AgentAttachmentID != photo.ID {
			test.Fatalf("the receipt is kept with its photo: %+v %v", receipt, err)
		}
		if kept, _ := tx.GetAgentAttachment(photo.ID); kept == nil {
			test.Fatal("the photo is kept")
		}
		if gone, _ := tx.GetAgentAttachment(other.ID); gone != nil {
			test.Fatalf("the conversation's other file goes: %+v", gone)
		}
	})
}

// A photo the person sends in a conversation is a receipt's source by the
// id the conversation names it with: the agent records from that upload
// without asking for it again. Another person's upload is not found,
// whoever names its id.
func TestRecordReceiptFromAPhotoSentInAConversation(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	grocer := transactions[0]
	if grocer.Description != "CORNER GROCER 0412" {
		grocer = transactions[1]
	}
	var photo, strangersPhoto *models.AgentAttachment
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: fixture.ownerAgent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
		if photo, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "photo.jpeg", ContentType: "image/jpeg", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if err := tx.ClaimAgentAttachments([]string{photo.ID}, conversation.ID, "message-invented"); err != nil {
			test.Fatal(err)
		}
		strangersAgent, err := tx.GetAgentByUser(fixture.stranger.ID)
		if err != nil || strangersAgent == nil {
			test.Fatalf("GetAgentByUser: %v %v", strangersAgent, err)
		}
		if strangersPhoto, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: strangersAgent.ID, Name: "photo.jpeg", ContentType: "image/jpeg", Size: 4}); err != nil {
			test.Fatal(err)
		}
	})
	fromPhoto := func(attachmentId string) RecordReceiptArguments {
		arguments := inventedCornerGrocerReceipt()
		arguments.GmailMessageID, arguments.AgentAttachmentID = "", attachmentId
		return arguments
	}
	fixture.asAgent(test, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.RecordReceipt(ctx, fromPhoto(strangersPhoto.ID)); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's upload is not found: %v", err)
		}
		if _, err := fixture.resolver.RecordReceipt(ctx, fromPhoto("")); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("a receipt from nowhere is refused: %v", err)
		}
	})
	fixture.asAgent(test, func(ctx context.Context, tx db.Transaction) {
		recorded, err := fixture.resolver.RecordReceipt(ctx, fromPhoto(photo.ID))
		if err != nil {
			test.Fatalf("RecordReceipt from the conversation's photo: %s", err)
		}
		stored := recorded.FinanceReceipt
		if stored.ReceiptSourceKind != models.ReceiptSourceKindAttachment || stored.AgentAttachmentID != photo.ID {
			test.Fatalf("the receipt names the photo it was read from: %+v", stored)
		}
		if len(stored.ReceiptMatches) != 1 || stored.ReceiptMatches[0].FinanceTransactionID != grocer.ID {
			test.Fatalf("it is matched to the charge: %+v", stored.ReceiptMatches)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.RecordReceipt(ctx, fromPhoto(photo.ID)); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("the owner's photo is not found for another person: %v", err)
		}
	})
}
