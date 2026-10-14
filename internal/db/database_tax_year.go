package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
)

// TaxYearOperation is a person's tax year settings, what the estimate
// needs that no document says, and the investment income their linked
// accounts were paid.
type TaxYearOperation interface {
	// GetTaxYearSetting is the agent's settings for a tax year, or nil.
	GetTaxYearSetting(agentId string, taxYear int) (*models.TaxYearSetting, error)

	// PutTaxYearSetting writes the agent's settings for a tax year in the
	// place of the ones before. ErrInvalidArguments for a count out of
	// range or an amount its column does not hold.
	PutTaxYearSetting(setting *models.TaxYearSetting) (*models.TaxYearSetting, error)

	// ListAccountInvestmentIncome adds up, by finance account and kind,
	// the interest and dividends posted to the agent's accounts from from
	// to to, both included ("2006-01-02"): money in that the provider
	// calls interest earned or dividends, or, from a provider that names
	// no category, whose description says interest or dividend. Mirrored
	// copies are counted once; the currency is the account's, and only
	// USD accounts are kept.
	ListAccountInvestmentIncome(agentId, from, to string) ([]*models.AccountInvestmentIncome, error)
}

type agentTaxYearSettingModel struct {
	AgentID                      string    `gorm:"column:agent_id;primaryKey"`
	TaxYear                      int       `gorm:"column:tax_year;primaryKey"`
	ChildCount                   int       `gorm:"column:child_count"`
	OtherDependentCount          int       `gorm:"column:other_dependent_count"`
	DependentCareExpenseAmount   string    `gorm:"column:dependent_care_expense_amount"`
	OtherIncomeAmount            string    `gorm:"column:other_income_amount"`
	CharitableContributionAmount string    `gorm:"column:charitable_contribution_amount"`
	PropertyTaxAmount            string    `gorm:"column:property_tax_amount"`
	StateIncomeTaxPaidAmount     string    `gorm:"column:state_income_tax_paid_amount"`
	EstimatedShortTermGainAmount *string   `gorm:"column:estimated_short_term_gain_amount"`
	EstimatedLongTermGainAmount  *string   `gorm:"column:estimated_long_term_gain_amount"`
	EstimatedGainNote            string    `gorm:"column:estimated_gain_note"`
	ModifiedAt                   time.Time `gorm:"column:modified_at"`
}

func (agentTaxYearSettingModel) TableName() string { return "agent_tax_year_setting" }

func (self *agentTaxYearSettingModel) setting() *models.TaxYearSetting {
	modifiedAt := self.ModifiedAt.In(time.Local)
	setting := &models.TaxYearSetting{
		AgentID: self.AgentID, TaxYear: self.TaxYear, ChildCount: self.ChildCount, OtherDependentCount: self.OtherDependentCount,
		DependentCareExpenseAmount: canonicalStoredAmount(self.DependentCareExpenseAmount), OtherIncomeAmount: canonicalStoredAmount(self.OtherIncomeAmount),
		CharitableContributionAmount: canonicalStoredAmount(self.CharitableContributionAmount), PropertyTaxAmount: canonicalStoredAmount(self.PropertyTaxAmount),
		StateIncomeTaxPaidAmount: canonicalStoredAmount(self.StateIncomeTaxPaidAmount),
		EstimatedGainNote:        self.EstimatedGainNote, ModifiedAt: &modifiedAt,
	}
	if self.EstimatedShortTermGainAmount != nil {
		setting.EstimatedShortTermGainAmount = canonicalStoredAmount(*self.EstimatedShortTermGainAmount)
	}
	if self.EstimatedLongTermGainAmount != nil {
		setting.EstimatedLongTermGainAmount = canonicalStoredAmount(*self.EstimatedLongTermGainAmount)
	}
	return setting
}

// canonicalStoredAmount is an amount as PostgreSQL gives a numeric back,
// written the way the API writes amounts; what does not read stays.
func canonicalStoredAmount(amount string) string {
	canonical, err := canonicalAmount("amount", amount)
	if err != nil {
		return amount
	}
	return canonical
}

func (self *transaction) GetTaxYearSetting(agentId string, taxYear int) (*models.TaxYearSetting, error) {
	var row agentTaxYearSettingModel
	if err := self.tx.Where(`"agent_id" = ? AND "tax_year" = ?`, agentId, taxYear).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row.setting(), nil
}

func (self *transaction) PutTaxYearSetting(setting *models.TaxYearSetting) (*models.TaxYearSetting, error) {
	if setting == nil || setting.AgentID == "" || setting.TaxYear < 1900 || setting.TaxYear > 2999 {
		return nil, fmt.Errorf("%w: tax year settings need their agent and a tax year", ErrInvalidArguments)
	}
	if setting.ChildCount < 0 || setting.ChildCount > 20 || setting.OtherDependentCount < 0 || setting.OtherDependentCount > 20 {
		return nil, fmt.Errorf("%w: children and other dependents are each 0 to 20", ErrInvalidArguments)
	}
	row := &agentTaxYearSettingModel{
		AgentID: setting.AgentID, TaxYear: setting.TaxYear, ChildCount: setting.ChildCount, OtherDependentCount: setting.OtherDependentCount,
		EstimatedGainNote: setting.EstimatedGainNote, ModifiedAt: time.Now(),
	}
	for _, field := range []struct {
		what         string
		amount       string
		target       *string
		isNegativeOK bool
	}{
		{"the dependent care paid", setting.DependentCareExpenseAmount, &row.DependentCareExpenseAmount, false},
		{"the other income", setting.OtherIncomeAmount, &row.OtherIncomeAmount, true},
		{"the charitable contributions", setting.CharitableContributionAmount, &row.CharitableContributionAmount, false},
		{"the property tax", setting.PropertyTaxAmount, &row.PropertyTaxAmount, false},
		{"the state income tax paid", setting.StateIncomeTaxPaidAmount, &row.StateIncomeTaxPaidAmount, false},
	} {
		canonical, err := canonicalOptionalReceiptAmount(field.what, field.amount)
		if err != nil {
			return nil, err
		}
		*field.target = "0"
		if canonical != nil {
			if !field.isNegativeOK && len(*canonical) > 0 && (*canonical)[0] == '-' {
				return nil, fmt.Errorf("%w: %s cannot be negative", ErrInvalidArguments, field.what)
			}
			*field.target = *canonical
		}
	}
	var err error
	if row.EstimatedShortTermGainAmount, err = canonicalOptionalReceiptAmount("the short-term gain", setting.EstimatedShortTermGainAmount); err != nil {
		return nil, err
	}
	if row.EstimatedLongTermGainAmount, err = canonicalOptionalReceiptAmount("the long-term gain", setting.EstimatedLongTermGainAmount); err != nil {
		return nil, err
	}
	before, err := self.GetTaxYearSetting(setting.AgentID, setting.TaxYear)
	if err != nil {
		return nil, err
	}
	action := models.AuditActionCreate
	if before != nil {
		action = models.AuditActionUpdate
	}
	resourceId := fmt.Sprintf("%s/%d", setting.AgentID, setting.TaxYear)
	if err := self.applyMutation(models.AuditResourceTaxYearSetting, resourceId, action, before, row.setting(), func(tx *gorm.DB) error {
		return tx.Save(row).Error
	}); err != nil {
		return nil, err
	}
	return self.GetTaxYearSetting(setting.AgentID, setting.TaxYear)
}

func (self *transaction) ListAccountInvestmentIncome(agentId, from, to string) ([]*models.AccountInvestmentIncome, error) {
	var rows []struct {
		FinanceAccountID string
		AccountName      string
		AccountSubtype   string
		IncomeKind       string
		Amount           string
		TransactionCount int
	}
	err := self.tx.Raw(`SELECT t."finance_account_id" AS finance_account_id, a."account_name" AS account_name,
			COALESCE(a."provider_metadata"->>'subtype', '') AS account_subtype,
			CASE WHEN t."provider_category_detailed" = 'INCOME_DIVIDENDS' OR (t."provider_category_detailed" = '' AND t."description" ILIKE '%dividend%')
				THEN 'dividend' ELSE 'interest' END AS income_kind,
			SUM(t."amount")::text AS amount, COUNT(*) AS transaction_count
		FROM "agent_finance_transaction" t JOIN "agent_finance_account" a ON a."id" = t."finance_account_id"
		WHERE t."agent_id" = ? AND t."posted_on" BETWEEN ? AND ? AND t."amount" > 0 AND t."duplicate_of_transaction_id" IS NULL
			AND t."currency_code" = 'USD'
			AND (t."provider_category_detailed" IN ('INCOME_DIVIDENDS', 'INCOME_INTEREST_EARNED')
				OR (t."provider_category_detailed" = '' AND (t."description" ILIKE '%dividend%' OR t."description" ILIKE '%interest%')))
		GROUP BY 1, 2, 3, 4 ORDER BY 2, 4`, agentId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	incomes := make([]*models.AccountInvestmentIncome, 0, len(rows))
	for _, row := range rows {
		incomes = append(incomes, &models.AccountInvestmentIncome{
			FinanceAccountID: row.FinanceAccountID, AccountName: row.AccountName, AccountSubtype: row.AccountSubtype,
			IncomeKind: row.IncomeKind, Amount: canonicalStoredAmount(row.Amount), TransactionCount: row.TransactionCount,
		})
	}
	return incomes, nil
}
