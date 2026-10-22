package models

import "time"

// FinanceShareLevel is how much of a finance account somebody may see. A
// person shares a finance account with a circle or with one person in a
// circle they share, at one of these three. Each level adds to the one before.
//
// Never at any level: the account's receipts and their images, what the person
// or their agent wrote on a transaction, the provider's own data, paystubs, tax
// forms, finance sources and their credentials, spending rules, budgets, and
// whether the institution waits for the person to sign in.
type FinanceShareLevel string

// The levels, from least to most.
const (
	// FinanceShareLevelBalance is the account's name, mask, kind and currency,
	// its current balance and the day of it, and for a credit card what is
	// owed against its limit. No transaction.
	FinanceShareLevelBalance FinanceShareLevel = "balance"
	// FinanceShareLevelSpending adds the account's money out and money in by
	// month and spending category, with no merchant and no description.
	FinanceShareLevelSpending FinanceShareLevel = "spending"
	// FinanceShareLevelTransactions adds each transaction's day, amount,
	// merchant name and spending category.
	FinanceShareLevelTransactions FinanceShareLevel = "transactions"
)

// IsGrantable reports whether a share may be made at this level.
func (self FinanceShareLevel) IsGrantable() bool {
	return self.Rank() > 0
}

// Rank orders the levels; zero is no access at all.
func (self FinanceShareLevel) Rank() int {
	switch self {
	case FinanceShareLevelBalance:
		return 1
	case FinanceShareLevelSpending:
		return 2
	case FinanceShareLevelTransactions:
		return 3
	}
	return 0
}

// AtLeast reports whether this level reaches the other.
func (self FinanceShareLevel) AtLeast(other FinanceShareLevel) bool {
	return self.Rank() >= other.Rank() && self.Rank() > 0
}

// HighestFinanceShareLevel is the greater of two levels, so that a person
// reached by both a direct share and a circle's takes the better of them.
func HighestFinanceShareLevel(first, second FinanceShareLevel) FinanceShareLevel {
	if second.Rank() > first.Rank() {
		return second
	}
	return first
}

// FinanceShare gives one person, or every member of one circle, a level of
// access to one finance account. Exactly one of GranteeUserID and
// GranteeCircleID is set.
type FinanceShare struct {
	ID               string `json:"id"`
	FinanceAccountID string `json:"financeAccountId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	FinanceShareLevel FinanceShareLevel `json:"financeShareLevel"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// The rest is filled in by the listings, for a screen that says which
	// account it is, whose it is and who it is shared with.
	OwnerUserID       string `json:"ownerUserId,omitempty"`
	OwnerUsername     string `json:"ownerUsername,omitempty"`
	OwnerName         string `json:"ownerName,omitempty"`
	OwnerUser         *User  `json:"-"`
	AccountName       string `json:"accountName,omitempty"`
	AccountMask       string `json:"accountMask,omitempty"`
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// ReachedFinanceAccount is a finance account as a person other than its owner
// reaches it: the account as stored and the level at which it is shared with
// them. The account holds more than any level shows; the one place that
// builds what a reader sees from it is the API's describeSharedFinanceAccount.
type ReachedFinanceAccount struct {
	Account *FinanceAccount

	// ProviderKind is what the account's finance source signs in through,
	// which says which way a credit card's balance is signed.
	ProviderKind string

	FinanceShareLevel FinanceShareLevel

	OwnerUserID   string
	OwnerUsername string
	OwnerName     string
}

// SharedFinanceSpendingRow is one month of one spending category of a shared
// finance account, in one currency. The name is the owner's own for the
// category and empty for transactions that have none. Transfers between the
// owner's accounts and mirrored copies are left out, as in the owner's own
// totals.
type SharedFinanceSpendingRow struct {
	// SpendingMonth is "2006-01".
	SpendingMonth        string `json:"spendingMonth"`
	SpendingCategoryName string `json:"spendingCategoryName"`
	CurrencyCode         string `json:"currencyCode"`

	// MoneyOut and MoneyIn are decimals, both positive.
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// SharedFinanceTransaction is a transaction of a shared finance account as a
// reader at the transactions level sees it. It has no description, no
// provider data, no annotation and no receipt, because the type has no field
// for them.
type SharedFinanceTransaction struct {
	ID string `json:"id"`

	// PostedOn is the day it posted, "2006-01-02".
	PostedOn string `json:"postedOn"`

	// Amount is a decimal; negative is money leaving the account.
	Amount       string `json:"amount"`
	CurrencyCode string `json:"currencyCode"`

	// MerchantName is written by outsiders; empty when the provider gave none.
	MerchantName         string `json:"merchantName,omitempty" graphapi:"nullable"`
	SpendingCategoryName string `json:"spendingCategoryName,omitempty" graphapi:"nullable"`
}
