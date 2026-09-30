package finance

import (
	"context"
	"strings"
	"testing"
)

// The products a link asks for: transactions as the one Plaid must find,
// the others where the institution has them, and transactions alone when
// the operator's Plaid account is not enabled for the others.
func TestPlaidCreateLinkTokenAsksForOptionalProducts(t *testing.T) {
	isInvalidProductAnswered := false
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		if _, hasOptional := body["optional_products"]; hasOptional && !isInvalidProductAnswered {
			isInvalidProductAnswered = true
			return 400, `{"error_type":"INVALID_REQUEST","error_code":"INVALID_PRODUCT","error_message":"not enabled"}`
		}
		return 200, `{"link_token":"link-example"}`
	})
	plaid, err := NewPlaid(PlaidEnvironmentSandbox, "client-example", "secret-example", nil, []string{"transactions", "investments", "liabilities"})
	if err != nil {
		t.Fatal(err)
	}
	plaid.baseUrl = testServer.server.URL

	linkToken, err := plaid.CreateLinkToken(context.Background(), "person-example", "")
	if err != nil || linkToken != "link-example" {
		t.Fatalf("CreateLinkToken: %v %q", err, linkToken)
	}
	if len(testServer.requestBodies) != 2 {
		t.Fatalf("%d requests, want the refused one and the one without optional products", len(testServer.requestBodies))
	}
	first, second := testServer.requestBodies[0], testServer.requestBodies[1]
	if products, _ := first["products"].([]any); len(products) != 1 || products[0] != "transactions" {
		t.Errorf("required products %v, want transactions alone", first["products"])
	}
	if optional, _ := first["optional_products"].([]any); len(optional) != 2 || optional[0] != "investments" || optional[1] != "liabilities" {
		t.Errorf("optional products %v", first["optional_products"])
	}
	if _, hasOptional := second["optional_products"]; hasOptional {
		t.Errorf("the second request still named optional products: %v", second)
	}
}

// plaidInvestmentsAnswer answers for a finance source linked with
// investments: a brokerage account holding a fund and cash, and four
// investment transactions over two pages.
func plaidInvestmentsAnswer(path string, body map[string]any) (int, string) {
	switch path {
	case "/transactions/sync":
		return 200, `{"added":[],"modified":[],"removed":[],"accounts":` + plaidAccountsJson + `,
			"next_cursor":"cursor-transactions","has_more":false}`
	case "/item/get":
		return 200, `{"item":{"item_id":"item-example","products":["transactions","investments"]}}`
	case "/investments/holdings/get":
		return 200, `{"accounts":[
			{"account_id":"account-1","name":"Everyday Checking","type":"depository","balances":{"current":1520.1,"iso_currency_code":"USD"}},
			{"account_id":"account-brokerage","name":"Individual","mask":"0009","type":"investment","subtype":"brokerage",
			 "balances":{"current":2150.25,"iso_currency_code":"USD"}}],
			"holdings":[
			{"account_id":"account-brokerage","security_id":"security-fund","quantity":12.123456789,"institution_price":150.5,
			 "institution_value":1824.58,"cost_basis":1500,"iso_currency_code":"USD","unknown_holding_field":"kept"},
			{"account_id":"account-brokerage","security_id":"security-cash","quantity":325.67,"institution_price":1,
			 "institution_value":325.67,"iso_currency_code":"USD"}],
			"securities":[
			{"security_id":"security-fund","name":"Example Index Fund","ticker_symbol":"EXIF","type":"mutual fund",
			 "close_price":150.5,"close_price_as_of":"2026-09-29","iso_currency_code":"USD"},
			{"security_id":"security-cash","name":"U S Dollar","ticker_symbol":"CUR:USD","type":"cash","iso_currency_code":"USD"}]}`
	case "/investments/transactions/get":
		options, _ := body["options"].(map[string]any)
		if offset, _ := options["offset"].(float64); offset == 0 {
			return 200, `{"total_investment_transactions":4,"securities":[],"investment_transactions":[
				{"investment_transaction_id":"investment-buy","account_id":"account-brokerage","security_id":"security-fund",
				 "date":"2026-09-02","name":"BUY EXIF","quantity":2,"amount":301,"price":150.5,"fees":0,"type":"buy","subtype":"buy",
				 "iso_currency_code":"USD"},
				{"investment_transaction_id":"investment-dividend","account_id":"account-brokerage","security_id":"security-fund",
				 "date":"2026-09-15","name":"DIVIDEND EXIF","quantity":0,"amount":-4.2,"price":0,"type":"cash","subtype":"dividend",
				 "iso_currency_code":"USD"},
				{"investment_transaction_id":"investment-deposit","account_id":"account-brokerage",
				 "date":"2026-09-01","name":"DEPOSIT","quantity":0,"amount":-500,"price":0,"type":"cash","subtype":"deposit",
				 "iso_currency_code":"USD"}]}`
		}
		return 200, `{"total_investment_transactions":4,"securities":[],"investment_transactions":[
			{"investment_transaction_id":"investment-fee","account_id":"account-brokerage",
			 "date":"2026-09-20","name":"ACCOUNT FEE","quantity":0,"amount":1.5,"price":0,"type":"fee","subtype":"account fee",
			 "iso_currency_code":"USD"}]}`
	}
	return 404, `{}`
}

// A finance source linked with investments: its investment account joins
// the accounts, its holdings come without the cash one, its buy is a trade
// and its cash movements are transactions with Plaid's categories, and the
// next sync reads investment transactions from a month before this one.
func TestPlaidSyncReadsInvestments(t *testing.T) {
	testServer := newPlaidTestServer(t, plaidInvestmentsAnswer)
	plaid := newTestPlaid(t, testServer)

	result, err := plaid.Sync(context.Background(), "access-example", "cursor-before")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Accounts) != 2 || result.Accounts[1].AccountKind != AccountKindInvestment || result.Accounts[1].CurrentBalance != "2150.2500" {
		t.Fatalf("accounts %+v, want the checking account and the brokerage", result.Accounts)
	}
	if len(result.HoldingsReadAccountIDs) != 1 || result.HoldingsReadAccountIDs[0] != "account-brokerage" {
		t.Errorf("holdings read for %v, want the brokerage alone", result.HoldingsReadAccountIDs)
	}
	if len(result.Holdings) != 1 {
		t.Fatalf("holdings %+v, want the fund without the cash", result.Holdings)
	}
	holding := result.Holdings[0]
	if holding.HeldQuantity != "12.12345679" || holding.UnitPrice != "150.50000000" || holding.HoldingValue != "1824.5800" || holding.CostBasis != "1500.0000" {
		t.Errorf("holding %+v", holding)
	}
	if !strings.Contains(string(holding.ProviderMetadata), `"unknown_holding_field":"kept"`) {
		t.Errorf("holding metadata lost an unknown field: %s", holding.ProviderMetadata)
	}
	securityKindByID := map[string]string{}
	for _, security := range result.Securities {
		securityKindByID[security.ProviderSecurityID] = security.SecurityKind
	}
	if securityKindByID["security-fund"] != SecurityKindMutualFund || securityKindByID["security-cash"] != SecurityKindCash {
		t.Errorf("security kinds %v", securityKindByID)
	}

	if len(result.Trades) != 1 {
		t.Fatalf("trades %+v, want the buy alone", result.Trades)
	}
	trade := result.Trades[0]
	if trade.TradeKind != TradeKindBuy || trade.TradeAmount != "-301.0000" || trade.TradedQuantity != "2.00000000" || trade.ProviderSecurityID != "security-fund" {
		t.Errorf("trade %+v: a buy takes cash out of the account", trade)
	}
	categoryByID := map[string]string{}
	amountByID := map[string]string{}
	for _, transaction := range result.Added {
		categoryByID[transaction.ProviderTransactionID] = transaction.ProviderCategoryDetailed
		amountByID[transaction.ProviderTransactionID] = transaction.Amount
	}
	for transactionId, want := range map[string]string{
		"investment-dividend": "INCOME_DIVIDENDS",
		"investment-deposit":  "TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS",
		"investment-fee":      "BANK_FEES_OTHER_BANK_FEES",
	} {
		if categoryByID[transactionId] != want {
			t.Errorf("%s category %q, want %q", transactionId, categoryByID[transactionId], want)
		}
	}
	if amountByID["investment-dividend"] != "4.2000" || amountByID["investment-fee"] != "-1.5000" {
		t.Errorf("amounts %v: a dividend is money in, a fee money out", amountByID)
	}

	cursor := parsePlaidCursor(result.NextCursor)
	if cursor.TransactionsCursor != "cursor-transactions" || cursor.InvestmentsReadThrough == "" {
		t.Fatalf("cursor %q", result.NextCursor)
	}
	var firstStartDay string
	for index, body := range testServer.requestBodies {
		if testServer.requestPaths[index] == "/investments/transactions/get" {
			firstStartDay, _ = body["start_date"].(string)
			break
		}
	}

	testServer.requestPaths, testServer.requestBodies = nil, nil
	if _, err := plaid.Sync(context.Background(), "access-example", result.NextCursor); err != nil {
		t.Fatal(err)
	}
	for index, body := range testServer.requestBodies {
		switch testServer.requestPaths[index] {
		case "/transactions/sync":
			if body["cursor"] != "cursor-transactions" {
				t.Errorf("the next sync asked for cursor %v", body["cursor"])
			}
		case "/investments/transactions/get":
			startDay, _ := body["start_date"].(string)
			if startDay <= firstStartDay {
				t.Errorf("the next sync read investment transactions from %s, the first from %s", startDay, firstStartDay)
			}
		}
	}
}

// Holdings not ready yet after a link are a warning, not a failed sync,
// and the next sync reads investment transactions from the beginning.
func TestPlaidSyncWithHoldingsNotReady(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		if path == "/investments/holdings/get" {
			return 400, `{"error_type":"ITEM_ERROR","error_code":"PRODUCT_NOT_READY","error_message":"not ready"}`
		}
		return plaidInvestmentsAnswer(path, body)
	})
	plaid := newTestPlaid(t, testServer)

	result, err := plaid.Sync(context.Background(), "access-example", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.HoldingsReadAccountIDs) != 0 || len(result.Holdings) != 0 || len(result.Trades) != 0 {
		t.Errorf("nothing about investments is known: %+v", result)
	}
	if len(result.ProviderWarnings) != 1 || !strings.Contains(result.ProviderWarnings[0], "PRODUCT_NOT_READY") {
		t.Errorf("warnings %v", result.ProviderWarnings)
	}
	if result.NextCursor != "cursor-transactions" {
		t.Errorf("cursor %q, want the plain transactions cursor", result.NextCursor)
	}
}

// A finance source linked with transactions alone never asks for
// investments, which would start billing for them.
func TestPlaidSyncWithoutInvestmentsAsksNothingOfThem(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		if path == "/item/get" {
			return 404, `{}`
		}
		return plaidInvestmentsAnswer(path, body)
	})
	plaid := newTestPlaid(t, testServer)

	if _, err := plaid.Sync(context.Background(), "access-example", ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range testServer.requestPaths {
		if strings.HasPrefix(path, "/investments/") {
			t.Errorf("asked %s of a source without investments", path)
		}
	}
}
