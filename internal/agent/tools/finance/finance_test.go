package finance_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/finance"
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
// through the operations every source has, or answer with where to go.
var spanningOperations = map[string][]string{
	"link_plaid":     {"CreateFinanceLinkToken", "CompleteFinanceLink"},
	"repair":         {"CreateFinanceLinkToken", "CompleteFinanceRepair"},
	"link_simplefin": {},
	"sync":           {},
	"disable_source": {},
	"enable_source":  {},
	"delete_source":  {},
}

// The one deliberate gap: a setup token is never taken in conversation.
const gapOperation = "LinkSimpleFIN"

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

// Every operation of the finance area has a tool operation named by the
// rule, or is one of the two that span calls, or is the one gap; and the
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
	byRule := map[string]bool{}
	operations := apiOperations()
	if len(operations) == 0 {
		test.Fatal("the finance area has no operations to check")
	}
	for _, operation := range operations {
		byRule[operationRule(operation)] = true
		if operation == gapOperation {
			if names[operationRule(operation)] {
				test.Errorf("%s is reachable from the tool; a setup token must never be taken in conversation", operation)
			}
			continue
		}
		if covered[operation] {
			continue
		}
		if !names[operationRule(operation)] {
			test.Errorf("%s has no tool operation %s", operation, operationRule(operation))
		}
	}
	for name := range names {
		if _, isSpanning := spanningOperations[name]; !isSpanning && !byRule[name] {
			test.Errorf("the tool's %s has no operation in the finance area behind it", name)
		}
	}
}

// Reads are read, deletes destructive, and the rest write. The operations
// that answer with an address or a place to paste a token change nothing.
func TestFinanceRiskPerOperation(test *testing.T) {
	test.Parallel()
	tool := financeTool(test)
	reads := map[string]bool{
		"providers": true, "sources": true, "accounts": true, "transactions": true, "spending_summary": true,
		"exchange_rate": true, "convert_currency": true, "net_worth": true, "assets": true, "asset_history": true,
		"spending_categories": true, "spending_rules": true, "budgets": true, "budget_status": true,
		"spending_by_day": true, "cash_flow": true, "savings_targets": true,
		"link_plaid": true, "repair": true, "link_simplefin": true,
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

// Linking through Plaid and repairing answer with the dashboard's page.
func TestFinanceToolGivesTheLinkingPage(test *testing.T) {
	test.Parallel()
	result, err := call(test, &fakeOperations{}, `{"operation":"link_plaid"}`)
	if err != nil || !strings.Contains(result.Content, "https://mail.example.com/finance-link") {
		test.Fatalf("%v %v", result, err)
	}
	result, err = call(test, &fakeOperations{}, `{"operation":"repair","source_id":"source-one"}`)
	if err != nil || !strings.Contains(result.Content, "/finance-link?source=source-one") {
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
