package apigraph

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Budgets: spending categories and rules, categorizing (transfers being
// the transfer category), counting mirrored copies, budgets and their
// status, spending by day, cash flow and savings targets. Part of the
// finance area (FinanceQuery, FinanceMutation).

// CategorizeTransactionView is the finance transaction as categorized, and
// the spending rule made for its merchant when one was asked for.
type CategorizeTransactionView struct {
	FinanceTransaction *models.FinanceTransaction `json:"financeTransaction"`
	SpendingRule       *models.SpendingRule       `json:"spendingRule,omitempty" graphapi:"nullable"`
}

// CategorizeTransactionsView is the finance transactions as categorized,
// in the order they were named, and the spending rules saved for them,
// the ones confirmed, when any were.
type CategorizeTransactionsView struct {
	FinanceTransactions []*models.FinanceTransaction `json:"financeTransactions"`
	SpendingRules       []*models.SpendingRule       `json:"spendingRules"`
}

// SpendingRuleProposalsView is the spending rules a bulk categorization
// would save, and how many distinct match texts were left out: too short
// or too generic (fewer than four letters, or more digits than letters),
// holding a number that changes each time (a long run of digits, or a
// date), or beyond the most one categorization saves.
type SpendingRuleProposalsView struct {
	SpendingRuleProposals        []*SpendingRuleProposalView `json:"spendingRuleProposals"`
	TooGenericMatchTextCount     int                         `json:"tooGenericMatchTextCount"`
	ChangingNumberMatchTextCount int                         `json:"changingNumberMatchTextCount"`
	OverLimitMatchTextCount      int                         `json:"overLimitMatchTextCount"`
}

// SpendingRuleProposalView is a spending rule that would be saved: what it
// matches and assigns; how many of the finance transactions named it
// matches; how many other finance transactions it would recategorize; and
// the existing spending rule it would be placed ahead of, the first that
// now wins for those transactions and sends them elsewhere, or none when
// it goes after every rule.
type SpendingRuleProposalView struct {
	MatchText               string               `json:"matchText"`
	SpendingCategoryID      string               `json:"spendingCategoryId"`
	FinanceTransactionCount int                  `json:"financeTransactionCount"`
	ChangedTransactionCount int                  `json:"changedTransactionCount"`
	AheadOfSpendingRule     *models.SpendingRule `json:"aheadOfSpendingRule,omitempty" graphapi:"nullable"`
}

// SpendingByDayView is cumulative spending per day of two months, in the
// reporting currency, each finance transaction converted at the rate of
// the day it posted.
type SpendingByDayView struct {
	Month                    string             `json:"month"`
	CompareMonth             string             `json:"compareMonth"`
	ReportingCurrencyCode    string             `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	MonthDays                []*SpendingDayView `json:"monthDays"`
	CompareMonthDays         []*SpendingDayView `json:"compareMonthDays"`
	UnconvertedCurrencyCodes []string           `json:"unconvertedCurrencyCodes"`
}

// SpendingDayView is one day's spending and the month's up to it.
type SpendingDayView struct {
	SpentOn                  string `json:"spentOn"`
	SpendingAmount           string `json:"spendingAmount"`
	CumulativeSpendingAmount string `json:"cumulativeSpendingAmount"`
}

// CashFlowView is income, spending and their difference per month.
type CashFlowView struct {
	// FromMonth and ToMonth are the range, both included, "2006-01".
	FromMonth string `json:"fromMonth"`
	ToMonth   string `json:"toMonth"`

	// CurrencyCashFlowMonths is each month per currency, never added
	// across currencies. Income is money in and spending money out,
	// transfers left out.
	CurrencyCashFlowMonths []*CurrencyCashFlowMonthView `json:"currencyCashFlowMonths"`

	// ReportingCurrencyCode is what CashFlowMonths are in, each finance
	// transaction converted at the rate of the day it posted; a currency
	// with no rate is left out and named in UnconvertedCurrencyCodes.
	ReportingCurrencyCode    string               `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	CashFlowMonths           []*CashFlowMonthView `json:"cashFlowMonths"`
	UnconvertedCurrencyCodes []string             `json:"unconvertedCurrencyCodes"`
}

// CashFlowMonthView is one month's cash flow in the reporting currency.
type CashFlowMonthView struct {
	CashFlowMonth  string `json:"cashFlowMonth"`
	IncomeAmount   string `json:"incomeAmount"`
	SpendingAmount string `json:"spendingAmount"`
	NetAmount      string `json:"netAmount"`
}

// CurrencyCashFlowMonthView is one month's cash flow in one currency.
type CurrencyCashFlowMonthView struct {
	CashFlowMonth  string `json:"cashFlowMonth"`
	CurrencyCode   string `json:"currencyCode"`
	IncomeAmount   string `json:"incomeAmount"`
	SpendingAmount string `json:"spendingAmount"`
	NetAmount      string `json:"netAmount"`
}

// SavingsTargetView is a savings target and how it stands.
type SavingsTargetView struct {
	SavingsTarget         *models.SavingsTarget        `json:"savingsTarget"`
	SavingsTargetProgress *agent.SavingsTargetProgress `json:"savingsTargetProgress"`
}

// SpendingCategoryArguments name one of the caller's spending categories.
type SpendingCategoryArguments struct {
	SpendingCategoryID string `json:"spendingCategoryId"`
}

// CreateSpendingCategoryArguments describe a new spending category.
type CreateSpendingCategoryArguments struct {
	SpendingCategoryName string `json:"spendingCategoryName"`

	// ParentSpendingCategoryID is its one level of parent, a top-level
	// spending category of the caller's.
	ParentSpendingCategoryID string `json:"parentSpendingCategoryId" graphapi:"nullable"`
	IsIncome                 *bool  `json:"isIncome" graphapi:"nullable"`
	IsHidden                 *bool  `json:"isHidden" graphapi:"nullable"`
}

// UpdateSpendingCategoryArguments change what is given; an empty parent
// makes it top-level.
type UpdateSpendingCategoryArguments struct {
	SpendingCategoryID       string  `json:"spendingCategoryId"`
	SpendingCategoryName     *string `json:"spendingCategoryName" graphapi:"nullable"`
	ParentSpendingCategoryID *string `json:"parentSpendingCategoryId" graphapi:"nullable"`
	IsIncome                 *bool   `json:"isIncome" graphapi:"nullable"`
	IsHidden                 *bool   `json:"isHidden" graphapi:"nullable"`
}

// SpendingRuleArguments name one of the caller's spending rules.
type SpendingRuleArguments struct {
	SpendingRuleID string `json:"spendingRuleId"`
}

// CreateSpendingRuleArguments describe a new spending rule. It assigns a
// spending category; the transfer category marks what it matches as
// transfers.
type CreateSpendingRuleArguments struct {
	// MatchText is matched case-insensitively within the merchant, or the
	// description when there is no merchant.
	MatchText          string `json:"matchText"`
	SpendingCategoryID string `json:"spendingCategoryId"`

	// FinanceAccountID limits it to one finance account; MinimumAmount and
	// MaximumAmount bound the signed amount.
	FinanceAccountID string `json:"financeAccountId" graphapi:"nullable"`
	MinimumAmount    string `json:"minimumAmount" graphapi:"nullable"`
	MaximumAmount    string `json:"maximumAmount" graphapi:"nullable"`

	// RulePriority orders the rules, the lowest first; left out, after
	// every rule there is.
	RulePriority *int `json:"rulePriority" graphapi:"nullable"`
}

// UpdateSpendingRuleArguments change what is given; an empty string
// clears an optional field.
type UpdateSpendingRuleArguments struct {
	SpendingRuleID     string  `json:"spendingRuleId"`
	MatchText          *string `json:"matchText" graphapi:"nullable"`
	SpendingCategoryID *string `json:"spendingCategoryId" graphapi:"nullable"`
	FinanceAccountID   *string `json:"financeAccountId" graphapi:"nullable"`
	MinimumAmount      *string `json:"minimumAmount" graphapi:"nullable"`
	MaximumAmount      *string `json:"maximumAmount" graphapi:"nullable"`
	RulePriority       *int    `json:"rulePriority" graphapi:"nullable"`
}

// CategorizeTransactionArguments give a finance transaction a spending
// category (empty takes it away) and may ask for a spending rule for its
// merchant.
type CategorizeTransactionArguments struct {
	FinanceTransactionID     string `json:"financeTransactionId"`
	SpendingCategoryID       string `json:"spendingCategoryId" graphapi:"nullable"`
	ShouldCreateSpendingRule *bool  `json:"shouldCreateSpendingRule" graphapi:"nullable"`
}

// CategorizeTransactionsArguments give several finance transactions one
// spending category (empty takes it away) and may save spending rules: the
// ones the person confirmed from ProposeSpendingRules, exactly as
// proposed, and no others.
type CategorizeTransactionsArguments struct {
	FinanceTransactionIDs []string                `json:"financeTransactionIds"`
	SpendingCategoryID    string                  `json:"spendingCategoryId" graphapi:"nullable"`
	SpendingRules         []ConfirmedSpendingRule `json:"spendingRules" graphapi:"nullable"`
}

// ConfirmedSpendingRule is a spending rule the person confirmed, as
// ProposeSpendingRules proposed it: what it matches, what it assigns
// (which must be the spending category being given), and the id of the
// spending rule it goes ahead of, empty for after every rule.
type ConfirmedSpendingRule struct {
	MatchText             string `json:"matchText"`
	SpendingCategoryID    string `json:"spendingCategoryId"`
	AheadOfSpendingRuleID string `json:"aheadOfSpendingRuleId" graphapi:"nullable"`
}

// ProposeSpendingRulesArguments name the finance transactions and the
// spending category the rules would assign.
type ProposeSpendingRulesArguments struct {
	FinanceTransactionIDs []string `json:"financeTransactionIds"`
	SpendingCategoryID    string   `json:"spendingCategoryId"`
}

// CountTransactionArguments name the finance transaction, a mirrored
// copy, the person counts or hands back to mirror detection.
type CountTransactionArguments struct {
	FinanceTransactionID string `json:"financeTransactionId"`
}

// SetBudgetArguments are a spending category's monthly amount, its
// currency (the reporting currency when left out), and the month it
// starts ("2006-01", this month when left out). Zero ends the budget.
type SetBudgetArguments struct {
	SpendingCategoryID string `json:"spendingCategoryId"`
	MonthlyAmount      string `json:"monthlyAmount"`
	CurrencyCode       string `json:"currencyCode" graphapi:"nullable"`
	EffectiveFrom      string `json:"effectiveFrom" graphapi:"nullable"`
}

// BudgetStatusArguments name the month, "2006-01", this month when left
// out, or instead a calendar year, "2006", whose months are added up as
// each was budgeted.
type BudgetStatusArguments struct {
	Month string `json:"month" graphapi:"nullable"`
	Year  string `json:"year" graphapi:"nullable"`
}

// SavingSummaryArguments name the month, "2006-01" (this one when left
// out), or instead a calendar year, "2006", and the currency to convert
// into instead of the reporting currency.
type SavingSummaryArguments struct {
	Month        string `json:"month" graphapi:"nullable"`
	Year         string `json:"year" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// SpendingByDayArguments name the month (this one when left out), the
// month to compare it with (the one before when left out), and the
// currency to convert into instead of the reporting currency.
type SpendingByDayArguments struct {
	Month        string `json:"month" graphapi:"nullable"`
	CompareMonth string `json:"compareMonth" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// CashFlowArguments are a range of months, "2006-01" (the twelve to this
// one when left out), and the currency to convert into instead of the
// reporting currency.
type CashFlowArguments struct {
	FromMonth    string `json:"fromMonth" graphapi:"nullable"`
	ToMonth      string `json:"toMonth" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// SavingsTargetArguments name one of the caller's savings targets.
type SavingsTargetArguments struct {
	SavingsTargetID string `json:"savingsTargetId"`
}

// CreateSavingsTargetArguments describe a new savings target.
type CreateSavingsTargetArguments struct {
	SavingsTargetName string `json:"savingsTargetName"`
	TargetAmount      string `json:"targetAmount"`

	// CurrencyCode is the reporting currency when left out; TargetOn the
	// day to reach it by, "2006-01-02".
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
	TargetOn     string `json:"targetOn"`

	// TargetMeasure is cash_flow (the default: income less spending since
	// it started), asset_value (what AssetIDs and every asset of
	// FinanceAccountIDs are worth, less StartingAmount) or net_worth (net
	// worth, less StartingAmount). A net_worth target's StartingAmount,
	// left out, is recorded as net worth on StartedOn.
	TargetMeasure     string   `json:"targetMeasure" graphapi:"nullable"`
	StartingAmount    string   `json:"startingAmount" graphapi:"nullable"`
	StartedOn         string   `json:"startedOn" graphapi:"nullable"`
	AssetIDs          []string `json:"assetIds" graphapi:"nullable"`
	FinanceAccountIDs []string `json:"financeAccountIds" graphapi:"nullable"`
}

// UpdateSavingsTargetArguments change what is given; AssetIDs and
// FinanceAccountIDs, when given, replace the ones it had. A change of
// TargetMeasure without a StartingAmount clears the old one, and a
// net_worth target left without one records net worth on StartedOn.
type UpdateSavingsTargetArguments struct {
	SavingsTargetID   string   `json:"savingsTargetId"`
	SavingsTargetName *string  `json:"savingsTargetName" graphapi:"nullable"`
	TargetAmount      *string  `json:"targetAmount" graphapi:"nullable"`
	CurrencyCode      *string  `json:"currencyCode" graphapi:"nullable"`
	TargetOn          *string  `json:"targetOn" graphapi:"nullable"`
	TargetMeasure     *string  `json:"targetMeasure" graphapi:"nullable"`
	StartingAmount    *string  `json:"startingAmount" graphapi:"nullable"`
	StartedOn         *string  `json:"startedOn" graphapi:"nullable"`
	AssetIDs          []string `json:"assetIds" graphapi:"nullable"`
	FinanceAccountIDs []string `json:"financeAccountIds" graphapi:"nullable"`
}

// CloseSavingsTargetArguments give the day a savings target closed (today
// when left out), or ask to open it again.
type CloseSavingsTargetArguments struct {
	SavingsTargetID string `json:"savingsTargetId"`
	ClosedOn        string `json:"closedOn" graphapi:"nullable"`
	ShouldReopen    *bool  `json:"shouldReopen" graphapi:"nullable"`
}

// --- spending categories and rules ---------------------------------------

func (self *graph) SpendingCategories(ctx context.Context) ([]*models.SpendingCategory, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	spendingCategories, err := self.transaction(ctx).ListSpendingCategories(found.ID)
	if err != nil {
		return nil, err
	}
	if spendingCategories == nil {
		spendingCategories = []*models.SpendingCategory{}
	}
	return spendingCategories, nil
}

func (self *graph) CreateSpendingCategory(ctx context.Context, arguments CreateSpendingCategoryArguments) (*models.SpendingCategory, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	spendingCategory := &models.SpendingCategory{
		AgentID: found.ID, SpendingCategoryName: strings.TrimSpace(arguments.SpendingCategoryName),
		ParentSpendingCategoryID: strings.TrimSpace(arguments.ParentSpendingCategoryID),
	}
	if arguments.IsIncome != nil {
		spendingCategory.IsIncome = *arguments.IsIncome
	}
	if arguments.IsHidden != nil {
		spendingCategory.IsHidden = *arguments.IsHidden
	}
	created, err := self.writing(ctx).CreateSpendingCategory(spendingCategory)
	return created, financeError(err)
}

func (self *graph) UpdateSpendingCategory(ctx context.Context, arguments UpdateSpendingCategoryArguments) (*models.SpendingCategory, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	updated, err := self.writing(ctx).UpdateSpendingCategory(found.ID, strings.TrimSpace(arguments.SpendingCategoryID), func(spendingCategory *models.SpendingCategory) error {
		if arguments.SpendingCategoryName != nil {
			spendingCategory.SpendingCategoryName = strings.TrimSpace(*arguments.SpendingCategoryName)
		}
		if arguments.ParentSpendingCategoryID != nil {
			spendingCategory.ParentSpendingCategoryID = strings.TrimSpace(*arguments.ParentSpendingCategoryID)
		}
		if arguments.IsIncome != nil {
			spendingCategory.IsIncome = *arguments.IsIncome
		}
		if arguments.IsHidden != nil {
			spendingCategory.IsHidden = *arguments.IsHidden
		}
		return nil
	})
	return updated, financeError(err)
}

// DeleteSpendingCategory is allowed on a server that no longer offers
// finance, as the other deletes are.
func (self *graph) DeleteSpendingCategory(ctx context.Context, arguments SpendingCategoryArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.writing(ctx).DeleteSpendingCategory(found.ID, strings.TrimSpace(arguments.SpendingCategoryID)); err != nil {
		return false, financeError(err)
	}
	return true, nil
}

func (self *graph) SpendingRules(ctx context.Context) ([]*models.SpendingRule, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	spendingRules, err := self.transaction(ctx).ListSpendingRules(found.ID)
	if err != nil {
		return nil, err
	}
	if spendingRules == nil {
		spendingRules = []*models.SpendingRule{}
	}
	return spendingRules, nil
}

// nextRulePriority is one after the last of the agent's spending rules, so
// a new rule is tried after every rule there is.
func nextRulePriority(tx db.Transaction, agentId string) (int, error) {
	spendingRules, err := tx.ListSpendingRules(agentId)
	if err != nil {
		return 0, err
	}
	return rulePriorityAfter(spendingRules), nil
}

// rulePriorityAfter is one after the last of the spending rules given.
func rulePriorityAfter(spendingRules []*models.SpendingRule) int {
	next := 0
	for _, spendingRule := range spendingRules {
		next = max(next, spendingRule.RulePriority+1)
	}
	return next
}

func (self *graph) CreateSpendingRule(ctx context.Context, arguments CreateSpendingRuleArguments) (*models.SpendingRule, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	spendingRule := &models.SpendingRule{
		AgentID: found.ID, MatchText: strings.TrimSpace(arguments.MatchText),
		SpendingCategoryID: strings.TrimSpace(arguments.SpendingCategoryID), FinanceAccountID: strings.TrimSpace(arguments.FinanceAccountID),
	}
	if spendingRule.MinimumAmount, err = optionalAmountArgument("minimumAmount", arguments.MinimumAmount); err != nil {
		return nil, err
	}
	if spendingRule.MaximumAmount, err = optionalAmountArgument("maximumAmount", arguments.MaximumAmount); err != nil {
		return nil, err
	}
	if arguments.RulePriority != nil {
		spendingRule.RulePriority = *arguments.RulePriority
	} else if spendingRule.RulePriority, err = nextRulePriority(tx, found.ID); err != nil {
		return nil, err
	}
	created, err := tx.CreateSpendingRule(spendingRule)
	return created, financeError(err)
}

func (self *graph) UpdateSpendingRule(ctx context.Context, arguments UpdateSpendingRuleArguments) (*models.SpendingRule, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	var minimumAmount, maximumAmount string
	if arguments.MinimumAmount != nil {
		if minimumAmount, err = optionalAmountArgument("minimumAmount", *arguments.MinimumAmount); err != nil {
			return nil, err
		}
	}
	if arguments.MaximumAmount != nil {
		if maximumAmount, err = optionalAmountArgument("maximumAmount", *arguments.MaximumAmount); err != nil {
			return nil, err
		}
	}
	updated, err := self.writing(ctx).UpdateSpendingRule(found.ID, strings.TrimSpace(arguments.SpendingRuleID), func(spendingRule *models.SpendingRule) error {
		if arguments.MatchText != nil {
			spendingRule.MatchText = strings.TrimSpace(*arguments.MatchText)
		}
		if arguments.SpendingCategoryID != nil {
			spendingRule.SpendingCategoryID = strings.TrimSpace(*arguments.SpendingCategoryID)
		}
		if arguments.FinanceAccountID != nil {
			spendingRule.FinanceAccountID = strings.TrimSpace(*arguments.FinanceAccountID)
		}
		if arguments.MinimumAmount != nil {
			spendingRule.MinimumAmount = minimumAmount
		}
		if arguments.MaximumAmount != nil {
			spendingRule.MaximumAmount = maximumAmount
		}
		if arguments.RulePriority != nil {
			spendingRule.RulePriority = *arguments.RulePriority
		}
		return nil
	})
	return updated, financeError(err)
}

// DeleteSpendingRule is allowed on a server that no longer offers finance,
// as the other deletes are.
func (self *graph) DeleteSpendingRule(ctx context.Context, arguments SpendingRuleArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.writing(ctx).DeleteSpendingRule(found.ID, strings.TrimSpace(arguments.SpendingRuleID)); err != nil {
		return false, financeError(err)
	}
	return true, nil
}

// ownFinanceTransaction is one of the caller's finance transactions, or
// not found.
func ownFinanceTransaction(tx db.Transaction, agentId, financeTransactionId string) (*models.FinanceTransaction, error) {
	if strings.TrimSpace(financeTransactionId) == "" {
		return nil, fmt.Errorf("%w: which finance transaction", api.ErrInvalidArguments)
	}
	financeTransaction, err := tx.GetFinanceTransaction(agentId, strings.TrimSpace(financeTransactionId))
	if err != nil {
		return nil, err
	}
	if financeTransaction == nil {
		return nil, api.ErrNotFound
	}
	return financeTransaction, nil
}

func (self *graph) CategorizeTransaction(ctx context.Context, arguments CategorizeTransactionArguments) (*CategorizeTransactionView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	financeTransaction, err := ownFinanceTransaction(tx, found.ID, arguments.FinanceTransactionID)
	if err != nil {
		return nil, err
	}
	spendingCategoryId := strings.TrimSpace(arguments.SpendingCategoryID)
	// Everything is checked before the first write: the request commits
	// what was written even when the resolver then fails, so a refused
	// spending rule must not leave the categorization behind.
	if spendingCategoryId != "" {
		if err := requireSpendingCategory(tx, found.ID, spendingCategoryId); err != nil {
			return nil, err
		}
	}
	isSpendingRuleWanted := arguments.ShouldCreateSpendingRule != nil && *arguments.ShouldCreateSpendingRule
	matchText := spendingRuleMatchText(financeTransaction)
	if isSpendingRuleWanted {
		if spendingCategoryId == "" {
			return nil, fmt.Errorf("%w: a spending rule needs the spending category it assigns", api.ErrInvalidArguments)
		}
		if matchText == "" {
			return nil, fmt.Errorf("%w: this finance transaction has no merchant or description for a spending rule to match", api.ErrInvalidArguments)
		}
	}
	view := &CategorizeTransactionView{}
	// The two writes go together or not at all.
	err = tx.TransactionContext(ctx, func(nested db.Transaction) error {
		if _, err := nested.SetTransactionCategorization(found.ID, financeTransaction.ID, spendingCategoryId, models.CategorizedByPerson, nil); err != nil {
			return err
		}
		if !isSpendingRuleWanted {
			return nil
		}
		priority, err := nextRulePriority(nested, found.ID)
		if err != nil {
			return err
		}
		view.SpendingRule, err = nested.CreateSpendingRule(&models.SpendingRule{
			AgentID: found.ID, MatchText: matchText, SpendingCategoryID: spendingCategoryId, RulePriority: priority,
		})
		return err
	})
	if err != nil {
		return nil, financeError(err)
	}
	if view.FinanceTransaction, err = tx.GetFinanceTransaction(found.ID, financeTransaction.ID); err != nil {
		return nil, err
	}
	return view, nil
}

// maximumCategorizedTransactionCount is how many finance transactions one
// CategorizeTransactions takes: a few pages of the dashboard's list, and a
// bound on one statement.
const maximumCategorizedTransactionCount = 500

// maximumProposedTransactionCount is how many finance transactions one
// ProposeSpendingRules takes. It writes nothing, so it takes a whole
// selection at once: the rules are proposed over all of it, never once per
// piece the dashboard sends to CategorizeTransactions, where a short match
// text and a longer one could each be proposed and both saved.
const maximumProposedTransactionCount = 5000

// maximumSpendingRuleProposalCount is how many spending rules one bulk
// categorization proposes and saves: more than a person reads before
// confirming.
const maximumSpendingRuleProposalCount = 50

// minimumSpendingRuleLetterCount is how many letters a match text needs to
// be saved as a rule in bulk: a shorter one, a card processor's two-letter
// prefix, is within the merchant of a great many unrelated charges.
const minimumSpendingRuleLetterCount = 4

// changingNumberPattern finds what makes a match text belong to one charge
// only: a run of six digits or more (a reference or an order number), or a
// date. A rule matching it would never match another transaction.
var changingNumberPattern = regexp.MustCompile(`\d{6,}` +
	`|\b\d{4}[-/.](0?[1-9]|1[0-2])[-/.](0?[1-9]|[12]\d|3[01])\b` +
	`|\b(0?[1-9]|[12]\d|3[01])[-/.](0?[1-9]|[12]\d|3[01])[-/.](\d{4}|\d{2})\b` +
	`|\b(0?[1-9]|1[0-2])/(0?[1-9]|[12]\d|3[01])\b`)

// bulkSpendingRuleRefusal is why a match text is not saved as a spending
// rule in bulk.
type bulkSpendingRuleRefusal int

const (
	// bulkSpendingRuleAccepted can be saved.
	bulkSpendingRuleAccepted bulkSpendingRuleRefusal = iota
	// bulkSpendingRuleTooGeneric has fewer than four letters, or more
	// digits than letters, and would match far more than was chosen.
	bulkSpendingRuleTooGeneric
	// bulkSpendingRuleChangingNumber holds a number that changes with each
	// charge, so it would match nothing again.
	bulkSpendingRuleChangingNumber
)

// bulkSpendingRuleRefusalOf says whether a match text can be saved as a
// spending rule in bulk. The rule made from one transaction's own row is
// not held to this: there the person sees the one text being saved.
func bulkSpendingRuleRefusalOf(matchText string) bulkSpendingRuleRefusal {
	letterCount, digitCount := 0, 0
	for _, character := range strings.TrimSpace(matchText) {
		switch {
		case unicode.IsLetter(character):
			letterCount++
		case unicode.IsDigit(character):
			digitCount++
		}
	}
	if letterCount < minimumSpendingRuleLetterCount || digitCount > letterCount {
		return bulkSpendingRuleTooGeneric
	}
	if changingNumberPattern.MatchString(matchText) {
		return bulkSpendingRuleChangingNumber
	}
	return bulkSpendingRuleAccepted
}

// requireSpendingCategory refuses a spending category that is not the
// caller's.
func requireSpendingCategory(tx db.Transaction, agentId, spendingCategoryId string) error {
	spendingCategory, err := tx.GetSpendingCategory(agentId, spendingCategoryId)
	if err != nil {
		return err
	}
	if spendingCategory == nil {
		return fmt.Errorf("%w: there is no spending category %q; SpendingCategories lists them", api.ErrInvalidArguments, spendingCategoryId)
	}
	return nil
}

// spendingRuleMatchText is what a spending rule for a finance transaction
// matches: what the rules are matched against, its merchant, or its
// description when it has none.
func spendingRuleMatchText(financeTransaction *models.FinanceTransaction) string {
	if matchText := strings.TrimSpace(financeTransaction.MerchantName); matchText != "" {
		return matchText
	}
	return strings.TrimSpace(financeTransaction.Description)
}

// ownFinanceTransactions is the caller's finance transactions by the ids
// given, each once, in the order first named. None, or more than the
// maximum, is refused, and any id that is not the caller's finds nothing,
// so a list with somebody else's id in it acts on none of them.
func ownFinanceTransactions(tx db.Transaction, agentId string, financeTransactionIds []string, maximumCount int) ([]*models.FinanceTransaction, error) {
	var distinctIds []string
	isNamed := map[string]bool{}
	for _, financeTransactionId := range financeTransactionIds {
		financeTransactionId = strings.TrimSpace(financeTransactionId)
		if financeTransactionId == "" || isNamed[financeTransactionId] {
			continue
		}
		isNamed[financeTransactionId] = true
		distinctIds = append(distinctIds, financeTransactionId)
	}
	if len(distinctIds) == 0 {
		return nil, fmt.Errorf("%w: which finance transactions", api.ErrInvalidArguments)
	}
	if len(distinctIds) > maximumCount {
		return nil, fmt.Errorf("%w: %d finance transactions at a time at most, not %d", api.ErrInvalidArguments, maximumCount, len(distinctIds))
	}
	found, err := tx.GetFinanceTransactions(agentId, distinctIds)
	if err != nil {
		return nil, err
	}
	if len(found) != len(distinctIds) {
		return nil, api.ErrNotFound
	}
	byId := make(map[string]*models.FinanceTransaction, len(found))
	for _, financeTransaction := range found {
		byId[financeTransaction.ID] = financeTransaction
	}
	financeTransactions := make([]*models.FinanceTransaction, 0, len(distinctIds))
	for _, financeTransactionId := range distinctIds {
		financeTransactions = append(financeTransactions, byId[financeTransactionId])
	}
	return financeTransactions, nil
}

// proposeSpendingRules is a spending rule to the spending category for
// each distinct match text of the finance transactions (in any case), each
// its own proposal, and how many match texts it left out and why.
//
// A mirrored copy is left out: the counted transaction it mirrors speaks
// for it. A match text too short or generic, or holding a number that
// changes each time, is left out and counted. One is covered, and left
// out, when the rule that applies first to every finance transaction
// carrying it (firstMatchingRuleIds, from FirstMatchingSpendingRules)
// already sends it to the spending category; a later rule that would also
// send it there does not cover it, since the earlier one wins. Otherwise
// the rule goes ahead of the earliest rule that now wins for one of them,
// so it takes effect, or after every rule when none matches them.
//
// Most finance transactions matched first, then by match text, and no
// more than the maximum; the rest are counted.
func proposeSpendingRules(financeTransactions []*models.FinanceTransaction, spendingRules []*models.SpendingRule, firstMatchingRuleIds map[string]string, spendingCategoryId string) *SpendingRuleProposalsView {
	view := &SpendingRuleProposalsView{SpendingRuleProposals: []*SpendingRuleProposalView{}}
	ruleIndexes := make(map[string]int, len(spendingRules))
	for index, spendingRule := range spendingRules {
		ruleIndexes[spendingRule.ID] = index
	}
	// Each distinct match text, as first written, with its transactions.
	type matchTextGroup struct {
		matchText           string
		financeTransactions []*models.FinanceTransaction
	}
	var groups []*matchTextGroup
	byLowered := map[string]*matchTextGroup{}
	var loweredTexts []string
	for _, financeTransaction := range financeTransactions {
		if financeTransaction.DuplicateOfTransactionID != "" {
			continue
		}
		matchText := spendingRuleMatchText(financeTransaction)
		if matchText == "" {
			continue
		}
		lowered := strings.ToLower(matchText)
		loweredTexts = append(loweredTexts, lowered)
		group := byLowered[lowered]
		if group == nil {
			group = &matchTextGroup{matchText: matchText}
			byLowered[lowered] = group
			groups = append(groups, group)
		}
		group.financeTransactions = append(group.financeTransactions, financeTransaction)
	}
	for _, group := range groups {
		switch bulkSpendingRuleRefusalOf(group.matchText) {
		case bulkSpendingRuleTooGeneric:
			view.TooGenericMatchTextCount++
			continue
		case bulkSpendingRuleChangingNumber:
			view.ChangingNumberMatchTextCount++
			continue
		}
		isCovered := true
		aheadOfIndex := -1
		for _, financeTransaction := range group.financeTransactions {
			ruleIndex, isMatched := ruleIndexes[firstMatchingRuleIds[financeTransaction.ID]]
			if isMatched && spendingRules[ruleIndex].SpendingCategoryID == spendingCategoryId {
				continue
			}
			isCovered = false
			if isMatched && (aheadOfIndex < 0 || ruleIndex < aheadOfIndex) {
				aheadOfIndex = ruleIndex
			}
		}
		if isCovered {
			continue
		}
		proposal := &SpendingRuleProposalView{MatchText: group.matchText, SpendingCategoryID: spendingCategoryId}
		if aheadOfIndex >= 0 {
			proposal.AheadOfSpendingRule = spendingRules[aheadOfIndex]
		}
		lowered := strings.ToLower(group.matchText)
		for _, candidate := range loweredTexts {
			if strings.Contains(candidate, lowered) {
				proposal.FinanceTransactionCount++
			}
		}
		view.SpendingRuleProposals = append(view.SpendingRuleProposals, proposal)
	}
	proposals := view.SpendingRuleProposals
	sort.SliceStable(proposals, func(left, right int) bool {
		if proposals[left].FinanceTransactionCount != proposals[right].FinanceTransactionCount {
			return proposals[left].FinanceTransactionCount > proposals[right].FinanceTransactionCount
		}
		return strings.ToLower(proposals[left].MatchText) < strings.ToLower(proposals[right].MatchText)
	})
	if len(proposals) > maximumSpendingRuleProposalCount {
		view.OverLimitMatchTextCount = len(proposals) - maximumSpendingRuleProposalCount
		view.SpendingRuleProposals = proposals[:maximumSpendingRuleProposalCount]
	}
	return view
}

func (self *graph) ProposeSpendingRules(ctx context.Context, arguments ProposeSpendingRulesArguments) (*SpendingRuleProposalsView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	spendingCategoryId := strings.TrimSpace(arguments.SpendingCategoryID)
	if spendingCategoryId == "" {
		return nil, fmt.Errorf("%w: a spending rule needs the spending category it assigns", api.ErrInvalidArguments)
	}
	if err := requireSpendingCategory(tx, found.ID, spendingCategoryId); err != nil {
		return nil, err
	}
	financeTransactions, err := ownFinanceTransactions(tx, found.ID, arguments.FinanceTransactionIDs, maximumProposedTransactionCount)
	if err != nil {
		return nil, err
	}
	spendingRules, err := tx.ListSpendingRules(found.ID)
	if err != nil {
		return nil, err
	}
	financeTransactionIds := make([]string, 0, len(financeTransactions))
	for _, financeTransaction := range financeTransactions {
		financeTransactionIds = append(financeTransactionIds, financeTransaction.ID)
	}
	firstMatchingRuleIds, err := tx.FirstMatchingSpendingRules(found.ID, financeTransactionIds)
	if err != nil {
		return nil, err
	}
	view := proposeSpendingRules(financeTransactions, spendingRules, firstMatchingRuleIds, spendingCategoryId)
	// What each would change beyond the chosen transactions, which become
	// the person's own choice and so are never a rule's to change.
	proposedSpendingRules := make([]*db.ProposedSpendingRule, 0, len(view.SpendingRuleProposals))
	for _, proposal := range view.SpendingRuleProposals {
		proposed := &db.ProposedSpendingRule{MatchText: proposal.MatchText, SpendingCategoryID: proposal.SpendingCategoryID}
		if proposal.AheadOfSpendingRule != nil {
			proposed.AheadOfSpendingRuleID = proposal.AheadOfSpendingRule.ID
		}
		proposedSpendingRules = append(proposedSpendingRules, proposed)
	}
	changedCounts, err := tx.CountSpendingRuleChanges(found.ID, proposedSpendingRules, financeTransactionIds)
	if err != nil {
		return nil, err
	}
	for index, proposal := range view.SpendingRuleProposals {
		proposal.ChangedTransactionCount = changedCounts[index]
	}
	return view, nil
}

// confirmedSpendingRules checks the spending rules the person confirmed
// before anything is written: each as a bulk proposal would allow it, to
// the spending category being given, once. They come back trimmed.
func confirmedSpendingRules(confirmed []ConfirmedSpendingRule, spendingCategoryId string) ([]ConfirmedSpendingRule, error) {
	if len(confirmed) == 0 {
		return nil, nil
	}
	if spendingCategoryId == "" {
		return nil, fmt.Errorf("%w: a spending rule needs the spending category it assigns", api.ErrInvalidArguments)
	}
	if len(confirmed) > maximumSpendingRuleProposalCount {
		return nil, fmt.Errorf("%w: %d spending rules at a time at most, not %d", api.ErrInvalidArguments, maximumSpendingRuleProposalCount, len(confirmed))
	}
	checked := make([]ConfirmedSpendingRule, 0, len(confirmed))
	isNamed := map[string]bool{}
	for _, spendingRule := range confirmed {
		spendingRule.MatchText = strings.TrimSpace(spendingRule.MatchText)
		spendingRule.SpendingCategoryID = strings.TrimSpace(spendingRule.SpendingCategoryID)
		spendingRule.AheadOfSpendingRuleID = strings.TrimSpace(spendingRule.AheadOfSpendingRuleID)
		switch bulkSpendingRuleRefusalOf(spendingRule.MatchText) {
		case bulkSpendingRuleTooGeneric:
			return nil, fmt.Errorf("%w: %q is too short or too generic for a spending rule: it needs %d letters and more letters than digits", api.ErrInvalidArguments, spendingRule.MatchText, minimumSpendingRuleLetterCount)
		case bulkSpendingRuleChangingNumber:
			return nil, fmt.Errorf("%w: %q holds a number that changes each time, so a spending rule for it would match nothing again", api.ErrInvalidArguments, spendingRule.MatchText)
		}
		if spendingRule.SpendingCategoryID != spendingCategoryId {
			return nil, fmt.Errorf("%w: the spending rule for %q was proposed for another spending category; propose them again", api.ErrInvalidArguments, spendingRule.MatchText)
		}
		lowered := strings.ToLower(spendingRule.MatchText)
		if isNamed[lowered] {
			return nil, fmt.Errorf("%w: the spending rule for %q is named twice", api.ErrInvalidArguments, spendingRule.MatchText)
		}
		isNamed[lowered] = true
		checked = append(checked, spendingRule)
	}
	return checked, nil
}

// placeSpendingRules puts each confirmed rule ahead of the existing rule it
// names, or after every rule when it names none, and answers the new rules
// with their priorities and the priorities existing rules move to, by id.
// Rules are tried by priority and then by id, and a new rule's id is
// random, so a new rule never shares a priority with a neighbour. An
// existing rule keeps its priority where there is room and is pushed back
// only as far as needed, in the same order; two that shared a priority
// still do. A rule named that is gone refuses them all: the proposal was
// made against rules that have since changed.
func placeSpendingRules(agentId string, spendingRules []*models.SpendingRule, confirmed []ConfirmedSpendingRule) ([]*models.SpendingRule, map[string]int, error) {
	ruleIndexes := make(map[string]int, len(spendingRules))
	for index, spendingRule := range spendingRules {
		ruleIndexes[spendingRule.ID] = index
	}
	aheadOf := make([][]ConfirmedSpendingRule, len(spendingRules))
	var afterEvery []ConfirmedSpendingRule
	for _, spendingRule := range confirmed {
		if spendingRule.AheadOfSpendingRuleID == "" {
			afterEvery = append(afterEvery, spendingRule)
			continue
		}
		index, isFound := ruleIndexes[spendingRule.AheadOfSpendingRuleID]
		if !isFound {
			return nil, nil, fmt.Errorf("%w: the spending rule for %q was to go ahead of a spending rule that is gone; propose them again", api.ErrConflict, spendingRule.MatchText)
		}
		aheadOf[index] = append(aheadOf[index], spendingRule)
	}
	var newSpendingRules []*models.SpendingRule
	movedPriorities := map[string]int{}
	lastPriority, hasLast, isLastNew := 0, false, false
	placeNew := func(spendingRule ConfirmedSpendingRule) {
		priority := 0
		if hasLast {
			priority = lastPriority + 1
		}
		newSpendingRules = append(newSpendingRules, &models.SpendingRule{
			AgentID: agentId, MatchText: spendingRule.MatchText, SpendingCategoryID: spendingRule.SpendingCategoryID, RulePriority: priority,
		})
		lastPriority, hasLast, isLastNew = priority, true, true
	}
	for index, spendingRule := range spendingRules {
		for _, confirmedRule := range aheadOf[index] {
			placeNew(confirmedRule)
		}
		priority := spendingRule.RulePriority
		switch {
		case !hasLast:
		case !isLastNew && spendingRule.RulePriority == spendingRules[index-1].RulePriority:
			priority = lastPriority
		default:
			priority = max(priority, lastPriority+1)
		}
		if priority != spendingRule.RulePriority {
			movedPriorities[spendingRule.ID] = priority
		}
		lastPriority, hasLast, isLastNew = priority, true, false
	}
	for _, confirmedRule := range afterEvery {
		placeNew(confirmedRule)
	}
	return newSpendingRules, movedPriorities, nil
}

func (self *graph) CategorizeTransactions(ctx context.Context, arguments CategorizeTransactionsArguments) (*CategorizeTransactionsView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	// Checked before the first write, as CategorizeTransaction is.
	financeTransactions, err := ownFinanceTransactions(tx, found.ID, arguments.FinanceTransactionIDs, maximumCategorizedTransactionCount)
	if err != nil {
		return nil, err
	}
	spendingCategoryId := strings.TrimSpace(arguments.SpendingCategoryID)
	if spendingCategoryId != "" {
		if err := requireSpendingCategory(tx, found.ID, spendingCategoryId); err != nil {
			return nil, err
		}
	}
	confirmed, err := confirmedSpendingRules(arguments.SpendingRules, spendingCategoryId)
	if err != nil {
		return nil, err
	}
	financeTransactionIds := make([]string, 0, len(financeTransactions))
	for _, financeTransaction := range financeTransactions {
		financeTransactionIds = append(financeTransactionIds, financeTransaction.ID)
	}
	view := &CategorizeTransactionsView{SpendingRules: []*models.SpendingRule{}}
	// Every categorization and every rule go together or not at all. The
	// person's choices are written first, so the rules applied after them
	// leave these transactions as the person said. The rules saved are the
	// ones confirmed, as confirmed, never proposed again here: what the
	// person read is what is saved.
	err = tx.TransactionContext(ctx, func(nested db.Transaction) error {
		if _, err := nested.CategorizeTransactionsByPerson(found.ID, financeTransactionIds, spendingCategoryId); err != nil {
			return err
		}
		if len(confirmed) == 0 {
			return nil
		}
		spendingRules, err := nested.ListSpendingRules(found.ID)
		if err != nil {
			return err
		}
		newSpendingRules, movedPriorities, err := placeSpendingRules(found.ID, spendingRules, confirmed)
		if err != nil {
			return err
		}
		if len(movedPriorities) > 0 {
			if err := nested.SetSpendingRulePriorities(found.ID, movedPriorities); err != nil {
				return err
			}
		}
		view.SpendingRules, err = nested.CreateSpendingRules(newSpendingRules)
		return err
	})
	if err != nil {
		return nil, financeError(err)
	}
	if view.FinanceTransactions, err = ownFinanceTransactions(tx, found.ID, financeTransactionIds, maximumCategorizedTransactionCount); err != nil {
		return nil, err
	}
	return view, nil
}

func (self *graph) CountTransaction(ctx context.Context, arguments CountTransactionArguments) (*models.FinanceTransaction, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	return setTransactionCounted(self.writing(ctx), found.ID, arguments.FinanceTransactionID, true)
}

func (self *graph) UndoCountTransaction(ctx context.Context, arguments CountTransactionArguments) (*models.FinanceTransaction, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	return setTransactionCounted(self.writing(ctx), found.ID, arguments.FinanceTransactionID, false)
}

// setTransactionCounted records or forgets the person's decision that a
// mirrored copy counts, and answers the finance transaction as it is
// after mirror detection has decided again.
func setTransactionCounted(tx db.Transaction, agentId, financeTransactionId string, isCountedByPerson bool) (*models.FinanceTransaction, error) {
	financeTransaction, err := ownFinanceTransaction(tx, agentId, financeTransactionId)
	if err != nil {
		return nil, err
	}
	if err := tx.SetFinanceTransactionCountedByPerson(agentId, financeTransaction.ID, isCountedByPerson); err != nil {
		return nil, financeError(err)
	}
	return tx.GetFinanceTransaction(agentId, financeTransaction.ID)
}

// --- budgets ------------------------------------------------------------

func (self *graph) Budgets(ctx context.Context) ([]*models.Budget, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	budgets, err := self.transaction(ctx).ListBudgets(found.ID)
	if err != nil {
		return nil, err
	}
	if budgets == nil {
		budgets = []*models.Budget{}
	}
	return budgets, nil
}

func (self *graph) SetBudget(ctx context.Context, arguments SetBudgetArguments) (*models.Budget, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	spendingCategory, err := tx.GetSpendingCategory(found.ID, strings.TrimSpace(arguments.SpendingCategoryID))
	if err != nil {
		return nil, err
	}
	if spendingCategory == nil {
		return nil, api.ErrNotFound
	}
	monthlyAmount, err := amountArgument("monthlyAmount", arguments.MonthlyAmount)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(monthlyAmount, "-") {
		return nil, fmt.Errorf("%w: a budget cannot be negative; zero ends it", api.ErrInvalidArguments)
	}
	fallbackCurrency, err := reportingCurrency(tx, found, "")
	if err != nil {
		return nil, err
	}
	currencyCode, err := currencyArgument("currencyCode", arguments.CurrencyCode, fallbackCurrency)
	if err != nil {
		return nil, err
	}
	if currencyCode == "" {
		return nil, fmt.Errorf("%w: a budget needs a currency, a code like USD", api.ErrInvalidArguments)
	}
	month, err := monthArgument("effectiveFrom", arguments.EffectiveFrom, personToday(principal)[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	budget, err := tx.SetBudget(&models.Budget{
		AgentID: found.ID, SpendingCategoryID: spendingCategory.ID, MonthlyAmount: monthlyAmount,
		CurrencyCode: currencyCode, EffectiveFrom: month + "-01",
	})
	return budget, financeError(err)
}

func (self *graph) BudgetStatus(ctx context.Context, arguments BudgetStatusArguments) (*models.BudgetStatus, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	today := personToday(principal)
	year, err := yearArgument("year", arguments.Year, arguments.Month)
	if err != nil {
		return nil, err
	}
	if year != "" {
		status, err := agent.YearBudgetStatus(ctx, self.transaction(ctx), self.exchangeRateFetcher(), found.ID, year, today)
		return status, financeError(err)
	}
	month, err := monthArgument("month", arguments.Month, today[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	status, err := agent.BudgetStatus(ctx, self.transaction(ctx), self.exchangeRateFetcher(), found.ID, month, today)
	return status, financeError(err)
}

func (self *graph) SavingSummary(ctx context.Context, arguments SavingSummaryArguments) (*models.SavingSummary, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	today := personToday(principal)
	year, err := yearArgument("year", arguments.Year, arguments.Month)
	if err != nil {
		return nil, err
	}
	month, err := monthArgument("month", arguments.Month, today[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	if year != "" {
		summary, err := agent.YearSavingSummary(ctx, tx, self.exchangeRateFetcher(), found.ID, year, today, currencyCode)
		return summary, financeError(err)
	}
	summary, err := agent.SavingSummary(ctx, tx, self.exchangeRateFetcher(), found.ID, month, today, currencyCode)
	return summary, financeError(err)
}

func (self *graph) SpendingByDay(ctx context.Context, arguments SpendingByDayArguments) (*SpendingByDayView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	today := personToday(principal)
	month, err := monthArgument("month", arguments.Month, today[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	monthStart, _ := time.Parse("2006-01", month)
	compareMonth, err := monthArgument("compareMonth", arguments.CompareMonth, monthStart.AddDate(0, -1, 0).Format("2006-01"))
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	view := &SpendingByDayView{
		Month: month, CompareMonth: compareMonth, ReportingCurrencyCode: currencyCode,
		MonthDays: []*SpendingDayView{}, CompareMonthDays: []*SpendingDayView{}, UnconvertedCurrencyCodes: []string{},
	}
	if currencyCode == "" {
		return view, nil
	}
	converter := rates.NewConverter(ctx, self.exchangeRateFetcher(), tx)
	unconverted := map[string]bool{}
	for _, wanted := range []struct {
		month string
		days  *[]*SpendingDayView
	}{{month, &view.MonthDays}, {compareMonth, &view.CompareMonthDays}} {
		days, err := spendingDaysOf(tx, converter, found.ID, wanted.month, currencyCode, today, unconverted)
		if err != nil {
			return nil, financeError(err)
		}
		*wanted.days = days
	}
	view.UnconvertedCurrencyCodes = sortedCurrencyCodes(unconverted)
	return view, nil
}

// spendingDaysOf is each day of a month to its end, or to today for this
// month, with the day's spending and the month's up to it, converted into
// one currency at each day's rate. Spending in a currency with no rate is
// left out and its currency added to unconverted.
func spendingDaysOf(tx db.Transaction, converter *rates.Converter, agentId, month, currencyCode, today string, unconverted map[string]bool) ([]*SpendingDayView, error) {
	rows, err := tx.ListSpendingCategoryDays(agentId, month)
	if err != nil {
		return nil, err
	}
	byDay := map[string]*big.Rat{}
	for _, row := range rows {
		converted, isConverted, err := convertOrSkip(converter, row.SpendingAmount, row.CurrencyCode, currencyCode, row.SpentOn)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			unconverted[row.CurrencyCode] = true
			continue
		}
		if byDay[row.SpentOn] == nil {
			byDay[row.SpentOn] = new(big.Rat)
		}
		byDay[row.SpentOn].Add(byDay[row.SpentOn], converted)
	}
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, err
	}
	days := []*SpendingDayView{}
	cumulative := new(big.Rat)
	for day := monthStart; day.Month() == monthStart.Month(); day = day.AddDate(0, 0, 1) {
		spentOn := day.Format(time.DateOnly)
		if spentOn > today {
			break
		}
		spent := byDay[spentOn]
		if spent == nil {
			spent = new(big.Rat)
		}
		cumulative.Add(cumulative, spent)
		days = append(days, &SpendingDayView{
			SpentOn: spentOn, SpendingAmount: finance.FormatAmount(spent), CumulativeSpendingAmount: finance.FormatAmount(cumulative),
		})
	}
	return days, nil
}

func (self *graph) CashFlow(ctx context.Context, arguments CashFlowArguments) (*CashFlowView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	today := personToday(principal)
	toMonth, err := monthArgument("toMonth", arguments.ToMonth, today[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	toStart, _ := time.Parse("2006-01", toMonth)
	fromMonth, err := monthArgument("fromMonth", arguments.FromMonth, toStart.AddDate(0, -11, 0).Format("2006-01"))
	if err != nil {
		return nil, err
	}
	fromStart, _ := time.Parse("2006-01", fromMonth)
	if fromStart.After(toStart) {
		return nil, fmt.Errorf("%w: fromMonth %s is after toMonth %s", api.ErrInvalidArguments, fromMonth, toMonth)
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	// Counted the way the day-by-day spending and budgets count it, so the
	// month's spending agrees everywhere it is shown: a refund in a
	// spending category lowers spending rather than counting as income.
	days, err := tx.ListCashFlowDays(found.ID, fromStart.Format(time.DateOnly), toStart.AddDate(0, 1, -1).Format(time.DateOnly))
	if err != nil {
		return nil, financeError(err)
	}
	view := &CashFlowView{
		FromMonth: fromMonth, ToMonth: toMonth, CurrencyCashFlowMonths: []*CurrencyCashFlowMonthView{},
		ReportingCurrencyCode: currencyCode, CashFlowMonths: []*CashFlowMonthView{}, UnconvertedCurrencyCodes: []string{},
	}
	type currencyMonth struct {
		cashFlowMonth string
		currencyCode  string
	}
	incomeByCurrencyMonth, spendingByCurrencyMonth := map[currencyMonth]*big.Rat{}, map[currencyMonth]*big.Rat{}
	// Converted into the reporting currency per currency first, so a
	// currency with a day that has no rate is left out whole, never in part.
	convertedIncomeByCurrencyMonth, convertedSpendingByCurrencyMonth := map[currencyMonth]*big.Rat{}, map[currencyMonth]*big.Rat{}
	isUnconverted := map[string]bool{}
	converter := rates.NewConverter(ctx, self.exchangeRateFetcher(), tx)
	for _, day := range days {
		key := currencyMonth{cashFlowMonth: day.CashFlowOn[:len("2006-01")], currencyCode: day.CurrencyCode}
		if incomeByCurrencyMonth[key], err = addAmount(incomeByCurrencyMonth[key], day.IncomeAmount); err != nil {
			return nil, err
		}
		if spendingByCurrencyMonth[key], err = addAmount(spendingByCurrencyMonth[key], day.SpendingAmount); err != nil {
			return nil, err
		}
		if currencyCode == "" || isUnconverted[day.CurrencyCode] {
			continue
		}
		income, isIncomeConverted, err := convertOrSkip(converter, day.IncomeAmount, day.CurrencyCode, currencyCode, day.CashFlowOn)
		if err != nil {
			return nil, err
		}
		spending, isSpendingConverted, err := convertOrSkip(converter, day.SpendingAmount, day.CurrencyCode, currencyCode, day.CashFlowOn)
		if err != nil {
			return nil, err
		}
		if !isIncomeConverted || !isSpendingConverted {
			isUnconverted[day.CurrencyCode] = true
			continue
		}
		if convertedIncomeByCurrencyMonth[key] == nil {
			convertedIncomeByCurrencyMonth[key], convertedSpendingByCurrencyMonth[key] = new(big.Rat), new(big.Rat)
		}
		convertedIncomeByCurrencyMonth[key].Add(convertedIncomeByCurrencyMonth[key], income)
		convertedSpendingByCurrencyMonth[key].Add(convertedSpendingByCurrencyMonth[key], spending)
	}
	incomeByMonth, spendingByMonth := map[string]*big.Rat{}, map[string]*big.Rat{}
	for key, income := range convertedIncomeByCurrencyMonth {
		if isUnconverted[key.currencyCode] {
			continue
		}
		if incomeByMonth[key.cashFlowMonth] == nil {
			incomeByMonth[key.cashFlowMonth], spendingByMonth[key.cashFlowMonth] = new(big.Rat), new(big.Rat)
		}
		incomeByMonth[key.cashFlowMonth].Add(incomeByMonth[key.cashFlowMonth], income)
		spendingByMonth[key.cashFlowMonth].Add(spendingByMonth[key.cashFlowMonth], convertedSpendingByCurrencyMonth[key])
	}
	for key, income := range incomeByCurrencyMonth {
		spending := spendingByCurrencyMonth[key]
		view.CurrencyCashFlowMonths = append(view.CurrencyCashFlowMonths, &CurrencyCashFlowMonthView{
			CashFlowMonth: key.cashFlowMonth, CurrencyCode: key.currencyCode, IncomeAmount: finance.FormatAmount(income),
			SpendingAmount: finance.FormatAmount(spending), NetAmount: finance.FormatAmount(new(big.Rat).Sub(income, spending)),
		})
	}
	sort.SliceStable(view.CurrencyCashFlowMonths, func(left, right int) bool {
		leftMonth, rightMonth := view.CurrencyCashFlowMonths[left], view.CurrencyCashFlowMonths[right]
		if leftMonth.CashFlowMonth != rightMonth.CashFlowMonth {
			return leftMonth.CashFlowMonth < rightMonth.CashFlowMonth
		}
		return leftMonth.CurrencyCode < rightMonth.CurrencyCode
	})
	if currencyCode == "" {
		return view, nil
	}
	for month := fromStart; !month.After(toStart); month = month.AddDate(0, 1, 0) {
		key := month.Format("2006-01")
		income, spending := incomeByMonth[key], spendingByMonth[key]
		if income == nil {
			income, spending = new(big.Rat), new(big.Rat)
		}
		view.CashFlowMonths = append(view.CashFlowMonths, &CashFlowMonthView{
			CashFlowMonth: key, IncomeAmount: finance.FormatAmount(income), SpendingAmount: finance.FormatAmount(spending),
			NetAmount: finance.FormatAmount(new(big.Rat).Sub(income, spending)),
		})
	}
	view.UnconvertedCurrencyCodes = sortedCurrencyCodes(isUnconverted)
	return view, nil
}

// --- savings targets ----------------------------------------------------

// savingsTargetView is a savings target with its progress as of today.
func (self *graph) savingsTargetView(ctx context.Context, tx db.Transaction, principal *api.Principal, agentId string, savingsTarget *models.SavingsTarget) (*SavingsTargetView, error) {
	progress, err := agent.SavingsTargetProgressOf(ctx, tx, self.exchangeRateFetcher(), agentId, savingsTarget, personToday(principal))
	if err != nil {
		return nil, financeError(err)
	}
	if savingsTarget.AssetIDs == nil {
		savingsTarget.AssetIDs = []string{}
	}
	if savingsTarget.FinanceAccountIDs == nil {
		savingsTarget.FinanceAccountIDs = []string{}
	}
	return &SavingsTargetView{SavingsTarget: savingsTarget, SavingsTargetProgress: progress}, nil
}

func (self *graph) SavingsTargets(ctx context.Context) ([]*SavingsTargetView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	savingsTargets, err := tx.ListSavingsTargets(found.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*SavingsTargetView, 0, len(savingsTargets))
	for _, savingsTarget := range savingsTargets {
		view, err := self.savingsTargetView(ctx, tx, principal, found.ID, savingsTarget)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// targetMeasureArgument is a savings target's measure, cash_flow when
// empty.
func targetMeasureArgument(value string) (models.TargetMeasure, error) {
	targetMeasure := models.TargetMeasure(strings.TrimSpace(value))
	if targetMeasure == "" {
		return models.TargetMeasureCashFlow, nil
	}
	if !targetMeasure.IsValid() {
		return "", fmt.Errorf("%w: targetMeasure %q is not cash_flow, asset_value or net_worth", api.ErrInvalidArguments, targetMeasure)
	}
	return targetMeasure, nil
}

// trimmedIds is a list of ids without blanks, never nil.
func trimmedIds(ids []string) []string {
	trimmed := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			trimmed = append(trimmed, id)
		}
	}
	return trimmed
}

// recordStartingNetWorth gives a net_worth savings target with no starting
// amount the net worth on the day it started, in its currency, so its
// progress is what net worth gained since rather than all of it.
func (self *graph) recordStartingNetWorth(ctx context.Context, tx db.Transaction, savingsTarget *models.SavingsTarget) error {
	if savingsTarget.TargetMeasure != models.TargetMeasureNetWorth || savingsTarget.StartingAmount != "" {
		return nil
	}
	startingAmount, _, err := agent.NetWorthOn(ctx, tx, self.exchangeRateFetcher(), savingsTarget.AgentID, savingsTarget.CurrencyCode, savingsTarget.StartedOn)
	if err != nil {
		return err
	}
	savingsTarget.StartingAmount = startingAmount
	return nil
}

func (self *graph) CreateSavingsTarget(ctx context.Context, arguments CreateSavingsTargetArguments) (*SavingsTargetView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	savingsTarget := &models.SavingsTarget{
		AgentID: found.ID, SavingsTargetName: strings.TrimSpace(arguments.SavingsTargetName),
		AssetIDs: trimmedIds(arguments.AssetIDs), FinanceAccountIDs: trimmedIds(arguments.FinanceAccountIDs),
	}
	if savingsTarget.TargetAmount, err = amountArgument("targetAmount", arguments.TargetAmount); err != nil {
		return nil, err
	}
	fallbackCurrency, err := reportingCurrency(tx, found, "")
	if err != nil {
		return nil, err
	}
	if savingsTarget.CurrencyCode, err = currencyArgument("currencyCode", arguments.CurrencyCode, fallbackCurrency); err != nil {
		return nil, err
	}
	if savingsTarget.CurrencyCode == "" {
		return nil, fmt.Errorf("%w: a savings target needs a currency, a code like USD", api.ErrInvalidArguments)
	}
	if savingsTarget.TargetOn, err = dayArgument("targetOn", arguments.TargetOn, ""); err != nil {
		return nil, err
	}
	if savingsTarget.TargetOn == "" {
		return nil, fmt.Errorf("%w: a savings target needs the day to reach it by", api.ErrInvalidArguments)
	}
	if savingsTarget.TargetMeasure, err = targetMeasureArgument(arguments.TargetMeasure); err != nil {
		return nil, err
	}
	if savingsTarget.StartingAmount, err = optionalAmountArgument("startingAmount", arguments.StartingAmount); err != nil {
		return nil, err
	}
	if savingsTarget.StartedOn, err = dayArgument("startedOn", arguments.StartedOn, personToday(principal)); err != nil {
		return nil, err
	}
	if err := self.recordStartingNetWorth(ctx, tx, savingsTarget); err != nil {
		return nil, financeError(err)
	}
	created, err := tx.CreateSavingsTarget(savingsTarget)
	if err != nil {
		return nil, financeError(err)
	}
	return self.savingsTargetView(ctx, tx, principal, found.ID, created)
}

func (self *graph) UpdateSavingsTarget(ctx context.Context, arguments UpdateSavingsTargetArguments) (*SavingsTargetView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	var targetAmount, currencyCode, targetOn, startingAmount, startedOn string
	var targetMeasure models.TargetMeasure
	if arguments.TargetAmount != nil {
		if targetAmount, err = amountArgument("targetAmount", *arguments.TargetAmount); err != nil {
			return nil, err
		}
	}
	if arguments.CurrencyCode != nil {
		if currencyCode, err = currencyArgument("currencyCode", *arguments.CurrencyCode, ""); err != nil {
			return nil, err
		}
	}
	if arguments.TargetOn != nil {
		if targetOn, err = dayArgument("targetOn", *arguments.TargetOn, ""); err != nil {
			return nil, err
		}
	}
	if arguments.TargetMeasure != nil {
		if targetMeasure, err = targetMeasureArgument(*arguments.TargetMeasure); err != nil {
			return nil, err
		}
	}
	if arguments.StartingAmount != nil {
		if startingAmount, err = optionalAmountArgument("startingAmount", *arguments.StartingAmount); err != nil {
			return nil, err
		}
	}
	if arguments.StartedOn != nil {
		if startedOn, err = dayArgument("startedOn", *arguments.StartedOn, ""); err != nil {
			return nil, err
		}
	}
	tx := self.writing(ctx)
	updated, err := tx.UpdateSavingsTarget(found.ID, strings.TrimSpace(arguments.SavingsTargetID), func(savingsTarget *models.SavingsTarget) error {
		if arguments.SavingsTargetName != nil {
			savingsTarget.SavingsTargetName = strings.TrimSpace(*arguments.SavingsTargetName)
		}
		if arguments.TargetAmount != nil {
			savingsTarget.TargetAmount = targetAmount
		}
		if arguments.CurrencyCode != nil && currencyCode != "" {
			savingsTarget.CurrencyCode = currencyCode
		}
		if arguments.TargetOn != nil && targetOn != "" {
			savingsTarget.TargetOn = targetOn
		}
		if arguments.TargetMeasure != nil && targetMeasure != savingsTarget.TargetMeasure {
			// What one measure started from means nothing to another.
			savingsTarget.TargetMeasure = targetMeasure
			savingsTarget.StartingAmount = ""
		}
		if arguments.StartingAmount != nil {
			savingsTarget.StartingAmount = startingAmount
		}
		if arguments.StartedOn != nil && startedOn != "" {
			savingsTarget.StartedOn = startedOn
		}
		if arguments.AssetIDs != nil {
			savingsTarget.AssetIDs = trimmedIds(arguments.AssetIDs)
		}
		if arguments.FinanceAccountIDs != nil {
			savingsTarget.FinanceAccountIDs = trimmedIds(arguments.FinanceAccountIDs)
		}
		return self.recordStartingNetWorth(ctx, tx, savingsTarget)
	})
	if err != nil {
		return nil, financeError(err)
	}
	return self.savingsTargetView(ctx, tx, principal, found.ID, updated)
}

func (self *graph) CloseSavingsTarget(ctx context.Context, arguments CloseSavingsTargetArguments) (*SavingsTargetView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	closedOn, err := dayArgument("closedOn", arguments.ClosedOn, personToday(principal))
	if err != nil {
		return nil, err
	}
	if arguments.ShouldReopen != nil && *arguments.ShouldReopen {
		closedOn = ""
	}
	tx := self.writing(ctx)
	closed, err := tx.CloseSavingsTarget(found.ID, strings.TrimSpace(arguments.SavingsTargetID), closedOn)
	if err != nil {
		return nil, financeError(err)
	}
	return self.savingsTargetView(ctx, tx, principal, found.ID, closed)
}
