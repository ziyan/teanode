package apigraph

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The finance imports are the files read into finance in the background,
// newest first: a tax document, a statement read from a picture or a PDF,
// an OFX statement mailed in, a receipt. Each is a job of the agent's,
// with the run that read it, so the person sees what is waiting, what is
// being read and what came of each one, without opening the runs.

// FinanceImportKind is what a file was read as.
type FinanceImportKind string

// The kinds of import.
const (
	FinanceImportTaxDocument FinanceImportKind = "tax_document"
	FinanceImportStatement   FinanceImportKind = "statement"
	FinanceImportReceipt     FinanceImportKind = "receipt"
)

// FinanceImportSource is where a file came from: handed in (shared or
// uploaded), a backed-up photo the reading of new photos picked out, or a
// message.
type FinanceImportSource string

// The sources of an import.
const (
	FinanceImportSourceUpload FinanceImportSource = "upload"
	FinanceImportSourcePhoto  FinanceImportSource = "photo"
	FinanceImportSourceMail   FinanceImportSource = "mail"
)

// financeImportJobKinds are the jobs that read a file into finance, and
// what each reads it as.
var financeImportJobKinds = map[models.AgentJobKind]FinanceImportKind{
	models.AgentJobTaxDocument:     FinanceImportTaxDocument,
	models.AgentJobTaxMail:         FinanceImportTaxDocument,
	models.AgentJobStatementPhoto:  FinanceImportStatement,
	models.AgentJobStatementImport: FinanceImportStatement,
	models.AgentJobReadReceipt:     FinanceImportReceipt,
}

// FinanceImportsArguments bound the listing.
type FinanceImportsArguments struct {
	Limit  *int `json:"limit" graphapi:"nullable"`
	Offset *int `json:"offset" graphapi:"nullable"`
}

// FinanceImport is one file read into finance: what it was read as, where
// it came from, how far its job is, and what came of it.
type FinanceImport struct {
	AgentJobID          string                `json:"agentJobId"`
	FinanceImportKind   FinanceImportKind     `json:"financeImportKind"`
	FinanceImportSource FinanceImportSource   `json:"financeImportSource"`
	AgentJobStatus      models.AgentJobStatus `json:"agentJobStatus"`

	// FileName is the file's name, or the subject of the message it came
	// in; empty when the file or the message is gone.
	FileName string `json:"fileName"`

	// AgentAttachmentID is the file read, to show or open it, with its
	// type; empty for a message, or when the file is gone.
	AgentAttachmentID     string `json:"agentAttachmentId"`
	AttachmentContentType string `json:"attachmentContentType"`

	// MailboxItemID is the message the file came in, to open it; empty for
	// an upload, or when the message is gone from the mailbox.
	MailboxItemID string `json:"mailboxItemId"`

	QueuedAt   time.Time  `json:"queuedAt"`
	FinishedAt *time.Time `json:"finishedAt" graphapi:"nullable"`

	// OutcomeText is what its run says: what it is reading while it runs,
	// what came of it once done, or why the job failed; empty while it
	// waits.
	OutcomeText string `json:"outcomeText"`

	// RunConversationID is the run that read it, for its whole transcript;
	// empty until one ran.
	RunConversationID string `json:"runConversationId"`
}

// FinanceImportPage is one page of imports and how many there are.
type FinanceImportPage struct {
	FinanceImports []*FinanceImport `json:"financeImports"`
	HasMore        bool             `json:"hasMore"`
	TotalCount     int64            `json:"totalCount"`
}

func (self *graph) FinanceImports(ctx context.Context, arguments FinanceImportsArguments) (*FinanceImportPage, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		if errors.Is(err, agent.ErrUnavailable) {
			return &FinanceImportPage{FinanceImports: []*FinanceImport{}}, nil
		}
		return nil, err
	}
	limit, offset := 50, 0
	if arguments.Limit != nil && *arguments.Limit > 0 {
		limit = min(*arguments.Limit, 200)
	}
	if arguments.Offset != nil {
		offset = max(*arguments.Offset, 0)
	}
	kinds := make([]models.AgentJobKind, 0, len(financeImportJobKinds))
	for kind := range financeImportJobKinds {
		kinds = append(kinds, kind)
	}
	filter := &db.AgentJobFilter{AgentID: found.ID, Kinds: kinds}
	tx := self.transaction(ctx)
	jobs, err := tx.ListAgentJobs(filter, &db.Options{Limit: uint64(limit), Offset: uint64(offset)})
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAgentJobs(filter)
	if err != nil {
		return nil, err
	}
	page := &FinanceImportPage{FinanceImports: make([]*FinanceImport, 0, len(jobs)), HasMore: int64(offset+len(jobs)) < total, TotalCount: total}
	for _, job := range jobs {
		financeImport, err := financeImportOf(tx, found.ID, job)
		if err != nil {
			return nil, err
		}
		page.FinanceImports = append(page.FinanceImports, financeImport)
	}
	return page, nil
}

// financeImportOf is a job as an import: its file, and its last run.
func financeImportOf(tx db.Transaction, agentId string, job *models.AgentJob) (*FinanceImport, error) {
	financeImport := &FinanceImport{
		AgentJobID: job.ID, FinanceImportKind: financeImportJobKinds[job.Kind], FinanceImportSource: FinanceImportSourceUpload,
		AgentJobStatus: job.Status, QueuedAt: job.CreatedAt, FinishedAt: job.FinishedAt,
	}
	// A job of a message names the message; the others an upload, which
	// is a backed-up photo's copy when it carries the photo.
	isOfMail := job.MailboxID != "" || job.Kind == models.AgentJobTaxMail || job.Kind == models.AgentJobStatementImport
	if isOfMail {
		financeImport.FinanceImportSource = FinanceImportSourceMail
		mail, err := tx.GetMail(job.SubjectID, nil)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
		if mail != nil {
			financeImport.FileName = mail.Subject
			items, err := tx.ListItemsByMail(mail.ID)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if !item.Deleted {
					financeImport.MailboxItemID = item.ID
					break
				}
			}
		}
	} else {
		attachment, err := tx.GetAgentAttachment(job.SubjectID)
		if err != nil {
			return nil, err
		}
		if attachment != nil && attachment.AgentID == agentId {
			financeImport.FileName = attachment.Name
			financeImport.AgentAttachmentID, financeImport.AttachmentContentType = attachment.ID, attachment.ContentType
			if attachment.PhotoID != "" {
				financeImport.FinanceImportSource = FinanceImportSourcePhoto
			}
		}
	}
	runs, err := tx.ListAgentRuns(agentId, &db.AgentRunFilter{JobID: job.ID}, &db.Options{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		financeImport.RunConversationID = runs[0].ID
		financeImport.OutcomeText = runs[0].Title
	}
	if job.Status == models.AgentJobDead && job.Error != "" {
		financeImport.OutcomeText = job.Error
	}
	return financeImport, nil
}
