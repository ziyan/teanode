package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/client"
	financecore "github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// record_receipt is how the agent records a receipt it read: an order
// email, an itemized receipt in a message, a Gmail message it read through
// the Gmail skill. It sends the receipt line by line, as printed; the check
// that the lines add up is the finance package's, the same one the API runs
// before writing, so a receipt that does not add up is refused before the
// person is asked, and one the person says to record anyway says so on its
// card. The card asks the server what recording would do
// (PreviewRecordReceipt, also the tool's preview_record_receipt): the check
// and the charge it would be matched to.

// receiptArguments are record_receipt's arguments, and
// preview_record_receipt's.
var receiptArguments = []string{
	"mailbox_item_id", "gmail_message_id", "agent_attachment_id", "merchant_name", "merchant_receipt_number", "purchased_on",
	"purchased_at", "currency_code", "subtotal_amount", "total_amount", "payment_account_mask", "receipt_lines",
	"finance_transaction_id", "is_unbalanced_accepted",
}

// receiptLineFields are what a line of receipt_lines says.
var receiptLineFields = []string{
	"line_number", "receipt_line_kind", "description", "quantity", "quantity_unit", "unit_price_amount", "line_amount",
	"tax_class_code", "discounted_line_number",
}

// checkReceiptLineFields refuses a line with a field it does not take,
// naming it: a price sent as "price" would be dropped, and the receipt
// would then be checked without it.
func checkReceiptLineFields(call map[string]any) error {
	isField := map[string]bool{}
	for _, field := range receiptLineFields {
		isField[field] = true
	}
	list, _ := call["receipt_lines"].([]any)
	for index, element := range list {
		object, isObject := element.(map[string]any)
		if !isObject {
			return fmt.Errorf("receipt_lines %d is not an object with %s", index+1, strings.Join(receiptLineFields, ", "))
		}
		for field := range object {
			if !isField[field] {
				return fmt.Errorf("receipt_lines %d has %s, which it does not take; it takes %s", index+1, field, strings.Join(receiptLineFields, ", "))
			}
		}
	}
	return nil
}

// wholeNumber is a whole-number argument, zero when absent: a model sends
// a number as a JSON number, which arrives as a float64.
func wholeNumber(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	}
	return 0
}

// receiptOf is record_receipt's arguments as the finance package checks
// them. The risk, the card and the run all read them through this one
// function, so what is judged is what is sent.
func receiptOf(call map[string]any) *models.FinanceReceipt {
	receipt := &models.FinanceReceipt{
		MerchantName: text(call, "merchant_name"), MerchantReceiptNumber: text(call, "merchant_receipt_number"),
		PurchasedOn: text(call, "purchased_on"), CurrencyCode: text(call, "currency_code"),
		SubtotalAmount: amountText(call["subtotal_amount"]), TotalAmount: amountText(call["total_amount"]),
		PaymentAccountMask: amountText(call["payment_account_mask"]),
	}
	for index, line := range objects(call, "receipt_lines") {
		lineNumber := wholeNumber(line["line_number"])
		if lineNumber == 0 {
			lineNumber = index + 1
		}
		receipt.ReceiptLines = append(receipt.ReceiptLines, &models.FinanceReceiptLine{
			LineNumber: lineNumber, ReceiptLineKind: models.ReceiptLineKind(strings.ToLower(text(line, "receipt_line_kind"))),
			Description: text(line, "description"), Quantity: amountText(line["quantity"]), QuantityUnit: text(line, "quantity_unit"),
			UnitPriceAmount: amountText(line["unit_price_amount"]), LineAmount: amountText(line["line_amount"]),
			TaxClassCode: text(line, "tax_class_code"), DiscountedLineNumber: wholeNumber(line["discounted_line_number"]),
		})
	}
	return receipt
}

// isRefusedReceipt says a record_receipt call would be refused: a line it
// does not take, a receipt that cannot be as printed, or one whose lines
// do not add up when the person has not said to record it anyway. Such a
// call is not put to the person, since nothing would be written; it runs,
// and its answer says what to read again.
func isRefusedReceipt(arguments json.RawMessage) bool {
	call := map[string]any{}
	if json.Unmarshal(arguments, &call) != nil {
		return false
	}
	if checkReceiptLineFields(call) != nil {
		return true
	}
	receiptCheckState, _, err := financecore.CheckReceipt(receiptOf(call))
	return err != nil || (receiptCheckState == models.ReceiptCheckStateUnbalanced && !isTrue(call, "is_unbalanced_accepted"))
}

// receiptVariables are record_receipt's arguments as the API names them,
// the lines' fields too, amounts as text.
func receiptVariables(asked map[string]any) (map[string]any, error) {
	if err := checkReceiptLineFields(asked); err != nil {
		return nil, err
	}
	variables := map[string]any{}
	for _, key := range receiptArguments {
		value, isGiven := asked[key]
		if !isGiven || value == nil {
			continue
		}
		switch key {
		case "receipt_lines":
			var converted []map[string]any
			for _, object := range objects(asked, key) {
				entry := map[string]any{}
				for field, fieldValue := range object {
					switch field {
					case "line_number", "discounted_line_number":
						fieldValue = wholeNumber(fieldValue)
					case "quantity", "unit_price_amount", "line_amount":
						fieldValue = amountText(fieldValue)
					}
					entry[camelCase(field)] = fieldValue
				}
				converted = append(converted, entry)
			}
			value = converted
		case "subtotal_amount", "total_amount", "payment_account_mask":
			value = amountText(value)
		}
		variables[camelCase(key)] = value
	}
	return variables, nil
}

// recordReceiptPreview is the confirmation card of a receipt, from the
// server's preview of it: the merchant, the day, the total and how many
// lines, the check, and the charge it would be matched to, or that it
// would be left for the person. With no run to ask the server through, it
// says what the receipt alone says.
func recordReceiptPreview(lookup *previewLookup, call map[string]any) string {
	receipt := receiptOf(call)
	receiptCheckState, checkDifferenceAmount, err := financecore.CheckReceipt(receipt)
	if err != nil {
		return "Record a receipt; it cannot be as printed and will be refused: " + receiptRefusalText(err)
	}
	line := fmt.Sprintf("Record a receipt from %s", tools.Named(receipt.MerchantName, "a merchant"))
	if receipt.PurchasedOn != "" {
		line += " on " + receipt.PurchasedOn
	}
	line += fmt.Sprintf(" for %s %s, %s; %s", financecore.FormatReceiptAmount(receipt.TotalAmount, receipt.CurrencyCode), receipt.CurrencyCode,
		countOf(len(receipt.ReceiptLines), "line"), financecore.ReceiptCheckSummary(receiptCheckState, checkDifferenceAmount, receipt.CurrencyCode))
	if receiptCheckState == models.ReceiptCheckStateUnbalanced {
		line += ", recorded anyway because the person said to"
	}
	if financeTransactionId := text(call, "finance_transaction_id"); financeTransactionId != "" {
		return line + "; matched to " + lookup.transaction(financeTransactionId)
	}
	if lookup == nil || lookup.executor == nil {
		return line
	}
	variables, err := receiptVariables(call)
	if err != nil {
		return line
	}
	delete(variables, "isUnbalancedAccepted")
	var preview *client.ReceiptPreview
	if err := client.RunFinance(lookup.ctx, lookup.executor, "PreviewRecordReceipt", variables, &preview); err != nil || preview == nil {
		return line
	}
	if preview.IsReplacing {
		line += "; it replaces the receipt read from the same message"
	}
	for _, candidate := range preview.ReceiptMatchCandidates {
		if candidate.IsAutomatic {
			return line + "; matched to " + lookup.transaction(candidate.FinanceTransactionID)
		}
	}
	if len(preview.ReceiptMatchCandidates) > 0 {
		return line + fmt.Sprintf("; not matched, with %s it could explain", countOf(len(preview.ReceiptMatchCandidates), "charge"))
	}
	return line + "; no charge it could explain was found"
}

// receiptRefusalText is a refusal of a receipt without the words every
// refusal begins with.
func receiptRefusalText(err error) string {
	said := err.Error()
	if index := strings.Index(said, financecore.ErrReceiptRefused.Error()+": "); index >= 0 {
		said = said[index+len(financecore.ErrReceiptRefused.Error()+": "):]
	}
	return said
}

// recordReceipt checks the receipt and sends it to RecordReceipt, or with
// preview_record_receipt to PreviewRecordReceipt. A message is taken only
// from a mailbox the person granted, as mail_read reads.
func recordReceipt(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	variables, err := receiptVariables(asked)
	if err != nil {
		return nil, err
	}
	receipt := receiptOf(asked)
	receiptCheckState, checkDifferenceAmount, err := financecore.CheckReceipt(receipt)
	if err != nil {
		return nil, err
	}
	if itemId := text(asked, "mailbox_item_id"); itemId != "" {
		views, err := mailbox.GrantedMailboxes(ctx, executor)
		if err != nil {
			return nil, err
		}
		if _, _, err := mailbox.MailboxOfItem(ctx, executor, views, itemId); err != nil {
			return nil, err
		}
	}
	operation := "RecordReceipt"
	if name == "preview_record_receipt" {
		operation = "PreviewRecordReceipt"
		delete(variables, "isUnbalancedAccepted")
	} else if receiptCheckState == models.ReceiptCheckStateUnbalanced && !isTrue(asked, "is_unbalanced_accepted") {
		return nil, fmt.Errorf("the receipt was not recorded: %s; "+
			"read it again line by line, and give is_unbalanced_accepted true only when the person says to record it as it is",
			financecore.ReceiptCheckDifferenceWords(checkDifferenceAmount, receipt.CurrencyCode))
	}
	var answered any
	if err := client.RunFinance(ctx, executor, operation, variables, &answered); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{name: answered})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	result.Note = strings.ReplaceAll(name, "_", " ")
	return result, nil
}

// readReceipt queues the receipt job for a message in a mailbox the person
// granted, as mail_read reads, and says it was queued.
func readReceipt(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	itemId := text(asked, "mailbox_item_id")
	views, err := mailbox.GrantedMailboxes(ctx, executor)
	if err != nil {
		return nil, err
	}
	if _, _, err := mailbox.MailboxOfItem(ctx, executor, views, itemId); err != nil {
		return nil, err
	}
	var reading *client.ReceiptReading
	if err := client.RunFinance(ctx, executor, "ReadReceipt", map[string]any{"mailboxItemId": itemId}, &reading); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{name: reading, "hint": "the receipt job reads it in the background; receipts shows it once it is read"})
	if err != nil {
		return nil, err
	}
	result.Note = "read a receipt"
	return result, nil
}

// annotateTransactionPreview is the card of an annotation: what is written
// on which transaction, or that it is taken away.
func annotateTransactionPreview(lookup *previewLookup, call map[string]any) string {
	transaction := lookup.transaction(text(call, "finance_transaction_id"))
	if annotation := text(call, "annotation"); annotation != "" {
		return "Write " + tools.Named(annotation, "") + " on " + transaction
	}
	return "Take the annotation off " + transaction
}

// receiptName is a receipt as the person knows it: its merchant, day and
// total.
func (self *previewLookup) receiptName(receiptId string) string {
	var receipt *client.FinanceReceipt
	if self.executor == nil || client.RunFinance(self.ctx, self.executor, "FinanceReceipt", map[string]any{"receiptId": receiptId}, &receipt) != nil || receipt == nil {
		return "a receipt"
	}
	name := "the receipt from " + tools.Named(receipt.MerchantName, "a merchant")
	if receipt.PurchasedOn != "" {
		name += " on " + receipt.PurchasedOn
	}
	return name + " for " + financecore.FormatReceiptAmount(receipt.TotalAmount, receipt.CurrencyCode) + " " + receipt.CurrencyCode
}
