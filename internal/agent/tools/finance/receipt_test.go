package finance_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// inventedReceiptCall is an invented grocery receipt as read off a Gmail
// message: two tax marks, a promotion under the item it takes money off,
// a weighed item, and amounts that add up (16.29 + 0.49 + 0.19 = 16.97).
// One amount comes as a JSON number, as some models send them.
const inventedReceiptCall = `{"operation":"record_receipt","gmail_message_id":"gmail-invented-one","merchant_name":"Maple Street Market",
"purchased_on":"2026-09-10","currency_code":"USD","subtotal_amount":"16.29","total_amount":"16.97","payment_account_mask":"4821",
"receipt_lines":[
 {"receipt_line_kind":"item","description":"CHICKEN STOCK","line_amount":"3.29","tax_class_code":"t"},
 {"receipt_line_kind":"item","description":"TOOTHPASTE","line_amount":6.99,"tax_class_code":"T"},
 {"receipt_line_kind":"item","description":"PEACHES","line_amount":"3.99","tax_class_code":"t"},
 {"receipt_line_kind":"discount","description":"Promotion","line_amount":"-2.00","tax_class_code":"t","discounted_line_number":3},
 {"receipt_line_kind":"item","description":"ONIONS","quantity":"2.53","quantity_unit":"lb","unit_price_amount":"1.59","line_amount":"4.02","tax_class_code":"t"},
 {"receipt_line_kind":"tax","description":"Sales Tax","line_amount":"0.49","tax_class_code":"T"},
 {"receipt_line_kind":"tax","description":"Food Tax","line_amount":"0.19","tax_class_code":"t"}
]}`

// A receipt that adds up is a write; one that does not is refused before
// the person is asked, unless the person said to record it as it is, which
// its card then says.
func TestFinanceToolJudgesAReceiptByItsCheck(test *testing.T) {
	test.Parallel()
	tool := financeTool(test)
	if risk := tool.RiskFor(json.RawMessage(inventedReceiptCall)); risk != tools.RiskWrite {
		test.Errorf("a receipt that adds up is %s", risk)
	}
	misread := strings.Replace(inventedReceiptCall, `"line_amount":"3.99"`, `"line_amount":"3.89"`, 1)
	if risk := tool.RiskFor(json.RawMessage(misread)); risk != tools.RiskRead {
		test.Errorf("a receipt that does not add up is %s", risk)
	}
	operations := &fakeOperations{}
	if _, err := call(test, operations, misread); err == nil || !strings.Contains(err.Error(), "the lines come to 0.10 USD less than printed") || !strings.Contains(err.Error(), "is_unbalanced_accepted") {
		test.Errorf("the refusal says by how much and what to do: %v", err)
	}
	if len(operations.documents) != 0 {
		test.Errorf("a receipt that does not add up was sent: %v", operations.documents)
	}
	accepted := strings.Replace(misread, `"operation":"record_receipt",`, `"operation":"record_receipt","is_unbalanced_accepted":true,`, 1)
	if risk := tool.RiskFor(json.RawMessage(accepted)); risk != tools.RiskWrite {
		test.Errorf("a receipt the person said to record is %s", risk)
	}
	if line := tool.PreviewLine(context.Background(), json.RawMessage(accepted)); !strings.Contains(line, "unbalanced") || !strings.Contains(line, "recorded anyway") {
		test.Errorf("the card says it does not add up and is recorded anyway: %q", line)
	}
	misnamed := strings.Replace(inventedReceiptCall, `"line_amount":"3.29"`, `"price":"3.29"`, 1)
	if _, err := call(test, &fakeOperations{}, misnamed); err == nil || !strings.Contains(err.Error(), "price") {
		test.Errorf("a field a line does not take is refused, naming it: %v", err)
	}
}

// The card from the receipt alone names the merchant, the day, the total,
// the lines and the check; with the server, the charge it would match.
func TestFinanceToolPreviewsAReceipt(test *testing.T) {
	test.Parallel()
	line := financeTool(test).PreviewLine(context.Background(), json.RawMessage(inventedReceiptCall))
	for _, said := range []string{`"Maple Street Market"`, "2026-09-10", "16.97 USD", "7 lines", "balanced"} {
		if !strings.Contains(line, said) {
			test.Errorf("the card %q does not say %s", line, said)
		}
	}
	operations := &fakeOperations{answers: map[string]string{
		"PreviewRecordReceipt": `{"receiptCheckState":"balanced","checkDifferenceAmount":"0","receiptCheckSummary":"balanced","isReplacing":false,
"receiptMatchCandidates":[{"financeTransactionId":"charge-invented","matchedAmount":"16.9700","isExactAmount":true,"isSameAccount":true,
"isMerchantNameShared":true,"dayDistanceCount":1,"isAutomatic":true,"matchConfidence":"1.00"}]}`,
	}}
	line = financeTool(test).PreviewLine(tools.WithRun(context.Background(), &fakeRun{operations: operations}), json.RawMessage(inventedReceiptCall))
	if !strings.Contains(line, "; matched to ") {
		test.Errorf("the card names the charge it would match: %q", line)
	}
	if len(operations.documents) == 0 || !strings.Contains(operations.documents[0], "PreviewRecordReceipt(") {
		test.Fatalf("the card asked the server: %v", operations.documents)
	}
}

// What is sent is the API's names, the lines' fields included, amounts as
// text even when a model sent a number; what comes back is untrusted.
func TestFinanceToolRecordsAReceipt(test *testing.T) {
	test.Parallel()
	operations := &fakeOperations{answers: map[string]string{
		"RecordReceipt": `{"financeReceipt":{"id":"receipt-invented","merchantName":"Maple Street Market"},"receiptCheckSummary":"balanced","receiptMatchCandidates":[],"isReplaced":false}`,
	}}
	result, err := call(test, operations, inventedReceiptCall)
	if err != nil {
		test.Fatalf("record_receipt: %s", err)
	}
	if !result.Untrusted || !strings.Contains(result.Content, "receipt-invented") {
		test.Errorf("the answer is untrusted and says what was stored: %+v", result)
	}
	if len(operations.documents) != 1 || !strings.Contains(operations.documents[0], "RecordReceipt(") {
		test.Fatalf("documents %v", operations.documents)
	}
	sent := operations.variables[0]
	if sent["gmailMessageId"] != "gmail-invented-one" || sent["totalAmount"] != "16.97" || sent["paymentAccountMask"] != "4821" {
		test.Errorf("sent %v", sent)
	}
	lines, _ := sent["receiptLines"].([]map[string]any)
	if len(lines) != 7 || lines[1]["lineAmount"] != "6.99" || lines[3]["discountedLineNumber"] != 3 || lines[4]["quantityUnit"] != "lb" {
		test.Errorf("the lines are sent by the API's names, amounts as text: %v", lines)
	}
}

// An annotation's card says what is written on which transaction, or that
// it is taken away; a receipt deleted is destructive.
func TestFinanceToolAnnotatesAndDeletes(test *testing.T) {
	test.Parallel()
	tool := financeTool(test)
	line := tool.PreviewLine(context.Background(), json.RawMessage(`{"operation":"annotate_transaction","finance_transaction_id":"charge-invented","annotation":"a birthday present"}`))
	if !strings.Contains(line, `Write "a birthday present" on a transaction`) {
		test.Errorf("the card %q", line)
	}
	line = tool.PreviewLine(context.Background(), json.RawMessage(`{"operation":"annotate_transaction","finance_transaction_id":"charge-invented","annotation":""}`))
	if !strings.Contains(line, "Take the annotation off a transaction") {
		test.Errorf("the card %q", line)
	}
	if risk := tool.RiskFor(json.RawMessage(`{"operation":"delete_receipt","receipt_id":"receipt-invented"}`)); risk != tools.RiskDestructive {
		test.Errorf("deleting a receipt is %s", risk)
	}
	operations := &fakeOperations{answers: map[string]string{"AnnotateTransaction": `{"id":"charge-invented","annotation":"a birthday present","annotatedBy":"agent"}`}}
	if _, err := call(test, operations, `{"operation":"annotate_transaction","finance_transaction_id":"charge-invented","annotation":"a birthday present"}`); err != nil {
		test.Fatalf("annotate_transaction: %s", err)
	}
	if sent := operations.variables[0]; sent["financeTransactionId"] != "charge-invented" || sent["annotation"] != "a birthday present" || sent["isAskedByPerson"] != false {
		test.Errorf("sent %v", sent)
	}
}

// Whether the agent may replace the person's annotation comes from the
// run, never from the model: true only when the person is there in the
// conversation, and false in a schedule, in mail, with nobody present, or
// for a call let through by what the person allows while away. A call
// from an MCP client is a direct run, which can always ask (the person at
// the harness made the call), so it counts as the person being there and
// may replace their annotation, as agent_profile treats direct runs.
func TestFinanceToolTellsWhetherThePersonAskedForTheAnnotation(test *testing.T) {
	test.Parallel()
	arguments := `{"operation":"annotate_transaction","finance_transaction_id":"charge-invented","annotation":"a birthday present"}`
	for name, want := range map[string]struct {
		run                   *fakeRun
		isConfirmedUnattended bool
		isAskedByPerson       bool
	}{
		"the drawer":         {run: &fakeRun{surface: "drawer", canAsk: true}, isAskedByPerson: true},
		"an MCP client":      {run: &fakeRun{surface: "mcp", canAsk: true}, isAskedByPerson: true},
		"a schedule":         {run: &fakeRun{surface: "schedule", canAsk: true}},
		"mail":               {run: &fakeRun{surface: "mail", canAsk: true}},
		"nobody present":     {run: &fakeRun{}},
		"allowed while away": {run: &fakeRun{surface: "drawer", canAsk: true}, isConfirmedUnattended: true},
	} {
		operations := &fakeOperations{answers: map[string]string{"AnnotateTransaction": `{"id":"charge-invented"}`}}
		want.run.operations = operations
		ctx := tools.WithRun(context.Background(), want.run)
		if _, err := financeTool(test).Run(ctx, &tools.Call{ID: "call-one", Arguments: json.RawMessage(arguments), IsConfirmedUnattended: want.isConfirmedUnattended}); err != nil {
			test.Fatalf("%s: %s", name, err)
		}
		if sent := operations.variables[0]; sent["isAskedByPerson"] != want.isAskedByPerson {
			test.Errorf("%s sends isAskedByPerson %v: %v", name, want.isAskedByPerson, sent)
		}
	}
	operations := &fakeOperations{answers: map[string]string{"AnnotateTransaction": `{"id":"charge-invented"}`}}
	_, err := call(test, operations, `{"operation":"annotate_transaction","finance_transaction_id":"charge-invented","annotation":"x","is_asked_by_person":true}`)
	if err != nil || operations.variables[0]["isAskedByPerson"] != false {
		test.Errorf("the model cannot say the person asked: %v %v", err, operations.variables)
	}
}
