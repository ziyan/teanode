package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Paystubs and tax forms: a person's pay statements and W-2s, each read out
// of one file handed in as a tax document. These are the operations the
// dashboard, the command line, the finance tool and the reading job all go
// through, so a paystub is checked the same way whoever recorded it, and
// the tax year is put together from what they recorded.

// ErrPaystubUnbalanced refuses recording a paystub whose lines do not add
// up to its printed totals, unless the person says to record it anyway.
var ErrPaystubUnbalanced = errors.New("its lines do not add up to its printed totals")

// RecordPaystub checks a paystub read out of an upload of the agent's and
// records it, in the place of whatever was read out of that upload before.
// One that does not add up is refused unless isUnbalancedAccepted, which
// only the person decides, or the reading job, which has nobody to ask and
// leaves the mark for the person to see. The upload is marked a tax
// document, which forgets any text kept of it.
func (self *Agent) RecordPaystub(ctx context.Context, agentRow *models.Agent, paystub *models.FinancePaystub, isUnbalancedAccepted bool) (*models.FinancePaystub, error) {
	paystubCheckState, checkDifferenceAmount, err := finance.CheckPaystub(paystub)
	if err != nil {
		return nil, err
	}
	if paystubCheckState == models.PaystubCheckStateUnbalanced && !isUnbalancedAccepted {
		return nil, fmt.Errorf("%w: %w: gross less taxes and deductions misses take-home pay or a printed total by %s; read it again, or record it anyway if that is how it prints",
			finance.ErrTaxDocumentRefused, ErrPaystubUnbalanced, finance.FormatReceiptAmount(checkDifferenceAmount, paystub.CurrencyCode))
	}
	paystub.AgentID = agentRow.ID
	paystub.AgentAttachmentID = strings.TrimSpace(paystub.AgentAttachmentID)
	paystub.PaystubCheckState, paystub.CheckDifferenceAmount = paystubCheckState, checkDifferenceAmount
	var stored *models.FinancePaystub
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.MarkAgentAttachmentTaxDocument(agentRow.ID, paystub.AgentAttachmentID); err != nil {
			return err
		}
		stored, err = tx.PutFinancePaystub(paystub)
		return err
	})
	return stored, err
}

// RecordTaxForm checks a tax form read out of an upload of the agent's and
// records it, as RecordPaystub records a paystub.
func (self *Agent) RecordTaxForm(ctx context.Context, agentRow *models.Agent, taxForm *models.FinanceTaxForm) (*models.FinanceTaxForm, error) {
	if err := finance.CheckTaxForm(taxForm); err != nil {
		return nil, err
	}
	taxForm.AgentID = agentRow.ID
	taxForm.AgentAttachmentID = strings.TrimSpace(taxForm.AgentAttachmentID)
	var stored *models.FinanceTaxForm
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.MarkAgentAttachmentTaxDocument(agentRow.ID, taxForm.AgentAttachmentID); err != nil {
			return err
		}
		var err error
		stored, err = tx.PutFinanceTaxForm(taxForm)
		return err
	})
	return stored, err
}

// DeletePaystub deletes a paystub and the file it was read from.
func (self *Agent) DeletePaystub(ctx context.Context, agentRow *models.Agent, paystubId string) error {
	var deleted *db.FinanceTaxDocumentDeleted
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		deleted, err = tx.DeleteFinancePaystub(agentRow.ID, strings.TrimSpace(paystubId))
		return err
	}); err != nil {
		return err
	}
	self.removeTaxDocumentBytes(ctx, deleted)
	return nil
}

// DeleteTaxForm deletes a tax form and the file it was read from.
func (self *Agent) DeleteTaxForm(ctx context.Context, agentRow *models.Agent, taxFormId string) error {
	var deleted *db.FinanceTaxDocumentDeleted
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		deleted, err = tx.DeleteFinanceTaxForm(agentRow.ID, strings.TrimSpace(taxFormId))
		return err
	}); err != nil {
		return err
	}
	self.removeTaxDocumentBytes(ctx, deleted)
	return nil
}

// removeTaxDocumentBytes removes a deleted document's file once its row is
// gone and committed: bytes left behind by a failure here are reclaimed by
// retention, while a row whose bytes went first would be a file nobody can
// open.
func (self *Agent) removeTaxDocumentBytes(ctx context.Context, deleted *db.FinanceTaxDocumentDeleted) {
	if deleted == nil || deleted.DeletedAgentAttachmentID == "" || self.settings.Storage == nil {
		return
	}
	if err := self.settings.Storage.DeleteFile(ctx, deleted.DeletedAgentAttachmentID); err != nil {
		log.Warningf("cannot remove the bytes of tax document file %q: %s", deleted.DeletedAgentAttachmentID, err)
	}
}

// QueueTaxDocumentReading marks an upload of the agent's a tax document,
// forgetting any text kept of it, and queues the job that reads it. The
// job's subject is the upload.
func (self *Agent) QueueTaxDocumentReading(tx db.Transaction, agentId, attachmentId string) (*models.AgentJob, error) {
	if err := tx.MarkAgentAttachmentTaxDocument(agentId, attachmentId); err != nil {
		return nil, err
	}
	return self.Enqueue(tx, models.AgentJobTaxDocument, agentId, "", attachmentId)
}

// taxDocumentPartBytes is the largest file of a message copied as a tax
// document: more than any paystub or W-2, few enough to hold in memory.
const taxDocumentPartBytes = 20 << 20

// CopyMailTaxDocuments copies the PDFs and pictures a stored message
// carries into uploads of the agent's, marked tax documents and so with no
// text kept, for reading each on its own. A part too large, or of another
// kind, is left. It answers the uploads made, none when the message
// carries no such file.
func (self *Agent) CopyMailTaxDocuments(ctx context.Context, agentRow *models.Agent, mailId string) ([]*models.AgentAttachment, error) {
	if self.settings.Storage == nil {
		return nil, fmt.Errorf("there is no storage to read the message from")
	}
	headers, body, err := self.settings.Storage.Get(ctx, mailId)
	if err != nil {
		return nil, fmt.Errorf("the message cannot be read: %w", err)
	}
	type taxDocumentPart struct {
		fileName, contentType string
		content               []byte
	}
	var parts []taxDocumentPart
	_ = mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		part, err := mailparse.DecodePart(header, reader, taxDocumentPartBytes+1)
		if err != nil || len(part.Content) == 0 || len(part.Content) > taxDocumentPartBytes {
			return nil
		}
		contentType := AttachmentContentType(part.ContentType, part.Content)
		if contentType != "application/pdf" && !IsImageAttachment(contentType) {
			return nil
		}
		// A picture inline in the message's HTML (a logo, a signature) is
		// not a document.
		if IsImageAttachment(contentType) && part.Inline && part.ContentID != "" {
			return nil
		}
		fileName := strings.TrimSpace(filepath.Base(strings.ReplaceAll(part.Filename, "\\", "/")))
		if fileName == "" || fileName == "." || strings.HasPrefix(fileName, ".") {
			extension := ".pdf"
			if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 && contentType != "application/pdf" {
				extension = extensions[0]
			}
			fileName = "document" + extension
		}
		parts = append(parts, taxDocumentPart{fileName: fileName, contentType: contentType, content: part.Content})
		return nil
	})
	attachments := []*models.AgentAttachment{}
	// A message read again reuses the uploads its files were copied into,
	// so its documents are read again in place rather than twice.
	var earlier []*models.AgentAttachment
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		earlier, err = tx.ListAgentAttachments(agentRow.ID, "")
		return err
	}); err != nil {
		return nil, err
	}
	for _, part := range parts {
		if existing := self.sameTaxDocument(ctx, earlier, part.fileName, part.content); existing != nil {
			attachments = append(attachments, existing)
			continue
		}
		var created *models.AgentAttachment
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
				AgentID: agentRow.ID, Name: part.fileName, ContentType: part.contentType, Size: int64(len(part.content)), IsTaxDocument: true,
			})
			if err != nil {
				return err
			}
			// The bytes before the row commits, as an upload's are.
			return self.settings.Storage.PutFile(ctx, created.ID, part.content)
		}); err != nil {
			return attachments, err
		}
		attachments = append(attachments, created)
	}
	return attachments, nil
}

// sameTaxDocument is an upload handed in as a tax document with the same
// name and the same bytes as a file about to be copied, or nil.
func (self *Agent) sameTaxDocument(ctx context.Context, attachments []*models.AgentAttachment, fileName string, content []byte) *models.AgentAttachment {
	for _, attachment := range attachments {
		if !attachment.IsTaxDocument || attachment.Name != fileName || attachment.Size != int64(len(content)) {
			continue
		}
		stored, err := self.settings.Storage.GetFile(ctx, attachment.ID)
		if err == nil && bytes.Equal(stored, content) {
			return attachment
		}
	}
	return nil
}

// TaxYear puts a tax year together from every paystub paid in it and every
// tax form for it.
func (self *Agent) TaxYear(ctx context.Context, agentRow *models.Agent, taxYear int) (*finance.TaxYearSummary, error) {
	if taxYear < 1900 || taxYear > 2999 {
		return nil, fmt.Errorf("%w: %d is not a tax year", db.ErrInvalidArguments, taxYear)
	}
	var paystubs []*models.FinancePaystub
	var taxForms []*models.FinanceTaxForm
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		filter := &db.FinancePaystubFilter{From: fmt.Sprintf("%04d-01-01", taxYear), To: fmt.Sprintf("%04d-12-31", taxYear), Limit: db.FinanceTaxDocumentLimitMost}
		for {
			page, err := tx.ListFinancePaystubs(agentRow.ID, filter)
			if err != nil {
				return err
			}
			paystubs = append(paystubs, page.FinancePaystubs...)
			if !page.HasMore {
				break
			}
			filter.Offset += len(page.FinancePaystubs)
		}
		formFilter := &db.FinanceTaxFormFilter{TaxYear: taxYear, Limit: db.FinanceTaxDocumentLimitMost}
		for {
			page, err := tx.ListFinanceTaxForms(agentRow.ID, formFilter)
			if err != nil {
				return err
			}
			taxForms = append(taxForms, page.FinanceTaxForms...)
			if !page.HasMore {
				break
			}
			formFilter.Offset += len(page.FinanceTaxForms)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return finance.SummarizeTaxYear(taxYear, paystubs, taxForms, time.Now()), nil
}
