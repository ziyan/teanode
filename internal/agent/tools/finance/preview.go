package finance

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
)

// previewTransactionPages is how many pages of the newest finance
// transactions a confirmation card looks through to name the one a call
// acts on; past them the card says "a transaction".
const previewTransactionPages = 5

// previewLookup names, for a confirmation card, what a call acts on the
// way the person knows it: the spending category by its name, the
// transaction by its day, amount and merchant. A card names what is
// approved, and an id says nothing to the person approving it. Each
// listing is read once per card, and anything that cannot be read leaves
// the card with a plainer word.
type previewLookup struct {
	ctx      context.Context
	executor client.Executor

	spendingCategories []*client.SpendingCategory
	assets             []*client.Asset
	savingsTargets     []*client.SavingsTargetStanding
	financeAccounts    []*client.FinanceAccount
	spendingRules      []*client.SpendingRule
	sources            []*client.FinanceSource
	isRead             map[string]bool
}

// newPreviewLookup reads as the person of the run in the context; with no
// run it names nothing.
func newPreviewLookup(ctx context.Context) *previewLookup {
	lookup := &previewLookup{ctx: ctx, isRead: map[string]bool{}}
	if current, err := tools.RunFrom(ctx); err == nil && current != nil {
		lookup.executor = current.Operations()
	}
	return lookup
}

// read runs a listing once, into result.
func (self *previewLookup) read(operation string, variables map[string]any, result any) bool {
	if self.executor == nil {
		return false
	}
	if self.isRead[operation] {
		return true
	}
	if err := client.RunFinance(self.ctx, self.executor, operation, variables, result); err != nil {
		return false
	}
	self.isRead[operation] = true
	return true
}

func (self *previewLookup) spendingCategoryName(spendingCategoryId string) string {
	if spendingCategory := self.spendingCategoryFor(spendingCategoryId); spendingCategory != nil {
		return tools.Named(spendingCategory.SpendingCategoryName, "")
	}
	return "a spending category"
}

// spendingCategoryFor is the person's spending category given by its id or
// by its name, in any case. The command line takes either, and a model
// that asked by name was refused and had to list the spending categories
// first to learn the id. Transfer is the transfer category whatever it is
// called: a person who already had a "transfer" of their own keeps it,
// and the built-in one beside it is named something else, but transfer is
// the word the tool's description gives for marking one.
func (self *previewLookup) spendingCategoryFor(idOrName string) *client.SpendingCategory {
	if idOrName == "" || !self.read("SpendingCategories", nil, &self.spendingCategories) {
		return nil
	}
	for _, spendingCategory := range self.spendingCategories {
		if spendingCategory.ID == idOrName {
			return spendingCategory
		}
	}
	if strings.EqualFold(strings.TrimSpace(idOrName), "transfer") {
		for _, spendingCategory := range self.spendingCategories {
			if spendingCategory.IsTransfer {
				return spendingCategory
			}
		}
	}
	for _, spendingCategory := range self.spendingCategories {
		if strings.EqualFold(strings.TrimSpace(spendingCategory.SpendingCategoryName), strings.TrimSpace(idOrName)) {
			return spendingCategory
		}
	}
	return nil
}

// isTransferSpendingCategory says the spending category is the transfer
// category; false when it cannot be read.
func (self *previewLookup) isTransferSpendingCategory(idOrName string) bool {
	spendingCategory := self.spendingCategoryFor(idOrName)
	return spendingCategory != nil && spendingCategory.IsTransfer
}

// isIncomeSpendingCategory says the spending category is income, whose
// budget is the income expected rather than a limit; false when it cannot
// be read.
func (self *previewLookup) isIncomeSpendingCategory(idOrName string) bool {
	spendingCategory := self.spendingCategoryFor(idOrName)
	return spendingCategory != nil && spendingCategory.IsIncome
}

func (self *previewLookup) assetName(assetId string) string {
	if self.read("Assets", nil, &self.assets) {
		for _, asset := range self.assets {
			if asset.ID == assetId {
				return tools.Named(asset.AssetName, "")
			}
		}
	}
	return "an asset"
}

func (self *previewLookup) financeAccountName(financeAccountId string) string {
	if self.read("FinanceAccounts", nil, &self.financeAccounts) {
		for _, financeAccount := range self.financeAccounts {
			if financeAccount.ID == financeAccountId {
				return tools.Named(financeAccount.AccountName, "")
			}
		}
	}
	return "a finance account"
}

// targetMeasureSuffix says how a savings target call measures progress,
// naming the finance accounts and assets an asset_value target counts;
// empty when the call does not say.
func (self *previewLookup) targetMeasureSuffix(call map[string]any) string {
	var named []string
	for _, financeAccountId := range texts(call, "finance_account_ids") {
		named = append(named, self.financeAccountName(financeAccountId)+" (the whole account)")
	}
	for _, assetId := range texts(call, "asset_ids") {
		named = append(named, self.assetName(assetId))
	}
	switch text(call, "target_measure") {
	case "net_worth":
		return ", measured by net worth"
	case "cash_flow":
		return ", measured by income less spending"
	}
	if len(named) == 0 {
		return ""
	}
	return ", measured by what " + strings.Join(named, ", ") + " are worth"
}

func (self *previewLookup) savingsTargetName(savingsTargetId string) string {
	if self.read("SavingsTargets", nil, &self.savingsTargets) {
		for _, standing := range self.savingsTargets {
			if standing.SavingsTarget != nil && standing.SavingsTarget.ID == savingsTargetId {
				return tools.Named(standing.SavingsTarget.SavingsTargetName, "")
			}
		}
	}
	return "a savings target"
}

func (self *previewLookup) spendingRuleMatch(spendingRuleId string) string {
	if self.read("SpendingRules", nil, &self.spendingRules) {
		for _, spendingRule := range self.spendingRules {
			if spendingRule.ID == spendingRuleId {
				return tools.Named(spendingRule.MatchText, "")
			}
		}
	}
	return "a merchant"
}

func (self *previewLookup) sourceName(sourceId string) string {
	if self.read("FinanceSources", nil, &self.sources) {
		for _, source := range self.sources {
			if source.ID == sourceId {
				name := source.InstitutionName
				if name == "" {
					name = source.Name
				}
				return tools.Named(name, "")
			}
		}
	}
	return "a finance source"
}

// transaction is a finance transaction as a statement line: what it was,
// its amount and its day.
func (self *previewLookup) transaction(financeTransactionId string) string {
	if lines := self.transactionLines([]string{financeTransactionId}); len(lines) == 1 {
		return lines[0]
	}
	return "a transaction"
}

// transactionLines is each finance transaction given that the newest
// pages hold, as transaction says it, in the order given; one the pages do
// not hold is left out.
func (self *previewLookup) transactionLines(financeTransactionIds []string) []string {
	if self.executor == nil || len(financeTransactionIds) == 0 {
		return nil
	}
	wanted := map[string]string{}
	for _, financeTransactionId := range financeTransactionIds {
		wanted[financeTransactionId] = ""
	}
	foundCount := 0
	after := ""
	for page := 0; page < previewTransactionPages && foundCount < len(wanted); page++ {
		var answered *client.FinanceTransactionPage
		variables := map[string]any{"limit": 200}
		if after != "" {
			variables["after"] = after
		}
		if client.RunFinance(self.ctx, self.executor, "FinanceTransactions", variables, &answered) != nil || answered == nil {
			break
		}
		for _, financeTransaction := range answered.FinanceTransactions {
			if line, isWanted := wanted[financeTransaction.ID]; !isWanted || line != "" {
				continue
			}
			what := financeTransaction.MerchantName
			if what == "" {
				what = financeTransaction.Description
			}
			wanted[financeTransaction.ID] = fmt.Sprintf("the transaction %s, %s %s on %s", tools.Named(what, "with no description"),
				financeTransaction.Amount, financeTransaction.CurrencyCode, financeTransaction.PostedOn)
			foundCount++
		}
		if answered.NextCursor == "" {
			break
		}
		after = answered.NextCursor
	}
	var lines []string
	for _, financeTransactionId := range financeTransactionIds {
		if line := wanted[financeTransactionId]; line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// spendingRuleProposals is the spending rules categorizing these finance
// transactions would save, each named with how many other transactions it
// would change and the rule it goes ahead of ("goes ahead of: zoomly eats →
// Dining"), and what was left out and why; false when they cannot be read.
func (self *previewLookup) spendingRuleProposals(financeTransactionIds []string, spendingCategoryIdOrName string) ([]string, []string, bool) {
	spendingCategory := self.spendingCategoryFor(spendingCategoryIdOrName)
	if self.executor == nil || spendingCategory == nil {
		return nil, nil, false
	}
	var proposals *client.SpendingRuleProposals
	variables := map[string]any{"financeTransactionIds": financeTransactionIds, "spendingCategoryId": spendingCategory.ID}
	if client.RunFinance(self.ctx, self.executor, "ProposeSpendingRules", variables, &proposals) != nil || proposals == nil {
		return nil, nil, false
	}
	named := make([]string, 0, len(proposals.SpendingRuleProposals))
	for _, proposal := range proposals.SpendingRuleProposals {
		line := tools.Named(proposal.MatchText, "") + fmt.Sprintf(" (changes %d other transactions", proposal.ChangedTransactionCount)
		if proposal.AheadOfSpendingRule != nil {
			line += "; goes ahead of: " + proposal.AheadOfSpendingRule.MatchText + " → " + self.spendingCategoryName(proposal.AheadOfSpendingRule.SpendingCategoryID)
		}
		named = append(named, line+")")
	}
	return named, proposals.LeftOutReasons(), true
}

// ruleEffect is what a spending rule does to what it matches.
func (self *previewLookup) ruleEffect(call map[string]any) string {
	spendingCategoryId := text(call, "spending_category_id")
	switch {
	case self.isTransferSpendingCategory(spendingCategoryId):
		return "marks it a transfer between their own accounts"
	case spendingCategoryId != "":
		return "files it under " + self.spendingCategoryName(spendingCategoryId)
	}
	return "files it under no spending category"
}
