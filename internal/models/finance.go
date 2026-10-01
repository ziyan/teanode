package models

import (
	"encoding/json"
	"strings"
	"time"
)

// FinanceSourceCron is how often a finance source syncs unless the person
// sets another schedule: four times a day. Institutions post a day's
// transactions in one or two batches, and a provider may charge per call,
// so more often buys nothing.
const FinanceSourceCron = "0 */6 * * *"

// FinanceCredentialSecretKey is the source secret a finance source keeps
// its credential under.
const FinanceCredentialSecretKey = "credential"

// The source secrets of the finance source that holds imported statements
// (provider kind "statement"): the token that makes its import address,
// which is what lets mail in, and the key its accounts' identifiers are
// hashed with, so the institution's account identifier is never stored.
const (
	FinanceStatementTokenSecretKey      = "statementImportToken"
	FinanceStatementAccountKeySecretKey = "statementAccountKey"
)

// FinanceCursorLastStatementImport is the cursor key of the statement
// finance source that holds its last import, a FinanceStatementImport.
const FinanceCursorLastStatementImport = "lastStatementImport"

// StatementImportOrigin is how a statement reached the server.
type StatementImportOrigin string

// A statement mailed to the person's import address, a file uploaded from
// the dashboard or the command line, or the attachment of a message the
// person pointed at.
const (
	StatementImportOriginMail    StatementImportOrigin = "mail"
	StatementImportOriginUpload  StatementImportOrigin = "upload"
	StatementImportOriginMessage StatementImportOrigin = "message"
)

// FinanceStatementImport is what one import of statement files did: kept
// on the statement finance source as its last import, and answered to
// whoever imported.
type FinanceStatementImport struct {
	ImportedAt            time.Time             `json:"importedAt"`
	StatementImportOrigin StatementImportOrigin `json:"statementImportOrigin"`

	// StatementFileNames are the files read, as they were named.
	StatementFileNames []string `json:"statementFileNames"`

	// The transactions: new ones added, stored ones whose fields changed,
	// stored ones the file repeated unchanged, and ones not written
	// because their account could not be found.
	AddedTransactionCount     int `json:"addedTransactionCount"`
	UpdatedTransactionCount   int `json:"updatedTransactionCount"`
	UnchangedTransactionCount int `json:"unchangedTransactionCount"`
	SkippedTransactionCount   int `json:"skippedTransactionCount"`

	// TransactionWithoutFITIDCount is how many had no FITID, the
	// institution's identifier, and were known by their day, amount and
	// name instead.
	TransactionWithoutFITIDCount int `json:"transactionWithoutFitIdCount"`

	// FinanceAccountIDs and FinanceAccountNames are the accounts the
	// statements went into, the names with the end of the account's
	// identifier.
	FinanceAccountIDs   []string `json:"financeAccountIds"`
	FinanceAccountNames []string `json:"financeAccountNames"`

	// ImportErrorMessage says why nothing was imported; empty when the
	// import worked.
	ImportErrorMessage string `json:"importErrorMessage,omitempty" graphapi:"nullable"`
}

// The keys of a finance source's cursor, which only its sync reads and
// writes: the provider's own cursor; whether the institution is waiting
// for the person to sign in again, while which the sync does not call the
// provider; whether the provider refused the credential itself, after
// which the sync never calls the provider again, since only deleting the
// finance source and linking the institution again mends it; whether
// transfers have been looked for across the source's whole history once,
// after which each sync looks at the last week only; and the version of the
// provider category mapping the agent's transactions were last judged by
// (finance.ProviderCategoryMappingVersion).
const (
	FinanceCursorProviderCursor                 = "providerCursor"
	FinanceCursorIsSignInRequired               = "isSignInRequired"
	FinanceCursorIsCredentialRefused            = "isCredentialRefused"
	FinanceCursorIsTransferHistoryDetected      = "isTransferHistoryDetected"
	FinanceCursorProviderCategoryMappingVersion = "providerCategoryMappingVersion"
)

// FinanceSourceSettings is what a finance source's Specification.Settings
// holds: the institution it reaches and the provider's reference for the
// link (Plaid's item id; empty for SimpleFIN).
type FinanceSourceSettings struct {
	InstitutionID     string `json:"institutionId,omitempty"`
	InstitutionName   string `json:"institutionName,omitempty"`
	ProviderReference string `json:"providerReference,omitempty"`
}

// FinanceSourceSettings reads a finance source's settings. Empty settings
// read as empty values, not as an error.
func (self *AgentKnowledgeSource) FinanceSourceSettings() (FinanceSourceSettings, error) {
	var settings FinanceSourceSettings
	trimmed := strings.TrimSpace(string(self.Specification.Settings))
	if trimmed == "" || trimmed == "null" {
		return settings, nil
	}
	err := json.Unmarshal([]byte(trimmed), &settings)
	return settings, err
}

// IsFinanceSignInRequired says the finance source is waiting for the
// person to sign in to the institution again.
func (self *AgentKnowledgeSource) IsFinanceSignInRequired() bool {
	isRequired, _ := self.Cursor[FinanceCursorIsSignInRequired].(bool)
	return isRequired
}

// IsFinanceCredentialRefused says the provider refused the finance
// source's credential, so it is not synced again.
func (self *AgentKnowledgeSource) IsFinanceCredentialRefused() bool {
	isRefused, _ := self.Cursor[FinanceCursorIsCredentialRefused].(bool)
	return isRefused
}

// FinanceAccountKind is what sort of account a finance account is, in the
// providers' shared vocabulary.
type FinanceAccountKind string

// The five kinds. Plaid's vocabulary, the richer of the two providers';
// SimpleFIN has none and its accounts are mostly "other".
const (
	FinanceAccountKindDepository FinanceAccountKind = "depository"
	FinanceAccountKindCredit     FinanceAccountKind = "credit"
	FinanceAccountKindLoan       FinanceAccountKind = "loan"
	FinanceAccountKindInvestment FinanceAccountKind = "investment"
	FinanceAccountKindOther      FinanceAccountKind = "other"
)

// IsValid says the kind is one of the five.
func (self FinanceAccountKind) IsValid() bool {
	switch self {
	case FinanceAccountKindDepository, FinanceAccountKindCredit, FinanceAccountKindLoan,
		FinanceAccountKindInvestment, FinanceAccountKindOther:
		return true
	}
	return false
}

// FinanceAccount is one account a finance source reports: checking,
// savings, a card, a brokerage account, a loan.
type FinanceAccount struct {
	ID       string `json:"id"`
	AgentID  string `json:"agentId"`
	SourceID string `json:"sourceId"`

	// ProviderAccountID is the provider's id for it, unique within its
	// finance source.
	ProviderAccountID string `json:"providerAccountId"`

	AccountName  string             `json:"accountName"`
	AccountMask  string             `json:"accountMask,omitempty" graphapi:"nullable"`
	AccountKind  FinanceAccountKind `json:"accountKind"`
	CurrencyCode string             `json:"currencyCode"`

	// CurrentBalance and AvailableBalance are decimals as the provider
	// reports them, empty when it does not say. For a card or a loan the
	// sign is the provider's: Plaid reports what is owed as positive,
	// most institutions behind SimpleFIN as negative.
	CurrentBalance   string     `json:"currentBalance,omitempty" graphapi:"nullable"`
	AvailableBalance string     `json:"availableBalance,omitempty" graphapi:"nullable"`
	BalanceAt        *time.Time `json:"balanceAt,omitempty" graphapi:"nullable"`

	// CreditLimitAmount is a card's credit limit as the provider gives it,
	// a positive decimal, empty when it gives none.
	CreditLimitAmount string `json:"creditLimitAmount,omitempty" graphapi:"nullable"`

	// ProviderMetadata is the provider's whole object for the account, as
	// it arrived.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// CreditLimitSource is where the credit limit a card's usage is measured
// against comes from.
type CreditLimitSource string

const (
	// CreditLimitSourceProvider is the limit the provider gave.
	CreditLimitSourceProvider CreditLimitSource = "provider"
	// CreditLimitSourceDerived is what is owed plus the credit still
	// available, for a card whose provider gave no limit.
	CreditLimitSourceDerived CreditLimitSource = "derived"
	// CreditLimitSourceUnknown is a card with neither, whose usage cannot
	// be measured.
	CreditLimitSourceUnknown CreditLimitSource = "unknown"
)

// IsValid says the source is one of the three.
func (self CreditLimitSource) IsValid() bool {
	switch self {
	case CreditLimitSourceProvider, CreditLimitSourceDerived, CreditLimitSourceUnknown:
		return true
	}
	return false
}

// FinanceTransaction is one transaction on a finance account.
type FinanceTransaction struct {
	ID               string `json:"id"`
	AgentID          string `json:"agentId"`
	FinanceAccountID string `json:"financeAccountId"`

	// ProviderTransactionID is the provider's id for it, unique within its
	// finance account.
	ProviderTransactionID string `json:"providerTransactionId"`

	// PostedOn is the day it posted, "2006-01-02".
	PostedOn     string     `json:"postedOn"`
	TransactedAt *time.Time `json:"transactedAt,omitempty" graphapi:"nullable"`

	// Amount is a decimal; negative is money leaving the account.
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currencyCode"`

	// Description and MerchantName are written by outsiders: whoever
	// charged the account, and the provider's cleaning of it.
	Description  string `json:"description"`
	MerchantName string `json:"merchantName,omitempty" graphapi:"nullable"`

	// The category the provider assigned: Plaid's primary and detailed
	// categories, or for SimpleFIN the merchant category code as
	// "mcc:5411" in the detailed one.
	ProviderCategoryPrimary  string `json:"providerCategoryPrimary,omitempty" graphapi:"nullable"`
	ProviderCategoryDetailed string `json:"providerCategoryDetailed,omitempty" graphapi:"nullable"`

	IsPending                    bool   `json:"isPending"`
	PendingProviderTransactionID string `json:"pendingProviderTransactionId,omitempty" graphapi:"nullable"`

	// ProviderMetadata is the provider's whole object for the transaction,
	// as it arrived.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty" graphapi:"nullable"`

	// SpendingCategoryID is the person's spending category for it, empty
	// while uncategorized, and CategorizedBy what gave it.
	// CategorizationConfidence is the decision model's confidence, a
	// decimal between 0 and 1, empty for anything else.
	SpendingCategoryID       string        `json:"spendingCategoryId,omitempty" graphapi:"nullable"`
	CategorizedBy            CategorizedBy `json:"categorizedBy,omitempty" graphapi:"nullable"`
	CategorizationConfidence string        `json:"categorizationConfidence,omitempty" graphapi:"nullable"`

	// IsTransfer says it moved money between the person's own accounts,
	// and so is neither spending nor income. TransferMarkedBy says what
	// decided that: the person, either way, after which nothing else may
	// change it; or what marked it a transfer, which clears its own mark
	// when it no longer holds. Empty when nothing did.
	IsTransfer       bool             `json:"isTransfer"`
	TransferMarkedBy TransferMarkedBy `json:"transferMarkedBy,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// TransferMarkedBy is what decided whether a finance transaction is a
// transfer.
type TransferMarkedBy string

// What may mark a transfer: the person (either way), a spending rule, the
// provider category mapping, or transfer detection pairing money out of
// one account with the same amount into another.
const (
	TransferMarkedByPerson                  TransferMarkedBy = "person"
	TransferMarkedBySpendingRule            TransferMarkedBy = "spending_rule"
	TransferMarkedByProviderCategoryMapping TransferMarkedBy = "provider_category_mapping"
	TransferMarkedByDetection               TransferMarkedBy = "detection"
)

// IsValid says it is one of the four.
func (self TransferMarkedBy) IsValid() bool {
	switch self {
	case TransferMarkedByPerson, TransferMarkedBySpendingRule, TransferMarkedByProviderCategoryMapping, TransferMarkedByDetection:
		return true
	}
	return false
}

// FinanceSpendingSummaryGroupBy is what a spending summary groups by.
type FinanceSpendingSummaryGroupBy string

// The ways a spending summary groups: by the provider's category, by the
// person's spending category, by merchant (the description when there is
// no merchant), by month, or by finance account.
const (
	FinanceSpendingSummaryGroupByProviderCategory FinanceSpendingSummaryGroupBy = "providerCategory"
	FinanceSpendingSummaryGroupBySpendingCategory FinanceSpendingSummaryGroupBy = "spendingCategory"
	FinanceSpendingSummaryGroupByMerchant         FinanceSpendingSummaryGroupBy = "merchant"
	FinanceSpendingSummaryGroupByMonth            FinanceSpendingSummaryGroupBy = "month"
	FinanceSpendingSummaryGroupByFinanceAccount   FinanceSpendingSummaryGroupBy = "financeAccount"
)

// IsValid says the grouping is one of the five.
func (self FinanceSpendingSummaryGroupBy) IsValid() bool {
	switch self {
	case FinanceSpendingSummaryGroupByProviderCategory, FinanceSpendingSummaryGroupBySpendingCategory,
		FinanceSpendingSummaryGroupByMerchant, FinanceSpendingSummaryGroupByMonth, FinanceSpendingSummaryGroupByFinanceAccount:
		return true
	}
	return false
}

// FinanceSpendingSummaryRow is one group's money out and money in, in one
// currency, transfers left out. Both are positive decimals.
type FinanceSpendingSummaryRow struct {
	// GroupKey is what the rows were grouped by: the provider category,
	// the spending category's id, the merchant, the month as "2006-01" or
	// the finance account's id. Empty for transactions with none.
	// GroupLabel is the name to show for an id: the spending category's or
	// the finance account's name; for the others it is the key.
	GroupKey   string `json:"groupKey"`
	GroupLabel string `json:"groupLabel"`

	CurrencyCode            string `json:"currencyCode"`
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}
