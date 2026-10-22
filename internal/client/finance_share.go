package client

import (
	"context"
	"time"
)

// Finance accounts shared with other people: the operations behind
// `teanode finance share`, `unshare`, `shares` and the --person reads. The
// agent's finance tool has no operation for any of them, and these documents
// are not among FinanceDocuments, which is what that tool runs from.

// FinanceShare is one share of one of the caller's finance accounts.
type FinanceShare struct {
	ID                string    `json:"id"`
	FinanceAccountID  string    `json:"financeAccountId"`
	AccountName       string    `json:"accountName"`
	AccountMask       string    `json:"accountMask"`
	GranteeUserID     *string   `json:"granteeUserId"`
	GranteeUsername   string    `json:"granteeUsername"`
	GranteeName       string    `json:"granteeName"`
	GranteeCircleID   *string   `json:"granteeCircleId"`
	GranteeCircleName string    `json:"granteeCircleName"`
	FinanceShareLevel string    `json:"financeShareLevel"`
	CreatedAt         time.Time `json:"createdAt"`
}

// SharedCreditUsage is what a shared credit card owes against its limit.
type SharedCreditUsage struct {
	OwedAmount        string   `json:"owedAmount,omitempty"`
	CreditLimitAmount string   `json:"creditLimitAmount,omitempty"`
	CreditLimitSource string   `json:"creditLimitSource"`
	UsageShare        *float64 `json:"usageShare,omitempty"`
}

// SharedFinanceAccount is somebody else's finance account as the level it is
// shared at shows it.
type SharedFinanceAccount struct {
	FinanceAccountID  string             `json:"financeAccountId"`
	OwnerUserID       string             `json:"ownerUserId"`
	OwnerUsername     string             `json:"ownerUsername"`
	OwnerName         string             `json:"ownerName"`
	FinanceShareLevel string             `json:"financeShareLevel"`
	AccountName       string             `json:"accountName"`
	AccountMask       string             `json:"accountMask,omitempty"`
	AccountKind       string             `json:"accountKind"`
	CurrencyCode      string             `json:"currencyCode"`
	CurrentBalance    string             `json:"currentBalance,omitempty"`
	BalanceAt         *time.Time         `json:"balanceAt,omitempty"`
	CreditUsage       *SharedCreditUsage `json:"creditUsage"`
}

// SharedFinanceSpendingRow is one month of one spending category of a shared
// account, in one currency.
type SharedFinanceSpendingRow struct {
	SpendingMonth           string `json:"spendingMonth"`
	SpendingCategoryName    string `json:"spendingCategoryName"`
	CurrencyCode            string `json:"currencyCode"`
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// SharedFinanceTransaction is a transaction of a shared account as a reader at
// the transactions level sees it.
type SharedFinanceTransaction struct {
	ID                   string `json:"id"`
	PostedOn             string `json:"postedOn"`
	Amount               string `json:"amount"`
	CurrencyCode         string `json:"currencyCode"`
	MerchantName         string `json:"merchantName,omitempty"`
	SpendingCategoryName string `json:"spendingCategoryName,omitempty"`
}

// SharedFinanceTransactionPage is one page of a shared account's
// transactions, and the cursor for the next, empty on the last.
type SharedFinanceTransactionPage struct {
	FinanceTransactions []*SharedFinanceTransaction `json:"financeTransactions"`
	NextCursor          string                      `json:"nextCursor,omitempty"`
	TotalCount          int                         `json:"totalCount"`
}

const (
	financeShareFields = `id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel createdAt`

	DocumentListFinanceAccountShares = `query ($financeAccountId: String) {
  ListFinanceAccountShares(financeAccountId: $financeAccountId) { ` + financeShareFields + ` }
}`
	DocumentShareFinanceAccount = `mutation ($financeAccountId: String!, $granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
  ShareFinanceAccount(financeAccountId: $financeAccountId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) { ` + financeShareFields + ` }
}`
	DocumentUnshareFinanceAccount     = `mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }`
	DocumentListSharedFinanceAccounts = `query ($ownerUserId: String) {
  ListSharedFinanceAccounts(ownerUserId: $ownerUserId) {
    financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
    creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
  }
}`
	DocumentListSharedFinanceSpending = `query ($financeAccountId: String!, $from: String, $to: String) {
  ListSharedFinanceSpending(financeAccountId: $financeAccountId, from: $from, to: $to) {
    spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
  }
}`
	DocumentListSharedFinanceTransactions = `query ($financeAccountId: String!, $from: String, $to: String, $limit: Int, $after: String, $offset: Int) {
  ListSharedFinanceTransactions(financeAccountId: $financeAccountId, from: $from, to: $to, limit: $limit, after: $after, offset: $offset) {
    nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
  }
}`
)

// FinanceShareDocuments is every document here by the operation it runs, for
// the test that checks each against the schema.
var FinanceShareDocuments = map[string]string{
	"ListFinanceAccountShares": DocumentListFinanceAccountShares, "ShareFinanceAccount": DocumentShareFinanceAccount,
	"UnshareFinanceAccount": DocumentUnshareFinanceAccount, "ListSharedFinanceAccounts": DocumentListSharedFinanceAccounts,
	"ListSharedFinanceSpending": DocumentListSharedFinanceSpending, "ListSharedFinanceTransactions": DocumentListSharedFinanceTransactions,
}

// ListFinanceAccountShares are the shares the caller has made, of one finance
// account when its identifier is given.
func ListFinanceAccountShares(ctx context.Context, connection *Client, financeAccountId string) ([]*FinanceShare, error) {
	variables := map[string]any{"financeAccountId": nil}
	if financeAccountId != "" {
		variables["financeAccountId"] = financeAccountId
	}
	var result struct {
		ListFinanceAccountShares []*FinanceShare `json:"ListFinanceAccountShares"`
	}
	if err := connection.Execute(ctx, DocumentListFinanceAccountShares, variables, &result); err != nil {
		return nil, err
	}
	return result.ListFinanceAccountShares, nil
}

// ShareFinanceAccount shares one of the caller's finance accounts with a
// person or a circle, given by identifier (one of the two), or changes the
// level of a share already made.
func ShareFinanceAccount(ctx context.Context, connection *Client, financeAccountId, granteeUserId, granteeCircleId, financeShareLevel string) (*FinanceShare, error) {
	variables := map[string]any{
		"financeAccountId": financeAccountId, "financeShareLevel": financeShareLevel, "granteeUserId": nil, "granteeCircleId": nil,
	}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		ShareFinanceAccount *FinanceShare `json:"ShareFinanceAccount"`
	}
	if err := connection.Execute(ctx, DocumentShareFinanceAccount, variables, &result); err != nil {
		return nil, err
	}
	return result.ShareFinanceAccount, nil
}

// UnshareFinanceAccount takes a share back.
func UnshareFinanceAccount(ctx context.Context, connection *Client, shareId string) error {
	var result struct {
		UnshareFinanceAccount bool `json:"UnshareFinanceAccount"`
	}
	return connection.Execute(ctx, DocumentUnshareFinanceAccount, map[string]any{"shareId": shareId}, &result)
}

// ListSharedFinanceAccounts are the finance accounts other people share with
// the caller, of one person when their identifier is given.
func ListSharedFinanceAccounts(ctx context.Context, connection *Client, ownerUserId string) ([]*SharedFinanceAccount, error) {
	variables := map[string]any{"ownerUserId": nil}
	if ownerUserId != "" {
		variables["ownerUserId"] = ownerUserId
	}
	var result struct {
		ListSharedFinanceAccounts []*SharedFinanceAccount `json:"ListSharedFinanceAccounts"`
	}
	if err := connection.Execute(ctx, DocumentListSharedFinanceAccounts, variables, &result); err != nil {
		return nil, err
	}
	return result.ListSharedFinanceAccounts, nil
}

// ListSharedFinanceSpending is a shared account's money out and in by month
// and spending category, for the days from and to (either may be empty).
func ListSharedFinanceSpending(ctx context.Context, connection *Client, financeAccountId, from, to string) ([]*SharedFinanceSpendingRow, error) {
	variables := map[string]any{"financeAccountId": financeAccountId, "from": nil, "to": nil}
	if from != "" {
		variables["from"] = from
	}
	if to != "" {
		variables["to"] = to
	}
	var result struct {
		ListSharedFinanceSpending []*SharedFinanceSpendingRow `json:"ListSharedFinanceSpending"`
	}
	if err := connection.Execute(ctx, DocumentListSharedFinanceSpending, variables, &result); err != nil {
		return nil, err
	}
	return result.ListSharedFinanceSpending, nil
}

// ListSharedFinanceTransactions is a page of a shared account's transactions,
// newest first. A limit of zero is the server's default; an offset passes over
// that many transactions first, from the cursor when one is given.
func ListSharedFinanceTransactions(ctx context.Context, connection *Client, financeAccountId, from, to string, limit int, after string, offset int) (*SharedFinanceTransactionPage, error) {
	variables := map[string]any{"financeAccountId": financeAccountId, "from": nil, "to": nil, "limit": nil, "after": nil, "offset": nil}
	if from != "" {
		variables["from"] = from
	}
	if to != "" {
		variables["to"] = to
	}
	if limit > 0 {
		variables["limit"] = limit
	}
	if after != "" {
		variables["after"] = after
	}
	if offset > 0 {
		variables["offset"] = offset
	}
	var result struct {
		ListSharedFinanceTransactions *SharedFinanceTransactionPage `json:"ListSharedFinanceTransactions"`
	}
	if err := connection.Execute(ctx, DocumentListSharedFinanceTransactions, variables, &result); err != nil {
		return nil, err
	}
	return result.ListSharedFinanceTransactions, nil
}
