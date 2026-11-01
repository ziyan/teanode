package agent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Reading new photos: a picture that arrives in a person's library is
// described as soon as its previews are made, and a receipt, a statement or
// a tax document in it is handed to the job that records it
// (docs/decisions/20261031-a-new-photo-is-read-as-it-arrives.md). Older
// photos are still described only when somebody looks at them.

const (
	// photoReadingEvery is how often the new photos are looked for.
	photoReadingEvery = time.Minute

	// photoReadingWindow is how recent a photo is to be new: taken, or
	// added when it carries no date, this long ago at most. An archive
	// of old pictures imported today is not read.
	photoReadingWindow = 30 * 24 * time.Hour

	// photoReadingBatch is the most photos of one person queued in one look,
	// so that a day of a thousand pictures is read over a quarter of an hour
	// rather than queued all at once.
	photoReadingBatch = 10

	// photoReadingWaitingMost is how many descriptions of one person may wait
	// before more are queued: with a model slower than the sweep, the queue
	// stays short and the person's own turns do not wait behind a backlog.
	photoReadingWaitingMost = 2 * photoReadingBatch
)

// queuePhotoReading queues the description of each person's new photos,
// for the people whose switch is on.
func (self *Agent) queuePhotoReading(ctx context.Context, now time.Time) {
	if now.Sub(self.lastPhotoReading) < photoReadingEvery || self.PhotoDescriptionUnavailableMessage(self.settings.Configuration()) != "" {
		return
	}
	self.lastPhotoReading = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to read new photos for: %s", err)
		return
	}
	for _, agent := range agents {
		if ctx.Err() != nil {
			return
		}
		if !agent.Active() || agent.UserID == "" {
			continue
		}
		// Every instance lists the same photos; the job queue keeps one job
		// per photo, and the mark keeps a photo from being listed again.
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			isEnabled, err := tx.IsPhotoReadingEnabled(agent.UserID)
			if err != nil || !isEnabled {
				return err
			}
			waitingCount, err := tx.CountAgentJobs(&db.AgentJobFilter{
				AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobDescribePhoto},
				Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
			})
			if err != nil || waitingCount >= photoReadingWaitingMost {
				return err
			}
			photos, err := tx.ListPhotosToRead(agent.UserID, now.Add(-photoReadingWindow), min(photoReadingBatch, photoReadingWaitingMost-int(waitingCount)))
			if err != nil {
				return err
			}
			for _, photo := range photos {
				if _, err := self.QueuePhotoDescription(tx, agent.ID, photo.ID); err != nil {
					return err
				}
				if err := tx.MarkPhotoReadingQueued(agent.UserID, photo.ID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			log.Warningf("cannot queue the new photos of agent %q to be read: %s", agent.ID, err)
		}
	}
}

// photoDocumentUnavailableMessage says why a document photo of the person
// is not handed on to be recorded, or "" when it is: their switch is on,
// the server offers finance, and the photo is their own.
func (self *Agent) photoDocumentUnavailableMessage(tx db.Transaction, configuration *config.Configuration, photo *models.Photo) (string, error) {
	if photo.PhotoSource == models.PhotoSourceShared {
		return "it was saved from somebody else's share", nil
	}
	if !isFinanceOffered(configuration) {
		return "this server does not offer finance", nil
	}
	isEnabled, err := tx.IsPhotoReadingEnabled(photo.UserID)
	if err != nil {
		return "", err
	}
	if !isEnabled {
		return "reading new photos is off", nil
	}
	return "", nil
}

// handOnPhotoDocument copies a photo the description said is a document into
// an upload of the agent's and queues the job that records that kind of
// document. A photo is handed on once: a second description of it finds its
// copy and does nothing. It answers what it did, for the run's title.
func (self *Agent) handOnPhotoDocument(ctx context.Context, run *Run, photo *models.Photo, documentKind models.PhotoDocumentKind, picture []byte, contentType string) (string, error) {
	if documentKind == models.PhotoDocumentNone || documentKind == "" {
		return "", nil
	}
	if run.Storage() == nil {
		return "", fmt.Errorf("there is no storage to keep the document of photo %s in", photo.ID)
	}
	handedOn := ""
	err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		why, err := self.photoDocumentUnavailableMessage(tx, run.Configuration(), photo)
		if err != nil {
			return err
		}
		if why != "" {
			handedOn = "not recorded, since " + why
			return nil
		}
		existing, err := tx.GetAgentAttachmentOfPhoto(run.Agent.ID, photo.ID)
		if err != nil || existing != nil {
			return err
		}
		attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: run.Agent.ID, Name: photoDocumentName(photo), ContentType: contentType, Size: int64(len(picture)),
			PhotoID: photo.ID, IsTaxDocument: documentKind == models.PhotoDocumentTaxDocument,
		})
		if err != nil {
			return err
		}
		if err := run.Storage().PutFile(ctx, attachment.ID, picture); err != nil {
			return err
		}
		switch documentKind {
		case models.PhotoDocumentReceipt:
			_, err = self.QueueReceiptReading(tx, run.Agent.ID, "", attachment.ID, "")
			handedOn = "queued to be recorded as a receipt"
		case models.PhotoDocumentStatement:
			_, err = self.Enqueue(tx, models.AgentJobStatementPhoto, run.Agent.ID, "", attachment.ID)
			handedOn = "queued for its transactions to be imported"
		case models.PhotoDocumentTaxDocument:
			_, err = self.QueueTaxDocumentReading(tx, run.Agent.ID, attachment.ID)
			handedOn = "queued to be recorded as a tax document"
		}
		return err
	})
	return handedOn, err
}

// photoDocumentName is the name a photo's document copy has: the photo's
// own, as a JPEG, since the copy is its large preview.
func photoDocumentName(photo *models.Photo) string {
	name := strings.TrimSpace(photo.OriginalFilename)
	if name == "" {
		return "photo-" + photo.ID + ".jpg"
	}
	return strings.TrimSuffix(name, path.Ext(name)) + ".jpg"
}

// recordedPurchaseOf is the receipt the agent holds already of the same
// purchase as this one, or nil: a receipt read from a photo is recorded
// only when it is not.
func (self *Agent) recordedPurchaseOf(ctx context.Context, agentId string, receipt *models.FinanceReceipt) (*models.FinanceReceipt, error) {
	if receipt.PurchasedOn == "" {
		return nil, nil
	}
	var page *db.FinanceReceiptPage
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		page, err = tx.ListFinanceReceipts(agentId, &db.FinanceReceiptFilter{From: receipt.PurchasedOn, To: receipt.PurchasedOn, Limit: db.FinanceReceiptLimitMost})
		return err
	}); err != nil {
		return nil, err
	}
	for _, recorded := range page.FinanceReceipts {
		if finance.IsSamePurchase(recorded, receipt) {
			return recorded, nil
		}
	}
	return nil, nil
}

// statementPhotoAnswer is what the model is asked for: whether the picture
// is one account's transactions, and the rows ImportTransactionRows takes.
type statementPhotoAnswer struct {
	IsStatement            bool                      `json:"isStatement"`
	InstitutionName        string                    `json:"institutionName"`
	AccountName            string                    `json:"accountName"`
	AccountNumber          string                    `json:"accountNumber"`
	IsAccountNumberPartial bool                      `json:"isAccountNumberPartial"`
	StatementAccountKind   string                    `json:"statementAccountKind"`
	CurrencyCode           string                    `json:"currencyCode"`
	LedgerBalanceAmount    receiptAmountText         `json:"ledgerBalanceAmount"`
	LedgerBalanceOn        string                    `json:"ledgerBalanceOn"`
	TransactionRows        []statementPhotoAnswerRow `json:"transactionRows"`
	MonthlyTotals          []statementPhotoTotal     `json:"monthlyTotals"`
}

// statementPhotoAnswerRow is one transaction of the model's answer.
type statementPhotoAnswerRow struct {
	PostedOn             string            `json:"postedOn"`
	Description          string            `json:"description"`
	Amount               receiptAmountText `json:"amount"`
	TransactionKind      string            `json:"transactionKind"`
	RunningBalanceAmount receiptAmountText `json:"runningBalanceAmount"`
	TotalMonth           string            `json:"totalMonth"`
}

// statementPhotoTotal is one month's total of the model's answer.
type statementPhotoTotal struct {
	TotalMonth  string            `json:"totalMonth"`
	TotalAmount receiptAmountText `json:"totalAmount"`
}

// transactionRowsInput is the answer as the rows an import checks.
func (self *statementPhotoAnswer) transactionRowsInput() *finance.TransactionRowsInput {
	input := &finance.TransactionRowsInput{
		InstitutionName: self.InstitutionName, AccountName: self.AccountName, AccountNumber: strings.TrimSpace(self.AccountNumber),
		IsAccountNumberPartial: self.IsAccountNumberPartial, StatementAccountKind: finance.StatementAccountKind(strings.ToLower(strings.TrimSpace(self.StatementAccountKind))),
		CurrencyCode: strings.ToUpper(strings.TrimSpace(self.CurrencyCode)), LedgerBalanceAmount: string(self.LedgerBalanceAmount), LedgerBalanceOn: strings.TrimSpace(self.LedgerBalanceOn),
	}
	for _, row := range self.TransactionRows {
		input.TransactionRows = append(input.TransactionRows, finance.TransactionRow{
			PostedOn: strings.TrimSpace(row.PostedOn), Description: row.Description, Amount: string(row.Amount),
			TransactionKind: strings.ToLower(strings.TrimSpace(row.TransactionKind)), RunningBalanceAmount: string(row.RunningBalanceAmount),
			TotalMonth: strings.TrimSpace(row.TotalMonth),
		})
	}
	for _, total := range self.MonthlyTotals {
		input.MonthlyTotals = append(input.MonthlyTotals, finance.MonthlyTotal{TotalMonth: strings.TrimSpace(total.TotalMonth), TotalAmount: string(total.TotalAmount)})
	}
	return input
}

// runReadStatementPhoto is the handler for a statement photo job: the
// picture's rows are read and imported into the account of imported
// statements they are of. Nobody is there to say which account a picture
// shows, so rows the server cannot place in an account the person imported
// before are not imported, and the run says why; the person can send the
// picture to their agent, which asks.
func (self *Agent) runReadStatementPhoto(ctx context.Context, run *Run) error {
	if unreadableWhy := self.receiptModelReady(run.Configuration()); unreadableWhy != "" {
		return noteReceiptRun(ctx, run, "the statement", unreadableWhy)
	}
	var attachment *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		attachment, err = tx.GetAgentAttachment(run.Job.SubjectID)
		return err
	}); err != nil {
		return err
	}
	if attachment == nil || attachment.AgentID != run.Agent.ID {
		return nil // the copy is gone
	}
	if !IsImageAttachment(attachment.ContentType) || attachment.Size > attachmentImageBytes {
		return noteReceiptRun(ctx, run, attachment.Name, "it is not a picture a model can be shown")
	}
	if run.Storage() == nil {
		return fmt.Errorf("there is no storage to read the picture of the statement from")
	}
	content, err := run.Storage().GetFile(ctx, attachment.ID)
	if err != nil {
		return fmt.Errorf("the picture of the statement cannot be read yet: %w", err)
	}
	prompt, err := render("read_statement_photo.txt", map[string]any{
		"PersonName":       personName(run.Owner),
		"TransactionKinds": strings.Join(finance.TransactionKinds(), ", "),
	})
	if err != nil {
		return err
	}
	thinking, err := self.thinkAbout(ctx, run, "Reading the statement "+attachment.Name, prompt,
		[]llm.ContentPart{{Type: "image", MediaType: attachment.ContentType, Data: content}}, noTools, 1,
		models.AgentJobStatementPhoto, config.AgentWorkScan)
	if err != nil {
		return err
	}
	answer := readModelAnswer[statementPhotoAnswer](thinking.Text, "isStatement")
	if !answer.IsValid {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and could not tell what the answer said: %s", attachment.Name, answer.Problem))
		return nil
	}
	if !answer.Value.IsStatement || len(answer.Value.TransactionRows) == 0 {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s: no transactions of an account, so nothing was imported", attachment.Name))
		return nil
	}
	today := time.Now().In(Location(run.Owner)).Format(time.DateOnly)
	check, err := finance.CheckTransactionRows(answer.Value.transactionRowsInput(), today)
	if err == nil {
		var plan *finance.TransactionRowsPlan
		if plan, err = self.PreviewTransactionRows(ctx, run.Agent, run.Owner, check); err == nil && plan.FinanceAccountID == "" {
			err = fmt.Errorf("%w: they are of an account not imported before; send the picture to your assistant to start one", finance.ErrTransactionRowsRefused)
		}
	}
	var imported *models.FinanceStatementImport
	if err == nil {
		imported, err = self.ImportTransactionRows(ctx, run.Agent, run.Owner, check)
	}
	if errors.Is(err, finance.ErrTransactionRowsRefused) {
		// Reading the picture again would read the same.
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and imported nothing: %s", attachment.Name, err))
		return nil
	}
	if err != nil {
		return err
	}
	if imported.ImportErrorMessage != "" {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s, and imported nothing: %s", attachment.Name, imported.ImportErrorMessage))
		return nil
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %s: %s added to %s", attachment.Name,
		countOf(imported.AddedTransactionCount, "transaction", "transactions"), strings.Join(imported.FinanceAccountNames, ", ")))
	return nil
}
