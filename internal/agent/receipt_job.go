package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The receipt job: a message sorting called a receipt, or a photo or text
// file the person uploaded to a finance transaction, is read by a model
// into the shape RecordReceipt takes and recorded through the same agent
// operation the finance tool uses, so the same check and the same matching
// apply. A message that turns out not to be a purchase records nothing,
// and reading the same source again replaces its receipt.

// QueueReceiptReading queues the receipt job for a source: a stored
// message in a mailbox (mailboxId given), or an uploaded file, which
// remembers the finance transaction it was uploaded to when one is given.
// The job's subject is the source alone: a job's subject has room for one
// id.
func (self *Agent) QueueReceiptReading(tx db.Transaction, agentId, mailboxId, sourceId, financeTransactionId string) (*models.AgentJob, error) {
	if financeTransactionId != "" {
		if mailboxId != "" {
			return nil, fmt.Errorf("%w: only an uploaded receipt is read for a finance transaction", db.ErrInvalidArguments)
		}
		if err := tx.SetAgentAttachmentFinanceTransaction(agentId, sourceId, financeTransactionId); err != nil {
			return nil, err
		}
	}
	return self.Enqueue(tx, models.AgentJobReadReceipt, agentId, mailboxId, sourceId)
}

// queueReceiptReading queues the receipt job for a message sorting just
// called a receipt: only on a server that offers finance, for a person
// with a finance account to match it to, and never for one the filter put
// in Junk or the person in the Trash.
func (self *Agent) queueReceiptReading(tx db.Transaction, run *Run, mail *models.Mail, insight *models.MailInsight) error {
	if insight.Category != "receipt" || run.Mailbox == nil || !isFinanceOffered(run.Configuration()) {
		return nil
	}
	financeAccounts, err := tx.ListFinanceAccounts(run.Agent.ID, "")
	if err != nil || len(financeAccounts) == 0 {
		return err
	}
	isFiled, err := self.filedAway(tx, run.Mailbox.ID, mail.ID)
	if err != nil || isFiled {
		return err
	}
	_, err = self.QueueReceiptReading(tx, run.Agent.ID, run.Mailbox.ID, mail.ID, "")
	return err
}

// receiptAmountText is an amount in the model's answer, as the decimal it
// wrote: a string, or a JSON number taken as written rather than through a
// float, which would turn 0.1 into 0.1000000000000000055511151231257827.
type receiptAmountText string

// UnmarshalJSON takes a string or a number as its text.
func (self *receiptAmountText) UnmarshalJSON(encoded []byte) error {
	trimmed := strings.TrimSpace(string(encoded))
	if trimmed == "null" {
		*self = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		*self = receiptAmountText(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(encoded, &number); err != nil {
		return fmt.Errorf("an amount is a decimal, not %s", cutRunes(trimmed, 40))
	}
	*self = receiptAmountText(number.String())
	return nil
}

// receiptAnswer is what the model is asked for: whether it is a purchase
// at all, and the receipt in the shape RecordReceipt takes.
type receiptAnswer struct {
	IsPurchase            bool                `json:"isPurchase"`
	MerchantName          string              `json:"merchantName"`
	MerchantReceiptNumber string              `json:"merchantReceiptNumber"`
	PurchasedOn           string              `json:"purchasedOn"`
	PurchasedAt           string              `json:"purchasedAt"`
	CurrencyCode          string              `json:"currencyCode"`
	SubtotalAmount        receiptAmountText   `json:"subtotalAmount"`
	TotalAmount           receiptAmountText   `json:"totalAmount"`
	PaymentAccountMask    receiptAmountText   `json:"paymentAccountMask"`
	ReceiptLines          []receiptAnswerLine `json:"receiptLines"`
}

// receiptAnswerLine is one line of the model's answer.
type receiptAnswerLine struct {
	LineNumber           int               `json:"lineNumber"`
	ReceiptLineKind      string            `json:"receiptLineKind"`
	Description          string            `json:"description"`
	Quantity             receiptAmountText `json:"quantity"`
	QuantityUnit         string            `json:"quantityUnit"`
	UnitPriceAmount      receiptAmountText `json:"unitPriceAmount"`
	LineAmount           receiptAmountText `json:"lineAmount"`
	TaxClassCode         string            `json:"taxClassCode"`
	DiscountedLineNumber int               `json:"discountedLineNumber"`
}

// receipt is the answer as a receipt to record.
func (self *receiptAnswer) receipt() *models.FinanceReceipt {
	receipt := &models.FinanceReceipt{
		MerchantName: self.MerchantName, MerchantReceiptNumber: self.MerchantReceiptNumber, PurchasedOn: strings.TrimSpace(self.PurchasedOn),
		CurrencyCode: self.CurrencyCode, SubtotalAmount: string(self.SubtotalAmount), TotalAmount: string(self.TotalAmount),
		PaymentAccountMask: strings.TrimSpace(string(self.PaymentAccountMask)),
	}
	if _, err := time.Parse(time.DateOnly, receipt.PurchasedOn); err != nil {
		receipt.PurchasedOn = ""
	}
	if purchasedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(self.PurchasedAt)); err == nil {
		receipt.PurchasedAt = &purchasedAt
	}
	for index, line := range self.ReceiptLines {
		lineNumber := line.LineNumber
		if lineNumber <= 0 {
			lineNumber = index + 1
		}
		receipt.ReceiptLines = append(receipt.ReceiptLines, &models.FinanceReceiptLine{
			LineNumber: lineNumber, ReceiptLineKind: models.ReceiptLineKind(strings.ToLower(strings.TrimSpace(line.ReceiptLineKind))),
			Description: line.Description, Quantity: string(line.Quantity), QuantityUnit: line.QuantityUnit,
			UnitPriceAmount: string(line.UnitPriceAmount), LineAmount: string(line.LineAmount), TaxClassCode: line.TaxClassCode,
			DiscountedLineNumber: line.DiscountedLineNumber,
		})
	}
	return receipt
}

// receiptSource is what a receipt job reads: the receipt's source fields,
// what to call it in the run's title, and the text or the picture to show
// the model. A source with nothing to read gives the reason instead.
type receiptSource struct {
	receipt              *models.FinanceReceipt
	financeTransactionId string
	sourceName           string
	receiptText          string
	pictures             []llm.ContentPart
	unreadableWhy        string
}

// readReceiptSource reads a receipt job's source: the stored message of a
// mailbox the person still grants, or an upload of the person's. Nil when
// it is gone.
func (self *Agent) readReceiptSource(ctx context.Context, run *Run, sourceId string) (*receiptSource, error) {
	configuration := run.Configuration()
	if run.Job.MailboxID != "" {
		if run.Mailbox == nil || run.Source == nil || !run.Source.Granted {
			return &receiptSource{unreadableWhy: "the mailbox is no longer granted to the agent"}, nil
		}
		var mail *models.Mail
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			mail, err = tx.GetMail(sourceId, nil)
			return err
		}); err != nil {
			return nil, err
		}
		if mail == nil {
			return nil, nil
		}
		messageContext, err := BuildMessageContext(ctx, run.Storage(), mail, configuration.Agent.Limits.MaxBodyCharacters, false)
		if err != nil {
			// The message is stored just after the delivery commits; a job
			// that runs first tries again on the ladder.
			return nil, fmt.Errorf("the message with the receipt cannot be read yet: %w", err)
		}
		return &receiptSource{
			receipt:     &models.FinanceReceipt{ReceiptSourceKind: models.ReceiptSourceKindMail, MailID: mail.ID},
			sourceName:  fmt.Sprintf("%q", mail.Subject),
			receiptText: messageContext.Render(),
		}, nil
	}
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(sourceId)
		return err
	}); err != nil {
		return nil, err
	}
	if attachment == nil || attachment.AgentID != run.Agent.ID {
		return nil, nil
	}
	source := &receiptSource{
		receipt:              &models.FinanceReceipt{ReceiptSourceKind: models.ReceiptSourceKindAttachment, AgentAttachmentID: attachment.ID},
		financeTransactionId: attachment.FinanceTransactionID, sourceName: attachment.Name,
	}
	switch {
	case IsImageAttachment(attachment.ContentType):
		if attachment.Size > attachmentImageBytes {
			source.unreadableWhy = fmt.Sprintf("the picture is %s, larger than a model takes", formatBytes(attachment.Size))
			return source, nil
		}
		if run.Storage() == nil {
			return nil, fmt.Errorf("there is no storage to read the picture of the receipt from")
		}
		content, err := run.Storage().GetFile(ctx, attachment.ID)
		if err != nil {
			return nil, fmt.Errorf("the picture of the receipt cannot be read yet: %w", err)
		}
		source.pictures = []llm.ContentPart{{Type: "image", MediaType: attachment.ContentType, Data: content}}
	case strings.TrimSpace(attachment.Text) != "":
		source.receiptText = attachment.Text
	default:
		source.unreadableWhy = "no text could be read out of it here; a PDF's text is not read on the server, so upload a photo or a screenshot of it"
	}
	return source, nil
}

// receiptModelReady says why no model can read the receipt, or "" when one
// can: the one the bulk work runs on, which is also the one the night shows
// pictures to.
func (self *Agent) receiptModelReady(configuration *config.Configuration) string {
	if !self.canThink(configuration) || self.settings.Registry == nil {
		return "no model is configured to read it"
	}
	if _, _, err := self.settings.Registry.ForModel(configuration.Agent.Models.ForWork(config.AgentWorkScan)); err != nil {
		return "no model is configured to read it"
	}
	return ""
}

// runReadReceipt is the handler for a receipt job.
func (self *Agent) runReadReceipt(ctx context.Context, run *Run) error {
	// Whether any model can read it is asked first, before anything is
	// fetched out of the store.
	if unreadableWhy := self.receiptModelReady(run.Configuration()); unreadableWhy != "" {
		return noteReceiptRun(ctx, run, "the receipt", unreadableWhy)
	}
	source, err := self.readReceiptSource(ctx, run, run.Job.SubjectID)
	if err != nil {
		return err
	}
	if source == nil {
		return nil // the message or the upload is gone
	}
	if source.unreadableWhy != "" {
		return noteReceiptRun(ctx, run, source.sourceName, source.unreadableWhy)
	}
	prompt, err := render("read_receipt.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"IsPicture":  len(source.pictures) > 0,
		"Receipt":    fenced(source.receiptText),
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Reading the receipt "+source.sourceName, prompt, source.pictures, noTools, 1,
		models.AgentJobReadReceipt, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[receiptAnswer](thinking.Text, "isPurchase")
	if !answer.IsValid {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and could not tell what the answer said: %s", source.sourceName, answer.Problem))
		return nil
	}
	if !answer.Value.IsPurchase {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s: not a purchase, so nothing was recorded", source.sourceName))
		return nil
	}
	receipt := answer.Value.receipt()
	receipt.ReceiptSourceKind, receipt.MailID, receipt.AgentAttachmentID = source.receipt.ReceiptSourceKind, source.receipt.MailID, source.receipt.AgentAttachmentID
	// Nobody is here to ask whether a receipt that does not add up should
	// be kept, so it is kept, marked unbalanced for the person to see; nor
	// whether one that cannot be matched to the charge it was uploaded to
	// should be, so it is kept unmatched, the run saying why. A refusal
	// ends the job without a retry, since reading again would read the
	// same, so refusing either would lose the reading for good.
	recorded, err := self.RecordReceipt(ctx, run.Agent, receipt, ReceiptRecording{
		IsUnbalancedAccepted: true, FinanceTransactionID: source.financeTransactionId, IsRecordedWhenHandMatchRefused: true,
	})
	if errors.Is(err, finance.ErrReceiptRefused) || errors.Is(err, db.ErrInvalidArguments) || errors.Is(err, db.ErrNotFound) {
		// What the model read cannot be a receipt; reading it again would
		// read the same, so the job ends here and says why.
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and recorded nothing: %s", source.sourceName, err))
		return nil
	}
	if err != nil {
		return err
	}
	title := receiptRunTitle(recorded)
	if len(recorded.DroppedReceiptMatchReasons) > 0 {
		title += "; " + strings.Join(recorded.DroppedReceiptMatchReasons, "; ")
	}
	self.retitle(ctx, run, thinking.Conversation, title)
	return nil
}

// noteReceiptRun records a receipt job that read nothing, saying why.
func noteReceiptRun(ctx context.Context, run *Run, sourceName, unreadableWhy string) error {
	if sourceName == "" {
		sourceName = "the receipt"
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := noteRun(tx, run, fmt.Sprintf("Did not read %s: %s", sourceName, unreadableWhy))
		return err
	})
}

// receiptRunTitle says in a line what a receipt job read and what it
// matched.
func receiptRunTitle(recorded *RecordedReceipt) string {
	stored := recorded.FinanceReceipt
	title := fmt.Sprintf("Read a receipt from %s for %s %s", stored.MerchantName,
		finance.FormatReceiptAmount(stored.TotalAmount, stored.CurrencyCode), stored.CurrencyCode)
	if stored.ReceiptCheckState == models.ReceiptCheckStateUnbalanced {
		title += " (" + finance.ReceiptCheckDifferenceWords(finance.FormatReceiptAmount(stored.CheckDifferenceAmount, stored.CurrencyCode), stored.CurrencyCode) + ")"
	}
	if recorded.HandMatchRefusalReason != "" && len(stored.ReceiptMatches) == 0 {
		title += ": not matched to the charge it was uploaded to, since " + strings.TrimSuffix(recorded.HandMatchRefusalReason, ".")
		if candidateCount := len(recorded.ReceiptMatchCandidates); candidateCount > 0 {
			title += fmt.Sprintf("; %s it could explain", countOf(candidateCount, "charge", "charges"))
		}
		return title
	}
	switch {
	case len(stored.ReceiptMatches) == 1:
		title += ": matched to the charge of " + finance.FormatReceiptAmount(stored.ReceiptMatches[0].MatchedAmount, stored.CurrencyCode)
	case len(stored.ReceiptMatches) > 1:
		title += fmt.Sprintf(": matched to %d charges", len(stored.ReceiptMatches))
	case len(recorded.ReceiptMatchCandidates) > 0:
		title += fmt.Sprintf(": not matched, %s it could explain", countOf(len(recorded.ReceiptMatchCandidates), "charge", "charges"))
	default:
		title += ": no charge it could explain was found"
	}
	return title
}
