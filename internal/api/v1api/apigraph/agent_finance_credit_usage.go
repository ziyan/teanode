package apigraph

import (
	"context"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Credit usage: what is owed on the person's credit cards against their
// credit limits, overall and per card. Part of the finance area
// (FinanceQuery). A credit card is a finance account of kind credit.

// CreditUsageView is what is owed on the caller's credit cards against
// their credit limits.
type CreditUsageView struct {
	// ReportingCurrencyCode is what the totals and each card's converted
	// amounts are in, each card at today's exchange rate, the way net
	// worth converts today's balances. A currency with no rate is left
	// out of the totals and named in UnconvertedCurrencyCodes.
	ReportingCurrencyCode    string   `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	UnconvertedCurrencyCodes []string `json:"unconvertedCurrencyCodes"`

	// TotalOwedAmount and TotalCreditLimitAmount add up the cards whose
	// credit limit and balance are both known. UsageShare is the one
	// over the other, 0.25 for a quarter of the credit used and more than
	// 1 past the limit; empty when no card counts.
	TotalOwedAmount        string   `json:"totalOwedAmount"`
	TotalCreditLimitAmount string   `json:"totalCreditLimitAmount"`
	UsageShare             *float64 `json:"usageShare,omitempty" graphapi:"nullable"`

	// LeftOutCardCount is how many cards are left out of the totals
	// because their credit limit or their balance is not known, and
	// LeftOutOwedAmount what is owed on them, so the whole of what the
	// cards owe can still be said.
	LeftOutCardCount  int    `json:"leftOutCardCount"`
	LeftOutOwedAmount string `json:"leftOutOwedAmount"`

	// CreditCards is every card, the highest usage first, then those whose
	// usage cannot be measured.
	CreditCards []*CreditCardUsageView `json:"creditCards"`
}

// CreditCardUsageView is one credit card: what it owes against its credit
// limit, in its own currency and in the reporting currency.
type CreditCardUsageView struct {
	FinanceAccountID string `json:"financeAccountId"`
	AccountName      string `json:"accountName"`
	AccountMask      string `json:"accountMask,omitempty" graphapi:"nullable"`
	InstitutionName  string `json:"institutionName,omitempty" graphapi:"nullable"`
	CurrencyCode     string `json:"currencyCode"`

	// OwedAmount is what the card owes, never negative: a card paid past
	// its balance owes nothing. Empty when the provider gave no balance.
	OwedAmount string `json:"owedAmount,omitempty" graphapi:"nullable"`

	// CreditLimitAmount is the limit usage is measured against, and
	// CreditLimitSource where it comes from: the provider, or derived as
	// what is owed plus the credit still available. Empty, and unknown,
	// when neither can be had.
	CreditLimitAmount string                   `json:"creditLimitAmount,omitempty" graphapi:"nullable"`
	CreditLimitSource models.CreditLimitSource `json:"creditLimitSource"`

	// UsageShare is OwedAmount over CreditLimitAmount, as the summary's;
	// empty when either is unknown.
	UsageShare *float64 `json:"usageShare,omitempty" graphapi:"nullable"`

	// ConvertedOwedAmount and ConvertedCreditLimitAmount are the two in
	// the reporting currency; empty when unknown or when there is no
	// exchange rate.
	ConvertedOwedAmount        string `json:"convertedOwedAmount,omitempty" graphapi:"nullable"`
	ConvertedCreditLimitAmount string `json:"convertedCreditLimitAmount,omitempty" graphapi:"nullable"`

	BalanceAt *time.Time `json:"balanceAt,omitempty" graphapi:"nullable"`
}

// CreditUsageArguments may name the currency to convert into instead of
// the reporting currency.
type CreditUsageArguments struct {
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

func (self *graph) CreditUsage(ctx context.Context, arguments CreditUsageArguments) (*CreditUsageView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	sources, err := financeSourcesOf(tx, found.ID)
	if err != nil {
		return nil, err
	}
	sourceById := map[string]*models.AgentKnowledgeSource{}
	for _, source := range sources {
		sourceById[source.ID] = source
	}
	accounts, err := tx.ListFinanceAccounts(found.ID, "")
	if err != nil {
		return nil, err
	}
	views := make([]*FinanceAccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, financeAccountView(account, sourceById[account.SourceID]))
	}
	converter := rates.NewConverter(ctx, self.exchangeRateFetcher(), tx)
	today := personToday(principal)
	view, err := creditUsageOf(views, currencyCode, func(amount, fromCurrencyCode string) (*big.Rat, bool, error) {
		return convertOrSkip(converter, amount, fromCurrencyCode, currencyCode, today)
	})
	if err != nil {
		return nil, financeError(err)
	}
	return view, nil
}

// creditUsageConverter converts an amount into the reporting currency,
// answering false when there is no exchange rate for it.
type creditUsageConverter func(amount, fromCurrencyCode string) (*big.Rat, bool, error)

// creditUsageOf is the credit usage of the credit cards among a person's
// finance accounts, converted with convert into the reporting currency
// (none when it is empty).
func creditUsageOf(accounts []*FinanceAccountView, reportingCurrencyCode string, convert creditUsageConverter) (*CreditUsageView, error) {
	view := &CreditUsageView{
		ReportingCurrencyCode: reportingCurrencyCode, UnconvertedCurrencyCodes: []string{}, CreditCards: []*CreditCardUsageView{},
	}
	totalOwed, totalCreditLimit, leftOutOwed := new(big.Rat), new(big.Rat), new(big.Rat)
	isAnyMeasured := false
	unconverted := map[string]bool{}
	for _, account := range accounts {
		if account.AccountKind != models.FinanceAccountKindCredit {
			continue
		}
		usage, err := creditCardUsageOf(account.CurrentBalance, account.AvailableBalance, account.CreditLimitAmount,
			finance.IsOwedBalancePositive(finance.ProviderKind(account.ProviderKind)))
		if err != nil {
			return nil, err
		}
		card := &CreditCardUsageView{
			FinanceAccountID: account.ID, AccountName: account.AccountName, AccountMask: account.AccountMask,
			InstitutionName: account.InstitutionName, CurrencyCode: account.CurrencyCode,
			CreditLimitSource: usage.creditLimitSource, BalanceAt: account.BalanceAt,
		}
		view.CreditCards = append(view.CreditCards, card)
		if usage.owedAmount != nil {
			card.OwedAmount = finance.FormatAmount(usage.owedAmount)
		}
		if usage.creditLimitAmount != nil {
			card.CreditLimitAmount = finance.FormatAmount(usage.creditLimitAmount)
		}
		isMeasured := usage.owedAmount != nil && usage.creditLimitAmount != nil
		if isMeasured {
			card.UsageShare = usageShare(usage.owedAmount, usage.creditLimitAmount)
		} else {
			view.LeftOutCardCount++
		}
		if reportingCurrencyCode == "" || unconverted[account.CurrencyCode] {
			continue
		}
		var convertedOwed, convertedCreditLimit *big.Rat
		isConverted := true
		if usage.owedAmount != nil {
			if convertedOwed, isConverted, err = convert(card.OwedAmount, account.CurrencyCode); err != nil {
				return nil, err
			}
		}
		if isConverted && usage.creditLimitAmount != nil {
			if convertedCreditLimit, isConverted, err = convert(card.CreditLimitAmount, account.CurrencyCode); err != nil {
				return nil, err
			}
		}
		if !isConverted {
			unconverted[account.CurrencyCode] = true
			continue
		}
		if convertedOwed != nil {
			card.ConvertedOwedAmount = finance.FormatAmount(convertedOwed)
		}
		if convertedCreditLimit != nil {
			card.ConvertedCreditLimitAmount = finance.FormatAmount(convertedCreditLimit)
		}
		switch {
		case isMeasured:
			totalOwed.Add(totalOwed, convertedOwed)
			totalCreditLimit.Add(totalCreditLimit, convertedCreditLimit)
			isAnyMeasured = true
		case convertedOwed != nil:
			leftOutOwed.Add(leftOutOwed, convertedOwed)
		}
	}
	view.UnconvertedCurrencyCodes = sortedCurrencyCodes(unconverted)
	view.TotalOwedAmount = finance.FormatAmount(totalOwed)
	view.TotalCreditLimitAmount = finance.FormatAmount(totalCreditLimit)
	view.LeftOutOwedAmount = finance.FormatAmount(leftOutOwed)
	if isAnyMeasured && totalCreditLimit.Sign() > 0 {
		view.UsageShare = usageShare(totalOwed, totalCreditLimit)
	}
	sort.SliceStable(view.CreditCards, func(left, right int) bool {
		leftCard, rightCard := view.CreditCards[left], view.CreditCards[right]
		if (leftCard.UsageShare == nil) != (rightCard.UsageShare == nil) {
			return leftCard.UsageShare != nil
		}
		if leftCard.UsageShare != nil && *leftCard.UsageShare != *rightCard.UsageShare {
			return *leftCard.UsageShare > *rightCard.UsageShare
		}
		return strings.ToLower(leftCard.AccountName) < strings.ToLower(rightCard.AccountName)
	})
	return view, nil
}

// creditCardUsage is what one card owes and the limit it is measured
// against, both in its own currency; nil where unknown.
type creditCardUsage struct {
	owedAmount        *big.Rat
	creditLimitAmount *big.Rat
	creditLimitSource models.CreditLimitSource
}

// creditCardUsageOf works out a card's owed amount and credit limit from
// its stored balances, which keep the provider's sign
// (isOwedBalancePositive says which way it goes).
//
// The limit is the provider's where it gave one. Otherwise it is what is
// owed plus the credit still available, taken with the balance's sign,
// so a card paid past its balance (owing a negative amount, with that
// much more available) still comes to its real limit; the owed amount
// shown for it is zero. A derived limit of zero or less is no limit. An
// available credit of exactly zero is taken as not known rather than as
// a card at its limit: a provider with nothing to say about it sends a
// zero more often than a card sits exactly at its limit, and a card
// shown at 100% that is not would be the worse mistake.
func creditCardUsageOf(currentBalance, availableBalance, creditLimitAmount string, isOwedBalancePositive bool) (creditCardUsage, error) {
	usage := creditCardUsage{creditLimitSource: models.CreditLimitSourceUnknown}
	var signedOwed *big.Rat
	if strings.TrimSpace(currentBalance) != "" {
		balance, err := finance.ParseAmount(currentBalance)
		if err != nil {
			return usage, err
		}
		signedOwed = balance
		if !isOwedBalancePositive {
			signedOwed = new(big.Rat).Neg(balance)
		}
		usage.owedAmount = new(big.Rat).Set(signedOwed)
		if usage.owedAmount.Sign() < 0 {
			usage.owedAmount = new(big.Rat)
		}
	}
	if strings.TrimSpace(creditLimitAmount) != "" {
		creditLimit, err := finance.ParseAmount(creditLimitAmount)
		if err != nil {
			return usage, err
		}
		if creditLimit.Sign() > 0 {
			usage.creditLimitAmount, usage.creditLimitSource = creditLimit, models.CreditLimitSourceProvider
			return usage, nil
		}
	}
	if signedOwed != nil && strings.TrimSpace(availableBalance) != "" {
		available, err := finance.ParseAmount(availableBalance)
		if err != nil {
			return usage, err
		}
		if available.Sign() != 0 {
			derived := new(big.Rat).Add(signedOwed, available)
			if derived.Sign() > 0 {
				usage.creditLimitAmount, usage.creditLimitSource = derived, models.CreditLimitSourceDerived
			}
		}
	}
	return usage, nil
}

// usageShare is owed over a positive limit, to four places.
func usageShare(owedAmount, creditLimitAmount *big.Rat) *float64 {
	share, _ := new(big.Rat).Quo(owedAmount, creditLimitAmount).Float64()
	share = math.Round(share*10000) / 10000
	return &share
}
