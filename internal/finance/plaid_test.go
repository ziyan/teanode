package finance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// plaidTestServer answers Plaid calls from a function per path and records
// every request body it was sent.
type plaidTestServer struct {
	server        *httptest.Server
	mutex         sync.Mutex
	requestBodies []map[string]any
	userAgents    []string
}

func newPlaidTestServer(t *testing.T, answer func(path string, body map[string]any) (int, string)) *plaidTestServer {
	t.Helper()
	testServer := &plaidTestServer{}
	testServer.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		encoded, _ := io.ReadAll(request.Body)
		var body map[string]any
		if err := json.Unmarshal(encoded, &body); err != nil {
			t.Errorf("request to %s was not JSON: %v", request.URL.Path, err)
		}
		testServer.mutex.Lock()
		testServer.requestBodies = append(testServer.requestBodies, body)
		testServer.userAgents = append(testServer.userAgents, request.Header.Get("User-Agent"))
		testServer.mutex.Unlock()
		statusCode, answerText := answer(request.URL.Path, body)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(statusCode)
		_, _ = writer.Write([]byte(answerText))
	}))
	t.Cleanup(testServer.server.Close)
	return testServer
}

func newTestPlaid(t *testing.T, testServer *plaidTestServer) *Plaid {
	t.Helper()
	plaid, err := NewPlaid(PlaidEnvironmentSandbox, "client-example", "secret-example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plaid.baseUrl = testServer.server.URL
	return plaid
}

const plaidAccountsJson = `[{"account_id":"account-1","name":"Everyday Checking","mask":"0001","type":"depository",
	"balances":{"current":1520.1,"available":1500,"iso_currency_code":"USD"},"unknown_account_field":"kept"}]`

func TestPlaidSyncPagesSignsAndKeepsMetadata(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		if path != "/transactions/sync" {
			return 404, `{}`
		}
		switch body["cursor"] {
		case nil:
			return 200, `{"added":[
				{"transaction_id":"transaction-1","account_id":"account-1","amount":12.5,"iso_currency_code":"USD",
				 "date":"2026-08-01","name":"CORNER GROCER 0001","merchant_name":"Corner Grocer","pending":false,
				 "personal_finance_category":{"primary":"FOOD_AND_DRINK","detailed":"FOOD_AND_DRINK_GROCERIES"},
				 "unknown_transaction_field":{"nested":[1,2]}},
				{"transaction_id":"transaction-2","account_id":"account-1","amount":-2000.10,"iso_currency_code":"USD",
				 "date":"2026-08-02","name":"PAYROLL EXAMPLE","pending":false}],
				"modified":[],"removed":[],"accounts":` + plaidAccountsJson + `,
				"next_cursor":"cursor-page-2","has_more":true}`
		case "cursor-page-2":
			return 200, `{"added":[
				{"transaction_id":"transaction-3","account_id":"account-1","amount":0.1,"iso_currency_code":null,
				 "date":"2026-08-03","name":"PENDING COFFEE","pending":true,
				 "authorized_datetime":"2026-08-03T08:15:00Z"}],
				"modified":[
				{"transaction_id":"transaction-1","account_id":"account-1","amount":13,"iso_currency_code":"USD",
				 "date":"2026-08-01","name":"CORNER GROCER 0001","pending":false}],
				"removed":[{"transaction_id":"transaction-old","account_id":"account-1"}],
				"accounts":` + plaidAccountsJson + `,
				"next_cursor":"cursor-final","has_more":false}`
		}
		return 400, `{"error_code":"INVALID_FIELD","error_message":"unexpected cursor"}`
	})
	plaid := newTestPlaid(t, testServer)

	result, err := plaid.Sync(context.Background(), "access-example", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.NextCursor != "cursor-final" {
		t.Errorf("next cursor %q", result.NextCursor)
	}
	if len(result.Added) != 3 {
		t.Fatalf("added %d transactions, want 3 (the modified one replaces its first copy)", len(result.Added))
	}
	amountByID := map[string]string{}
	for _, transaction := range result.Added {
		amountByID[transaction.ProviderTransactionID] = transaction.Amount
	}
	// Plaid's positive is money out; ours is negative. The modified copy
	// of transaction-1 wins over the added one.
	for transactionId, want := range map[string]string{"transaction-1": "-13.0000", "transaction-2": "2000.1000", "transaction-3": "-0.1000"} {
		if amountByID[transactionId] != want {
			t.Errorf("%s amount %q, want %q", transactionId, amountByID[transactionId], want)
		}
	}
	pending := result.Added[2]
	if !pending.IsPending || pending.CurrencyCode != "USD" || pending.TransactedAt == nil {
		t.Errorf("pending transaction %+v: want pending, the account's currency and a time", pending)
	}
	if len(result.RemovedProviderTransactionIDs) != 1 || result.RemovedProviderTransactionIDs[0] != "transaction-old" {
		t.Errorf("removed %v", result.RemovedProviderTransactionIDs)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts %d, want 1", len(result.Accounts))
	}
	account := result.Accounts[0]
	if account.CurrentBalance != "1520.1000" || account.AvailableBalance != "1500.0000" || account.AccountKind != AccountKindDepository || account.AccountMask != "0001" {
		t.Errorf("account %+v", account)
	}
	if !account.IsOwedBalancePositive {
		t.Errorf("Plaid reports what is owed as positive, and the account must say so: %+v", account)
	}
	if !strings.Contains(string(account.ProviderMetadata), `"unknown_account_field":"kept"`) {
		t.Errorf("account metadata lost an unknown field: %s", account.ProviderMetadata)
	}

	// The first page's copy of transaction-1 carried the unknown field; the
	// modified copy that replaced it did not. Check the field survives on a
	// transaction that was not replaced by reading page one again alone.
	first, err := plaidTransactionFrom(json.RawMessage(`{"transaction_id":"transaction-9","account_id":"account-1","amount":1,"date":"2026-08-01","unknown_transaction_field":{"nested":[1,2]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.ProviderMetadata), `"unknown_transaction_field":{"nested":[1,2]}`) {
		t.Errorf("transaction metadata lost an unknown field: %s", first.ProviderMetadata)
	}

	for index, body := range testServer.requestBodies {
		if body["client_id"] != "client-example" || body["secret"] != "secret-example" || body["access_token"] != "access-example" {
			t.Errorf("request %d did not carry the keys and credential: %v", index, body)
		}
		if count, _ := body["count"].(float64); count != 500 {
			t.Errorf("request %d asked for %v transactions", index, body["count"])
		}
		if !strings.HasPrefix(testServer.userAgents[index], "teanode/") {
			t.Errorf("request %d user agent %q", index, testServer.userAgents[index])
		}
	}
}

func TestPlaidSignFlipIsExact(t *testing.T) {
	for _, testCase := range []struct {
		plaidAmount string
		want        string
	}{
		{"12.5", "-12.5000"},
		{"-7.25", "7.2500"},
		{"0", "0.0000"},
		{"0.1", "-0.1000"},
		{"123456789012345.67", "-123456789012345.6700"},
		{"1e-2", "-0.0100"},
		{"0.00005", "-0.0001"},
	} {
		got, err := negatedJsonAmount(json.Number(testCase.plaidAmount))
		if err != nil {
			t.Errorf("%s: %v", testCase.plaidAmount, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s negated to %q, want %q", testCase.plaidAmount, got, testCase.want)
		}
	}
	for _, refused := range []string{"", "1e400", "12,50", "NaN"} {
		if _, err := negatedJsonAmount(json.Number(refused)); err == nil {
			t.Errorf("%q was accepted", refused)
		}
	}
}

func TestPlaidSignInRequired(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		return 400, `{"error_type":"ITEM_ERROR","error_code":"ITEM_LOGIN_REQUIRED","error_message":"the login details of this item have changed","request_id":"request-example"}`
	})
	plaid := newTestPlaid(t, testServer)

	_, err := plaid.Sync(context.Background(), "access-example", "cursor-example")
	if !errors.Is(err, ErrSignInRequired) {
		t.Fatalf("error %v, want ErrSignInRequired", err)
	}
	var plaidError *PlaidError
	if !errors.As(err, &plaidError) || plaidError.ErrorCode != "ITEM_LOGIN_REQUIRED" {
		t.Errorf("error %v does not carry Plaid's code", err)
	}
	for _, secret := range []string{"secret-example", "access-example"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q contains a secret", err)
		}
	}
	if !strings.Contains(err.Error(), "request-example") {
		t.Errorf("error %q does not name the request", err)
	}
}

// A link that is gone or a consent withdrawn is a refused credential, not
// a sign-in to repair; any other refusal is neither.
func TestPlaidCredentialRefused(t *testing.T) {
	for errorCode, isRefused := range map[string]bool{
		"ITEM_NOT_FOUND":          true,
		"INVALID_ACCESS_TOKEN":    true,
		"ACCESS_NOT_GRANTED":      true,
		"USER_PERMISSION_REVOKED": true,
		"USER_ACCOUNT_REVOKED":    true,
		"INTERNAL_SERVER_ERROR":   false,
		"ITEM_LOGIN_REQUIRED":     false,
	} {
		testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
			return 400, `{"error_type":"ITEM_ERROR","error_code":"` + errorCode + `","error_message":"refused","request_id":"request-example"}`
		})
		plaid := newTestPlaid(t, testServer)
		_, err := plaid.Sync(context.Background(), "access-example", "cursor-example")
		if err == nil {
			t.Fatalf("%s: want an error", errorCode)
		}
		if errors.Is(err, ErrCredentialRefused) != isRefused {
			t.Errorf("%s: is a refused credential %v, want %v", errorCode, errors.Is(err, ErrCredentialRefused), isRefused)
		}
		if isRefused && errors.Is(err, ErrSignInRequired) {
			t.Errorf("%s: a refused credential is not a sign-in to repair", errorCode)
		}
	}
}

func TestPlaidSyncRestartsAfterMutationDuringPagination(t *testing.T) {
	var mutex sync.Mutex
	secondPageCount := 0
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		mutex.Lock()
		defer mutex.Unlock()
		switch body["cursor"] {
		case "cursor-start":
			return 200, `{"added":[{"transaction_id":"transaction-1","account_id":"account-1","amount":5,"iso_currency_code":"USD","date":"2026-08-01","name":"FIRST"}],
				"modified":[],"removed":[],"accounts":[],"next_cursor":"cursor-middle","has_more":true}`
		case "cursor-middle":
			secondPageCount++
			if secondPageCount == 1 {
				return 400, `{"error_type":"TRANSACTIONS_ERROR","error_code":"TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION","error_message":"underlying transaction data changed"}`
			}
			return 200, `{"added":[{"transaction_id":"transaction-2","account_id":"account-1","amount":6,"iso_currency_code":"USD","date":"2026-08-02","name":"SECOND"}],
				"modified":[],"removed":[],"accounts":[],"next_cursor":"cursor-end","has_more":false}`
		}
		return 400, `{"error_code":"INVALID_FIELD"}`
	})
	plaid := newTestPlaid(t, testServer)

	result, err := plaid.Sync(context.Background(), "access-example", "cursor-start")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 2 || result.NextCursor != "cursor-end" {
		t.Errorf("added %d, cursor %q; want 2 and cursor-end", len(result.Added), result.NextCursor)
	}
	// start, middle (refused), start again, middle.
	var cursors []any
	for _, body := range testServer.requestBodies {
		cursors = append(cursors, body["cursor"])
	}
	if len(cursors) != 4 || cursors[2] != "cursor-start" {
		t.Errorf("cursors asked %v, want a restart from cursor-start", cursors)
	}
}

func TestPlaidSyncGivesUpAfterRepeatedMutation(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		return 400, `{"error_code":"TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION"}`
	})
	plaid := newTestPlaid(t, testServer)
	if _, err := plaid.Sync(context.Background(), "access-example", ""); err == nil {
		t.Fatal("a source that never stops changing synced")
	}
	if len(testServer.requestBodies) != plaidSyncAttemptLimit {
		t.Errorf("%d attempts, want %d", len(testServer.requestBodies), plaidSyncAttemptLimit)
	}
}

func TestPlaidCreateLinkToken(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		if path != "/link/token/create" {
			return 404, `{}`
		}
		return 200, `{"link_token":"link-sandbox-example"}`
	})
	plaid := newTestPlaid(t, testServer)

	linkToken, err := plaid.CreateLinkToken(context.Background(), "person-example", "")
	if err != nil || linkToken != "link-sandbox-example" {
		t.Fatalf("link token %q, %v", linkToken, err)
	}
	created := testServer.requestBodies[0]
	if created["client_name"] != "TeaNode" || created["language"] != "en" {
		t.Errorf("link token request %v", created)
	}
	if user, _ := created["user"].(map[string]any); user["client_user_id"] != "person-example" {
		t.Errorf("person reference not sent: %v", created["user"])
	}
	if transactions, _ := created["transactions"].(map[string]any); transactions["days_requested"] != float64(730) {
		t.Errorf("days requested %v", created["transactions"])
	}
	if products, _ := created["products"].([]any); len(products) != 1 || products[0] != "transactions" {
		t.Errorf("products %v", created["products"])
	}
	if _, hasAccessToken := created["access_token"]; hasAccessToken {
		t.Error("a new link sent a credential")
	}

	if _, err := plaid.CreateLinkToken(context.Background(), "person-example", "access-example"); err != nil {
		t.Fatal(err)
	}
	repaired := testServer.requestBodies[1]
	if repaired["access_token"] != "access-example" {
		t.Errorf("update mode did not send the credential: %v", repaired)
	}
	if _, hasProducts := repaired["products"]; hasProducts {
		t.Error("update mode named products, which Plaid refuses")
	}
}

func TestPlaidExchangeAndRemove(t *testing.T) {
	testServer := newPlaidTestServer(t, func(path string, body map[string]any) (int, string) {
		switch path {
		case "/item/public_token/exchange":
			return 200, `{"access_token":"access-sandbox-example","item_id":"item-example"}`
		case "/item/remove":
			return 200, `{"request_id":"request-example"}`
		case "/institutions/get_by_id":
			return 200, `{"institution":{"institution_id":"institution-example","name":"Example Savings Bank"}}`
		}
		return 404, `{}`
	})
	plaid := newTestPlaid(t, testServer)

	credential, providerReference, err := plaid.ExchangePublicToken(context.Background(), "public-sandbox-example")
	if err != nil || credential != "access-sandbox-example" || providerReference != "item-example" {
		t.Fatalf("exchange %q %q %v", credential, providerReference, err)
	}
	if err := plaid.Remove(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	if testServer.requestBodies[1]["access_token"] != credential {
		t.Errorf("remove sent %v", testServer.requestBodies[1])
	}
	institutionName, err := plaid.InstitutionName(context.Background(), "institution-example")
	if err != nil || institutionName != "Example Savings Bank" {
		t.Errorf("institution name %q %v", institutionName, err)
	}
}

func TestNewPlaidRefusesBadSettings(t *testing.T) {
	if _, err := NewPlaid("staging", "client-example", "secret-example", nil, nil); err == nil {
		t.Error("an unknown environment was accepted")
	}
	if _, err := NewPlaid(PlaidEnvironmentProduction, "client-example", "", nil, nil); err == nil {
		t.Error("a missing secret was accepted")
	}
	plaid, err := NewPlaid(PlaidEnvironmentProduction, "client-example", "secret-example", nil, nil)
	if err != nil || plaid.baseUrl != plaidProductionBaseUrl || plaid.countryCodes[0] != "US" {
		t.Errorf("production client %+v %v", plaid, err)
	}
}

func TestPlaidAccountKind(t *testing.T) {
	for accountType, want := range map[string]string{
		"depository": AccountKindDepository,
		"credit":     AccountKindCredit,
		"loan":       AccountKindLoan,
		"investment": AccountKindInvestment,
		"brokerage":  AccountKindInvestment,
		"other":      AccountKindOther,
		"":           AccountKindOther,
	} {
		if got := plaidAccountKind(accountType); got != want {
			t.Errorf("%q maps to %q, want %q", accountType, got, want)
		}
	}
}
