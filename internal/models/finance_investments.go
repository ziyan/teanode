package models

import (
	"encoding/json"
	"time"
)

// SecurityKind is what a security is: a share, a fund, a bond, a coin,
// cash. Plaid's vocabulary, written with underscores.
type SecurityKind string

// The nine kinds; anything a provider sends outside them is other.
const (
	SecurityKindCash           SecurityKind = "cash"
	SecurityKindCryptocurrency SecurityKind = "cryptocurrency"
	SecurityKindDerivative     SecurityKind = "derivative"
	SecurityKindEquity         SecurityKind = "equity"
	SecurityKindETF            SecurityKind = "etf"
	SecurityKindFixedIncome    SecurityKind = "fixed_income"
	SecurityKindLoan           SecurityKind = "loan"
	SecurityKindMutualFund     SecurityKind = "mutual_fund"
	SecurityKindOther          SecurityKind = "other"
)

// IsValid says the kind is one of the nine.
func (self SecurityKind) IsValid() bool {
	switch self {
	case SecurityKindCash, SecurityKindCryptocurrency, SecurityKindDerivative, SecurityKindEquity, SecurityKindETF,
		SecurityKindFixedIncome, SecurityKindLoan, SecurityKindMutualFund, SecurityKindOther:
		return true
	}
	return false
}

// FinanceSecurity is something an investment account can hold, as the
// provider that reported it describes it. One per agent per provider's
// security; it outlives the finance source that brought it.
type FinanceSecurity struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// ProviderKind is the provider that reported it, and
	// ProviderSecurityID the provider's id for it.
	ProviderKind       string `json:"providerKind"`
	ProviderSecurityID string `json:"providerSecurityId"`

	TickerSymbol string       `json:"tickerSymbol,omitempty" graphapi:"nullable"`
	SecurityName string       `json:"securityName"`
	SecurityKind SecurityKind `json:"securityKind"`
	CurrencyCode string       `json:"currencyCode,omitempty" graphapi:"nullable"`

	// ClosePrice is the last close the provider reported, a decimal, and
	// ClosePriceOn its day, "2006-01-02"; both empty when it gave none.
	ClosePrice   string `json:"closePrice,omitempty" graphapi:"nullable"`
	ClosePriceOn string `json:"closePriceOn,omitempty" graphapi:"nullable"`

	// ProviderMetadata is the provider's whole object for the security, as
	// it last arrived.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// TradeKind is what a trade was.
type TradeKind string

// A buy or a sell, a trade the institution cancelled, and a security moved
// into or out of the account (from another brokerage, or by a split or a
// merger).
const (
	TradeKindBuy      TradeKind = "buy"
	TradeKindSell     TradeKind = "sell"
	TradeKindCancel   TradeKind = "cancel"
	TradeKindTransfer TradeKind = "transfer"
)

// IsValid says the kind is one of the four.
func (self TradeKind) IsValid() bool {
	switch self {
	case TradeKindBuy, TradeKindSell, TradeKindCancel, TradeKindTransfer:
		return true
	}
	return false
}

// FinanceTrade is one trade in an investment account: cash swapped for a
// security or back inside the account. It is never spending or income,
// and it is not a finance transaction.
type FinanceTrade struct {
	ID               string `json:"id"`
	AgentID          string `json:"agentId"`
	FinanceAccountID string `json:"financeAccountId"`

	// FinanceSecurityID is the security traded, empty when the provider
	// named none, and FinanceSecurity the security itself when the trade
	// was read with it.
	FinanceSecurityID string           `json:"financeSecurityId,omitempty" graphapi:"nullable"`
	FinanceSecurity   *FinanceSecurity `json:"financeSecurity,omitempty" graphapi:"nullable"`

	// ProviderTradeID is the provider's id for it, unique within its
	// finance account.
	ProviderTradeID string `json:"providerTradeId"`

	// TradedOn is the day, "2006-01-02".
	TradedOn string `json:"tradedOn"`

	// TradeKind is what it was, and TradeSubkind the provider's finer word
	// for it, as the provider wrote it.
	TradeKind    TradeKind `json:"tradeKind"`
	TradeSubkind string    `json:"tradeSubkind,omitempty" graphapi:"nullable"`

	// TradedQuantity is how much of the security it moved, and UnitPrice
	// the price of one unit, decimals; empty when the provider gave none.
	TradedQuantity string `json:"tradedQuantity,omitempty" graphapi:"nullable"`
	UnitPrice      string `json:"unitPrice,omitempty" graphapi:"nullable"`

	// TradeAmount is the cash it moved in the account, a decimal, negative
	// when cash left it (a buy); FeeAmount what it cost in fees.
	TradeAmount  string `json:"tradeAmount"`
	FeeAmount    string `json:"feeAmount,omitempty" graphapi:"nullable"`
	CurrencyCode string `json:"currencyCode"`

	// Description is the institution's, as the provider passed it on.
	Description string `json:"description"`

	// ProviderMetadata is the provider's whole object for the trade, as it
	// arrived.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}
