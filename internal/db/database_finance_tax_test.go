package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// inventedTaxPaystub is a paycheck that adds up, as CheckPaystub leaves
// it: 4,000.00 gross, 880.00 in taxes, 400.00 in deductions.
func inventedTaxPaystub(agentId, attachmentId, payDate string) *models.FinancePaystub {
	return &models.FinancePaystub{
		AgentID: agentId, AgentAttachmentID: attachmentId, EmployerName: "Example Employer LLC", EmployeeName: "Pat Example",
		EmployeeTaxIDMask: "0123", PayDate: payDate, PeriodStartOn: "2026-02-15", PeriodEndOn: "2026-02-28", CurrencyCode: "USD",
		GrossAmount: "4000.00", YearToDateGrossAmount: "20000.00", TotalTaxAmount: "880.00", TotalDeductionAmount: "400.00", NetAmount: "2720.00",
		PaystubCheckState: models.PaystubCheckStateBalanced, CheckDifferenceAmount: "0",
		PaystubLines: []*models.FinancePaystubLine{
			{LineNumber: 1, PaystubLineKind: models.PaystubLineKindEarning, Description: "Regular", Hours: "80", CurrentAmount: "4000.00", YearToDateAmount: "20000.00"},
			{LineNumber: 2, PaystubLineKind: models.PaystubLineKindTax, Description: "Federal", PaystubTaxKind: models.PaystubTaxKindFederalIncome,
				CurrentAmount: "500.00", YearToDateAmount: "2500.00", CurrentWagesAmount: "3600.00", YearToDateWagesAmount: "18000.00"},
			{LineNumber: 3, PaystubLineKind: models.PaystubLineKindTax, Description: "State", PaystubTaxKind: models.PaystubTaxKindStateIncome,
				TaxJurisdictionCode: "CA", CurrentAmount: "380.00", YearToDateAmount: "1900.00"},
			{LineNumber: 4, PaystubLineKind: models.PaystubLineKindDeduction, Description: "HSA", PaystubDeductionKind: models.PaystubDeductionKindHSA,
				CurrentAmount: "400.00", YearToDateAmount: "2000.00", EmployerCurrentAmount: "50.00", EmployerYearToDateAmount: "250.00"},
		},
	}
}

func inventedTaxForm(agentId, attachmentId string, taxYear int) *models.FinanceTaxForm {
	return &models.FinanceTaxForm{
		AgentID: agentId, AgentAttachmentID: attachmentId, TaxFormKind: models.TaxFormKindW2, TaxYear: taxYear,
		PayerName: "Example Employer LLC", PayerTaxID: "12-3456789", RecipientName: "Pat Example", RecipientTaxIDMask: "0123",
		TaxFormEntries: []*models.FinanceTaxFormEntry{
			{EntryNumber: 1, BoxCode: "1", Amount: "21600.00"},
			{EntryNumber: 2, BoxCode: "12", BoxQualifier: "D", Amount: "1800.00"},
			{EntryNumber: 3, BoxCode: "13", BoxQualifier: "retirement_plan", IsChecked: true},
			{EntryNumber: 4, BoxCode: "15", BoxQualifier: "CA", TextValue: "123-4567-8"},
		},
	}
}

func createTaxUpload(t *testing.T, tx db.Transaction, agentId, name string) *models.AgentAttachment {
	t.Helper()
	attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: agentId, Name: name, ContentType: "application/pdf", Size: 10, Text: "read text", IsTaxDocument: true})
	if err != nil {
		t.Fatalf("CreateAgentAttachment: %s", err)
	}
	return attachment
}

// A paystub is kept as written, reading its upload again replaces it under
// the same id, and nobody reaches another agent's.
func TestFinancePaystubsArePutListedAndReplaced(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	stranger := createFinanceFixture(t, database, "sam")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		upload := createTaxUpload(t, tx, fixture.agentId, "stub.pdf")
		if upload.Text != "" || !upload.IsTaxDocument {
			t.Fatalf("a tax document's text is never kept: %+v", upload)
		}
		stored, err := tx.PutFinancePaystub(inventedTaxPaystub(fixture.agentId, upload.ID, "2026-03-06"))
		if err != nil {
			t.Fatalf("PutFinancePaystub: %s", err)
		}
		if stored.GrossAmount != "4000.0000" || stored.PeriodEndOn != "2026-02-28" || len(stored.PaystubLines) != 4 {
			t.Fatalf("kept as written: %+v", stored)
		}
		if line := stored.PaystubLines[3]; line.PaystubDeductionKind != models.PaystubDeductionKindHSA || line.EmployerYearToDateAmount != "250.0000" {
			t.Fatalf("a deduction's employer part is kept: %+v", line)
		}
		again := inventedTaxPaystub(fixture.agentId, upload.ID, "2026-03-06")
		again.NetAmount = "2721.00"
		again.PaystubLines = again.PaystubLines[:2]
		replaced, err := tx.PutFinancePaystub(again)
		if err != nil || replaced.ID != stored.ID || replaced.NetAmount != "2721.0000" || len(replaced.PaystubLines) != 2 {
			t.Fatalf("reading the upload again replaces the paystub: %+v %v", replaced, err)
		}
		second := createTaxUpload(t, tx, fixture.agentId, "later.pdf")
		if _, err := tx.PutFinancePaystub(inventedTaxPaystub(fixture.agentId, second.ID, "2026-03-20")); err != nil {
			t.Fatalf("PutFinancePaystub: %s", err)
		}
		page, err := tx.ListFinancePaystubs(fixture.agentId, &db.FinancePaystubFilter{ShouldCountTotal: true})
		if err != nil || page.TotalCount != 2 || page.FinancePaystubs[0].PayDate != "2026-03-20" {
			t.Fatalf("the latest pay date first: %+v %v", page, err)
		}
		page, err = tx.ListFinancePaystubs(fixture.agentId, &db.FinancePaystubFilter{From: "2026-03-01", To: "2026-03-10"})
		if err != nil || len(page.FinancePaystubs) != 1 || page.FinancePaystubs[0].ID != stored.ID {
			t.Fatalf("pay dates bound the listing: %+v %v", page, err)
		}
		if found, err := tx.GetFinancePaystub(stranger.agentId, stored.ID); err != nil || found != nil {
			t.Fatalf("another agent finds nothing: %+v %v", found, err)
		}
		if _, err := tx.PutFinancePaystub(inventedTaxPaystub(stranger.agentId, upload.ID, "2026-03-06")); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent's upload is refused: %v", err)
		}
		withFullNumber := inventedTaxPaystub(fixture.agentId, upload.ID, "2026-03-06")
		withFullNumber.EmployeeTaxIDMask = "123450123"
		if _, err := tx.PutFinancePaystub(withFullNumber); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("more than four digits of a taxpayer number is refused: %v", err)
		}
	})
}

// One upload is one document: read as a W-2 after it was read as a
// paystub, the paystub goes. Deleting the document deletes the upload.
func TestFinanceTaxFormsTakeTheirUploadsPlace(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		upload := createTaxUpload(t, tx, fixture.agentId, "form.pdf")
		paystub, err := tx.PutFinancePaystub(inventedTaxPaystub(fixture.agentId, upload.ID, "2026-03-06"))
		if err != nil {
			t.Fatalf("PutFinancePaystub: %s", err)
		}
		taxForm, err := tx.PutFinanceTaxForm(inventedTaxForm(fixture.agentId, upload.ID, 2025))
		if err != nil {
			t.Fatalf("PutFinanceTaxForm: %s", err)
		}
		if found, _ := tx.GetFinancePaystub(fixture.agentId, paystub.ID); found != nil {
			t.Fatalf("the paystub read from the same upload went: %+v", found)
		}
		if len(taxForm.TaxFormEntries) != 4 || taxForm.TaxFormEntries[1].Amount != "1800.0000" || !taxForm.TaxFormEntries[2].IsChecked || taxForm.TaxFormEntries[3].TextValue != "123-4567-8" {
			t.Fatalf("the boxes are kept: %+v", taxForm.TaxFormEntries)
		}
		other := createTaxUpload(t, tx, fixture.agentId, "older.pdf")
		if _, err := tx.PutFinanceTaxForm(inventedTaxForm(fixture.agentId, other.ID, 2024)); err != nil {
			t.Fatalf("PutFinanceTaxForm: %s", err)
		}
		page, err := tx.ListFinanceTaxForms(fixture.agentId, &db.FinanceTaxFormFilter{TaxYear: 2025})
		if err != nil || len(page.FinanceTaxForms) != 1 || page.FinanceTaxForms[0].ID != taxForm.ID {
			t.Fatalf("one year's forms: %+v %v", page, err)
		}
		orphans, err := tx.ListOrphanAgentAttachments(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("ListOrphanAgentAttachments: %s", err)
		}
		for _, orphan := range orphans {
			if orphan.ID == upload.ID || orphan.ID == other.ID {
				t.Fatalf("a tax document's upload is never swept: %+v", orphan)
			}
		}
		deleted, err := tx.DeleteFinanceTaxForm(fixture.agentId, taxForm.ID)
		if err != nil || deleted.DeletedAgentAttachmentID != upload.ID {
			t.Fatalf("deleting the form deletes its upload: %+v %v", deleted, err)
		}
		if gone, _ := tx.GetAgentAttachment(upload.ID); gone != nil {
			t.Fatalf("the upload is gone: %+v", gone)
		}
		if _, err := tx.DeleteFinanceTaxForm(fixture.agentId, taxForm.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a form deleted twice is not found: %v", err)
		}
	})
}

// A file handed in as a tax document that nothing was read out of goes
// with the sweep once TaxDocumentUnreadRetention has passed; one a paystub
// was read out of stays.
func TestUnreadTaxDocumentIsSweptAfterItsRetention(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		unread, read := createTaxUpload(t, tx, fixture.agentId, "card.png"), createTaxUpload(t, tx, fixture.agentId, "stub.pdf")
		if _, err := tx.PutFinancePaystub(inventedTaxPaystub(fixture.agentId, read.ID, "2026-03-06")); err != nil {
			t.Fatalf("PutFinancePaystub: %s", err)
		}
		isSwept := func(when time.Time, attachmentId string) bool {
			orphans, err := tx.ListOrphanAgentAttachments(when)
			if err != nil {
				t.Fatalf("ListOrphanAgentAttachments: %s", err)
			}
			for _, orphan := range orphans {
				if orphan.ID == attachmentId {
					return true
				}
			}
			return false
		}
		if isSwept(time.Now().Add(time.Hour), unread.ID) {
			t.Fatal("an unread tax document is kept while it may still be read")
		}
		later := time.Now().Add(db.TaxDocumentUnreadRetention + time.Hour)
		if !isSwept(later, unread.ID) || isSwept(later, read.ID) {
			t.Fatal("after its retention an unread tax document goes, and one with a paystub stays")
		}
	})
}

// An upload handed in as a tax document forgets the text kept of it.
func TestMarkAgentAttachmentTaxDocumentForgetsItsText(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		upload, err := tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agentId, Name: "stub.pdf", ContentType: "application/pdf", Size: 10, Text: "read text"})
		if err != nil || upload.Text != "read text" {
			t.Fatalf("CreateAgentAttachment: %+v %v", upload, err)
		}
		if err := tx.MarkAgentAttachmentTaxDocument(fixture.agentId, upload.ID); err != nil {
			t.Fatalf("MarkAgentAttachmentTaxDocument: %s", err)
		}
		marked, err := tx.GetAgentAttachment(upload.ID)
		if err != nil || marked.Text != "" || !marked.IsTaxDocument {
			t.Fatalf("the text is gone and the mark is kept: %+v %v", marked, err)
		}
		if err := tx.MarkAgentAttachmentTaxDocument("another-agent", upload.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent's upload is not found: %v", err)
		}
	})
}

func TestFinanceTaxDocumentsMigrationReversesAndReapplies(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		upload := createTaxUpload(t, tx, fixture.agentId, "stub.pdf")
		if _, err := tx.PutFinancePaystub(inventedTaxPaystub(fixture.agentId, upload.ID, "2026-03-06")); err != nil {
			t.Fatalf("PutFinancePaystub: %s", err)
		}
	})
	var reversed []migrations.Migration
	for _, migration := range migrations.Migrations() {
		if migration.ID >= "0181_finance_tax_documents" {
			reversed = append(reversed, migration)
		}
	}
	if len(reversed) == 0 || reversed[0].ID != "0181_finance_tax_documents" {
		t.Fatal("there is no migration 0181_finance_tax_documents")
	}
	for index := len(reversed) - 1; index >= 0; index-- {
		dbtest.Exec(t, database, reversed[index].ReverseSQL)
	}
	if tables := dbtest.QueryString(t, database, `SELECT COUNT(*)::text FROM information_schema.tables
		WHERE table_name LIKE 'agent_finance_paystub%' OR table_name LIKE 'agent_finance_tax_form%'`); tables != "0" {
		t.Fatalf("the reverse drops the tables: %s left", tables)
	}
	for _, migration := range reversed {
		dbtest.Exec(t, database, migration.SQL)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		upload := createTaxUpload(t, tx, fixture.agentId, "again.pdf")
		if _, err := tx.PutFinanceTaxForm(inventedTaxForm(fixture.agentId, upload.ID, 2025)); err != nil {
			t.Fatalf("the tables work again: %s", err)
		}
	})
}
