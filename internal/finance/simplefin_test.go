package finance

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var simpleFinTestNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// simpleFinTestServer is a TLS server standing in for the bridge, since
// the client refuses a credential that is not https.
type simpleFinTestServer struct {
	server   *httptest.Server
	mutex    sync.Mutex
	requests []*http.Request
}

func newSimpleFINTestServer(t *testing.T, handler func(writer http.ResponseWriter, request *http.Request)) *simpleFinTestServer {
	t.Helper()
	testServer := &simpleFinTestServer{}
	testServer.server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		testServer.mutex.Lock()
		testServer.requests = append(testServer.requests, request.Clone(context.Background()))
		testServer.mutex.Unlock()
		handler(writer, request)
	}))
	t.Cleanup(testServer.server.Close)
	return testServer
}

// newTestSimpleFin is the client with the test server's own client in
// place of safefetch's, which refuses the loopback address the test server
// listens on.
func newTestSimpleFin(testServer *simpleFinTestServer) *SimpleFIN {
	return &SimpleFIN{
		http: testServer.server.Client(),
		now:  func() time.Time { return simpleFinTestNow },
	}
}

func (self *simpleFinTestServer) credential() string {
	address, _ := url.Parse(self.server.URL)
	address.User = url.UserPassword("user-example", "password-example")
	address.Path = "/simplefin"
	return address.String()
}

func TestSimpleFINClaim(t *testing.T) {
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/claim/token-example" {
			http.NotFound(writer, request)
			return
		}
		if !strings.HasPrefix(request.UserAgent(), "teanode/") {
			// What the bridge does to a request with a library's user agent.
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = fmt.Fprint(writer, "https://user-example:password-example@bridge.example.com/simplefin\n")
	})
	simpleFin := newTestSimpleFin(testServer)

	claimUrl := testServer.server.URL + "/claim/token-example"
	encoded := base64.StdEncoding.EncodeToString([]byte(claimUrl))
	// Pasted with its padding lost and a line break in the middle.
	pasted := strings.TrimRight(encoded, "=")
	pasted = pasted[:10] + "\n  " + pasted[10:]

	credential, err := simpleFin.Claim(context.Background(), pasted)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "https://user-example:password-example@bridge.example.com/simplefin" {
		t.Errorf("credential %q", credential)
	}
	claimed := testServer.requests[0]
	if claimed.ContentLength != 0 {
		t.Errorf("claim body length %d", claimed.ContentLength)
	}
}

func TestSimpleFINClaimRefusals(t *testing.T) {
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	})
	simpleFin := newTestSimpleFin(testServer)

	claimUrl := testServer.server.URL + "/claim/token-example"
	_, err := simpleFin.Claim(context.Background(), base64.StdEncoding.EncodeToString([]byte(claimUrl)))
	if err == nil || !strings.Contains(err.Error(), "claimed already") {
		t.Errorf("a refused claim answered %v", err)
	}
	if strings.Contains(fmt.Sprint(err), "token-example") {
		t.Errorf("error %q quotes the claim address", err)
	}

	for _, setupToken := range []string{
		"",
		"not base64 at all!",
		base64.StdEncoding.EncodeToString([]byte("http://bridge.example.com/claim/plain-http")),
		base64.StdEncoding.EncodeToString([]byte("file:///claim")),
		base64.StdEncoding.EncodeToString([]byte("https://user:password@bridge.example.com/claim")),
	} {
		if _, err := simpleFin.Claim(context.Background(), setupToken); err == nil {
			t.Errorf("setup token %q was accepted", setupToken)
		}
	}
}

// simpleFinAccountSetFor answers a window with one account, holding the
// transactions posted inside the window.
func simpleFinAccountSetFor(transactions []string, extra string) string {
	return `{` + extra + `"accounts":[{"id":"account-1","name":"Everyday Checking","currency":"USD",
		"balance":"1520.10","available-balance":"1500.00","balance-date":` + strconv.FormatInt(simpleFinTestNow.Unix(), 10) + `,
		"org":{"name":"Example Credit Union","domain":"bank.example.com"},"unknown_account_field":"kept",
		"transactions":[` + strings.Join(transactions, ",") + `]}]}`
}

func TestSimpleFINFirstSyncReadsTwoWindows(t *testing.T) {
	older := simpleFinTestNow.Add(-60 * 24 * time.Hour).Unix()
	newer := simpleFinTestNow.Add(-2 * 24 * time.Hour).Unix()
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		username, password, hasBasicAuth := request.BasicAuth()
		if !hasBasicAuth || username != "user-example" || password != "password-example" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if request.URL.Path != "/simplefin/accounts" {
			http.NotFound(writer, request)
			return
		}
		startSeconds, _ := strconv.ParseInt(request.URL.Query().Get("start-date"), 10, 64)
		var transactions []string
		if startSeconds <= older {
			transactions = append(transactions, fmt.Sprintf(`{"id":"transaction-old","posted":%d,"amount":"-42.17","description":"CORNER GROCER 0001","payee":"Corner Grocer","mcc":"5411","pending":false,"transacted_at":%d,"unknown_transaction_field":[1,2]}`, older, older-3600))
		} else {
			transactions = append(transactions,
				fmt.Sprintf(`{"id":"transaction-new","posted":%d,"amount":"2000.00","description":"PAYROLL EXAMPLE","pending":false}`, newer),
				`{"id":"transaction-pending","posted":0,"amount":"-3.50","description":"COFFEE EXAMPLE","mcc":5814,"pending":true}`)
		}
		_, _ = fmt.Fprint(writer, simpleFinAccountSetFor(transactions, `"errors":["exceeds recommended range of 45 days"],`))
	})
	simpleFin := newTestSimpleFin(testServer)

	result, err := simpleFin.Sync(context.Background(), testServer.credential(), "")
	if err != nil {
		t.Fatal(err)
	}

	if len(testServer.requests) != 2 {
		t.Fatalf("%d requests, want two 45-day windows", len(testServer.requests))
	}
	first := testServer.requests[0].URL.Query()
	second := testServer.requests[1].URL.Query()
	ninetyDaysAgo := simpleFinTestNow.Add(-90 * 24 * time.Hour).Unix()
	fortyFiveDaysAgo := simpleFinTestNow.Add(-45 * 24 * time.Hour).Unix()
	if first.Get("start-date") != strconv.FormatInt(ninetyDaysAgo, 10) || first.Get("end-date") != strconv.FormatInt(fortyFiveDaysAgo, 10) {
		t.Errorf("first window %v", first)
	}
	if second.Get("start-date") != strconv.FormatInt(fortyFiveDaysAgo, 10) || second.Get("end-date") != "" {
		t.Errorf("second window %v, want it open at the end", second)
	}
	for index, request := range testServer.requests {
		if request.URL.Query().Get("pending") != "1" {
			t.Errorf("request %d did not ask for pending transactions", index)
		}
		if !strings.HasPrefix(request.UserAgent(), "teanode/") {
			t.Errorf("request %d user agent %q", index, request.UserAgent())
		}
		if request.URL.User != nil {
			t.Errorf("request %d carried the credential in its address", index)
		}
	}

	if result.NextCursor != strconv.FormatInt(newer, 10) {
		t.Errorf("cursor %q, want the newest posted time %d", result.NextCursor, newer)
	}
	if result.InstitutionName != "Example Credit Union" {
		t.Errorf("institution %q", result.InstitutionName)
	}
	if len(result.ProviderWarnings) != 1 {
		t.Errorf("warnings %v, want the repeated one once", result.ProviderWarnings)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts %v, want the account once", result.Accounts)
	}
	account := result.Accounts[0]
	if account.CurrentBalance != "1520.10" || account.AvailableBalance != "1500.00" || account.AccountKind != AccountKindDepository || !account.BalanceAt.Equal(simpleFinTestNow) {
		t.Errorf("account %+v", account)
	}
	metadata := string(account.ProviderMetadata)
	if !strings.Contains(metadata, `"unknown_account_field":"kept"`) || strings.Contains(metadata, "transactions") {
		t.Errorf("account metadata %s: want unknown fields and no transactions", metadata)
	}

	byID := map[string]Transaction{}
	for _, transaction := range result.Added {
		byID[transaction.ProviderTransactionID] = transaction
	}
	if len(byID) != 3 {
		t.Fatalf("transactions %v", result.Added)
	}
	grocery := byID["transaction-old"]
	if grocery.Amount != "-42.17" || grocery.MerchantName != "Corner Grocer" || grocery.ProviderCategoryDetailed != "mcc:5411" || grocery.CurrencyCode != "USD" {
		t.Errorf("grocery %+v", grocery)
	}
	if grocery.PostedOn != time.Unix(older, 0).UTC().Format(time.DateOnly) || grocery.TransactedAt == nil || grocery.TransactedAt.Unix() != older-3600 {
		t.Errorf("grocery dates %+v", grocery)
	}
	if !strings.Contains(string(grocery.ProviderMetadata), `"unknown_transaction_field":[1,2]`) {
		t.Errorf("transaction metadata lost an unknown field: %s", grocery.ProviderMetadata)
	}
	payroll := byID["transaction-new"]
	if payroll.MerchantName != "PAYROLL EXAMPLE" {
		t.Errorf("merchant %q, want the description when there is no payee", payroll.MerchantName)
	}
	pending := byID["transaction-pending"]
	if !pending.IsPending || pending.ProviderCategoryDetailed != "mcc:5814" || pending.PostedOn != simpleFinTestNow.Format(time.DateOnly) {
		t.Errorf("pending %+v", pending)
	}
}

func TestSimpleFINIncrementalSync(t *testing.T) {
	cursorTime := simpleFinTestNow.Add(-3 * 24 * time.Hour)
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, simpleFinAccountSetFor([]string{
			`{"id":"transaction-pending","posted":0,"amount":"-3.50","description":"COFFEE EXAMPLE","pending":true,"transacted_at":` + strconv.FormatInt(simpleFinTestNow.Unix(), 10) + `}`,
		}, `"errlist":[{"code":"gen.auth","msg":"the institution needs attention"}],`))
	})
	simpleFin := newTestSimpleFin(testServer)

	cursor := strconv.FormatInt(cursorTime.Unix(), 10)
	result, err := simpleFin.Sync(context.Background(), testServer.credential(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(testServer.requests) != 1 {
		t.Fatalf("%d requests, want one window", len(testServer.requests))
	}
	wantStart := cursorTime.Add(-14 * 24 * time.Hour)
	query := testServer.requests[0].URL.Query()
	if query.Get("start-date") != strconv.FormatInt(wantStart.Unix(), 10) || query.Get("pending") != "1" || query.Get("end-date") != "" {
		t.Errorf("window %v", query)
	}
	if result.PendingReplacedFrom == nil || !result.PendingReplacedFrom.Equal(wantStart) {
		t.Errorf("pending replaced from %v, want %v", result.PendingReplacedFrom, wantStart)
	}
	if result.NextCursor != cursor {
		t.Errorf("cursor %q, want the old one kept when nothing posted", result.NextCursor)
	}
	if len(result.ProviderWarnings) != 1 || result.ProviderWarnings[0] != "gen.auth: the institution needs attention" {
		t.Errorf("warnings %v", result.ProviderWarnings)
	}
}

func TestSimpleFINSyncKeepsGoingPastABadTransaction(t *testing.T) {
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, simpleFinAccountSetFor([]string{
			`{"id":"transaction-bad","posted":1,"amount":"1,000.00","description":"BAD AMOUNT"}`,
			`{"id":"transaction-good","posted":1,"amount":"-1.00","description":"GOOD AMOUNT"}`,
		}, ""))
	})
	simpleFin := newTestSimpleFin(testServer)
	result, err := simpleFin.Sync(context.Background(), testServer.credential(), strconv.FormatInt(simpleFinTestNow.Unix(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 1 || result.Added[0].ProviderTransactionID != "transaction-good" {
		t.Errorf("added %v", result.Added)
	}
	if len(result.ProviderWarnings) != 1 || !strings.Contains(result.ProviderWarnings[0], "transaction-bad") {
		t.Errorf("warnings %v", result.ProviderWarnings)
	}
}

func TestSimpleFINRefusedCredential(t *testing.T) {
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	})
	simpleFin := newTestSimpleFin(testServer)
	_, err := simpleFin.Sync(context.Background(), testServer.credential(), "")
	if !errors.Is(err, ErrCredentialRefused) {
		t.Fatalf("error %v, want ErrCredentialRefused", err)
	}
	if strings.Contains(err.Error(), "password-example") {
		t.Errorf("error %q contains the password", err)
	}

	paymentServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusPaymentRequired)
	})
	_, err = newTestSimpleFin(paymentServer).Sync(context.Background(), paymentServer.credential(), "")
	if err == nil || errors.Is(err, ErrCredentialRefused) || !strings.Contains(err.Error(), "402") {
		t.Errorf("error %v, want a plain 402", err)
	}
}

// A posted transaction keeps the UTC day the bridge stood for; a pending
// one, which has only a moment, takes that moment's day in the person's
// time zone. A balance is not signed the way Plaid's is.
func TestSimpleFINDaysAndBalanceSign(t *testing.T) {
	// Half past three in the morning UTC on the 20th is still the 19th
	// eight hours west of Greenwich.
	earlyMorning := time.Date(2026, 9, 20, 3, 30, 0, 0, time.UTC).Unix()
	postedMidnight := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC).Unix()
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, simpleFinAccountSetFor([]string{
			fmt.Sprintf(`{"id":"transaction-pending","posted":0,"amount":"-3.50","description":"COFFEE EXAMPLE","pending":true,"transacted_at":%d}`, earlyMorning),
			fmt.Sprintf(`{"id":"transaction-posted","posted":%d,"amount":"-8.00","description":"BAKERY EXAMPLE","transacted_at":%d}`, postedMidnight, earlyMorning),
		}, ""))
	})
	simpleFin := newTestSimpleFin(testServer).InLocation(time.FixedZone("eight hours west", -8*60*60))
	result, err := simpleFin.Sync(context.Background(), testServer.credential(), strconv.FormatInt(simpleFinTestNow.Unix(), 10))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Transaction{}
	for _, transaction := range result.Added {
		byID[transaction.ProviderTransactionID] = transaction
	}
	if pending := byID["transaction-pending"]; pending.PostedOn != "2026-09-19" {
		t.Errorf("a pending transaction's day is its moment's in the person's zone: %q", pending.PostedOn)
	}
	if posted := byID["transaction-posted"]; posted.PostedOn != "2026-09-18" {
		t.Errorf("a posted transaction keeps the UTC day of its posted time: %q", posted.PostedOn)
	}
	if len(result.Accounts) != 1 || result.Accounts[0].IsOwedBalancePositive {
		t.Errorf("institutions behind SimpleFIN report what is owed as negative: %+v", result.Accounts)
	}
}

func TestSimpleFINRefusesUnusableCredentials(t *testing.T) {
	simpleFin := NewSimpleFIN()
	for _, credential := range []string{
		"",
		"https://bridge.example.com/simplefin",
		"https://user-example@bridge.example.com/simplefin",
		"http://user-example:password-example@bridge.example.com/simplefin",
	} {
		if _, err := simpleFin.Sync(context.Background(), credential, ""); err == nil {
			t.Errorf("credential %q was accepted", credential)
		}
	}
}

func TestSimpleFINAccountKind(t *testing.T) {
	for _, testCase := range []struct {
		accountName string
		hasHoldings bool
		want        string
	}{
		{"Everyday Checking", false, AccountKindDepository},
		{"High Yield Savings", false, AccountKindDepository},
		{"Rewards Credit Card", false, AccountKindCredit},
		{"Example Visa Signature", false, AccountKindCredit},
		{"Home Mortgage", false, AccountKindLoan},
		{"Roth IRA", false, AccountKindInvestment},
		{"Anything", true, AccountKindInvestment},
		{"Visalia Account", false, AccountKindOther},
		{"Account 0001", false, AccountKindOther},
	} {
		if got := simpleFinAccountKind(testCase.accountName, testCase.hasHoldings); got != testCase.want {
			t.Errorf("%q maps to %q, want %q", testCase.accountName, got, testCase.want)
		}
	}
}

// A credential claimed elsewhere is proved by one balances-only read, and
// the institution is named only when every account names the same one.
func TestSimpleFINDescribeCredential(t *testing.T) {
	answer := `{"errors":[],"accounts":[
		{"id":"account-1","name":"Everyday Checking","currency":"USD","balance":"10.00","org":{"name":"Example Credit Union"}},
		{"id":"account-2","name":"Rainy Day Savings","currency":"USD","balance":"20.00","org":{"name":"Example Credit Union"}}]}`
	testServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprint(writer, answer)
	})
	description, err := newTestSimpleFin(testServer).DescribeCredential(context.Background(), testServer.credential())
	if err != nil || description.InstitutionName != "Example Credit Union" || description.ProviderReference != "" {
		t.Fatalf("description %+v %v", description, err)
	}
	if len(testServer.requests) != 1 {
		t.Fatalf("%d requests, want one", len(testServer.requests))
	}
	request := testServer.requests[0]
	if request.URL.Path != "/simplefin/accounts" || request.URL.Query().Get("balances-only") != "1" || request.URL.Query().Get("start-date") != "" {
		t.Errorf("asked for %s", request.URL)
	}

	answer = `{"accounts":[
		{"id":"account-1","name":"Everyday Checking","currency":"USD","balance":"10.00","org":{"name":"Example Credit Union"}},
		{"id":"account-3","name":"Travel Card","currency":"USD","balance":"-5.00","org":{"name":"Example Card Issuer"}}]}`
	description, err = newTestSimpleFin(testServer).DescribeCredential(context.Background(), testServer.credential())
	if err != nil || description.InstitutionName != "" {
		t.Errorf("two institutions were named as one: %+v %v", description, err)
	}

	refusingServer := newSimpleFINTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	})
	_, err = newTestSimpleFin(refusingServer).DescribeCredential(context.Background(), refusingServer.credential())
	if !errors.Is(err, ErrCredentialRefused) || strings.Contains(err.Error(), "password-example") {
		t.Errorf("a revoked credential answered %v", err)
	}
}

// Only an https address with a user name and a password is a credential.
func TestCheckSimpleFINCredential(t *testing.T) {
	if err := CheckSimpleFINCredential("https://user-example:password-example@bridge.example.com/simplefin"); err != nil {
		t.Errorf("a well formed credential was refused: %v", err)
	}
	for _, credential := range []string{
		"",
		"access-sandbox-example",
		"https://bridge.example.com/simplefin",
		"https://user-example@bridge.example.com/simplefin",
		"http://user-example:password-example@bridge.example.com/simplefin",
		"ftp://user-example:password-example@bridge.example.com/simplefin",
	} {
		err := CheckSimpleFINCredential(credential)
		if err == nil {
			t.Errorf("credential %q was accepted", credential)
			continue
		}
		if strings.Contains(err.Error(), "password-example") {
			t.Errorf("the refusal of %q repeats the password: %s", credential, err)
		}
	}
}
