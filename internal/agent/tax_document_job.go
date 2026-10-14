package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The tax document job: a file handed in as a paystub or a tax form is
// read by a model, which says which it is, into the shape RecordPaystub or
// RecordTaxForm takes, and recorded through the same operation the finance
// tool uses, so the same checks apply. The text the model reads has every
// number shaped like a taxpayer number masked to its last four digits, and
// the text is never stored. Reading the same file again replaces what was
// read out of it.

// taxDocumentAnswer is what the model is asked for.
type taxDocumentAnswer struct {
	DocumentKind string         `json:"documentKind"`
	Why          string         `json:"why"`
	Paystub      *paystubAnswer `json:"paystub"`
	TaxForm      *taxFormAnswer `json:"taxForm"`
}

type paystubAnswer struct {
	EmployerName          string              `json:"employerName"`
	EmployerTaxID         string              `json:"employerTaxId"`
	EmployeeName          string              `json:"employeeName"`
	EmployeeTaxIDMask     receiptAmountText   `json:"employeeTaxIdMask"`
	PayDate               string              `json:"payDate"`
	PeriodStartOn         string              `json:"periodStartOn"`
	PeriodEndOn           string              `json:"periodEndOn"`
	CurrencyCode          string              `json:"currencyCode"`
	GrossAmount           receiptAmountText   `json:"grossAmount"`
	YearToDateGrossAmount receiptAmountText   `json:"yearToDateGrossAmount"`
	TotalTaxAmount        receiptAmountText   `json:"totalTaxAmount"`
	TotalDeductionAmount  receiptAmountText   `json:"totalDeductionAmount"`
	NetAmount             receiptAmountText   `json:"netAmount"`
	PaystubLines          []paystubAnswerLine `json:"paystubLines"`
}

type paystubAnswerLine struct {
	PaystubLineKind          string            `json:"paystubLineKind"`
	Description              string            `json:"description"`
	Hours                    receiptAmountText `json:"hours"`
	RateAmount               receiptAmountText `json:"rateAmount"`
	CurrentAmount            receiptAmountText `json:"currentAmount"`
	YearToDateAmount         receiptAmountText `json:"yearToDateAmount"`
	PaystubTaxKind           string            `json:"paystubTaxKind"`
	TaxJurisdictionCode      string            `json:"taxJurisdictionCode"`
	CurrentWagesAmount       receiptAmountText `json:"currentWagesAmount"`
	YearToDateWagesAmount    receiptAmountText `json:"yearToDateWagesAmount"`
	PaystubDeductionKind     string            `json:"paystubDeductionKind"`
	EmployerCurrentAmount    receiptAmountText `json:"employerCurrentAmount"`
	EmployerYearToDateAmount receiptAmountText `json:"employerYearToDateAmount"`
}

type taxFormAnswer struct {
	TaxFormKind        string               `json:"taxFormKind"`
	TaxYear            int                  `json:"taxYear"`
	IsCorrected        bool                 `json:"isCorrected"`
	PayerName          string               `json:"payerName"`
	PayerTaxID         string               `json:"payerTaxId"`
	RecipientName      string               `json:"recipientName"`
	RecipientTaxIDMask receiptAmountText    `json:"recipientTaxIdMask"`
	TaxFormEntries     []taxFormAnswerEntry `json:"taxFormEntries"`
}

type taxFormAnswerEntry struct {
	BoxCode      receiptAmountText `json:"boxCode"`
	BoxQualifier string            `json:"boxQualifier"`
	Amount       receiptAmountText `json:"amount"`
	TextValue    string            `json:"textValue"`
	IsChecked    bool              `json:"isChecked"`
}

func lowerTrimmed(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

// paystub is the answer as a paystub to record.
func (self *paystubAnswer) paystub() *models.FinancePaystub {
	paystub := &models.FinancePaystub{
		EmployerName: self.EmployerName, EmployerTaxID: self.EmployerTaxID, EmployeeName: self.EmployeeName,
		EmployeeTaxIDMask: string(self.EmployeeTaxIDMask), PayDate: self.PayDate, PeriodStartOn: self.PeriodStartOn, PeriodEndOn: self.PeriodEndOn,
		CurrencyCode: self.CurrencyCode, GrossAmount: string(self.GrossAmount), YearToDateGrossAmount: string(self.YearToDateGrossAmount),
		TotalTaxAmount: string(self.TotalTaxAmount), TotalDeductionAmount: string(self.TotalDeductionAmount), NetAmount: string(self.NetAmount),
	}
	for index, line := range self.PaystubLines {
		paystub.PaystubLines = append(paystub.PaystubLines, &models.FinancePaystubLine{
			LineNumber: index + 1, PaystubLineKind: models.PaystubLineKind(lowerTrimmed(line.PaystubLineKind)), Description: line.Description,
			Hours: string(line.Hours), RateAmount: string(line.RateAmount), CurrentAmount: string(line.CurrentAmount), YearToDateAmount: string(line.YearToDateAmount),
			PaystubTaxKind: models.PaystubTaxKind(lowerTrimmed(line.PaystubTaxKind)), TaxJurisdictionCode: line.TaxJurisdictionCode,
			CurrentWagesAmount: string(line.CurrentWagesAmount), YearToDateWagesAmount: string(line.YearToDateWagesAmount),
			PaystubDeductionKind:  models.PaystubDeductionKind(lowerTrimmed(line.PaystubDeductionKind)),
			EmployerCurrentAmount: string(line.EmployerCurrentAmount), EmployerYearToDateAmount: string(line.EmployerYearToDateAmount),
		})
	}
	return paystub
}

// taxForm is the answer as a tax form to record.
func (self *taxFormAnswer) taxForm() *models.FinanceTaxForm {
	taxFormKind := lowerTrimmed(self.TaxFormKind)
	if taxFormKind == "" || taxFormKind == "w-2" || taxFormKind == "w2c" || taxFormKind == "w-2c" {
		taxFormKind = string(models.TaxFormKindW2)
	}
	taxForm := &models.FinanceTaxForm{
		TaxFormKind: models.TaxFormKind(taxFormKind), TaxYear: self.TaxYear, IsCorrected: self.IsCorrected,
		PayerName: self.PayerName, PayerTaxID: self.PayerTaxID, RecipientName: self.RecipientName, RecipientTaxIDMask: string(self.RecipientTaxIDMask),
	}
	for index, entry := range self.TaxFormEntries {
		taxForm.TaxFormEntries = append(taxForm.TaxFormEntries, &models.FinanceTaxFormEntry{
			EntryNumber: index + 1, BoxCode: string(entry.BoxCode), BoxQualifier: entry.BoxQualifier,
			Amount: string(entry.Amount), TextValue: entry.TextValue, IsChecked: entry.IsChecked,
		})
	}
	return taxForm
}

// taxDocumentPDFPageCount is the most pages of a scanned tax document shown
// to the model: a paystub is a page, a W-2 two, and every page is a
// picture's worth of tokens.
const taxDocumentPDFPageCount = 4

// taxDocumentSource is what a tax document job reads: the upload, and the
// masked text or the pictures to show the model, or why it cannot.
type taxDocumentSource struct {
	attachment    *models.AgentAttachment
	documentText  string
	pictures      []llm.ContentPart
	unreadableWhy string
}

// readTaxDocumentSource reads the job's upload out of storage: a picture
// as it is, a PDF's text (its first pages as pictures when it has none), a
// text file's text. Nil when the upload is gone or not the agent's.
func (self *Agent) readTaxDocumentSource(ctx context.Context, run *Run, attachmentId string) (*taxDocumentSource, error) {
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(attachmentId)
		return err
	}); err != nil {
		return nil, err
	}
	if attachment == nil || attachment.AgentID != run.Agent.ID {
		return nil, nil
	}
	source := &taxDocumentSource{attachment: attachment}
	if run.Storage() == nil {
		return nil, fmt.Errorf("there is no storage to read the document from")
	}
	content, err := run.Storage().GetFile(ctx, attachment.ID)
	if err != nil {
		return nil, fmt.Errorf("the document cannot be read yet: %w", err)
	}
	switch {
	case IsImageAttachment(attachment.ContentType):
		if len(content) > attachmentImageBytes {
			source.unreadableWhy = fmt.Sprintf("the picture is %s, larger than a model takes", formatBytes(int64(len(content))))
			return source, nil
		}
		source.pictures = []llm.ContentPart{{Type: "image", MediaType: attachment.ContentType, Data: content}}
	case isPDFContent(content):
		text, err := readPDFText(ctx, content)
		if errors.Is(err, ErrPDFReaderMissing) {
			source.unreadableWhy = "this server cannot read PDFs, so upload a photo or a screenshot of it"
			return source, nil
		}
		if err != nil {
			source.unreadableWhy = "the PDF could not be read: " + err.Error()
			return source, nil
		}
		if strings.TrimSpace(text) != "" {
			source.documentText = text
			return source, nil
		}
		pages, err := renderPDFPages(ctx, content, taxDocumentPDFPageCount)
		if err != nil {
			source.unreadableWhy = "the PDF has no text and could not be rendered: " + err.Error()
			return source, nil
		}
		for _, page := range pages {
			if len(page) <= attachmentImageBytes {
				source.pictures = append(source.pictures, llm.ContentPart{Type: "image", MediaType: "image/png", Data: page})
			}
		}
		if len(source.pictures) == 0 {
			source.unreadableWhy = "the PDF has no text, and no page of it renders to a picture small enough for a model"
		}
	case utf8.Valid(content) && isTextAttachment(attachment.Name, attachment.ContentType):
		source.documentText = string(content)
	default:
		source.unreadableWhy = "it is not a PDF, a picture or a text file"
	}
	return source, nil
}

// runReadTaxDocument is the handler for a tax document job.
func (self *Agent) runReadTaxDocument(ctx context.Context, run *Run) error {
	if unreadableWhy := self.receiptModelReady(run.Configuration()); unreadableWhy != "" {
		return noteTaxDocumentRun(ctx, run, "the document", unreadableWhy)
	}
	source, err := self.readTaxDocumentSource(ctx, run, run.Job.SubjectID)
	if err != nil {
		return err
	}
	if source == nil {
		return nil // the upload is gone
	}
	sourceName := source.attachment.Name
	if source.unreadableWhy != "" {
		return noteTaxDocumentRun(ctx, run, sourceName, source.unreadableWhy)
	}
	prompt, err := render("read_tax_document.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"IsPicture":  len(source.pictures) > 0,
		"Document":   fenced(capAttachmentText(finance.MaskTaxpayerNumbers(source.documentText))),
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Reading the tax document "+sourceName, prompt, source.pictures, noTools, 1,
		models.AgentJobTaxDocument, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[taxDocumentAnswer](thinking.Text, "documentKind")
	if !answer.IsValid {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and could not tell what the answer said: %s", sourceName, answer.Problem))
		return nil
	}
	var title string
	switch documentKind := lowerTrimmed(answer.Value.DocumentKind); {
	case documentKind == "paystub" && answer.Value.Paystub != nil:
		paystub := answer.Value.Paystub.paystub()
		paystub.AgentAttachmentID = source.attachment.ID
		// Nobody is here to ask whether a paystub that does not add up
		// should be kept, so it is kept, marked for the person to see.
		recorded, err := self.RecordPaystub(ctx, run.Agent, paystub, true)
		if isTaxDocumentRefusal(err) {
			title = fmt.Sprintf("Read %s, and recorded nothing: %s", sourceName, err)
			break
		}
		if err != nil {
			return err
		}
		title = paystubRunTitle(recorded)
	case (documentKind == "w2" || documentKind == "w-2") && answer.Value.TaxForm != nil:
		taxForm := answer.Value.TaxForm.taxForm()
		taxForm.AgentAttachmentID = source.attachment.ID
		recorded, err := self.RecordTaxForm(ctx, run.Agent, taxForm)
		if isTaxDocumentRefusal(err) {
			title = fmt.Sprintf("Read %s, and recorded nothing: %s", sourceName, err)
			break
		}
		if err != nil {
			return err
		}
		title = taxFormRunTitle(recorded)
	default:
		why := strings.TrimSpace(answer.Value.Why)
		if why == "" {
			why = "it is neither a paystub nor a W-2"
		}
		title = fmt.Sprintf("Read %s: not a paystub or a W-2, so nothing was recorded (%s)", sourceName, finance.MaskTaxpayerNumbers(why))
	}
	self.retitle(ctx, run, thinking.Conversation, title)
	return nil
}

// isTaxDocumentRefusal says the reading cannot be recorded, and reading it
// again would read the same, so the job ends and says why.
func isTaxDocumentRefusal(err error) bool {
	return errors.Is(err, finance.ErrTaxDocumentRefused) || errors.Is(err, db.ErrInvalidArguments) || errors.Is(err, db.ErrNotFound)
}

// noteTaxDocumentRun records a tax document job that read nothing, saying
// why.
func noteTaxDocumentRun(ctx context.Context, run *Run, sourceName, unreadableWhy string) error {
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := noteRun(tx, run, fmt.Sprintf("Did not read %s: %s", sourceName, unreadableWhy))
		return err
	})
}

// paystubRunTitle says in a line what a tax document job recorded.
func paystubRunTitle(paystub *models.FinancePaystub) string {
	title := fmt.Sprintf("Read a paystub from %s paid %s: %s gross, %s take-home", paystub.EmployerName, paystub.PayDate,
		finance.FormatReceiptAmount(paystub.GrossAmount, paystub.CurrencyCode), finance.FormatReceiptAmount(paystub.NetAmount, paystub.CurrencyCode))
	if paystub.PaystubCheckState == models.PaystubCheckStateUnbalanced {
		title += fmt.Sprintf(" (does not add up, by %s)", finance.FormatReceiptAmount(paystub.CheckDifferenceAmount, paystub.CurrencyCode))
	}
	return title
}

func taxFormRunTitle(taxForm *models.FinanceTaxForm) string {
	title := fmt.Sprintf("Read a %d W-2 from %s", taxForm.TaxYear, taxForm.PayerName)
	if taxForm.IsCorrected {
		title = fmt.Sprintf("Read a corrected %d W-2 from %s", taxForm.TaxYear, taxForm.PayerName)
	}
	disagreeing := 0
	for _, finding := range finance.TaxFormFindings(taxForm) {
		if !finding.IsAgreeing {
			disagreeing++
		}
	}
	if disagreeing > 0 {
		title += fmt.Sprintf(" (%s does not agree with itself)", countOf(disagreeing, "check", "checks"))
	}
	return title
}
