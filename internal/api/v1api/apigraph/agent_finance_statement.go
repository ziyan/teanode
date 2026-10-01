package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance/ofx"
	"github.com/ziyan/teanode/internal/models"
)

// Imported statements: the finance source that holds the accounts of OFX
// files a person mails to their statement import address or uploads, for
// the accounts no provider reaches. Part of the finance area (FinanceQuery,
// FinanceMutation). A file reaches ImportStatement the way a file reaches
// a conversation with the agent: uploaded to the agent's attachments, then
// named by id; or as the attachment of a message in the person's mailbox.

// StatementImportView is the person's statement import: the address to
// mail statements to, whether importing is on, and what the last import
// did.
type StatementImportView struct {
	// SourceID is the statement finance source, which its finance accounts
	// name.
	SourceID string `json:"sourceId"`

	// ImportAddress is where to mail OFX files: the person's own mailbox
	// address with "+statements-" and a token. Empty when they have no
	// mailbox with an address.
	ImportAddress string `json:"importAddress,omitempty" graphapi:"nullable"`

	// IsEnabled says statements are imported; switched off, mail to the
	// address is refused and uploads are not imported.
	IsEnabled bool `json:"isEnabled"`

	// MaximumStatementBytes is the largest file imported.
	MaximumStatementBytes int `json:"maximumStatementBytes"`

	LastStatementImport *models.FinanceStatementImport `json:"lastStatementImport,omitempty" graphapi:"nullable"`
}

// ImportStatementArguments name the OFX file to import: an upload to the
// agent's attachments, or a message in the person's mailbox, whose every
// OFX attachment is imported. One of the two.
type ImportStatementArguments struct {
	AgentAttachmentID string `json:"agentAttachmentId" graphapi:"nullable"`
	MailboxItemID     string `json:"mailboxItemId" graphapi:"nullable"`
}

// statementSourceOf is the person's statement source: made, with its
// address, the first time it is asked for on a server that offers finance;
// only read on one that no longer does.
func (self *graph) statementSourceOf(ctx context.Context, worker *agent.Agent, found *models.Agent) (*models.AgentKnowledgeSource, error) {
	if isFinanceOffered(self.config.Current()) {
		return worker.EnsureStatementSource(ctx, found)
	}
	var source *models.AgentKnowledgeSource
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		sources, err := financeSourcesOf(tx, found.ID)
		if err != nil {
			return err
		}
		for _, candidate := range sources {
			if agent.IsStatementSource(candidate) {
				source = candidate
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errFinanceNotOffered
	}
	return source, nil
}

// statementImportView reads the statement source again and shows it.
func (self *graph) statementImportView(ctx context.Context, worker *agent.Agent, principal *api.Principal, found *models.Agent, sourceId string) (*StatementImportView, error) {
	var view *StatementImportView
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		source, err := tx.GetAgentSource(found.ID, sourceId)
		if err != nil {
			return err
		}
		if source == nil {
			return api.ErrNotFound
		}
		address, err := worker.StatementImportAddress(tx, principal.User, source)
		if err != nil {
			return err
		}
		view = &StatementImportView{
			SourceID: source.ID, ImportAddress: address, IsEnabled: source.Enabled,
			MaximumStatementBytes: ofx.MaximumFileBytes, LastStatementImport: agent.LastStatementImport(source),
		}
		return nil
	})
	return view, err
}

func (self *graph) StatementImport(ctx context.Context) (*StatementImportView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	source, err := self.statementSourceOf(ctx, worker, found)
	if err != nil {
		return nil, err
	}
	return self.statementImportView(ctx, worker, principal, found, source.ID)
}

func (self *graph) RegenerateStatementImportAddress(ctx context.Context) (*StatementImportView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	source, err := worker.RegenerateStatementImportToken(ctx, found)
	if err != nil {
		return nil, err
	}
	return self.statementImportView(ctx, worker, principal, found, source.ID)
}

func (self *graph) ImportStatement(ctx context.Context, arguments ImportStatementArguments) (*models.FinanceStatementImport, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	attachmentId := strings.TrimSpace(arguments.AgentAttachmentID)
	itemId := strings.TrimSpace(arguments.MailboxItemID)
	if (attachmentId == "") == (itemId == "") {
		return nil, fmt.Errorf("%w: give the uploaded file (agentAttachmentId) or the message it is attached to (mailboxItemId), one of the two", api.ErrInvalidArguments)
	}
	var files []*agent.StatementFile
	origin := models.StatementImportOriginUpload
	if attachmentId != "" {
		attachment, err := self.transaction(ctx).GetAgentAttachment(attachmentId)
		if err != nil {
			return nil, err
		}
		if attachment == nil || attachment.AgentID != found.ID {
			return nil, api.ErrNotFound
		}
		if attachment.Size > int64(ofx.MaximumFileBytes) {
			return nil, fmt.Errorf("%w: %s is larger than %d MB, more than any statement", api.ErrInvalidArguments, attachment.Name, ofx.MaximumFileBytes/(1024*1024))
		}
		content, err := self.storage.GetFile(ctx, attachment.ID)
		if err != nil {
			return nil, err
		}
		files = append(files, &agent.StatementFile{StatementFileName: attachment.Name, Content: content})
	} else {
		origin = models.StatementImportOriginMessage
		items, _, err := self.requireItems(ctx, models.PermissionMailRead, []string{itemId})
		if err != nil {
			return nil, err
		}
		headers, body, err := self.storage.Get(ctx, items[0].MailID)
		if err != nil {
			return nil, err
		}
		files = agent.StatementFilesOf(headers, body)
		if len(files) == 0 {
			return nil, fmt.Errorf("%w: the message has no OFX file attached (.ofx, .qfx or .qbo)", api.ErrInvalidArguments)
		}
	}
	result, err := worker.ImportStatementFiles(ctx, found, principal.User, files, origin, "")
	if err != nil {
		return nil, err
	}
	// Nothing went in: the reason is the answer, as a refusal the caller
	// shows. What did go in is answered with what did not beside it.
	if len(result.FinanceAccountIDs) == 0 && result.ImportErrorMessage != "" {
		return nil, fmt.Errorf("%w: nothing was imported: %s", api.ErrInvalidArguments, result.ImportErrorMessage)
	}
	return result, nil
}
