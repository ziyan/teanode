package agent

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// inventedCardScreenshotAnswer is the model's reading of an invented
// screenshot of a card's recent charges.
const inventedCardScreenshotAnswer = `{"isStatement": true, "institutionName": "Example Bank", "accountNumber": "****4821", "isAccountNumberPartial": true,
"statementAccountKind": "card", "currencyCode": "USD",
"transactionRows": [{"postedOn": "2026-09-12", "description": "CORNER CAFE", "amount": "-4.50"}, {"postedOn": "2026-09-14", "description": "BOOK SHOP", "amount": "-12.00"}]}`

// statementUpload stores a file the person handed in to be imported.
func (self *financeFixture) statementUpload(t *testing.T, name, contentType string, content []byte) *models.AgentAttachment {
	t.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: self.agent.ID, Name: name, ContentType: contentType, Size: int64(len(content))}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := self.worker.settings.Storage.PutFile(t.Context(), attachment.ID, content); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	return attachment
}

func (self *financeFixture) statementJobRun(subjectId string) *Run {
	run := self.run()
	run.Job = &models.AgentJob{ID: "job-statement", AgentID: self.agent.ID, Kind: models.AgentJobStatementPhoto, SubjectID: subjectId}
	return run
}

// feedCards are the titles and bodies of the cards in the person's feed.
func (self *financeFixture) feedCards(t *testing.T) []string {
	t.Helper()
	cards := dbtest.QueryString(t, self.database, `SELECT coalesce(string_agg("title" || ' | ' || "body", E'\n' ORDER BY "created_at"), '') FROM "agent_feed_item" WHERE "agent_id" = '`+self.agent.ID+`'`)
	if cards == "" {
		return nil
	}
	return strings.Split(cards, "\n")
}

// A screenshot the person hands in is theirs to import: its rows start an
// account of imported statements when none was imported before, and a card
// in their feed says what came of it.
func TestAHandedInScreenshotStartsAnAccountAndSaysSoInTheFeed(t *testing.T) {
	fixture, _ := receiptJobFixture(t, inventedCardScreenshotAnswer)
	upload := fixture.statementUpload(t, "picture-1.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented screenshot"))
	countOf := func() int {
		count, err := strconv.Atoi(dbtest.QueryString(t, fixture.database, `SELECT count(*)::text FROM "agent_finance_transaction"`))
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := countOf()
	if err := fixture.worker.runReadStatementPhoto(t.Context(), fixture.statementJobRun(upload.ID)); err != nil {
		t.Fatalf("runReadStatementPhoto: %s", err)
	}
	if added := countOf() - before; added != 2 {
		t.Fatalf("%d transactions imported, want the screenshot's two", added)
	}
	cards := fixture.feedCards(t)
	if len(cards) != 1 || !strings.HasPrefix(cards[0], "Imported 2 transactions into ") {
		t.Fatalf("the feed says what was imported: %q", cards)
	}
}

// A file handed in that shows no transactions imports nothing, and the
// card says why, so the person is not left waiting.
func TestAHandedInFileWithNoTransactionsSaysWhyInTheFeed(t *testing.T) {
	fixture, _ := receiptJobFixture(t, `{"isStatement": false}`)
	upload := fixture.statementUpload(t, "picture-1.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented picture of a cat"))
	if err := fixture.worker.runReadStatementPhoto(t.Context(), fixture.statementJobRun(upload.ID)); err != nil {
		t.Fatalf("runReadStatementPhoto: %s", err)
	}
	cards := fixture.feedCards(t)
	if len(cards) != 1 || !strings.HasPrefix(cards[0], "Nothing was imported from picture-1.jpg | It shows no transactions of one account.") {
		t.Fatalf("the feed says why nothing was imported: %q", cards)
	}
}

// A statement handed in as a tax document is handed on to be imported as
// the statement it is; a file that is neither says so in the feed.
func TestATaxDocumentThatIsAStatementIsImportedAndOneThatIsNeitherIsTold(t *testing.T) {
	fixture, _ := receiptJobFixture(t, `{"documentKind": "statement"}`, `{"documentKind": "none", "why": "a holiday card"}`)
	statement := fixture.taxDocumentUpload(t, "picture-1.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented bank screenshot"))
	if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(statement.ID)); err != nil {
		t.Fatalf("runReadTaxDocument: %s", err)
	}
	if got := dbtest.QueryString(t, fixture.database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = 'statement_photo' AND "subject_id" = '`+statement.ID+`'`); got != "1" {
		t.Fatalf("%s statement jobs queued for it, want 1", got)
	}
	if cards := fixture.feedCards(t); len(cards) != 0 {
		t.Fatalf("a statement handed on posts nothing yet: %q", cards)
	}

	card := fixture.taxDocumentUpload(t, "picture-2.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented holiday card"))
	if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(card.ID)); err != nil {
		t.Fatalf("runReadTaxDocument: %s", err)
	}
	cards := fixture.feedCards(t)
	if len(cards) != 1 || !strings.HasPrefix(cards[0], "Nothing was recorded from picture-2.jpg | It is not a paystub, a W-2, a 1099 or a 1098: a holiday card.") {
		t.Fatalf("the feed says why nothing was recorded: %q", cards)
	}
}

// A screenshot that shows no account number goes into the one account of
// imported statements at its bank, of its kind and currency; with none
// there, nothing is imported and the card says what to share instead.
func TestAHandedInScreenshotWithNoNumberGoesIntoTheOneAccountThere(test *testing.T) {
	numberless := strings.Replace(inventedCardScreenshotAnswer, `"accountNumber": "****4821", "isAccountNumberPartial": true,`, ``, 1)
	later := strings.NewReplacer("2026-09-12", "2026-09-20", "2026-09-14", "2026-09-21", "CORNER CAFE", "TEA HOUSE", "BOOK SHOP", "SHOE SHOP").Replace(numberless)
	fixture, _ := receiptJobFixture(test, numberless, inventedCardScreenshotAnswer, later)
	first := fixture.statementUpload(test, "picture-1.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented screenshot"))
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.statementJobRun(first.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	cards := fixture.feedCards(test)
	if len(cards) != 1 || !strings.Contains(cards[0], "It shows no account number, and no account at Example Bank was imported before") {
		test.Fatalf("the feed says the screenshot shows no number: %q", cards)
	}

	// A screenshot with the card's last digits starts its account; one of
	// the same card without them then goes into it.
	numbered := fixture.statementUpload(test, "picture-2.jpg", "image/jpeg", []byte("\xff\xd8\xffanother invented screenshot"))
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.statementJobRun(numbered.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	again := fixture.statementUpload(test, "picture-3.jpg", "image/jpeg", []byte("\xff\xd8\xffa third invented screenshot"))
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.statementJobRun(again.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	cards = fixture.feedCards(test)
	if len(cards) != 3 || !strings.HasPrefix(cards[2], "Imported 2 transactions into ") || !strings.Contains(cards[2], "4821") {
		test.Fatalf("the screenshot without the number goes into the card's account: %q", cards)
	}
}

// The reading is shown the person's accounts: a screenshot naming its bank
// another way and showing no number goes into the account the reading
// says it is of, and one of an account the bank syncs is never imported.
func TestTheReadingNamesTheAccountAScreenshotIsOf(test *testing.T) {
	fixture, model := receiptJobFixture(test)
	check, err := finance.CheckTransactionRows(&finance.TransactionRowsInput{
		InstitutionName: "Example Bank", AccountNumber: "****4821", IsAccountNumberPartial: true, StatementAccountKind: finance.StatementAccountKindCard, CurrencyCode: "USD",
		TransactionRows: []finance.TransactionRow{{PostedOn: "2026-09-01", Description: "TEA HOUSE", Amount: "-3.00"}},
	}, "")
	if err != nil {
		test.Fatal(err)
	}
	if _, err := fixture.worker.ImportTransactionRows(test.Context(), fixture.agent, fixture.owner, check); err != nil {
		test.Fatalf("ImportTransactionRows: %s", err)
	}
	cardId := dbtest.QueryString(test, fixture.database, `SELECT "id" FROM "agent_finance_account" WHERE "currency_code" = 'USD' AND "account_kind" = 'credit'`)
	syncedId := dbtest.QueryString(test, fixture.database, `SELECT "id" FROM "agent_finance_account" WHERE "account_name" = 'Everyday Checking'`)
	model.mutex.Lock()
	model.answers = append(model.answers,
		`{"isStatement": true, "financeAccountId": "`+cardId+`", "institutionName": "Banco de Ejemplo", "statementAccountKind": "card", "currencyCode": "USD",
 "transactionRows": [{"postedOn": "2026-09-12", "description": "BOOK SHOP", "amount": "-12.00"}]}`,
		`{"isStatement": true, "syncedFinanceAccountId": "`+syncedId+`", "institutionName": "Another Bank", "statementAccountKind": "bank", "currencyCode": "USD",
 "transactionRows": [{"postedOn": "2026-09-12", "description": "CORNER CAFE", "amount": "-4.50"}]}`)
	model.mutex.Unlock()

	other := fixture.statementUpload(test, "picture-1.jpg", "image/jpeg", []byte("\xff\xd8\xffan invented screenshot"))
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.statementJobRun(other.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	model.mutex.Lock()
	prompt := model.prompts[len(model.prompts)-1]
	model.mutex.Unlock()
	if !strings.Contains(prompt, cardId+": ") || !strings.Contains(prompt, syncedId+": Everyday Checking") {
		test.Fatalf("the reading is shown the accounts: %s", prompt)
	}
	synced := fixture.statementUpload(test, "picture-2.jpg", "image/jpeg", []byte("\xff\xd8\xffanother invented screenshot"))
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.statementJobRun(synced.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	cards := fixture.feedCards(test)
	if len(cards) != 2 || !strings.HasPrefix(cards[0], "Imported 1 transaction into ") || !strings.Contains(cards[0], "4821") ||
		!strings.Contains(cards[1], "Everyday Checking, which is already synced from the bank") {
		test.Fatalf("the cards say where each went: %q", cards)
	}
}
