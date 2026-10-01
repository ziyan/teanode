package finance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/finance/ofx"
)

// inventedCardDocument is a card statement as the OFX reader hands it over:
// a purchase, a payment to the card and a refund, signed as OFX signs
// them, and a ledger balance negative for what is owed.
func inventedCardDocument() *ofx.Document {
	return &ofx.Document{
		InstitutionOrganization: "Invented Card Issuer", InstitutionID: "99999",
		Statements: []*ofx.Statement{{
			StatementKind: ofx.StatementKindCreditCard, CurrencyCode: "usd", AccountID: "11111a11-1aa1-1111-a11",
			StartedOn: "2026-01-01", EndedOn: "2026-01-31",
			LedgerBalance: &ofx.Balance{Amount: "-311.25", AsOf: time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC), AsOfDay: "2026-01-31"},
			Transactions: []*ofx.Transaction{
				{TransactionType: "DEBIT", PostedOn: "2026-01-05", PostedAt: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), HasPostedTime: true, Amount: "-23.40", FITID: "fit-one", Name: "INVENTED COFFEE ROASTERS"},
				{TransactionType: "PAYMENT", PostedOn: "2026-01-15", Amount: "150.00", FITID: "fit-two", Name: "PAYMENT RECEIVED"},
				{TransactionType: "CREDIT", PostedOn: "2026-01-20", Amount: "12.00", FITID: "fit-three", Name: "INVENTED BOOKSHOP"},
			},
		}},
	}
}

var inventedAccountKey = []byte("an invented account key of thirty-two bytes")

// A card's statement becomes a credit account whose owed balance is
// negative, with its purchases negative and its payment and refund
// positive, as Plaid's and SimpleFIN's transactions are stored.
func TestStatementImportOfACard(test *testing.T) {
	test.Parallel()
	document := inventedCardDocument()
	statementImport, err := NewStatementImport(inventedAccountKey, document, document.Statements[0])
	if err != nil {
		test.Fatal(err)
	}
	account := statementImport.SyncResult.Accounts[0]
	if account.AccountKind != AccountKindCredit || account.CurrencyCode != "USD" || account.CurrentBalance != "-311.25" ||
		account.IsOwedBalancePositive || account.AccountName != "Invented Card Issuer" || account.AccountMask != "1a11" {
		test.Errorf("account %+v", account)
	}
	if statementImport.BalanceOn != "2026-01-31" || statementImport.FirstPostedOn != "2026-01-05" || statementImport.GeneratedIDCount != 0 {
		test.Errorf("import %+v", statementImport)
	}
	if strings.Contains(account.ProviderAccountID, "11111a11") || strings.Contains(string(account.ProviderMetadata), "11111a11") {
		test.Errorf("the account's identifier was kept: %s %s", account.ProviderAccountID, account.ProviderMetadata)
	}
	added := statementImport.SyncResult.Added
	if len(added) != 3 {
		test.Fatalf("%d transactions", len(added))
	}
	for index, wanted := range []struct{ amount, detailed string }{
		{"-23.4000", "ofx:DEBIT"}, {"150.0000", "ofx:PAYMENT"}, {"12.0000", "ofx:CREDIT"},
	} {
		transaction := added[index]
		if transaction.Amount != wanted.amount || transaction.ProviderCategoryDetailed != wanted.detailed ||
			transaction.ProviderCategoryPrimary != StatementCategoryPrimaryCreditCard || transaction.ProviderAccountID != account.ProviderAccountID ||
			transaction.CurrencyCode != "USD" {
			test.Errorf("transaction %d: %+v", index, transaction)
		}
	}
	if added[0].ProviderTransactionID != "fit-one" || added[0].TransactedAt == nil || added[1].TransactedAt != nil {
		test.Errorf("ids and times: %+v %+v", added[0], added[1])
	}
	// The payment is a transfer and nothing else is decided for the
	// categorize model.
	if _, isTransfer := MapProviderCategory(added[1].ProviderCategoryPrimary, added[1].ProviderCategoryDetailed); !isTransfer {
		test.Error("a payment to the card is not a transfer")
	}
	var metadata map[string]any
	if json.Unmarshal(added[0].ProviderMetadata, &metadata) != nil || metadata["transactionType"] != "DEBIT" {
		test.Errorf("metadata %s", added[0].ProviderMetadata)
	}
}

// The same account in two files is the same provider account id; another
// account, institution or key is another.
func TestStatementAccountID(test *testing.T) {
	test.Parallel()
	document := inventedCardDocument()
	statement := document.Statements[0]
	first := StatementAccountID(inventedAccountKey, document, statement)
	if second := StatementAccountID(inventedAccountKey, inventedCardDocument(), inventedCardDocument().Statements[0]); second != first {
		test.Errorf("the same account is %s and %s", first, second)
	}
	other := inventedCardDocument()
	other.Statements[0].AccountID = "22222b22-2bb2-2222-b22"
	if StatementAccountID(inventedAccountKey, other, other.Statements[0]) == first {
		test.Error("another account has the same id")
	}
	if StatementAccountID([]byte("another invented key"), document, statement) == first {
		test.Error("another key gives the same id")
	}
	otherInstitution := inventedCardDocument()
	otherInstitution.InstitutionID = "88888"
	if StatementAccountID(inventedAccountKey, otherInstitution, otherInstitution.Statements[0]) == first {
		test.Error("another institution's account has the same id")
	}
	if StatementAccountMask("000123456789") != "6789" || StatementAccountMask("12") != "12" || StatementAccountMask("33333c33-3cc3-3333-c3") != "33c3" {
		test.Error("the mask is not the end of the identifier")
	}
}

// A transaction without a FITID is known by its day, amount and name, the
// same way each time, and two identical ones on one day stay two.
func TestStatementImportWithoutFITIDs(test *testing.T) {
	test.Parallel()
	document := inventedCardDocument()
	statement := document.Statements[0]
	statement.Transactions = []*ofx.Transaction{
		{TransactionType: "DEBIT", PostedOn: "2026-01-05", Amount: "-3.00", Name: "INVENTED PARKING"},
		{TransactionType: "DEBIT", PostedOn: "2026-01-05", Amount: "-3.00", Name: "INVENTED PARKING"},
		{TransactionType: "DEBIT", PostedOn: "2026-01-06", Amount: "-3.00", Name: "INVENTED PARKING"},
	}
	first, err := NewStatementImport(inventedAccountKey, document, statement)
	if err != nil {
		test.Fatal(err)
	}
	second, err := NewStatementImport(inventedAccountKey, document, statement)
	if err != nil {
		test.Fatal(err)
	}
	if first.GeneratedIDCount != 3 {
		test.Errorf("%d generated", first.GeneratedIDCount)
	}
	seen := map[string]bool{}
	for index, transaction := range first.SyncResult.Added {
		if seen[transaction.ProviderTransactionID] {
			test.Errorf("%d shares an id: %s", index, transaction.ProviderTransactionID)
		}
		seen[transaction.ProviderTransactionID] = true
		if second.SyncResult.Added[index].ProviderTransactionID != transaction.ProviderTransactionID {
			test.Errorf("%d is %s once and %s again", index, transaction.ProviderTransactionID, second.SyncResult.Added[index].ProviderTransactionID)
		}
	}
}

// A bank statement is a depository account, and a statement without a
// currency is refused rather than guessed.
func TestStatementImportOfABank(test *testing.T) {
	test.Parallel()
	document := &ofx.Document{InstitutionOrganization: "Invented Savings Bank", Statements: []*ofx.Statement{{
		StatementKind: ofx.StatementKindBank, CurrencyCode: "EUR", AccountID: "000123456789", BankID: "000000000", AccountType: "CHECKING",
		Transactions: []*ofx.Transaction{{TransactionType: "PAYMENT", PostedOn: "2026-02-03", Amount: "-80.00", FITID: "bank-one", Name: "INVENTED UTILITY"}},
	}}}
	statementImport, err := NewStatementImport(inventedAccountKey, document, document.Statements[0])
	if err != nil {
		test.Fatal(err)
	}
	account := statementImport.SyncResult.Accounts[0]
	if account.AccountKind != AccountKindDepository || account.AccountMask != "6789" || account.CurrentBalance != "" || statementImport.BalanceOn != "" {
		test.Errorf("account %+v, balance on %q", account, statementImport.BalanceOn)
	}
	paid := statementImport.SyncResult.Added[0]
	if _, isTransfer := MapProviderCategory(paid.ProviderCategoryPrimary, paid.ProviderCategoryDetailed); isTransfer {
		test.Error("a bill paid from a bank account is a transfer")
	}
	document.Statements[0].CurrencyCode = ""
	if _, err := NewStatementImport(inventedAccountKey, document, document.Statements[0]); err == nil {
		test.Error("a statement without a currency was imported")
	}
	if _, err := NewStatementImport(nil, inventedCardDocument(), inventedCardDocument().Statements[0]); err == nil {
		test.Error("a statement was imported without the account key")
	}
}
