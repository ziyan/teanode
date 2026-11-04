package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// newPhotoReadingFixture is the photo job's fixture on a server that offers
// finance, with the fixture's photo ready and taken now.
func newPhotoReadingFixture(test *testing.T, answers ...string) *photoJobFixture {
	test.Helper()
	fixture := newPhotoJobFixture(test, false, answers...)
	fixture.configuration.Agent.Finance.OfferedProviders = []string{config.AgentFinanceProviderSimpleFIN}
	fixture.configuration.Server.Secret = "a secret long enough to seal things with"
	fixture.ready(test, fixture.photo, time.Now())
	return fixture
}

// ready marks a photo's previews made, taken at takenAt.
func (self *photoJobFixture) ready(test *testing.T, photo *models.Photo, takenAt time.Time) {
	test.Helper()
	dbtest.Exec(test, self.database, `UPDATE "photo" SET "photo_processing_state" = 'ready', "taken_at" = '`+takenAt.UTC().Format(time.RFC3339)+`' WHERE "id" = '`+photo.ID+`'`)
}

// queuedJobCount is how many jobs of a kind wait for a subject.
func (self *photoJobFixture) queuedJobCount(test *testing.T, kind models.AgentJobKind, subjectId string) string {
	test.Helper()
	return dbtest.QueryString(test, self.database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = '`+string(kind)+`' AND "subject_id" = '`+subjectId+`'`)
}

// documentOf is the copy of a photo made to be read as a document.
func (self *photoJobFixture) documentOf(test *testing.T, photo *models.Photo) *models.AgentAttachment {
	test.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.GetAgentAttachmentOfPhoto(self.agent.ID, photo.ID); err != nil {
			test.Fatal(err)
		}
	})
	return attachment
}

// jobRun is a run of a job of the fixture's agent.
func (self *photoJobFixture) jobRun(kind models.AgentJobKind, subjectId string) *Run {
	return &Run{
		Agent: self.agent, Owner: self.owner, Now: time.Now(), settings: self.worker.settings,
		Job: &models.AgentJob{ID: "job-" + string(kind), AgentID: self.agent.ID, Kind: kind, SubjectID: subjectId},
	}
}

func (self *photoJobFixture) titlesOf(test *testing.T, kind models.AgentJobKind) []string {
	test.Helper()
	var titles []string
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(self.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			test.Fatal(err)
		}
		for _, conversation := range conversations {
			if conversation.JobKind == string(kind) {
				titles = append(titles, conversation.Title)
			}
		}
	})
	return titles
}

const (
	receiptPhotoAnswer = `{"photoDescription": "A till receipt from Example Market held over a car seat.", "visibleText": "EXAMPLE MARKET\nTOTAL 9.50", "documentKind": "receipt"}`
	receiptLinesAnswer = `{"isPurchase": true, "merchantName": "EXAMPLE MARKET", "purchasedOn": "2026-09-11", "currencyCode": "USD", "subtotalAmount": "9.50", "totalAmount": "9.50",
 "receiptLines": [{"lineNumber": 1, "receiptLineKind": "item", "description": "BREAD", "lineAmount": "4.50"}, {"lineNumber": 2, "receiptLineKind": "item", "description": "MILK", "lineAmount": "5.00"}]}`
)

// Each new picture of the person's own is queued to be described once: not a
// picture taken long ago, not a video, not a photo saved from somebody
// else's share, not one whose previews are not made, and nothing at all while
// the person's switch is off.
func TestANewPhotoIsQueuedToBeReadOnce(test *testing.T) {
	fixture := newPhotoReadingFixture(test, lighthouseAnswer)
	old := fixture.addPhoto(test, "old-one", "", true)
	fixture.ready(test, old, time.Now().Add(-60*24*time.Hour))
	video := fixture.addPhoto(test, "video-one", "", true)
	fixture.ready(test, video, time.Now())
	dbtest.Exec(test, fixture.database, `UPDATE "photo" SET "photo_kind" = 'video' WHERE "id" = '`+video.ID+`'`)
	shared := fixture.addPhoto(test, "shared-one", "", true)
	fixture.ready(test, shared, time.Now())
	dbtest.Exec(test, fixture.database, `UPDATE "photo" SET "photo_source" = 'shared' WHERE "id" = '`+shared.ID+`'`)
	waiting := fixture.addPhoto(test, "waiting-one", "", true)

	now := time.Now()
	fixture.worker.queuePhotoReading(test.Context(), now)
	if got := fixture.queuedJobCount(test, models.AgentJobDescribePhoto, fixture.photo.ID); got != "1" {
		test.Fatalf("%s jobs describe the new photo, want 1", got)
	}
	if got := fixture.queuedJobCount(test, models.AgentJobDescribePhoto, video.ID); got != "1" {
		test.Errorf("%s jobs describe the new video, want 1", got)
	}
	for _, photo := range []*models.Photo{old, shared, waiting} {
		if got := fixture.queuedJobCount(test, models.AgentJobDescribePhoto, photo.ID); got != "0" {
			test.Errorf("photo %s was queued", photo.ClientPhotoID)
		}
	}
	if read := fixture.read(test, fixture.photo); read.ReadingQueuedAt == nil || !read.IsDescriptionRequested {
		test.Errorf("the photo is marked queued and its description on its way: %+v", read)
	}

	// Its previews made later, the waiting photo is read; the first is not
	// queued again even once its job is done.
	dbtest.Exec(test, fixture.database, `DELETE FROM "agent_job"`)
	fixture.ready(test, waiting, time.Now())
	fixture.worker.queuePhotoReading(test.Context(), now.Add(2*photoReadingEvery))
	if fixture.queuedJobCount(test, models.AgentJobDescribePhoto, waiting.ID) != "1" || fixture.queuedJobCount(test, models.AgentJobDescribePhoto, fixture.photo.ID) != "0" {
		test.Error("only the photo whose previews were made since is queued")
	}

	another := fixture.addPhoto(test, "another-one", "", true)
	fixture.ready(test, another, time.Now())
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if err := tx.SetPhotoReading(fixture.owner.ID, false); err != nil {
			test.Fatal(err)
		}
	})
	fixture.worker.queuePhotoReading(test.Context(), now.Add(4*photoReadingEvery))
	if got := fixture.queuedJobCount(test, models.AgentJobDescribePhoto, another.ID); got != "0" {
		test.Error("a photo was queued while the switch is off")
	}
}

// A picture the description says is a receipt is copied, once, into an
// upload the receipt job reads, and recorded. A second photo of the same
// receipt records nothing.
func TestAReceiptPhotoIsRecordedOnce(test *testing.T) {
	fixture := newPhotoReadingFixture(test, receiptPhotoAnswer, receiptLinesAnswer, receiptPhotoAnswer, receiptPhotoAnswer, receiptLinesAnswer)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	if read := fixture.read(test, fixture.photo); read.PhotoDocumentKind != models.PhotoDocumentReceipt {
		test.Fatalf("the photo is kept as a receipt: %q", read.PhotoDocumentKind)
	}
	document := fixture.documentOf(test, fixture.photo)
	if document == nil || document.ContentType != "image/jpeg" || document.IsTaxDocument || !strings.HasSuffix(document.Name, ".jpg") {
		test.Fatalf("the photo's copy: %+v", document)
	}
	if got := fixture.queuedJobCount(test, models.AgentJobReadReceipt, document.ID); got != "1" {
		test.Fatalf("%s receipt jobs, want 1", got)
	}
	if titles := fixture.runTitles(test); len(titles) != 1 || !strings.Contains(titles[0], "(a receipt, queued to be recorded as a receipt)") {
		test.Errorf("the run says the receipt was handed on: %q", titles)
	}
	if err := fixture.worker.runReadReceipt(test.Context(), fixture.jobRun(models.AgentJobReadReceipt, document.ID)); err != nil {
		test.Fatalf("runReadReceipt: %s", err)
	}

	// Described again, the photo is not handed on a second time.
	fixture.request(test, fixture.photo)
	dbtest.Exec(test, fixture.database, `DELETE FROM "agent_job"`)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto again: %s", err)
	}
	if got := fixture.queuedJobCount(test, models.AgentJobReadReceipt, document.ID); got != "0" {
		test.Errorf("the same photo was handed on again: %s jobs", got)
	}

	// Another photo of the same receipt reads the same purchase.
	second := fixture.addPhoto(test, "receipt-again", "", true)
	fixture.ready(test, second, time.Now())
	fixture.request(test, second)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(second)); err != nil {
		test.Fatalf("runDescribePhoto of the second: %s", err)
	}
	secondDocument := fixture.documentOf(test, second)
	if secondDocument == nil {
		test.Fatal("the second photo was not handed on")
	}
	if err := fixture.worker.runReadReceipt(test.Context(), fixture.jobRun(models.AgentJobReadReceipt, secondDocument.ID)); err != nil {
		test.Fatalf("runReadReceipt of the second: %s", err)
	}
	var receipts []*models.FinanceReceipt
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceReceipts(fixture.agent.ID, nil)
		if err != nil {
			test.Fatal(err)
		}
		receipts = page.FinanceReceipts
	})
	if len(receipts) != 1 || receipts[0].AgentAttachmentID != document.ID || receipts[0].TotalAmount != "9.5000" {
		test.Fatalf("one receipt, from the first photo: %+v", receipts)
	}
	titles := fixture.titlesOf(test, models.AgentJobReadReceipt)
	isSaidRecorded := false
	for _, title := range titles {
		isSaidRecorded = isSaidRecorded || strings.Contains(title, "is recorded already, so nothing was recorded")
	}
	if !isSaidRecorded {
		test.Errorf("the second reading says the purchase is recorded: %q", titles)
	}
}

// A tax document's picture goes to the tax document job, and the taxpayer
// number read in it is masked in the words a search reads.
func TestATaxDocumentPhotoGoesToTheTaxJobWithItsNumberMasked(test *testing.T) {
	fixture := newPhotoReadingFixture(test, `{"photoDescription": "A W-2 form on a table, social security number 123-45-6789.", "visibleText": "Wage and Tax Statement\nEmployee's social security number 123-45-6789", "documentKind": "taxDocument"}`)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	read := fixture.read(test, fixture.photo)
	if strings.Contains(read.DescriptionVisibleText, "123-45-6789") || !strings.Contains(read.DescriptionVisibleText, "Wage and Tax Statement") {
		test.Errorf("the words kept: %q", read.DescriptionVisibleText)
	}
	if strings.Contains(read.PhotoDescription, "123-45-6789") || !strings.Contains(read.PhotoDescription, "A W-2 form") {
		test.Errorf("the description kept: %q", read.PhotoDescription)
	}
	// Nor does the run keep what the model answered with the number in it.
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(fixture.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			test.Fatal(err)
		}
		for _, conversation := range conversations {
			messages, err := tx.ListAgentMessages(conversation.ID, nil)
			if err != nil {
				test.Fatal(err)
			}
			for _, message := range messages {
				if strings.Contains(message.Content, "123-45-6789") || strings.Contains(conversation.Title, "123-45-6789") {
					test.Errorf("a run keeps the number: %q", message.Content)
				}
			}
		}
	})
	document := fixture.documentOf(test, fixture.photo)
	if document == nil || !document.IsTaxDocument {
		test.Fatalf("the copy is a tax document: %+v", document)
	}
	if got := fixture.queuedJobCount(test, models.AgentJobTaxDocument, document.ID); got != "1" {
		test.Errorf("%s tax document jobs, want 1", got)
	}
}

// A document is described but not recorded while the switch is off, and a
// picture of nothing in particular is never handed on.
func TestADocumentIsNotHandedOnWhileReadingIsOff(test *testing.T) {
	fixture := newPhotoReadingFixture(test, receiptPhotoAnswer, lighthouseAnswer)
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if err := tx.SetPhotoReading(fixture.owner.ID, false); err != nil {
			test.Fatal(err)
		}
	})
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	if fixture.documentOf(test, fixture.photo) != nil {
		test.Error("a receipt was handed on while reading is off")
	}
	if titles := fixture.runTitles(test); len(titles) != 1 || !strings.Contains(titles[0], "not recorded, since reading new photos is off") {
		test.Errorf("the run says why: %q", titles)
	}

	plain := fixture.addPhoto(test, "plain-one", "", true)
	fixture.request(test, plain)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(plain)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	if read := fixture.read(test, plain); read.PhotoDocumentKind != models.PhotoDocumentNone || fixture.documentOf(test, plain) != nil {
		test.Errorf("a lighthouse is no document: %q", read.PhotoDocumentKind)
	}
}

// A statement's rows are imported into the account of imported statements
// they are of, and rows of an account never imported are not: nobody is
// there to say which account a picture shows.
func TestAStatementPhotoImportsIntoTheAccountImportedBefore(test *testing.T) {
	statementAnswer := `{"isStatement": true, "institutionName": "Example Bank", "accountNumber": "****4821", "isAccountNumberPartial": true, "statementAccountKind": "card", "currencyCode": "USD",
 "transactionRows": [{"postedOn": "2026-09-12", "description": "CORNER CAFE", "amount": "-4.50"}, {"postedOn": "2026-09-14", "description": "BOOK SHOP", "amount": "-12.00"}]}`
	fixture := newPhotoReadingFixture(test, `{"photoDescription": "A phone screen listing card charges.", "visibleText": "Example Bank", "documentKind": "statement"}`, statementAnswer)
	fixture.request(test, fixture.photo)
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	document := fixture.documentOf(test, fixture.photo)
	if document == nil || fixture.queuedJobCount(test, models.AgentJobStatementPhoto, document.ID) != "1" {
		test.Fatalf("the statement is queued: %+v", document)
	}

	// No account of Example Bank was imported: nothing is.
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.jobRun(models.AgentJobStatementPhoto, document.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto: %s", err)
	}
	if titles := fixture.titlesOf(test, models.AgentJobStatementPhoto); len(titles) != 1 || !strings.Contains(titles[0], "not imported before") {
		test.Fatalf("the run says why nothing was imported: %q", titles)
	}
	if got := dbtest.QueryString(test, fixture.database, `SELECT count(*)::text FROM "agent_finance_transaction"`); got != "0" {
		test.Fatalf("%s transactions imported", got)
	}

	// Once the person imported the card, the picture's new rows go into it.
	check, err := finance.CheckTransactionRows(&finance.TransactionRowsInput{
		InstitutionName: "Example Bank", AccountNumber: "****4821", IsAccountNumberPartial: true, StatementAccountKind: finance.StatementAccountKindCard, CurrencyCode: "USD",
		TransactionRows: []finance.TransactionRow{{PostedOn: "2026-09-12", Description: "CORNER CAFE", Amount: "-4.50"}},
	}, "")
	if err != nil {
		test.Fatal(err)
	}
	if _, err := fixture.worker.ImportTransactionRows(test.Context(), fixture.agent, fixture.owner, check); err != nil {
		test.Fatalf("ImportTransactionRows: %s", err)
	}
	if err := fixture.worker.runReadStatementPhoto(test.Context(), fixture.jobRun(models.AgentJobStatementPhoto, document.ID)); err != nil {
		test.Fatalf("runReadStatementPhoto again: %s", err)
	}
	if got := dbtest.QueryString(test, fixture.database, `SELECT count(*)::text FROM "agent_finance_transaction"`); got != "2" {
		test.Errorf("%s transactions, want the imported one and the picture's new one", got)
	}
	titles := fixture.titlesOf(test, models.AgentJobStatementPhoto)
	if !strings.Contains(strings.Join(titles, "\n"), "1 transaction added to") {
		test.Errorf("the run says what it added: %q", titles)
	}
}

// A document whose handing on failed after its description was kept is
// handed on when the job runs again, without asking the model a second time.
func TestADocumentNotHandedOnIsHandedOnWhenTheJobRunsAgain(test *testing.T) {
	fixture := newPhotoReadingFixture(test, lighthouseAnswer)
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if err := tx.SetPhotoDescription(fixture.owner.ID, fixture.photo.ID, &db.PhotoDescription{
			PhotoDescription: "A till receipt.", ModelName: "p:thinker", PhotoDocumentKind: models.PhotoDocumentReceipt,
		}); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.worker.runDescribePhoto(test.Context(), fixture.run(fixture.photo)); err != nil {
		test.Fatalf("runDescribePhoto: %s", err)
	}
	if fixture.provider.chatCount() != 0 {
		test.Errorf("the model was asked again: %d", fixture.provider.chatCount())
	}
	document := fixture.documentOf(test, fixture.photo)
	if document == nil || fixture.queuedJobCount(test, models.AgentJobReadReceipt, document.ID) != "1" {
		test.Fatalf("the receipt is handed on: %+v", document)
	}
	// Its copy is kept while the job waits, however long that is.
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		orphans, err := tx.ListOrphanAgentAttachments(time.Now().Add(72 * time.Hour))
		if err != nil {
			test.Fatal(err)
		}
		for _, orphan := range orphans {
			if orphan.ID == document.ID {
				test.Error("a copy whose job waits is taken for an orphan")
			}
		}
	})
}

// A person's waiting descriptions are capped, so that a backlog of new
// photos never fills the queue ahead of their own turns.
func TestNewPhotosWaitInAShortQueue(test *testing.T) {
	fixture := newPhotoReadingFixture(test, lighthouseAnswer)
	for index := range 2 * photoReadingWaitingMost {
		photo := fixture.addPhoto(test, "many-"+strings.Repeat("x", index+1), "", true)
		fixture.ready(test, photo, time.Now().Add(-time.Duration(index)*time.Minute))
	}
	waiting := func() string {
		return dbtest.QueryString(test, fixture.database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = 'describe_photo' AND "status" = 'queued'`)
	}
	now := time.Now()
	for look := range 4 {
		fixture.worker.queuePhotoReading(test.Context(), now.Add(time.Duration(look)*2*photoReadingEvery))
	}
	if got := waiting(); got != "20" {
		test.Errorf("%s descriptions wait, want %d", got, photoReadingWaitingMost)
	}
}
