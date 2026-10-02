package db

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// BudgetOperation is a person's spending categories, spending rules,
// budgets and savings targets, and the raw spending the budget pace is
// computed from. The projection itself is the agent's. Every call takes
// the agent id and filters on it.
type BudgetOperation interface {
	// ListSpendingCategories is the agent's spending categories by name.
	ListSpendingCategories(agentId string) ([]*models.SpendingCategory, error)

	// GetSpendingCategory is one spending category of the agent, or nil.
	GetSpendingCategory(agentId, spendingCategoryId string) (*models.SpendingCategory, error)

	// CreateSpendingCategory adds a spending category. ErrAlreadyExists
	// when the agent has one of that name.
	CreateSpendingCategory(spendingCategory *models.SpendingCategory) (*models.SpendingCategory, error)

	// UpdateSpendingCategory changes a spending category of the agent
	// through a function given a copy.
	UpdateSpendingCategory(agentId, spendingCategoryId string, modify func(*models.SpendingCategory) error) (*models.SpendingCategory, error)

	// DeleteSpendingCategory removes a spending category of the agent,
	// with its budgets and the spending rules that assign it. Its finance
	// transactions become uncategorized, the person's choices included,
	// so they are categorized again. The transfer category is refused.
	DeleteSpendingCategory(agentId, spendingCategoryId string) error

	// EnsureDefaultSpendingCategories gives an agent with no spending
	// categories but the transfer category the default list, and does
	// nothing for one that has any: a person who deleted a default does
	// not get it back. It makes the transfer category too where it is
	// missing. It answers how many defaults it made.
	EnsureDefaultSpendingCategories(agentId string) (int, error)

	// EnsureTransferSpendingCategory is the agent's transfer category,
	// made when the agent has none: every agent is given one when it is
	// made, so this only mends one that somehow lost it.
	EnsureTransferSpendingCategory(agentId string) (*models.SpendingCategory, error)

	// ListSpendingRules is the agent's spending rules in the order they
	// are tried.
	ListSpendingRules(agentId string) ([]*models.SpendingRule, error)

	// GetSpendingRule is one spending rule of the agent, or nil.
	GetSpendingRule(agentId, spendingRuleId string) (*models.SpendingRule, error)

	// CreateSpendingRule adds a spending rule and applies the rules again.
	CreateSpendingRule(spendingRule *models.SpendingRule) (*models.SpendingRule, error)

	// CreateSpendingRules adds several spending rules and applies the
	// rules again once, after the last.
	CreateSpendingRules(spendingRules []*models.SpendingRule) ([]*models.SpendingRule, error)

	// UpdateSpendingRule changes a spending rule of the agent through a
	// function given a copy, and applies the rules again.
	UpdateSpendingRule(agentId, spendingRuleId string, modify func(*models.SpendingRule) error) (*models.SpendingRule, error)

	// DeleteSpendingRule removes a spending rule of the agent and applies
	// the rest again.
	DeleteSpendingRule(agentId, spendingRuleId string) error

	// SetSpendingRulePriorities moves spending rules of the agent to the
	// priorities given, by id, recording each move, without applying the
	// rules: the caller adds the rules the room was made for with
	// CreateSpendingRules, which applies them once. ErrNotFound when an id
	// is none of the agent's.
	SetSpendingRulePriorities(agentId string, rulePriorities map[string]int) error

	// FirstMatchingSpendingRules is, for each finance transaction of the
	// agent named, the id of the first spending rule by priority that
	// matches it, as ApplySpendingRules judges it; one no rule matches,
	// or that is none of the agent's, is left out.
	FirstMatchingSpendingRules(agentId string, financeTransactionIds []string) (map[string]string, error)

	// CountSpendingRuleChanges is, for each spending rule proposed, how
	// many of the agent's finance transactions it would recategorize once
	// placed ahead of the rule it names (after every rule when it names
	// none): those it matches that no earlier rule matches, whose spending
	// category would change, leaving out what ApplySpendingRules never
	// touches (the person's choices, transfers something else gave) and
	// the finance transactions excluded. One answer per proposal, in
	// order.
	CountSpendingRuleChanges(agentId string, proposedSpendingRules []*ProposedSpendingRule, excludedTransactionIds []string) ([]int, error)

	// ApplySpendingRules applies the agent's spending rules to all of its
	// finance transactions in one statement: each takes the first rule
	// that matches by priority. A spending category the person chose is
	// never touched. A spending category a rule gave that no rule matches
	// any more is cleared, to be categorized again, the transfer category
	// included. A rule does not take over a transfer something else gave
	// first (transfer detection, the provider category mapping), so
	// deleting the rule leaves that one as it was. It answers how many
	// finance transactions changed.
	ApplySpendingRules(agentId string) (int, error)

	// SetBudget keeps the monthly amount of a spending category from a
	// month on, replacing the one already set from that same month. On an
	// income spending category it is the income expected each month. The
	// transfer category, which is neither spending nor income, is refused.
	SetBudget(budget *models.Budget) (*models.Budget, error)

	// ListBudgets is every budget row of the agent, by spending category
	// and month.
	ListBudgets(agentId string) ([]*models.Budget, error)

	// BudgetsForMonth is the budget in force in a month ("2006-01") for
	// each spending category: the row with the latest EffectiveFrom on or
	// before it. A budget ended by a zero is left out.
	BudgetsForMonth(agentId, month string) ([]*models.Budget, error)

	// ListSavingsTargets is the agent's savings targets, open ones first.
	ListSavingsTargets(agentId string) ([]*models.SavingsTarget, error)

	// GetSavingsTarget is one savings target of the agent, or nil.
	GetSavingsTarget(agentId, savingsTargetId string) (*models.SavingsTarget, error)

	// CreateSavingsTarget adds a savings target with the assets and
	// finance accounts it
	// measures, each of which must be the agent's.
	CreateSavingsTarget(savingsTarget *models.SavingsTarget) (*models.SavingsTarget, error)

	// UpdateSavingsTarget changes a savings target of the agent through a
	// function given a copy; its AssetIDs and FinanceAccountIDs replace
	// the ones it had.
	UpdateSavingsTarget(agentId, savingsTargetId string, modify func(*models.SavingsTarget) error) (*models.SavingsTarget, error)

	// CloseSavingsTarget records the day a savings target was closed; an
	// empty day opens it again.
	CloseSavingsTarget(agentId, savingsTargetId, closedOn string) (*models.SavingsTarget, error)

	// DeleteSavingsTarget removes a savings target of the agent.
	DeleteSavingsTarget(agentId, savingsTargetId string) error

	// ListSpendingCategoryDays is each day's spending in a month
	// ("2006-01") per spending category and currency: money out less
	// refunds in the same spending category, the transfer category,
	// income categories and mirrored copies left out. Money in that is not
	// categorized is left out too, since it may be income; money out that
	// is not categorized is counted under an empty spending category.
	ListSpendingCategoryDays(agentId, month string) ([]*models.SpendingCategoryDay, error)

	// ListIncomeCategoryDays is each day's income in a month ("2006-01")
	// per income spending category and currency: money in less money
	// taken back, transfers and mirrored copies left out. Money in that is
	// not categorized is left out, since no income budget can count it;
	// ListCashFlowDays counts it as income.
	ListIncomeCategoryDays(agentId, month string) ([]*models.IncomeCategoryDay, error)

	// ListCashFlowDays is each day's income and spending per currency from
	// one day to another, both included ("2006-01-02"), counted as
	// ListSpendingCategoryDays counts spending, so the two agree: a refund
	// in a spending category lowers spending rather than counting as
	// income. Transfers and mirrored copies are left out.
	ListCashFlowDays(agentId, from, to string) ([]*models.CashFlowDay, error)

	// ListMerchantMonthSpending is what each merchant charged each
	// spending category in each of the three full months before a month
	// ("2006-01"), per currency, transfers and mirrored copies left out:
	// what the budget pace reads to expect a fixed monthly charge.
	ListMerchantMonthSpending(agentId, month string) ([]*models.MerchantMonthSpending, error)
}

// --- spending categories -------------------------------------------------

type agentSpendingCategoryModel struct {
	ID                       string    `gorm:"column:id;primaryKey"`
	AgentID                  string    `gorm:"column:agent_id"`
	SpendingCategoryName     string    `gorm:"column:spending_category_name"`
	ParentSpendingCategoryID *string   `gorm:"column:parent_spending_category_id"`
	IsIncome                 bool      `gorm:"column:is_income"`
	IsHidden                 bool      `gorm:"column:is_hidden"`
	IsTransfer               bool      `gorm:"column:is_transfer"`
	CreatedAt                time.Time `gorm:"column:created_at"`
	ModifiedAt               time.Time `gorm:"column:modified_at"`
}

func (agentSpendingCategoryModel) TableName() string { return "agent_spending_category" }

func (self *agentSpendingCategoryModel) toModel() *models.SpendingCategory {
	return &models.SpendingCategory{
		ID: self.ID, AgentID: self.AgentID, SpendingCategoryName: self.SpendingCategoryName,
		ParentSpendingCategoryID: optionalString(self.ParentSpendingCategoryID), IsIncome: self.IsIncome, IsHidden: self.IsHidden,
		IsTransfer: self.IsTransfer, CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

func spendingCategoryToModel(spendingCategory *models.SpendingCategory) *agentSpendingCategoryModel {
	return &agentSpendingCategoryModel{
		ID: spendingCategory.ID, AgentID: spendingCategory.AgentID, SpendingCategoryName: spendingCategory.SpendingCategoryName,
		ParentSpendingCategoryID: optionalID(spendingCategory.ParentSpendingCategoryID),
		IsIncome:                 spendingCategory.IsIncome, IsHidden: spendingCategory.IsHidden, IsTransfer: spendingCategory.IsTransfer,
		CreatedAt: spendingCategory.CreatedAt, ModifiedAt: spendingCategory.ModifiedAt,
	}
}

// validateSpendingCategory checks a spending category before it is
// written: a name no other of the agent's has, and a parent of the agent's
// that is itself top-level, since there is one level of parents. The
// transfer category stands alone: not income, with no parent and no
// children, since a transfer counted as income, or spending filed under
// it, would be counted where transfers are left out.
func (self *transaction) validateSpendingCategory(spendingCategory *models.SpendingCategory) error {
	spendingCategory.SpendingCategoryName = strings.TrimSpace(spendingCategory.SpendingCategoryName)
	if spendingCategory.AgentID == "" || spendingCategory.SpendingCategoryName == "" {
		return fmt.Errorf("%w: a spending category needs an agent and a name", ErrInvalidArguments)
	}
	if spendingCategory.IsTransfer && spendingCategory.IsIncome {
		return fmt.Errorf("%w: the transfer category is neither spending nor income", ErrInvalidArguments)
	}
	if spendingCategory.IsTransfer && spendingCategory.ParentSpendingCategoryID != "" {
		return fmt.Errorf("%w: the transfer category cannot have a parent", ErrInvalidArguments)
	}
	var sameNameCount int64
	if err := self.tx.Model(&agentSpendingCategoryModel{}).
		Where(`"agent_id" = ? AND "spending_category_name" = ? AND "id" <> ?`, spendingCategory.AgentID, spendingCategory.SpendingCategoryName, spendingCategory.ID).
		Count(&sameNameCount).Error; err != nil {
		return err
	}
	if sameNameCount > 0 {
		return ErrAlreadyExists
	}
	if spendingCategory.ParentSpendingCategoryID == "" {
		return nil
	}
	if spendingCategory.ParentSpendingCategoryID == spendingCategory.ID {
		return fmt.Errorf("%w: a spending category cannot be its own parent", ErrInvalidArguments)
	}
	parent, err := self.GetSpendingCategory(spendingCategory.AgentID, spendingCategory.ParentSpendingCategoryID)
	if err != nil {
		return err
	}
	if parent == nil {
		return ErrNotFound
	}
	if parent.ParentSpendingCategoryID != "" {
		return fmt.Errorf("%w: a spending category's parent must not have a parent of its own", ErrInvalidArguments)
	}
	if parent.IsTransfer {
		return fmt.Errorf("%w: the transfer category cannot have children", ErrInvalidArguments)
	}
	if spendingCategory.ID != "" {
		var childCount int64
		if err := self.tx.Model(&agentSpendingCategoryModel{}).
			Where(`"agent_id" = ? AND "parent_spending_category_id" = ?`, spendingCategory.AgentID, spendingCategory.ID).
			Count(&childCount).Error; err != nil {
			return err
		}
		if childCount > 0 {
			return fmt.Errorf("%w: a spending category with children of its own cannot have a parent", ErrInvalidArguments)
		}
	}
	return nil
}

func (self *transaction) ListSpendingCategories(agentId string) ([]*models.SpendingCategory, error) {
	var found []agentSpendingCategoryModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"spending_category_name" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	spendingCategories := make([]*models.SpendingCategory, 0, len(found))
	for index := range found {
		spendingCategories = append(spendingCategories, found[index].toModel())
	}
	return spendingCategories, nil
}

func (self *transaction) GetSpendingCategory(agentId, spendingCategoryId string) (*models.SpendingCategory, error) {
	var found []agentSpendingCategoryModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingCategoryId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) CreateSpendingCategory(spendingCategory *models.SpendingCategory) (*models.SpendingCategory, error) {
	created := *spendingCategory
	// The transfer category is built in, one per agent, and made only by
	// EnsureTransferSpendingCategory.
	created.ID, created.IsTransfer = "", false
	if err := self.validateSpendingCategory(&created); err != nil {
		return nil, err
	}
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if err := self.applyMutation(models.AuditResourceSpendingCategory, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(spendingCategoryToModel(&created)).Error
	}); err != nil {
		return nil, err
	}
	return self.GetSpendingCategory(created.AgentID, created.ID)
}

func (self *transaction) UpdateSpendingCategory(agentId, spendingCategoryId string, modify func(*models.SpendingCategory) error) (*models.SpendingCategory, error) {
	var found []agentSpendingCategoryModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingCategoryId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	before := found[0].toModel()
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	after.ID, after.AgentID, after.CreatedAt, after.IsTransfer = before.ID, before.AgentID, before.CreatedAt, before.IsTransfer
	if err := self.validateSpendingCategory(&after); err != nil {
		return nil, err
	}
	after.ModifiedAt = time.Now()
	model := spendingCategoryToModel(&after)
	if err := self.applyMutation(models.AuditResourceSpendingCategory, spendingCategoryId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&agentSpendingCategoryModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingCategoryId).Updates(map[string]any{
			"spending_category_name": model.SpendingCategoryName, "parent_spending_category_id": model.ParentSpendingCategoryID,
			"is_income": model.IsIncome, "is_hidden": model.IsHidden, "modified_at": model.ModifiedAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	return self.GetSpendingCategory(agentId, spendingCategoryId)
}

func (self *transaction) DeleteSpendingCategory(agentId, spendingCategoryId string) error {
	before, err := self.GetSpendingCategory(agentId, spendingCategoryId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	if before.IsTransfer {
		return fmt.Errorf("%w: the transfer category is built in and cannot be deleted", ErrInvalidArguments)
	}
	return self.applyMutation(models.AuditResourceSpendingCategory, spendingCategoryId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		// The foreign key would leave categorized_by saying who chose a
		// spending category that is gone, and the transaction would never
		// be categorized again.
		if err := tx.Exec(`UPDATE "agent_finance_transaction" SET "spending_category_id" = NULL, "categorized_by" = '',
				"categorization_confidence" = NULL, "categorize_attempted_at" = NULL, "modified_at" = ?
			WHERE "agent_id" = ? AND "spending_category_id" = ?`, time.Now(), agentId, spendingCategoryId).Error; err != nil {
			return err
		}
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingCategoryId).Delete(&agentSpendingCategoryModel{}).Error
	})
}

func (self *transaction) EnsureDefaultSpendingCategories(agentId string) (int, error) {
	if agentId == "" {
		return 0, fmt.Errorf("%w: default spending categories need an agent", ErrInvalidArguments)
	}
	if _, err := self.EnsureTransferSpendingCategory(agentId); err != nil {
		return 0, err
	}
	var existingCount int64
	if err := self.tx.Model(&agentSpendingCategoryModel{}).Where(`"agent_id" = ? AND NOT "is_transfer"`, agentId).Count(&existingCount).Error; err != nil {
		return 0, err
	}
	if existingCount > 0 {
		return 0, nil
	}
	for _, spendingCategoryName := range finance.DefaultSpendingCategoryNames {
		if _, err := self.CreateSpendingCategory(&models.SpendingCategory{
			AgentID: agentId, SpendingCategoryName: spendingCategoryName,
			IsIncome: spendingCategoryName == finance.SpendingCategoryIncome,
		}); err != nil {
			return 0, err
		}
	}
	return len(finance.DefaultSpendingCategoryNames), nil
}

func (self *transaction) EnsureTransferSpendingCategory(agentId string) (*models.SpendingCategory, error) {
	if agentId == "" {
		return nil, fmt.Errorf("%w: the transfer category needs an agent", ErrInvalidArguments)
	}
	found, err := self.transferSpendingCategory(agentId)
	if err != nil || found != nil {
		return found, err
	}
	// A spending category the person named transfer, in any case, stays
	// theirs, as migration 0143 left it.
	name := finance.SpendingCategoryTransfer
	var sameNameCount int64
	if err := self.tx.Model(&agentSpendingCategoryModel{}).
		Where(`"agent_id" = ? AND lower("spending_category_name") = ?`, agentId, finance.SpendingCategoryTransfer).
		Count(&sameNameCount).Error; err != nil {
		return nil, err
	}
	if sameNameCount > 0 {
		name = finance.SpendingCategoryTransferFallback
	}
	now := time.Now()
	created := &models.SpendingCategory{ID: newID(), AgentID: agentId, SpendingCategoryName: name, IsTransfer: true, CreatedAt: now, ModifiedAt: now}
	if err := self.applyMutation(models.AuditResourceSpendingCategory, created.ID, models.AuditActionCreate, nil, created, func(tx *gorm.DB) error {
		return tx.Create(spendingCategoryToModel(created)).Error
	}); err != nil {
		return nil, err
	}
	return self.transferSpendingCategory(agentId)
}

// transferSpendingCategory is the agent's transfer category, or nil.
func (self *transaction) transferSpendingCategory(agentId string) (*models.SpendingCategory, error) {
	var found []agentSpendingCategoryModel
	if err := self.tx.Where(`"agent_id" = ? AND "is_transfer"`, agentId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// --- spending rules --------------------------------------------------------

// ProposedSpendingRule is a spending rule not yet saved, for
// CountSpendingRuleChanges: what it matches, what it assigns, and the
// existing rule it would be placed ahead of, empty for after every rule.
type ProposedSpendingRule struct {
	MatchText             string
	SpendingCategoryID    string
	AheadOfSpendingRuleID string
}

// spendingRuleMatchesCandidate is when the spending rule "rule" matches
// the finance transaction "candidate": its words within the merchant, or
// the description when there is none, in any case, and its finance account
// and amounts when it names them. Every query that asks which rule applies
// uses it, so none can disagree with ApplySpendingRules.
const spendingRuleMatchesCandidate = `"rule"."agent_id" = "candidate"."agent_id"
	AND strpos(lower(CASE WHEN "candidate"."merchant_name" <> '' THEN "candidate"."merchant_name" ELSE "candidate"."description" END),
	           lower("rule"."match_text")) > 0
	AND ("rule"."finance_account_id" IS NULL OR "rule"."finance_account_id" = "candidate"."finance_account_id")
	AND ("rule"."minimum_amount" IS NULL OR "candidate"."amount" >= "rule"."minimum_amount")
	AND ("rule"."maximum_amount" IS NULL OR "candidate"."amount" <= "rule"."maximum_amount")`

// candidateKeptFromRules is when spending rules leave the finance
// transaction "candidate" as it is, its spending category joined as
// "current_category": the person chose it, or it is a transfer something
// other than a rule gave.
const candidateKeptFromRules = `("candidate"."categorized_by" = 'person'
	OR (COALESCE("current_category"."is_transfer", false) AND "candidate"."categorized_by" <> 'spending_rule'))`

type agentSpendingRuleModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	AgentID            string    `gorm:"column:agent_id"`
	MatchText          string    `gorm:"column:match_text"`
	FinanceAccountID   *string   `gorm:"column:finance_account_id"`
	MinimumAmount      *string   `gorm:"column:minimum_amount"`
	MaximumAmount      *string   `gorm:"column:maximum_amount"`
	SpendingCategoryID string    `gorm:"column:spending_category_id"`
	RulePriority       int       `gorm:"column:rule_priority"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	ModifiedAt         time.Time `gorm:"column:modified_at"`
}

func (agentSpendingRuleModel) TableName() string { return "agent_spending_rule" }

func (self *agentSpendingRuleModel) toModel() *models.SpendingRule {
	return &models.SpendingRule{
		ID: self.ID, AgentID: self.AgentID, MatchText: self.MatchText, FinanceAccountID: optionalString(self.FinanceAccountID),
		MinimumAmount: optionalString(self.MinimumAmount), MaximumAmount: optionalString(self.MaximumAmount),
		SpendingCategoryID: self.SpendingCategoryID, RulePriority: self.RulePriority,
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

// validateSpendingRule checks a spending rule before it is written and
// writes its amounts the way the columns keep them. What it names must be
// the agent's.
func (self *transaction) validateSpendingRule(spendingRule *models.SpendingRule) (*agentSpendingRuleModel, error) {
	spendingRule.MatchText = strings.TrimSpace(spendingRule.MatchText)
	if spendingRule.AgentID == "" || spendingRule.MatchText == "" {
		return nil, fmt.Errorf("%w: a spending rule needs an agent and a text to match", ErrInvalidArguments)
	}
	if spendingRule.SpendingCategoryID == "" {
		return nil, fmt.Errorf("%w: a spending rule assigns a spending category; the transfer category marks transfers", ErrInvalidArguments)
	}
	minimumAmount, err := canonicalOptionalAmount("minimum amount", spendingRule.MinimumAmount)
	if err != nil {
		return nil, err
	}
	maximumAmount, err := canonicalOptionalAmount("maximum amount", spendingRule.MaximumAmount)
	if err != nil {
		return nil, err
	}
	spendingRule.MinimumAmount, spendingRule.MaximumAmount = optionalString(minimumAmount), optionalString(maximumAmount)
	spendingCategory, err := self.GetSpendingCategory(spendingRule.AgentID, spendingRule.SpendingCategoryID)
	if err != nil {
		return nil, err
	}
	if spendingCategory == nil {
		return nil, ErrNotFound
	}
	if spendingRule.FinanceAccountID != "" {
		account, err := self.GetFinanceAccount(spendingRule.AgentID, spendingRule.FinanceAccountID)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return nil, ErrNotFound
		}
	}
	return &agentSpendingRuleModel{
		ID: spendingRule.ID, AgentID: spendingRule.AgentID, MatchText: spendingRule.MatchText,
		FinanceAccountID: optionalID(spendingRule.FinanceAccountID), MinimumAmount: minimumAmount, MaximumAmount: maximumAmount,
		SpendingCategoryID: spendingRule.SpendingCategoryID,
		RulePriority:       spendingRule.RulePriority, CreatedAt: spendingRule.CreatedAt, ModifiedAt: spendingRule.ModifiedAt,
	}, nil
}

func (self *transaction) ListSpendingRules(agentId string) ([]*models.SpendingRule, error) {
	var found []agentSpendingRuleModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"rule_priority" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	spendingRules := make([]*models.SpendingRule, 0, len(found))
	for index := range found {
		spendingRules = append(spendingRules, found[index].toModel())
	}
	return spendingRules, nil
}

func (self *transaction) GetSpendingRule(agentId, spendingRuleId string) (*models.SpendingRule, error) {
	var found []agentSpendingRuleModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) CreateSpendingRule(spendingRule *models.SpendingRule) (*models.SpendingRule, error) {
	created, err := self.CreateSpendingRules([]*models.SpendingRule{spendingRule})
	if err != nil {
		return nil, err
	}
	return created[0], nil
}

func (self *transaction) CreateSpendingRules(spendingRules []*models.SpendingRule) ([]*models.SpendingRule, error) {
	createdIds := make([]string, 0, len(spendingRules))
	agentIds := []string{}
	for _, spendingRule := range spendingRules {
		created := *spendingRule
		created.ID = newID()
		created.CreatedAt = time.Now()
		created.ModifiedAt = created.CreatedAt
		model, err := self.validateSpendingRule(&created)
		if err != nil {
			return nil, err
		}
		if err := self.applyMutation(models.AuditResourceSpendingRule, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
			return tx.Create(model).Error
		}); err != nil {
			return nil, err
		}
		createdIds = append(createdIds, created.ID)
		if !slices.Contains(agentIds, created.AgentID) {
			agentIds = append(agentIds, created.AgentID)
		}
	}
	for _, agentId := range agentIds {
		if _, err := self.ApplySpendingRules(agentId); err != nil {
			return nil, err
		}
	}
	createdRules := make([]*models.SpendingRule, 0, len(createdIds))
	for index, createdId := range createdIds {
		createdRule, err := self.GetSpendingRule(spendingRules[index].AgentID, createdId)
		if err != nil {
			return nil, err
		}
		createdRules = append(createdRules, createdRule)
	}
	return createdRules, nil
}

func (self *transaction) UpdateSpendingRule(agentId, spendingRuleId string, modify func(*models.SpendingRule) error) (*models.SpendingRule, error) {
	var found []agentSpendingRuleModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	before := found[0].toModel()
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	after.ID, after.AgentID, after.CreatedAt = before.ID, before.AgentID, before.CreatedAt
	after.ModifiedAt = time.Now()
	model, err := self.validateSpendingRule(&after)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceSpendingRule, spendingRuleId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&agentSpendingRuleModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).Updates(map[string]any{
			"match_text": model.MatchText, "finance_account_id": model.FinanceAccountID,
			"minimum_amount": model.MinimumAmount, "maximum_amount": model.MaximumAmount,
			"spending_category_id": model.SpendingCategoryID, "rule_priority": model.RulePriority, "modified_at": model.ModifiedAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	if _, err := self.ApplySpendingRules(agentId); err != nil {
		return nil, err
	}
	return self.GetSpendingRule(agentId, spendingRuleId)
}

func (self *transaction) DeleteSpendingRule(agentId, spendingRuleId string) error {
	before, err := self.GetSpendingRule(agentId, spendingRuleId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	if err := self.applyMutation(models.AuditResourceSpendingRule, spendingRuleId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).Delete(&agentSpendingRuleModel{}).Error
	}); err != nil {
		return err
	}
	_, err = self.ApplySpendingRules(agentId)
	return err
}

func (self *transaction) SetSpendingRulePriorities(agentId string, rulePriorities map[string]int) error {
	spendingRuleIds := make([]string, 0, len(rulePriorities))
	for spendingRuleId := range rulePriorities {
		spendingRuleIds = append(spendingRuleIds, spendingRuleId)
	}
	// In a fixed order, so two of these never wait on each other's locks.
	slices.Sort(spendingRuleIds)
	for _, spendingRuleId := range spendingRuleIds {
		var found []agentSpendingRuleModel
		if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).
			Limit(1).Find(&found).Error; err != nil {
			return err
		}
		if len(found) == 0 {
			return ErrNotFound
		}
		before := found[0].toModel()
		if before.RulePriority == rulePriorities[spendingRuleId] {
			continue
		}
		after := *before
		after.RulePriority = rulePriorities[spendingRuleId]
		after.ModifiedAt = time.Now()
		if err := self.applyMutation(models.AuditResourceSpendingRule, spendingRuleId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
			return tx.Model(&agentSpendingRuleModel{}).Where(`"agent_id" = ? AND "id" = ?`, agentId, spendingRuleId).
				Updates(map[string]any{"rule_priority": after.RulePriority, "modified_at": after.ModifiedAt}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) FirstMatchingSpendingRules(agentId string, financeTransactionIds []string) (map[string]string, error) {
	firstMatching := map[string]string{}
	if len(financeTransactionIds) == 0 {
		return firstMatching, nil
	}
	var found []struct {
		FinanceTransactionID string `gorm:"column:finance_transaction_id"`
		SpendingRuleID       string `gorm:"column:spending_rule_id"`
	}
	if err := self.tx.Raw(`SELECT "candidate"."id" AS "finance_transaction_id", "matched"."id" AS "spending_rule_id"
		FROM "agent_finance_transaction" AS "candidate"
		CROSS JOIN LATERAL (
			SELECT "rule"."id"
			FROM "agent_spending_rule" AS "rule"
			WHERE `+spendingRuleMatchesCandidate+`
			ORDER BY "rule"."rule_priority" ASC, "rule"."id" ASC
			LIMIT 1
		) AS "matched"
		WHERE "candidate"."agent_id" = ? AND "candidate"."id" = ANY(?::text[])`,
		agentId, pq.Array(financeTransactionIds)).Scan(&found).Error; err != nil {
		return nil, err
	}
	for _, row := range found {
		firstMatching[row.FinanceTransactionID] = row.SpendingRuleID
	}
	return firstMatching, nil
}

func (self *transaction) CountSpendingRuleChanges(agentId string, proposedSpendingRules []*ProposedSpendingRule, excludedTransactionIds []string) ([]int, error) {
	changedCounts := make([]int, len(proposedSpendingRules))
	if len(proposedSpendingRules) == 0 {
		return changedCounts, nil
	}
	matchTexts := make([]string, 0, len(proposedSpendingRules))
	spendingCategoryIds := make([]string, 0, len(proposedSpendingRules))
	aheadOfSpendingRuleIds := make([]string, 0, len(proposedSpendingRules))
	for _, proposed := range proposedSpendingRules {
		matchTexts = append(matchTexts, strings.TrimSpace(proposed.MatchText))
		spendingCategoryIds = append(spendingCategoryIds, proposed.SpendingCategoryID)
		aheadOfSpendingRuleIds = append(aheadOfSpendingRuleIds, proposed.AheadOfSpendingRuleID)
	}
	if excludedTransactionIds == nil {
		excludedTransactionIds = []string{}
	}
	// A proposed rule wins where no rule matches, or where the first rule
	// that does is the one it goes ahead of or a later one; of those, only
	// the rows ApplySpendingRules would give another spending category.
	var found []struct {
		ProposalIndex int `gorm:"column:proposal_index"`
		ChangedCount  int `gorm:"column:changed_count"`
	}
	if err := self.tx.Raw(`WITH "proposed" AS (
			SELECT "listed"."match_text", "listed"."spending_category_id", "listed"."proposal_index",
				"ahead"."rule_priority" AS "ahead_priority", "ahead"."id" AS "ahead_id"
			FROM unnest(CAST(@match_texts AS text[]), CAST(@spending_category_ids AS text[]), CAST(@ahead_of_spending_rule_ids AS text[]))
				WITH ORDINALITY AS "listed"("match_text", "spending_category_id", "ahead_of_spending_rule_id", "proposal_index")
			LEFT JOIN "agent_spending_rule" AS "ahead"
			  ON "ahead"."agent_id" = @agent_id AND "ahead"."id" = "listed"."ahead_of_spending_rule_id"
		)
		SELECT "proposed"."proposal_index", count(*) AS "changed_count"
		FROM "agent_finance_transaction" AS "candidate"
		LEFT JOIN "agent_spending_category" AS "current_category"
		  ON "current_category"."id" = "candidate"."spending_category_id" AND "current_category"."agent_id" = "candidate"."agent_id"
		LEFT JOIN LATERAL (
			SELECT "rule"."rule_priority", "rule"."id"
			FROM "agent_spending_rule" AS "rule"
			WHERE `+spendingRuleMatchesCandidate+`
			ORDER BY "rule"."rule_priority" ASC, "rule"."id" ASC
			LIMIT 1
		) AS "matched" ON true
		JOIN "proposed"
		  ON strpos(lower(CASE WHEN "candidate"."merchant_name" <> '' THEN "candidate"."merchant_name" ELSE "candidate"."description" END),
		            lower("proposed"."match_text")) > 0
		WHERE "candidate"."agent_id" = @agent_id
		  AND NOT `+candidateKeptFromRules+`
		  AND NOT ("candidate"."id" = ANY(CAST(@excluded_transaction_ids AS text[])))
		  AND ("matched"."id" IS NULL
		       OR ("proposed"."ahead_id" IS NOT NULL
		           AND ("matched"."rule_priority", "matched"."id") >= ("proposed"."ahead_priority", "proposed"."ahead_id")))
		  AND "candidate"."spending_category_id" IS DISTINCT FROM "proposed"."spending_category_id"
		GROUP BY "proposed"."proposal_index"`,
		map[string]any{
			"agent_id": agentId, "match_texts": pq.Array(matchTexts), "spending_category_ids": pq.Array(spendingCategoryIds),
			"ahead_of_spending_rule_ids": pq.Array(aheadOfSpendingRuleIds), "excluded_transaction_ids": pq.Array(excludedTransactionIds),
		}).Scan(&found).Error; err != nil {
		return nil, err
	}
	for _, row := range found {
		if row.ProposalIndex >= 1 && row.ProposalIndex <= len(changedCounts) {
			changedCounts[row.ProposalIndex-1] = row.ChangedCount
		}
	}
	return changedCounts, nil
}

func (self *transaction) ApplySpendingRules(agentId string) (int, error) {
	// For every finance transaction of the agent, the first rule by
	// priority that matches it, if any; then what that decides, keeping
	// the person's choices and the transfers something other than a rule
	// gave; then only the rows whose outcome differs.
	updated := self.tx.Exec(`UPDATE "agent_finance_transaction" AS "target" SET
			"spending_category_id" = "decided"."spending_category_id",
			"categorized_by" = "decided"."categorized_by",
			"categorization_confidence" = "decided"."categorization_confidence",
			"categorize_attempted_at" = CASE WHEN "decided"."categorized_by" = 'spending_rule' THEN NULL ELSE "target"."categorize_attempted_at" END,
			"modified_at" = @modified_at
		FROM (
			SELECT "candidate"."id",
				CASE WHEN "kept"."is_kept" THEN "candidate"."spending_category_id"
				     WHEN "matched"."spending_category_id" IS NOT NULL THEN "matched"."spending_category_id"
				     WHEN "candidate"."categorized_by" = 'spending_rule' THEN NULL
				     ELSE "candidate"."spending_category_id" END AS "spending_category_id",
				CASE WHEN "kept"."is_kept" THEN "candidate"."categorized_by"
				     WHEN "matched"."spending_category_id" IS NOT NULL THEN 'spending_rule'
				     WHEN "candidate"."categorized_by" = 'spending_rule' THEN ''
				     ELSE "candidate"."categorized_by" END AS "categorized_by",
				CASE WHEN "kept"."is_kept" THEN "candidate"."categorization_confidence"
				     WHEN "matched"."spending_category_id" IS NOT NULL OR "candidate"."categorized_by" = 'spending_rule' THEN NULL
				     ELSE "candidate"."categorization_confidence" END AS "categorization_confidence"
			FROM "agent_finance_transaction" AS "candidate"
			LEFT JOIN "agent_spending_category" AS "current_category"
			  ON "current_category"."id" = "candidate"."spending_category_id" AND "current_category"."agent_id" = "candidate"."agent_id"
			CROSS JOIN LATERAL (
				SELECT `+candidateKeptFromRules+` AS "is_kept"
			) AS "kept"
			LEFT JOIN LATERAL (
				SELECT "rule"."spending_category_id"
				FROM "agent_spending_rule" AS "rule"
				WHERE `+spendingRuleMatchesCandidate+`
				ORDER BY "rule"."rule_priority" ASC, "rule"."id" ASC
				LIMIT 1
			) AS "matched" ON true
			WHERE "candidate"."agent_id" = @agent_id
		) AS "decided"
		WHERE "target"."id" = "decided"."id" AND "target"."agent_id" = @agent_id
		  AND ("target"."spending_category_id", "target"."categorized_by", "target"."categorization_confidence")
		      IS DISTINCT FROM ("decided"."spending_category_id", "decided"."categorized_by", "decided"."categorization_confidence")`,
		map[string]any{"agent_id": agentId, "modified_at": time.Now()})
	if updated.Error != nil {
		return 0, updated.Error
	}
	return int(updated.RowsAffected), nil
}

// --- budgets ---------------------------------------------------------------

type agentBudgetModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	AgentID            string    `gorm:"column:agent_id"`
	SpendingCategoryID string    `gorm:"column:spending_category_id"`
	MonthlyAmount      string    `gorm:"column:monthly_amount"`
	CurrencyCode       string    `gorm:"column:currency_code"`
	EffectiveFrom      time.Time `gorm:"column:effective_from"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	ModifiedAt         time.Time `gorm:"column:modified_at"`
}

func (agentBudgetModel) TableName() string { return "agent_budget" }

func (self *agentBudgetModel) toModel() *models.Budget {
	return &models.Budget{
		ID: self.ID, AgentID: self.AgentID, SpendingCategoryID: self.SpendingCategoryID, MonthlyAmount: self.MonthlyAmount,
		CurrencyCode: self.CurrencyCode, EffectiveFrom: formatDay(self.EffectiveFrom),
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

func (self *transaction) SetBudget(budget *models.Budget) (*models.Budget, error) {
	if budget == nil || budget.AgentID == "" || budget.SpendingCategoryID == "" || strings.TrimSpace(budget.CurrencyCode) == "" {
		return nil, fmt.Errorf("%w: a budget needs an agent, a spending category and a currency", ErrInvalidArguments)
	}
	monthlyAmount, err := canonicalAmount("monthly amount", budget.MonthlyAmount)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(monthlyAmount, "-") {
		return nil, fmt.Errorf("%w: a budget's monthly amount cannot be negative", ErrInvalidArguments)
	}
	effectiveFrom, err := parseMonth(budget.EffectiveFrom)
	if err != nil {
		return nil, err
	}
	spendingCategory, err := self.GetSpendingCategory(budget.AgentID, budget.SpendingCategoryID)
	if err != nil {
		return nil, err
	}
	if spendingCategory == nil {
		return nil, ErrNotFound
	}
	if spendingCategory.IsTransfer {
		return nil, fmt.Errorf("%w: transfers are neither spending nor income, so the transfer category takes no budget", ErrInvalidArguments)
	}
	now := time.Now()
	written := &models.Budget{
		ID: newID(), AgentID: budget.AgentID, SpendingCategoryID: budget.SpendingCategoryID, MonthlyAmount: monthlyAmount,
		CurrencyCode: strings.TrimSpace(budget.CurrencyCode), EffectiveFrom: effectiveFrom.Format(time.DateOnly),
		CreatedAt: now, ModifiedAt: now,
	}
	// What was in force from this month before the change: the later rows
	// that only repeated it are dropped below, with any that repeat the new
	// amount, so changing a budget from its start month changes all of it.
	var replaced []agentBudgetModel
	if err := self.tx.Where(`"agent_id" = ? AND "spending_category_id" = ? AND "effective_from" <= ?::date`,
		budget.AgentID, budget.SpendingCategoryID, written.EffectiveFrom).Order(`"effective_from" DESC`).Limit(1).Find(&replaced).Error; err != nil {
		return nil, err
	}
	var existing []agentBudgetModel
	if err := self.tx.Where(`"agent_id" = ? AND "spending_category_id" = ? AND "effective_from" = ?::date`,
		budget.AgentID, budget.SpendingCategoryID, written.EffectiveFrom).Limit(1).Find(&existing).Error; err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		before := existing[0].toModel()
		written.ID, written.CreatedAt = before.ID, before.CreatedAt
		if err := self.applyMutation(models.AuditResourceBudget, before.ID, models.AuditActionUpdate, before, written, func(tx *gorm.DB) error {
			return tx.Model(&agentBudgetModel{}).Where(`"agent_id" = ? AND "id" = ?`, budget.AgentID, before.ID).Updates(map[string]any{
				"monthly_amount": written.MonthlyAmount, "currency_code": written.CurrencyCode, "modified_at": now,
			}).Error
		}); err != nil {
			return nil, err
		}
	} else {
		if err := self.applyMutation(models.AuditResourceBudget, written.ID, models.AuditActionCreate, nil, written, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO "agent_budget" ("id", "agent_id", "spending_category_id", "monthly_amount", "currency_code",
					"effective_from", "created_at", "modified_at") VALUES (?, ?, ?, ?::numeric, ?, ?::date, ?, ?)`,
				written.ID, written.AgentID, written.SpendingCategoryID, written.MonthlyAmount, written.CurrencyCode,
				written.EffectiveFrom, now, now).Error
		}); err != nil {
			return nil, err
		}
	}
	if err := self.dropRepeatedBudgets(written, replaced); err != nil {
		return nil, err
	}
	var found []agentBudgetModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, budget.AgentID, written.ID).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("db: the budget was not kept")
	}
	return found[0].toModel(), nil
}

// dropRepeatedBudgets deletes the rows after written, in month order, that
// repeat either the budget written or the one it replaced (the row in force
// from written's month before, if any), stopping at the first row that is a
// real change. A row that only repeated the old amount would otherwise
// bring it back from its month, and one that repeats the new amount says
// nothing. A later change to another amount is kept.
func (self *transaction) dropRepeatedBudgets(written *models.Budget, replaced []agentBudgetModel) error {
	var later []agentBudgetModel
	if err := self.tx.Where(`"agent_id" = ? AND "spending_category_id" = ? AND "effective_from" > ?::date`,
		written.AgentID, written.SpendingCategoryID, written.EffectiveFrom).Order(`"effective_from" ASC`).Find(&later).Error; err != nil {
		return err
	}
	isSame := func(row *models.Budget, monthlyAmount, currencyCode string) bool {
		rowAmount, rowErr := canonicalAmount("monthly amount", row.MonthlyAmount)
		otherAmount, otherErr := canonicalAmount("monthly amount", monthlyAmount)
		return rowErr == nil && otherErr == nil && rowAmount == otherAmount && row.CurrencyCode == currencyCode
	}
	for index := range later {
		row := later[index].toModel()
		isRepeat := isSame(row, written.MonthlyAmount, written.CurrencyCode)
		// replaced was read before the write, so it holds the old amount
		// even when written updated that very row.
		if !isRepeat && len(replaced) > 0 {
			before := replaced[0].toModel()
			isRepeat = isSame(row, before.MonthlyAmount, before.CurrencyCode)
		}
		if !isRepeat {
			return nil
		}
		if err := self.applyMutation(models.AuditResourceBudget, row.ID, models.AuditActionDelete, row, nil, func(tx *gorm.DB) error {
			return tx.Where(`"agent_id" = ? AND "id" = ?`, written.AgentID, row.ID).Delete(&agentBudgetModel{}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) ListBudgets(agentId string) ([]*models.Budget, error) {
	var found []agentBudgetModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"spending_category_id" ASC, "effective_from" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	budgets := make([]*models.Budget, 0, len(found))
	for index := range found {
		budgets = append(budgets, found[index].toModel())
	}
	return budgets, nil
}

func (self *transaction) BudgetsForMonth(agentId, month string) ([]*models.Budget, error) {
	monthStart, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	var found []agentBudgetModel
	if err := self.tx.Raw(`SELECT * FROM (
			SELECT DISTINCT ON ("spending_category_id") * FROM "agent_budget"
			WHERE "agent_id" = ? AND "effective_from" <= ?::date
			ORDER BY "spending_category_id", "effective_from" DESC
		) AS "in_force" WHERE "monthly_amount" > 0 ORDER BY "spending_category_id"`,
		agentId, monthStart.Format(time.DateOnly)).Scan(&found).Error; err != nil {
		return nil, err
	}
	budgets := make([]*models.Budget, 0, len(found))
	for index := range found {
		budgets = append(budgets, found[index].toModel())
	}
	return budgets, nil
}

// --- savings targets -------------------------------------------------------

type agentSavingsTargetModel struct {
	ID                string     `gorm:"column:id;primaryKey"`
	AgentID           string     `gorm:"column:agent_id"`
	SavingsTargetName string     `gorm:"column:savings_target_name"`
	TargetAmount      string     `gorm:"column:target_amount"`
	CurrencyCode      string     `gorm:"column:currency_code"`
	TargetOn          time.Time  `gorm:"column:target_on"`
	TargetMeasure     string     `gorm:"column:target_measure"`
	StartingAmount    *string    `gorm:"column:starting_amount"`
	StartedOn         time.Time  `gorm:"column:started_on"`
	ClosedOn          *time.Time `gorm:"column:closed_on"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	ModifiedAt        time.Time  `gorm:"column:modified_at"`
}

func (agentSavingsTargetModel) TableName() string { return "agent_savings_target" }

func (self *agentSavingsTargetModel) toModel(assetIds, financeAccountIds []string) *models.SavingsTarget {
	if assetIds == nil {
		assetIds = []string{}
	}
	if financeAccountIds == nil {
		financeAccountIds = []string{}
	}
	return &models.SavingsTarget{
		ID: self.ID, AgentID: self.AgentID, SavingsTargetName: self.SavingsTargetName, TargetAmount: self.TargetAmount,
		CurrencyCode: self.CurrencyCode, TargetOn: formatDay(self.TargetOn), TargetMeasure: models.TargetMeasure(self.TargetMeasure),
		StartingAmount: optionalString(self.StartingAmount), StartedOn: formatDay(self.StartedOn), ClosedOn: formatOptionalDay(self.ClosedOn),
		AssetIDs: assetIds, FinanceAccountIDs: financeAccountIds, CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
}

// validateSavingsTarget checks a savings target before it is written and
// writes its amounts and days the way the columns keep them. Its assets
// and finance accounts must be the agent's. Only an asset_value target
// keeps them: the other measures choose nothing.
func (self *transaction) validateSavingsTarget(savingsTarget *models.SavingsTarget) error {
	savingsTarget.SavingsTargetName = strings.TrimSpace(savingsTarget.SavingsTargetName)
	savingsTarget.CurrencyCode = strings.TrimSpace(savingsTarget.CurrencyCode)
	if savingsTarget.AgentID == "" || savingsTarget.SavingsTargetName == "" || savingsTarget.CurrencyCode == "" {
		return fmt.Errorf("%w: a savings target needs an agent, a name and a currency", ErrInvalidArguments)
	}
	if !savingsTarget.TargetMeasure.IsValid() {
		return fmt.Errorf("%w: %q is not how a savings target is measured", ErrInvalidArguments, savingsTarget.TargetMeasure)
	}
	var err error
	if savingsTarget.TargetAmount, err = canonicalAmount("target amount", savingsTarget.TargetAmount); err != nil {
		return err
	}
	startingAmount, err := canonicalOptionalAmount("starting amount", savingsTarget.StartingAmount)
	if err != nil {
		return err
	}
	savingsTarget.StartingAmount = optionalString(startingAmount)
	if savingsTarget.TargetOn, err = parseDay(savingsTarget.TargetOn); err != nil {
		return err
	}
	if savingsTarget.StartedOn, err = parseDay(savingsTarget.StartedOn); err != nil {
		return err
	}
	if savingsTarget.ClosedOn, err = parseOptionalDay(savingsTarget.ClosedOn); err != nil {
		return err
	}
	uniqueAssetIds := []string{}
	seen := map[string]bool{}
	for _, assetId := range savingsTarget.AssetIDs {
		if assetId == "" || seen[assetId] {
			continue
		}
		seen[assetId] = true
		asset, err := self.GetAsset(savingsTarget.AgentID, assetId)
		if err != nil {
			return err
		}
		if asset == nil {
			return ErrNotFound
		}
		uniqueAssetIds = append(uniqueAssetIds, assetId)
	}
	savingsTarget.AssetIDs = uniqueAssetIds
	uniqueFinanceAccountIds := []string{}
	seen = map[string]bool{}
	for _, financeAccountId := range savingsTarget.FinanceAccountIDs {
		if financeAccountId == "" || seen[financeAccountId] {
			continue
		}
		seen[financeAccountId] = true
		financeAccount, err := self.GetFinanceAccount(savingsTarget.AgentID, financeAccountId)
		if err != nil {
			return err
		}
		if financeAccount == nil {
			return ErrNotFound
		}
		uniqueFinanceAccountIds = append(uniqueFinanceAccountIds, financeAccountId)
	}
	savingsTarget.FinanceAccountIDs = uniqueFinanceAccountIds
	if savingsTarget.TargetMeasure != models.TargetMeasureAssetValue {
		savingsTarget.AssetIDs, savingsTarget.FinanceAccountIDs = []string{}, []string{}
	}
	return nil
}

// writeSavingsTarget inserts or replaces the savings target's row, its
// assets and its finance accounts.
func writeSavingsTarget(tx *gorm.DB, savingsTarget *models.SavingsTarget, isCreate bool) error {
	arguments := []any{
		savingsTarget.SavingsTargetName, savingsTarget.TargetAmount, savingsTarget.CurrencyCode, savingsTarget.TargetOn,
		string(savingsTarget.TargetMeasure), optionalID(savingsTarget.StartingAmount), savingsTarget.StartedOn,
		optionalDay(savingsTarget.ClosedOn), savingsTarget.ModifiedAt,
	}
	if isCreate {
		if err := tx.Exec(`INSERT INTO "agent_savings_target" ("savings_target_name", "target_amount", "currency_code", "target_on",
				"target_measure", "starting_amount", "started_on", "closed_on", "modified_at", "id", "agent_id", "created_at")
			VALUES (?, ?::numeric, ?, ?::date, ?, ?::numeric, ?::date, ?::date, ?, ?, ?, ?)`,
			append(arguments, savingsTarget.ID, savingsTarget.AgentID, savingsTarget.CreatedAt)...).Error; err != nil {
			return err
		}
	} else {
		if err := tx.Exec(`UPDATE "agent_savings_target" SET "savings_target_name" = ?, "target_amount" = ?::numeric,
				"currency_code" = ?, "target_on" = ?::date, "target_measure" = ?, "starting_amount" = ?::numeric,
				"started_on" = ?::date, "closed_on" = ?::date, "modified_at" = ?
			WHERE "id" = ? AND "agent_id" = ?`,
			append(arguments, savingsTarget.ID, savingsTarget.AgentID)...).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM "agent_savings_target_asset" WHERE "savings_target_id" = ?`, savingsTarget.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM "agent_savings_target_finance_account" WHERE "savings_target_id" = ?`, savingsTarget.ID).Error; err != nil {
			return err
		}
	}
	if len(savingsTarget.AssetIDs) > 0 {
		if err := tx.Exec(`INSERT INTO "agent_savings_target_asset" ("savings_target_id", "asset_id")
			SELECT ?, unnest(?::text[])`, savingsTarget.ID, pq.Array(savingsTarget.AssetIDs)).Error; err != nil {
			return err
		}
	}
	if len(savingsTarget.FinanceAccountIDs) > 0 {
		if err := tx.Exec(`INSERT INTO "agent_savings_target_finance_account" ("savings_target_id", "finance_account_id")
			SELECT ?, unnest(?::text[])`, savingsTarget.ID, pq.Array(savingsTarget.FinanceAccountIDs)).Error; err != nil {
			return err
		}
	}
	return nil
}

// savingsTargetAssetIds is the assets of each savings target given, by id.
func (self *transaction) savingsTargetAssetIds(savingsTargetIds []string) (map[string][]string, error) {
	assetIdsBySavingsTargetId := map[string][]string{}
	if len(savingsTargetIds) == 0 {
		return assetIdsBySavingsTargetId, nil
	}
	var rows []struct {
		SavingsTargetID string `gorm:"column:savings_target_id"`
		AssetID         string `gorm:"column:asset_id"`
	}
	if err := self.tx.Raw(`SELECT "savings_target_id", "asset_id" FROM "agent_savings_target_asset"
		WHERE "savings_target_id" = ANY(?::text[]) ORDER BY "savings_target_id", "asset_id"`, pq.Array(savingsTargetIds)).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		assetIdsBySavingsTargetId[row.SavingsTargetID] = append(assetIdsBySavingsTargetId[row.SavingsTargetID], row.AssetID)
	}
	return assetIdsBySavingsTargetId, nil
}

// savingsTargetFinanceAccountIds is the finance accounts of each savings
// target given, by id.
func (self *transaction) savingsTargetFinanceAccountIds(savingsTargetIds []string) (map[string][]string, error) {
	financeAccountIdsBySavingsTargetId := map[string][]string{}
	if len(savingsTargetIds) == 0 {
		return financeAccountIdsBySavingsTargetId, nil
	}
	var rows []struct {
		SavingsTargetID  string `gorm:"column:savings_target_id"`
		FinanceAccountID string `gorm:"column:finance_account_id"`
	}
	if err := self.tx.Raw(`SELECT "savings_target_id", "finance_account_id" FROM "agent_savings_target_finance_account"
		WHERE "savings_target_id" = ANY(?::text[]) ORDER BY "savings_target_id", "finance_account_id"`, pq.Array(savingsTargetIds)).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		financeAccountIdsBySavingsTargetId[row.SavingsTargetID] = append(financeAccountIdsBySavingsTargetId[row.SavingsTargetID], row.FinanceAccountID)
	}
	return financeAccountIdsBySavingsTargetId, nil
}

func (self *transaction) savingsTargetsFrom(found []agentSavingsTargetModel) ([]*models.SavingsTarget, error) {
	savingsTargetIds := make([]string, 0, len(found))
	for index := range found {
		savingsTargetIds = append(savingsTargetIds, found[index].ID)
	}
	assetIdsBySavingsTargetId, err := self.savingsTargetAssetIds(savingsTargetIds)
	if err != nil {
		return nil, err
	}
	financeAccountIdsBySavingsTargetId, err := self.savingsTargetFinanceAccountIds(savingsTargetIds)
	if err != nil {
		return nil, err
	}
	savingsTargets := make([]*models.SavingsTarget, 0, len(found))
	for index := range found {
		savingsTargets = append(savingsTargets, found[index].toModel(assetIdsBySavingsTargetId[found[index].ID],
			financeAccountIdsBySavingsTargetId[found[index].ID]))
	}
	return savingsTargets, nil
}

func (self *transaction) ListSavingsTargets(agentId string) ([]*models.SavingsTarget, error) {
	var found []agentSavingsTargetModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).
		Order(`("closed_on" IS NOT NULL) ASC, "target_on" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	return self.savingsTargetsFrom(found)
}

func (self *transaction) GetSavingsTarget(agentId, savingsTargetId string) (*models.SavingsTarget, error) {
	var found []agentSavingsTargetModel
	if err := self.tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, savingsTargetId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	savingsTargets, err := self.savingsTargetsFrom(found)
	if err != nil {
		return nil, err
	}
	return savingsTargets[0], nil
}

func (self *transaction) CreateSavingsTarget(savingsTarget *models.SavingsTarget) (*models.SavingsTarget, error) {
	created := *savingsTarget
	if err := self.validateSavingsTarget(&created); err != nil {
		return nil, err
	}
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if err := self.applyMutation(models.AuditResourceSavingsTarget, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return writeSavingsTarget(tx, &created, true)
	}); err != nil {
		return nil, err
	}
	return self.GetSavingsTarget(created.AgentID, created.ID)
}

func (self *transaction) UpdateSavingsTarget(agentId, savingsTargetId string, modify func(*models.SavingsTarget) error) (*models.SavingsTarget, error) {
	var found []agentSavingsTargetModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" = ?`, agentId, savingsTargetId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	savingsTargets, err := self.savingsTargetsFrom(found)
	if err != nil {
		return nil, err
	}
	before := savingsTargets[0]
	after := *before
	after.AssetIDs = append([]string(nil), before.AssetIDs...)
	after.FinanceAccountIDs = append([]string(nil), before.FinanceAccountIDs...)
	if err := modify(&after); err != nil {
		return nil, err
	}
	after.ID, after.AgentID, after.CreatedAt = before.ID, before.AgentID, before.CreatedAt
	if err := self.validateSavingsTarget(&after); err != nil {
		return nil, err
	}
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceSavingsTarget, savingsTargetId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return writeSavingsTarget(tx, &after, false)
	}); err != nil {
		return nil, err
	}
	return self.GetSavingsTarget(agentId, savingsTargetId)
}

func (self *transaction) CloseSavingsTarget(agentId, savingsTargetId, closedOn string) (*models.SavingsTarget, error) {
	closedOn, err := parseOptionalDay(closedOn)
	if err != nil {
		return nil, err
	}
	return self.UpdateSavingsTarget(agentId, savingsTargetId, func(savingsTarget *models.SavingsTarget) error {
		savingsTarget.ClosedOn = closedOn
		return nil
	})
}

func (self *transaction) DeleteSavingsTarget(agentId, savingsTargetId string) error {
	before, err := self.GetSavingsTarget(agentId, savingsTargetId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceSavingsTarget, savingsTargetId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where(`"agent_id" = ? AND "id" = ?`, agentId, savingsTargetId).Delete(&agentSavingsTargetModel{}).Error
	})
}

// --- what the budget pace reads --------------------------------------------

func (self *transaction) ListSpendingCategoryDays(agentId, month string) ([]*models.SpendingCategoryDay, error) {
	monthStart, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		SpendingCategoryID string `gorm:"column:spending_category_id"`
		CurrencyCode       string `gorm:"column:currency_code"`
		SpentOn            string `gorm:"column:spent_on"`
		SpendingAmount     string `gorm:"column:spending_amount"`
	}
	if err := self.tx.Raw(`SELECT COALESCE("spent"."spending_category_id", '') AS "spending_category_id", "spent"."currency_code",
			to_char("spent"."posted_on", 'YYYY-MM-DD') AS "spent_on", SUM(-"spent"."amount")::text AS "spending_amount"
		FROM "agent_finance_transaction" AS "spent"
		LEFT JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "spent"."spending_category_id" AND "spending_category"."agent_id" = "spent"."agent_id"
		WHERE "spent"."agent_id" = ? AND "spent"."duplicate_of_transaction_id" IS NULL
		  AND "spent"."posted_on" >= ?::date AND "spent"."posted_on" < ?::date
		  AND (("spending_category"."id" IS NULL AND "spent"."amount" < 0)
		    OR ("spending_category"."id" IS NOT NULL AND NOT "spending_category"."is_income" AND NOT "spending_category"."is_transfer"))
		GROUP BY 1, 2, 3
		ORDER BY 3, 1, 2`,
		agentId, monthStart.Format(time.DateOnly), monthStart.AddDate(0, 1, 0).Format(time.DateOnly)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	days := make([]*models.SpendingCategoryDay, 0, len(rows))
	for _, row := range rows {
		days = append(days, &models.SpendingCategoryDay{
			SpendingCategoryID: row.SpendingCategoryID, CurrencyCode: row.CurrencyCode, SpentOn: row.SpentOn, SpendingAmount: row.SpendingAmount,
		})
	}
	return days, nil
}

func (self *transaction) ListIncomeCategoryDays(agentId, month string) ([]*models.IncomeCategoryDay, error) {
	monthStart, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		SpendingCategoryID string `gorm:"column:spending_category_id"`
		CurrencyCode       string `gorm:"column:currency_code"`
		ReceivedOn         string `gorm:"column:received_on"`
		IncomeAmount       string `gorm:"column:income_amount"`
	}
	if err := self.tx.Raw(`SELECT "received"."spending_category_id", "received"."currency_code",
			to_char("received"."posted_on", 'YYYY-MM-DD') AS "received_on", SUM("received"."amount")::text AS "income_amount"
		FROM "agent_finance_transaction" AS "received"
		JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "received"."spending_category_id" AND "spending_category"."agent_id" = "received"."agent_id"
		WHERE "received"."agent_id" = ? AND "spending_category"."is_income" AND NOT "spending_category"."is_transfer"
		  AND "received"."duplicate_of_transaction_id" IS NULL
		  AND "received"."posted_on" >= ?::date AND "received"."posted_on" < ?::date
		GROUP BY 1, 2, 3
		ORDER BY 3, 1, 2`,
		agentId, monthStart.Format(time.DateOnly), monthStart.AddDate(0, 1, 0).Format(time.DateOnly)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	days := make([]*models.IncomeCategoryDay, 0, len(rows))
	for _, row := range rows {
		days = append(days, &models.IncomeCategoryDay{
			SpendingCategoryID: row.SpendingCategoryID, CurrencyCode: row.CurrencyCode, ReceivedOn: row.ReceivedOn, IncomeAmount: row.IncomeAmount,
		})
	}
	return days, nil
}

func (self *transaction) ListCashFlowDays(agentId, from, to string) ([]*models.CashFlowDay, error) {
	from, err := parseDay(from)
	if err != nil {
		return nil, err
	}
	to, err = parseDay(to)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		CashFlowOn     string `gorm:"column:cash_flow_on"`
		CurrencyCode   string `gorm:"column:currency_code"`
		IncomeAmount   string `gorm:"column:income_amount"`
		SpendingAmount string `gorm:"column:spending_amount"`
	}
	if err := self.tx.Raw(`SELECT to_char("flowed"."posted_on", 'YYYY-MM-DD') AS "cash_flow_on", "flowed"."currency_code",
			SUM(CASE WHEN "spending_category"."id" IS NOT NULL AND "spending_category"."is_income" THEN "flowed"."amount"
			         WHEN "spending_category"."id" IS NULL AND "flowed"."amount" > 0 THEN "flowed"."amount"
			         ELSE 0 END)::text AS "income_amount",
			SUM(CASE WHEN "spending_category"."id" IS NOT NULL AND NOT "spending_category"."is_income" THEN -"flowed"."amount"
			         WHEN "spending_category"."id" IS NULL AND "flowed"."amount" < 0 THEN -"flowed"."amount"
			         ELSE 0 END)::text AS "spending_amount"
		FROM "agent_finance_transaction" AS "flowed"
		LEFT JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "flowed"."spending_category_id" AND "spending_category"."agent_id" = "flowed"."agent_id"
		WHERE "flowed"."agent_id" = ? AND NOT COALESCE("spending_category"."is_transfer", false)
		  AND "flowed"."duplicate_of_transaction_id" IS NULL
		  AND "flowed"."posted_on" >= ?::date AND "flowed"."posted_on" <= ?::date
		GROUP BY 1, 2
		ORDER BY 1, 2`, agentId, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	days := make([]*models.CashFlowDay, 0, len(rows))
	for _, row := range rows {
		days = append(days, &models.CashFlowDay{
			CashFlowOn: row.CashFlowOn, CurrencyCode: row.CurrencyCode, IncomeAmount: row.IncomeAmount, SpendingAmount: row.SpendingAmount,
		})
	}
	return days, nil
}

func (self *transaction) ListMerchantMonthSpending(agentId, month string) ([]*models.MerchantMonthSpending, error) {
	monthStart, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		SpendingCategoryID      string `gorm:"column:spending_category_id"`
		MerchantName            string `gorm:"column:merchant_name"`
		SpendingMonth           string `gorm:"column:spending_month"`
		CurrencyCode            string `gorm:"column:currency_code"`
		SpendingAmount          string `gorm:"column:spending_amount"`
		FinanceTransactionCount int    `gorm:"column:finance_transaction_count"`
	}
	if err := self.tx.Raw(`SELECT "spent"."spending_category_id",
			COALESCE(NULLIF("spent"."merchant_name", ''), "spent"."description") AS "merchant_name",
			to_char("spent"."posted_on", 'YYYY-MM') AS "spending_month", "spent"."currency_code",
			SUM(-"spent"."amount")::text AS "spending_amount", COUNT(*) AS "finance_transaction_count"
		FROM "agent_finance_transaction" AS "spent"
		JOIN "agent_spending_category" AS "spending_category"
		  ON "spending_category"."id" = "spent"."spending_category_id" AND "spending_category"."agent_id" = "spent"."agent_id"
		WHERE "spent"."agent_id" = ? AND "spent"."amount" < 0 AND "spent"."duplicate_of_transaction_id" IS NULL
		  AND NOT "spending_category"."is_income" AND NOT "spending_category"."is_transfer"
		  AND "spent"."posted_on" >= ?::date AND "spent"."posted_on" < ?::date
		GROUP BY 1, 2, 3, 4
		ORDER BY 1, 2, 3, 4`,
		agentId, monthStart.AddDate(0, -3, 0).Format(time.DateOnly), monthStart.Format(time.DateOnly)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	spending := make([]*models.MerchantMonthSpending, 0, len(rows))
	for _, row := range rows {
		spending = append(spending, &models.MerchantMonthSpending{
			SpendingCategoryID: row.SpendingCategoryID, MerchantName: row.MerchantName, SpendingMonth: row.SpendingMonth,
			CurrencyCode: row.CurrencyCode, SpendingAmount: row.SpendingAmount, FinanceTransactionCount: row.FinanceTransactionCount,
		})
	}
	return spending, nil
}
