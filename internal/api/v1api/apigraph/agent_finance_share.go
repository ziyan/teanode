package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing a finance account: with one person or one circle, at a level. Part
// of the finance area, but not of FinanceQuery or FinanceMutation, because the
// agent has no tool operation for any of it: what a finance account holds is
// the person's, and what others share is theirs to read, not their agents' to
// send to a model provider. docs/subsystems/finance.md ("Sharing") says what
// each level shows.
//
// Who may read which account, and at what level, is decided in one place, the
// database's ReachFinanceAccount, and what a level shows is made in one place
// for each kind of answer: describeSharedFinanceAccount here, and the types
// the database's ListSharedFinanceSpending and ListSharedFinanceTransactions
// return, which have no field for what a level does not show. Sharing is the
// account owner's alone.

// FinanceShareQuery reads who a finance account is shared with, and what is
// shared with the caller. Needs agent:use.
type FinanceShareQuery interface {
	// The shares the caller has made of their finance accounts, oldest first,
	// with who each is for; of one finance account when financeAccountId is
	// given. Not available to the agent.
	ListFinanceAccountShares(ctx context.Context, arguments ListFinanceAccountSharesArguments) ([]*FinanceShareView, error)

	// The finance accounts other people share with the caller, oldest first,
	// each as its level shows it: the account's name, kind and balance, and
	// for a credit card what is owed against its limit. Of one person when
	// ownerUserId is given. A person whose account, agent or circle is gone,
	// or who the caller no longer shares a circle with, is not there. Not
	// available to the agent.
	ListSharedFinanceAccounts(ctx context.Context, arguments ListSharedFinanceAccountsArguments) ([]*SharedFinanceAccountView, error)

	// A shared finance account's money out and in by month and spending
	// category, newest month first, in each currency, without merchants or
	// descriptions; for the days from and to ("2006-01-02", either may be
	// left out; at the spending level they are widened to whole months, so a
	// range of single days cannot be used to find one charge). Needs the
	// spending level or the transactions level. Not available to the agent.
	ListSharedFinanceSpending(ctx context.Context, arguments ListSharedFinanceSpendingArguments) ([]*models.SharedFinanceSpendingRow, error)

	// A page of a shared finance account's transactions, newest first: the
	// day, amount, merchant name and spending category of each. Needs the
	// transactions level. Not available to the agent.
	ListSharedFinanceTransactions(ctx context.Context, arguments ListSharedFinanceTransactionsArguments) (*SharedFinanceTransactionPageView, error)
}

// FinanceShareMutation shares one of the caller's finance accounts.
type FinanceShareMutation interface {
	// Share one of the caller's finance accounts with one person or one
	// circle at a level: balance, spending or transactions. The person must
	// share a circle with the caller, and the caller must be in the circle.
	// Sharing again changes the level. Sharing an account with somebody it
	// is not already shared with, or at a higher level, is done where a
	// person taps, not from a command line or an API token. Not available to
	// the agent.
	ShareFinanceAccount(ctx context.Context, arguments ShareFinanceAccountArguments) (*FinanceShareView, error)

	// Take one of the caller's shares of a finance account back. Open to
	// every sign-in. Not available to the agent.
	UnshareFinanceAccount(ctx context.Context, arguments UnshareFinanceAccountArguments) (bool, error)
}

// FinanceShareView is one share of a finance account.
type FinanceShareView struct {
	ID               string `json:"id"`
	FinanceAccountID string `json:"financeAccountId"`
	AccountName      string `json:"accountName"`
	AccountMask      string `json:"accountMask"`

	// Who it is shared with: a person or a circle, never both.
	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// balance, spending or transactions
	FinanceShareLevel string `json:"financeShareLevel"`

	CreatedAt time.Time `json:"createdAt"`
}

func describeFinanceShare(share *models.FinanceShare) *FinanceShareView {
	view := &FinanceShareView{
		ID: share.ID, FinanceAccountID: share.FinanceAccountID, AccountName: share.AccountName, AccountMask: share.AccountMask,
		FinanceShareLevel: string(share.FinanceShareLevel), CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

// SharedFinanceAccountView is somebody else's finance account as the level it
// is shared at shows it. Everything here is shown at every level; spending and
// transactions are read with ListSharedFinanceSpending and
// ListSharedFinanceTransactions.
type SharedFinanceAccountView struct {
	FinanceAccountID string `json:"financeAccountId"`

	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	// balance, spending or transactions: the highest level that reaches the
	// caller.
	FinanceShareLevel string `json:"financeShareLevel"`

	AccountName  string                    `json:"accountName"`
	AccountMask  string                    `json:"accountMask,omitempty" graphapi:"nullable"`
	AccountKind  models.FinanceAccountKind `json:"accountKind"`
	CurrencyCode string                    `json:"currencyCode"`

	// CurrentBalance is as the provider reports it, in the account's own
	// currency, and BalanceAt the day of it; empty when it does not say.
	CurrentBalance string     `json:"currentBalance,omitempty" graphapi:"nullable"`
	BalanceAt      *time.Time `json:"balanceAt,omitempty" graphapi:"nullable"`

	// CreditUsage is what a credit card owes against its limit; null for any
	// other account and for a credit line from an imported statement.
	CreditUsage *SharedCreditUsageView `json:"creditUsage" graphapi:"nullable"`
}

// SharedCreditUsageView is what a shared credit card owes against its credit
// limit, in the card's own currency.
type SharedCreditUsageView struct {
	// OwedAmount is never negative: a card paid past its balance owes
	// nothing. Empty when the provider gave no balance.
	OwedAmount string `json:"owedAmount,omitempty" graphapi:"nullable"`

	// CreditLimitAmount is the limit usage is measured against, and
	// CreditLimitSource where it comes from: the provider, or derived as what
	// is owed plus the credit still available.
	CreditLimitAmount string                   `json:"creditLimitAmount,omitempty" graphapi:"nullable"`
	CreditLimitSource models.CreditLimitSource `json:"creditLimitSource"`

	// UsageShare is OwedAmount over CreditLimitAmount, 0.25 for a quarter of
	// the credit used; empty when either is unknown.
	UsageShare *float64 `json:"usageShare,omitempty" graphapi:"nullable"`
}

// describeSharedFinanceAccount is the one place a reader's view of somebody
// else's finance account is made. It copies the fields every level shows and
// computes the card's usage; the provider's data, the available balance, the
// finance source and whether the institution waits for a sign-in are not
// copied.
func describeSharedFinanceAccount(reached *models.ReachedFinanceAccount) (*SharedFinanceAccountView, error) {
	account := reached.Account
	view := &SharedFinanceAccountView{
		FinanceAccountID: account.ID, OwnerUserID: reached.OwnerUserID, OwnerUsername: reached.OwnerUsername, OwnerName: reached.OwnerName,
		FinanceShareLevel: string(reached.FinanceShareLevel),
		AccountName:       account.AccountName, AccountMask: account.AccountMask, AccountKind: account.AccountKind,
		CurrencyCode: account.CurrencyCode, CurrentBalance: account.CurrentBalance, BalanceAt: account.BalanceAt,
	}
	if account.AccountKind == models.FinanceAccountKindCredit && !finance.IsStatementCreditLine(account.ProviderMetadata) {
		usage, err := creditCardUsageOf(account.CurrentBalance, account.AvailableBalance, account.CreditLimitAmount,
			finance.IsOwedBalancePositive(finance.ProviderKind(reached.ProviderKind)))
		if err != nil {
			return nil, err
		}
		view.CreditUsage = &SharedCreditUsageView{CreditLimitSource: usage.creditLimitSource}
		if usage.owedAmount != nil {
			view.CreditUsage.OwedAmount = finance.FormatAmount(usage.owedAmount)
		}
		if usage.creditLimitAmount != nil {
			view.CreditUsage.CreditLimitAmount = finance.FormatAmount(usage.creditLimitAmount)
			if usage.owedAmount != nil {
				view.CreditUsage.UsageShare = usageShare(usage.owedAmount, usage.creditLimitAmount)
			}
		}
	}
	return view, nil
}

// SharedFinanceTransactionPageView is one page of a shared account's
// transactions, the cursor for the next, empty on the last, and how many
// transactions the account has in the days asked for.
type SharedFinanceTransactionPageView struct {
	FinanceTransactions []*models.SharedFinanceTransaction `json:"financeTransactions"`
	NextCursor          string                             `json:"nextCursor,omitempty" graphapi:"nullable"`
	TotalCount          int                                `json:"totalCount"`
}

type ListFinanceAccountSharesArguments struct {
	// Only the shares of this finance account
	FinanceAccountID string `json:"financeAccountId" graphapi:"nullable"`
}

type ListSharedFinanceAccountsArguments struct {
	// Only the accounts of this person
	OwnerUserID string `json:"ownerUserId" graphapi:"nullable"`
}

type ListSharedFinanceSpendingArguments struct {
	FinanceAccountID string `json:"financeAccountId"`

	// From and To bound the posted day, both included, "2006-01-02". At the
	// spending level From becomes the first of its month and To the last of
	// its month.
	From string `json:"from" graphapi:"nullable"`
	To   string `json:"to" graphapi:"nullable"`
}

type ListSharedFinanceTransactionsArguments struct {
	FinanceAccountID string `json:"financeAccountId"`

	// From and To bound the posted day, both included, "2006-01-02"
	From string `json:"from" graphapi:"nullable"`
	To   string `json:"to" graphapi:"nullable"`

	// Limit is at most 200; zero is 50. After is the nextCursor of the page
	// before; Offset is how many to pass over, for a page by its number,
	// counted from After when both are given.
	Limit  *int   `json:"limit" graphapi:"nullable"`
	After  string `json:"after" graphapi:"nullable"`
	Offset *int   `json:"offset" graphapi:"nullable"`
}

type ShareFinanceAccountArguments struct {
	// One of the caller's own finance accounts
	FinanceAccountID string `json:"financeAccountId"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// balance, spending or transactions
	FinanceShareLevel string `json:"financeShareLevel"`
}

type UnshareFinanceAccountArguments struct {
	ShareID string `json:"shareId"`
}

// financeShareOnATap is what a call is told when only a person's own tap may
// make it. It begins with api.TapRequiredPhrase, which the command line
// recognizes.
const financeShareOnATap = api.TapRequiredPhrase + ": sharing a finance account with somebody it is not already shared with, or at a higher level, is done on the Settings tab of the Finance page on the dashboard or in Finance in the iPhone app, not from a command line or an API token"

// requireFinanceNotAgent refuses the person's agent: it has no tool for
// sharing finance or for reading what others share, and the API says the same
// to an agent that reaches these operations some other way.
func requireFinanceNotAgent(ctx context.Context) error {
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent {
		return api.ErrNotFound
	}
	return nil
}

// requireFinanceSharer is the caller as the owner of finance accounts who
// starts or changes a share: a person with an agent that is on, who is not
// their agent. Listing shares needs only requireFinanceReader, and ending one
// needs no permission at all (requireShareOwner), so an owner who has turned the
// agent off, or whose role lost agent:use, can still stop what they share.
func (self *graph) requireFinanceSharer(ctx context.Context) (*api.Principal, error) {
	if err := requireFinanceNotAgent(ctx); err != nil {
		return nil, err
	}
	principal, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return principal, nil
}

// requireFinanceReader is the caller as a reader of what others share, and as
// the owner listing their own shares: a person who may use the agent area,
// which is the permission finance needs, who is not their agent. Their own
// agent need not be on.
func (self *graph) requireFinanceReader(ctx context.Context) (*api.Principal, error) {
	if err := requireFinanceNotAgent(ctx); err != nil {
		return nil, err
	}
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

func (self *graph) ListFinanceAccountShares(ctx context.Context, arguments ListFinanceAccountSharesArguments) ([]*FinanceShareView, error) {
	principal, err := self.requireFinanceReader(ctx)
	if err != nil {
		return nil, err
	}
	shares, err := self.transaction(ctx).ListFinanceSharesBy(principal.User.ID)
	if err != nil {
		return nil, err
	}
	financeAccountId := strings.TrimSpace(arguments.FinanceAccountID)
	views := make([]*FinanceShareView, 0, len(shares))
	for _, share := range shares {
		if financeAccountId == "" || share.FinanceAccountID == financeAccountId {
			views = append(views, describeFinanceShare(share))
		}
	}
	return views, nil
}

func (self *graph) ShareFinanceAccount(ctx context.Context, arguments ShareFinanceAccountArguments) (*FinanceShareView, error) {
	principal, err := self.requireFinanceSharer(ctx)
	if err != nil {
		return nil, err
	}
	financeShareLevel := models.FinanceShareLevel(strings.ToLower(strings.TrimSpace(arguments.FinanceShareLevel)))
	if !financeShareLevel.IsGrantable() {
		return nil, fmt.Errorf("%w: a finance account is shared at balance, spending or transactions", api.ErrInvalidArguments)
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	financeAccountId := strings.TrimSpace(arguments.FinanceAccountID)
	tx := self.transaction(ctx)
	made, err := tx.ListFinanceSharesBy(principal.User.ID)
	if err != nil {
		return nil, err
	}
	// Sharing an account with somebody who does not already see it, or showing
	// them more of it, is disclosing finance to somebody, a person's tap.
	// Narrowing a share discloses nothing and is open to every sign-in.
	if !principal.IsPersonsTap() {
		var alreadyShared models.FinanceShareLevel
		for _, share := range made {
			if share.FinanceAccountID == financeAccountId && share.GranteeUserID == granteeUserId && share.GranteeCircleID == granteeCircleId {
				alreadyShared = share.FinanceShareLevel
			}
		}
		if !alreadyShared.AtLeast(financeShareLevel) {
			return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, financeShareOnATap)
		}
	}
	saved, err := tx.ShareFinanceAccount(principal.User.ID, &models.FinanceShare{
		FinanceAccountID: financeAccountId, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId,
		FinanceShareLevel: financeShareLevel,
	})
	if err != nil {
		// Not an account of the caller's, not a circle in common, a person who
		// does not exist, a circle the caller is not in: all of them answer as
		// nothing found, because which of them it was is not the caller's to
		// learn.
		return nil, translateError(err)
	}
	shares, err := tx.ListFinanceSharesBy(principal.User.ID)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.ID == saved.ID {
			saved = share
		}
	}
	if isFinanceShareWider(made, saved) {
		if err := self.tellAboutShare(ctx, principal.User, models.ShareReceivedNote{
			SharerName: principal.User.DisplayName(), ShareKind: models.ShareKindFinance, ShareLevel: string(saved.FinanceShareLevel),
			ShareSubjectID: saved.FinanceAccountID,
		}, granteeUserId, granteeCircleId); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s shared a finance account at %s", principal.User.Username, financeShareLevel)
	return describeFinanceShare(saved), nil
}

// isFinanceShareWider says whether the share gives its grantee more of the
// account than the owner's shares did before it: there was none for them, or a
// lower level.
func isFinanceShareWider(earlier []*models.FinanceShare, saved *models.FinanceShare) bool {
	for _, share := range earlier {
		if share.FinanceAccountID == saved.FinanceAccountID && share.GranteeUserID == saved.GranteeUserID && share.GranteeCircleID == saved.GranteeCircleID {
			return saved.FinanceShareLevel.Rank() > share.FinanceShareLevel.Rank()
		}
	}
	return true
}

func (self *graph) UnshareFinanceAccount(ctx context.Context, arguments UnshareFinanceAccountArguments) (bool, error) {
	if err := requireFinanceNotAgent(ctx); err != nil {
		return false, err
	}
	principal, err := self.requireShareOwner(ctx)
	if err != nil {
		return false, err
	}
	// The share is the account owner's to take back; anyone else is told there
	// is no such share.
	if err := self.transaction(ctx).UnshareFinanceAccount(principal.User.ID, strings.TrimSpace(arguments.ShareID)); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

func (self *graph) ListSharedFinanceAccounts(ctx context.Context, arguments ListSharedFinanceAccountsArguments) ([]*SharedFinanceAccountView, error) {
	principal, err := self.requireFinanceReader(ctx)
	if err != nil {
		return nil, err
	}
	reached, err := self.transaction(ctx).ListReachedFinanceAccounts(principal.User.ID, strings.TrimSpace(arguments.OwnerUserID))
	if err != nil {
		return nil, err
	}
	views := make([]*SharedFinanceAccountView, 0, len(reached))
	for _, each := range reached {
		view, err := describeSharedFinanceAccount(each)
		if err != nil {
			return nil, financeError(err)
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListSharedFinanceSpending(ctx context.Context, arguments ListSharedFinanceSpendingArguments) ([]*models.SharedFinanceSpendingRow, error) {
	principal, err := self.requireFinanceReader(ctx)
	if err != nil {
		return nil, err
	}
	from, err := dayArgument("from", arguments.From, "")
	if err != nil {
		return nil, err
	}
	to, err := dayArgument("to", arguments.To, "")
	if err != nil {
		return nil, err
	}
	rows, err := self.transaction(ctx).ListSharedFinanceSpending(principal.User.ID, strings.TrimSpace(arguments.FinanceAccountID), from, to)
	if err != nil {
		return nil, financeError(err)
	}
	return rows, nil
}

func (self *graph) ListSharedFinanceTransactions(ctx context.Context, arguments ListSharedFinanceTransactionsArguments) (*SharedFinanceTransactionPageView, error) {
	principal, err := self.requireFinanceReader(ctx)
	if err != nil {
		return nil, err
	}
	from, err := dayArgument("from", arguments.From, "")
	if err != nil {
		return nil, err
	}
	to, err := dayArgument("to", arguments.To, "")
	if err != nil {
		return nil, err
	}
	filter := &db.SharedFinanceTransactionFilter{From: from, To: to, After: strings.TrimSpace(arguments.After)}
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if arguments.Offset != nil {
		if *arguments.Offset < 0 {
			return nil, fmt.Errorf("%w: offset cannot be negative", api.ErrInvalidArguments)
		}
		filter.Offset = *arguments.Offset
	}
	page, err := self.transaction(ctx).ListSharedFinanceTransactions(principal.User.ID, strings.TrimSpace(arguments.FinanceAccountID), filter)
	if err != nil {
		return nil, financeError(err)
	}
	return &SharedFinanceTransactionPageView{FinanceTransactions: page.FinanceTransactions, NextCursor: page.NextCursor, TotalCount: page.TotalCount}, nil
}
