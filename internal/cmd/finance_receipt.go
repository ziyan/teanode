package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Annotations and receipts on finance transactions: teanode finance
// annotate-transaction, receipts, receipt, record-receipt, read-receipt,
// propose-receipt-matches, match-receipt, unmatch-receipt and
// delete-receipt, each calling the finance operation of the same name.

// maximumReceiptFileBytes is the largest receipt file record-receipt
// reads: more than any receipt's lines written out.
const maximumReceiptFileBytes = 1 << 20

// financeReceiptCommands are the subcommands for annotations and receipts.
func financeReceiptCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name: "annotate-transaction", Usage: "write what a transaction was for; your agent replaces it only when you ask it to",
			ArgsUsage: "<transaction-id> <annotation>",
			Flags:     []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "clear", Usage: "take the annotation away instead"}},
			Action:    runFinanceAnnotateTransaction,
		},
		{
			Name: "receipts", Usage: "your receipts, the newest purchase first and those that print no day last, each with its check and what it is matched to",
			Flags: append(rangeFlagsWithoutDefault(), JSONFlag(),
				&cli.StringFlag{Name: "finance-transaction", Usage: "only the receipts of this transaction, by id"},
				&cli.BoolFlag{Name: "is-unmatched", Usage: "only the receipts matched to no transaction"},
				&cli.BoolFlag{Name: "is-undated", Usage: "only the receipts that print no day of purchase; not with a range"},
				&cli.StringFlag{Name: "text", Usage: "only those whose merchant, receipt number or a line's description contains this"},
				&cli.IntFlag{Name: "limit", Usage: "how many, at most 200", Value: 50},
				&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page or one further on"},
				&cli.StringFlag{Name: "after", Usage: "the next page: the cursor the page before printed"},
			),
			Action: runFinanceReceipts,
		},
		{Name: "receipt", Usage: "one receipt, line by line", ArgsUsage: "<receipt-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceReceipt},
		{
			Name: "record-receipt", Usage: "record a receipt from a JSON file of its lines, checked and matched to the charge it explains",
			ArgsUsage: "<file.json | ->",
			Description: "The file is one JSON object with the RecordReceipt arguments: where it was read from (one of\n" +
				"mailboxItemId, gmailMessageId and agentAttachmentId), merchantName, merchantReceiptNumber, purchasedOn,\n" +
				"purchasedAt, currencyCode, subtotalAmount, totalAmount, paymentAccountMask (the card's last digits),\n" +
				"financeTransactionId (the charge it is for, matched by hand) and receiptLines ([{lineNumber,\n" +
				"receiptLineKind, description, quantity, quantityUnit, unitPriceAmount, lineAmount, taxClassCode,\n" +
				"discountedLineNumber}]). One line per printed line, as printed: items, discounts (negative, with the\n" +
				"item they take money off), taxes, fees and tips; leave out lines such as \"You saved\". A receipt\n" +
				"whose lines do not add up to its totals is refused unless --is-unbalanced-accepted. Recording the\n" +
				"same source again replaces its receipt. --dry-run checks it and proposes matches, writing nothing.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.BoolFlag{Name: "dry-run", Usage: "check it and say which charge it would match, writing nothing"},
				&cli.BoolFlag{Name: "is-unbalanced-accepted", Usage: "record it even when its lines do not add up, marked unbalanced"},
				&cli.StringFlag{Name: "finance-transaction", Usage: "the charge it is for, matched by hand, by id"},
			},
			Action: runFinanceRecordReceipt,
		},
		{
			Name: "read-receipt", Usage: "have your agent read a receipt from a photo or a text file, or from a message, record it and match it",
			ArgsUsage: "<file | ->",
			Description: "The file goes up the way a file for your agent does, and the receipt job reads it in the\n" +
				"background: a picture is shown to a model, a text file is read; a PDF's text is not read on the\n" +
				"server, so give a photo or a screenshot of one. --mailbox-item reads a message in a mailbox your\n" +
				"agent is granted instead of a file. --finance-transaction, with a file, matches what it reads\n" +
				"to that transaction. teanode finance receipts shows the receipt once it is read.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "mailbox-item", Usage: "read the receipt in this message instead of a file, by its item id"},
				&cli.StringFlag{Name: "finance-transaction", Usage: "with a file: the transaction the receipt is for, by id"},
			},
			Action: runFinanceReadReceipt,
		},
		{
			Name: "propose-receipt-matches", Usage: "the charges a receipt could explain, the likeliest first; matches nothing",
			ArgsUsage: "<receipt-id>", Flags: []cli.Flag{JSONFlag()}, Action: runFinanceProposeReceiptMatches,
		},
		{
			Name: "match-receipt", Usage: "match a receipt to a transaction by hand; one receipt may explain several charges",
			ArgsUsage: "<receipt-id> <transaction-id>",
			Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{
				Name: "amount", Usage: "how much of the charge the receipt explains; the charge or the receipt's total, whichever is less, by default",
			}},
			Action: runFinanceMatchReceipt,
		},
		{
			Name: "unmatch-receipt", Usage: "take a receipt off a transaction", ArgsUsage: "<receipt-id> <transaction-id>",
			Flags: []cli.Flag{JSONFlag()}, Action: runFinanceUnmatchReceipt,
		},
		{
			Name: "delete-receipt", Usage: "delete a receipt, its lines and matches, and the photo or PDF it was read from",
			ArgsUsage: "<receipt-id>", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runFinanceDeleteReceipt,
		},
	}
}

// rangeFlagsWithoutDefault are --from, --to, --since and --month for a
// listing that covers all of time when none is given.
func rangeFlagsWithoutDefault() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "from", Usage: "the first day of purchase, as 2026-09-01"},
		&cli.StringFlag{Name: "to", Usage: "the last day of purchase, as 2026-09-30"},
		&cli.StringFlag{Name: "since", Usage: "instead of --from: a span back from today, as 30d, 12w, 6m or 1y"},
		&cli.StringFlag{Name: "month", Usage: "instead of --from and --to: one whole month, as 2026-09"},
	}
}

func runFinanceAnnotateTransaction(ctx context.Context, command *cli.Command) error {
	financeTransactionId, err := financeArgument(command, 0, "the transaction's id and what it was for: teanode finance annotate-transaction <id> \"a birthday present\"")
	if err != nil {
		return err
	}
	annotation := strings.TrimSpace(strings.Join(command.Args().Slice()[1:], " "))
	if annotation == "" && !command.Bool("clear") {
		return usage("give the annotation, or --clear to take it away")
	}
	if annotation != "" && command.Bool("clear") {
		return usage("give the annotation or --clear, not both")
	}
	var annotated *client.FinanceTransaction
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"financeTransactionId": financeTransactionId, "annotation": annotation}, &annotated); err != nil {
		return err
	}
	if annotated.Annotation == "" {
		return printDone(command, annotated, annotated.ID+": annotation taken away")
	}
	return printDone(command, annotated, fmt.Sprintf("%s: %s", annotated.ID, annotated.Annotation))
}

func runFinanceReceipts(ctx context.Context, command *cli.Command) error {
	from, to, err := rangeOf(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"from": from, "to": to, "limit": int(command.Int("limit")), "offset": int(command.Int("offset"))}
	setString(command, variables, "finance-transaction", "financeTransactionId")
	setBool(command, variables, "is-unmatched", "isUnmatched")
	setBool(command, variables, "is-undated", "isUndated")
	setString(command, variables, "text", "text")
	setString(command, variables, "after", "after")
	var page *client.FinanceReceiptPage
	if err := financeCall(ctx, command, operationOf(command), variables, &page); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FinanceReceipts) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no receipts match")
		return nil
	}
	rows := make([][]string, 0, len(page.FinanceReceipts))
	for _, receipt := range page.FinanceReceipts {
		rows = append(rows, []string{receipt.PurchasedOn, money(receipt.TotalAmount, receipt.CurrencyCode), receipt.MerchantName,
			receipt.ReceiptCheckState, receiptMatchedTo(receipt), receipt.ID})
	}
	if err := printTable([]string{"purchased", "total", "merchant", "check", "matched to", "id"}, rows); err != nil {
		return err
	}
	if note := pageNote(len(page.FinanceReceipts), int(command.Int("offset")), page.TotalCount, page.NextCursor, command.String("after") != ""); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return nil
}

// receiptMatchedTo names the transactions a receipt is matched to, or says
// it is matched to none.
func receiptMatchedTo(receipt *client.FinanceReceipt) string {
	if len(receipt.ReceiptMatches) == 0 {
		return "unmatched"
	}
	matched := make([]string, 0, len(receipt.ReceiptMatches))
	for _, match := range receipt.ReceiptMatches {
		matched = append(matched, match.FinanceTransactionID)
	}
	return strings.Join(matched, ", ")
}

func runFinanceReceipt(ctx context.Context, command *cli.Command) error {
	receiptId, err := financeArgument(command, 0, "the receipt's id; teanode finance receipts lists them")
	if err != nil {
		return err
	}
	var receipt *client.FinanceReceipt
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"receiptId": receiptId}, &receipt); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(receipt)
	}
	return printReceipt(command, receipt)
}

// printReceipt writes a receipt: what and when, each line, the totals, the
// check, and what it is matched to.
func printReceipt(command *cli.Command, receipt *client.FinanceReceipt) error {
	heading := receipt.MerchantName
	if receipt.PurchasedOn != "" {
		heading += ", " + receipt.PurchasedOn
	}
	if receipt.MerchantReceiptNumber != "" {
		heading += ", receipt " + receipt.MerchantReceiptNumber
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(heading))
	rows := make([][]string, 0, len(receipt.ReceiptLines))
	for _, line := range receipt.ReceiptLines {
		quantity := ""
		if line.Quantity != "" {
			quantity = strings.TrimSpace(decimal(line.Quantity) + " " + line.QuantityUnit)
			if line.UnitPriceAmount != "" {
				quantity += " @ " + unitPrice(line.UnitPriceAmount, receipt.CurrencyCode)
			}
		}
		kind := line.ReceiptLineKind
		if line.DiscountedLineNumber != 0 {
			kind += fmt.Sprintf(" of %d", line.DiscountedLineNumber)
		}
		rows = append(rows, []string{fmt.Sprint(line.LineNumber), kind, line.Description, quantity,
			finance.FormatReceiptAmount(line.LineAmount, receipt.CurrencyCode), line.TaxClassCode})
	}
	if err := printTable([]string{"line", "kind", "description", "quantity", "amount", "tax"}, rows); err != nil {
		return err
	}
	if receipt.SubtotalAmount != "" {
		_, _ = fmt.Fprintln(command.Writer, "subtotal "+money(receipt.SubtotalAmount, receipt.CurrencyCode))
	}
	_, _ = fmt.Fprintln(command.Writer, "total "+money(receipt.TotalAmount, receipt.CurrencyCode))
	_, _ = fmt.Fprintln(command.Writer, finance.ReceiptCheckSummary(models.ReceiptCheckState(receipt.ReceiptCheckState),
		finance.FormatReceiptAmount(receipt.CheckDifferenceAmount, receipt.CurrencyCode), receipt.CurrencyCode))
	if len(receipt.ReceiptMatches) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "matched to no transaction; teanode finance propose-receipt-matches "+receipt.ID+" lists the charges it could explain")
	}
	for _, match := range receipt.ReceiptMatches {
		_, _ = fmt.Fprintf(command.Writer, "matched to %s for %s, by %s\n", match.FinanceTransactionID,
			money(match.MatchedAmount, receipt.CurrencyCode), receiptMatchSourceWords[match.ReceiptMatchSource])
	}
	return nil
}

// receiptMatchSourceWords say what matched a receipt.
var receiptMatchSourceWords = map[string]string{"receipt_matcher": "the receipt matcher", "person": "you"}

// receiptFile is what record-receipt reads: the RecordReceipt arguments.
type receiptFile struct {
	MailboxItemID         string            `json:"mailboxItemId,omitempty"`
	GmailMessageID        string            `json:"gmailMessageId,omitempty"`
	AgentAttachmentID     string            `json:"agentAttachmentId,omitempty"`
	MerchantName          string            `json:"merchantName"`
	MerchantReceiptNumber string            `json:"merchantReceiptNumber,omitempty"`
	PurchasedOn           string            `json:"purchasedOn,omitempty"`
	PurchasedAt           string            `json:"purchasedAt,omitempty"`
	CurrencyCode          string            `json:"currencyCode"`
	SubtotalAmount        string            `json:"subtotalAmount,omitempty"`
	TotalAmount           string            `json:"totalAmount"`
	PaymentAccountMask    string            `json:"paymentAccountMask,omitempty"`
	ReceiptLines          []receiptFileLine `json:"receiptLines"`
	FinanceTransactionID  string            `json:"financeTransactionId,omitempty"`
	IsUnbalancedAccepted  bool              `json:"isUnbalancedAccepted,omitempty"`
}

// receiptFileLine is one line of a receipt file. A number in an amount's
// place is taken as the decimal it was written as.
type receiptFileLine struct {
	LineNumber           int         `json:"lineNumber,omitempty"`
	ReceiptLineKind      string      `json:"receiptLineKind"`
	Description          string      `json:"description,omitempty"`
	Quantity             json.Number `json:"quantity,omitempty"`
	QuantityUnit         string      `json:"quantityUnit,omitempty"`
	UnitPriceAmount      json.Number `json:"unitPriceAmount,omitempty"`
	LineAmount           json.Number `json:"lineAmount"`
	TaxClassCode         string      `json:"taxClassCode,omitempty"`
	DiscountedLineNumber int         `json:"discountedLineNumber,omitempty"`
}

// variables are the file as the RecordReceipt arguments, an amount
// written as a number sent as the decimal it was written as.
func (self *receiptFile) variables() map[string]any {
	variables := map[string]any{
		"merchantName": self.MerchantName, "currencyCode": self.CurrencyCode, "totalAmount": self.TotalAmount,
		"isUnbalancedAccepted": self.IsUnbalancedAccepted,
	}
	for key, value := range map[string]string{
		"mailboxItemId": self.MailboxItemID, "gmailMessageId": self.GmailMessageID, "agentAttachmentId": self.AgentAttachmentID,
		"merchantReceiptNumber": self.MerchantReceiptNumber, "purchasedOn": self.PurchasedOn, "purchasedAt": self.PurchasedAt,
		"subtotalAmount": self.SubtotalAmount, "paymentAccountMask": self.PaymentAccountMask, "financeTransactionId": self.FinanceTransactionID,
	} {
		if strings.TrimSpace(value) != "" {
			variables[key] = strings.TrimSpace(value)
		}
	}
	lines := make([]map[string]any, 0, len(self.ReceiptLines))
	for _, line := range self.ReceiptLines {
		entry := map[string]any{"receiptLineKind": line.ReceiptLineKind, "lineAmount": line.LineAmount.String()}
		if line.LineNumber != 0 {
			entry["lineNumber"] = line.LineNumber
		}
		if line.DiscountedLineNumber != 0 {
			entry["discountedLineNumber"] = line.DiscountedLineNumber
		}
		for key, value := range map[string]string{
			"description": line.Description, "quantity": line.Quantity.String(), "quantityUnit": line.QuantityUnit,
			"unitPriceAmount": line.UnitPriceAmount.String(), "taxClassCode": line.TaxClassCode,
		} {
			if value != "" {
				entry[key] = value
			}
		}
		lines = append(lines, entry)
	}
	variables["receiptLines"] = lines
	return variables
}

// readReceiptFile reads a receipt file, or standard input for -,
// refusing a field it does not know.
func readReceiptFile(path string) (*receiptFile, error) {
	var content []byte
	var err error
	if path == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumReceiptFileBytes+1))
	} else {
		content, err = readLimitedFile(path, maximumReceiptFileBytes+1)
	}
	if err != nil {
		return nil, err
	}
	if len(content) > maximumReceiptFileBytes {
		return nil, usage(fmt.Sprintf("the file is larger than %d kB, more than any receipt", maximumReceiptFileBytes/1024))
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var receipt receiptFile
	if err := decoder.Decode(&receipt); err != nil {
		return nil, usage("the file is not the receipt record-receipt reads: " + err.Error())
	}
	return &receipt, nil
}

func runFinanceRecordReceipt(ctx context.Context, command *cli.Command) error {
	path, err := financeArgument(command, 0, "the JSON file of the receipt: teanode finance record-receipt receipt.json")
	if err != nil {
		return err
	}
	receipt, err := readReceiptFile(path)
	if err != nil {
		return err
	}
	if command.IsSet("finance-transaction") {
		receipt.FinanceTransactionID = strings.TrimSpace(command.String("finance-transaction"))
	}
	if command.Bool("is-unbalanced-accepted") {
		receipt.IsUnbalancedAccepted = true
	}
	variables := receipt.variables()
	if command.Bool("dry-run") {
		// The preview takes what the record does, and writes nothing.
		delete(variables, "isUnbalancedAccepted")
		var preview *client.ReceiptPreview
		if err := financeCall(ctx, command, "PreviewRecordReceipt", variables, &preview); err != nil {
			return err
		}
		lines := []string{preview.ReceiptCheckSummary}
		if preview.IsReplacing {
			lines = append(lines, "would replace the receipt read from the same source")
		}
		lines = append(lines, receiptCandidateLines(preview.ReceiptMatchCandidates, receipt.CurrencyCode)...)
		return printDone(command, preview, strings.Join(append(lines, "nothing was written"), "\n"))
	}
	var recorded *client.RecordedReceipt
	if err := financeCall(ctx, command, operationOf(command), variables, &recorded); err != nil {
		return err
	}
	stored := recorded.FinanceReceipt
	line := fmt.Sprintf("%s: %s %s, %s", stored.ID, stored.MerchantName, money(stored.TotalAmount, stored.CurrencyCode), recorded.ReceiptCheckSummary)
	if recorded.IsReplaced {
		line += "; replaced the receipt read from the same source"
	}
	lines := append([]string{line}, recorded.DroppedReceiptMatchReasons...)
	for _, match := range stored.ReceiptMatches {
		lines = append(lines, fmt.Sprintf("matched to %s for %s, by %s", match.FinanceTransactionID,
			money(match.MatchedAmount, stored.CurrencyCode), receiptMatchSourceWords[match.ReceiptMatchSource]))
	}
	if len(stored.ReceiptMatches) == 0 {
		lines = append(lines, "matched to no transaction")
		lines = append(lines, receiptCandidateLines(recorded.ReceiptMatchCandidates, stored.CurrencyCode)...)
	}
	return printDone(command, recorded, strings.Join(lines, "\n"))
}

// receiptCandidateLines say the charges a receipt could explain, the one
// the matcher would take marked.
func receiptCandidateLines(candidates []*client.ReceiptMatchCandidate, currencyCode string) []string {
	if len(candidates) == 0 {
		return []string{"no charge it could explain was found"}
	}
	lines := []string{}
	for _, candidate := range candidates {
		what := candidate.FinanceTransactionID
		if transaction := candidate.FinanceTransaction; transaction != nil {
			what = fmt.Sprintf("%s %s %s (%s)", transaction.PostedOn, money(transaction.Amount, transaction.CurrencyCode),
				forTerminal(transaction.Description), transaction.ID)
		}
		signs := []string{}
		if candidate.IsExactAmount {
			signs = append(signs, "the exact amount")
		}
		if candidate.IsSameAccount {
			signs = append(signs, "the same card")
		}
		if candidate.IsMerchantNameShared {
			signs = append(signs, "the same merchant")
		}
		line := "  candidate " + what + ", explaining " + money(candidate.MatchedAmount, currencyCode)
		if len(signs) > 0 {
			line += ": " + strings.Join(signs, ", ")
		}
		if candidate.IsAutomatic {
			line = "  proposed match " + what + ", explaining " + money(candidate.MatchedAmount, currencyCode) + ", confidence " + candidate.MatchConfidence
		}
		lines = append(lines, line)
	}
	return lines
}

// maximumReceiptPictureBytes is the largest picture read-receipt sends:
// what a model takes as a picture.
const maximumReceiptPictureBytes = 10 << 20

func runFinanceReadReceipt(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{}
	setString(command, variables, "finance-transaction", "financeTransactionId")
	itemId := strings.TrimSpace(command.String("mailbox-item"))
	path := strings.TrimSpace(command.Args().Get(0))
	if (itemId == "") == (path == "") {
		return usage("give the file of the receipt, or --mailbox-item with the message it is in, one of the two")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if itemId != "" {
		variables["mailboxItemId"] = itemId
	} else {
		var content []byte
		fileName := filepath.Base(path)
		if path == "-" {
			fileName = "receipt.jpg"
			content, err = io.ReadAll(io.LimitReader(os.Stdin, maximumReceiptPictureBytes+1))
		} else {
			content, err = readLimitedFile(path, maximumReceiptPictureBytes+1)
		}
		if err != nil {
			return err
		}
		if len(content) > maximumReceiptPictureBytes {
			return usage(fmt.Sprintf("%s is larger than %d MB, more than a model takes as a picture", fileName, maximumReceiptPictureBytes>>20))
		}
		// Up the way a file for the agent goes, then named by id, as the
		// dashboard does it: bytes are not a GraphQL argument.
		attachment, err := client.UploadAgentAttachment(ctx, connection, fileName, content)
		if err != nil {
			return describeError(command, err)
		}
		variables["agentAttachmentId"] = attachment.ID
	}
	var reading *client.ReceiptReading
	if err := describeError(command, client.RunFinance(ctx, connection, operationOf(command), variables, &reading)); err != nil {
		return err
	}
	return printDone(command, reading, "queued: the receipt job "+reading.AgentJobID+" reads it; teanode finance receipts shows it once it is read")
}

func runFinanceProposeReceiptMatches(ctx context.Context, command *cli.Command) error {
	receiptId, err := financeArgument(command, 0, "the receipt's id; teanode finance receipts lists them")
	if err != nil {
		return err
	}
	var receipt *client.FinanceReceipt
	if err := financeCall(ctx, command, "FinanceReceipt", map[string]any{"receiptId": receiptId}, &receipt); err != nil {
		return err
	}
	var candidates []*client.ReceiptMatchCandidate
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"receiptId": receiptId}, &candidates); err != nil {
		return err
	}
	return printDone(command, candidates, strings.Join(receiptCandidateLines(candidates, receipt.CurrencyCode), "\n"))
}

func runFinanceMatchReceipt(ctx context.Context, command *cli.Command) error {
	receiptId, err := financeArgument(command, 0, "the receipt's id and the transaction's: teanode finance match-receipt <receipt-id> <transaction-id>")
	if err != nil {
		return err
	}
	financeTransactionId, err := financeArgument(command, 1, "the transaction's id after the receipt's")
	if err != nil {
		return err
	}
	variables := map[string]any{"receiptId": receiptId, "financeTransactionId": financeTransactionId}
	setString(command, variables, "amount", "matchedAmount")
	var matched *client.FinanceReceipt
	if err := financeCall(ctx, command, operationOf(command), variables, &matched); err != nil {
		return err
	}
	return printDone(command, matched, matched.ID+": matched to "+receiptMatchedTo(matched))
}

func runFinanceUnmatchReceipt(ctx context.Context, command *cli.Command) error {
	receiptId, err := financeArgument(command, 0, "the receipt's id and the transaction's: teanode finance unmatch-receipt <receipt-id> <transaction-id>")
	if err != nil {
		return err
	}
	financeTransactionId, err := financeArgument(command, 1, "the transaction's id after the receipt's")
	if err != nil {
		return err
	}
	var unmatched *client.FinanceReceipt
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"receiptId": receiptId, "financeTransactionId": financeTransactionId}, &unmatched); err != nil {
		return err
	}
	return printDone(command, unmatched, unmatched.ID+": matched to "+receiptMatchedTo(unmatched))
}

func runFinanceDeleteReceipt(ctx context.Context, command *cli.Command) error {
	receiptId, err := financeArgument(command, 0, "the receipt's id; teanode finance receipts lists them")
	if err != nil {
		return err
	}
	if err := confirm(command, "Delete receipt "+receiptId+", its lines and matches, and the photo or PDF it was read from?"); err != nil {
		return err
	}
	var isDeleted bool
	if err := financeCall(ctx, command, operationOf(command), map[string]any{"receiptId": receiptId}, &isDeleted); err != nil {
		return err
	}
	return printDone(command, map[string]any{"receiptId": receiptId, "isDeleted": isDeleted}, receiptId+": deleted")
}
