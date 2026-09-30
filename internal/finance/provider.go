// Package finance talks to the providers that sign in to a person's
// financial institutions, and to the European Central Bank for exchange
// rates. It talks to nothing else: it holds no database handle and reads
// no settings, so everything in it can be tested against an httptest
// server alone, and the agent's reader decides what to keep.
//
// Two conventions hold for everything this package returns. An amount is
// a decimal string, never a float, with negative meaning money leaving the
// account whichever way the provider signs it. And every account and
// transaction carries the provider's whole object as it arrived, because
// a provider sends more than the normalized fields hold and SimpleFIN
// keeps only about ninety days: what is not kept at sync time is gone.
package finance

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/version"
)

var log = logging.MustGetLogger("finance") //nolint:unused

// ProviderKind names the outside service a finance source signs in
// through.
type ProviderKind string

const (
	ProviderKindPlaid     ProviderKind = "plaid"
	ProviderKindSimpleFIN ProviderKind = "simplefin"
)

// Account kinds. Plaid's own vocabulary, which is the richer of the two
// providers'; SimpleFIN has none and its accounts are mostly "other".
const (
	AccountKindDepository = "depository"
	AccountKindCredit     = "credit"
	AccountKindLoan       = "loan"
	AccountKindInvestment = "investment"
	AccountKindOther      = "other"
)

// Account is one finance account as a provider reported it.
//
// Balances are as the provider reports them. For Plaid a card's or loan's
// balance is the amount owed, positive; SimpleFIN leaves the sign to the
// institution, and they report what is owed as negative.
// IsOwedBalancePositive says which of the two the provider does, so the
// amount owed on a liability can be told from a credit in the person's
// favour, such as a refund left on a card.
type Account struct {
	ProviderAccountID     string
	AccountName           string
	AccountMask           string // last digits, when the provider gives them
	AccountKind           string // depository, credit, loan, investment, other
	CurrencyCode          string
	CurrentBalance        string // decimal, "" when unknown
	AvailableBalance      string // decimal, "" when unknown
	IsOwedBalancePositive bool
	BalanceAt             time.Time
	ProviderMetadata      json.RawMessage
}

// Transaction is one finance transaction as a provider reported it.
type Transaction struct {
	ProviderTransactionID        string
	ProviderAccountID            string
	PostedOn                     string // "2006-01-02"
	TransactedAt                 *time.Time
	Amount                       string // decimal, negative is money out
	CurrencyCode                 string
	Description                  string
	MerchantName                 string
	ProviderCategoryPrimary      string
	ProviderCategoryDetailed     string
	IsPending                    bool
	PendingProviderTransactionID string
	ProviderMetadata             json.RawMessage
}

// SyncResult is one sync's worth of change. RemovedProviderTransactionIDs
// are deleted. PendingReplacedFrom, when set, means every stored pending
// transaction of these accounts posted on or after it that is not in
// Added is gone (SimpleFIN's way of reporting removal).
type SyncResult struct {
	Accounts                      []Account
	Added                         []Transaction // upserted by provider transaction id
	RemovedProviderTransactionIDs []string
	PendingReplacedFrom           *time.Time
	NextCursor                    string
	InstitutionName               string
	ProviderWarnings              []string
}

// CredentialDescription is what a provider says about a credential a
// person brings from elsewhere, asked before a finance source is made for
// it: the answer proves the credential works, and says what to call it.
type CredentialDescription struct {
	// ProviderReference is Plaid's id for the link; empty for SimpleFIN.
	ProviderReference string
	InstitutionID     string
	InstitutionName   string
}

// Provider is what the agent's reader needs of a provider once a finance
// source exists. Linking differs too much between the two to share an
// interface: Plaid's goes through a browser widget and a token exchange,
// SimpleFIN's through a token the person pastes.
type Provider interface {
	Kind() ProviderKind
	Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error)
	Remove(ctx context.Context, credential string) error
}

// ErrSignInRequired is the institution refusing the stored sign-in until
// the person signs in again. Retrying does not help, so the reader stops
// calling the provider until the finance source is repaired.
var ErrSignInRequired = errors.New("the institution needs the person to sign in again")

// ErrCredentialRefused is a provider refusing the credential itself: for
// SimpleFIN, the person revoked access on the bridge's website or the
// bridge ended it; for Plaid, the link is gone or the person withdrew
// their consent. Unlike a sign-in, it cannot be repaired: the person
// deletes the finance source and links the institution again, and until
// then the reader does not call the provider.
var ErrCredentialRefused = errors.New("the provider refused the credential; access may have been revoked")

// UserAgent is what every request this package makes says it is. The
// SimpleFIN bridge refuses a request with a generic library's user agent,
// the one-time claim included, and naming the version lets a provider tell
// an old build from a new one when something goes wrong.
func UserAgent() string {
	return "teanode/" + version.Version()
}
