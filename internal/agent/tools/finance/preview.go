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
// first to learn the id.
func (self *previewLookup) spendingCategoryFor(idOrName string) *client.SpendingCategory {
	if idOrName == "" || !self.read("SpendingCategories", nil, &self.spendingCategories) {
		return nil
	}
	for _, spendingCategory := range self.spendingCategories {
		if spendingCategory.ID == idOrName {
			return spendingCategory
		}
	}
	for _, spendingCategory := range self.spendingCategories {
		if strings.EqualFold(strings.TrimSpace(spendingCategory.SpendingCategoryName), strings.TrimSpace(idOrName)) {
			return spendingCategory
		}
	}
	return nil
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
	if self.executor == nil || financeTransactionId == "" {
		return "a transaction"
	}
	after := ""
	for page := 0; page < previewTransactionPages; page++ {
		var answered *client.FinanceTransactionPage
		variables := map[string]any{"limit": 200}
		if after != "" {
			variables["after"] = after
		}
		if client.RunFinance(self.ctx, self.executor, "FinanceTransactions", variables, &answered) != nil || answered == nil {
			break
		}
		for _, financeTransaction := range answered.FinanceTransactions {
			if financeTransaction.ID != financeTransactionId {
				continue
			}
			what := financeTransaction.MerchantName
			if what == "" {
				what = financeTransaction.Description
			}
			return fmt.Sprintf("the transaction %s, %s %s on %s", tools.Named(what, "with no description"),
				financeTransaction.Amount, financeTransaction.CurrencyCode, financeTransaction.PostedOn)
		}
		if answered.NextCursor == "" {
			break
		}
		after = answered.NextCursor
	}
	return "a transaction"
}

// ruleEffect is what a spending rule does to what it matches.
func (self *previewLookup) ruleEffect(call map[string]any) string {
	spendingCategoryId := text(call, "spending_category_id")
	switch {
	case spendingCategoryId != "" && isTrue(call, "is_transfer"):
		return "files it under " + self.spendingCategoryName(spendingCategoryId) + " and marks it a transfer"
	case spendingCategoryId != "":
		return "files it under " + self.spendingCategoryName(spendingCategoryId)
	case isTrue(call, "is_transfer"):
		return "marks it a transfer"
	}
	return "files it under no spending category"
}
