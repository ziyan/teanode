package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

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

	// ListFinanceTransactions is a page of the agent's finance
	// transactions, newest first, narrowed by the filter.
	ListFinanceTransactions(agentId string, filter *FinanceTransactionFilter) (*FinanceTransactionPage, error)

	// ListUncategorizedFinanceTransactions is the agent's finance
	// transactions with no spending category that the person has not
	// decided about, that are not transfers, and that the categorize
	// model has not already been asked about and failed to place: what
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
	// model, its confidence. It refuses, answering false, to overwrite
	// what the person chose unless the person is choosing again.
	// ErrNotFound when the agent has no such finance transaction or no
	// such spending category.
	//
	// The person clearing a spending category leaves the finance
	// transaction uncategorized for good: it is their decision, so the
	// categorize model is not asked about it again.
	SetTransactionCategorization(agentId, financeTransactionId, spendingCategoryId string, categorizedBy models.CategorizedBy, categorizationConfidence *string) (bool, error)

	// MarkFinanceTransactionTransfer marks a finance transaction as a
	// transfer or not, saying what decided it. The person decides either
	// way, and nothing else may change it after. Anything else marks a
	// transfer only where the person has not decided, and unmarks only a
	// transfer it marked itself; otherwise it is refused, answering false.
	MarkFinanceTransactionTransfer(agentId, financeTransactionId string, isTransfer bool, transferMarkedBy models.TransferMarkedBy) (bool, error)

	// DetectFinanceTransfers marks as transfers the finance transactions
	// posted on or after sinceDate in one finance source's accounts (every
	// source's when sourceId is empty) whose provider category is a
	// transfer, and pairs of the same absolute amount and currency,
	// opposite signs, on two different finance accounts of the agent,
	// posted within three days of each other, each finance transaction in
	// at most one pair and each pair the closest in days for both of its
	// sides. A finance transaction already a transfer, or whose spending
	// category or transfer the person decided, is neither marked nor
	// paired. It answers how many it marked.
	DetectFinanceTransfers(agentId, sourceId, sinceDate string) (int, error)

	// FinanceSpendingSummary is money out and money in per group per
	// currency over a range of posted days, transfers left out, biggest
	// money out first.
	FinanceSpendingSummary(agentId string, filter *FinanceSpendingSummaryFilter) ([]*models.FinanceSpendingSummaryRow, error)
}

// FinanceSyncApplied is what one ApplyFinanceSync wrote.
type FinanceSyncApplied struct {
	// InsertedFinanceAccountIDs are the finance accounts seen for the
	// first time, and CreatedAssetIDs the assets made for them.
	InsertedFinanceAccountIDs []string
	CreatedAssetIDs           []string

	// WrittenTransactionCount is the finance transactions inserted or
	// changed; one sent again unchanged is not counted.
	WrittenTransactionCount int

	// RemovedTransactionCount is the finance transactions the provider
	// removed, and ReplacedPendingTransactionCount the pending ones it
	// stopped reporting inside the window it replaced.
	RemovedTransactionCount         int
	ReplacedPendingTransactionCount int

	// SkippedTransactionCount is the transactions for a finance account
	// neither the result nor the store knows, which were not written.
	SkippedTransactionCount int

	// RecordedValuationCount is the finance accounts whose balance was
	// recorded as their asset's valuation for the day.
	RecordedValuationCount int

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
	// category that are not transfers.
	IsUncategorized bool

	// Limit is at most FinanceTransactionLimitMost; zero is
	// FinanceTransactionLimitDefault.
	Limit int

	// After is the NextCursor of the page before.
	After string
}

// How many finance transactions one page holds.
const (
	FinanceTransactionLimitDefault = 50
	FinanceTransactionLimitMost    = 200
)

// FinanceTransactionPage is one page of finance transactions, and the
// cursor for the next, empty on the last.
type FinanceTransactionPage struct {
	FinanceTransactions []*models.FinanceTransaction
	NextCursor          string
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
		ProviderMetadata: rawJSON(self.ProviderMetadata),
		CreatedAt:        self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
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
	IsTransfer                   bool       `gorm:"column:is_transfer"`
	TransferMarkedBy             string     `gorm:"column:transfer_marked_by"`
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
		IsTransfer:               self.IsTransfer, TransferMarkedBy: models.TransferMarkedBy(self.TransferMarkedBy),
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
	var isSourceFound bool
	if err := self.tx.Raw(`SELECT EXISTS (SELECT 1 FROM "agent_source" WHERE "id" = ? AND "agent_id" = ?)`, sourceId, agentId).
		Scan(&isSourceFound).Error; err != nil {
		return nil, err
	}
	if !isSourceFound {
		return nil, ErrNotFound
	}

	applied := &FinanceSyncApplied{
		InsertedFinanceAccountIDs: []string{}, CreatedAssetIDs: []string{}, FinanceTransactionIDsToCategorize: []string{},
	}
	now := time.Now()

	reportedAccountIds := make([]string, 0, len(syncResult.Accounts))
	reportedBalances := map[string]finance.Account{}
	for _, account := range syncResult.Accounts {
		financeAccountId, isInserted, err := self.upsertFinanceAccount(agentId, sourceId, account, now)
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
		if !written.IsPending && written.CreatedAt.Equal(written.ModifiedAt) {
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

	for _, financeAccountId := range reportedAccountIds {
		account := reportedBalances[financeAccountId]
		isRecorded, err := self.recordFinanceSyncValuation(agentId, financeAccountId, account, syncedOn, now)
		if err != nil {
			return nil, err
		}
		if isRecorded {
			applied.RecordedValuationCount++
		}
	}

	// Read back rather than taken from the upsert: a decision carried
	// from a pending transaction may have categorized or marked one since.
	if len(writtenIds) > 0 {
		var toCategorize []string
		if err := self.tx.Raw(`SELECT "id" FROM "agent_finance_transaction"
			WHERE "agent_id" = ? AND "id" = ANY(?::text[]) AND "categorized_by" = '' AND NOT "is_transfer"
			ORDER BY "posted_on" DESC, "id" DESC`, agentId, pq.Array(writtenIds)).Scan(&toCategorize).Error; err != nil {
			return nil, err
		}
		applied.FinanceTransactionIDsToCategorize = append(applied.FinanceTransactionIDsToCategorize, toCategorize...)
	}
	return applied, nil
}

// carriedDecisions is the SET list that gives the finance transaction
// "posted" what the person decided about the pending one it became,
// "pending": their spending category and their transfer mark, each only
// where the person decided it and has not decided it again on the posted
// one.
const carriedDecisions = `
	"spending_category_id" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN "pending"."spending_category_id" ELSE "posted"."spending_category_id" END,
	"categorization_confidence" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN NULL ELSE "posted"."categorization_confidence" END,
	"categorized_by" = CASE WHEN "pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person'
		THEN 'person' ELSE "posted"."categorized_by" END,
	"is_transfer" = CASE WHEN "pending"."transfer_marked_by" = 'person' AND "posted"."transfer_marked_by" <> 'person'
		THEN "pending"."is_transfer" ELSE "posted"."is_transfer" END,
	"transfer_marked_by" = CASE WHEN "pending"."transfer_marked_by" = 'person' AND "posted"."transfer_marked_by" <> 'person'
		THEN 'person' ELSE "posted"."transfer_marked_by" END,
	"modified_at" = @modified_at`

// hasDecisionToCarry is true when the pending finance transaction holds a
// decision of the person's the posted one does not have yet.
const hasDecisionToCarry = `(("pending"."categorized_by" = 'person' AND "posted"."categorized_by" <> 'person')
	OR ("pending"."transfer_marked_by" = 'person' AND "posted"."transfer_marked_by" <> 'person'))`

// carryPendingDecisions gives a posted finance transaction what the person
// decided about the pending one of the same finance source it names, which
// the provider is about to remove: the spending category they chose and
// the transfer they marked or unmarked would otherwise be lost as soon as
// the charge posted.
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
			  AND ("categorized_by" = 'person' OR "transfer_marked_by" = 'person')
		), "arrived" AS (
			SELECT "id", "amount", "currency_code", "posted_on",
				ROW_NUMBER() OVER (PARTITION BY "amount", "currency_code" ORDER BY "posted_on", "id") AS "amount_rank"
			FROM "agent_finance_transaction"
			WHERE "agent_id" = @agent_id AND "finance_account_id" = @finance_account_id AND NOT "is_pending"
			  AND "id" = ANY(CAST(@inserted AS text[]))
			  AND "categorized_by" <> 'person' AND "transfer_marked_by" <> 'person'
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
// it, and says whether it was new.
func (self *transaction) upsertFinanceAccount(agentId, sourceId string, account finance.Account, now time.Time) (string, bool, error) {
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
	err = self.tx.Raw(`INSERT INTO "agent_finance_account" ("id", "agent_id", "source_id", "provider_account_id",
			"account_name", "account_mask", "account_kind", "currency_code", "current_balance", "available_balance",
			"balance_at", "provider_metadata", "created_at", "modified_at")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::numeric, ?::numeric, ?, ?::jsonb, ?, ?)
		ON CONFLICT ("source_id", "provider_account_id") DO UPDATE SET
			"account_name" = EXCLUDED."account_name", "account_mask" = EXCLUDED."account_mask",
			"account_kind" = EXCLUDED."account_kind", "currency_code" = EXCLUDED."currency_code",
			"current_balance" = EXCLUDED."current_balance", "available_balance" = EXCLUDED."available_balance",
			"balance_at" = EXCLUDED."balance_at", "provider_metadata" = EXCLUDED."provider_metadata",
			"modified_at" = EXCLUDED."modified_at"
		RETURNING "id", ("xmax" = 0) AS "is_inserted"`,
		newID(), agentId, sourceId, account.ProviderAccountID, account.AccountName, account.AccountMask,
		string(accountKind), account.CurrencyCode, currentBalance, availableBalance, balanceAt, providerMetadata, now, now).
		Scan(&upserted).Error
	if err != nil {
		return "", false, err
	}
	return upserted.ID, upserted.IsInserted, nil
}

// assetKindForFinanceAccount is the kind of the asset made for a finance
// account. Plaid says a loan is a mortgage only in the account's subtype,
// which is in its provider metadata.
func assetKindForFinanceAccount(accountKind models.FinanceAccountKind, providerMetadata json.RawMessage) models.AssetKind {
	switch accountKind {
	case models.FinanceAccountKindDepository:
		return models.AssetKindCash
	case models.FinanceAccountKindInvestment:
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
	var found []agentAssetModel
	if err := self.tx.Where(`"agent_id" = ? AND "finance_account_id" = ?`, agentId, financeAccountId).Limit(1).Find(&found).Error; err != nil {
		return false, err
	}
	if len(found) == 0 || found[0].ValuationSource != string(models.ValuationSourceFinanceSync) {
		return false, nil
	}
	value, err := financeValuationValue(account.CurrentBalance, found[0].IsLiability, account.IsOwedBalancePositive)
	if err != nil {
		return false, err
	}
	currencyCode := account.CurrencyCode
	if currencyCode == "" {
		currencyCode = found[0].CurrencyCode
	}
	if _, err := self.upsertAssetValuation(&models.AssetValuation{
		AgentID: agentId, AssetID: found[0].ID, ValuedOn: syncedOn, Value: value, CurrencyCode: currencyCode,
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
// The person's columns survive: a spending category the person chose, and
// a transfer the person marked or unmarked. A spending category anything
// else gave is dropped when what it was judged from (the merchant, the
// description, the provider's category) changed, so it is judged again,
// and so is a categorize model's earlier failure to place it. A transfer
// found by pairing is dropped when the amount changed, so the pairing runs
// again; one a spending rule or the provider category mapping marked is
// dropped when the amount or what it was judged from changed, and the
// rules and the mapping run again after the sync.
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
			"spending_category_id" = CASE WHEN "existing"."categorized_by" <> 'person' AND `+financeJudgedFromChanged+`
				THEN NULL ELSE "existing"."spending_category_id" END,
			"categorized_by" = CASE WHEN "existing"."categorized_by" <> 'person' AND `+financeJudgedFromChanged+`
				THEN '' ELSE "existing"."categorized_by" END,
			"categorization_confidence" = CASE WHEN "existing"."categorized_by" <> 'person' AND `+financeJudgedFromChanged+`
				THEN NULL ELSE "existing"."categorization_confidence" END,
			"categorize_attempted_at" = CASE WHEN `+financeJudgedFromChanged+`
				THEN NULL ELSE "existing"."categorize_attempted_at" END,
			"is_transfer" = CASE WHEN `+financeTransferNoLongerHolds+`
				THEN false ELSE "existing"."is_transfer" END,
			"transfer_marked_by" = CASE WHEN `+financeTransferNoLongerHolds+`
				THEN '' ELSE "existing"."transfer_marked_by" END
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

// financeTransferNoLongerHolds is true inside the upsert when what marked
// the finance transaction a transfer was judged from something that
// changed: the amount, for a pair; the amount or the text and provider
// category, for a spending rule or the mapping.
const financeTransferNoLongerHolds = `(("existing"."transfer_marked_by" = 'detection' AND "existing"."amount" <> EXCLUDED."amount")
	OR ("existing"."transfer_marked_by" IN ('spending_rule', 'provider_category_mapping')
		AND ("existing"."amount" <> EXCLUDED."amount" OR ` + financeJudgedFromChanged + `)))`

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
		query = query.Where(`"spending_category_id" IS NULL AND NOT "is_transfer"`)
	}
	if filter.After != "" {
		postedOn, financeTransactionId, err := parseFinanceTransactionCursor(filter.After)
		if err != nil {
			return nil, err
		}
		query = query.Where(`("posted_on", "id") < (?::date, ?)`, postedOn, financeTransactionId)
	}
	var found []agentFinanceTransactionModel
	// One more than the page, to know whether there is another.
	if err := query.Order(`"posted_on" DESC, "id" DESC`).Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, err
	}
	page := &FinanceTransactionPage{FinanceTransactions: make([]*models.FinanceTransaction, 0, len(found))}
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
	if err := self.tx.Where(`"agent_id" = ? AND "spending_category_id" IS NULL AND "categorized_by" <> 'person' AND NOT "is_transfer"
			AND "categorize_attempted_at" IS NULL`, agentId).
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
	if spendingCategoryId != "" {
		category, err := self.GetSpendingCategory(agentId, spendingCategoryId)
		if err != nil {
			return false, err
		}
		if category == nil {
			return false, ErrNotFound
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

func (self *transaction) MarkFinanceTransactionTransfer(agentId, financeTransactionId string, isTransfer bool, transferMarkedBy models.TransferMarkedBy) (bool, error) {
	if !transferMarkedBy.IsValid() {
		return false, fmt.Errorf("%w: %q is not what marks a transfer", ErrInvalidArguments, transferMarkedBy)
	}
	var updated *gorm.DB
	switch {
	case transferMarkedBy == models.TransferMarkedByPerson:
		updated = self.tx.Exec(`UPDATE "agent_finance_transaction" SET "is_transfer" = ?, "transfer_marked_by" = 'person', "modified_at" = ?
			WHERE "agent_id" = ? AND "id" = ?`, isTransfer, time.Now(), agentId, financeTransactionId)
	case isTransfer:
		// A transfer something else already marked keeps its origin, so
		// that what marked it first is what may clear it.
		updated = self.tx.Exec(`UPDATE "agent_finance_transaction" SET "is_transfer" = true, "transfer_marked_by" = ?, "modified_at" = ?
			WHERE "agent_id" = ? AND "id" = ? AND NOT "is_transfer" AND "transfer_marked_by" <> 'person'`,
			string(transferMarkedBy), time.Now(), agentId, financeTransactionId)
	default:
		updated = self.tx.Exec(`UPDATE "agent_finance_transaction" SET "is_transfer" = false, "transfer_marked_by" = '', "modified_at" = ?
			WHERE "agent_id" = ? AND "id" = ? AND "transfer_marked_by" = ?`,
			time.Now(), agentId, financeTransactionId, string(transferMarkedBy))
	}
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

func (self *transaction) DetectFinanceTransfers(agentId, sourceId, sinceDate string) (int, error) {
	sinceDate, err := parseDay(sinceDate)
	if err != nil {
		return 0, err
	}
	// The finance accounts in scope: one source's, or all of the agent's.
	scope := `SELECT "id" FROM "agent_finance_account" WHERE "agent_id" = @agent_id AND (@source_id = '' OR "source_id" = @source_id)`
	arguments := map[string]any{"agent_id": agentId, "source_id": sourceId, "since_date": sinceDate, "modified_at": time.Now()}

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
		  AND NOT "is_transfer" AND "transfer_marked_by" <> 'person' AND "categorized_by" <> 'person'
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
		marked := self.tx.Exec(`UPDATE "agent_finance_transaction" SET "is_transfer" = true, "transfer_marked_by" = 'provider_category_mapping',
				"modified_at" = @modified_at
			WHERE "agent_id" = @agent_id AND "id" = ANY(CAST(@transfer_ids AS text[])) AND NOT "is_transfer" AND "transfer_marked_by" <> 'person'`, arguments)
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
	// One to one: each side takes the other side closest in days, and a
	// pair is marked only when each is the other's first choice. A 500
	// moved to savings on Monday and a 500 rent check on Tuesday are two
	// candidates for the one 500 that arrived in savings on Monday; the
	// transfer is the closer, and the rent stays spending. What a round
	// leaves over pairs in the next, once the pairs before it are marked.
	arguments["pairing_days"] = transferPairingDays
	for round := 0; round < transferPairingRounds; round++ {
		paired := self.tx.Exec(`WITH "eligible" AS (
				SELECT "id", "finance_account_id", "currency_code", "amount", "posted_on" FROM "agent_finance_transaction"
				WHERE "agent_id" = @agent_id AND NOT "is_transfer" AND "transfer_marked_by" <> 'person' AND "categorized_by" <> 'person'
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
			UPDATE "agent_finance_transaction" SET "is_transfer" = true, "transfer_marked_by" = 'detection', "modified_at" = @modified_at
			WHERE "agent_id" = @agent_id AND NOT "is_transfer" AND "transfer_marked_by" <> 'person' AND "id" IN (
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
	conditions := []string{`"summarized"."agent_id" = @agent_id`, `NOT "summarized"."is_transfer"`}
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
