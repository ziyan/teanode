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

// ImportTransactionsArguments are one account's transactions sent as rows:
// the account as its list shows it, and each row as read. An account
// number with masked digits (****1234) is kept as the digits shown and
// marked partial; letters are refused, since a made-up number keys the
// account wrongly for good.
//
// FinanceAccountID names the existing account of imported statements the
// rows go into, and the number may then be left out. Without it the
// account is the one the number keys, else the one account at the
// institution of the kind and currency whose last digits the number ends
// with; an account at the institution that matches neither is refused,
// naming it, unless IsNewAccount says the rows are of an account not
// imported before.
type ImportTransactionsArguments struct {
	FinanceAccountID       string `json:"financeAccountId" graphapi:"nullable"`
	IsNewAccount           *bool  `json:"isNewAccount" graphapi:"nullable"`
	InstitutionName        string `json:"institutionName"`
	AccountName            string `json:"accountName" graphapi:"nullable"`
	AccountNumber          string `json:"accountNumber" graphapi:"nullable"`
	IsAccountNumberPartial *bool  `json:"isAccountNumberPartial" graphapi:"nullable"`

	// StatementAccountKind is bank, card or other.
	StatementAccountKind string `json:"statementAccountKind"`
	CurrencyCode         string `json:"currencyCode"`
	BankCode             string `json:"bankCode" graphapi:"nullable"`

	TransactionRows []TransactionRow `json:"transactionRows"`

	// The account's balance as the newest list shows it, the day it is as
	// of, and that day's zone (the person's when left out).
	LedgerBalanceAmount   string `json:"ledgerBalanceAmount" graphapi:"nullable"`
	LedgerBalanceOn       string `json:"ledgerBalanceOn" graphapi:"nullable"`
	LedgerBalanceTimeZone string `json:"ledgerBalanceTimeZone" graphapi:"nullable"`

	// MonthlyTotals are the totals a card's list shows per month, each
	// checked against that month's rows.
	MonthlyTotals []MonthlyTotal `json:"monthlyTotals" graphapi:"nullable"`
}

// TransactionRow is one transaction as read: its day, its description as
// shown, its amount signed (money out negative), what kind it is when the
// list says (deposit, withdrawal, purchase, refund, payment, transfer,
// interest, dividend, fee, cash, other), the running balance shown after
// it, and the month heading it was listed under when that is not the month
// it posted in.
type TransactionRow struct {
	PostedOn             string `json:"postedOn"`
	Description          string `json:"description"`
	Amount               string `json:"amount"`
	TransactionKind      string `json:"transactionKind" graphapi:"nullable"`
	RunningBalanceAmount string `json:"runningBalanceAmount" graphapi:"nullable"`
	TotalMonth           string `json:"totalMonth" graphapi:"nullable"`
}

// MonthlyTotal is a month's total as a card's list shows it, "2026-09".
type MonthlyTotal struct {
	TotalMonth  string `json:"totalMonth"`
	TotalAmount string `json:"totalAmount"`
}

// RenameStatementAccountArguments name an account of imported statements
// and the name to give it.
type RenameStatementAccountArguments struct {
	FinanceAccountID string `json:"financeAccountId"`
	AccountName      string `json:"accountName"`
}

// StatementAccountArguments name an account of imported statements.
type StatementAccountArguments struct {
	FinanceAccountID string `json:"financeAccountId"`
}

// StatementAccountDeletedView is what deleting an account of imported
// statements removed.
type StatementAccountDeletedView struct {
	FinanceAccountID        string `json:"financeAccountId"`
	DeletedTransactionCount int    `json:"deletedTransactionCount"`
	DeletedAssetCount       int    `json:"deletedAssetCount"`
}

// TransactionRowsInput is the arguments as the finance package checks
// them.
func (self *ImportTransactionsArguments) TransactionRowsInput() *finance.TransactionRowsInput {
	input := &finance.TransactionRowsInput{
		InstitutionName: self.InstitutionName, AccountName: self.AccountName, AccountNumber: self.AccountNumber,
		IsAccountNumberPartial: self.IsAccountNumberPartial != nil && *self.IsAccountNumberPartial,
		StatementAccountKind:   finance.StatementAccountKind(strings.ToLower(strings.TrimSpace(self.StatementAccountKind))),
		CurrencyCode:           self.CurrencyCode, BankCode: self.BankCode,
		LedgerBalanceAmount: self.LedgerBalanceAmount, LedgerBalanceOn: self.LedgerBalanceOn, LedgerBalanceTimeZone: self.LedgerBalanceTimeZone,
		FinanceAccountID: self.FinanceAccountID, IsNewAccount: self.IsNewAccount != nil && *self.IsNewAccount,
	}
	for _, row := range self.TransactionRows {
		input.TransactionRows = append(input.TransactionRows, finance.TransactionRow{
			PostedOn: row.PostedOn, Description: row.Description, Amount: row.Amount, TransactionKind: row.TransactionKind,
			RunningBalanceAmount: row.RunningBalanceAmount, TotalMonth: row.TotalMonth,
		})
	}
	for _, total := range self.MonthlyTotals {
		input.MonthlyTotals = append(input.MonthlyTotals, finance.MonthlyTotal{TotalMonth: total.TotalMonth, TotalAmount: total.TotalAmount})
	}
	return input
}

// TransactionRowsPreviewView is what importing transaction rows would do:
// the account they would go into, the rows it does not hold yet and those
// it does, the days and the money of the new rows, and what was checked.
type TransactionRowsPreviewView struct {
	// FinanceAccountID is the existing account's, empty when a new one
	// would be made; AccountName is its name with the end of its
	// identifier, the new one's as it would be named.
	FinanceAccountID string `json:"financeAccountId,omitempty" graphapi:"nullable"`
	AccountName      string `json:"accountName"`
	IsNewAccount     bool   `json:"isNewAccount"`

	// AccountMatch is how the account was found: finance_account_id,
	// account_number, account_mask (its last digits), or new_account.
	AccountMatch string `json:"accountMatch"`

	CurrencyCode string `json:"currencyCode"`

	// NewTransactionRows would be added, oldest first;
	// PresentTransactionRows are held already, by day and amount, and
	// would not be.
	NewTransactionRows     []*TransactionRowPreview `json:"newTransactionRows"`
	PresentTransactionRows []*TransactionRowPreview `json:"presentTransactionRows"`

	// The days and the money of the new rows alone.
	FirstPostedOn  string `json:"firstPostedOn,omitempty" graphapi:"nullable"`
	LastPostedOn   string `json:"lastPostedOn,omitempty" graphapi:"nullable"`
	MoneyInAmount  string `json:"moneyInAmount"`
	MoneyOutAmount string `json:"moneyOutAmount"`

	// VerificationSummary says what was checked over every row sent:
	// running balances, monthly totals.
	VerificationSummary string `json:"verificationSummary"`

	LedgerBalanceAmount   string `json:"ledgerBalanceAmount,omitempty" graphapi:"nullable"`
	LedgerBalanceOn       string `json:"ledgerBalanceOn,omitempty" graphapi:"nullable"`
	LedgerBalanceTimeZone string `json:"ledgerBalanceTimeZone,omitempty" graphapi:"nullable"`
}

// TransactionRowPreview is one row of a preview, by its number among the
// rows as sent, normalized as it would be kept. HasNearbyStoredTransaction
// marks a new row with a stored transaction of the same amount a few days
// off that no row matched: perhaps the same one, dated differently by an
// app and an export, for the person to look at.
type TransactionRowPreview struct {
	RowNumber                  int    `json:"rowNumber"`
	PostedOn                   string `json:"postedOn"`
	Description                string `json:"description"`
	Amount                     string `json:"amount"`
	HasNearbyStoredTransaction bool   `json:"hasNearbyStoredTransaction"`
}

// transactionRowsPreviewView is a plan as the API shows it.
func transactionRowsPreviewView(check *finance.TransactionRowsCheck, plan *finance.TransactionRowsPlan) *TransactionRowsPreviewView {
	view := &TransactionRowsPreviewView{
		FinanceAccountID: plan.FinanceAccountID, AccountName: plan.AccountName, IsNewAccount: plan.FinanceAccountID == "",
		AccountMatch: string(plan.AccountMatch), CurrencyCode: check.CurrencyCode,
		NewTransactionRows: []*TransactionRowPreview{}, PresentTransactionRows: []*TransactionRowPreview{},
		FirstPostedOn: plan.FirstPostedOn, LastPostedOn: plan.LastPostedOn, MoneyInAmount: plan.MoneyInAmount, MoneyOutAmount: plan.MoneyOutAmount,
		VerificationSummary: check.VerificationSummary(), LedgerBalanceAmount: check.LedgerBalanceAmount, LedgerBalanceOn: check.LedgerBalanceOn,
		LedgerBalanceTimeZone: check.LedgerBalanceTimeZone,
	}
	rowPreview := func(row *finance.CheckedTransactionRow) *TransactionRowPreview {
		return &TransactionRowPreview{
			RowNumber: row.RowNumber, PostedOn: row.PostedOn, Description: row.Description, Amount: row.Amount,
			HasNearbyStoredTransaction: plan.HasNearbyStoredTransaction[row.RowNumber],
		}
	}
	for _, row := range plan.NewTransactionRows {
		view.NewTransactionRows = append(view.NewTransactionRows, rowPreview(row))
	}
	for _, row := range plan.PresentTransactionRows {
		view.PresentTransactionRows = append(view.PresentTransactionRows, rowPreview(row))
	}
	return view
}

// transactionRowsError is a refusal of transaction rows as the API says
// it.
func transactionRowsError(err error) error {
	if errors.Is(err, finance.ErrTransactionRowsRefused) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return err
}

// checkedTransactionRows is the agent's worker and the rows checked, as
// both the import and its preview begin once the person is authorized.
func (self *graph) checkedTransactionRows(principal *api.Principal, arguments ImportTransactionsArguments) (*agent.Agent, *finance.TransactionRowsCheck, error) {
	if err := self.requireFinanceOffered(); err != nil {
		return nil, nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, nil, agent.ErrUnavailable
	}
	// Checked before anything is written, against the person's own day, so
	// a year misread into the future is refused too.
	check, err := finance.CheckTransactionRows(arguments.TransactionRowsInput(), personToday(principal))
	if err != nil {
		return nil, nil, transactionRowsError(err)
	}
	return worker, check, nil
}

func (self *graph) PreviewImportTransactions(ctx context.Context, arguments ImportTransactionsArguments) (*TransactionRowsPreviewView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, check, err := self.checkedTransactionRows(principal, arguments)
	if err != nil {
		return nil, err
	}
	plan, err := worker.PreviewTransactionRows(ctx, found, principal.User, check)
	if err != nil {
		return nil, transactionRowsError(err)
	}
	return transactionRowsPreviewView(check, plan), nil
}

func (self *graph) ImportTransactions(ctx context.Context, arguments ImportTransactionsArguments) (*models.FinanceStatementImport, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, check, err := self.checkedTransactionRows(principal, arguments)
	if err != nil {
		return nil, err
	}
	result, err := worker.ImportTransactionRows(ctx, found, principal.User, check)
	if err != nil {
		return nil, transactionRowsError(err)
	}
	if len(result.FinanceAccountIDs) == 0 && result.ImportErrorMessage != "" {
		return nil, fmt.Errorf("%w: nothing was imported: %s", api.ErrInvalidArguments, result.ImportErrorMessage)
	}
	return result, nil
}

// statementAccountError is a rename's or a delete's refusal as the API
// says it.
func statementAccountError(err error) error {
	if errors.Is(err, agent.ErrNotStatementAccount) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return financeError(err)
}

func (self *graph) RenameStatementAccount(ctx context.Context, arguments RenameStatementAccountArguments) (*FinanceAccountView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	renamed, err := worker.RenameStatementAccount(ctx, found, arguments.FinanceAccountID, arguments.AccountName)
	if err != nil {
		return nil, statementAccountError(err)
	}
	source, err := self.transaction(ctx).GetAgentSource(found.ID, renamed.SourceID)
	if err != nil {
		return nil, err
	}
	return financeAccountView(renamed, source), nil
}

func (self *graph) DeleteStatementAccount(ctx context.Context, arguments StatementAccountArguments) (*StatementAccountDeletedView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	deleted, err := worker.DeleteStatementAccount(ctx, found, arguments.FinanceAccountID)
	if err != nil {
		return nil, statementAccountError(err)
	}
	return &StatementAccountDeletedView{
		FinanceAccountID: strings.TrimSpace(arguments.FinanceAccountID), DeletedTransactionCount: deleted.DeletedTransactionCount,
		DeletedAssetCount: deleted.DeletedAssetCount,
	}, nil
}
