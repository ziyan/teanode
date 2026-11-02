package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Sharing a finance account is on the API and the command line, and nowhere in
// the agent's finance tool: what a finance account holds is the person's to
// share, and what others share is not the agent's to read. Every operation is
// a subcommand of teanode finance, and the three reads take --person.
func TestFinanceSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.FinanceShareQuery](), reflect.TypeFor[apigraph.FinanceShareMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	table := map[string]string{
		"ListFinanceAccountShares":      "shares",
		"ListFinanceShares":             "shares",
		"ListSharedFinance":             "shares",
		"ListSharedFinanceAccounts":     "shares",
		"ListSharedFinanceSpending":     "spending-summary",
		"ListSharedFinanceTransactions": "transactions",
		"ShareFinance":                  "share",
		"ShareFinanceAccount":           "share",
		"UnshareFinance":                "unshare",
		"UnshareFinanceAccount":         "unshare",
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	commands := map[string]*cli.Command{}
	for _, subcommand := range NewFinanceCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	for operation, subcommand := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if commands[subcommand] == nil {
			test.Errorf("teanode finance has no %s", subcommand)
		}
	}
	for _, name := range []string{"accounts", "transactions", "spending-summary"} {
		hasPerson := false
		for _, flag := range commands[name].Flags {
			hasPerson = hasPerson || flag.Names()[0] == "person"
		}
		if !hasPerson {
			test.Errorf("teanode finance %s has no --person", name)
		}
	}

	// The finance tool has nothing for any of it: not an operation named for
	// it, and no argument that names a person.
	for _, tool := range tools.Build().All() {
		if tool.Name != "finance" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		for name := range properties {
			if strings.Contains(name, "share") || name == "person" || name == "owner_user_id" {
				test.Errorf("the finance tool takes %s", name)
			}
		}
		encoded, _ := json.Marshal(tool.Parameters)
		for _, word := range []string{"share_finance", "unshare", "shared_finance", "list_finance_account_shares", "list_finance_shares"} {
			if strings.Contains(string(encoded), word) {
				test.Errorf("the finance tool offers %s", word)
			}
		}
	}
}

// financeSharingServer answers the documents the sharing commands send with a
// small world: ada is the caller, with a checking account and a card, ben and
// cora share a circle with her, ben shares two accounts with her, cora shares
// all her finance with her at the spending level, and ada shares all of hers
// with ben. A share identifier that begins with w is of all finance: asking to
// unshare it as an account's share is not found.
func financeSharingServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	answers := map[string]string{
		"FinanceAccounts": `[
			{"id":"a1","accountName":"Everyday Checking","accountMask":"0001","accountKind":"depository","currencyCode":"USD"},
			{"id":"a2","accountName":"Travel Card","accountKind":"credit","currencyCode":"USD"},
			{"id":"a3","accountName":"Everyday Checking","accountMask":"0002","accountKind":"depository","currencyCode":"USD"}]`,
		"ListCirclePeople":         `[{"userId":"u-ben","username":"ben","name":"Ben"},{"userId":"u-cora","username":"cora","name":"Cora"}]`,
		"ListCircles":              `[{"id":"c1","name":"Home","circleKind":"family","members":[],"pendingInvitations":[]}]`,
		"ShareFinanceAccount":      `{"id":"s1","financeAccountId":"a2","accountName":"Travel Card","granteeUserId":"u-ben","granteeUsername":"ben","financeShareLevel":"spending"}`,
		"UnshareFinanceAccount":    `true`,
		"ListFinanceAccountShares": `[{"id":"s1","financeAccountId":"a2","accountName":"Travel Card","accountMask":"","granteeUserId":"u-ben","granteeUsername":"ben","financeShareLevel":"balance"}, {"id":"s2","financeAccountId":"a1","accountName":"Everyday Checking","accountMask":"0001","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"spending"}]`,
		"ListSharedFinanceAccounts": `[
			{"financeAccountId":"b1","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"Ben","financeShareLevel":"transactions","accountName":"Joint Checking","accountMask":"7788","accountKind":"depository","currencyCode":"USD","currentBalance":"250.0000","creditUsage":null},
			{"financeAccountId":"b2","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"Ben","financeShareLevel":"balance","accountName":"Store Card","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}]`,
		"ShareFinance":                  `{"id":"w1","granteeUserId":"u-ben","granteeUsername":"ben","financeShareLevel":"spending"}`,
		"UnshareFinance":                `true`,
		"ListFinanceShares":             `[{"id":"w1","granteeUserId":"u-ben","granteeUsername":"ben","financeShareLevel":"spending"},{"id":"w2","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"transactions"}]`,
		"ListSharedFinance":             `[{"ownerUserId":"u-cora","ownerUsername":"cora","ownerName":"Cora","financeShareLevel":"spending"},{"ownerUserId":"u-dana","ownerUsername":"dana","ownerName":"Dana","financeShareLevel":"transactions"}]`,
		"ListSharedFinanceSpending":     `[{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"42.1700","moneyIn":"0.0000","financeTransactionCount":1}]`,
		"ListSharedFinanceTransactions": `{"nextCursor":"next-one","totalCount":5,"financeTransactions":[{"id":"t1","postedOn":"2026-09-10","amount":"-42.1700","currencyCode":"USD","merchantName":"Corner Grocer","spendingCategoryName":"Groceries"}]}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		data := map[string]json.RawMessage{}
		response.Header().Set("Content-Type", "application/json")
		for operation, answer := range answers {
			if strings.Contains(document.Query, " "+operation+"(") || strings.Contains(document.Query, "{ "+operation+" ") || strings.Contains(document.Query, "\n  "+operation+"(") {
				data[operation] = json.RawMessage(answer)
				lock.Lock()
				calls[operation] = document.Variables
				lock.Unlock()
				if shareId, _ := document.Variables["shareId"].(string); operation == "UnshareFinanceAccount" && strings.HasPrefix(shareId, "w") {
					_ = json.NewEncoder(response).Encode(map[string]any{"errors": []map[string]string{{"message": "api: not found"}}})
					return
				}
			}
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return calls[operation]
	}
}

func runFinanceCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewFinanceCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "finance"}, arguments...))
	})
	return output, err
}

// Sharing names the account by id or by name and the person by username or the
// circle by name, and sends the identifiers the server wants; what cannot be
// said is refused here, in words, before anything is sent.
func TestFinanceShareResolvesWhatAndWhoItIsFor(test *testing.T) {
	server, called := financeSharingServer(test)

	output, err := runFinanceCommand(test, server, "share", "Travel Card", "ben", "--level", "spending")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareFinanceAccount")
	if variables["financeAccountId"] != "a2" || variables["granteeUserId"] != "u-ben" || variables["granteeCircleId"] != nil || variables["financeShareLevel"] != "spending" {
		test.Errorf("an account is shared by its name with a person by their account: %v", variables)
	}
	if !strings.Contains(output, "Travel Card") || !strings.Contains(output, "ben") || !strings.Contains(output, "spending") {
		test.Errorf("it says what it did: %q", output)
	}

	if _, err = runFinanceCommand(test, server, "share", "a1", "Home", "--level", "balance"); err != nil {
		test.Fatal(err)
	}
	if variables = called("ShareFinanceAccount"); variables["financeAccountId"] != "a1" || variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil {
		test.Errorf("an account is shared by its id with a circle by name: %v", variables)
	}
	if _, err = runFinanceCommand(test, server, "share", "Everyday Checking ••0002", "ben", "--level", "balance"); err != nil || called("ShareFinanceAccount")["financeAccountId"] != "a3" {
		test.Errorf("a name with its mask: %v %v", err, called("ShareFinanceAccount"))
	}

	if _, err = runFinanceCommand(test, server, "share", "Everyday Checking", "ben", "--level", "balance"); err == nil || !strings.Contains(err.Error(), "a1, a3") {
		test.Errorf("two accounts with one name are listed by id: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "Nothing", "ben", "--level", "balance"); err == nil || !strings.Contains(err.Error(), "teanode finance accounts") {
		test.Errorf("an account that is not there: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "a1", "stranger", "--level", "balance"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("a stranger: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "a1", "ben"); err == nil || !strings.Contains(err.Error(), "--level") {
		test.Errorf("no level: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "a1", "ben", "--level", "everything"); err == nil {
		test.Errorf("a level there is not")
	}
}

func TestFinanceSharesAndUnshare(test *testing.T) {
	server, called := financeSharingServer(test)

	output, err := runFinanceCommand(test, server, "shares")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"shared by you", "Travel Card", "ben", "balance", "s1", `the circle "Home"`, "Everyday Checking ••0001", "spending", "s2",
		"all accounts", "w1", "w2", "shared with you", "cora", "Joint Checking ••7788", "transactions", "b1", "Store Card", "b2"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shares lacks %q:\n%s", wanted, output)
		}
	}
	if _, err = runFinanceCommand(test, server, "unshare", "s2"); err != nil {
		test.Fatal(err)
	}
	if called("UnshareFinanceAccount")["shareId"] != "s2" {
		test.Errorf("unshare sends the share: %v", called("UnshareFinanceAccount"))
	}
	if _, err = runFinanceCommand(test, server, "unshare"); err == nil {
		test.Errorf("unshare without a share")
	}

	// A share of all finance is ended by the same command: the server does not
	// know the identifier as an account's share, so the other kind is tried.
	if _, err = runFinanceCommand(test, server, "unshare", "w1"); err != nil {
		test.Fatal(err)
	}
	if called("UnshareFinance")["financeShareId"] != "w1" {
		test.Errorf("unshare falls back to the share of all finance: %v", called("UnshareFinance"))
	}

	output, err = runFinanceCommand(test, server, "shares", "--json")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"sharedByYou", "allFinanceSharedByYou", "sharedWithYou", "allFinanceSharedWithYou", "w2"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shares --json lacks %q:\n%s", wanted, output)
		}
	}
}

// Sharing all of one's finance names the level and who it is for, and refuses
// in words what cannot be said: a balance belongs to one account.
func TestFinanceShareAllResolvesWhoItIsFor(test *testing.T) {
	server, called := financeSharingServer(test)

	output, err := runFinanceCommand(test, server, "share", "all", "ben", "--level", "spending")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareFinance")
	if variables["granteeUserId"] != "u-ben" || variables["granteeCircleId"] != nil || variables["financeShareLevel"] != "spending" || variables["financeAccountId"] != nil {
		test.Errorf("all finance is shared with a person by their account: %v", variables)
	}
	if !strings.Contains(output, "all your finance") || !strings.Contains(output, "ben") || !strings.Contains(output, "spending") {
		test.Errorf("it says what it did: %q", output)
	}
	if called("ShareFinanceAccount") != nil {
		test.Errorf("no account was asked for: %v", called("ShareFinanceAccount"))
	}

	if _, err = runFinanceCommand(test, server, "share", "ALL", "Home", "--level", "transactions"); err != nil {
		test.Fatal(err)
	}
	if variables = called("ShareFinance"); variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil || variables["financeShareLevel"] != "transactions" {
		test.Errorf("all finance is shared with a circle by its name: %v", variables)
	}

	if _, err = runFinanceCommand(test, server, "share", "all", "ben", "--level", "balance"); err == nil || !strings.Contains(err.Error(), "spending or transactions") {
		test.Errorf("a balance belongs to one account: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "all", "ben"); err == nil || !strings.Contains(err.Error(), "--level") {
		test.Errorf("no level: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "share", "all", "stranger", "--level", "spending"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("a stranger: %v", err)
	}
}

// A person who shares all their finance is read as one: no account is named,
// and a read of one account is still of that account.
func TestFinanceReadsOfAllFinanceAPersonShares(test *testing.T) {
	server, called := financeSharingServer(test)

	output, err := runFinanceCommand(test, server, "spending-summary", "--person", "cora")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"month", "2026-09", "Groceries", "42.17 USD"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("spending-summary --person lacks %q:\n%s", wanted, output)
		}
	}
	if strings.Contains(output, "account") {
		test.Errorf("the combined view names no account:\n%s", output)
	}
	if variables := called("ListSharedFinanceSpending"); variables["ownerUserId"] != "u-cora" || variables["financeAccountId"] != nil {
		test.Errorf("the combined read asks for the person: %v", variables)
	}

	output, err = runFinanceCommand(test, server, "spending-summary", "--person", "cora", "--json")
	if err != nil || !strings.Contains(output, `"spendingRows"`) || !strings.Contains(output, `"ownerUsername": "cora"`) {
		test.Errorf("the combined read as JSON: %v\n%s", err, output)
	}

	// Her level is spending, so there are no transactions to list, and the
	// message says why.
	if _, err = runFinanceCommand(test, server, "transactions", "--person", "cora"); err == nil || !strings.Contains(err.Error(), "all their finance with you at spending") {
		test.Errorf("transactions at the spending level: %v", err)
	}

	output, err = runFinanceCommand(test, server, "accounts", "--person", "cora")
	if err != nil || !strings.Contains(output, "shares all their finance with you at spending") {
		test.Errorf("accounts --person says that all finance is shared: %v\n%s", err, output)
	}
}

// With a share of all finance at the transactions level the page is of all
// accounts, and naming an account asks for that account alone.
func TestFinanceTransactionsOfAllFinanceAPersonShares(test *testing.T) {
	server, called := financeSharingServer(test)

	output, err := runFinanceCommand(test, server, "transactions", "--person", "dana", "--limit", "20")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"2026-09-10", "-42.17 USD", "Corner Grocer", "Groceries"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("transactions --person lacks %q:\n%s", wanted, output)
		}
	}
	if variables := called("ListSharedFinanceTransactions"); variables["ownerUserId"] != "u-dana" || variables["financeAccountId"] != nil || variables["limit"] != float64(20) {
		test.Errorf("all of the person's finance is read together: %v", variables)
	}
	if _, err = runFinanceCommand(test, server, "transactions", "--person", "dana", "--finance-account", "b1"); err == nil || !strings.Contains(err.Error(), "no finance account") {
		test.Errorf("naming an account asks for what is shared of that account: %v", err)
	}
	if _, err = runFinanceCommand(test, server, "spending-summary", "--person", "dana"); err != nil {
		test.Fatal(err)
	}
	if variables := called("ListSharedFinanceSpending"); variables["ownerUserId"] != "u-dana" {
		test.Errorf("spending of all finance: %v", variables)
	}

	// A person who shares accounts only is read account by account.
	if _, err = runFinanceCommand(test, server, "transactions", "--person", "ben"); err != nil {
		test.Fatal(err)
	}
	if variables := called("ListSharedFinanceTransactions"); variables["financeAccountId"] != "b1" || variables["ownerUserId"] != nil {
		test.Errorf("a person who shares accounts only: %v", variables)
	}
}
