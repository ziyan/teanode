package finance_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/finance"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// operationRule is a finance operation's tool name by the rule: snake case,
// a leading Finance dropped.
func operationRule(operation string) string {
	var words []string
	runes := []rune(strings.TrimPrefix(operation, "Finance"))
	start := 0
	for index := 1; index < len(runes); index++ {
		if unicode.IsUpper(runes[index]) && (unicode.IsLower(runes[index-1]) || (index+1 < len(runes) && unicode.IsLower(runes[index+1]))) {
			words = append(words, strings.ToLower(string(runes[start:index])))
			start = index
		}
	}
	return strings.Join(append(words, strings.ToLower(string(runes[start:]))), "_")
}

// The tool operations that stand for more than one call, act on a source
// through the operations every source has, answer with where to go, or
// take fewer arguments than the operation they call (import_statement takes
// a message, never an uploaded file, whose id the model is not shown).
var spanningOperations = map[string][]string{
	"import_statement":  {"ImportStatement"},
	"link_plaid":        {"CreateFinanceLinkToken", "CompleteFinanceLink"},
	"repair":            {"CreateFinanceLinkToken", "CompleteFinanceRepair"},
	"link_simplefin":    {},
	"import_credential": {},
	"sync":              {},
	"disable_source":    {},
	"enable_source":     {},
	"delete_source":     {},
}

// The deliberate gaps: a setup token and a provider credential are never
// taken in conversation, so the tool operations named for these only say
// where to give them.
var gapOperations = map[string]bool{"LinkSimpleFIN": true, "ImportFinanceCredential": true}

func financeTool(test *testing.T) *tools.Tool {
	test.Helper()
	tool := tools.Build().Get("finance")
	if tool == nil {
		test.Fatal("there is no finance tool")
	}
	return tool
}

func toolOperations(test *testing.T, tool *tools.Tool) map[string]bool {
	test.Helper()
	properties := tool.Parameters["properties"].(map[string]any)
	names := map[string]bool{}
	for _, name := range properties["operation"].(map[string]any)["enum"].([]string) {
		names[name] = true
	}
	return names
}

func apiOperations() []string {
	var operations []string
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.FinanceQuery](), reflect.TypeFor[apigraph.FinanceMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations = append(operations, interfaceType.Method(index).Name)
		}
	}
	return operations
}

// Every operation of the finance area is called by the tool operation its
// name gives by the rule, or is one of those that span calls, or is the
// one gap; each tool operation calls the operation its name says; and the
// tool has no operation the API does not back.
func TestFinanceParityWithTheTool(test *testing.T) {
	test.Parallel()
	names := toolOperations(test, financeTool(test))
	covered := map[string]bool{}
	for name, operations := range spanningOperations {
		if !names[name] {
			test.Errorf("the finance tool has no %s", name)
		}
		for _, operation := range operations {
			covered[operation] = true
		}
	}
	operations := apiOperations()
	if len(operations) == 0 {
		test.Fatal("the finance area has no operations to check")
	}
	isOperation := map[string]bool{}
	for _, operation := range operations {
		isOperation[operation] = true
	}
	for name := range names {
		called, isKnown := finance.GraphqlOperationOf(name)
		if !isKnown {
			test.Errorf("the schema offers %s, which the tool does not run", name)
			continue
		}
		if gapOperations[called] {
			test.Errorf("%s calls %s; a setup token or a credential must never be taken in conversation", name, called)
		}
		if _, isSpanning := spanningOperations[name]; isSpanning {
			if called != "" {
				test.Errorf("%s stands for more than one call but calls %s", name, called)
			}
			continue
		}
		if !isOperation[called] {
			test.Errorf("the tool's %s calls %q, which is not an operation of the finance area", name, called)
			continue
		}
		if operationRule(called) != name {
			test.Errorf("the tool's %s calls %s, whose name by the rule is %s", name, called, operationRule(called))
		}
		covered[called] = true
	}
	for _, operation := range operations {
		if !gapOperations[operation] && !covered[operation] {
			test.Errorf("%s has no tool operation %s", operation, operationRule(operation))
		}
	}
}

// Each tool operation takes the arguments of the operation it calls, in
// snake case, and nothing else but month, its shorthand for a range. The
// one deliberate exception: whether an asset may be estimated from the
// web, and what the estimate searches for, are the person's to set in the
// dashboard or with teanode finance, never the agent's.
func TestFinanceToolArgumentsMatchTheAPI(test *testing.T) {
	test.Parallel()
	personOnly := map[string]bool{}
	for _, argument := range finance.PersonOnlyAssetArguments {
		personOnly[argument] = true
	}
	arguments := apiArguments()
	for name := range toolOperations(test, financeTool(test)) {
		called, _ := finance.GraphqlOperationOf(name)
		if called == "" {
			continue
		}
		wanted, isKnown := arguments[called]
		if !isKnown {
			test.Errorf("no arguments known for %s", called)
			continue
		}
		accepted := map[string]bool{}
		for _, argument := range finance.AcceptedArgumentsOf(name) {
			accepted[argument] = true
			if argument != "month" && !wanted[argument] {
				test.Errorf("the tool's %s takes %s, which %s does not", name, argument, called)
			}
		}
		for argument := range wanted {
			isException := personOnly[argument] && (called == "CreateAsset" || called == "UpdateAsset")
			if isException && accepted[argument] {
				test.Errorf("the tool's %s takes %s, which only the person may set", name, argument)
			}
			if !isException && !accepted[argument] {
				test.Errorf("the tool's %s does not take %s, which %s does", name, argument, called)
			}
		}
	}
	properties := financeTool(test).Parameters["properties"].(map[string]any)
	for argument := range personOnly {
		if _, isOffered := properties[argument]; isOffered {
			test.Errorf("the schema offers %s", argument)
		}
	}
}

// apiArguments is each finance operation's arguments in snake case.
func apiArguments() map[string]map[string]bool {
	arguments := map[string]map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.FinanceQuery](), reflect.TypeFor[apigraph.FinanceMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			method := interfaceType.Method(index)
			names := map[string]bool{}
			if method.Type.NumIn() > 1 {
				structType := method.Type.In(1)
				for field := 0; field < structType.NumField(); field++ {
					key := strings.Split(structType.Field(field).Tag.Get("json"), ",")[0]
					names[snakeCase(key)] = true
				}
			}
			arguments[method.Name] = names
		}
	}
	return arguments
}

// snakeCase is an argument's name as the tool spells it: financeAccountId
// is finance_account_id.
func snakeCase(camel string) string {
	var builder strings.Builder
	for _, letter := range camel {
		if unicode.IsUpper(letter) {
			builder.WriteRune('_')
			letter = unicode.ToLower(letter)
		}
		builder.WriteRune(letter)
	}
	return builder.String()
}

// Reads are read, deletes destructive, and the rest write. The operations
// that answer with an address or a place to paste a token change nothing.
func TestFinanceRiskPerOperation(test *testing.T) {
	test.Parallel()
	tool := financeTool(test)
	reads := map[string]bool{
		"providers": true, "sources": true, "accounts": true, "transactions": true, "trades": true, "spending_summary": true,
		"exchange_rate": true, "convert_currency": true, "net_worth": true, "assets": true, "asset_history": true,
		"spending_categories": true, "spending_rules": true, "budgets": true, "budget_status": true, "saving_summary": true,
		"spending_by_day": true, "cash_flow": true, "savings_targets": true,
		"link_plaid": true, "repair": true, "link_simplefin": true, "import_credential": true, "reporting_currency": true,
		"statement_import": true,
	}
	for name := range toolOperations(test, tool) {
		wanted := tools.RiskWrite
		switch {
		case strings.HasPrefix(name, "delete_"):
			wanted = tools.RiskDestructive
		case reads[name]:
			wanted = tools.RiskRead
		}
		if risk := tool.RiskFor(json.RawMessage(`{"operation":"` + name + `"}`)); risk != wanted {
			test.Errorf("%s is %s, not %s", name, risk, wanted)
		}
	}
}

// fakeOperations answers finance documents with canned data and records
// what was sent.
type fakeOperations struct {
	answers   map[string]string
	documents []string
	variables []map[string]any
}

func (self *fakeOperations) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
	for operation, answer := range self.answers {
		if strings.Contains(document, " "+operation+"(") || strings.Contains(document, "{ "+operation+" ") {
			if result == nil {
				return nil
			}
			return json.Unmarshal([]byte(`{"`+operation+`": `+answer+`}`), result)
		}
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{}`), result)
}

func (self *fakeOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
}

type fakeRun struct {
	tools.Run
	operations *fakeOperations
}

func (self *fakeRun) Operations() tools.Operations { return self.operations }
func (self *fakeRun) Owner() *models.User          { return &models.User{ID: "owner-one"} }
func (self *fakeRun) Configuration() *config.Configuration {
	configuration := config.Default()
	configuration.Server.Name = "mail.example.com"
	return configuration
}

func call(test *testing.T, operations *fakeOperations, arguments string) (*tools.Result, error) {
	test.Helper()
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	return financeTool(test).Run(ctx, &tools.Call{ID: "call-one", Arguments: json.RawMessage(arguments)})
}

// A setup token is never sent anywhere from a conversation: the tool says
// where to paste it and calls nothing.
func TestFinanceToolRefusesASetupToken(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{}
	result, err := call(test, operations, `{"operation":"link_simplefin","setup_token":"aW52ZW50ZWQ="}`)
	if err != nil {
		test.Fatal(err)
	}
	if len(operations.documents) != 0 {
		test.Errorf("link_simplefin sent %d documents", len(operations.documents))
	}
	if !strings.Contains(result.Content, "teanode finance link-simplefin") || !strings.Contains(result.Content, "Finance tab") {
		test.Errorf("it does not say where to paste the token: %s", result.Content)
	}
	if strings.Contains(result.Content, "aW52ZW50ZWQ=") {
		test.Errorf("the token came back: %s", result.Content)
	}
}

// A provider credential is never sent anywhere from a conversation either:
// import_credential says where to bring the connection in and calls
// nothing, whatever came with it.
func TestFinanceToolRefusesACredential(test *testing.T) {
	test.Parallel()
	for _, arguments := range []string{
		`{"operation":"import_credential"}`,
		`{"operation":"import_credential","provider_kind":"plaid","credential":"access-sandbox-invented-0010"}`,
		`{"operation":"import_credential","credential":"https://person:invented-password@bridge.example.net/simplefin"}`,
	} {
		operations := &fakeOperations{}
		result, err := call(test, operations, arguments)
		if err != nil {
			test.Fatalf("%s: %s", arguments, err)
		}
		if len(operations.documents) != 0 {
			test.Errorf("import_credential sent %d documents", len(operations.documents))
		}
		if !strings.Contains(result.Content, "teanode finance import-credential") || !strings.Contains(result.Content, "Finance tab") {
			test.Errorf("it does not say where to bring the connection in: %s", result.Content)
		}
		for _, secret := range []string{"access-sandbox-invented-0010", "invented-password", "bridge.example.net"} {
			if strings.Contains(result.Content, secret) {
				test.Errorf("the credential came back: %s", result.Content)
			}
		}
	}
}

// Linking through Plaid and repairing answer with the dashboard's page.
func TestFinanceToolGivesTheLinkingPage(test *testing.T) {
	test.Parallel()
	result, err := call(test, &fakeOperations{}, `{"operation":"link_plaid"}`)
	if err != nil || !strings.Contains(result.Content, "https://mail.example.com/finance/link") {
		test.Fatalf("%v %v", result, err)
	}
	result, err = call(test, &fakeOperations{}, `{"operation":"repair","source_id":"source-one"}`)
	if err != nil || !strings.Contains(result.Content, "/finance/link?source=source-one") {
		test.Fatalf("%v %v", result, err)
	}
}

// What the agent records is a reading or an estimate, never the person's
// own manual value; a reading is the default.
func TestFinanceToolRecordsReadingsAndEstimatesOnly(test *testing.T) {
	test.Parallel()
	if _, err := call(test, &fakeOperations{}, `{"operation":"record_valuation","asset_id":"asset-one","value":"100","valuation_source":"manual"}`); err == nil {
		test.Error("a manual valuation was taken from the tool")
	}
	operations := &fakeOperations{answers: map[string]string{"RecordValuation": `{"id":"valuation-one","value":"100.0000"}`}}
	result, err := call(test, operations, `{"operation":"record_valuation","asset_id":"asset-one","value":"100"}`)
	if err != nil {
		test.Fatal(err)
	}
	if len(operations.variables) != 1 || operations.variables[0]["valuationSource"] != "agent_reading" || operations.variables[0]["assetId"] != "asset-one" {
		test.Errorf("sent %v", operations.variables)
	}
	if !result.Untrusted {
		test.Error("a valuation's note and evidence are not marked untrusted")
	}
}

// Transactions carry what merchants wrote, and come back marked untrusted,
// with the arguments passed on in the API's spelling.
func TestFinanceToolTransactionsAreUntrusted(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceTransactions": `{"financeTransactions":[{"id":"transaction-one","description":"CORNER GROCER 0412","amount":"-42.1700","currencyCode":"USD"}],"nextCursor":""}`,
	}}
	result, err := call(test, operations, `{"operation":"transactions","finance_account_id":"account-one","limit":20,"is_uncategorized":true}`)
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "CORNER GROCER") {
		test.Errorf("%+v", result)
	}
	sent := operations.variables[0]
	if sent["financeAccountId"] != "account-one" || sent["limit"] != 20 || sent["isUncategorized"] != true {
		test.Errorf("sent %v", sent)
	}
}

// Trades carry what the institution wrote, and come back marked
// untrusted, with the arguments passed on in the API's spelling and a month
// spread into its first and last day.
func TestFinanceToolTradesAreUntrusted(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceTrades": `{"financeTrades":[{"id":"trade-one","tradeKind":"buy","tradedQuantity":"3","unitPrice":"101.25","tradeAmount":"-303.75","currencyCode":"USD","description":"BOUGHT INVENTED FUND"}],"nextCursor":""}`,
	}}
	result, err := call(test, operations, `{"operation":"trades","finance_account_id":"account-one","finance_security_id":"security-one","limit":20,"month":"2026-02"}`)
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "BOUGHT INVENTED FUND") {
		test.Errorf("%+v", result)
	}
	sent := operations.variables[0]
	if sent["financeAccountId"] != "account-one" || sent["financeSecurityId"] != "security-one" || sent["limit"] != 20 ||
		sent["from"] != "2026-02-01" || sent["to"] != "2026-02-28" || sent["month"] != nil {
		test.Errorf("sent %v", sent)
	}
	if !strings.Contains(operations.documents[0], "FinanceTrades(") {
		test.Errorf("sent %s", operations.documents[0])
	}
	if _, err := call(test, operations, `{"operation":"trades","account_id":"account-one"}`); err == nil || !strings.Contains(err.Error(), "finance_account_id instead of account_id") {
		test.Errorf("trades took account_id for finance_account_id: %v", err)
	}
}

// A source operation acts only on one of the person's finance sources.
func TestFinanceToolActsOnFinanceSourcesOnly(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"FinanceSources": `[{"id":"source-one","name":"Invented Bank"}]`}}
	if _, err := call(test, operations, `{"operation":"delete_source","source_id":"source-two"}`); err == nil {
		test.Error("a source that is not a finance source of theirs was deleted")
	}
	for _, document := range operations.documents {
		if strings.Contains(document, "DeleteAgentKnowledgeSource") {
			test.Error("the delete was sent")
		}
	}
	result, err := call(test, operations, `{"operation":"delete_source","source_id":"source-one"}`)
	if err != nil || !strings.Contains(result.Content, "Invented Bank") {
		test.Fatalf("%v %v", result, err)
	}
	if !strings.Contains(operations.documents[len(operations.documents)-1], "DeleteAgentKnowledgeSource") {
		test.Error("the delete was not sent")
	}
}

// The call that once came back as an all-time total in the wrong
// currency: a month and a currency under names spending_summary does not
// read. It is refused, naming what it does not take, what it does, and
// the argument meant; the retry with month and currency_code sends the
// month's days and the currency.
func TestFinanceToolRefusesArgumentsItDoesNotRead(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"FinanceSpendingSummary": `{"groupBy":"spendingCategory"}`}}
	_, err := call(test, operations, `{"operation":"spending_summary","month":"2026-08","to_currency_code":"EUR"}`)
	if err == nil {
		test.Fatal("to_currency_code was taken by spending_summary")
	}
	for _, said := range []string{"does not take to_currency_code", "currency_code instead of to_currency_code", "group_by", "month"} {
		if !strings.Contains(err.Error(), said) {
			test.Errorf("the refusal does not say %q: %s", said, err)
		}
	}
	if len(operations.documents) != 0 {
		test.Errorf("a refused call sent %d documents", len(operations.documents))
	}
	if _, err := call(test, operations, `{"operation":"spending_summary","month":"2026-08","currency_code":"EUR"}`); err != nil {
		test.Fatal(err)
	}
	sent := operations.variables[0]
	if sent["from"] != "2026-08-01" || sent["to"] != "2026-08-31" || sent["currencyCode"] != "EUR" || sent["month"] != nil {
		test.Errorf("sent %v", sent)
	}
	if _, err := call(test, operations, `{"operation":"spending_summary","month":"2026-08","from":"2026-08-03"}`); err == nil {
		test.Error("a month and a range were both taken")
	}
	if _, err := call(test, operations, `{"operation":"spending_summary","month":"2026-08","to_currency_code":""}`); err != nil {
		test.Errorf("an empty to_currency_code was refused: %v", err)
	}
	if _, err := call(test, operations, `{"operation":"create_asset","asset_name":"the car","asset_kind":"vehicle","currency_code":"USD","is_estimate_allowed":true}`); err == nil ||
		!strings.Contains(err.Error(), "the person's to set") {
		test.Errorf("create_asset took is_estimate_allowed from the agent: %v", err)
	}
}

// A model that fills in every argument on every call: the ones it has
// nothing for empty, an argument with a fixed list of values as its first
// value, and a guess. assets is answered rather than refused; a misnamed
// argument with something in it is still refused. An argument the
// operation reads keeps an empty value, which may mean something.
func TestFinanceToolIgnoresArgumentsItDoesNotRead(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"Assets":                `[]`,
		"CategorizeTransaction": `{"id":"transaction-one"}`,
	}}
	if _, err := call(test, operations, `{"operation":"assets","after":"","asset_id":"","asset_ids":[],"asset_kind":"vehicle","estimate_low":null,
		"group_by":"spendingCategory","target_measure":"cash_flow","valuation_source":"agent_estimate",
		"is_transfer":false,"is_uncategorized":false,"limit":0,"month":""}`); err != nil {
		test.Fatalf("assets refused arguments it does not read: %v", err)
	}
	if _, err := call(test, operations, `{"operation":"assets","asset_kind":"vehicle","to_currency_code":"EUR"}`); err != nil {
		test.Errorf("assets refused a to_currency_code, which it does not read under any name: %v", err)
	}
	if _, err := call(test, operations, `{"operation":"categorize_transaction","finance_transaction_id":"transaction-one","spending_category_id":"",
		"asset_id":"","is_hidden":false}`); err != nil {
		test.Fatalf("categorize_transaction refused arguments sent empty: %v", err)
	}
	if sent := operations.variables[len(operations.variables)-1]; sent["spendingCategoryId"] != "" {
		test.Errorf("an empty spending category, which takes one away, was not sent: %v", sent)
	}
}

// A month is a whole month for transactions too, and both ends of the
// range for cash_flow.
func TestFinanceToolMonthIsShorthandForARange(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceTransactions": `{"financeTransactions":[],"nextCursor":""}`,
		"CashFlow":            `{"fromMonth":"2026-02","toMonth":"2026-02"}`,
	}}
	if _, err := call(test, operations, `{"operation":"transactions","month":"2026-02"}`); err != nil {
		test.Fatal(err)
	}
	if sent := operations.variables[0]; sent["from"] != "2026-02-01" || sent["to"] != "2026-02-28" {
		test.Errorf("sent %v", sent)
	}
	if _, err := call(test, operations, `{"operation":"cash_flow","month":"2026-02"}`); err != nil {
		test.Fatal(err)
	}
	if sent := operations.variables[1]; sent["fromMonth"] != "2026-02" || sent["toMonth"] != "2026-02" {
		test.Errorf("sent %v", sent)
	}
	if _, err := call(test, operations, `{"operation":"transactions","month":"February"}`); err == nil {
		test.Error("a month not written 2026-02 was taken")
	}
}

// A spending category may be named, in any case, as on the command line;
// the call carries its id. A name that is none of the person's goes on as
// given, for the API to refuse.
func TestFinanceToolTakesASpendingCategoryByName(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"SpendingCategories": `[{"id":"category-groceries","spendingCategoryName":"groceries"}]`,
		"SetBudget":          `{"id":"budget-one"}`,
	}}
	if _, err := call(test, operations, `{"operation":"set_budget","spending_category_id":"Groceries","monthly_amount":"500","currency_code":"USD"}`); err != nil {
		test.Fatal(err)
	}
	if sent := operations.variables[len(operations.variables)-1]; sent["spendingCategoryId"] != "category-groceries" {
		test.Errorf("sent %v", sent)
	}
	if _, err := call(test, operations, `{"operation":"set_budget","spending_category_id":"an invented name","monthly_amount":"5","currency_code":"USD"}`); err != nil {
		test.Fatal(err)
	}
	if sent := operations.variables[len(operations.variables)-1]; sent["spendingCategoryId"] != "an invented name" {
		test.Errorf("an unknown name was changed: %v", sent)
	}
}

// The agent cannot allow itself to estimate an asset from the web.
func TestFinanceToolLeavesEstimatesToThePerson(test *testing.T) {
	test.Parallel()
	for _, arguments := range []string{
		`{"operation":"update_asset","asset_id":"asset-one","is_estimate_allowed":true}`,
		`{"operation":"create_asset","asset_name":"the house","asset_kind":"property","currency_code":"USD","estimate_description":"an invented street"}`,
	} {
		operations := &fakeOperations{}
		_, err := call(test, operations, arguments)
		if err == nil || !strings.Contains(err.Error(), "the person's to set") {
			test.Errorf("%s: %v", arguments, err)
		}
		if len(operations.documents) != 0 {
			test.Errorf("%s was sent", arguments)
		}
	}
}

// An asset can be made with its first value in one call; the value is
// the agent's reading, never the person's own.
func TestFinanceToolCreatesAnAssetWithAValue(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"CreateAsset": `{"id":"asset-one","assetName":"brokerage"}`}}
	result, err := call(test, operations, `{"operation":"create_asset","asset_name":"brokerage","asset_kind":"investment","currency_code":"USD","value":"52380","valued_on":"2026-09-01"}`)
	if err != nil {
		test.Fatal(err)
	}
	sent := operations.variables[0]
	if sent["value"] != "52380" || sent["valuedOn"] != "2026-09-01" || sent["valuationSource"] != "agent_reading" {
		test.Errorf("sent %v", sent)
	}
	if !result.Untrusted {
		test.Error("an asset's name came back trusted")
	}
	if _, err := call(test, operations, `{"operation":"create_asset","asset_name":"the car","asset_kind":"vehicle","currency_code":"USD","value":"18000","valuation_source":"manual"}`); err == nil {
		test.Error("a manual value was recorded from the tool")
	}
}

// Syncing a switched-off finance source is refused rather than switching
// it on behind the person's back.
func TestFinanceToolDoesNotSyncASwitchedOffSource(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"FinanceSources": `[{"id":"source-one","name":"Invented Bank","isEnabled":false}]`}}
	_, err := call(test, operations, `{"operation":"sync","source_id":"source-one"}`)
	if err == nil || !strings.Contains(err.Error(), "enable_source") {
		test.Errorf("a switched-off source was synced: %v", err)
	}
	for _, document := range operations.documents {
		if strings.Contains(document, "SyncAgentKnowledgeSource") {
			test.Error("the sync was sent")
		}
	}
	operations.answers["FinanceSources"] = `[{"id":"source-one","name":"Invented Bank","isEnabled":true}]`
	result, err := call(test, operations, `{"operation":"sync","source_id":"source-one"}`)
	if err != nil || !result.Untrusted {
		test.Errorf("%+v %v", result, err)
	}
}

// The reporting currency is an operation of its own.
func TestFinanceToolSaysTheReportingCurrency(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{"ReportingCurrency": `{"reportingCurrencyCode":"EUR","isChosen":false}`}}
	result, err := call(test, operations, `{"operation":"reporting_currency"}`)
	if err != nil || !strings.Contains(result.Content, `"reportingCurrencyCode":"EUR"`) {
		test.Errorf("%+v %v", result, err)
	}
}

// A confirmation card names what is approved: the spending category by
// its name, and the transaction by what it was, its amount and its day.
func TestFinanceToolPreviewsNameWhatIsApproved(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"SpendingCategories":  `[{"id":"category-dining","spendingCategoryName":"Dining"}]`,
		"FinanceTransactions": `{"financeTransactions":[{"id":"transaction-one","postedOn":"2026-09-12","amount":"-42.17","currencyCode":"USD","description":"CORNER GROCER 0412","merchantName":"Corner Grocer"}],"nextCursor":""}`,
	}}
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	tool := financeTool(test)
	for arguments, wanted := range map[string][]string{
		`{"operation":"set_budget","spending_category_id":"category-dining","monthly_amount":"400","currency_code":"USD"}`: {"400 USD", `"Dining"`},
		`{"operation":"categorize_transaction","finance_transaction_id":"transaction-one","spending_category_id":"category-dining"}`: {
			`"Corner Grocer"`, "-42.17 USD", "2026-09-12", `"Dining"`,
		},
		`{"operation":"delete_spending_category","spending_category_id":"category-dining"}`: {`"Dining"`},
	} {
		line := tool.PreviewLine(ctx, json.RawMessage(arguments))
		for _, said := range wanted {
			if !strings.Contains(line, said) {
				test.Errorf("%s: the card %q does not say %s", arguments, line, said)
			}
		}
	}
}

// A budget on an income spending category is the income expected, and
// the card says so rather than calling it a limit; saving_summary passes
// the month and the currency through.
func TestFinanceToolIncomeBudgetAndSavingSummary(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"SpendingCategories": `[{"id":"category-salary","spendingCategoryName":"Salary","isIncome":true},{"id":"category-dining","spendingCategoryName":"Dining"}]`,
		"SavingSummary":      `{"month":"2026-08","expectedSavingAmount":"1000.0000","savingPace":"on_track"}`,
	}}
	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	tool := financeTool(test)
	for arguments, wanted := range map[string]string{
		`{"operation":"set_budget","spending_category_id":"category-salary","monthly_amount":"4000","currency_code":"USD"}`: `Expect a monthly income of 4000 USD in "Salary"`,
		`{"operation":"set_budget","spending_category_id":"category-salary","monthly_amount":"0"}`:                          `Stop expecting income in "Salary"`,
		`{"operation":"set_budget","spending_category_id":"category-dining","monthly_amount":"400","currency_code":"USD"}`:  `Set a monthly budget of 400 USD for "Dining"`,
	} {
		if line := tool.PreviewLine(ctx, json.RawMessage(arguments)); !strings.Contains(line, wanted) {
			test.Errorf("%s: the card %q does not say %s", arguments, line, wanted)
		}
	}
	result, err := call(test, operations, `{"operation":"saving_summary","month":"2026-08","currency_code":"EUR"}`)
	if err != nil || !strings.Contains(result.Content, `"expectedSavingAmount":"1000.0000"`) {
		test.Fatalf("%+v %v", result, err)
	}
	sent := operations.variables[len(operations.variables)-1]
	if sent["month"] != "2026-08" || sent["currencyCode"] != "EUR" {
		test.Errorf("sent %v", sent)
	}
}

// A savings target can measure whole finance accounts or net worth: the
// accounts reach the API as financeAccountIds, and the card names them.
func TestFinanceToolSavingsTargetOnWholeAccounts(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceAccounts":     `[{"id":"account-brokerage","accountName":"Invented Brokerage"}]`,
		"Assets":              `[{"id":"asset-boat","assetName":"the boat"}]`,
		"CreateSavingsTarget": `{"savingsTarget":{"id":"target-one"}}`,
	}}
	if _, err := call(test, operations, `{"operation":"create_savings_target","savings_target_name":"invested","target_amount":"5000","target_on":"2027-09-01","target_measure":"asset_value","finance_account_ids":["account-brokerage"]}`); err != nil {
		test.Fatal(err)
	}
	sent := operations.variables[len(operations.variables)-1]
	if accounts, isList := sent["financeAccountIds"].([]any); !isList || len(accounts) != 1 || accounts[0] != "account-brokerage" {
		test.Errorf("sent %v", sent)
	}

	ctx := tools.WithRun(context.Background(), &fakeRun{operations: operations})
	tool := financeTool(test)
	for arguments, wanted := range map[string][]string{
		`{"operation":"create_savings_target","savings_target_name":"invested","target_amount":"5000","target_on":"2027-09-01","target_measure":"asset_value","finance_account_ids":["account-brokerage"],"asset_ids":["asset-boat"]}`: {
			`"Invented Brokerage" (the whole account)`, `"the boat"`,
		},
		`{"operation":"create_savings_target","savings_target_name":"worth more","target_amount":"5000","target_on":"2027-09-01","target_measure":"net_worth"}`: {
			"measured by net worth",
		},
	} {
		line := tool.PreviewLine(ctx, json.RawMessage(arguments))
		for _, said := range wanted {
			if !strings.Contains(line, said) {
				test.Errorf("%s: the card %q does not say %s", arguments, line, said)
			}
		}
	}
}

// import_statement sends the message it was pointed at and nothing else,
// and what comes back, account names included, is untrusted.
func TestFinanceToolImportsAStatementFromAMessage(test *testing.T) {
	test.Parallel()
	answers := func(isGranted bool) map[string]string {
		granted := "false"
		if isGranted {
			granted = "true"
		}
		return map[string]string{
			"ListMailboxes":    `[{"mailbox":{"id":"mailbox-one","name":"Personal","agent":{"granted":` + granted + `}},"folders":[{"id":"folder-archive","mailboxId":"mailbox-one","name":"Archive","kind":"archive"}]}]`,
			"GetMailboxThread": `{"threadId":"thread-one","items":[{"folderId":"folder-archive","item":{"id":"item-one"}}]}`,
			"ImportStatement":  `{"addedTransactionCount":3,"financeAccountNames":["Invented Card ··a1b2"]}`,
		}
	}
	operations := &fakeOperations{answers: answers(true)}
	result, err := call(test, operations, `{"operation":"import_statement","mailbox_item_id":"item-one"}`)
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "Invented Card") {
		test.Errorf("%+v", result)
	}
	sent := operations.variables[len(operations.variables)-1]
	if !strings.Contains(operations.documents[len(operations.documents)-1], "ImportStatement(") || len(sent) != 1 || sent["mailboxItemId"] != "item-one" {
		test.Errorf("sent %v", operations.variables)
	}
	// A mailbox the person kept back from the agent stays kept back.
	kept := &fakeOperations{answers: answers(false)}
	if _, err := call(test, kept, `{"operation":"import_statement","mailbox_item_id":"item-one"}`); err == nil {
		test.Error("a message in a mailbox not granted was imported")
	}
	for _, document := range kept.documents {
		if strings.Contains(document, "ImportStatement(") {
			test.Error("the import was sent for a mailbox not granted")
		}
	}
	if _, err := call(test, &fakeOperations{}, `{"operation":"import_statement","agent_attachment_id":"attachment-one"}`); err == nil {
		test.Error("an uploaded file's id was taken")
	}
}

// The finance source of imported statements has nothing to sync.
func TestFinanceToolDoesNotSyncTheStatementSource(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"FinanceSources": `[{"id":"source-one","name":"Imported statements","providerKind":"statement","isEnabled":true}]`,
	}}
	if _, err := call(test, operations, `{"operation":"sync","source_id":"source-one"}`); err == nil || !strings.Contains(err.Error(), "nothing to sync") {
		test.Errorf("the statement source was synced: %v", err)
	}
	for _, document := range operations.documents {
		if strings.Contains(document, "SyncAgentKnowledgeSource") {
			test.Error("the sync was sent")
		}
	}
}
