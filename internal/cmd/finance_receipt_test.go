package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// inventedReceiptStored is an invented receipt as the server answers it,
// matched by the receipt matcher.
const inventedReceiptStored = `{"id":"receipt-invented","receiptSourceKind":"gmail_message","gmailMessageId":"gmail-invented-one",
"merchantName":"Maple Street Market","purchasedOn":"2026-09-10","currencyCode":"USD","subtotalAmount":"16.2900","totalAmount":"16.9700",
"receiptCheckState":"balanced","checkDifferenceAmount":"0.0000","createdAt":"2026-10-01T10:00:00Z","modifiedAt":"2026-10-01T10:00:00Z",
"receiptLines":[{"id":"line-1","lineNumber":1,"receiptLineKind":"item","description":"PEACHES","lineAmount":"3.9900"}],
"receiptMatches":[{"receiptId":"receipt-invented","financeTransactionId":"charge-invented","matchedAmount":"16.9700","receiptMatchSource":"receipt_matcher","matchConfidence":"0.8500","createdAt":"2026-10-01T10:00:00Z"}]}`

// receiptServer answers the receipt operations with invented results,
// recording what it was sent and which operations.
func receiptServer(test *testing.T) (*httptest.Server, func() []map[string]any, func() []string) {
	test.Helper()
	var mutex sync.Mutex
	var asked []map[string]any
	var operationsAsked []string
	candidate := `{"financeTransactionId":"charge-invented","financeTransaction":{"id":"charge-invented","financeAccountId":"account-invented","postedOn":"2026-09-11","amount":"-16.9700","currencyCode":"USD","description":"MAPLE STREET MKT","isPending":false,"receiptCount":0},"matchedAmount":"16.9700","isExactAmount":true,"isSameAccount":true,"isMerchantNameShared":true,"dayDistanceCount":1,"isAutomatic":true,"matchConfidence":"1.00"}`
	answers := map[string]string{
		" PreviewRecordReceipt(": `{"PreviewRecordReceipt":{"receiptCheckState":"balanced","checkDifferenceAmount":"0","receiptCheckSummary":"balanced: the lines add up to the printed totals","isReplacing":false,"receiptMatchCandidates":[` + candidate + `]}}`,
		" RecordReceipt(":        `{"RecordReceipt":{"financeReceipt":` + inventedReceiptStored + `,"receiptCheckSummary":"balanced: the lines add up to the printed totals","receiptMatchCandidates":[],"isReplaced":false}}`,
		" FinanceReceipts(":      `{"FinanceReceipts":{"financeReceipts":[` + inventedReceiptStored + `],"nextCursor":"2026-09-10/receipt-invented","totalCount":3}}`,
		"AnnotateTransaction(":   `{"AnnotateTransaction":{"id":"charge-invented","financeAccountId":"account-invented","postedOn":"2026-09-11","amount":"-16.9700","currencyCode":"USD","description":"MAPLE STREET MKT","isPending":false,"annotation":"a birthday present","annotatedBy":"person","receiptCount":1}}`,
		" MatchReceipt(":         `{"MatchReceipt":` + inventedReceiptStored + `}`,
		"ReadReceipt(":           `{"ReadReceipt":{"agentJobId":"job-invented"}}`,
		"DeleteReceipt(":         `{"DeleteReceipt":true}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/agent/attachments") {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"attachments":[{"id":"attachment-invented","agentId":"agent-invented","name":"receipt.jpg","contentType":"image/jpeg","size":4}]}`))
			return
		}
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation) {
				mutex.Lock()
				asked = append(asked, document.Variables)
				operationsAsked = append(operationsAsked, strings.TrimSuffix(strings.TrimSpace(operation), "("))
				mutex.Unlock()
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":` + answer + `}`))
				return
			}
		}
		response.WriteHeader(http.StatusBadRequest)
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
			mutex.Lock()
			defer mutex.Unlock()
			return append([]map[string]any(nil), asked...)
		}, func() []string {
			mutex.Lock()
			defer mutex.Unlock()
			return append([]string(nil), operationsAsked...)
		}
}

// inventedReceiptFile is an invented receipt as a JSON file, one amount
// written as a number.
const inventedReceiptFile = `{"gmailMessageId":"gmail-invented-one","merchantName":"Maple Street Market","purchasedOn":"2026-09-10",
"currencyCode":"USD","subtotalAmount":"3.99","totalAmount":"3.99",
"receiptLines":[{"receiptLineKind":"item","description":"PEACHES","lineAmount":3.99,"taxClassCode":"t"}]}`

// record-receipt --dry-run says the check and the proposed match and
// writes nothing; without it, it records and says what it matched,
// amounts sent as text. annotate-transaction, match-receipt and
// delete-receipt send what they name.
func TestFinanceReceiptCommands(test *testing.T) {
	test.Parallel()
	server, asked, operations := receiptServer(test)
	receiptPath := filepath.Join(test.TempDir(), "receipt.json")
	if err := os.WriteFile(receiptPath, []byte(inventedReceiptFile), 0o600); err != nil {
		test.Fatal(err)
	}
	printed, err := runFinanceAgainst(test, server, "record-receipt", "--dry-run", receiptPath)
	if err != nil {
		test.Fatalf("record-receipt --dry-run: %s", err)
	}
	if !strings.Contains(printed, "balanced") || !strings.Contains(printed, "proposed match 2026-09-11 -16.97 USD MAPLE STREET MKT (charge-invented)") ||
		!strings.Contains(printed, "nothing was written") {
		test.Errorf("the dry run printed %q", printed)
	}
	printed, err = runFinanceAgainst(test, server, "record-receipt", receiptPath)
	if err != nil {
		test.Fatalf("record-receipt: %s", err)
	}
	if !strings.Contains(printed, "receipt-invented: Maple Street Market 16.97 USD, balanced") ||
		!strings.Contains(printed, "matched to charge-invented for 16.97 USD, by the receipt matcher") {
		test.Errorf("record-receipt printed %q", printed)
	}
	sent := asked()
	if len(sent) != 2 || sent[0]["gmailMessageId"] != "gmail-invented-one" || sent[0]["isUnbalancedAccepted"] != nil {
		test.Fatalf("sent %v", sent)
	}
	lines, _ := sent[1]["receiptLines"].([]any)
	if first, _ := lines[0].(map[string]any); len(lines) != 1 || first["lineAmount"] != "3.99" || first["taxClassCode"] != "t" {
		test.Errorf("the lines sent %v", sent[1]["receiptLines"])
	}
	if names := operations(); names[0] != "PreviewRecordReceipt" || names[1] != "RecordReceipt" {
		test.Errorf("the dry run asked for the preview, the record for the record: %v", names)
	}

	unknownPath := filepath.Join(test.TempDir(), "unknown.json")
	if err := os.WriteFile(unknownPath, []byte(strings.Replace(inventedReceiptFile, `"lineAmount"`, `"price"`, 1)), 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := runFinanceAgainst(test, server, "record-receipt", unknownPath); err == nil || !strings.Contains(err.Error(), "price") {
		test.Errorf("a field the file does not take answered %v", err)
	}

	printed, err = runFinanceAgainst(test, server, "annotate-transaction", "charge-invented", "a", "birthday", "present")
	if err != nil || !strings.Contains(printed, "charge-invented: a birthday present") {
		test.Errorf("annotate-transaction printed %q, %v", printed, err)
	}
	if _, err := runFinanceAgainst(test, server, "annotate-transaction", "charge-invented"); err == nil || !strings.Contains(err.Error(), "--clear") {
		test.Errorf("an annotation left out answered %v", err)
	}
	if _, err := runFinanceAgainst(test, server, "match-receipt", "receipt-invented", "charge-invented", "--amount", "10.00"); err != nil {
		test.Errorf("match-receipt: %s", err)
	}
	if _, err := runFinanceAgainst(test, server, "delete-receipt", "--force", "receipt-invented"); err != nil {
		test.Errorf("delete-receipt: %s", err)
	}
	sent = asked()
	if annotation := sent[2]; annotation["financeTransactionId"] != "charge-invented" || annotation["annotation"] != "a birthday present" {
		test.Errorf("annotate-transaction sent %v", annotation)
	}
	if match := sent[3]; match["receiptId"] != "receipt-invented" || match["financeTransactionId"] != "charge-invented" || match["matchedAmount"] != "10.00" {
		test.Errorf("match-receipt sent %v", match)
	}
	if deletion := sent[4]; deletion["receiptId"] != "receipt-invented" {
		test.Errorf("delete-receipt sent %v", deletion)
	}

	photoPath := filepath.Join(test.TempDir(), "receipt.jpg")
	if err := os.WriteFile(photoPath, []byte("jpeg"), 0o600); err != nil {
		test.Fatal(err)
	}
	printed, err = runFinanceAgainst(test, server, "read-receipt", "--finance-transaction", "charge-invented", photoPath)
	if err != nil || !strings.Contains(printed, "the receipt job job-invented reads it") {
		test.Errorf("read-receipt printed %q, %v", printed, err)
	}
	if reading := asked()[5]; reading["agentAttachmentId"] != "attachment-invented" || reading["financeTransactionId"] != "charge-invented" {
		test.Errorf("read-receipt sent %v", reading)
	}
	if _, err := runFinanceAgainst(test, server, "read-receipt", "--mailbox-item", "item-invented"); err != nil {
		test.Errorf("read-receipt --mailbox-item: %s", err)
	}
	if reading := asked()[6]; reading["mailboxItemId"] != "item-invented" || reading["agentAttachmentId"] != nil {
		test.Errorf("read-receipt --mailbox-item sent %v", reading)
	}
	if _, err := runFinanceAgainst(test, server, "read-receipt"); err == nil || !strings.Contains(err.Error(), "one of the two") {
		test.Errorf("read-receipt with nothing to read answered %v", err)
	}
}

// receipts pages as transactions does: it sends the limit, the offset,
// the cursor and isUndated.
func TestFinanceReceiptsCommandPages(test *testing.T) {
	test.Parallel()
	server, asked, _ := receiptServer(test)
	if _, err := runFinanceAgainst(test, server, "receipts", "--is-undated", "--limit", "1", "--offset", "1", "--after", "2026-09-12/receipt-earlier"); err != nil {
		test.Fatalf("receipts: %s", err)
	}
	sent := asked()
	if len(sent) != 1 || sent[0]["isUndated"] != true || sent[0]["limit"] != float64(1) || sent[0]["offset"] != float64(1) || sent[0]["after"] != "2026-09-12/receipt-earlier" {
		test.Fatalf("sent %v", sent)
	}
}
