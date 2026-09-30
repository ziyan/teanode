package apigraph

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Budgets: spending categories and rules, categorizing and transfers,
// budgets and their status, spending by day, cash flow and savings
// targets. Part of the finance area (FinanceQuery, FinanceMutation).

// CategorizeTransactionView is the finance transaction as categorized, and
// the spending rule made for its merchant when one was asked for.
type CategorizeTransactionView struct {
	FinanceTransaction *models.FinanceTransaction `json:"financeTransaction"`
	SpendingRule       *models.SpendingRule       `json:"spendingRule,omitempty" graphapi:"nullable"`
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
// spending category, marks transfers, or both.
type CreateSpendingRuleArguments struct {
	// MatchText is matched case-insensitively within the merchant, or the
	// description when there is no merchant.
	MatchText          string `json:"matchText"`
	SpendingCategoryID string `json:"spendingCategoryId" graphapi:"nullable"`
	IsTransfer         *bool  `json:"isTransfer" graphapi:"nullable"`

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
	IsTransfer         *bool   `json:"isTransfer" graphapi:"nullable"`
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

// MarkTransferArguments mark a finance transaction as a transfer or not.
type MarkTransferArguments struct {
	FinanceTransactionID string `json:"financeTransactionId"`
	IsTransfer           bool   `json:"isTransfer"`
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
// out.
type BudgetStatusArguments struct {
	Month string `json:"month" graphapi:"nullable"`
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
	// it started) or asset_value (what AssetIDs are worth, less
	// StartingAmount).
	TargetMeasure  string   `json:"targetMeasure" graphapi:"nullable"`
	StartingAmount string   `json:"startingAmount" graphapi:"nullable"`
	StartedOn      string   `json:"startedOn" graphapi:"nullable"`
	AssetIDs       []string `json:"assetIds" graphapi:"nullable"`
}

// UpdateSavingsTargetArguments change what is given; AssetIDs, when given,
// replace the assets it had.
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
	next := 0
	for _, spendingRule := range spendingRules {
		next = max(next, spendingRule.RulePriority+1)
	}
	return next, nil
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
	if arguments.IsTransfer != nil {
		spendingRule.IsTransfer = *arguments.IsTransfer
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
		if arguments.IsTransfer != nil {
			spendingRule.IsTransfer = *arguments.IsTransfer
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
		spendingCategory, err := tx.GetSpendingCategory(found.ID, spendingCategoryId)
		if err != nil {
			return nil, err
		}
		if spendingCategory == nil {
			return nil, fmt.Errorf("%w: there is no spending category %q; SpendingCategories lists them", api.ErrInvalidArguments, spendingCategoryId)
		}
	}
	isSpendingRuleWanted := arguments.ShouldCreateSpendingRule != nil && *arguments.ShouldCreateSpendingRule
	// The rule matches what the transaction is matched by: its merchant,
	// or its description when it has none.
	matchText := strings.TrimSpace(financeTransaction.MerchantName)
	if matchText == "" {
		matchText = strings.TrimSpace(financeTransaction.Description)
	}
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

func (self *graph) MarkTransfer(ctx context.Context, arguments MarkTransferArguments) (*models.FinanceTransaction, error) {
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
	if _, err := tx.MarkFinanceTransactionTransfer(found.ID, financeTransaction.ID, arguments.IsTransfer, models.TransferMarkedByPerson); err != nil {
		return nil, financeError(err)
	}
	return tx.GetFinanceTransaction(found.ID, financeTransaction.ID)
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
	month, err := monthArgument("month", arguments.Month, today[:len("2006-01")])
	if err != nil {
		return nil, err
	}
	status, err := agent.BudgetStatus(ctx, self.transaction(ctx), self.exchangeRateFetcher(), found.ID, month, today)
	return status, financeError(err)
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
		return "", fmt.Errorf("%w: targetMeasure %q is not cash_flow or asset_value", api.ErrInvalidArguments, targetMeasure)
	}
	return targetMeasure, nil
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
	savingsTarget := &models.SavingsTarget{AgentID: found.ID, SavingsTargetName: strings.TrimSpace(arguments.SavingsTargetName), AssetIDs: []string{}}
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
	for _, assetId := range arguments.AssetIDs {
		if assetId = strings.TrimSpace(assetId); assetId != "" {
			savingsTarget.AssetIDs = append(savingsTarget.AssetIDs, assetId)
		}
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
		if arguments.TargetMeasure != nil {
			savingsTarget.TargetMeasure = targetMeasure
		}
		if arguments.StartingAmount != nil {
			savingsTarget.StartingAmount = startingAmount
		}
		if arguments.StartedOn != nil && startedOn != "" {
			savingsTarget.StartedOn = startedOn
		}
		if arguments.AssetIDs != nil {
			savingsTarget.AssetIDs = []string{}
			for _, assetId := range arguments.AssetIDs {
				if assetId = strings.TrimSpace(assetId); assetId != "" {
					savingsTarget.AssetIDs = append(savingsTarget.AssetIDs, assetId)
				}
			}
		}
		return nil
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
