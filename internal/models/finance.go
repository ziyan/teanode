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
// the dashboard or the command line, the attachment of a message the
// person pointed at, or transaction rows sent instead of a file (what the
// agent read off screenshots, with ImportTransactions).
const (
	StatementImportOriginMail            StatementImportOrigin = "mail"
	StatementImportOriginUpload          StatementImportOrigin = "upload"
	StatementImportOriginMessage         StatementImportOrigin = "message"
	StatementImportOriginTransactionRows StatementImportOrigin = "transaction_rows"
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

// InstitutionName is the institution the finance account is at: the one
// its finance source names, else the one its provider metadata names.
// SimpleFIN gives each account its institution ("org"), since one of its
// finance sources can reach several, and the finance source itself keeps
// none; an imported statement names the institution that wrote it
// ("institutionOrganization"). The source may be nil.
func (self *FinanceAccount) InstitutionName(source *AgentKnowledgeSource) string {
	if source != nil {
		if settings, _ := source.FinanceSourceSettings(); settings.InstitutionName != "" {
			return settings.InstitutionName
		}
	}
	if len(self.ProviderMetadata) == 0 {
		return ""
	}
	var metadata struct {
		Organization struct {
			Name string `json:"name"`
		} `json:"org"`
		InstitutionOrganization string `json:"institutionOrganization"`
	}
	if json.Unmarshal(self.ProviderMetadata, &metadata) != nil {
		return ""
	}
	if name := strings.TrimSpace(metadata.Organization.Name); name != "" {
		return name
	}
	return strings.TrimSpace(metadata.InstitutionOrganization)
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
	// while uncategorized, and CategorizedBy what gave it. It is a
	// transfer between the person's own accounts, neither spending nor
	// income, exactly when the spending category is the agent's transfer
	// category (SpendingCategory.IsTransfer); nothing else says so.
	// CategorizationConfidence is the decision model's confidence, a
	// decimal between 0 and 1, empty for anything else.
	SpendingCategoryID       string        `json:"spendingCategoryId,omitempty" graphapi:"nullable"`
	CategorizedBy            CategorizedBy `json:"categorizedBy,omitempty" graphapi:"nullable"`
	CategorizationConfidence string        `json:"categorizationConfidence,omitempty" graphapi:"nullable"`

	// DuplicateOfTransactionID is the counted copy this one mirrors: the
	// same charge reported again on another investment account of the
	// same Plaid finance source, left out of every total like a transfer.
	// Empty when it is counted. DuplicateDecidedBy says what decided:
	// mirror detection, which marks and clears copies after each sync, or
	// the person, whose "count this one" detection then leaves alone.
	// Empty when nothing did.
	DuplicateOfTransactionID string             `json:"duplicateOfTransactionId,omitempty" graphapi:"nullable"`
	DuplicateDecidedBy       DuplicateDecidedBy `json:"duplicateDecidedBy,omitempty" graphapi:"nullable"`

	// Annotation is what the person or their agent wrote on it, and
	// AnnotatedBy which of them; both empty when nobody has. The agent
	// never overwrites the person's.
	Annotation  string      `json:"annotation,omitempty" graphapi:"nullable"`
	AnnotatedBy AnnotatedBy `json:"annotatedBy,omitempty" graphapi:"nullable"`

	// ReceiptCount is how many receipts are matched to it.
	ReceiptCount int `json:"receiptCount"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// AnnotatedBy is who wrote a finance transaction's annotation.
type AnnotatedBy string

// The person, or their agent acting for them.
const (
	AnnotatedByPerson AnnotatedBy = "person"
	AnnotatedByAgent  AnnotatedBy = "agent"
)

// IsValid says the author is one of the two.
func (self AnnotatedBy) IsValid() bool {
	return self == AnnotatedByPerson || self == AnnotatedByAgent
}

// DuplicateDecidedBy is what decided whether a finance transaction is a
// mirrored copy of another.
type DuplicateDecidedBy string

// What may decide it: mirror detection, which finds the same day, amount,
// currency and description on two or more investment accounts of one
// Plaid finance source and keeps one of them counted; or the person, who
// says a copy is real and counts, after which detection leaves it alone.
const (
	DuplicateDecidedByMirrorDetection DuplicateDecidedBy = "mirror_detection"
	DuplicateDecidedByPerson          DuplicateDecidedBy = "person"
)

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

// ReceiptSourceKind is where a finance receipt was read from.
type ReceiptSourceKind string

// A message stored here (the mail row, which keeps its id as the person
// files it from folder to folder), a Gmail message the agent read through
// the Gmail skill, or a photo or PDF uploaded as an agent attachment.
const (
	ReceiptSourceKindMail         ReceiptSourceKind = "mail"
	ReceiptSourceKindGmailMessage ReceiptSourceKind = "gmail_message"
	ReceiptSourceKindAttachment   ReceiptSourceKind = "attachment"
)

// IsValid says the kind is one of the three.
func (self ReceiptSourceKind) IsValid() bool {
	switch self {
	case ReceiptSourceKindMail, ReceiptSourceKindGmailMessage, ReceiptSourceKindAttachment:
		return true
	}
	return false
}

// ReceiptLineKind is what one printed line of a receipt is.
type ReceiptLineKind string

// An item bought, a discount (negative, as printed), a tax, a fee (a bag,
// a delivery) or a tip.
const (
	ReceiptLineKindItem     ReceiptLineKind = "item"
	ReceiptLineKindDiscount ReceiptLineKind = "discount"
	ReceiptLineKindTax      ReceiptLineKind = "tax"
	ReceiptLineKindFee      ReceiptLineKind = "fee"
	ReceiptLineKindTip      ReceiptLineKind = "tip"
)

// IsValid says the kind is one of the five.
func (self ReceiptLineKind) IsValid() bool {
	switch self {
	case ReceiptLineKindItem, ReceiptLineKindDiscount, ReceiptLineKindTax, ReceiptLineKindFee, ReceiptLineKindTip:
		return true
	}
	return false
}

// ReceiptCheckState is whether a receipt's lines add up to its printed
// totals.
type ReceiptCheckState string

// Balanced: every line comes to the total, to the cent, and when a
// subtotal is printed the items and discounts come to it, with the fees
// or without them (a receipt prints its fees before the subtotal or after
// it). Unbalanced: they miss, by CheckDifferenceAmount.
const (
	ReceiptCheckStateBalanced   ReceiptCheckState = "balanced"
	ReceiptCheckStateUnbalanced ReceiptCheckState = "unbalanced"
)

// IsValid says the state is one of the two.
func (self ReceiptCheckState) IsValid() bool {
	return self == ReceiptCheckStateBalanced || self == ReceiptCheckStateUnbalanced
}

// ReceiptMatchSource is what matched a receipt to a finance transaction.
type ReceiptMatchSource string

// The receipt matcher, which matches only an exact amount that is the one
// candidate, or the person (or their agent asked by them), whose match the
// matcher never removes or replaces.
const (
	ReceiptMatchSourceReceiptMatcher ReceiptMatchSource = "receipt_matcher"
	ReceiptMatchSourcePerson         ReceiptMatchSource = "person"
)

// IsValid says the source is one of the two.
func (self ReceiptMatchSource) IsValid() bool {
	return self == ReceiptMatchSourceReceiptMatcher || self == ReceiptMatchSourcePerson
}

// FinanceReceipt is one merchant's printed or emailed record of one
// purchase or order, stored line by line exactly as printed, checked
// against its own totals, and matched to the charges it explains. Amounts
// are decimals, positive for what was paid. The finance transaction, not
// the receipt, is what totals count.
type FinanceReceipt struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// ReceiptSourceKind says where it was read from, and exactly one of
	// MailID, GmailMessageID and AgentAttachmentID is set, the one it
	// names.
	ReceiptSourceKind ReceiptSourceKind `json:"receiptSourceKind"`
	MailID            string            `json:"mailId,omitempty" graphapi:"nullable"`
	GmailMessageID    string            `json:"gmailMessageId,omitempty" graphapi:"nullable"`
	AgentAttachmentID string            `json:"agentAttachmentId,omitempty" graphapi:"nullable"`

	// MailboxItemID is where the message is in the person's mailboxes now,
	// for opening it; filled when the receipt is read for the person, not
	// stored, and empty when the message is gone.
	MailboxItemID string `json:"mailboxItemId,omitempty" graphapi:"nullable"`

	// MerchantName and MerchantReceiptNumber (an order or receipt number)
	// are as the merchant wrote them.
	MerchantName          string `json:"merchantName"`
	MerchantReceiptNumber string `json:"merchantReceiptNumber,omitempty" graphapi:"nullable"`

	// PurchasedOn is the day, "2006-01-02", and PurchasedAt the moment
	// when the receipt prints a time.
	PurchasedOn string     `json:"purchasedOn,omitempty" graphapi:"nullable"`
	PurchasedAt *time.Time `json:"purchasedAt,omitempty" graphapi:"nullable"`

	CurrencyCode string `json:"currencyCode"`

	// SubtotalAmount is empty when the receipt prints none.
	SubtotalAmount string `json:"subtotalAmount,omitempty" graphapi:"nullable"`
	TotalAmount    string `json:"totalAmount"`

	// PaymentAccountMask is the last digits of the card or account the
	// receipt says paid, empty when it does not say.
	PaymentAccountMask string `json:"paymentAccountMask,omitempty" graphapi:"nullable"`

	// ReceiptCheckState says whether the lines add up, and
	// CheckDifferenceAmount by how much they miss: what the lines come to
	// less what is printed, zero when balanced.
	ReceiptCheckState     ReceiptCheckState `json:"receiptCheckState"`
	CheckDifferenceAmount string            `json:"checkDifferenceAmount"`

	// IsFeeAfterSubtotal says the receipt prints its fees after the
	// subtotal, with the taxes and tips, rather than among the items the
	// subtotal adds up; worked out from the lines each time the receipt
	// is read (finance.IsReceiptFeeAfterSubtotal), not stored.
	IsFeeAfterSubtotal bool `json:"isFeeAfterSubtotal"`

	ReceiptLines   []*FinanceReceiptLine  `json:"receiptLines"`
	ReceiptMatches []*FinanceReceiptMatch `json:"receiptMatches"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// FinanceReceiptLine is one printed line of a finance receipt.
type FinanceReceiptLine struct {
	ID string `json:"id"`

	// LineNumber is its place on the receipt, from 1.
	LineNumber      int             `json:"lineNumber"`
	ReceiptLineKind ReceiptLineKind `json:"receiptLineKind"`
	Description     string          `json:"description"`

	// Quantity, QuantityUnit ("lb", "kg") and UnitPriceAmount are what the
	// line prints of them, empty when it prints none.
	Quantity        string `json:"quantity,omitempty" graphapi:"nullable"`
	QuantityUnit    string `json:"quantityUnit,omitempty" graphapi:"nullable"`
	UnitPriceAmount string `json:"unitPriceAmount,omitempty" graphapi:"nullable"`

	// LineAmount is signed as printed: a discount is negative.
	LineAmount string `json:"lineAmount"`

	// TaxClassCode is the receipt's own mark: on an item or a discount the
	// tax it falls under, on a tax line the mark it covers. Empty when the
	// receipt does not say.
	TaxClassCode string `json:"taxClassCode,omitempty" graphapi:"nullable"`

	// DiscountedLineNumber and DiscountedLineID are a discount's item:
	// written by its number, kept by its id.
	DiscountedLineNumber int    `json:"discountedLineNumber,omitempty" graphapi:"nullable"`
	DiscountedLineID     string `json:"discountedLineId,omitempty" graphapi:"nullable"`

	// SpendingCategoryID is reserved for splitting a charge across
	// spending categories by its receipt; nothing writes it yet.
	SpendingCategoryID string `json:"spendingCategoryId,omitempty" graphapi:"nullable"`
}

// FinanceReceiptMatch is a link between one finance receipt and one
// finance transaction: how much of the transaction the receipt explains,
// a positive decimal, and what made the link.
type FinanceReceiptMatch struct {
	ReceiptID            string             `json:"receiptId"`
	FinanceTransactionID string             `json:"financeTransactionId"`
	MatchedAmount        string             `json:"matchedAmount"`
	ReceiptMatchSource   ReceiptMatchSource `json:"receiptMatchSource"`

	// MatchConfidence is the matcher's confidence, a decimal between 0
	// and 1; empty for the person's.
	MatchConfidence string    `json:"matchConfidence,omitempty" graphapi:"nullable"`
	CreatedAt       time.Time `json:"createdAt"`
}
