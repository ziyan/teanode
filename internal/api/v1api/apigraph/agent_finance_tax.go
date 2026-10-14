package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Paystubs and tax forms: a person's pay statements and W-2s, each read
// out of one file handed in as a tax document, and the tax year put
// together from them. Part of the finance area (FinanceQuery,
// FinanceMutation). Only the last four digits of the person's taxpayer
// number are ever stored or answered.

// PaystubLine is one printed line of a paystub with an amount: its kind
// (earning, tax or deduction), what it says, an earning's hours and rate,
// its amount this period and for the year to date (a deduction's employee
// part), a tax's kind, jurisdiction and the wages it was figured on, and a
// deduction's kind and the employer's part. Its number is its place in the
// list.
type PaystubLine struct {
	PaystubLineKind          string `json:"paystubLineKind"`
	Description              string `json:"description" graphapi:"nullable"`
	Hours                    string `json:"hours" graphapi:"nullable"`
	RateAmount               string `json:"rateAmount" graphapi:"nullable"`
	CurrentAmount            string `json:"currentAmount" graphapi:"nullable"`
	YearToDateAmount         string `json:"yearToDateAmount" graphapi:"nullable"`
	PaystubTaxKind           string `json:"paystubTaxKind" graphapi:"nullable"`
	TaxJurisdictionCode      string `json:"taxJurisdictionCode" graphapi:"nullable"`
	CurrentWagesAmount       string `json:"currentWagesAmount" graphapi:"nullable"`
	YearToDateWagesAmount    string `json:"yearToDateWagesAmount" graphapi:"nullable"`
	PaystubDeductionKind     string `json:"paystubDeductionKind" graphapi:"nullable"`
	EmployerCurrentAmount    string `json:"employerCurrentAmount" graphapi:"nullable"`
	EmployerYearToDateAmount string `json:"employerYearToDateAmount" graphapi:"nullable"`
}

// RecordPaystubArguments are a paystub as read and the upload it was read
// from. Days are written 2006-01-02, amounts as printed without a sign.
// EmployeeTaxIDMask is the last four digits of the person's taxpayer
// number, never more. IsUnbalancedAccepted records one whose lines do not
// add up, marked so.
type RecordPaystubArguments struct {
	AgentAttachmentID     string        `json:"agentAttachmentId"`
	EmployerName          string        `json:"employerName"`
	EmployerTaxID         string        `json:"employerTaxId" graphapi:"nullable"`
	EmployeeName          string        `json:"employeeName"`
	EmployeeTaxIDMask     string        `json:"employeeTaxIdMask" graphapi:"nullable"`
	PayDate               string        `json:"payDate"`
	PeriodStartOn         string        `json:"periodStartOn" graphapi:"nullable"`
	PeriodEndOn           string        `json:"periodEndOn" graphapi:"nullable"`
	CurrencyCode          string        `json:"currencyCode" graphapi:"nullable"`
	GrossAmount           string        `json:"grossAmount"`
	YearToDateGrossAmount string        `json:"yearToDateGrossAmount" graphapi:"nullable"`
	TotalTaxAmount        string        `json:"totalTaxAmount" graphapi:"nullable"`
	TotalDeductionAmount  string        `json:"totalDeductionAmount" graphapi:"nullable"`
	NetAmount             string        `json:"netAmount"`
	PaystubLines          []PaystubLine `json:"paystubLines"`
	IsUnbalancedAccepted  *bool         `json:"isUnbalancedAccepted" graphapi:"nullable"`
}

// TaxFormEntry is one box of a tax form as read: its number ("1", "12"),
// what tells boxes of one number apart (box 12's letter, a state's code,
// a locality, box 14's label, box 13's checkbox), and its amount, text or
// mark.
type TaxFormEntry struct {
	BoxCode      string `json:"boxCode"`
	BoxQualifier string `json:"boxQualifier" graphapi:"nullable"`
	Amount       string `json:"amount" graphapi:"nullable"`
	TextValue    string `json:"textValue" graphapi:"nullable"`
	IsChecked    *bool  `json:"isChecked" graphapi:"nullable"`
}

// RecordTaxFormArguments are a tax form as read and the upload it was read
// from. TaxFormKind is w2. RecipientTaxIDMask is the last four digits of
// the person's taxpayer number, never more.
type RecordTaxFormArguments struct {
	AgentAttachmentID  string         `json:"agentAttachmentId"`
	TaxFormKind        string         `json:"taxFormKind" graphapi:"nullable"`
	TaxYear            int            `json:"taxYear"`
	IsCorrected        *bool          `json:"isCorrected" graphapi:"nullable"`
	PayerName          string         `json:"payerName"`
	PayerTaxID         string         `json:"payerTaxId" graphapi:"nullable"`
	RecipientName      string         `json:"recipientName"`
	RecipientTaxIDMask string         `json:"recipientTaxIdMask" graphapi:"nullable"`
	TaxFormEntries     []TaxFormEntry `json:"taxFormEntries"`
}

// PaystubArguments name one paystub.
type PaystubArguments struct {
	PaystubID string `json:"paystubId"`
}

// TaxFormArguments name one tax form.
type TaxFormArguments struct {
	TaxFormID string `json:"taxFormId"`
}

// PaystubsArguments narrow a listing of paystubs: one tax year's (paid in
// it), or pay dates from and to (2006-01-02, both included), and a page
// of at most 200 (60 when left out) after offset.
type PaystubsArguments struct {
	TaxYear *int   `json:"taxYear" graphapi:"nullable"`
	From    string `json:"from" graphapi:"nullable"`
	To      string `json:"to" graphapi:"nullable"`
	Limit   *int   `json:"limit" graphapi:"nullable"`
	Offset  *int   `json:"offset" graphapi:"nullable"`
}

// TaxFormsArguments narrow a listing of tax forms to one tax year, and a
// page of at most 200 (60 when left out) after offset.
type TaxFormsArguments struct {
	TaxYear *int `json:"taxYear" graphapi:"nullable"`
	Limit   *int `json:"limit" graphapi:"nullable"`
	Offset  *int `json:"offset" graphapi:"nullable"`
}

// TaxYearArguments name a tax year.
type TaxYearArguments struct {
	TaxYear int `json:"taxYear"`
}

// ReadTaxDocumentArguments name what to read a paystub or a tax form out
// of: a file uploaded to the agent's attachments, or a message in the
// person's mailbox, whose PDFs and pictures are each read on their own.
type ReadTaxDocumentArguments struct {
	AgentAttachmentID string `json:"agentAttachmentId" graphapi:"nullable"`
	MailboxItemID     string `json:"mailboxItemId" graphapi:"nullable"`
}

// PaystubPageView is one page of paystubs, whether there are more, and how
// many there are on every page.
type PaystubPageView struct {
	FinancePaystubs []*models.FinancePaystub `json:"financePaystubs"`
	HasMore         bool                     `json:"hasMore"`
	TotalCount      int                      `json:"totalCount"`
}

// TaxFormView is a tax form with what it says about itself (a W-2's
// Social Security and Medicare against their rates).
type TaxFormView struct {
	FinanceTaxForm  *models.FinanceTaxForm   `json:"financeTaxForm"`
	TaxFormFindings []finance.TaxFormFinding `json:"taxFormFindings"`
}

// TaxFormPageView is one page of tax forms.
type TaxFormPageView struct {
	TaxForms   []*TaxFormView `json:"taxForms"`
	HasMore    bool           `json:"hasMore"`
	TotalCount int            `json:"totalCount"`
}

// TaxDocumentReadingView is the uploads queued to be read, a job for each,
// whose runs say what was read; a message carries several files, or none.
type TaxDocumentReadingView struct {
	AgentAttachmentIDs []string `json:"agentAttachmentIds"`
	AgentJobIDs        []string `json:"agentJobIds"`
}

// taxDocumentError is a refusal about a paystub or a tax form as the API
// says it.
func taxDocumentError(err error) error {
	if errors.Is(err, finance.ErrTaxDocumentRefused) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return financeError(err)
}

func (self *graph) taxDocumentWorker(ctx context.Context) (*models.Agent, *agent.Agent, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, nil, agent.ErrUnavailable
	}
	return found, worker, nil
}

func taxFormView(taxForm *models.FinanceTaxForm) *TaxFormView {
	return &TaxFormView{FinanceTaxForm: taxForm, TaxFormFindings: finance.TaxFormFindings(taxForm)}
}

func (self *graph) RecordPaystub(ctx context.Context, arguments RecordPaystubArguments) (*models.FinancePaystub, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return nil, err
	}
	paystub := &models.FinancePaystub{
		AgentAttachmentID: arguments.AgentAttachmentID, EmployerName: arguments.EmployerName, EmployerTaxID: arguments.EmployerTaxID,
		EmployeeName: arguments.EmployeeName, EmployeeTaxIDMask: arguments.EmployeeTaxIDMask, PayDate: arguments.PayDate,
		PeriodStartOn: arguments.PeriodStartOn, PeriodEndOn: arguments.PeriodEndOn, CurrencyCode: arguments.CurrencyCode,
		GrossAmount: arguments.GrossAmount, YearToDateGrossAmount: arguments.YearToDateGrossAmount, TotalTaxAmount: arguments.TotalTaxAmount,
		TotalDeductionAmount: arguments.TotalDeductionAmount, NetAmount: arguments.NetAmount,
	}
	for index, line := range arguments.PaystubLines {
		paystub.PaystubLines = append(paystub.PaystubLines, &models.FinancePaystubLine{
			LineNumber: index + 1, PaystubLineKind: models.PaystubLineKind(strings.ToLower(strings.TrimSpace(line.PaystubLineKind))),
			Description: line.Description, Hours: line.Hours, RateAmount: line.RateAmount, CurrentAmount: line.CurrentAmount,
			YearToDateAmount: line.YearToDateAmount, PaystubTaxKind: models.PaystubTaxKind(strings.ToLower(strings.TrimSpace(line.PaystubTaxKind))),
			TaxJurisdictionCode: line.TaxJurisdictionCode, CurrentWagesAmount: line.CurrentWagesAmount, YearToDateWagesAmount: line.YearToDateWagesAmount,
			PaystubDeductionKind:  models.PaystubDeductionKind(strings.ToLower(strings.TrimSpace(line.PaystubDeductionKind))),
			EmployerCurrentAmount: line.EmployerCurrentAmount, EmployerYearToDateAmount: line.EmployerYearToDateAmount,
		})
	}
	recorded, err := worker.RecordPaystub(ctx, found, paystub, arguments.IsUnbalancedAccepted != nil && *arguments.IsUnbalancedAccepted)
	if err != nil {
		return nil, taxDocumentError(err)
	}
	return recorded, nil
}

func (self *graph) RecordTaxForm(ctx context.Context, arguments RecordTaxFormArguments) (*TaxFormView, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return nil, err
	}
	taxFormKind := strings.ToLower(strings.TrimSpace(arguments.TaxFormKind))
	if taxFormKind == "" {
		taxFormKind = string(models.TaxFormKindW2)
	}
	taxForm := &models.FinanceTaxForm{
		AgentAttachmentID: arguments.AgentAttachmentID, TaxFormKind: models.TaxFormKind(taxFormKind), TaxYear: arguments.TaxYear,
		IsCorrected: arguments.IsCorrected != nil && *arguments.IsCorrected, PayerName: arguments.PayerName, PayerTaxID: arguments.PayerTaxID,
		RecipientName: arguments.RecipientName, RecipientTaxIDMask: arguments.RecipientTaxIDMask,
	}
	for index, entry := range arguments.TaxFormEntries {
		taxForm.TaxFormEntries = append(taxForm.TaxFormEntries, &models.FinanceTaxFormEntry{
			EntryNumber: index + 1, BoxCode: entry.BoxCode, BoxQualifier: entry.BoxQualifier, Amount: entry.Amount,
			TextValue: entry.TextValue, IsChecked: entry.IsChecked != nil && *entry.IsChecked,
		})
	}
	recorded, err := worker.RecordTaxForm(ctx, found, taxForm)
	if err != nil {
		return nil, taxDocumentError(err)
	}
	return taxFormView(recorded), nil
}

func (self *graph) DeletePaystub(ctx context.Context, arguments PaystubArguments) (bool, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return false, err
	}
	if err := worker.DeletePaystub(ctx, found, arguments.PaystubID); err != nil {
		return false, taxDocumentError(err)
	}
	return true, nil
}

func (self *graph) DeleteTaxForm(ctx context.Context, arguments TaxFormArguments) (bool, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return false, err
	}
	if err := worker.DeleteTaxForm(ctx, found, arguments.TaxFormID); err != nil {
		return false, taxDocumentError(err)
	}
	return true, nil
}

func (self *graph) ReadTaxDocument(ctx context.Context, arguments ReadTaxDocumentArguments) (*TaxDocumentReadingView, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return nil, err
	}
	attachmentId, itemId := strings.TrimSpace(arguments.AgentAttachmentID), strings.TrimSpace(arguments.MailboxItemID)
	if (attachmentId == "") == (itemId == "") {
		return nil, fmt.Errorf("%w: give the uploaded file (agentAttachmentId) or the message (mailboxItemId) the paystub or W-2 is in, one of the two", api.ErrInvalidArguments)
	}
	var attachmentIds []string
	if attachmentId != "" {
		attachment, err := self.transaction(ctx).GetAgentAttachment(attachmentId)
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != found.ID {
			return nil, api.ErrNotFound
		}
		if agent.IsPDFAttachment(attachment) && !agent.CanReadPDFs() {
			return nil, fmt.Errorf("%w: this server cannot read PDFs, so upload a photo or a screenshot of %s", api.ErrInvalidArguments, attachment.Name)
		}
		attachmentIds = []string{attachment.ID}
	} else {
		items, _, err := self.requireItems(ctx, models.PermissionMailRead, []string{itemId})
		if err != nil {
			return nil, err
		}
		copied, err := worker.CopyMailTaxDocuments(ctx, found, items[0].MailID)
		if err != nil {
			return nil, err
		}
		if len(copied) == 0 {
			return nil, fmt.Errorf("%w: the message carries no PDF or picture to read a paystub or W-2 out of", api.ErrInvalidArguments)
		}
		for _, attachment := range copied {
			attachmentIds = append(attachmentIds, attachment.ID)
		}
	}
	view := &TaxDocumentReadingView{AgentAttachmentIDs: attachmentIds, AgentJobIDs: []string{}}
	tx := self.transaction(ctx)
	for _, id := range attachmentIds {
		job, err := worker.QueueTaxDocumentReading(tx, found.ID, id)
		if err != nil {
			return nil, financeError(err)
		}
		view.AgentJobIDs = append(view.AgentJobIDs, job.ID)
	}
	return view, nil
}

func (self *graph) FinancePaystub(ctx context.Context, arguments PaystubArguments) (*models.FinancePaystub, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	paystub, err := self.transaction(ctx).GetFinancePaystub(found.ID, strings.TrimSpace(arguments.PaystubID))
	if err != nil {
		return nil, err
	}
	if paystub == nil {
		return nil, api.ErrNotFound
	}
	return paystub, nil
}

func (self *graph) FinancePaystubs(ctx context.Context, arguments PaystubsArguments) (*PaystubPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	filter := &db.FinancePaystubFilter{ShouldCountTotal: true}
	if arguments.TaxYear != nil {
		if strings.TrimSpace(arguments.From) != "" || strings.TrimSpace(arguments.To) != "" {
			return nil, fmt.Errorf("%w: give a tax year or pay dates, not both", api.ErrInvalidArguments)
		}
		filter.From, filter.To = fmt.Sprintf("%04d-01-01", *arguments.TaxYear), fmt.Sprintf("%04d-12-31", *arguments.TaxYear)
	} else {
		if filter.From, err = dayArgument("from", arguments.From, ""); err != nil {
			return nil, err
		}
		if filter.To, err = dayArgument("to", arguments.To, ""); err != nil {
			return nil, err
		}
	}
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if filter.Offset, err = offsetArgument(arguments.Offset); err != nil {
		return nil, err
	}
	page, err := self.transaction(ctx).ListFinancePaystubs(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	paystubs := page.FinancePaystubs
	if paystubs == nil {
		paystubs = []*models.FinancePaystub{}
	}
	return &PaystubPageView{FinancePaystubs: paystubs, HasMore: page.HasMore, TotalCount: page.TotalCount}, nil
}

func (self *graph) FinanceTaxForm(ctx context.Context, arguments TaxFormArguments) (*TaxFormView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	taxForm, err := self.transaction(ctx).GetFinanceTaxForm(found.ID, strings.TrimSpace(arguments.TaxFormID))
	if err != nil {
		return nil, err
	}
	if taxForm == nil {
		return nil, api.ErrNotFound
	}
	return taxFormView(taxForm), nil
}

func (self *graph) FinanceTaxForms(ctx context.Context, arguments TaxFormsArguments) (*TaxFormPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	filter := &db.FinanceTaxFormFilter{ShouldCountTotal: true}
	if arguments.TaxYear != nil {
		filter.TaxYear = *arguments.TaxYear
	}
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if filter.Offset, err = offsetArgument(arguments.Offset); err != nil {
		return nil, err
	}
	page, err := self.transaction(ctx).ListFinanceTaxForms(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	view := &TaxFormPageView{TaxForms: []*TaxFormView{}, HasMore: page.HasMore, TotalCount: page.TotalCount}
	for _, taxForm := range page.FinanceTaxForms {
		view.TaxForms = append(view.TaxForms, taxFormView(taxForm))
	}
	return view, nil
}

func (self *graph) TaxYear(ctx context.Context, arguments TaxYearArguments) (*finance.TaxYearSummary, error) {
	found, worker, err := self.taxDocumentWorker(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := worker.TaxYear(ctx, found, arguments.TaxYear)
	if err != nil {
		return nil, financeError(err)
	}
	return summary, nil
}
