package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// inventedReceiptAnswer is the model's reading of an invented grocery
// receipt of 16.97, one amount written as a JSON number.
const inventedReceiptAnswer = `{"isPurchase": true, "merchantName": "Maple Street Market", "purchasedOn": "2026-09-10", "currencyCode": "USD",
"subtotalAmount": "16.29", "totalAmount": 16.97, "paymentAccountMask": "4821", "receiptLines": [
 {"lineNumber": 1, "receiptLineKind": "item", "description": "CHICKEN STOCK", "lineAmount": "3.29", "taxClassCode": "t"},
 {"lineNumber": 2, "receiptLineKind": "item", "description": "TOOTHPASTE", "lineAmount": "6.99", "taxClassCode": "T"},
 {"lineNumber": 3, "receiptLineKind": "item", "description": "PEACHES", "lineAmount": "3.99", "taxClassCode": "t"},
 {"lineNumber": 4, "receiptLineKind": "discount", "description": "Promotion", "lineAmount": "-2.00", "taxClassCode": "t", "discountedLineNumber": 3},
 {"lineNumber": 5, "receiptLineKind": "item", "description": "ONIONS", "quantity": "2.53", "quantityUnit": "lb", "unitPriceAmount": "1.59", "lineAmount": "4.02", "taxClassCode": "t"},
 {"lineNumber": 6, "receiptLineKind": "tax", "description": "Sales Tax", "lineAmount": "0.49", "taxClassCode": "T"},
 {"lineNumber": 7, "receiptLineKind": "tax", "description": "Food Tax", "lineAmount": "0.19", "taxClassCode": "t"}]}`

// inventedReceiptEmail is the same receipt as an email's text.
const inventedReceiptEmail = "Thank you for shopping with us.\n\nCHICKEN STOCK 3.29 t\nTOOTHPASTE 6.99 T\nPEACHES 3.99 t\n  Promotion -2.00 t\n" +
	"ONIONS 2.53 lb @ 1.59/lb 4.02 t\nSUBTOTAL 16.29\nSales Tax 0.49\nFood Tax 0.19\nTOTAL 16.97\nCARD ****4821\n"

// receiptJobFixture is a person with a finance account holding the
// grocery charge, a model answering to order, and storage.
func receiptJobFixture(t *testing.T, answers ...string) (*financeFixture, *alertModel) {
	t.Helper()
	model := &alertModel{answers: answers}
	server := model.serve(t)
	fixture := newFinanceFixture(t, server.URL)
	fixture.applySync(t, &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()},
		Added: []finance.Transaction{
			inventedTransaction("grocery-charge", "2026-09-11", "-16.97", "MAPLE STREET MKT 012", "", ""),
			inventedTransaction("other-charge", "2026-09-12", "-4.50", "CORNER CAFE", "", ""),
		},
	})
	return fixture, model
}

// receiptJobRun is a run of the receipt job for a subject.
func (self *financeFixture) receiptJobRun(mailbox *models.Mailbox, subjectId string) *Run {
	run := self.run()
	run.Job = &models.AgentJob{ID: "job-receipt", AgentID: self.agent.ID, Kind: models.AgentJobReadReceipt, SubjectID: subjectId}
	if mailbox != nil {
		run.Job.MailboxID, run.Mailbox, run.Source = mailbox.ID, mailbox, mailbox.Agent
	}
	return run
}

// receipts is every receipt of the person.
func (self *financeFixture) receipts(t *testing.T) []*models.FinanceReceipt {
	t.Helper()
	var receipts []*models.FinanceReceipt
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceReceipts(self.agent.ID, nil)
		if err != nil {
			t.Fatalf("ListFinanceReceipts: %s", err)
		}
		receipts = page.FinanceReceipts
	})
	return receipts
}

// runTitles are the titles of the receipt job's runs.
func (self *financeFixture) runTitles(t *testing.T) []string {
	t.Helper()
	var titles []string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(self.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatalf("ListAgentConversations: %s", err)
		}
		for _, conversation := range conversations {
			if conversation.JobKind == string(models.AgentJobReadReceipt) {
				titles = append(titles, conversation.Title)
			}
		}
	})
	return titles
}

// A receipt email sorting filed is read by the model, recorded with the
// same check and matching the tool's go through, and matched to the one
// charge of its amount; the run says what it read and matched. Run again
// on the same message, it replaces the receipt rather than adding one.
// What the model is sent is the message inside the untrusted fence.
func TestReceiptJobReadsAMessageAndMatchesIt(t *testing.T) {
	fixture, model := receiptJobFixture(t, inventedReceiptAnswer)
	var mailbox *models.Mailbox
	var mail *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "Your receipt", From: "receipts@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
	})
	if err := fixture.worker.settings.Storage.Put(t.Context(), mail.ID, []string{"From: receipts@example.com", "Subject: Your receipt", "Content-Type: text/plain"}, []byte(inventedReceiptEmail)); err != nil {
		t.Fatalf("Put: %s", err)
	}
	for range 2 {
		if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(mailbox, mail.ID)); err != nil {
			t.Fatalf("runReadReceipt: %s", err)
		}
	}
	receipts := fixture.receipts(t)
	if len(receipts) != 1 {
		t.Fatalf("reading the message twice keeps one receipt: %d", len(receipts))
	}
	receipt := receipts[0]
	if receipt.MailID != mail.ID || receipt.ReceiptCheckState != models.ReceiptCheckStateBalanced || len(receipt.ReceiptLines) != 7 || receipt.TotalAmount != "16.9700" {
		t.Fatalf("the receipt is recorded from the message, balanced: %+v", receipt)
	}
	charge := fixture.transactions(t)["MAPLE STREET MKT 012"]
	if len(receipt.ReceiptMatches) != 1 || receipt.ReceiptMatches[0].FinanceTransactionID != charge.ID ||
		receipt.ReceiptMatches[0].ReceiptMatchSource != models.ReceiptMatchSourceReceiptMatcher {
		t.Fatalf("it is matched to the grocery charge by the matcher: %+v", receipt.ReceiptMatches)
	}
	titles := fixture.runTitles(t)
	if len(titles) != 2 || !strings.Contains(titles[0], "Read a receipt from Maple Street Market for 16.97 USD: matched to the charge of 16.97") {
		t.Fatalf("the run says what it read and matched: %q", titles)
	}
	// The request is JSON, so the fence's angle brackets come escaped.
	if model.callCount() != 2 || !strings.Contains(model.prompts[0], "untrusted-data") || !strings.Contains(model.prompts[0], "TOOTHPASTE 6.99 T") {
		t.Fatalf("the model is sent the message inside the fence")
	}
}

// A message that is not a purchase records nothing, and says so.
func TestReceiptJobRecordsNothingForANonPurchase(t *testing.T) {
	fixture, _ := receiptJobFixture(t, `{"isPurchase": false}`)
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "shipping.txt", ContentType: "text/plain", Size: 40,
			Text: "Your order has shipped and arrives on Thursday."}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(nil, attachment.ID)); err != nil {
		t.Fatalf("runReadReceipt: %s", err)
	}
	if receipts := fixture.receipts(t); len(receipts) != 0 {
		t.Fatalf("nothing is recorded: %+v", receipts)
	}
	if titles := fixture.runTitles(t); len(titles) != 1 || !strings.Contains(titles[0], "not a purchase") {
		t.Fatalf("the run says it was not a purchase: %q", titles)
	}
}

// A text file uploaded to a charge is matched to that charge, by hand,
// even when another charge has the receipt's amount; one whose lines miss
// is kept, marked unbalanced, since nobody is there to ask.
func TestReceiptJobMatchesAnUploadToItsCharge(t *testing.T) {
	misread := strings.Replace(inventedReceiptAnswer, `"lineAmount": "3.99"`, `"lineAmount": "3.89"`, 1)
	fixture, _ := receiptJobFixture(t, misread)
	other := fixture.transactions(t)["CORNER CAFE"]
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "receipt.txt", ContentType: "text/plain", Size: 200,
			Text: inventedReceiptEmail}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		job, err := fixture.worker.QueueReceiptReading(tx, fixture.agent.ID, "", attachment.ID, other.ID)
		if err != nil || job.SubjectID != attachment.ID {
			t.Fatalf("QueueReceiptReading: %+v %v", job, err)
		}
	})
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(nil, attachment.ID)); err != nil {
		t.Fatalf("runReadReceipt: %s", err)
	}
	receipts := fixture.receipts(t)
	if len(receipts) != 1 || receipts[0].AgentAttachmentID != attachment.ID || receipts[0].ReceiptCheckState != models.ReceiptCheckStateUnbalanced {
		t.Fatalf("the upload's receipt is kept, marked unbalanced: %+v", receipts)
	}
	if matches := receipts[0].ReceiptMatches; len(matches) != 1 || matches[0].FinanceTransactionID != other.ID || matches[0].ReceiptMatchSource != models.ReceiptMatchSourcePerson {
		t.Fatalf("it is matched to the charge it was uploaded to, by hand: %+v", matches)
	}
	if titles := fixture.runTitles(t); len(titles) != 1 || !strings.Contains(titles[0], "the lines come to 0.10 USD less than printed") {
		t.Fatalf("the run says it does not add up: %q", titles)
	}
}

// A photo uploaded to a charge an email receipt already explains is still
// recorded, unmatched, and the run says why it was not matched to that
// charge, rather than the reading being dropped.
func TestReceiptJobKeepsAnUploadToAnExplainedChargeUnmatched(t *testing.T) {
	fixture, _ := receiptJobFixture(t, inventedReceiptAnswer, inventedReceiptAnswer)
	var mailbox *models.Mailbox
	var mail *models.Mail
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "Your receipt", From: "receipts@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "receipt.txt", ContentType: "text/plain", Size: 200,
			Text: inventedReceiptEmail}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := fixture.worker.settings.Storage.Put(t.Context(), mail.ID, []string{"From: receipts@example.com", "Subject: Your receipt", "Content-Type: text/plain"}, []byte(inventedReceiptEmail)); err != nil {
		t.Fatalf("Put: %s", err)
	}
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(mailbox, mail.ID)); err != nil {
		t.Fatalf("runReadReceipt for the email: %s", err)
	}
	charge := fixture.transactions(t)["MAPLE STREET MKT 012"]
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := fixture.worker.QueueReceiptReading(tx, fixture.agent.ID, "", attachment.ID, charge.ID); err != nil {
			t.Fatalf("QueueReceiptReading: %s", err)
		}
	})
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(nil, attachment.ID)); err != nil {
		t.Fatalf("runReadReceipt for the upload: %s", err)
	}
	var fromEmail, fromUpload *models.FinanceReceipt
	for _, receipt := range fixture.receipts(t) {
		switch {
		case receipt.MailID == mail.ID:
			fromEmail = receipt
		case receipt.AgentAttachmentID == attachment.ID:
			fromUpload = receipt
		}
	}
	if fromEmail == nil || len(fromEmail.ReceiptMatches) != 1 || fromEmail.ReceiptMatches[0].FinanceTransactionID != charge.ID {
		t.Fatalf("the email explains the charge: %+v", fromEmail)
	}
	if fromUpload == nil || len(fromUpload.ReceiptMatches) != 0 {
		t.Fatalf("the upload's receipt is recorded, unmatched: %+v", fromUpload)
	}
	titles := fixture.runTitles(t)
	isReasonSaid := false
	for _, title := range titles {
		isReasonSaid = isReasonSaid || (strings.Contains(title, "not matched to the charge it was uploaded to") &&
			strings.Contains(title, "other receipts already explain all of that charge"))
	}
	if !isReasonSaid {
		t.Fatalf("the run says why it was not matched to that charge: %q", titles)
	}
}

// With no model to show a picture to, the job reads nothing and its run
// says why.
func TestReceiptJobSaysWhyWithoutAModel(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "receipt.jpg", ContentType: "image/jpeg", Size: 4}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(nil, attachment.ID)); err != nil {
		t.Fatalf("runReadReceipt: %s", err)
	}
	if titles := fixture.runTitles(t); len(titles) != 1 || titles[0] != "Did not read the receipt: no model is configured to read it" {
		t.Fatalf("the run says why: %q", titles)
	}
}

// Sorting a message as a receipt queues the job, for a person with a
// finance account, unless the message is in Junk; another category, or a
// person with no finance account, queues nothing.
func TestSortingAReceiptQueuesItsReading(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	var mailbox *models.Mailbox
	var inbox, junk *models.MailboxFolder
	var kept, junked *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if inbox, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %v", inbox, err)
		}
		if junk, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk); err != nil || junk == nil {
			t.Fatalf("GetFolderByKind: %v %v", junk, err)
		}
		for _, filed := range []struct {
			mail   **models.Mail
			folder *models.MailboxFolder
		}{{&kept, inbox}, {&junked, junk}} {
			if *filed.mail, err = tx.CreateMail(&models.Mail{Subject: "Your receipt", From: "receipts@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if _, err := tx.AddItem(filed.folder.ID, (*filed.mail).ID, "", models.MailboxItemFlags{}); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
		}
	})
	run := fixture.run()
	run.Mailbox, run.Source = mailbox, mailbox.Agent
	queue := func(mail *models.Mail, category string) {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			if err := fixture.worker.queueReceiptReading(tx, run, mail, &models.MailInsight{Category: category}); err != nil {
				t.Fatalf("queueReceiptReading: %s", err)
			}
		})
	}
	queue(kept, "receipt")
	if jobs := fixture.queuedJobsOfKind(t, models.AgentJobReadReceipt); len(jobs) != 0 {
		t.Fatalf("a person with no finance account has nothing to match it to: %+v", jobs)
	}
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}})
	queue(kept, "notification")
	queue(junked, "receipt")
	queue(kept, "receipt")
	jobs := fixture.queuedJobsOfKind(t, models.AgentJobReadReceipt)
	if len(jobs) != 1 || jobs[0].SubjectID != kept.ID || jobs[0].MailboxID != mailbox.ID {
		t.Fatalf("one job, for the receipt in the inbox: %+v", jobs)
	}
}
