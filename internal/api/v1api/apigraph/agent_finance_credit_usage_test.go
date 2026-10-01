package apigraph

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A card's owed amount and limit, from balances in either provider's
// sign: the provider's limit first, else owed plus available, else none.
func TestCreditCardUsageOf(test *testing.T) {
	test.Parallel()
	for _, example := range []struct {
		name                  string
		currentBalance        string
		availableBalance      string
		creditLimitAmount     string
		isOwedBalancePositive bool
		wantedOwed            string
		wantedCreditLimit     string
		wantedSource          models.CreditLimitSource
	}{
		{"the provider's limit", "250", "4750", "5000", true, "250.0000", "5000.0000", models.CreditLimitSourceProvider},
		{"the provider's limit beats the balances", "250", "100", "5000", true, "250.0000", "5000.0000", models.CreditLimitSourceProvider},
		{"derived, owed positive", "250", "750", "", true, "250.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"derived, owed negative", "-250", "750", "", false, "250.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"past the limit", "-1100", "-100", "", false, "1100.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"overpaid owes nothing and keeps its limit", "-40", "1040", "", true, "0.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"overpaid, owed negative", "40", "1040", "", false, "0.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"no available credit", "-80", "", "", false, "80.0000", "", models.CreditLimitSourceUnknown},
		{"an available credit of zero says nothing", "-80", "0", "", false, "80.0000", "", models.CreditLimitSourceUnknown},
		{"a derived limit of nothing", "-500", "-500", "", false, "500.0000", "", models.CreditLimitSourceUnknown},
		{"a provider's limit of zero falls back", "300", "700", "0", true, "300.0000", "1000.0000", models.CreditLimitSourceDerived},
		{"no balance, a provider's limit", "", "", "2000", true, "", "2000.0000", models.CreditLimitSourceProvider},
		{"no balance, nothing to derive from", "", "900", "", true, "", "", models.CreditLimitSourceUnknown},
	} {
		usage, err := creditCardUsageOf(example.currentBalance, example.availableBalance, example.creditLimitAmount, example.isOwedBalancePositive)
		if err != nil {
			test.Errorf("%s: %s", example.name, err)
			continue
		}
		owed, creditLimit := "", ""
		if usage.owedAmount != nil {
			owed = finance.FormatAmount(usage.owedAmount)
		}
		if usage.creditLimitAmount != nil {
			creditLimit = finance.FormatAmount(usage.creditLimitAmount)
		}
		if owed != example.wantedOwed || creditLimit != example.wantedCreditLimit || usage.creditLimitSource != example.wantedSource {
			test.Errorf("%s: owed %q limit %q from %s, want %q %q %s", example.name, owed, creditLimit, usage.creditLimitSource,
				example.wantedOwed, example.wantedCreditLimit, example.wantedSource)
		}
	}
	if _, err := creditCardUsageOf("a lot", "", "", true); err == nil {
		test.Error("a balance that is not a decimal was read")
	}
}

// The summary adds up the cards it can measure, converted, and leaves out
// and counts the ones it cannot; each card's share is in its own currency.
func TestCreditUsageOfAddsUpTheMeasuredCards(test *testing.T) {
	test.Parallel()
	euroInDollars := big.NewRat(5, 4)
	convert := func(amount, fromCurrencyCode string) (*big.Rat, bool, error) {
		value, err := finance.ParseAmount(amount)
		if err != nil {
			return nil, false, err
		}
		switch fromCurrencyCode {
		case "USD":
			return value, true, nil
		case "EUR":
			return value.Mul(value, euroInDollars), true, nil
		}
		return nil, false, nil
	}
	credit := models.FinanceAccountKindCredit
	accounts := []*FinanceAccountView{
		{ID: "checking", ProviderKind: "plaid", AccountName: "Everyday Checking", AccountKind: models.FinanceAccountKindDepository, CurrencyCode: "USD", CurrentBalance: "900.0000"},
		{ID: "provider", ProviderKind: "plaid", AccountName: "Card A", AccountKind: credit, CurrencyCode: "USD", CurrentBalance: "600.0000", AvailableBalance: "400.0000", CreditLimitAmount: "1000.0000"},
		{ID: "derived", ProviderKind: "simplefin", AccountName: "Card B", AccountKind: credit, CurrencyCode: "USD", CurrentBalance: "-250.0000", AvailableBalance: "750.0000"},
		{ID: "euro", ProviderKind: "statement", AccountName: "Card C", AccountKind: credit, CurrencyCode: "EUR", CurrentBalance: "-100.0000", AvailableBalance: "400.0000"},
		{ID: "unknown", ProviderKind: "simplefin", AccountName: "Card D", AccountKind: credit, CurrencyCode: "USD", CurrentBalance: "-80.0000"},
		{ID: "unrated", ProviderKind: "simplefin", AccountName: "Card E", AccountKind: credit, CurrencyCode: "GBP", CurrentBalance: "-50.0000", AvailableBalance: "950.0000"},
		{ID: "overpaid", ProviderKind: "plaid", AccountName: "Card F", AccountKind: credit, CurrencyCode: "USD", CurrentBalance: "-40.0000", AvailableBalance: "1040.0000"},
	}
	view, err := creditUsageOf(accounts, "USD", convert)
	if err != nil {
		test.Fatal(err)
	}
	// 600 + 250 + 100 EUR at 1.25 + nothing, against 1000 + 1000 + 500
	// EUR at 1.25 + 1000.
	if view.TotalOwedAmount != "975.0000" || view.TotalCreditLimitAmount != "3625.0000" || view.UsageShare == nil || *view.UsageShare != 0.269 {
		test.Errorf("totals %s of %s, share %v", view.TotalOwedAmount, view.TotalCreditLimitAmount, view.UsageShare)
	}
	if view.LeftOutCardCount != 1 || view.LeftOutOwedAmount != "80.0000" || strings.Join(view.UnconvertedCurrencyCodes, ",") != "GBP" {
		test.Errorf("left out %d owing %s, unconverted %v", view.LeftOutCardCount, view.LeftOutOwedAmount, view.UnconvertedCurrencyCodes)
	}
	var order []string
	byId := map[string]*CreditCardUsageView{}
	for _, card := range view.CreditCards {
		order = append(order, card.FinanceAccountID)
		byId[card.FinanceAccountID] = card
	}
	if strings.Join(order, ",") != "provider,derived,euro,unrated,overpaid,unknown" {
		test.Errorf("cards in the order %v", order)
	}
	if card := byId["euro"]; card.OwedAmount != "100.0000" || card.ConvertedOwedAmount != "125.0000" || card.ConvertedCreditLimitAmount != "625.0000" ||
		*card.UsageShare != 0.2 || card.CreditLimitSource != models.CreditLimitSourceDerived {
		test.Errorf("euro card %+v", card)
	}
	if card := byId["unrated"]; card.ConvertedOwedAmount != "" || card.UsageShare == nil || *card.UsageShare != 0.05 {
		test.Errorf("a card with no rate keeps its own share: %+v", card)
	}
	if card := byId["overpaid"]; card.OwedAmount != "0.0000" || card.CreditLimitAmount != "1000.0000" || *card.UsageShare != 0 {
		test.Errorf("overpaid card %+v", card)
	}
	if card := byId["unknown"]; card.UsageShare != nil || card.CreditLimitAmount != "" || card.CreditLimitSource != models.CreditLimitSourceUnknown {
		test.Errorf("unknown card %+v", card)
	}
	if card := byId["provider"]; card.CreditLimitSource != models.CreditLimitSourceProvider || *card.UsageShare != 0.6 {
		test.Errorf("provider card %+v", card)
	}

	// Nothing to convert into: each card still has its own share, and no
	// totals.
	view, err = creditUsageOf(accounts, "", convert)
	if err != nil {
		test.Fatal(err)
	}
	if view.UsageShare != nil || view.TotalOwedAmount != "0.0000" || len(view.CreditCards) != 6 || view.LeftOutCardCount != 1 {
		test.Errorf("without a reporting currency %+v", view)
	}

	// No cards at all.
	view, err = creditUsageOf(accounts[:1], "USD", convert)
	if err != nil || len(view.CreditCards) != 0 || view.UsageShare != nil {
		test.Errorf("no cards %+v %v", view, err)
	}
}

// Through the API, from what syncs stored: a card whose institution writes
// what is owed as negative, a limit the provider gave, a card in another
// currency converted at today's rate, and a card with no limit to find.
// Another person sees none of the owner's cards.
func TestCreditUsageThroughTheAPI(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	balanceAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if _, err := tx.ApplyFinanceSync(fixture.ownerAgent.ID, source.ID, &finance.SyncResult{Accounts: []finance.Account{
			{ProviderAccountID: "card-derived", AccountName: "Example Rewards Card", AccountKind: "credit", CurrencyCode: "USD",
				CurrentBalance: "-250", AvailableBalance: "750", BalanceAt: balanceAt},
			{ProviderAccountID: "card-limited", AccountName: "Example Travel Card", AccountKind: "credit", CurrencyCode: "USD",
				CurrentBalance: "-600", AvailableBalance: "100", CreditLimitAmount: "1000", BalanceAt: balanceAt},
			{ProviderAccountID: "card-euro", AccountName: "Example Euro Card", AccountKind: "credit", CurrencyCode: "EUR",
				CurrentBalance: "-100", AvailableBalance: "400", BalanceAt: balanceAt},
			{ProviderAccountID: "card-unknown", AccountName: "Example Store Card", AccountKind: "credit", CurrencyCode: "USD",
				CurrentBalance: "-80", BalanceAt: balanceAt},
		}}, "2026-09-20"); err != nil {
			test.Fatal(err)
		}
		// Converted at today's rate, and a rate is used up to a week after
		// its day, so the rate is dated from the clock.
		if _, err := tx.UpsertExchangeRates([]models.ExchangeRate{
			{RateOn: time.Now().UTC().AddDate(0, 0, -2).Format(time.DateOnly), CurrencyCode: "USD", EuroRate: "1.2500000000", RateSource: models.RateSourceECB},
		}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		usage, err := fixture.resolver.CreditUsage(ctx, CreditUsageArguments{})
		if err != nil {
			test.Fatal(err)
		}
		// The checking account the fixture seeds makes USD the reporting
		// currency. 250 + 600 + 125, against 1000 + 1000 + 625.
		if usage.ReportingCurrencyCode != "USD" || usage.TotalOwedAmount != "975.0000" || usage.TotalCreditLimitAmount != "2625.0000" ||
			usage.UsageShare == nil || *usage.UsageShare != 0.3714 {
			test.Errorf("usage %+v share %v", usage, usage.UsageShare)
		}
		if len(usage.CreditCards) != 4 || usage.LeftOutCardCount != 1 || usage.LeftOutOwedAmount != "80.0000" {
			test.Fatalf("cards %+v", usage)
		}
		first := usage.CreditCards[0]
		if first.AccountName != "Example Travel Card" || first.CreditLimitSource != models.CreditLimitSourceProvider || first.OwedAmount != "600.0000" {
			test.Errorf("the highest usage first: %+v", first)
		}
		if last := usage.CreditCards[3]; last.AccountName != "Example Store Card" || last.CreditLimitSource != models.CreditLimitSourceUnknown {
			test.Errorf("the unknown limit last: %+v", last)
		}

		// Into a currency nobody publishes, nothing adds up and both are
		// named, while each card keeps its own share.
		usage, err = fixture.resolver.CreditUsage(ctx, CreditUsageArguments{CurrencyCode: "ZZZ"})
		if err != nil {
			test.Fatal(err)
		}
		if usage.UsageShare != nil || strings.Join(usage.UnconvertedCurrencyCodes, ",") != "EUR,USD" || usage.CreditCards[0].UsageShare == nil {
			test.Errorf("unconverted %+v", usage)
		}

		accounts, err := fixture.resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil {
			test.Fatal(err)
		}
		for _, account := range accounts {
			if account.AccountName == "Example Travel Card" && account.CreditLimitAmount != "1000.0000" {
				test.Errorf("the account carries the provider's limit: %+v", account)
			}
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		usage, err := fixture.resolver.CreditUsage(ctx, CreditUsageArguments{})
		if err != nil {
			test.Fatal(err)
		}
		if len(usage.CreditCards) != 0 || usage.UsageShare != nil {
			test.Errorf("a stranger sees %+v", usage)
		}
	})
}
