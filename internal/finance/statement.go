package finance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/finance/ofx"
)

// ProviderKindStatement is the finance source that holds the accounts of
// imported statements: OFX files a person mails in or uploads. It has no
// credential and nothing to poll; it changes only when a statement arrives.
const ProviderKindStatement ProviderKind = "statement"

// The provider categories a statement's transactions carry. The detailed
// one is the OFX transaction type, written "ofx:DEBIT"; the primary one is
// the side of the account it was on, because a PAYMENT means paying the
// card on a card's statement and paying a bill on a bank's.
const (
	StatementCategoryPrefix             = "ofx:"
	StatementCategoryPrimaryCreditCard  = "ofx:creditcard"
	StatementCategoryPrimaryBankAccount = "ofx:bank"
)

// statementAccountMaskLength is how many of the end of an account's
// identifier are kept to tell two accounts at one institution apart. A
// bank's identifier is commonly its account number, so no more than a
// bank would print on a statement is kept.
const statementAccountMaskLength = 4

// StatementImport is one statement turned into what a sync writes: its one
// account and its transactions, and the person's day its balance is from.
type StatementImport struct {
	SyncResult *SyncResult

	// BalanceOn is the day the ledger balance is as of, "2006-01-02", in
	// the file's own zone; empty when the statement has none, and then no
	// value is recorded.
	BalanceOn string

	// FirstPostedOn is the earliest day a transaction posted, empty for a
	// statement without transactions.
	FirstPostedOn string

	// GeneratedIDCount is how many transactions had no FITID and were given
	// an identifier made from what they say.
	GeneratedIDCount int
}

// statementAccountKeyInstitutionField is the account metadata field that
// names the institution an account's provider id was keyed with, for an
// account made before the institution was left out of the key.
const statementAccountKeyInstitutionField = "statementAccountKeyInstitution"

// ExistingStatementAccount is an account the statement source already
// holds, which a statement is matched against before a new one is made.
type ExistingStatementAccount struct {
	ProviderAccountID string
	ProviderMetadata  json.RawMessage
}

// StatementAccountID is the provider account id a statement's account is
// kept under: a keyed hash of the statement's kind and the account's
// identifier (with the routing number, for a bank), never the identifier
// itself, which for a bank is the account number. The key is the finance
// source's own, so the same account in two files is the same finance
// account, and the stored id says nothing to anybody who reads the table
// without the key.
//
// The institution is not part of it. The FI block that names it is
// optional, so one export can carry FID and the next only ORG, and keying
// on whichever was there split one account in two and counted every
// transaction twice. Two cards at two institutions sharing an identifier
// is the rarer case, and a card's identifier is its number or an opaque
// one the issuer made.
func StatementAccountID(accountKey []byte, statement *ofx.Statement) string {
	return statementAccountHash(accountKey, string(statement.StatementKind), strings.TrimSpace(statement.BankID), strings.TrimSpace(statement.AccountID))
}

// legacyStatementAccountID is the provider account id the first version
// kept an account under, with the institution as written in the file,
// FID or else ORG, lower case.
func legacyStatementAccountID(accountKey []byte, institution string, statement *ofx.Statement) string {
	return statementAccountHash(accountKey, string(statement.StatementKind), strings.ToLower(strings.TrimSpace(institution)), strings.TrimSpace(statement.BankID), strings.TrimSpace(statement.AccountID))
}

func statementAccountHash(accountKey []byte, parts ...string) string {
	mac := hmac.New(sha256.New, accountKey)
	for _, part := range parts {
		mac.Write([]byte(part))
		mac.Write([]byte{0})
	}
	return "ofx-" + hex.EncodeToString(mac.Sum(nil))[:32]
}

// resolveStatementAccountID is the provider account id to import a
// statement under, and the institution it is keyed with when that is an
// account made the earlier way. An account already under the current id
// is that; otherwise an account made with the institution in its key is
// found by keying this statement's identifier with the institution the
// account's metadata names, and keeps its id, so an account made before
// the change is not split from the one made after it.
func resolveStatementAccountID(accountKey []byte, statement *ofx.Statement, existingAccounts []ExistingStatementAccount) (string, string) {
	providerAccountId := StatementAccountID(accountKey, statement)
	for _, existing := range existingAccounts {
		if existing.ProviderAccountID == providerAccountId {
			return providerAccountId, ""
		}
	}
	for _, existing := range existingAccounts {
		institution := statementAccountKeyInstitution(existing.ProviderMetadata)
		if institution != "" && legacyStatementAccountID(accountKey, institution, statement) == existing.ProviderAccountID {
			return existing.ProviderAccountID, institution
		}
	}
	return providerAccountId, ""
}

// statementAccountKeyInstitution is the institution an account made the
// earlier way was keyed with: the field that records it once the account
// has been imported into since, or else the FID, or else the ORG, its
// metadata kept from the file it was made from.
func statementAccountKeyInstitution(providerMetadata json.RawMessage) string {
	var metadata struct {
		StatementAccountKeyInstitution string `json:"statementAccountKeyInstitution"`
		InstitutionID                  string `json:"institutionId"`
		InstitutionOrganization        string `json:"institutionOrganization"`
	}
	if len(providerMetadata) == 0 || json.Unmarshal(providerMetadata, &metadata) != nil {
		return ""
	}
	for _, institution := range []string{metadata.StatementAccountKeyInstitution, metadata.InstitutionID, metadata.InstitutionOrganization} {
		if institution = strings.ToLower(strings.TrimSpace(institution)); institution != "" {
			return institution
		}
	}
	return ""
}

// StatementAccountMask is the end of an account's identifier, its letters
// and digits only, kept to tell accounts apart; the rest is never stored.
// Some identifiers are numbers and some are opaque strings with dashes in
// them, and a mask that ends in a dash tells nobody anything.
func StatementAccountMask(accountId string) string {
	runes := []rune{}
	for _, character := range accountId {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			runes = append(runes, character)
		}
	}
	if len(runes) <= statementAccountMaskLength {
		return string(runes)
	}
	return string(runes[len(runes)-statementAccountMaskLength:])
}

// NewStatementImport turns one statement of a document into an account and
// its transactions in this package's conventions. existingAccounts are the
// accounts the statement source holds already, which the statement's
// account is matched against.
//
// The signs need nothing done to them. OFX signs an amount from the
// account holder's side, as this program does: a card purchase is
// negative, a payment to the card and a refund positive, a bank
// withdrawal negative. A card's ledger balance is negative for what is
// owed, which is how most institutions behind SimpleFIN report it, so the
// account says the owed balance is not positive and the valuation turns it
// into the amount owed.
func NewStatementImport(accountKey []byte, document *ofx.Document, statement *ofx.Statement, existingAccounts []ExistingStatementAccount) (*StatementImport, error) {
	if len(accountKey) == 0 {
		return nil, errors.New("finance: a statement needs its finance source's account key")
	}
	providerAccountId, keyInstitution := resolveStatementAccountID(accountKey, statement, existingAccounts)
	currencyCode := strings.ToUpper(strings.TrimSpace(statement.CurrencyCode))
	if currencyCode == "" {
		return nil, errors.New("finance: the statement does not say its currency (CURDEF)")
	}
	accountKind := AccountKindDepository
	primaryCategory := StatementCategoryPrimaryBankAccount
	switch {
	case statement.StatementKind == ofx.StatementKindCreditCard:
		accountKind = AccountKindCredit
		primaryCategory = StatementCategoryPrimaryCreditCard
	case statement.AccountType == "CREDITLINE":
		accountKind = AccountKindCredit
	}
	accountMask := StatementAccountMask(statement.AccountID)
	accountName := strings.TrimSpace(document.InstitutionOrganization)
	if accountName == "" {
		accountName = "Imported account"
		if accountKind == AccountKindCredit {
			accountName = "Imported card"
		}
	}

	// What the file said about the account, less its identifier: the rest
	// is kept as every provider's whole object is, so a field nobody
	// mapped can be dealt with later from stored data.
	accountMetadata := map[string]any{
		"statementKind": string(statement.StatementKind), "currencyCode": currencyCode, "accountMask": accountMask,
		"institutionOrganization": document.InstitutionOrganization, "institutionId": document.InstitutionID,
		"accountType": statement.AccountType, "startedOn": statement.StartedOn, "endedOn": statement.EndedOn,
	}
	if keyInstitution != "" {
		// Kept so the next file finds the account again, whatever its FI
		// block says.
		accountMetadata[statementAccountKeyInstitutionField] = keyInstitution
	}
	account := Account{
		ProviderAccountID: providerAccountId, AccountName: accountName, AccountMask: accountMask,
		AccountKind: accountKind, CurrencyCode: currencyCode,
	}
	statementImport := &StatementImport{}
	if statement.LedgerBalance != nil {
		account.CurrentBalance = statement.LedgerBalance.Amount
		account.BalanceAt = statement.LedgerBalance.AsOf
		statementImport.BalanceOn = statement.LedgerBalance.AsOfDay
		accountMetadata["ledgerBalance"] = statement.LedgerBalance.Amount
		accountMetadata["ledgerBalanceOn"] = statement.LedgerBalance.AsOfDay
		if statementImport.BalanceOn == "" {
			statementImport.BalanceOn = statement.EndedOn
		}
	}
	if statement.AvailableBalance != nil {
		account.AvailableBalance = statement.AvailableBalance.Amount
		accountMetadata["availableBalance"] = statement.AvailableBalance.Amount
		accountMetadata["availableBalanceOn"] = statement.AvailableBalance.AsOfDay
	}
	if account.BalanceAt.IsZero() && statementImport.BalanceOn != "" {
		if balanceDay, err := time.Parse(time.DateOnly, statementImport.BalanceOn); err == nil {
			account.BalanceAt = balanceDay
		}
	}
	encodedAccount, err := json.Marshal(accountMetadata)
	if err != nil {
		return nil, err
	}
	account.ProviderMetadata = encodedAccount

	syncResult := &SyncResult{Accounts: []Account{account}, InstitutionName: document.InstitutionOrganization}
	// Counted per what an identifier is made from, so two transactions
	// that say the same thing on the same day stay two.
	generatedOccurrences := map[string]int{}
	for _, entry := range statement.Transactions {
		amount, err := CanonicalAmount(entry.Amount)
		if err != nil {
			return nil, err
		}
		transactionCurrencyCode := currencyCode
		if entry.CurrencyCode != "" {
			transactionCurrencyCode = entry.CurrencyCode
		}
		description := strings.TrimSpace(entry.Name)
		if description == "" {
			description = strings.TrimSpace(entry.Memo)
		}
		providerTransactionId := strings.TrimSpace(entry.FITID)
		if providerTransactionId == "" {
			// A file without FITIDs gives nothing stable to know a
			// transaction by across imports, so one is made from what it
			// says: its day, amount and name. Identical transactions on one
			// day are numbered in the order the file lists them, so a
			// genuine second charge is kept, and importing the same file
			// again, or a later one listing the same day the same way,
			// finds the same identifiers. A file that lists that day
			// differently can count one of them twice; that is the cost of
			// an institution that does not say which transaction is which.
			basis := entry.PostedOn + "\x00" + amount + "\x00" + strings.ToLower(description)
			generatedOccurrences[basis]++
			digest := sha256.Sum256([]byte(basis))
			providerTransactionId = "generated-" + hex.EncodeToString(digest[:])[:24] + "-" + strconv.Itoa(generatedOccurrences[basis])
			statementImport.GeneratedIDCount++
		}
		transactionMetadata := map[string]any{
			"transactionType": entry.TransactionType, "name": entry.Name, "memo": entry.Memo, "payeeName": entry.PayeeName,
			"fitId": entry.FITID, "postedOn": entry.PostedOn,
		}
		// The amount is in transactionCurrencyCode; the original currency
		// only says what the purchase was made in, and is kept for the
		// record.
		for key, value := range map[string]string{
			"currencyRate": entry.CurrencyRate, "originalCurrencyCode": entry.OriginalCurrencyCode, "originalCurrencyRate": entry.OriginalCurrencyRate,
		} {
			if value != "" {
				transactionMetadata[key] = value
			}
		}
		encodedTransaction, err := json.Marshal(transactionMetadata)
		if err != nil {
			return nil, err
		}
		transaction := Transaction{
			ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: entry.PostedOn,
			Amount: amount, CurrencyCode: transactionCurrencyCode, Description: description, MerchantName: strings.TrimSpace(entry.PayeeName),
			ProviderCategoryPrimary: primaryCategory, ProviderMetadata: encodedTransaction,
		}
		if entry.TransactionType != "" {
			transaction.ProviderCategoryDetailed = StatementCategoryPrefix + entry.TransactionType
		}
		if entry.HasPostedTime {
			postedAt := entry.PostedAt
			transaction.TransactedAt = &postedAt
		}
		if statementImport.FirstPostedOn == "" || entry.PostedOn < statementImport.FirstPostedOn {
			statementImport.FirstPostedOn = entry.PostedOn
		}
		syncResult.Added = append(syncResult.Added, transaction)
	}
	statementImport.SyncResult = syncResult
	return statementImport, nil
}
