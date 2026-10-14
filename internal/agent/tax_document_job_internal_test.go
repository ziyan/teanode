package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// inventedPaystubAnswer is the model's reading of an invented paystub
// that adds up: 4,000.00 gross, 880.00 in taxes, 400.00 in deductions.
const inventedPaystubAnswer = `{"documentKind": "paystub", "paystub": {"employerName": "Example Employer LLC", "employeeName": "Pat Example",
"employeeTaxIdMask": "XXX-XX-0123", "payDate": "2026-03-06", "periodStartOn": "2026-02-15", "periodEndOn": "2026-02-28", "currencyCode": "USD",
"grossAmount": 4000.00, "totalTaxAmount": "880.00", "totalDeductionAmount": "400.00", "netAmount": "2720.00", "paystubLines": [
 {"paystubLineKind": "earning", "description": "Regular", "hours": "80", "currentAmount": "4000.00", "yearToDateAmount": "20000.00"},
 {"paystubLineKind": "tax", "description": "Federal", "paystubTaxKind": "federal_income", "currentAmount": "500.00", "currentWagesAmount": "3600.00"},
 {"paystubLineKind": "tax", "description": "State", "paystubTaxKind": "state_income", "taxJurisdictionCode": "ca", "currentAmount": "380.00"},
 {"paystubLineKind": "deduction", "description": "HSA", "paystubDeductionKind": "hsa", "currentAmount": "400.00", "employerCurrentAmount": "50.00"}]}}`

// inventedW2Answer is the model's reading of an invented W-2.
const inventedW2Answer = `{"documentKind": "w2", "taxForm": {"taxFormKind": "W-2", "taxYear": 2025, "payerName": "Example Employer LLC",
"payerTaxId": "12-3456789", "recipientName": "Pat Example", "recipientTaxIdMask": "0123", "taxFormEntries": [
 {"boxCode": "1", "amount": "21600.00"}, {"boxCode": 3, "amount": "22200.00"}, {"boxCode": "4", "amount": "1376.40"},
 {"boxCode": "12", "boxQualifier": "d", "amount": "1800.00"}]}}`

// taxDocumentUpload stores a file handed in as a tax document.
func (self *financeFixture) taxDocumentUpload(t *testing.T, name, contentType string, content []byte) *models.AgentAttachment {
	t.Helper()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: self.agent.ID, Name: name, ContentType: contentType, Size: int64(len(content)), IsTaxDocument: true})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := self.worker.settings.Storage.PutFile(t.Context(), attachment.ID, content); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	return attachment
}

func (self *financeFixture) taxDocumentJobRun(subjectId string) *Run {
	run := self.run()
	run.Job = &models.AgentJob{ID: "job-tax-document", AgentID: self.agent.ID, Kind: models.AgentJobTaxDocument, SubjectID: subjectId}
	return run
}

// A paystub PDF is read as text with every number shaped like a taxpayer
// number masked, recorded as a paystub that adds up, and read again it
// replaces what it read before. The run says what it read.
func TestTaxDocumentJobReadsAPaystub(t *testing.T) {
	requirePoppler(t)
	fixture, model := receiptJobFixture(t, inventedPaystubAnswer)
	upload := fixture.taxDocumentUpload(t, "stub.pdf", "application/pdf", inventedPDF("EXAMPLE EMPLOYER LLC PAY STATEMENT", "SSN 987654321", "NET PAY 2720.00"))
	for range 2 {
		if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(upload.ID)); err != nil {
			t.Fatalf("runReadTaxDocument: %s", err)
		}
	}
	prompt := model.prompts[0]
	if !strings.Contains(prompt, "NET PAY 2720.00") || !strings.Contains(prompt, "XXXXX4321") || strings.Contains(prompt, "987654321") || !strings.Contains(prompt, "untrusted-data") {
		t.Fatalf("the model is sent the PDF's text, the taxpayer number masked, inside the fence")
	}
	var paystubs []*models.FinancePaystub
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinancePaystubs(fixture.agent.ID, nil)
		if err != nil {
			t.Fatalf("ListFinancePaystubs: %s", err)
		}
		paystubs = page.FinancePaystubs
	})
	if len(paystubs) != 1 {
		t.Fatalf("reading the file twice keeps one paystub: %d", len(paystubs))
	}
	paystub := paystubs[0]
	if paystub.AgentAttachmentID != upload.ID || paystub.PaystubCheckState != models.PaystubCheckStateBalanced || paystub.EmployeeTaxIDMask != "0123" || len(paystub.PaystubLines) != 4 {
		t.Fatalf("the paystub is recorded from the file, adding up, with only four digits kept: %+v", paystub)
	}
	if line := paystub.PaystubLines[2]; line.TaxJurisdictionCode != "CA" || line.PaystubTaxKind != models.PaystubTaxKindStateIncome {
		t.Fatalf("the state tax keeps its state: %+v", line)
	}
	titles := fixture.taxDocumentRunTitles(t)
	if len(titles) == 0 || !strings.Contains(titles[0], "Read a paystub from Example Employer LLC paid 2026-03-06") {
		t.Fatalf("the run says what it read: %q", titles)
	}
}

// A W-2 answer records a tax form, and an answer of none records nothing.
func TestTaxDocumentJobReadsAW2AndLeavesTheRest(t *testing.T) {
	fixture, _ := receiptJobFixture(t, inventedW2Answer, `{"documentKind": "none", "why": "a holiday card"}`)
	form := fixture.taxDocumentUpload(t, "w2.png", "image/png", []byte("\x89PNG\r\n\x1a\nan invented picture"))
	if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(form.ID)); err != nil {
		t.Fatalf("runReadTaxDocument: %s", err)
	}
	other := fixture.taxDocumentUpload(t, "card.png", "image/png", []byte("\x89PNG\r\n\x1a\nanother invented picture"))
	if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(other.ID)); err != nil {
		t.Fatalf("runReadTaxDocument: %s", err)
	}
	var taxForms []*models.FinanceTaxForm
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTaxForms(fixture.agent.ID, nil)
		if err != nil {
			t.Fatalf("ListFinanceTaxForms: %s", err)
		}
		taxForms = page.FinanceTaxForms
	})
	if len(taxForms) != 1 || taxForms[0].AgentAttachmentID != form.ID || taxForms[0].TaxFormKind != models.TaxFormKindW2 || len(taxForms[0].TaxFormEntries) != 4 {
		t.Fatalf("one W-2, from the first picture: %+v", taxForms)
	}
	if entry := taxForms[0].TaxFormEntries[3]; entry.BoxCode != "12" || entry.BoxQualifier != "D" {
		t.Fatalf("box 12 D is normalized: %+v", entry)
	}
	titles := strings.Join(fixture.taxDocumentRunTitles(t), "\n")
	if !strings.Contains(titles, "Read a 2025 W-2 from Example Employer LLC") || !strings.Contains(titles, "not a paystub, a W-2, a 1099 or a 1098") {
		t.Fatalf("the runs say what they read: %q", titles)
	}
	summary, err := fixture.worker.TaxYear(t.Context(), fixture.agent, 2025, "")
	if err != nil || len(summary.TaxYearEmployments) != 1 || summary.Figures.WagesAmount != "21600.00" {
		t.Fatalf("the tax year takes the W-2's figures: %+v %v", summary, err)
	}
	if _, err := fixture.worker.TaxYear(t.Context(), fixture.agent, time.Now().Year(), ""); err != nil {
		t.Fatalf("a year with nothing in it is an empty year: %v", err)
	}
}

// taxDocumentRunTitles are the titles of the tax document job's runs.
func (self *financeFixture) taxDocumentRunTitles(t *testing.T) []string {
	t.Helper()
	var titles []string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(self.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatalf("ListAgentConversations: %s", err)
		}
		for _, conversation := range conversations {
			if conversation.JobKind == string(models.AgentJobTaxDocument) {
				titles = append(titles, conversation.Title)
			}
		}
	})
	return titles
}

// A W-2c is recorded as a corrected W-2, whatever the model called it.
func TestTaxFormAnswerFoldsAW2cIntoACorrection(t *testing.T) {
	for _, kind := range []string{"W-2c", "w2c"} {
		taxForm := (&taxFormAnswer{TaxFormKind: kind, TaxYear: 2025}).taxForm()
		if taxForm.TaxFormKind != models.TaxFormKindW2 || !taxForm.IsCorrected {
			t.Errorf("%s: %+v", kind, taxForm)
		}
	}
	if taxForm := (&taxFormAnswer{TaxFormKind: "W-2", TaxYear: 2025}).taxForm(); taxForm.IsCorrected {
		t.Errorf("a W-2 is not a correction: %+v", taxForm)
	}
}

// A 1099's kind is read however the model writes it.
func TestTaxFormAnswerReads1099Kinds(t *testing.T) {
	for written, kind := range map[string]models.TaxFormKind{"1099-INT": models.TaxFormKind1099Int, "Form 1099-DIV": models.TaxFormKind1099Div, "1099B": models.TaxFormKind1099B, "1099_b": models.TaxFormKind1099B} {
		if taxForm := (&taxFormAnswer{TaxFormKind: written, TaxYear: 2025}).taxForm(); taxForm.TaxFormKind != kind {
			t.Errorf("%s: %s", written, taxForm.TaxFormKind)
		}
	}
	answer := &taxFormAnswer{TaxFormKind: "1099-INT", TaxYear: 2025, TaxFormEntries: []taxFormAnswerEntry{{BoxCode: "1", Amount: "6.35"}, {BoxCode: "2", Amount: "0.00"}, {BoxCode: "3", Amount: "0"}}}
	if taxForm := answer.taxForm(); len(taxForm.TaxFormEntries) != 1 || taxForm.TaxFormEntries[0].EntryNumber != 1 {
		t.Errorf("boxes of nothing are left out: %+v", taxForm.TaxFormEntries)
	}
}

// A statement of two accounts with a 1099-DIV each keeps the first and
// says so; read again into nothing it can record, it keeps what it had.
func TestTaxDocumentJobReadsAStatementOfTwoAccounts(t *testing.T) {
	statement := `{"documentKind": "1099", "taxForms": [
 {"taxFormKind": "1099-INT", "taxYear": 2025, "payerName": "Example Brokerage", "recipientName": "Pat Example", "taxFormEntries": [{"boxCode": "1", "amount": "10.00"}]},
 {"taxFormKind": "1099-DIV", "taxYear": 2025, "payerName": "Example Brokerage", "recipientName": "Pat Example", "taxFormEntries": [{"boxCode": "1a", "amount": "20.00"}, {"boxCode": "account", "textValue": "XX1111"}]},
 {"taxFormKind": "1099-DIV", "taxYear": 2025, "payerName": "Example Brokerage", "recipientName": "Pat Example", "taxFormEntries": [{"boxCode": "1a", "amount": "30.00"}, {"boxCode": "account", "textValue": "XX2222"}]}]}`
	refused := `{"documentKind": "1099", "taxForms": [{"taxFormKind": "1099-DIV", "taxYear": 1066, "payerName": "Example Brokerage", "recipientName": "Pat Example", "taxFormEntries": [{"boxCode": "1a", "amount": "20.00"}]}]}`
	fixture, _ := receiptJobFixture(t, statement, refused)
	upload := fixture.taxDocumentUpload(t, "statement.png", "image/png", []byte("\x89PNG\r\n\x1a\nan invented statement"))
	for range 2 {
		if err := fixture.worker.runReadTaxDocument(t.Context(), fixture.taxDocumentJobRun(upload.ID)); err != nil {
			t.Fatalf("runReadTaxDocument: %s", err)
		}
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTaxForms(fixture.agent.ID, nil)
		if err != nil || len(page.FinanceTaxForms) != 2 {
			t.Fatalf("the 1099-INT and the first 1099-DIV, kept though the second reading recorded nothing: %+v %v", page, err)
		}
	})
	titles := strings.Join(fixture.taxDocumentRunTitles(t), "\n")
	if !strings.Contains(titles, "a second 1099-DIV in the file was not kept") {
		t.Fatalf("the run says what it left: %q", titles)
	}
}
