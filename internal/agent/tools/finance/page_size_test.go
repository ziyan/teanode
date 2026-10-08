package finance_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// transactionPage is a page of the API's transactions as it answers them,
// rowCount rows of totalCount from offset, every field there whether it
// holds anything or not, as a real page has them.
func transactionPage(test *testing.T, offset, rowCount, totalCount int) string {
	test.Helper()
	rows := make([]map[string]any, 0, rowCount)
	for number := offset + 1; number <= min(offset+rowCount, totalCount); number++ {
		rows = append(rows, map[string]any{
			"id": fmt.Sprintf("01JTRANSACTION%012d", number), "financeAccountId": "01JACCOUNT000000000000000001",
			"postedOn": "2030-03-14", "transactedAt": nil, "amount": "-12.50", "currencyCode": "USD",
			"description":             fmt.Sprintf("INVENTED GROCER STORE %04d ANYTOWN", number),
			"merchantName":            "Invented Grocer",
			"providerCategoryPrimary": "FOOD_AND_DRINK", "providerCategoryDetailed": "FOOD_AND_DRINK_GROCERIES",
			"isPending": false, "spendingCategoryId": "01JCATEGORY00000000000000001", "categorizedBy": "rule",
			"categorizationConfidence": 0, "duplicateOfTransactionId": "", "duplicateDecidedBy": "",
			"annotation": "", "annotatedBy": "", "receiptCount": 0,
		})
	}
	nextCursor := ""
	if offset+rowCount < totalCount {
		nextCursor = fmt.Sprintf("2030-03-14/01JTRANSACTION%012d", offset+rowCount)
	}
	encoded, err := json.Marshal(map[string]any{"financeTransactions": rows, "nextCursor": nextCursor, "totalCount": totalCount, "leftOutDuplicateCount": 0})
	if err != nil {
		test.Fatal(err)
	}
	return string(encoded)
}

// A listing of transactions with no limit asks for a page of twenty, its
// rows carry only the fields that hold something, it says how many more
// there are and the offset that reads them, and that offset carries on
// where the page stopped.
//
// A page of the API's fifty rows, each with every field empty or not, came
// to thirty thousand characters, which clients cut short without a word.
func TestFinanceToolListsAPageThatFitsAndSaysHowToReadOn(test *testing.T) {
	test.Parallel()
	before := transactionPage(test, 0, 50, 60)
	test.Logf("fifty rows as the API answers them: %d characters", len(before))

	operations := &fakeOperations{answers: map[string]string{"FinanceTransactions": transactionPage(test, 0, 20, 60)}}
	first, err := call(test, operations, `{"operation":"transactions"}`)
	if err != nil {
		test.Fatal(err)
	}
	test.Logf("the tool's first page: %d characters", len(first.Content))
	if sent := operations.variables[0]; sent["limit"] != 20 {
		test.Errorf("a listing with no limit asks for twenty: %v", sent)
	}
	if len(first.Content) > 8000 {
		test.Errorf("a first page is %d characters", len(first.Content))
	}
	for _, empty := range []string{"transactedAt", "providerCategoryPrimary", "isPending", "categorizationConfidence", "duplicateOfTransactionId", "annotation", "receiptCount", "leftOutDuplicateCount"} {
		if strings.Contains(first.Content, `"`+empty+`"`) {
			test.Errorf("a row leaves out %s: %s", empty, first.Content)
		}
	}
	if !strings.Contains(first.Content, `"merchantName":"Invented Grocer"`) || !strings.Contains(first.Content, `"amount":"-12.50"`) {
		test.Errorf("and keeps what holds something: %s", first.Content)
	}
	if !strings.Contains(first.Content, "rows 1 to 20 of 60 shown") || !strings.Contains(first.Content, "40 more, and offset 20 reads the next page") {
		test.Fatalf("a page says how many more and how to read them: %s", first.Content)
	}

	operations.answers["FinanceTransactions"] = transactionPage(test, 20, 20, 60)
	next, err := call(test, operations, `{"operation":"transactions","offset":20}`)
	if err != nil {
		test.Fatal(err)
	}
	if sent := operations.variables[1]; sent["offset"] != 20 || sent["limit"] != 20 {
		test.Errorf("the next page is asked for from where the first stopped: %v", sent)
	}
	if !strings.Contains(next.Content, "rows 21 to 40 of 60 shown") || !strings.Contains(next.Content, "offset 40 reads the next page") {
		test.Errorf("and says where to read on again: %s", next.Content)
	}

	operations.answers["FinanceTransactions"] = transactionPage(test, 0, 20, 60)
	cursor, err := call(test, operations, `{"operation":"transactions","after":"2030-03-14/01JTRANSACTION000000000000"}`)
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(cursor.Content, "and after 2030-03-14/01JTRANSACTION000000000020 reads the next page") {
		test.Errorf("a page read from a cursor names the cursor of the next: %s", cursor.Content)
	}

	// A limit of zero or less is none given: the page of twenty, and a
	// hint that does not ask for a limit of zero.
	for _, limit := range []string{"0", "-5"} {
		unlimited, err := call(test, operations, `{"operation":"transactions","limit":`+limit+`}`)
		if err != nil {
			test.Fatal(err)
		}
		if sent := operations.variables[len(operations.variables)-1]; sent["limit"] != 20 {
			test.Errorf("limit %s asks for twenty: %v", limit, sent)
		}
		if !strings.Contains(unlimited.Content, "offset 20 reads the next page") || strings.Contains(unlimited.Content, "with limit") {
			test.Errorf("limit %s reads on by offset alone: %s", limit, unlimited.Content)
		}
	}
}
