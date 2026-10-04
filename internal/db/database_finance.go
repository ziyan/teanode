package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// FinanceOperation is a person's finance accounts and finance
// transactions: what their finance sources' syncs write, and the reads the
// finance tool, the command line and the dashboard answer from. Every call
// takes the agent id and filters on it, so an id from another person finds
// nothing.
type FinanceOperation interface {
	// ListFinanceAccounts is the agent's finance accounts, of one finance
	// source when sourceId is not empty, oldest first.
	ListFinanceAccounts(agentId, sourceId string) ([]*models.FinanceAccount, error)

	// GetFinanceAccount is one finance account of the agent, or nil.
	GetFinanceAccount(agentId, financeAccountId string) (*models.FinanceAccount, error)

	// RenameFinanceAccount gives a finance account the person's own name,
	// kept in its metadata too so the imports that follow keep it (only a
	// statement account's imports read it; a provider's sync names its
	// accounts itself). The asset that values the account takes the new
	// name when it still has the old one. ErrNotFound when the agent has
	// no such finance account.
	RenameFinanceAccount(agentId, financeAccountId, accountName string) (*models.FinanceAccount, error)

	// DeleteFinanceAccount deletes a finance account with its finance
	// transactions and every asset that values it, with their history.
	// A transaction on another account that transfer detection had paired
	// with one of the deleted ones is let go of, uncategorized, so the
	// caller can detect transfers again and pair it with what remains.
	// ErrNotFound when the agent has no such finance account.
	DeleteFinanceAccount(agentId, financeAccountId string) (*FinanceAccountDeleted, error)

	// ApplyFinanceSync writes one sync of a finance source, all or
	// nothing: it upserts the finance accounts and the added finance
	// transactions, deletes the removed ones and the pending ones the
	// result replaced, makes an asset for each finance account seen for
	// the first time, and records each finance account's balance as its
	// asset's valuation for syncedOn, the person's local day ("2006-01-02").
	// A transaction the provider sends again unchanged is not written.
	ApplyFinanceSync(agentId, sourceId string, syncResult *finance.SyncResult, syncedOn string) (*FinanceSyncApplied, error)

	// GetFinanceTransaction is one finance transaction of the agent, or nil.
	GetFinanceTransaction(agentId, financeTransactionId string) (*models.FinanceTransaction, error)

	// GetFinanceTransactions is the finance transactions of the agent among
	// the ids given, in no particular order. An id that is none of the
	// agent's is left out, so the caller compares the counts.
	GetFinanceTransactions(agentId string, financeTransactionIds []string) ([]*models.FinanceTransaction, error)

	// ListFinanceTransactions is a page of the agent's finance
	// transactions, newest first, narrowed by the filter.
	ListFinanceTransactions(agentId string, filter *FinanceTransactionFilter) (*FinanceTransactionPage, error)

	// ListFinanceTrades is a page of the agent's trades, newest first,
	// narrowed by the filter, each with its security.
	ListFinanceTrades(agentId string, filter *FinanceTradeFilter) (*FinanceTradePage, error)

	// ListUncategorizedFinanceTransactions is the agent's finance
	// transactions with no spending category (a transfer has the transfer
	// category) that the person has not decided about, that are not
	// mirrored copies (whose category counts for nothing), and that the
	// categorize model has not already been asked about and failed to
	// place: what
	// the categorize model is asked about. Newest first, since this
	// month's spending is what a budget is judged by; a run that places
	// nothing still marks what it was asked about, so older history gets
	// its turn on the next.
	ListUncategorizedFinanceTransactions(agentId string, limit int) ([]*models.FinanceTransaction, error)

	// MarkCategorizeAttempted records that the categorize model was asked
	// about these finance transactions and placed none of them, so they
	// are not listed for it again until their merchant, description or
	// provider category changes, or a spending category they had is taken
	// away by what gave it. One that has a spending category by now is
	// left alone. It answers how many it marked.
	MarkCategorizeAttempted(agentId string, financeTransactionIds []string) (int, error)

	// SetTransactionCategorization gives a finance transaction a spending
	// category (empty for none), saying what gave it and, for the decision
	// model, its confidence. The transfer category makes it a transfer and
	// any other takes that away. It refuses, answering false, to overwrite
	// what the person chose unless the person is choosing again.
	// ErrNotFound when the agent has no such finance transaction or no
	// such spending category; the categorize model is never let give the
	// transfer category, since what it cannot place is spending until
	// something surer says otherwise.
	//
	// The person never clears a spending category: no spending category
	// means not decided yet, and the person's "fits nothing" is the other
	// category, so an empty one from the person is refused.
	SetTransactionCategorization(agentId, financeTransactionId, spendingCategoryId string, categorizedBy models.CategorizedBy, categorizationConfidence *string) (bool, error)

	// CategorizeTransactionsByPerson gives every finance transaction named
	// the spending category as the person's choice, in one statement, as
	// SetTransactionCategorization does for one; an empty one is refused,
	// as it is there. It writes nothing and answers ErrNotFound when any
	// id is none of the agent's or the spending category is not theirs;
	// otherwise it answers how many it wrote.
	CategorizeTransactionsByPerson(agentId string, financeTransactionIds []string, spendingCategoryId string) (int, error)

	// DetectFinanceTransfers gives the transfer category to the finance
	// transactions posted on or after sinceDate in one finance source's
	// accounts (every source's when sourceId is empty) whose provider
	// category is a transfer (categorized by the provider category
	// mapping), and to pairs of the same absolute amount and currency,
	// opposite signs, on two different finance accounts of the agent,
	// posted within three days of each other, each finance transaction in
	// at most one pair and each pair the closest in days for both of its
	// sides (categorized by transfer detection). A finance transaction
	// already a transfer, or whose spending category the person chose, is
	// neither marked nor paired. It answers how many it marked.
	DetectFinanceTransfers(agentId, sourceId, sinceDate string) (int, error)

	// DetectMirroredFinanceTransactions decides again which finance
	// transactions of one finance source (every source of the agent's when
	// sourceId is empty) are mirrored copies: on two or more different
	// finance accounts of one Plaid finance source, every one of them an
	// investment account, with the same day, amount, currency and
	// description (trimmed, in any case), pending or posted. One of each
	// set is counted, a posted one before a pending one, then the one
	// stored first and then the one on the oldest finance account, and the
	// rest become duplicates of it.
	// Two on one account are never copies of each other. Other providers'
	// sources and other kinds of account are not looked at, and neither is
	// a finance transaction the person counted. A copy whose set no longer
	// holds (a member deleted or changed) counts again, and a set whose
	// counted copy went counts another. It locks the finance source first.
	// It answers how many changed.
	DetectMirroredFinanceTransactions(agentId, sourceId string) (int, error)

	// SetFinanceTransactionCountedByPerson records the person saying a
	// mirrored copy is real and counts, after which detection leaves it
	// alone; with isCountedByPerson false it forgets that, and detection
	// decides again at once. Counting a finance transaction that is not a
	// duplicate is refused, since it counts already. ErrNotFound when the
	// agent has no such finance transaction.
	SetFinanceTransactionCountedByPerson(agentId, financeTransactionId string, isCountedByPerson bool) error

	// FinanceSpendingSummary is money out and money in per group per
	// currency over a range of posted days, transfers (the transfer
	// category) and mirrored copies left out, biggest money out first.
	FinanceSpendingSummary(agentId string, filter *FinanceSpendingSummaryFilter) ([]*models.FinanceSpendingSummaryRow, error)
}

// FinanceAccountDeleted is what one DeleteFinanceAccount deleted.
type FinanceAccountDeleted struct {
	// DeletedTransactionCount is the finance transactions deleted with the
	// account, and DeletedAssetCount the assets.
	DeletedTransactionCount int
	DeletedAssetCount       int

	// ReleasedTransactionIDs are the transactions on other accounts that
	// were paired with a deleted one as a transfer and are uncategorized
	// now; EarliestPostedOn is the first day a deleted transaction posted,
	// where detecting transfers again starts. Empty when none.
	ReleasedTransactionIDs []string
	EarliestPostedOn       string
}

// FinanceSyncApplied is what one ApplyFinanceSync wrote.
type FinanceSyncApplied struct {
	// InsertedFinanceAccountIDs are the finance accounts seen for the
	// first time, and CreatedAssetIDs the assets made for them.
	InsertedFinanceAccountIDs []string
	CreatedAssetIDs           []string

	// WrittenTransactionCount is the finance transactions inserted or
	// changed; one sent again unchanged is not counted.
	// InsertedTransactionCount is those of them that were new.
	WrittenTransactionCount  int
	InsertedTransactionCount int

	// RemovedTransactionCount is the finance transactions the provider
	// removed, and ReplacedPendingTransactionCount the pending ones it
	// stopped reporting inside the window it replaced.
	RemovedTransactionCount         int
	ReplacedPendingTransactionCount int

	// SkippedTransactionCount is the transactions for a finance account
	// neither the result nor the store knows, which were not written.
	SkippedTransactionCount int

	// RecordedValuationCount is the finance accounts and holdings whose
	// value was recorded as their asset's valuation for the day.
	RecordedValuationCount int

	// WrittenTradeCount is the trades inserted or changed; one sent again
	// unchanged is not counted. SkippedTradeCount is those for a finance
	// account neither the result nor the store knows.
	WrittenTradeCount int
	SkippedTradeCount int

	// FinanceTransactionIDsToCategorize are the inserted or changed finance
	// transactions left without a spending category that are not
	// transfers: new ones, and those whose merchant, description or
	// provider category changed under a spending category the person did
	// not choose.
	FinanceTransactionIDsToCategorize []string
}

// FinanceTransactionFilter narrows a listing of finance transactions.
// Every field is optional.
type FinanceTransactionFilter struct {
	// From and To bound the posted day, both included, "2006-01-02".
	From string
	To   string

	FinanceAccountID string

	// Text is matched case-insensitively within the description or the
	// merchant.
	Text string

	// MinimumAmount and MaximumAmount bound the signed amount, both ends
	// included: money out is negative.
	MinimumAmount string
	MaximumAmount string

	// ProviderCategory matches the provider's primary or detailed
	// category exactly.
	ProviderCategory string

	SpendingCategoryID string

	// IsUncategorized keeps only finance transactions with no spending
	// category; a transfer has the transfer category, and a mirrored copy
	// is left out, since its category counts for nothing.
	IsUncategorized bool

	// IsTransferExcluded leaves out the transfers, the finance
	// transactions in the transfer category.
	IsTransferExcluded bool

	// IsDuplicateExcluded leaves out the mirrored copies, the finance
	// transactions that are duplicates of another.
	IsDuplicateExcluded bool

	// DuplicateOfTransactionID keeps only the duplicates of this finance
	// transaction.
	DuplicateOfTransactionID string

	// FinanceTransactionIDs keeps only these finance transactions, so one
	// can be read by its id with the same fields as a page.
	FinanceTransactionIDs []string

	// Limit is at most FinanceTransactionLimitMost; zero is
	// FinanceTransactionLimitDefault.
	Limit int

	// After is the NextCursor of the page before. Offset is how many to
	// pass over first, for a page by its number; with After it counts
	// from the cursor.
	After  string
	Offset int

	// ShouldCountTotal fills the page's TotalCount, and with
	// IsDuplicateExcluded its LeftOutDuplicateCount: one more statement,
	// so the reads that walk every page by the cursor leave it off.
	ShouldCountTotal bool

	// ShouldReadIDsOnly reads each finance transaction's id and posted
	// day alone, which is all a read that collects ids needs; every
	// other field of the rows it gives is empty.
	ShouldReadIDsOnly bool
}

// How many finance transactions one page holds.
const (
	FinanceTransactionLimitDefault = 50
	FinanceTransactionLimitMost    = 200
)

// FinanceTransactionPage is one page of finance transactions, and the
// cursor for the next, empty on the last. TotalCount is how many match
// the filter on every page, the cursor and the offset aside, when the
// filter asked for it; LeftOutDuplicateCount is then how many mirrored
// copies would match too but IsDuplicateExcluded left out.
type FinanceTransactionPage struct {
	FinanceTransactions   []*models.FinanceTransaction
	NextCursor            string
	TotalCount            int
	LeftOutDuplicateCount int
}

// FinanceSpendingSummaryFilter is what a spending summary covers.
type FinanceSpendingSummaryFilter struct {
	// From and To bound the posted day, both included, "2006-01-02";
	// either may be empty.
	From string
	To   string

	GroupBy models.FinanceSpendingSummaryGroupBy

	// FinanceAccountID limits it to one finance account; empty is all.
	FinanceAccountID string
}

// transferPairingDays is how far apart the two sides of a transfer may
// post: an ACH transfer or a card payment commonly lands a day or two
// after it leaves, and a weekend in between adds two more.
const transferPairingDays = 3

// transferPairingRounds bounds how many times one detection pairs again
// what the rounds before left over. Each round takes only the pairs that
// are both sides' closest, so a run of equal amounts close together needs
// a few; more than this many is not worth guessing at.
const transferPairingRounds = 5

// pendingPostingDays is how far apart a pending transaction's day and the
// day it posted under a new id may be for the person's decisions about it
// to follow it: a card authorization commonly posts a day or two later,
// and a weekend adds two more.
const pendingPostingDays = 5

type agentFinanceAccountModel struct {
	ID                string     `gorm:"column:id;primaryKey"`
	AgentID           string     `gorm:"column:agent_id"`
	SourceID          string     `gorm:"column:source_id"`
	ProviderAccountID string     `gorm:"column:provider_account_id"`
	AccountName       string     `gorm:"column:account_name"`
	AccountMask       string     `gorm:"column:account_mask"`
	AccountKind       string     `gorm:"column:account_kind"`
	CurrencyCode      string     `gorm:"column:currency_code"`
	CurrentBalance    *string    `gorm:"column:current_balance"`
	AvailableBalance  *string    `gorm:"column:available_balance"`
	BalanceAt         *time.Time `gorm:"column:balance_at"`
	CreditLimitAmount *string    `gorm:"column:credit_limit_amount"`
	ProviderMetadata  []byte     `gorm:"column:provider_metadata;type:jsonb"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	ModifiedAt        time.Time  `gorm:"column:modified_at"`
}

func (agentFinanceAccountModel) TableName() string { return "agent_finance_account" }

func (self *agentFinanceAccountModel) toModel() *models.FinanceAccount {
	return &models.FinanceAccount{
		ID: self.ID, AgentID: self.AgentID, SourceID: self.SourceID, ProviderAccountID: self.ProviderAccountID,
		AccountName: self.AccountName, AccountMask: self.AccountMask, AccountKind: models.FinanceAccountKind(self.AccountKind),
		CurrencyCode: self.CurrencyCode, CurrentBalance: optionalString(self.CurrentBalance),
		AvailableBalance: optionalString(self.AvailableBalance), BalanceAt: localTime(self.BalanceAt),
		CreditLimitAmount: optionalString(self.CreditLimitAmount), ProviderMetadata: rawJSON(self.ProviderMetadata),
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

type agentFinanceTransactionModel struct {
	ID                           string     `gorm:"column:id;primaryKey"`
	AgentID                      string     `gorm:"column:agent_id"`
	FinanceAccountID             string     `gorm:"column:finance_account_id"`
	ProviderTransactionID        string     `gorm:"column:provider_transaction_id"`
	PostedOn                     time.Time  `gorm:"column:posted_on"`
	TransactedAt                 *time.Time `gorm:"column:transacted_at"`
	Amount                       string     `gorm:"column:amount"`
	CurrencyCode                 string     `gorm:"column:currency_code"`
	Description                  string     `gorm:"column:description"`
	MerchantName                 string     `gorm:"column:merchant_name"`
	ProviderCategoryPrimary      string     `gorm:"column:provider_category_primary"`
	ProviderCategoryDetailed     string     `gorm:"column:provider_category_detailed"`
	IsPending                    bool       `gorm:"column:is_pending"`
	PendingProviderTransactionID string     `gorm:"column:pending_provider_transaction_id"`
	ProviderMetadata             []byte     `gorm:"column:provider_metadata;type:jsonb"`
	SpendingCategoryID           *string    `gorm:"column:spending_category_id"`
	CategorizedBy                string     `gorm:"column:categorized_by"`
	CategorizationConfidence     *string    `gorm:"column:categorization_confidence"`
	CategorizeAttemptedAt        *time.Time `gorm:"column:categorize_attempted_at"`
	DuplicateOfTransactionID     *string    `gorm:"column:duplicate_of_transaction_id"`
	DuplicateDecidedBy           string     `gorm:"column:duplicate_decided_by"`
	CreatedAt                    time.Time  `gorm:"column:created_at"`
	ModifiedAt                   time.Time  `gorm:"column:modified_at"`
}

func (agentFinanceTransactionModel) TableName() string { return "agent_finance_transaction" }

func (self *agentFinanceTransactionModel) toModel() *models.FinanceTransaction {
	return &models.FinanceTransaction{
		ID: self.ID, AgentID: self.AgentID, FinanceAccountID: self.FinanceAccountID,
		ProviderTransactionID: self.ProviderTransactionID, PostedOn: formatDay(self.PostedOn),
		TransactedAt: localTime(self.TransactedAt), Amount: self.Amount, CurrencyCode: self.CurrencyCode,
		Description: self.Description, MerchantName: self.MerchantName,
		ProviderCategoryPrimary: self.ProviderCategoryPrimary, ProviderCategoryDetailed: self.ProviderCategoryDetailed,
		IsPending: self.IsPending, PendingProviderTransactionID: self.PendingProviderTransactionID,
		ProviderMetadata:   rawJSON(self.ProviderMetadata),
		SpendingCategoryID: optionalString(self.SpendingCategoryID), CategorizedBy: models.CategorizedBy(self.CategorizedBy),
		CategorizationConfidence: optionalString(self.CategorizationConfidence),
		DuplicateOfTransactionID: optionalString(self.DuplicateOfTransactionID), DuplicateDecidedBy: models.DuplicateDecidedBy(self.DuplicateDecidedBy),
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

// --- values shared by the finance, net worth and budget tables ---------

// parseDay checks a day written "2006-01-02" and returns it as written.
func parseDay(day string) (string, error) {
	parsed, err := time.Parse(time.DateOnly, strings.TrimSpace(day))
	if err != nil {
		return "", fmt.Errorf("%w: %q is not a day like 2006-01-02", ErrInvalidArguments, day)
	}
	return parsed.Format(time.DateOnly), nil
}

// parseOptionalDay is parseDay, with empty allowed and kept empty.
func parseOptionalDay(day string) (string, error) {
	if strings.TrimSpace(day) == "" {
		return "", nil
	}
	return parseDay(day)
}

// parseMonth reads a month written "2006-01", or any day in it written
// "2006-01-02", and returns its first day.
func parseMonth(month string) (time.Time, error) {
	month = strings.TrimSpace(month)
	if parsed, err := time.Parse("2006-01", month); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse(time.DateOnly, month)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not a month like 2006-01", ErrInvalidArguments, month)
	}
	return time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

// formatDay writes a date column as read. The driver reads a date as
// midnight UTC, so it is formatted without moving it to the local zone,
// which would put it on the day before west of Greenwich.
func formatDay(day time.Time) string {
	return day.Format(time.DateOnly)
}

// formatOptionalDay is formatDay for a nullable column, empty for NULL.
func formatOptionalDay(day *time.Time) string {
	if day == nil {
		return ""
	}
	return formatDay(*day)
}

// optionalDay is a day for a nullable date column: NULL for empty.
func optionalDay(day string) *string {
	if day == "" {
		return nil
	}
	return &day
}

// canonicalAmount checks a decimal and writes it with four places, the
// way the numeric(19,4) columns keep it.
func canonicalAmount(field, amount string) (string, error) {
	canonical, err := finance.CanonicalAmount(amount)
	if err != nil {
		return "", fmt.Errorf("%w: %s %q is not a decimal amount", ErrInvalidArguments, field, amount)
	}
	return canonical, nil
}

// canonicalOptionalAmount is canonicalAmount for a nullable column: nil
// for empty.
func canonicalOptionalAmount(field, amount string) (*string, error) {
	if strings.TrimSpace(amount) == "" {
		return nil, nil
	}
	canonical, err := canonicalAmount(field, amount)
	if err != nil {
		return nil, err
	}
	return &canonical, nil
}

// optionalString reads a nullable column, empty for NULL.
func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// optionalID is an id for a nullable reference: NULL for empty.
func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// rawJSON is a jsonb column as it was stored, nil when empty.
func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(append([]byte(nil), value...))
}

// providerMetadataJSON is the provider's object as the jsonb column keeps
// it: an empty object when there is none, and refused when it is not JSON
// at all, since a provider that sends that has changed its format.
func providerMetadataJSON(providerMetadata json.RawMessage) (string, error) {
	if len(providerMetadata) == 0 {
		return "{}", nil
	}
	if !json.Valid(providerMetadata) {
		return "", fmt.Errorf("%w: the provider metadata is not JSON", ErrInvalidArguments)
	}
	return string(providerMetadata), nil
}

// contextOrBackground is the transaction's context, for a nested
// transaction opened inside it.
func (self *transaction) contextOrBackground() context.Context {
	if self.ctx == nil {
		return context.Background()
	}
	return self.ctx
}

// --- finance accounts --------------------------------------------------

func (self *transaction) ListFinanceAccounts(agentId, sourceId string) ([]*models.FinanceAccount, error) {
	query := self.tx.Where(`"agent_id" = ?`, agentId)
	if sourceId != "" {
		query = query.Where(`"source_id" = ?`, sourceId)
	}
	var found []agentFinanceAccountModel
	if err := query.Order(`"created_at" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	accounts := make([]*models.FinanceAccount, 0, len(found))
	for index := range found {
		accounts = append(accounts, found[index].toModel())
	}
	return accounts, nil
}

func (self *transaction) GetFinanceAccount(agentId, financeAccountId string) (*models.FinanceAccount, error) {
	var found []agentFinanceAccountModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, financeAccountId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// maximumFinanceAccountNameLength is the longest name a person may give a
// finance account, in characters: longer than any account's name, short
// enough to fit a table's cell and a confirmation card.
const maximumFinanceAccountNameLength = 200

// financeAccountAudit is what the audit log keeps of a finance account:
// its name and how it is told apart, never its provider metadata.
func financeAccountAudit(account *models.FinanceAccount) map[string]any {
	return map[string]any{
		"accountName": account.AccountName, "accountMask": account.AccountMask, "accountKind": account.AccountKind,
		"currencyCode": account.CurrencyCode, "sourceId": account.SourceID,
	}
}

func (self *transaction) RenameFinanceAccount(agentId, financeAccountId, accountName string) (*models.FinanceAccount, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return nil, fmt.Errorf("%w: give the account a name", ErrInvalidArguments)
	}
	if len([]rune(accountName)) > maximumFinanceAccountNameLength {
		return nil, fmt.Errorf("%w: an account's name is at most %d characters", ErrInvalidArguments, maximumFinanceAccountNameLength)
	}
	before, err := self.GetFinanceAccount(agentId, financeAccountId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.AccountName = accountName
	encodedName, err := json.Marshal(accountName)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := self.applyMutation(models.AuditResourceFinanceAccount, financeAccountId, models.AuditActionUpdate, financeAccountAudit(before), financeAccountAudit(&after), func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE "agent_finance_account" SET "account_name" = ?,
				"provider_metadata" = jsonb_set(CASE WHEN jsonb_typeof("provider_metadata") = 'object' THEN "provider_metadata" ELSE '{}'::jsonb END,
					'{`+finance.StatementPersonAccountNameField+`}', CAST(? AS jsonb)),
				"modified_at" = ?
			WHERE "agent_id" = ? AND "id" = ?`, accountName, string(encodedName), now, agentId, financeAccountId).Error; err != nil {
			return err
		}
		// The account's own asset was named after it when it was made; one
		// the person has renamed since keeps their name.
		return tx.Exec(`UPDATE "agent_asset" SET "asset_name" = ?, "modified_at" = ?
			WHERE "agent_id" = ? AND "finance_account_id" = ? AND "finance_security_id" IS NULL AND "asset_name" = ?`,
			accountName, now, agentId, financeAccountId, strings.TrimSpace(before.AccountName)).Error
	}); err != nil {
		return nil, err
	}
	return self.GetFinanceAccount(agentId, financeAccountId)
}

func (self *transaction) DeleteFinanceAccount(agentId, financeAccountId string) (*FinanceAccountDeleted, error) {
	before, err := self.GetFinanceAccount(agentId, financeAccountId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	deleted := &FinanceAccountDeleted{ReleasedTransactionIDs: []string{}}
	var counted struct {
		TransactionCount int        `gorm:"column:transaction_count"`
		EarliestPostedOn *time.Time `gorm:"column:earliest_posted_on"`
	}
	if err := self.tx.Raw(`SELECT COUNT(*) AS "transaction_count", MIN("posted_on") AS "earliest_posted_on" FROM "agent_finance_transaction"
		WHERE "agent_id" = ? AND "finance_account_id" = ?`, agentId, financeAccountId).Scan(&counted).Error; err != nil {
		return nil, err
	}
	deleted.DeletedTransactionCount = counted.TransactionCount
	if counted.EarliestPostedOn != nil {
		deleted.EarliestPostedOn = formatDay(*counted.EarliestPostedOn)
	}

	// Pairing keeps no link between the two sides of a transfer, so the
	// other side is found the way pairing found it (releasedTransferSides).
	// Left marked, it could never pair again (pairing takes only what is
	// not a transfer yet), and the same transfer imported again under the
	// right account would count as spending or income.
	releasedIds, err := self.releasedTransferSides(agentId, financeAccountId)
	if err != nil {
		return nil, err
	}
	if len(releasedIds) > 0 {
		if err := self.tx.Raw(`UPDATE "agent_finance_transaction" SET "spending_category_id" = NULL, "categorized_by" = '',
				"categorization_confidence" = NULL, "categorize_attempted_at" = NULL, "modified_at" = ?
			WHERE "agent_id" = ? AND "id" = ANY(?::text[]) AND "categorized_by" = 'transfer_detection'
			RETURNING "id"`, time.Now(), agentId, pq.Array(releasedIds)).
			Scan(&deleted.ReleasedTransactionIDs).Error; err != nil {
			return nil, err
		}
	}

	// Every asset that values the account goes with it, history and all.
	// Kept, it would be detached and still counted in net worth from its
	// last value on, and an account imported under a wrong identifier is
	// what this is for: its history beside the right account's would count
	// the same money twice.
	var assetIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "agent_asset" WHERE "agent_id" = ? AND "finance_account_id" = ? ORDER BY "id"`,
		agentId, financeAccountId).Scan(&assetIds).Error; err != nil {
		return nil, err
	}
	for _, assetId := range assetIds {
		if err := self.DeleteAsset(agentId, assetId); err != nil {
			return nil, err
		}
	}
	deleted.DeletedAssetCount = len(assetIds)
	if err := self.applyMutation(models.AuditResourceFinanceAccount, financeAccountId, models.AuditActionDelete, financeAccountAudit(before), nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, financeAccountId).Delete(&agentFinanceAccountModel{}).Error
	}); err != nil {
		return nil, err
	}
	return deleted, nil
}

// transferSide is a transaction transfer detection marked, as the pairs
// are worked out again when an account is deleted.
type transferSide struct {
	ID               string    `gorm:"column:id"`
	FinanceAccountID string    `gorm:"column:finance_account_id"`
	CurrencyCode     string    `gorm:"column:currency_code"`
	Amount           string    `gorm:"column:amount"`
	PostedOn         time.Time `gorm:"column:posted_on"`
}

// releasedTransferSides are the transactions on other accounts that were
// paired with one of the deleted account's as a transfer.
//
// A transaction marked by transfer detection with the opposite amount
// within the pairing days of a deleted one is only a candidate: another
// transfer of the same amount a day later, to an account that stays, is
// one too. Checking that pays 500 to one card on Monday and 500 to
// another on Tuesday has two candidates when the first card goes, and
// letting go of Tuesday's would leave it uncategorized beside the second
// card's side, which stays marked and so never pairs with it again. So
// the pairs are worked out again, one to one, the way detection makes
// them: every marked transaction near enough to matter, closest pairs
// first, and a candidate is let go of only when what it pairs with is a
// deleted transaction, or nothing. Three times the pairing days reach
// from a deleted transaction to a candidate, to the transaction it may
// pair with instead, to another that transaction may pair with.
func (self *transaction) releasedTransferSides(agentId, financeAccountId string) ([]string, error) {
	var sides []transferSide
	if err := self.tx.Raw(`SELECT "side"."id", "side"."finance_account_id", "side"."currency_code", "side"."amount", "side"."posted_on"
		FROM "agent_finance_transaction" AS "side"
		WHERE "side"."agent_id" = ? AND "side"."categorized_by" = 'transfer_detection'
		  AND EXISTS (SELECT 1 FROM "agent_finance_transaction" AS "gone"
			WHERE "gone"."agent_id" = "side"."agent_id" AND "gone"."finance_account_id" = ? AND "gone"."categorized_by" = 'transfer_detection'
			  AND "gone"."currency_code" = "side"."currency_code" AND abs("gone"."amount") = abs("side"."amount")
			  AND "gone"."posted_on" BETWEEN "side"."posted_on" - CAST(? AS integer) AND "side"."posted_on" + CAST(? AS integer))
		ORDER BY "side"."posted_on", "side"."id"`, agentId, financeAccountId, 3*transferPairingDays, 3*transferPairingDays).
		Scan(&sides).Error; err != nil {
		return nil, err
	}
	return releasedTransferSidesOf(sides, financeAccountId), nil
}

// releasedTransferSidesOf works out the transfer pairs among marked
// transactions again, closest first and one to one, and answers the
// transactions off the deleted account that had a deleted one within the
// pairing days and now pair with a deleted one or with nothing.
func releasedTransferSidesOf(sides []transferSide, deletedFinanceAccountId string) []string {
	amountValues := make([]*big.Rat, len(sides))
	for index, side := range sides {
		amountValue, isParsed := new(big.Rat).SetString(side.Amount)
		if !isParsed {
			amountValue = nil
		}
		amountValues[index] = amountValue
	}
	type transferPairing struct {
		moneyOutIndex, moneyInIndex, dayCount int
	}
	var pairings []transferPairing
	isCandidate := make([]bool, len(sides))
	for moneyOutIndex, moneyOut := range sides {
		if amountValues[moneyOutIndex] == nil || amountValues[moneyOutIndex].Sign() >= 0 {
			continue
		}
		for moneyInIndex, moneyIn := range sides {
			if amountValues[moneyInIndex] == nil || moneyIn.FinanceAccountID == moneyOut.FinanceAccountID || moneyIn.CurrencyCode != moneyOut.CurrencyCode ||
				new(big.Rat).Add(amountValues[moneyOutIndex], amountValues[moneyInIndex]).Sign() != 0 {
				continue
			}
			dayCount := int(math.Round(moneyIn.PostedOn.Sub(moneyOut.PostedOn).Hours() / 24))
			if dayCount < 0 {
				dayCount = -dayCount
			}
			if dayCount > transferPairingDays {
				continue
			}
			pairings = append(pairings, transferPairing{moneyOutIndex: moneyOutIndex, moneyInIndex: moneyInIndex, dayCount: dayCount})
			isMoneyOutGone, isMoneyInGone := moneyOut.FinanceAccountID == deletedFinanceAccountId, moneyIn.FinanceAccountID == deletedFinanceAccountId
			if isMoneyOutGone {
				isCandidate[moneyInIndex] = true
			}
			if isMoneyInGone {
				isCandidate[moneyOutIndex] = true
			}
		}
	}
	sort.SliceStable(pairings, func(left, right int) bool {
		if pairings[left].dayCount != pairings[right].dayCount {
			return pairings[left].dayCount < pairings[right].dayCount
		}
		if sides[pairings[left].moneyOutIndex].ID != sides[pairings[right].moneyOutIndex].ID {
			return sides[pairings[left].moneyOutIndex].ID < sides[pairings[right].moneyOutIndex].ID
		}
		return sides[pairings[left].moneyInIndex].ID < sides[pairings[right].moneyInIndex].ID
	})
	pairedWith := make([]int, len(sides))
	for index := range pairedWith {
		pairedWith[index] = -1
	}
	for _, pairing := range pairings {
		if pairedWith[pairing.moneyOutIndex] >= 0 || pairedWith[pairing.moneyInIndex] >= 0 {
			continue
		}
		pairedWith[pairing.moneyOutIndex], pairedWith[pairing.moneyInIndex] = pairing.moneyInIndex, pairing.moneyOutIndex
	}
	releasedIds := []string{}
	for index, side := range sides {
		if !isCandidate[index] {
			continue
		}
		if partner := pairedWith[index]; partner < 0 || sides[partner].FinanceAccountID == deletedFinanceAccountId {
			releasedIds = append(releasedIds, side.ID)
		}
	}
	return releasedIds
}

// --- the sync ----------------------------------------------------------

func (self *transaction) ApplyFinanceSync(agentId, sourceId string, syncResult *finance.SyncResult, syncedOn string) (*FinanceSyncApplied, error) {
	if agentId == "" || sourceId == "" || syncResult == nil {
		return nil, fmt.Errorf("%w: a finance sync needs an agent, a finance source and a result", ErrInvalidArguments)
	}
	syncedOn, err := parseDay(syncedOn)
	if err != nil {
		return nil, err
	}
	var applied *FinanceSyncApplied
	// Under a savepoint, so a caller that records the failure on the
	// source in the same transaction does not also keep half a sync.
	err = self.TransactionContext(self.contextOrBackground(), func(nested Transaction) error {
		var err error
		applied, err = nested.(*transaction).applyFinanceSync(agentId, sourceId, syncResult, syncedOn)
		return err
	})
	if err != nil {
		return nil, err
	}
	return applied, nil
}

func (self *transaction) applyFinanceSync(agentId, sourceId string, syncResult *finance.SyncResult, syncedOn string) (*FinanceSyncApplied, error) {
	// The provider is the source's type; securities are kept per provider.
	var foundSources []struct {
		ProviderKind string `gorm:"column:provider_kind"`
	}
	if err := self.tx.Raw(`SELECT COALESCE("specification"->>'type', '') AS "provider_kind" FROM "agent_source" WHERE "id" = ? AND "agent_id" = ?`,
		sourceId, agentId).Scan(&foundSources).Error; err != nil {
		return nil, err
	}
	if len(foundSources) == 0 {
		return nil, ErrNotFound
	}
	providerKind := foundSources[0].ProviderKind
	// Statements arrive in any order, and one imported after a newer one
	// must not put the account's balance back to an older day; its
	// valuation is still recorded, on its own day.
	isNewerBalanceKept := providerKind == string(finance.ProviderKindStatement)

	applied := &FinanceSyncApplied{
		InsertedFinanceAccountIDs: []string{}, CreatedAssetIDs: []string{}, FinanceTransactionIDsToCategorize: []string{},
	}
	now := time.Now()

	reportedAccountIds := make([]string, 0, len(syncResult.Accounts))
	reportedBalances := map[string]finance.Account{}
	for _, account := range syncResult.Accounts {
		financeAccountId, isInserted, err := self.upsertFinanceAccount(agentId, sourceId, account, isNewerBalanceKept, now)
		if err != nil {
			return nil, err
		}
		reportedAccountIds = append(reportedAccountIds, financeAccountId)
		reportedBalances[financeAccountId] = account
		if !isInserted {
			continue
		}
		applied.InsertedFinanceAccountIDs = append(applied.InsertedFinanceAccountIDs, financeAccountId)
		assetId, err := self.createFinanceSyncAsset(agentId, financeAccountId, account, now)
		if err != nil {
			return nil, err
		}
		applied.CreatedAssetIDs = append(applied.CreatedAssetIDs, assetId)
	}

	// Every finance account of the source, not only those in this result:
	// a transaction may name an account the provider left out this time.
	var knownAccounts []agentFinanceAccountModel
	if err := self.tx.Where(`"agent_id" = ? AND "source_id" = ?`, agentId, sourceId).Find(&knownAccounts).Error; err != nil {
		return nil, err
	}
	financeAccountIdByProviderAccountId := make(map[string]string, len(knownAccounts))
	for _, known := range knownAccounts {
		financeAccountIdByProviderAccountId[known.ProviderAccountID] = known.ID
	}

	securityIdByProviderSecurityId, err := self.upsertFinanceSecurities(agentId, providerKind, syncResult.Securities, now)
	if err != nil {
		return nil, err
	}
	namedSecurityIds := make([]string, 0, len(syncResult.Holdings)+len(syncResult.Trades))
	for _, holding := range syncResult.Holdings {
		namedSecurityIds = append(namedSecurityIds, holding.ProviderSecurityID)
	}
	for _, trade := range syncResult.Trades {
		namedSecurityIds = append(namedSecurityIds, trade.ProviderSecurityID)
	}
	if err := self.knownFinanceSecurityIds(agentId, providerKind, namedSecurityIds, securityIdByProviderSecurityId); err != nil {
		return nil, err
	}
	for _, trade := range syncResult.Trades {
		financeAccountId, isKnown := financeAccountIdByProviderAccountId[trade.ProviderAccountID]
		if !isKnown {
			applied.SkippedTradeCount++
			continue
		}
		var securityId *string
		if knownSecurityId, isSecurityKnown := securityIdByProviderSecurityId[trade.ProviderSecurityID]; isSecurityKnown {
			securityId = &knownSecurityId
		}
		isWritten, err := self.upsertFinanceTrade(agentId, financeAccountId, securityId, trade, now)
		if err != nil {
			return nil, err
		}
		if isWritten {
			applied.WrittenTradeCount++
		}
	}

	addedProviderTransactionIdsByFinanceAccountId := map[string][]string{}
	insertedPostedIdsByFinanceAccountId := map[string][]string{}
	writtenIds := []string{}
	for _, added := range syncResult.Added {
		financeAccountId, isKnown := financeAccountIdByProviderAccountId[added.ProviderAccountID]
		if !isKnown {
			applied.SkippedTransactionCount++
			continue
		}
		addedProviderTransactionIdsByFinanceAccountId[financeAccountId] = append(addedProviderTransactionIdsByFinanceAccountId[financeAccountId], added.ProviderTransactionID)
		written, err := self.upsertFinanceTransaction(agentId, financeAccountId, added, now)
		if err != nil {
			return nil, err
		}
		// Before the pending one is removed below: Plaid names it on the
		// posted one it became, in the same sync that removes it.
		if added.PendingProviderTransactionID != "" && added.PendingProviderTransactionID != added.ProviderTransactionID {
			if err := self.carryPendingDecisions(agentId, sourceId, financeAccountId, added.ProviderTransactionID, added.PendingProviderTransactionID, now); err != nil {
				return nil, err
			}
		}
		if written == nil {
			continue
		}
		applied.WrittenTransactionCount++
		writtenIds = append(writtenIds, written.ID)
		// Inserted rather than changed: an insert writes both times as the
		// same moment, an update only the modified one.
		isInserted := written.CreatedAt.Equal(written.ModifiedAt)
		if isInserted {
			applied.InsertedTransactionCount++
		}
		if !written.IsPending && isInserted {
			insertedPostedIdsByFinanceAccountId[financeAccountId] = append(insertedPostedIdsByFinanceAccountId[financeAccountId], written.ID)
		}
	}

	if len(syncResult.RemovedProviderTransactionIDs) > 0 {
		removed := self.tx.Exec(`DELETE FROM "agent_finance_transaction" WHERE "agent_id" = ?
			AND "finance_account_id" IN (SELECT "id" FROM "agent_finance_account" WHERE "agent_id" = ? AND "source_id" = ?)
			AND "provider_transaction_id" = ANY(?::text[])`,
			agentId, agentId, sourceId, pq.Array(syncResult.RemovedProviderTransactionIDs))
		if removed.Error != nil {
			return nil, removed.Error
		}
		applied.RemovedTransactionCount = int(removed.RowsAffected)
	}

	if syncResult.PendingReplacedFrom != nil {
		replacedFrom := syncResult.PendingReplacedFrom.UTC().Format(time.DateOnly)
		for _, financeAccountId := range reportedAccountIds {
			kept := addedProviderTransactionIdsByFinanceAccountId[financeAccountId]
			if kept == nil {
				kept = []string{}
			}
			if err := self.carryReplacedPendingDecisions(agentId, financeAccountId, replacedFrom, kept, insertedPostedIdsByFinanceAccountId[financeAccountId], now); err != nil {
				return nil, err
			}
			replaced := self.tx.Exec(`DELETE FROM "agent_finance_transaction" WHERE "agent_id" = ? AND "finance_account_id" = ?
				AND "is_pending" AND "posted_on" >= ?::date AND "provider_transaction_id" <> ALL(?::text[])`,
				agentId, financeAccountId, replacedFrom, pq.Array(kept))
			if replaced.Error != nil {
				return nil, replaced.Error
			}
			applied.ReplacedPendingTransactionCount += int(replaced.RowsAffected)
		}
	}

	holdingsApplied, err := self.applyFinanceHoldings(agentId, syncResult, financeAccountIdByProviderAccountId, reportedBalances,
		securityIdByProviderSecurityId, syncedOn, now, applied)
	if err != nil {
		return nil, err
	}
	for _, financeAccountId := range reportedAccountIds {
		if holdingsApplied.isValuationSkipped[financeAccountId] {
			continue
		}
		account := reportedBalances[financeAccountId]
		// An account whose holdings were read is worth its cash; the
		// holdings are valued on their own assets.
		if cashAccount, isRead := holdingsApplied.accountByFinanceAccountId[financeAccountId]; isRead {
			account = cashAccount
		}
		isRecorded, err := self.recordFinanceSyncValuation(agentId, financeAccountId, account, syncedOn, now)
		if err != nil {
			return nil, err
		}
		if isRecorded {
			applied.RecordedValuationCount++
		}
	}

	// Within the sync's own transaction, so a copy whose counted copy this
	// sync removed or changed never reads as a duplicate of nothing.
	if _, err := self.DetectMirroredFinanceTransactions(agentId, sourceId); err != nil {
		return nil, err
	}

	// Read back rather than taken from the upsert: a decision carried
	// from a pending transaction may have categorized or marked one since.
	if len(writtenIds) > 0 {
		var toCategorize []string
		if err := self.tx.Raw(`SELECT "id" FROM "agent_finance_transaction"
			WHERE "agent_id" = ? AND "id" = ANY(?::text[]) AND "categorized_by" = ''
			ORDER BY "posted_on" DESC, "id" DESC`, agentId, pq.Array(writtenIds)).Scan(&toCategorize).Error; err != nil {
			return nil, err
		}
		applied.FinanceTransactionIDsToCategorize = append(applied.FinanceTransactionIDsToCategorize, toCategorize...)
	}
	return applied, nil
}

// carriedDecisions is the SET list that gives the finance transaction
// "posted" what the person decided about the pending one it became,
// "pending": their spending category, the transfer category included, and
// their word that a mirrored copy counts, each only where the person
// decided it and has not decided again on the posted one. Without the
// second, detection would mark the posted copy a duplicate again as soon
// as the charge posted.
const carriedDecisions = `
	"spending_category_id" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN "pending"."spending_category_id" ELSE "posted"."spending_category_id" END,
	"categorization_confidence" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN NULL ELSE "posted"."categorization_confidence" END,
	"categorized_by" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN 'person' ELSE "posted"."categorized_by" END,
	"duplicate_of_transaction_id" = CASE WHEN "pending"."duplicate_decided_by" = 'person' AND "posted"."duplicate_decided_by" <> 'person'
		THEN NULL ELSE "posted"."duplicate_of_transaction_id" END,
	"duplicate_decided_by" = CASE WHEN "pending"."duplicate_decided_by" = 'person' AND "posted"."duplicate_decided_by" <> 'person'
		THEN 'person' ELSE "posted"."duplicate_decided_by" END,
	"modified_at" = @modified_at`

// hasDecisionToCarry is true when the pending finance transaction holds a
// decision of the person's the posted one does not have yet.
const hasDecisionToCarry = `(("pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person')
	OR ("pending"."duplicate_decided_by" = 'person' AND "posted"."duplicate_decided_by" <> 'person'))`

// carryPendingDecisions gives a posted finance transaction what the person
// decided about the pending one of the same finance source it names, which
// the provider is about to remove: the spending category they chose,
// transfer or not, would otherwise be lost as soon as the charge posted.
func (self *transaction) carryPendingDecisions(agentId, sourceId, financeAccountId, postedProviderTransactionId, pendingProviderTransactionId string, now time.Time) error {
	return self.tx.Exec(`UPDATE "agent_finance_transaction" AS "posted" SET `+carriedDecisions+`
		FROM "agent_finance_transaction" AS "pending"
		WHERE "posted"."agent_id" = @agent_id AND "posted"."finance_account_id" = @finance_account_id
		  AND "posted"."provider_transaction_id" = @posted_provider_transaction_id
		  AND "pending"."agent_id" = @agent_id AND "pending"."id" <> "posted"."id"
		  AND "pending"."provider_transaction_id" = @pending_provider_transaction_id
		  AND "pending"."finance_account_id" IN (SELECT "id" FROM "agent_finance_account" WHERE "agent_id" = @agent_id AND "source_id" = @source_id)
		  AND `+hasDecisionToCarry,
		map[string]any{
			"agent_id": agentId, "source_id": sourceId, "finance_account_id": financeAccountId, "modified_at": now,
			"posted_provider_transaction_id": postedProviderTransactionId, "pending_provider_transaction_id": pendingProviderTransactionId,
		}).Error
}

// carryReplacedPendingDecisions is carryPendingDecisions for a provider
// that does not say which posted transaction a pending one became
// (SimpleFIN): before the pending ones of a finance account that this sync
// replaced are deleted, each the person decided about hands its decisions
// to a posted one this sync inserted for the same account, with the same
// amount and currency, posted within a few days of it. Charges post in
// the order they were made, so among equal amounts the first pending one
// goes to the first posted one, the second to the second, and so on; a
// pair further apart than a few days is not taken.
func (self *transaction) carryReplacedPendingDecisions(agentId, financeAccountId, replacedFrom string, keptProviderTransactionIds, insertedPostedIds []string, now time.Time) error {
	if len(insertedPostedIds) == 0 {
		return nil
	}
	return self.tx.Exec(`WITH "replaced" AS (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY "amount", "currency_code" ORDER BY "posted_on", "id") AS "amount_rank"
			FROM "agent_finance_transaction"
			WHERE "agent_id" = @agent_id AND "finance_account_id" = @finance_account_id AND "is_pending"
			  AND "posted_on" >= CAST(@replaced_from AS date) AND "provider_transaction_id" <> ALL(CAST(@kept AS text[]))
			  AND "categorized_by" = 'person'
		), "arrived" AS (
			SELECT "id", "amount", "currency_code", "posted_on",
				ROW_NUMBER() OVER (PARTITION BY "amount", "currency_code" ORDER BY "posted_on", "id") AS "amount_rank"
			FROM "agent_finance_transaction"
			WHERE "agent_id" = @agent_id AND "finance_account_id" = @finance_account_id AND NOT "is_pending"
			  AND "id" = ANY(CAST(@inserted AS text[]))
			  AND "categorized_by" <> 'person'
		)
		UPDATE "agent_finance_transaction" AS "posted" SET `+carriedDecisions+`
		FROM "arrived" JOIN "replaced" AS "pending"
		  ON "pending"."amount" = "arrived"."amount" AND "pending"."currency_code" = "arrived"."currency_code"
		 AND "pending"."amount_rank" = "arrived"."amount_rank"
		 AND "arrived"."posted_on" BETWEEN "pending"."posted_on" - CAST(@posting_days AS integer)
		                               AND "pending"."posted_on" + CAST(@posting_days AS integer)
		WHERE "posted"."id" = "arrived"."id" AND "posted"."agent_id" = @agent_id
		  AND `+hasDecisionToCarry,
		map[string]any{
			"agent_id": agentId, "finance_account_id": financeAccountId, "replaced_from": replacedFrom,
			"kept": pq.Array(keptProviderTransactionIds), "inserted": pq.Array(insertedPostedIds),
			"posting_days": pendingPostingDays, "modified_at": now,
		}).Error
}

// upsertFinanceAccount writes one finance account as the provider reported
// it, and says whether it was new. With isNewerBalanceKept, a balance
// older than the one stored leaves the stored one in place.
func (self *transaction) upsertFinanceAccount(agentId, sourceId string, account finance.Account, isNewerBalanceKept bool, now time.Time) (string, bool, error) {
	if account.ProviderAccountID == "" {
		return "", false, fmt.Errorf("%w: a finance account needs the provider's id", ErrInvalidArguments)
	}
	accountKind := models.FinanceAccountKind(account.AccountKind)
	if !accountKind.IsValid() {
		accountKind = models.FinanceAccountKindOther
	}
	currentBalance, err := canonicalOptionalAmount("current balance", account.CurrentBalance)
	if err != nil {
		return "", false, err
	}
	availableBalance, err := canonicalOptionalAmount("available balance", account.AvailableBalance)
	if err != nil {
		return "", false, err
	}
	creditLimitAmount, err := canonicalOptionalAmount("credit limit", account.CreditLimitAmount)
	if err != nil {
		return "", false, err
	}
	var balanceAt *time.Time
	if !account.BalanceAt.IsZero() {
		balanceAt = &account.BalanceAt
	}
	providerMetadata, err := providerMetadataJSON(account.ProviderMetadata)
	if err != nil {
		return "", false, err
	}
	var upserted struct {
		ID         string `gorm:"column:id"`
		IsInserted bool   `gorm:"column:is_inserted"`
	}
	// xmax is zero on a row this statement inserted and set on one it
	// updated, which is how the one round trip says which it was.
	err = self.tx.Raw(`INSERT INTO "agent_finance_account" AS "existing" ("id", "agent_id", "source_id", "provider_account_id",
			"account_name", "account_mask", "account_kind", "currency_code", "current_balance", "available_balance",
			"balance_at", "credit_limit_amount", "provider_metadata", "created_at", "modified_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::numeric, ?::numeric, ?, ?::numeric, ?::jsonb, ?, ?)
		ON CONFLICT ("source_id", "provider_account_id") DO UPDATE SET
			"account_name" = EXCLUDED."account_name", "account_mask" = EXCLUDED."account_mask",
			"account_kind" = EXCLUDED."account_kind", "currency_code" = EXCLUDED."currency_code",
			"current_balance" = CASE WHEN `+financeBalanceIsOlder+` THEN "existing"."current_balance" ELSE EXCLUDED."current_balance" END,
			"available_balance" = CASE WHEN `+financeBalanceIsOlder+` THEN "existing"."available_balance" ELSE EXCLUDED."available_balance" END,
			"balance_at" = CASE WHEN `+financeBalanceIsOlder+` THEN "existing"."balance_at" ELSE EXCLUDED."balance_at" END,
			"credit_limit_amount" = CASE WHEN `+financeBalanceIsOlder+` THEN "existing"."credit_limit_amount" ELSE EXCLUDED."credit_limit_amount" END,
			"provider_metadata" = CASE WHEN `+financeBalanceIsOlder+` THEN "existing"."provider_metadata" ELSE EXCLUDED."provider_metadata" END,
			"modified_at" = EXCLUDED."modified_at"
		RETURNING "id", ("xmax" = 0) AS "is_inserted"`,
		newID(), agentId, sourceId, account.ProviderAccountID, account.AccountName, account.AccountMask,
		string(accountKind), account.CurrencyCode, currentBalance, availableBalance, balanceAt, creditLimitAmount, providerMetadata, now, now,
		isNewerBalanceKept, isNewerBalanceKept, isNewerBalanceKept, isNewerBalanceKept, isNewerBalanceKept).
		Scan(&upserted).Error
	if err != nil {
		return "", false, err
	}
	return upserted.ID, upserted.IsInserted, nil
}

// financeBalanceIsOlder is true inside the account upsert when the caller
// keeps the newer balance (its one argument) and the balance being
// written is from before the one stored, or there is none: a statement
// with no balance in it says nothing about the balance, and must not
// empty the one an earlier statement gave.
const financeBalanceIsOlder = `(CAST(? AS boolean) AND "existing"."balance_at" IS NOT NULL
	AND (EXCLUDED."balance_at" IS NULL OR EXCLUDED."balance_at" < "existing"."balance_at"))`

// retirementAccountSubtypes are Plaid's subtypes of an investment account
// that is saved for retirement, in the United States and Canada and the
// United Kingdom, where it names them.
var retirementAccountSubtypes = map[string]bool{
	"401a": true, "401k": true, "403b": true, "457b": true, "ira": true, "keogh": true, "pension": true,
	"profit sharing plan": true, "retirement": true, "roth": true, "roth 401k": true, "sarsep": true, "sep ira": true,
	"simple ira": true, "thrift savings plan": true, "lif": true, "lira": true, "lrif": true, "lrsp": true, "prif": true,
	"rlif": true, "rrif": true, "rrsp": true, "sipp": true,
}

// assetKindForFinanceAccount is the kind of the asset made for a finance
// account. Plaid says a loan is a mortgage only in the account's subtype,
// which is in its provider metadata.
func assetKindForFinanceAccount(accountKind models.FinanceAccountKind, providerMetadata json.RawMessage) models.AssetKind {
	switch accountKind {
	case models.FinanceAccountKindDepository:
		return models.AssetKindCash
	case models.FinanceAccountKindInvestment:
		var described struct {
			Subtype string `json:"subtype"`
		}
		if json.Unmarshal(providerMetadata, &described) == nil && retirementAccountSubtypes[strings.ToLower(described.Subtype)] {
			return models.AssetKindRetirement
		}
		return models.AssetKindInvestment
	case models.FinanceAccountKindCredit:
		return models.AssetKindCreditCard
	case models.FinanceAccountKindLoan:
		var described struct {
			Subtype string `json:"subtype"`
		}
		if json.Unmarshal(providerMetadata, &described) == nil && strings.EqualFold(described.Subtype, "mortgage") {
			return models.AssetKindMortgage
		}
		return models.AssetKindLoan
	}
	return models.AssetKindOtherAsset
}

// financeValuationValue is the value a finance account's balance records
// for its asset.
//
// Providers disagree on the sign of what is owed: Plaid reports a card or
// loan balance as a positive amount owed, SimpleFIN leaves it to the
// institution and they report it negative; the account says which
// (isOwedBalancePositive). A valuation's value is the size of the thing
// and the asset's is_liability gives the sign, so for a liability the
// value is the amount owed: positive for a debt, and negative for a
// credit in the person's favour, such as a refund left on a paid-off
// card, which then adds to net worth rather than subtracting. Anything
// else keeps the balance as reported, which may be negative: an overdrawn
// checking account is a negative asset, not a liability.
func financeValuationValue(currentBalance string, isLiability, isOwedBalancePositive bool) (string, error) {
	valuationValue, err := canonicalAmount("current balance", currentBalance)
	if err != nil {
		return "", err
	}
	if isLiability && !isOwedBalancePositive {
		amountOwed, err := finance.ParseAmount(valuationValue)
		if err != nil {
			return "", err
		}
		valuationValue = finance.FormatAmount(amountOwed.Neg(amountOwed))
	}
	return valuationValue, nil
}

// createFinanceSyncAsset makes the asset a finance account seen for the
// first time is valued as, in the same transaction that inserted it.
func (self *transaction) createFinanceSyncAsset(agentId, financeAccountId string, account finance.Account, now time.Time) (string, error) {
	assetKind := assetKindForFinanceAccount(models.FinanceAccountKind(account.AccountKind), account.ProviderMetadata)
	assetName := strings.TrimSpace(account.AccountName)
	if assetName == "" {
		assetName = "Account " + account.AccountMask
	}
	model := &agentAssetModel{
		ID: newID(), AgentID: agentId, AssetName: strings.TrimSpace(assetName), AssetKind: string(assetKind),
		IsLiability: assetKind.IsLiability(), CurrencyCode: account.CurrencyCode, FinanceAccountID: &financeAccountId,
		ValuationSource: string(models.ValuationSourceFinanceSync), CreatedAt: now, ModifiedAt: now,
	}
	// The same account linked again, after its source was deleted: the
	// asset the old link made was kept, closed on the day of the delete,
	// and taking it back keeps the account's history on one asset instead
	// of starting a second one. It is taken back, and opened again, when
	// exactly one detached asset has this name, kind, side and currency
	// and has valuations the sync recorded, which no asset the person
	// made by hand has.
	var detachedIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "agent_asset" AS "asset"
		WHERE "agent_id" = ? AND "finance_account_id" IS NULL AND "finance_security_id" IS NULL AND "valuation_source" = ?
		  AND "asset_name" = ? AND "asset_kind" = ? AND "is_liability" = ? AND "currency_code" = ?
		  AND EXISTS (SELECT 1 FROM "agent_asset_valuation" WHERE "asset_id" = "asset"."id" AND "valuation_source" = ?)
		LIMIT 2`, agentId, string(models.ValuationSourceManual), model.AssetName, model.AssetKind, model.IsLiability,
		model.CurrencyCode, string(models.ValuationSourceFinanceSync)).Scan(&detachedIds).Error; err != nil {
		return "", err
	}
	if len(detachedIds) == 1 {
		if err := self.tx.Model(&agentAssetModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, detachedIds[0]).Updates(map[string]any{
			"finance_account_id": financeAccountId, "valuation_source": string(models.ValuationSourceFinanceSync), "closed_on": nil,
			"modified_at": now,
		}).Error; err != nil {
			return "", err
		}
		return detachedIds[0], nil
	}
	if err := self.tx.Create(model).Error; err != nil {
		return "", err
	}
	return model.ID, nil
}

// recordFinanceSyncValuation records a finance account's balance as its
// asset's valuation for the day, once a day however often it syncs. It
// records nothing when the balance is unknown, when the person deleted the
// asset, or when they turned it to another valuation source.
func (self *transaction) recordFinanceSyncValuation(agentId, financeAccountId string, account finance.Account, syncedOn string, now time.Time) (bool, error) {
	if strings.TrimSpace(account.CurrentBalance) == "" {
		return false, nil
	}
	found, err := self.financeAccountOwnAsset(agentId, financeAccountId)
	if err != nil {
		return false, err
	}
	if found == nil || found.ValuationSource != string(models.ValuationSourceFinanceSync) {
		return false, nil
	}
	value, err := financeValuationValue(account.CurrentBalance, found.IsLiability, account.IsOwedBalancePositive)
	if err != nil {
		return false, err
	}
	currencyCode := account.CurrencyCode
	if currencyCode == "" {
		currencyCode = found.CurrencyCode
	}
	if _, err := self.upsertAssetValuation(&models.AssetValuation{
		AgentID: agentId, AssetID: found.ID, ValuedOn: syncedOn, Value: value, CurrencyCode: currencyCode,
		ValuationSource: models.ValuationSourceFinanceSync,
	}, now); err != nil {
		return false, err
	}
	return true, nil
}

// upsertFinanceTransaction writes one finance transaction the provider
// added or changed, and returns it as written, or nil when it was already
// stored exactly so.
//
// The person's columns survive: a spending category the person chose,
// the transfer category included. A spending category anything else gave
// is dropped when what it was judged from (the merchant, the description,
// the provider's category) changed, so it is judged again, and so is a
// categorize model's earlier failure to place it. A transfer found by
// pairing is dropped when the amount changed, so the pairing runs again;
// one a spending rule or the provider category mapping gave is dropped
// when the amount or what it was judged from changed, and the rules and
// the mapping run again after the sync.
func (self *transaction) upsertFinanceTransaction(agentId, financeAccountId string, added finance.Transaction, now time.Time) (*agentFinanceTransactionModel, error) {
	if added.ProviderTransactionID == "" {
		return nil, fmt.Errorf("%w: a finance transaction needs the provider's id", ErrInvalidArguments)
	}
	postedOn, err := parseDay(added.PostedOn)
	if err != nil {
		return nil, err
	}
	amount, err := canonicalAmount("amount", added.Amount)
	if err != nil {
		return nil, err
	}
	providerMetadata, err := providerMetadataJSON(added.ProviderMetadata)
	if err != nil {
		return nil, err
	}
	var written []agentFinanceTransactionModel
	err = self.tx.Raw(`INSERT INTO "agent_finance_transaction" AS "existing" ("id", "agent_id", "finance_account_id",
			"provider_transaction_id", "posted_on", "transacted_at", "amount", "currency_code", "description", "merchant_name",
			"provider_category_primary", "provider_category_detailed", "is_pending", "pending_provider_transaction_id",
			"provider_metadata", "created_at", "modified_at")
		VALUES (?, ?, ?, ?, ?::date, ?, ?::numeric, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?)
		ON CONFLICT ("finance_account_id", "provider_transaction_id") DO UPDATE SET
			"posted_on" = EXCLUDED."posted_on", "transacted_at" = EXCLUDED."transacted_at",
			"amount" = EXCLUDED."amount", "currency_code" = EXCLUDED."currency_code",
			"description" = EXCLUDED."description", "merchant_name" = EXCLUDED."merchant_name",
			"provider_category_primary" = EXCLUDED."provider_category_primary",
			"provider_category_detailed" = EXCLUDED."provider_category_detailed",
			"is_pending" = EXCLUDED."is_pending", "pending_provider_transaction_id" = EXCLUDED."pending_provider_transaction_id",
			"provider_metadata" = EXCLUDED."provider_metadata", "modified_at" = EXCLUDED."modified_at",
			"spending_category_id" = CASE WHEN `+financeCategoryNoLongerHolds+`
				THEN NULL ELSE "existing"."spending_category_id" END,
			"categorized_by" = CASE WHEN `+financeCategoryNoLongerHolds+`
				THEN '' ELSE "existing"."categorized_by" END,
			"categorization_confidence" = CASE WHEN `+financeCategoryNoLongerHolds+`
				THEN NULL ELSE "existing"."categorization_confidence" END,
			"categorize_attempted_at" = CASE WHEN `+financeJudgedFromChanged+`
				THEN NULL ELSE "existing"."categorize_attempted_at" END
		WHERE ("existing"."posted_on", "existing"."transacted_at", "existing"."amount", "existing"."currency_code",
				"existing"."description", "existing"."merchant_name", "existing"."provider_category_primary",
				"existing"."provider_category_detailed", "existing"."is_pending", "existing"."pending_provider_transaction_id",
				"existing"."provider_metadata")
			IS DISTINCT FROM (EXCLUDED."posted_on", EXCLUDED."transacted_at", EXCLUDED."amount", EXCLUDED."currency_code",
				EXCLUDED."description", EXCLUDED."merchant_name", EXCLUDED."provider_category_primary",
				EXCLUDED."provider_category_detailed", EXCLUDED."is_pending", EXCLUDED."pending_provider_transaction_id",
				EXCLUDED."provider_metadata")
		RETURNING *`,
		newID(), agentId, financeAccountId, added.ProviderTransactionID, postedOn, added.TransactedAt, amount,
		added.CurrencyCode, added.Description, added.MerchantName, added.ProviderCategoryPrimary,
		added.ProviderCategoryDetailed, added.IsPending, added.PendingProviderTransactionID, providerMetadata, now, now).
		Scan(&written).Error
	if err != nil {
		return nil, err
	}
	if len(written) == 0 {
		return nil, nil
	}
	return &written[0], nil
}

// financeJudgedFromChanged is true inside the upsert when what a spending
// category is judged from changed.
const financeJudgedFromChanged = `("existing"."description", "existing"."merchant_name", "existing"."provider_category_primary", "existing"."provider_category_detailed")
	IS DISTINCT FROM (EXCLUDED."description", EXCLUDED."merchant_name", EXCLUDED."provider_category_primary", EXCLUDED."provider_category_detailed")`

// financeCategoryNoLongerHolds is true inside the upsert when what gave
// the finance transaction its spending category, other than the person,
// judged it from something that changed: a pair from the amount alone; a
// spending rule, the mapping or the categorize model from the text and
// provider category, and from the amount too when what it gave is the
// transfer category.
const financeCategoryNoLongerHolds = `("existing"."categorized_by" NOT IN ('', 'person') AND (
	("existing"."categorized_by" = 'transfer_detection' AND "existing"."amount" <> EXCLUDED."amount")
	OR ("existing"."categorized_by" <> 'transfer_detection' AND (` + financeJudgedFromChanged + `
		OR ("existing"."amount" <> EXCLUDED."amount" AND "existing"."spending_category_id" IN (
			SELECT "id" FROM "agent_spending_category" WHERE "agent_id" = "existing"."agent_id" AND "is_transfer"))))))`

// --- reads -------------------------------------------------------------

func (self *transaction) GetFinanceTransaction(agentId, financeTransactionId string) (*models.FinanceTransaction, error) {
	var found []agentFinanceTransactionModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, financeTransactionId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) GetFinanceTransactions(agentId string, financeTransactionIds []string) ([]*models.FinanceTransaction, error) {
	if len(financeTransactionIds) == 0 {
		return nil, nil
	}
	var found []agentFinanceTransactionModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ANY(?::text[])`, agentId, pq.Array(financeTransactionIds)).Find(&found).Error; err != nil {
		return nil, err
	}
	financeTransactions := make([]*models.FinanceTransaction, 0, len(found))
	for index := range found {
		financeTransactions = append(financeTransactions, found[index].toModel())
	}
	return financeTransactions, nil
}

// financeTransactionCursor writes where a page ended: the posted day and
// the id of its last row, which is the order pages are read in.
func financeTransactionCursor(last *agentFinanceTransactionModel) string {
	return formatDay(last.PostedOn) + "/" + last.ID
}

func parseFinanceTransactionCursor(cursor string) (string, string, error) {
	postedOn, financeTransactionId, isCut := strings.Cut(cursor, "/")
	if !isCut || financeTransactionId == "" {
		return "", "", fmt.Errorf("%w: %q is not a cursor from a page of finance transactions", ErrInvalidArguments, cursor)
	}
	postedOn, err := parseDay(postedOn)
	if err != nil {
		return "", "", fmt.Errorf("%w: %q is not a cursor from a page of finance transactions", ErrInvalidArguments, cursor)
	}
	return postedOn, financeTransactionId, nil
}

// financeTransactionQuery is the finance transactions a filter matches,
// the cursor, the offset and the limit aside: what a page is read from
// and what its total counts.
func (self *transaction) financeTransactionQuery(agentId string, filter *FinanceTransactionFilter) (*gorm.DB, error) {
	query := self.tx.Model(&agentFinanceTransactionModel{}).Where(`"agent_id" = ?`, agentId)
	from, err := parseOptionalDay(filter.From)
	if err != nil {
		return nil, err
	}
	if from != "" {
		query = query.Where(`"posted_on" >= ?::date`, from)
	}
	to, err := parseOptionalDay(filter.To)
	if err != nil {
		return nil, err
	}
	if to != "" {
		query = query.Where(`"posted_on" <= ?::date`, to)
	}
	if filter.FinanceAccountID != "" {
		query = query.Where(`"finance_account_id" = ?`, filter.FinanceAccountID)
	}
	if text := strings.TrimSpace(filter.Text); text != "" {
		pattern := "%" + escapeLike(text) + "%"
		query = query.Where(`("description" ILIKE ? OR "merchant_name" ILIKE ?)`, pattern, pattern)
	}
	minimumAmount, err := canonicalOptionalAmount("minimum amount", filter.MinimumAmount)
	if err != nil {
		return nil, err
	}
	if minimumAmount != nil {
		query = query.Where(`"amount" >= ?::numeric`, *minimumAmount)
	}
	maximumAmount, err := canonicalOptionalAmount("maximum amount", filter.MaximumAmount)
	if err != nil {
		return nil, err
	}
	if maximumAmount != nil {
		query = query.Where(`"amount" <= ?::numeric`, *maximumAmount)
	}
	if filter.ProviderCategory != "" {
		query = query.Where(`("provider_category_primary" = ? OR "provider_category_detailed" = ?)`, filter.ProviderCategory, filter.ProviderCategory)
	}
	if filter.SpendingCategoryID != "" {
		query = query.Where(`"spending_category_id" = ?`, filter.SpendingCategoryID)
	}
	if filter.IsUncategorized {
		query = query.Where(`"spending_category_id" IS NULL AND "duplicate_of_transaction_id" IS NULL`)
	}
	if filter.IsTransferExcluded {
		query = query.Where(`("spending_category_id" IS NULL OR "spending_category_id" NOT IN (
			SELECT "id" FROM "agent_spending_category" WHERE "agent_id" = ? AND "is_transfer"))`, agentId)
	}
	if filter.IsDuplicateExcluded {
		query = query.Where(`"duplicate_of_transaction_id" IS NULL`)
	}
	if filter.DuplicateOfTransactionID != "" {
		query = query.Where(`"duplicate_of_transaction_id" = ?`, filter.DuplicateOfTransactionID)
	}
	if len(filter.FinanceTransactionIDs) > 0 {
		query = query.Where(`"id" = ANY(?::text[])`, pq.Array(filter.FinanceTransactionIDs))
	}
	return query, nil
}

func (self *transaction) ListFinanceTransactions(agentId string, filter *FinanceTransactionFilter) (*FinanceTransactionPage, error) {
	if filter == nil {
		filter = &FinanceTransactionFilter{}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = FinanceTransactionLimitDefault
	}
	if limit > FinanceTransactionLimitMost {
		limit = FinanceTransactionLimitMost
	}
	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: an offset cannot be negative", ErrInvalidArguments)
	}
	page := &FinanceTransactionPage{}
	if filter.ShouldCountTotal {
		// The mirrored copies left out are counted in the same statement
		// as the total: the filter without IsDuplicateExcluded, counting
		// all of its rows and, apart, the ones that are not a copy.
		withDuplicates := *filter
		withDuplicates.IsDuplicateExcluded = false
		counted, err := self.financeTransactionQuery(agentId, &withDuplicates)
		if err != nil {
			return nil, err
		}
		var counts struct {
			TotalWithDuplicatesCount    int64
			TotalWithoutDuplicatesCount int64
		}
		if err := counted.Select(`COUNT(*) AS "total_with_duplicates_count",
			COUNT(*) FILTER (WHERE "duplicate_of_transaction_id" IS NULL) AS "total_without_duplicates_count"`).
			Scan(&counts).Error; err != nil {
			return nil, err
		}
		page.TotalCount = int(counts.TotalWithDuplicatesCount)
		if filter.IsDuplicateExcluded {
			page.TotalCount = int(counts.TotalWithoutDuplicatesCount)
			page.LeftOutDuplicateCount = int(counts.TotalWithDuplicatesCount - counts.TotalWithoutDuplicatesCount)
		}
	}
	query, err := self.financeTransactionQuery(agentId, filter)
	if err != nil {
		return nil, err
	}
	if filter.After != "" {
		postedOn, financeTransactionId, err := parseFinanceTransactionCursor(filter.After)
		if err != nil {
			return nil, err
		}
		query = query.Where(`("posted_on", "id") < (?::date, ?)`, postedOn, financeTransactionId)
	}
	if filter.ShouldReadIDsOnly {
		// The posted day too, since the cursor for the next page is written
		// from it.
		query = query.Select(`"id"`, `"posted_on"`)
	}
	var found []agentFinanceTransactionModel
	// One more than the page, to know whether there is another.
	if err := query.Order(`"posted_on" DESC, "id" DESC`).Offset(filter.Offset).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	page.FinanceTransactions = make([]*models.FinanceTransaction, 0, len(found))
	if len(found) > limit {
		found = found[:limit]
		page.NextCursor = financeTransactionCursor(&found[len(found)-1])
	}
	for index := range found {
		page.FinanceTransactions = append(page.FinanceTransactions, found[index].toModel())
	}
	return page, nil
}

func (self *transaction) ListUncategorizedFinanceTransactions(agentId string, limit int) ([]*models.FinanceTransaction, error) {
	if limit <= 0 || limit > FinanceTransactionLimitMost {
		limit = FinanceTransactionLimitMost
	}
	var found []agentFinanceTransactionModel
	if err := self.tx.Where(`"agent_id" = ? AND "spending_category_id" IS NULL AND "categorized_by" <> 'person'
			AND "categorize_attempted_at" IS NULL AND "duplicate_of_transaction_id" IS NULL`, agentId).
		Order(`"posted_on" DESC, "id" DESC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	transactions := make([]*models.FinanceTransaction, 0, len(found))
	for index := range found {
		transactions = append(transactions, found[index].toModel())
	}
	return transactions, nil
}

func (self *transaction) MarkCategorizeAttempted(agentId string, financeTransactionIds []string) (int, error) {
	if len(financeTransactionIds) == 0 {
		return 0, nil
	}
	marked := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "categorize_attempted_at" = ?
		WHERE "agent_id" = ? AND "id" = ANY(?::text[]) AND "spending_category_id" IS NULL`,
		time.Now(), agentId, pq.Array(financeTransactionIds))
	return int(marked.RowsAffected), marked.Error
}

// --- categorization and transfers --------------------------------------

// errNoSpendingCategoryChosen refuses the person taking a finance
// transaction's spending category away. No spending category is the
// state of one not decided yet, which the categorize model and the
// listing of what needs a category work on; the person saying it fits
// nothing is the other category.
var errNoSpendingCategoryChosen = fmt.Errorf("%w: choose a spending category; for one that fits none of them, choose the built-in other category", ErrInvalidArguments)

func (self *transaction) SetTransactionCategorization(agentId, financeTransactionId, spendingCategoryId string, categorizedBy models.CategorizedBy, categorizationConfidence *string) (bool, error) {
	if !categorizedBy.IsValid() {
		return false, fmt.Errorf("%w: %q is not who categorized it", ErrInvalidArguments, categorizedBy)
	}
	var confidence *string
	if categorizationConfidence != nil && strings.TrimSpace(*categorizationConfidence) != "" {
		canonical, err := finance.CanonicalAmount(*categorizationConfidence)
		value, isParsed := new(big.Rat).SetString(canonical)
		if err != nil || !isParsed || value.Sign() < 0 || value.Cmp(big.NewRat(1, 1)) > 0 {
			return false, fmt.Errorf("%w: the confidence %q is not a decimal between 0 and 1", ErrInvalidArguments, *categorizationConfidence)
		}
		confidence = &canonical
	}
	if spendingCategoryId == "" && categorizedBy == models.CategorizedByPerson {
		return false, errNoSpendingCategoryChosen
	}
	if spendingCategoryId != "" {
		category, err := self.GetSpendingCategory(agentId, spendingCategoryId)
		if err != nil {
			return false, err
		}
		if category == nil {
			return false, ErrNotFound
		}
		if category.IsTransfer && categorizedBy == models.CategorizedByCategorizeModel {
			return false, fmt.Errorf("%w: the categorize model does not mark transfers", ErrInvalidArguments)
		}
	}
	// A spending category given clears the categorize model's earlier
	// failure, so that if what gave it takes it away again (a spending
	// rule deleted) the model is asked afresh.
	updated := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "spending_category_id" = ?, "categorized_by" = ?,
			"categorization_confidence" = ?::numeric, "modified_at" = ?,
			"categorize_attempted_at" = CASE WHEN ? THEN NULL ELSE "categorize_attempted_at" END
		WHERE "agent_id" = ? AND "id" = ? AND ("categorized_by" <> 'person' OR ? = 'person')`,
		optionalID(spendingCategoryId), string(categorizedBy), confidence, time.Now(), spendingCategoryId != "",
		agentId, financeTransactionId, string(categorizedBy))
	if updated.Error != nil {
		return false, updated.Error
	}
	if updated.RowsAffected > 0 {
		return true, nil
	}
	existing, err := self.GetFinanceTransaction(agentId, financeTransactionId)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, ErrNotFound
	}
	return false, nil
}

func (self *transaction) CategorizeTransactionsByPerson(agentId string, financeTransactionIds []string, spendingCategoryId string) (int, error) {
	isDistinct := map[string]bool{}
	for _, financeTransactionId := range financeTransactionIds {
		isDistinct[financeTransactionId] = true
	}
	if len(isDistinct) == 0 {
		return 0, nil
	}
	if spendingCategoryId == "" {
		return 0, errNoSpendingCategoryChosen
	}
	category, err := self.GetSpendingCategory(agentId, spendingCategoryId)
	if err != nil {
		return 0, err
	}
	if category == nil {
		return 0, ErrNotFound
	}
	// Counted before the write, so a list naming somebody else's finance
	// transaction leaves none of the agent's written either.
	var ownedCount int64
	if err := self.tx.Model(&agentFinanceTransactionModel{}).
		Where(`"agent_id" = ? AND "id" = ANY(?::text[])`, agentId, pq.Array(financeTransactionIds)).Count(&ownedCount).Error; err != nil {
		return 0, err
	}
	if int(ownedCount) != len(isDistinct) {
		return 0, ErrNotFound
	}
	updated := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "spending_category_id" = ?, "categorized_by" = ?,
			"categorization_confidence" = NULL, "modified_at" = ?, "categorize_attempted_at" = NULL
		WHERE "agent_id" = ? AND "id" = ANY(?::text[])`,
		spendingCategoryId, string(models.CategorizedByPerson), time.Now(), agentId, pq.Array(financeTransactionIds))
	return int(updated.RowsAffected), updated.Error
}

func (self *transaction) DetectFinanceTransfers(agentId, sourceId, sinceDate string) (int, error) {
	sinceDate, err := parseDay(sinceDate)
	if err != nil {
		return 0, err
	}
	transferCategory, err := self.EnsureTransferSpendingCategory(agentId)
	if err != nil {
		return 0, err
	}
	// The finance accounts in scope: one source's, or all of the agent's.
	scope := `SELECT "id" FROM "agent_finance_account" WHERE "agent_id" = @agent_id AND (@source_id = '' OR "source_id" = @source_id)`
	arguments := map[string]any{
		"agent_id": agentId, "source_id": sourceId, "since_date": sinceDate, "modified_at": time.Now(),
		"transfer_category_id": transferCategory.ID,
	}

	// What the provider calls a transfer. The mapping is in Go, so the
	// candidates are read and the matches written back. A finance
	// transaction the person categorized is their decision that it is
	// spending or income.
	var candidates []struct {
		ID                       string `gorm:"column:id"`
		ProviderCategoryPrimary  string `gorm:"column:provider_category_primary"`
		ProviderCategoryDetailed string `gorm:"column:provider_category_detailed"`
	}
	if err := self.tx.Raw(`SELECT "id", "provider_category_primary", "provider_category_detailed" FROM "agent_finance_transaction"
		WHERE "agent_id" = @agent_id AND "finance_account_id" IN (`+scope+`) AND "posted_on" >= CAST(@since_date AS date)
		  AND "spending_category_id" IS DISTINCT FROM @transfer_category_id AND "categorized_by" <> 'person'
		  AND ("provider_category_primary" <> '' OR "provider_category_detailed" <> '')`, arguments).
		Scan(&candidates).Error; err != nil {
		return 0, err
	}
	providerTransferIds := []string{}
	for _, candidate := range candidates {
		if _, isTransfer := finance.MapProviderCategory(candidate.ProviderCategoryPrimary, candidate.ProviderCategoryDetailed); isTransfer {
			providerTransferIds = append(providerTransferIds, candidate.ID)
		}
	}
	markedCount := 0
	if len(providerTransferIds) > 0 {
		arguments["transfer_ids"] = pq.Array(providerTransferIds)
		marked := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "spending_category_id" = @transfer_category_id,
				"categorized_by" = 'provider_category_mapping', "categorization_confidence" = NULL, "modified_at" = @modified_at
			WHERE "agent_id" = @agent_id AND "id" = ANY(CAST(@transfer_ids AS text[]))
			  AND "spending_category_id" IS DISTINCT FROM @transfer_category_id AND "categorized_by" <> 'person'`, arguments)
		if marked.Error != nil {
			return 0, marked.Error
		}
		markedCount += int(marked.RowsAffected)
	}

	// Money out of one account matched by the same amount into another
	// within a few days. At least one side is in scope and recent; the
	// other may be any of the agent's accounts, so a card payment pairs
	// with its checking withdrawal however the two were linked.
	//
	// Posted money only. A pending transaction is replaced by a new row
	// when it posts, and the mark does not travel with it: a pending side
	// paired now would leave its partner marked and the posted side, which
	// then has nothing left to pair with, counted as spending.
	//
	// One to one: each side takes the other side closest in days, and a
	// pair is marked only when each is the other's first choice. A 500
	// moved to savings on Monday and a 500 rent check on Tuesday are two
	// candidates for the one 500 that arrived in savings on Monday; the
	// transfer is the closer, and the rent stays spending. What a round
	// leaves over pairs in the next, once the pairs before it are marked.
	//
	// A side the provider category already called a transfer can still be
	// paired, and pairing marks it as paired: a card payment out of
	// checking is a transfer by its category, and the card's side of it,
	// often from another provider with no category, is found only by
	// pairing. Leaving the marked side out left that credit counted as a
	// refund on the card, and a month's spending went below zero. A side
	// already paired, given the transfer category by a spending rule, or
	// categorized by the person is never taken again, and a mirrored copy
	// is never taken at all: the money moved once, on its counted copy.
	arguments["pairing_days"] = transferPairingDays
	for round := 0; round < transferPairingRounds; round++ {
		paired := self.tx.Exec(`WITH "eligible" AS (
				SELECT "id", "finance_account_id", "currency_code", "amount", "posted_on" FROM "agent_finance_transaction"
				WHERE "agent_id" = @agent_id AND "categorized_by" <> 'person' AND NOT "is_pending" AND "duplicate_of_transaction_id" IS NULL
				  AND ("spending_category_id" IS DISTINCT FROM @transfer_category_id OR "categorized_by" = 'provider_category_mapping')
			), "ranked" AS (
				SELECT "money_out"."id" AS "money_out_id", "money_in"."id" AS "money_in_id",
					ROW_NUMBER() OVER (PARTITION BY "money_out"."id"
						ORDER BY abs("money_in"."posted_on" - "money_out"."posted_on"), "money_in"."posted_on", "money_in"."id") AS "money_out_choice",
					ROW_NUMBER() OVER (PARTITION BY "money_in"."id"
						ORDER BY abs("money_in"."posted_on" - "money_out"."posted_on"), "money_out"."posted_on", "money_out"."id") AS "money_in_choice"
				FROM "eligible" AS "money_out"
				JOIN "eligible" AS "money_in"
				  ON "money_in"."finance_account_id" <> "money_out"."finance_account_id"
				 AND "money_in"."currency_code" = "money_out"."currency_code"
				 AND "money_in"."amount" = -"money_out"."amount"
				 AND "money_in"."posted_on" BETWEEN "money_out"."posted_on" - CAST(@pairing_days AS integer) AND "money_out"."posted_on" + CAST(@pairing_days AS integer)
				WHERE "money_out"."amount" < 0
				  AND (("money_out"."posted_on" >= CAST(@since_date AS date) AND "money_out"."finance_account_id" IN (`+scope+`))
				    OR ("money_in"."posted_on" >= CAST(@since_date AS date) AND "money_in"."finance_account_id" IN (`+scope+`)))
			)
			UPDATE "agent_finance_transaction" SET "spending_category_id" = @transfer_category_id,
				"categorized_by" = 'transfer_detection', "categorization_confidence" = NULL, "modified_at" = @modified_at
			WHERE "agent_id" = @agent_id AND "categorized_by" <> 'person'
			  AND ("spending_category_id" IS DISTINCT FROM @transfer_category_id OR "categorized_by" = 'provider_category_mapping')
			  AND "id" IN (
				SELECT unnest(ARRAY["money_out_id", "money_in_id"]) FROM "ranked"
				WHERE "money_out_choice" = 1 AND "money_in_choice" = 1
			)`, arguments)
		if paired.Error != nil {
			return 0, paired.Error
		}
		markedCount += int(paired.RowsAffected)
		if paired.RowsAffected == 0 {
			break
		}
	}
	return markedCount, nil
}

// --- mirrored copies ---------------------------------------------------

// mirroredFinanceTransactionsDecided is every finance transaction of the
// agent in scope (one source's accounts, or all of them when @source_id is
// empty) that the person has not counted, with the counted copy it is a
// duplicate of, or null. Migration 0144 marked the copies stored before it
// by the same rule.
//
// Only Plaid sources, and only sets whose every copy is on an investment
// account: that is where one account-level fee is reported on each
// account of a connection. A SimpleFIN credential can reach accounts at
// several institutions, and a checking and a savings account of one Plaid
// item can each be charged the same monthly fee for real.
//
// A pending copy goes with posted ones too: the copies of one charge post
// on different syncs, and a pending copy left alone in its set while
// another account's copy has posted would count the charge twice until
// it posted too. A posted copy is counted before a pending one, so the
// counted copy is never one the provider is about to replace while a
// posted copy is there to count instead.
//
// The n-th of a day's repeats on one account goes with the n-th on each
// other account, posted ones numbered first, so a set never holds two of
// one account: a fee charged twice on one account is two charges, not a
// copy. Among posted copies, or among pending ones, the counted copy is the
// one stored first, so a copy that arrives later never takes over; among
// those a sync stored together, the one on the oldest account, so a source's
// fees are counted on the same account month after month.
const mirroredFinanceTransactionsDecided = `WITH "scope" AS (
		SELECT "copy"."id", "copy"."finance_account_id", "account"."source_id", "copy"."is_pending", "copy"."posted_on", "copy"."amount",
			"copy"."currency_code", lower(btrim("copy"."description")) AS "description_key",
			"account"."account_kind" = 'investment' AS "is_investment_account",
			"copy"."created_at", "account"."created_at" AS "account_created_at"
		FROM "agent_finance_transaction" AS "copy"
		JOIN "agent_finance_account" AS "account" ON "account"."id" = "copy"."finance_account_id" AND "account"."agent_id" = "copy"."agent_id"
		JOIN "agent_source" AS "source" ON "source"."id" = "account"."source_id" AND "source"."agent_id" = "copy"."agent_id"
		WHERE "copy"."agent_id" = @agent_id AND (@source_id = '' OR "account"."source_id" = @source_id)
		  AND "copy"."duplicate_decided_by" <> 'person' AND btrim("copy"."description") <> ''
		  AND COALESCE("source"."specification"->>'type', '') = 'plaid'
	), "numbered" AS (
		SELECT *, ROW_NUMBER() OVER (PARTITION BY "finance_account_id", "posted_on", "amount", "currency_code", "description_key"
			ORDER BY "is_pending", "created_at", "id") AS "occurrence"
		FROM "scope"
	), "ranked" AS (
		SELECT "id",
			FIRST_VALUE("id") OVER "mirrored_set" AS "counted_id",
			COUNT(*) OVER "mirrored_set" AS "member_count",
			bool_and("is_investment_account") OVER "mirrored_set" AS "is_every_account_investment"
		FROM "numbered"
		WINDOW "mirrored_set" AS (PARTITION BY "source_id", "posted_on", "amount", "currency_code", "description_key", "occurrence"
			ORDER BY "is_pending", "created_at", "account_created_at", "finance_account_id", "id"
			ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
	), "decided" AS (
		SELECT "candidate"."id",
			CASE WHEN "ranked"."member_count" > 1 AND "ranked"."is_every_account_investment" AND "ranked"."counted_id" <> "ranked"."id"
				THEN "ranked"."counted_id" END AS "duplicate_of_transaction_id"
		FROM "agent_finance_transaction" AS "candidate"
		JOIN "agent_finance_account" AS "account" ON "account"."id" = "candidate"."finance_account_id" AND "account"."agent_id" = "candidate"."agent_id"
		LEFT JOIN "ranked" ON "ranked"."id" = "candidate"."id"
		WHERE "candidate"."agent_id" = @agent_id AND (@source_id = '' OR "account"."source_id" = @source_id)
		  AND "candidate"."duplicate_decided_by" <> 'person'
	)`

// lockMirroredFinanceSources locks the finance source (every source of the
// agent's when sourceId is empty, in id order) the way LockAgentSource
// does, so detection and the person counting a copy of the same source
// take turns: a sync never writes over a "count this one" made while it
// ran, and two detections never wait on each other's rows.
func (self *transaction) lockMirroredFinanceSources(agentId, sourceId string) error {
	query := self.tx.Model(&agentSourceModel{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ?`, agentId)
	if sourceId != "" {
		query = query.Where(`"id" = ?`, sourceId)
	}
	var lockedIds []string
	return query.Order(`"id"`).Pluck(`"id"`, &lockedIds).Error
}

func (self *transaction) DetectMirroredFinanceTransactions(agentId, sourceId string) (int, error) {
	if agentId == "" {
		return 0, fmt.Errorf("%w: mirror detection needs an agent", ErrInvalidArguments)
	}
	if err := self.lockMirroredFinanceSources(agentId, sourceId); err != nil {
		return 0, err
	}
	// Every row in scope is decided afresh rather than only those a sync
	// wrote: a deleted counted copy is no longer there to say which rows
	// were its copies, and one source's rows are few enough to group in
	// one statement. A row the person counted after this statement read
	// it is checked again on its new version and left alone.
	updated := self.tx.Exec(mirroredFinanceTransactionsDecided+`
		UPDATE "agent_finance_transaction" AS "target" SET
			"duplicate_of_transaction_id" = "decided"."duplicate_of_transaction_id",
			"duplicate_decided_by" = CASE WHEN "decided"."duplicate_of_transaction_id" IS NULL THEN '' ELSE 'mirror_detection' END,
			"modified_at" = @modified_at
		FROM "decided"
		WHERE "target"."id" = "decided"."id" AND "target"."agent_id" = @agent_id AND "target"."duplicate_decided_by" <> 'person'
		  AND ("target"."duplicate_of_transaction_id", "target"."duplicate_decided_by")
		      IS DISTINCT FROM ("decided"."duplicate_of_transaction_id",
		          CASE WHEN "decided"."duplicate_of_transaction_id" IS NULL THEN '' ELSE 'mirror_detection' END)`,
		map[string]any{"agent_id": agentId, "source_id": sourceId, "modified_at": time.Now()})
	if updated.Error != nil {
		return 0, updated.Error
	}
	return int(updated.RowsAffected), nil
}

func (self *transaction) SetFinanceTransactionCountedByPerson(agentId, financeTransactionId string, isCountedByPerson bool) error {
	var sourceIds []string
	if err := self.tx.Raw(`SELECT "account"."source_id" FROM "agent_finance_transaction" AS "copy"
		JOIN "agent_finance_account" AS "account" ON "account"."id" = "copy"."finance_account_id" AND "account"."agent_id" = "copy"."agent_id"
		WHERE "copy"."agent_id" = ? AND "copy"."id" = ?`, agentId, financeTransactionId).Scan(&sourceIds).Error; err != nil {
		return err
	}
	if len(sourceIds) == 0 {
		return ErrNotFound
	}
	// The source first, then the row read under it, so a sync of the same
	// source finishes before or starts after the person's word.
	if err := self.lockMirroredFinanceSources(agentId, sourceIds[0]); err != nil {
		return err
	}
	var found []agentFinanceTransactionModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, financeTransactionId).Limit(1).Find(&found).Error; err != nil {
		return err
	}
	if len(found) == 0 {
		return ErrNotFound
	}
	existing := found[0]
	if isCountedByPerson {
		if existing.DuplicateDecidedBy == string(models.DuplicateDecidedByPerson) {
			return nil
		}
		if existing.DuplicateOfTransactionID == nil {
			return fmt.Errorf("%w: the finance transaction is not a duplicate of another, so it counts already", ErrInvalidArguments)
		}
	} else if existing.DuplicateDecidedBy != string(models.DuplicateDecidedByPerson) {
		return nil
	}
	decidedBy := ""
	if isCountedByPerson {
		decidedBy = string(models.DuplicateDecidedByPerson)
	}
	if err := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "duplicate_of_transaction_id" = NULL, "duplicate_decided_by" = ?, "modified_at" = ?
		WHERE "agent_id" = ? AND "id" = ?`, decidedBy, time.Now(), agentId, financeTransactionId).Error; err != nil {
		return err
	}
	// Decided again now rather than at the next sync, so a copy handed back
	// to detection is left out of the totals as soon as the person says so.
	_, err := self.DetectMirroredFinanceTransactions(agentId, sourceIds[0])
	return err
}

// --- the spending summary ----------------------------------------------

func (self *transaction) FinanceSpendingSummary(agentId string, filter *FinanceSpendingSummaryFilter) ([]*models.FinanceSpendingSummaryRow, error) {
	if filter == nil {
		filter = &FinanceSpendingSummaryFilter{}
	}
	groupBy := filter.GroupBy
	if groupBy == "" {
		groupBy = models.FinanceSpendingSummaryGroupBySpendingCategory
	}
	var groupKey, groupLabel string
	switch groupBy {
	case models.FinanceSpendingSummaryGroupByProviderCategory:
		groupKey = `COALESCE(NULLIF("summarized"."provider_category_primary", ''), "summarized"."provider_category_detailed")`
		groupLabel = groupKey
	case models.FinanceSpendingSummaryGroupBySpendingCategory:
		groupKey = `COALESCE("summarized"."spending_category_id", '')`
		groupLabel = `COALESCE("spending_category"."spending_category_name", '')`
	case models.FinanceSpendingSummaryGroupByMerchant:
		groupKey = `COALESCE(NULLIF("summarized"."merchant_name", ''), "summarized"."description")`
		groupLabel = groupKey
	case models.FinanceSpendingSummaryGroupByMonth:
		groupKey = `to_char("summarized"."posted_on", 'YYYY-MM')`
		groupLabel = groupKey
	case models.FinanceSpendingSummaryGroupByFinanceAccount:
		groupKey = `"summarized"."finance_account_id"`
		groupLabel = `COALESCE("finance_account"."account_name", '')`
	default:
		return nil, fmt.Errorf("%w: %q is not a way to group spending", ErrInvalidArguments, groupBy)
	}
	conditions := []string{
		`"summarized"."agent_id" = @agent_id`, `NOT COALESCE("spending_category"."is_transfer", false)`,
		`"summarized"."duplicate_of_transaction_id" IS NULL`,
	}
	arguments := map[string]any{"agent_id": agentId}
	from, err := parseOptionalDay(filter.From)
	if err != nil {
		return nil, err
	}
	if from != "" {
		conditions = append(conditions, `"summarized"."posted_on" >= CAST(@from AS date)`)
		arguments["from"] = from
	}
	to, err := parseOptionalDay(filter.To)
	if err != nil {
		return nil, err
	}
	if to != "" {
		conditions = append(conditions, `"summarized"."posted_on" <= CAST(@to AS date)`)
		arguments["to"] = to
	}
	if filter.FinanceAccountID != "" {
		conditions = append(conditions, `"summarized"."finance_account_id" = @finance_account_id`)
		arguments["finance_account_id"] = filter.FinanceAccountID
	}
	var rows []struct {
		GroupKey                string `gorm:"column:group_key"`
		GroupLabel              string `gorm:"column:group_label"`
		CurrencyCode            string `gorm:"column:currency_code"`
		MoneyOut                string `gorm:"column:money_out"`
		MoneyIn                 string `gorm:"column:money_in"`
		FinanceTransactionCount int    `gorm:"column:finance_transaction_count"`
	}
	moneyOut := `COALESCE(SUM(-"summarized"."amount") FILTER (WHERE "summarized"."amount" < 0), 0::numeric(19,4))`
	moneyIn := `COALESCE(SUM("summarized"."amount") FILTER (WHERE "summarized"."amount" > 0), 0::numeric(19,4))`
	err = self.tx.Raw(`SELECT `+groupKey+` AS "group_key", MAX(`+groupLabel+`) AS "group_label", "summarized"."currency_code",
			`+moneyOut+`::text AS "money_out", `+moneyIn+`::text AS "money_in", COUNT(*) AS "finance_transaction_count"
		FROM "agent_finance_transaction" AS "summarized"
		LEFT JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "summarized"."spending_category_id" AND "spending_category"."agent_id" = "summarized"."agent_id"
		LEFT JOIN "agent_finance_account" AS "finance_account"
		  ON "finance_account"."id" = "summarized"."finance_account_id" AND "finance_account"."agent_id" = "summarized"."agent_id"
		WHERE `+strings.Join(conditions, " AND ")+`
		GROUP BY 1, "summarized"."currency_code"
		ORDER BY `+moneyOut+` DESC, 1 ASC, "summarized"."currency_code" ASC`, arguments).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	summary := make([]*models.FinanceSpendingSummaryRow, 0, len(rows))
	for _, row := range rows {
		summary = append(summary, &models.FinanceSpendingSummaryRow{
			GroupKey: row.GroupKey, GroupLabel: row.GroupLabel, CurrencyCode: row.CurrencyCode,
			MoneyOut: row.MoneyOut, MoneyIn: row.MoneyIn, FinanceTransactionCount: row.FinanceTransactionCount,
		})
	}
	return summary, nil
}
