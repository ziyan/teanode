package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// inventedPaystubArguments is an invented paycheck that adds up: 4,000.00
// gross, 880.00 in taxes, 400.00 in deductions.
func inventedPaystubArguments(attachmentId, payDate string) RecordPaystubArguments {
	return RecordPaystubArguments{
		AgentAttachmentID: attachmentId, EmployerName: "Example Employer LLC", EmployeeName: "Pat Example", EmployeeTaxIDMask: "0123",
		PayDate: payDate, CurrencyCode: "USD", GrossAmount: "4000.00", YearToDateGrossAmount: "20000.00", NetAmount: "2720.00",
		PaystubLines: []PaystubLine{
			{PaystubLineKind: "earning", Description: "Regular", CurrentAmount: "4000.00", YearToDateAmount: "20000.00"},
			{PaystubLineKind: "tax", Description: "Federal", PaystubTaxKind: "federal_income", CurrentAmount: "500.00", YearToDateAmount: "2500.00",
				CurrentWagesAmount: "3600.00", YearToDateWagesAmount: "18000.00"},
			{PaystubLineKind: "tax", Description: "State", PaystubTaxKind: "state_income", TaxJurisdictionCode: "CA", CurrentAmount: "380.00", YearToDateAmount: "1900.00"},
			{PaystubLineKind: "deduction", Description: "401(k)", PaystubDeductionKind: "retirement_traditional", CurrentAmount: "400.00", YearToDateAmount: "2000.00"},
		},
	}
}

// A paystub and a W-2 are recorded from the person's uploads, listed,
// read one by one, put together as a tax year and deleted; one that does
// not add up is refused until the person says to record it; another
// person reaches none of it, nor reads a file of the person's.
func TestPaystubsAndTaxFormsOnTheAPI(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	var stub, form *models.AgentAttachment
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if stub, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "stub.pdf", ContentType: "application/pdf", Size: 4, Text: "kept"}); err != nil {
			test.Fatalf("CreateAgentAttachment: %s", err)
		}
		if form, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.ownerAgent.ID, Name: "w2.pdf", ContentType: "application/pdf", Size: 4, IsTaxDocument: true}); err != nil {
			test.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	var paystubId, taxFormId string
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		unbalanced := inventedPaystubArguments(stub.ID, "2026-03-06")
		unbalanced.NetAmount = "2700.00"
		if _, err := resolver.RecordPaystub(ctx, unbalanced); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "20.00") {
			test.Fatalf("a paystub that does not add up is refused, naming the difference: %v", err)
		}
		isAccepted := true
		unbalanced.IsUnbalancedAccepted = &isAccepted
		accepted, err := resolver.RecordPaystub(ctx, unbalanced)
		if err != nil || accepted.PaystubCheckState != models.PaystubCheckStateUnbalanced {
			test.Fatalf("recorded as it is when the person says so: %+v %v", accepted, err)
		}
		recorded, err := resolver.RecordPaystub(ctx, inventedPaystubArguments(stub.ID, "2026-03-06"))
		if err != nil || recorded.ID != accepted.ID || recorded.PaystubCheckState != models.PaystubCheckStateBalanced {
			test.Fatalf("recording the same file again replaces it: %+v %v", recorded, err)
		}
		paystubId = recorded.ID
		marked, _ := tx.GetAgentAttachment(stub.ID)
		if marked.Text != "" || !marked.IsTaxDocument {
			test.Fatalf("the file is a tax document now, its text forgotten: %+v", marked)
		}
		view, err := resolver.RecordTaxForm(ctx, RecordTaxFormArguments{
			AgentAttachmentID: form.ID, TaxYear: 2025, PayerName: "Example Employer LLC", RecipientName: "Pat Example",
			TaxFormEntries: []TaxFormEntry{{BoxCode: "3", Amount: "10000.00"}, {BoxCode: "4", Amount: "620.00"}},
		})
		if err != nil || view.FinanceTaxForm.TaxFormKind != models.TaxFormKindW2 || len(view.TaxFormFindings) != 2 || !view.TaxFormFindings[0].IsAgreeing {
			test.Fatalf("the W-2 is recorded and agrees with itself: %+v %v", view, err)
		}
		taxFormId = view.FinanceTaxForm.ID
		if _, err := resolver.RecordTaxForm(ctx, RecordTaxFormArguments{AgentAttachmentID: form.ID, TaxYear: 2025, PayerName: "Example", RecipientName: "Pat Example",
			RecipientTaxIDMask: "123-45-0123", TaxFormEntries: []TaxFormEntry{{BoxCode: "1", Amount: "1.00"}}}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("more than four digits of a taxpayer number is refused: %v", err)
		}

		taxYear := 2026
		page, err := resolver.FinancePaystubs(ctx, PaystubsArguments{TaxYear: &taxYear})
		if err != nil || page.TotalCount != 1 || page.FinancePaystubs[0].ID != paystubId || len(page.FinancePaystubs[0].PaystubLines) != 4 {
			test.Fatalf("the year's paystubs: %+v %v", page, err)
		}
		formYear := 2025
		forms, err := resolver.FinanceTaxForms(ctx, TaxFormsArguments{TaxYear: &formYear})
		if err != nil || len(forms.TaxForms) != 1 || forms.TaxForms[0].FinanceTaxForm.ID != taxFormId {
			test.Fatalf("the year's W-2s: %+v %v", forms, err)
		}
		summary, err := resolver.TaxYear(ctx, TaxYearArguments{TaxYear: 2026})
		if err != nil || len(summary.TaxYearEmployments) != 1 || summary.TaxYearEmployments[0].TaxYearFigureSource != finance.TaxYearFigureSourcePaystubs ||
			summary.Figures.WagesAmount != "18000.00" || summary.Figures.RetirementTraditionalAmount != "2000.00" {
			test.Fatalf("the year from the paystub's year to date: %+v %v", summary, err)
		}
		reading, err := resolver.ReadTaxDocument(ctx, ReadTaxDocumentArguments{AgentAttachmentID: form.ID})
		if err != nil || len(reading.AgentJobIDs) != 1 || reading.AgentAttachmentIDs[0] != form.ID {
			test.Fatalf("reading a file queues one job: %+v %v", reading, err)
		}
		if _, err := resolver.ReadTaxDocument(ctx, ReadTaxDocumentArguments{}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("reading needs a file or a message: %v", err)
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.FinancePaystub(ctx, PaystubArguments{PaystubID: paystubId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's paystub is not found: %v", err)
		}
		if _, err := resolver.ReadTaxDocument(ctx, ReadTaxDocumentArguments{AgentAttachmentID: form.ID}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's file is not found: %v", err)
		}
		if _, err := resolver.RecordPaystub(ctx, inventedPaystubArguments(stub.ID, "2026-03-06")); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's file cannot be recorded from: %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if isDeleted, err := resolver.DeletePaystub(ctx, PaystubArguments{PaystubID: paystubId}); err != nil || !isDeleted {
			test.Fatalf("DeletePaystub: %v %v", isDeleted, err)
		}
		if isDeleted, err := resolver.DeleteTaxForm(ctx, TaxFormArguments{TaxFormID: taxFormId}); err != nil || !isDeleted {
			test.Fatalf("DeleteTaxForm: %v %v", isDeleted, err)
		}
		if _, err := resolver.FinanceTaxForm(ctx, TaxFormArguments{TaxFormID: taxFormId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("a deleted W-2 is not found: %v", err)
		}
	})
}
