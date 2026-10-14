package finance

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/client"
)

// Paystubs and W-2s: read_tax_document has the reading job read a file
// the person handed in (in the chat, by the agent_attachment_id the
// conversation names it with) or the files a message carries, and
// record_paystub and record_tax_form record one the agent read itself,
// line by line or box by box. The server checks them; only the last four
// digits of a taxpayer number are ever sent or kept.

// paystubArguments are record_paystub's arguments.
var paystubArguments = []string{
	"agent_attachment_id", "employer_name", "employer_tax_id", "employee_name", "employee_tax_id_mask", "pay_date", "period_start_on",
	"period_end_on", "currency_code", "gross_amount", "year_to_date_gross_amount", "total_tax_amount", "total_deduction_amount",
	"net_amount", "paystub_lines", "is_unbalanced_accepted",
}

// paystubLineFields are what a line of paystub_lines says.
var paystubLineFields = []string{
	"paystub_line_kind", "description", "hours", "rate_amount", "current_amount", "year_to_date_amount", "paystub_tax_kind",
	"tax_jurisdiction_code", "current_wages_amount", "year_to_date_wages_amount", "paystub_deduction_kind",
	"employer_current_amount", "employer_year_to_date_amount",
}

// taxFormArguments are record_tax_form's arguments.
var taxFormArguments = []string{
	"agent_attachment_id", "tax_form_kind", "tax_year", "is_corrected", "payer_name", "payer_tax_id", "recipient_name",
	"recipient_tax_id_mask", "tax_form_entries",
}

// taxFormEntryFields are what an entry of tax_form_entries says.
var taxFormEntryFields = []string{"box_code", "box_qualifier", "amount", "text_value", "is_checked"}

// isAmountField says a field holds an amount or a number the server takes
// as a decimal string, which a model may send as a JSON number.
func isAmountField(field string) bool {
	return strings.HasSuffix(field, "_amount") || field == "hours" || field == "amount" || field == "box_code"
}

// listVariables is a list argument's objects with their fields in camel
// case and their amounts as decimal strings, refusing a field the list
// does not take, naming it: a field the server does not know would be
// dropped, and the document checked without it.
func listVariables(call map[string]any, key string, fields []string) ([]map[string]any, error) {
	isField := map[string]bool{}
	for _, field := range fields {
		isField[field] = true
	}
	list, _ := call[key].([]any)
	converted := make([]map[string]any, 0, len(list))
	for index, element := range list {
		object, isObject := element.(map[string]any)
		if !isObject {
			return nil, fmt.Errorf("%s %d is not an object with %s", key, index+1, strings.Join(fields, ", "))
		}
		variables := map[string]any{}
		for field, value := range object {
			if !isField[field] {
				return nil, fmt.Errorf("%s %d has %s, which it does not take; it takes %s", key, index+1, field, strings.Join(fields, ", "))
			}
			if value == nil {
				continue
			}
			if isAmountField(field) {
				value = amountText(value)
			}
			variables[camelCase(field)] = value
		}
		converted = append(converted, variables)
	}
	return converted, nil
}

// taxDocumentVariables are a record operation's arguments as the API takes
// them: the list converted, amounts as decimal strings, the tax year whole.
func taxDocumentVariables(asked map[string]any, arguments []string, listKey string, listFields []string) (map[string]any, error) {
	variables := map[string]any{}
	for _, key := range arguments {
		value, isGiven := asked[key]
		if !isGiven || value == nil {
			continue
		}
		switch {
		case key == listKey:
			list, err := listVariables(asked, key, listFields)
			if err != nil {
				return nil, err
			}
			value = list
		case key == "tax_year":
			value = wholeNumber(value)
		case strings.HasSuffix(key, "_amount") || strings.HasSuffix(key, "_mask"):
			value = amountText(value)
		}
		variables[camelCase(key)] = value
	}
	return variables, nil
}

func recordTaxDocument(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	var variables map[string]any
	var err error
	var recorded any
	operation := "RecordPaystub"
	if name == "record_tax_form" {
		operation = "RecordTaxForm"
		variables, err = taxDocumentVariables(asked, taxFormArguments, "tax_form_entries", taxFormEntryFields)
	} else {
		variables, err = taxDocumentVariables(asked, paystubArguments, "paystub_lines", paystubLineFields)
	}
	if err != nil {
		return nil, err
	}
	if err := client.RunFinance(ctx, executor, operation, variables, &recorded); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{name: recorded})
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

func readTaxDocument(ctx context.Context, executor tools.Operations, name string, asked map[string]any) (*tools.Result, error) {
	attachmentId, itemId := text(asked, "agent_attachment_id"), text(asked, "mailbox_item_id")
	if (attachmentId == "") == (itemId == "") {
		return nil, fmt.Errorf("%s needs agent_attachment_id (a file handed over in the conversation) or mailbox_item_id (a message), one of the two", name)
	}
	variables := map[string]any{"agentAttachmentId": attachmentId}
	if itemId != "" {
		// Only a message in a mailbox the person granted, as mail_read
		// reads: a mailbox kept back from the agent stays kept back.
		views, err := mailbox.GrantedMailboxes(ctx, executor)
		if err != nil {
			return nil, err
		}
		if _, _, err := mailbox.MailboxOfItem(ctx, executor, views, itemId); err != nil {
			return nil, err
		}
		variables = map[string]any{"mailboxItemId": itemId}
	}
	var reading *client.TaxDocumentReading
	if err := client.RunFinance(ctx, executor, "ReadTaxDocument", variables, &reading); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{name: reading, "hint": "the reading job reads each file in the background; paystubs, tax_forms and tax_year show them once read"})
	if err != nil {
		return nil, err
	}
	result.Note = "read a tax document"
	return result, nil
}

// recordPaystubPreview is the card of a paystub the agent read: whose,
// paid when, gross and take-home.
func recordPaystubPreview(_ *previewLookup, call map[string]any) string {
	line := fmt.Sprintf("Record a paystub from %s paid %s: %s gross, %s take-home", tools.Named(text(call, "employer_name"), "an employer"),
		text(call, "pay_date"), amountText(call["gross_amount"]), amountText(call["net_amount"]))
	if isAccepted, _ := call["is_unbalanced_accepted"].(bool); isAccepted {
		line += ", even if it does not add up"
	}
	return line
}

// recordTaxFormPreview is the card of a W-2 the agent read.
func recordTaxFormPreview(_ *previewLookup, call map[string]any) string {
	return fmt.Sprintf("Record a %d W-2 from %s with %d boxes", wholeNumber(call["tax_year"]), tools.Named(text(call, "payer_name"), "an employer"),
		len(objects(call, "tax_form_entries")))
}
