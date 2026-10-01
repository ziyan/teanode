package agent

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance/ofx"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// inventedCardStatement is an invented OFX 1.x card statement for a month:
// a purchase, a payment to the card and a refund, and the ledger balance
// owed at the end of it. Every name, number and identifier is made up.
func inventedCardStatement(endDay, ledgerBalance, purchaseName string) string {
	return `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
<SIGNONMSGSRSV1>
<SONRS>
<STATUS>
<CODE>0
<SEVERITY>INFO
</STATUS>
<DTSERVER>20260202093000[0:GMT]
<LANGUAGE>ENG
<FI>
<ORG>Invented Card Issuer
<FID>99999
</FI>
</SONRS>
</SIGNONMSGSRSV1>
<CREDITCARDMSGSRSV1>
<CCSTMTTRNRS>
<TRNUID>0
<STATUS>
<CODE>0
<SEVERITY>INFO
</STATUS>
<CCSTMTRS>
<CURDEF>USD
<CCACCTFROM>
<ACCTID>11111a11-1aa1-1111-a11
</CCACCTFROM>
<BANKTRANLIST>
<DTSTART>20260101000000[0:GMT]
<DTEND>` + endDay + `235959[0:GMT]
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260105120000[0:GMT]
<TRNAMT>-23.40
<FITID>aaaa0001-0000-0000-0000-000000000001
<NAME>` + purchaseName + `
</STMTTRN>
<STMTTRN>
<TRNTYPE>PAYMENT
<DTPOSTED>20260115120000[0:GMT]
<TRNAMT>150.00
<FITID>aaaa0001-0000-0000-0000-000000000002
<NAME>PAYMENT RECEIVED - THANK YOU
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20260120120000[0:GMT]
<TRNAMT>12.00
<FITID>aaaa0001-0000-0000-0000-000000000003
<NAME>INVENTED BOOKSHOP
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL>
<BALAMT>` + ledgerBalance + `
<DTASOF>` + endDay + `235959[0:GMT]
</LEDGERBAL>
</CCSTMTRS>
</CCSTMTTRNRS>
</CREDITCARDMSGSRSV1>
</OFX>
`
}

func statementFile(content string) []*StatementFile {
	return []*StatementFile{{StatementFileName: "Invented Card Transactions.ofx", Content: []byte(content)}}
}

func (self *financeFixture) importStatement(t *testing.T, content string) *models.FinanceStatementImport {
	t.Helper()
	result, err := self.worker.ImportStatementFiles(t.Context(), self.agent, self.owner, statementFile(content), models.StatementImportOriginUpload, "")
	if err != nil {
		t.Fatalf("ImportStatementFiles: %s", err)
	}
	return result
}

// statementTransactions are the statement source's finance transactions by
// their FITID.
func (self *financeFixture) statementTransactions(t *testing.T) map[string]*models.FinanceTransaction {
	t.Helper()
	found := map[string]*models.FinanceTransaction{}
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(self.agent.ID, &db.FinanceTransactionFilter{Limit: db.FinanceTransactionLimitMost})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, transaction := range page.FinanceTransactions {
			found[transaction.ProviderTransactionID] = transaction
		}
	})
	return found
}

// Importing a statement adds its transactions; importing it again adds
// nothing; a transaction the institution changed is updated, not added a
// second time. A transaction is its account and its FITID.
func TestStatementImportDeduplicatesByFITID(t *testing.T) {
	fixture := newFinanceFixture(t, "")

	first := fixture.importStatement(t, inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS"))
	if first.AddedTransactionCount != 3 || first.UpdatedTransactionCount != 0 || first.UnchangedTransactionCount != 0 || first.ImportErrorMessage != "" {
		t.Fatalf("first import %+v", first)
	}
	if len(first.FinanceAccountNames) != 1 || first.FinanceAccountNames[0] != "Invented Card Issuer ··1a11" {
		t.Errorf("accounts %v", first.FinanceAccountNames)
	}

	again := fixture.importStatement(t, inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS"))
	if again.AddedTransactionCount != 0 || again.UpdatedTransactionCount != 0 || again.UnchangedTransactionCount != 3 {
		t.Errorf("the same file again %+v", again)
	}

	changed := fixture.importStatement(t, inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS LTD"))
	if changed.AddedTransactionCount != 0 || changed.UpdatedTransactionCount != 1 || changed.UnchangedTransactionCount != 2 {
		t.Errorf("a changed name %+v", changed)
	}

	transactions := fixture.statementTransactions(t)
	if len(transactions) != 3 {
		t.Fatalf("%d finance transactions after three imports", len(transactions))
	}
	purchase := transactions["aaaa0001-0000-0000-0000-000000000001"]
	payment := transactions["aaaa0001-0000-0000-0000-000000000002"]
	refund := transactions["aaaa0001-0000-0000-0000-000000000003"]
	transferCategoryId := fixture.transferCategoryId(t)
	if purchase.Amount != "-23.4000" || purchase.Description != "INVENTED COFFEE ROASTERS LTD" || purchase.SpendingCategoryID == transferCategoryId {
		t.Errorf("purchase %+v", purchase)
	}
	// A payment to the card is a transfer: neither spending nor income.
	if payment.Amount != "150.0000" || payment.SpendingCategoryID != transferCategoryId || payment.CategorizedBy != models.CategorizedByProviderCategoryMapping {
		t.Errorf("payment %+v", payment)
	}
	// A refund is money in on the card, left for the categorize model to
	// place against what it refunds.
	if refund.Amount != "12.0000" || refund.SpendingCategoryID == transferCategoryId {
		t.Errorf("refund %+v", refund)
	}

	source := fixture.statementSource(t)
	last := LastStatementImport(source)
	if last == nil || last.UpdatedTransactionCount != 1 || last.StatementImportOrigin != models.StatementImportOriginUpload {
		t.Errorf("last import %+v", last)
	}
}

func (self *financeFixture) statementSource(t *testing.T) *models.AgentKnowledgeSource {
	t.Helper()
	var source *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if source, err = findStatementSource(tx, self.agent.ID); err != nil || source == nil {
			t.Fatalf("findStatementSource: %v %v", source, err)
		}
	})
	return source
}

// The ledger balance is the card's balance and its asset's value for the
// day it is as of, the amount owed, so net worth counts it. A statement
// imported after a newer one records its own day's value and leaves the
// account's balance at the newer one.
func TestStatementImportRecordsTheBalance(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	fixture.importStatement(t, inventedCardStatement("20260228", "-400.00", "INVENTED COFFEE ROASTERS"))
	fixture.importStatement(t, inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS"))

	source := fixture.statementSource(t)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(fixture.agent.ID, source.ID)
		if err != nil || len(accounts) != 1 {
			t.Fatalf("accounts %v %v", accounts, err)
		}
		account := accounts[0]
		if account.AccountKind != models.FinanceAccountKindCredit || account.CurrentBalance != "-400.0000" || account.AccountMask != "1a11" {
			t.Errorf("account %+v", account)
		}
		if strings.Contains(account.ProviderAccountID, "11111a11") || strings.Contains(string(account.ProviderMetadata), "11111a11-1aa1") {
			t.Errorf("the account's identifier was stored: %s %s", account.ProviderAccountID, account.ProviderMetadata)
		}
		assets, err := tx.ListAssets(fixture.agent.ID)
		if err != nil {
			t.Fatalf("ListAssets: %s", err)
		}
		var asset *models.Asset
		for _, candidate := range assets {
			if candidate.FinanceAccountID == account.ID {
				asset = candidate
			}
		}
		if asset == nil || !asset.IsLiability || asset.AssetKind != models.AssetKindCreditCard {
			t.Fatalf("asset %+v", asset)
		}
		valuations, err := tx.ListAssetValuations(fixture.agent.ID, asset.ID)
		if err != nil {
			t.Fatalf("ListAssetValuations: %s", err)
		}
		valueOn := map[string]string{}
		for _, valuation := range valuations {
			valueOn[valuation.ValuedOn] = valuation.Value
		}
		if valueOn["2026-02-28"] != "400.0000" || valueOn["2026-01-31"] != "311.2500" {
			t.Errorf("valuations %v", valueOn)
		}
	})
}

// A file that is not OFX imports nothing and says why, on the answer and
// on the source; a switched-off source imports nothing either.
func TestStatementImportRefusals(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	result := fixture.importStatement(t, "Date,Description,Amount\n2026-01-05,INVENTED,-3.00\n")
	if len(result.FinanceAccountIDs) != 0 || !strings.Contains(result.ImportErrorMessage, "not an OFX file") {
		t.Errorf("a spreadsheet %+v", result)
	}
	source := fixture.statementSource(t)
	if source.LastError == "" || LastStatementImport(source) == nil {
		t.Errorf("the failure was not recorded: %q", source.LastError)
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		source.Enabled = false
		if _, err := tx.PutAgentSource(source); err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
	})
	result = fixture.importStatement(t, inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS"))
	if len(result.FinanceAccountIDs) != 0 || !strings.Contains(result.ImportErrorMessage, "switched off") {
		t.Errorf("a switched-off source %+v", result)
	}
	if len(fixture.statementTransactions(t)) != 0 {
		t.Error("a switched-off source imported")
	}
}

// The token is the gate: the current one opens, a wrong one does not, the
// old one stops opening once regenerated, and none opens a switched-off
// source.
func TestStatementImportToken(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	source, err := fixture.worker.EnsureStatementSource(t.Context(), fixture.agent)
	if err != nil {
		t.Fatalf("EnsureStatementSource: %s", err)
	}
	again, err := fixture.worker.EnsureStatementSource(t.Context(), fixture.agent)
	if err != nil || again.ID != source.ID {
		t.Fatalf("a second statement source %v %v", again, err)
	}
	mailbox := &models.Mailbox{ID: "mailbox-one", UserID: fixture.owner.ID}
	token := fixture.statementToken(t, source)
	if len(token) != 16 || strings.ToLower(token) != token {
		t.Errorf("token %q", token)
	}
	isOpen := func(candidate string) bool {
		var answer bool
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			answer = fixture.worker.IsStatementImportToken(tx, mailbox, candidate)
		})
		return answer
	}
	if !isOpen(token) || !isOpen(strings.ToUpper(token)) {
		t.Error("the token does not open")
	}
	if isOpen("aaaaaaaaaaaaaaaa") || isOpen("") {
		t.Error("a wrong token opens")
	}
	if _, err := fixture.worker.RegenerateStatementImportToken(t.Context(), fixture.agent); err != nil {
		t.Fatalf("RegenerateStatementImportToken: %s", err)
	}
	if isOpen(token) {
		t.Error("the old token still opens")
	}
	newToken := fixture.statementToken(t, source)
	if newToken == token || !isOpen(newToken) {
		t.Error("the new token does not open")
	}
	other := &models.Mailbox{ID: "mailbox-two", UserID: "somebody-else"}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if fixture.worker.IsStatementImportToken(tx, other, newToken) {
			t.Error("the token opens somebody else's mailbox")
		}
	})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		current, _ := tx.GetAgentSource(fixture.agent.ID, source.ID)
		current.Enabled = false
		if _, err := tx.PutAgentSource(current); err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
	})
	if isOpen(newToken) {
		t.Error("a switched-off source's token opens")
	}
}

func (self *financeFixture) statementToken(t *testing.T, source *models.AgentKnowledgeSource) string {
	t.Helper()
	var token string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if token, err = self.worker.statementSecret(tx, source, models.FinanceStatementTokenSecretKey); err != nil {
			t.Fatalf("statementSecret: %s", err)
		}
	})
	return token
}

// A phone's mail sends the statement as application/octet-stream; it is
// found by its name, and by its content when the name says nothing.
// Anything else the message carries is left alone.
func TestStatementFilesOf(t *testing.T) {
	t.Parallel()
	content := inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS")
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	headers := []string{"From: person@example.com", "Subject: ", "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="invented-boundary"`}
	body := strings.Join([]string{
		"--invented-boundary",
		"Content-Type: text/plain; charset=us-ascii",
		"",
		"",
		"--invented-boundary",
		`Content-Type: application/octet-stream; name="Invented Card Transactions.ofx"`,
		`Content-Disposition: attachment; filename="Invented Card Transactions.ofx"`,
		"Content-Transfer-Encoding: base64",
		"",
		encoded,
		"--invented-boundary",
		`Content-Type: application/octet-stream; name="export.dat"`,
		"Content-Transfer-Encoding: base64",
		"",
		encoded,
		"--invented-boundary",
		`Content-Type: application/pdf; name="notes.pdf"`,
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString([]byte("%PDF-1.7 invented")),
		"--invented-boundary--",
		"",
	}, "\r\n")
	files := StatementFilesOf(headers, []byte(body))
	if len(files) != 2 {
		t.Fatalf("%d files", len(files))
	}
	if files[0].StatementFileName != "Invented Card Transactions.ofx" || string(files[0].Content) != content || files[0].IsTooLarge {
		t.Errorf("first %q", files[0].StatementFileName)
	}
	if files[1].StatementFileName != "export.dat" || string(files[1].Content) != content {
		t.Errorf("second %q", files[1].StatementFileName)
	}
}

// The job a mailed statement queues imports it once, and tells the person
// in their main conversation what came of it; run again, it does not
// import a second time.
func TestStatementImportJob(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	fixture.worker.settings.Storage = store
	if _, err := fixture.worker.EnsureStatementSource(t.Context(), fixture.agent); err != nil {
		t.Fatalf("EnsureStatementSource: %s", err)
	}
	var mail *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if mail, err = tx.CreateMail(&models.Mail{Subject: "", From: "person@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
	})
	content := inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS")
	headers := []string{"From: person@example.com", `Content-Type: application/octet-stream; name="Invented Card Transactions.ofx"`, "Content-Transfer-Encoding: base64"}
	if err := store.Put(t.Context(), mail.ID, headers, []byte(base64.StdEncoding.EncodeToString([]byte(content)))); err != nil {
		t.Fatalf("Put: %s", err)
	}
	run := fixture.run()
	run.Job = &models.AgentJob{ID: "job-one", AgentID: fixture.agent.ID, Kind: models.AgentJobStatementImport, SubjectID: mail.ID}
	runJob := func() {
		if err := fixture.worker.runStatementImport(t.Context(), run); err != nil {
			var deferral *Deferral
			if errors.As(err, &deferral) {
				t.Fatalf("deferred with no turn running: %s", err)
			}
			t.Fatalf("runStatementImport: %s", err)
		}
	}
	runJob()
	if transactions := fixture.statementTransactions(t); len(transactions) != 3 {
		t.Errorf("%d finance transactions", len(transactions))
	}
	last := LastStatementImport(fixture.statementSource(t))
	if last == nil || last.AddedTransactionCount != 3 || last.StatementImportOrigin != models.StatementImportOriginMail {
		t.Errorf("last import %+v", last)
	}
	countSaid := func() int {
		said := 0
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			conversation, err := scheduleConversation(tx, fixture.agent.ID, "")
			if err != nil {
				t.Fatalf("scheduleConversation: %s", err)
			}
			messages, err := tx.ListAgentMessages(conversation.ID, nil)
			if err != nil {
				t.Fatalf("ListAgentMessages: %s", err)
			}
			for _, message := range messages {
				if message.Role == "assistant" && strings.Contains(message.Content, "3 transactions added") && strings.Contains(message.Content, "Invented Card Issuer ··1a11") {
					said++
				}
			}
		})
		return said
	}
	if countSaid() != 1 {
		t.Errorf("the person was told %d times", countSaid())
	}
	// Run again, as a job whose notice had to wait is: the import it
	// already did is told, not done a second time.
	runJob()
	if last := LastStatementImport(fixture.statementSource(t)); last == nil || last.AddedTransactionCount != 3 || last.UnchangedTransactionCount != 0 {
		t.Errorf("imported again: %+v", last)
	}
}

// A mailed statement whose notice waits on a running turn tells its own
// import when it is told, even after an upload was imported in between,
// and is not imported a second time. The notice used to read back the
// source's last import, which the upload had replaced.
func TestStatementImportJobTellsItsOwnImportAfterAnUpload(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	fixture.worker.settings.Storage = store
	if _, err := fixture.worker.EnsureStatementSource(t.Context(), fixture.agent); err != nil {
		t.Fatalf("EnsureStatementSource: %s", err)
	}
	var mail *models.Mail
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if mail, err = tx.CreateMail(&models.Mail{From: "person@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if conversation, err = scheduleConversation(tx, fixture.agent.ID, ""); err != nil {
			t.Fatalf("scheduleConversation: %s", err)
		}
	})
	content := inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS")
	headers := []string{"From: person@example.com", `Content-Type: application/octet-stream; name="Invented Card Transactions.ofx"`, "Content-Transfer-Encoding: base64"}
	if err := store.Put(t.Context(), mail.ID, headers, []byte(base64.StdEncoding.EncodeToString([]byte(content)))); err != nil {
		t.Fatalf("Put: %s", err)
	}
	run := fixture.run()
	run.Job = &models.AgentJob{ID: "job-one", AgentID: fixture.agent.ID, Kind: models.AgentJobStatementImport, SubjectID: mail.ID}

	// A turn is running in the conversation the notice goes to.
	fixture.worker.runsMutex.Lock()
	if fixture.worker.latest == nil {
		fixture.worker.latest = map[string]*AskRun{}
	}
	fixture.worker.latest[conversation.ID] = &AskRun{}
	fixture.worker.runsMutex.Unlock()
	var deferral *Deferral
	if err := fixture.worker.runStatementImport(t.Context(), run); !errors.As(err, &deferral) {
		t.Fatalf("the notice did not wait for the turn: %v", err)
	}

	// The person uploads the next month meanwhile: one transaction
	// changed, two already here.
	upload := fixture.importStatement(t, inventedCardStatement("20260228", "-200.00", "INVENTED COFFEE ROASTERS RENAMED"))
	if upload.UnchangedTransactionCount != 2 {
		t.Fatalf("upload %+v", upload)
	}

	fixture.worker.runsMutex.Lock()
	delete(fixture.worker.latest, conversation.ID)
	fixture.worker.runsMutex.Unlock()
	if err := fixture.worker.runStatementImport(t.Context(), run); err != nil {
		t.Fatalf("runStatementImport: %s", err)
	}
	var said []string
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		for _, message := range messages {
			if message.Role == "assistant" {
				said = append(said, message.Content)
			}
		}
	})
	if len(said) != 1 || !strings.Contains(said[0], "3 transactions added") || strings.Contains(said[0], "already here") {
		t.Errorf("the person was told %q; want the mailed import, 3 transactions added", said)
	}
	if last := LastStatementImport(fixture.statementSource(t)); last == nil || last.StatementImportOrigin != models.StatementImportOriginUpload {
		t.Errorf("the mailed statement was imported again: the last import is %+v", last)
	}
}

// A base64 part is limited by what it decodes to, whatever its line breaks
// add: a file of exactly the largest size is read whole, and one a little
// larger is known to be too large rather than cut short as though whole.
func TestStatementFilesOfNearTheLimit(t *testing.T) {
	t.Parallel()
	header := "OFXHEADER:100\n<OFX>\n"
	for _, test := range []struct {
		size         int
		isTooLarge   bool
		expectedSize int
	}{
		{ofx.MaximumFileBytes, false, ofx.MaximumFileBytes},
		{ofx.MaximumFileBytes + 1000, true, ofx.MaximumFileBytes},
	} {
		content := header + strings.Repeat("x", test.size-len(header))
		encoded := base64.StdEncoding.EncodeToString([]byte(content))
		var wrapped strings.Builder
		for start := 0; start < len(encoded); start += 76 {
			wrapped.WriteString(encoded[start:min(start+76, len(encoded))])
			wrapped.WriteString("\r\n")
		}
		headers := []string{"From: person@example.com", `Content-Type: application/octet-stream; name="Invented Card Transactions.ofx"`, "Content-Transfer-Encoding: base64"}
		files := StatementFilesOf(headers, []byte(wrapped.String()))
		if len(files) != 1 || files[0].IsTooLarge != test.isTooLarge || len(files[0].Content) != test.expectedSize {
			if len(files) == 1 {
				t.Errorf("%d bytes: read %d, too large %v", test.size, len(files[0].Content), files[0].IsTooLarge)
			} else {
				t.Errorf("%d bytes: %d files", test.size, len(files))
			}
		}
	}
}

// Two exports of one card, one naming its institution by FID and the next
// only by ORG, are one account, and their transactions are not counted
// twice.
func TestStatementImportIsOneAccountWhateverTheInstitutionBlockSays(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	content := inventedCardStatement("20260131", "-311.25", "INVENTED COFFEE ROASTERS")
	first := fixture.importStatement(t, content)
	second := fixture.importStatement(t, strings.Replace(content, "<FID>99999\n", "", 1))
	if len(first.FinanceAccountIDs) != 1 || len(second.FinanceAccountIDs) != 1 || first.FinanceAccountIDs[0] != second.FinanceAccountIDs[0] {
		t.Errorf("one card became %v and %v", first.FinanceAccountIDs, second.FinanceAccountIDs)
	}
	if transactions := fixture.statementTransactions(t); len(transactions) != 3 || second.UnchangedTransactionCount != 3 {
		t.Errorf("%d finance transactions, %+v", len(transactions), second)
	}
}
