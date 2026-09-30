package models

import "time"

// AssetKind is what sort of thing an asset is. The last four are owed
// rather than owned, and an asset of one of them subtracts from net worth.
type AssetKind string

// The kinds of asset.
const (
	AssetKindCash           AssetKind = "cash"
	AssetKindInvestment     AssetKind = "investment"
	AssetKindRetirement     AssetKind = "retirement"
	AssetKindProperty       AssetKind = "property"
	AssetKindVehicle        AssetKind = "vehicle"
	AssetKindOtherAsset     AssetKind = "other_asset"
	AssetKindCreditCard     AssetKind = "credit_card"
	AssetKindLoan           AssetKind = "loan"
	AssetKindMortgage       AssetKind = "mortgage"
	AssetKindOtherLiability AssetKind = "other_liability"
)

// IsValid says the kind is one of the ten.
func (self AssetKind) IsValid() bool {
	switch self {
	case AssetKindCash, AssetKindInvestment, AssetKindRetirement, AssetKindProperty, AssetKindVehicle,
		AssetKindOtherAsset, AssetKindCreditCard, AssetKindLoan, AssetKindMortgage, AssetKindOtherLiability:
		return true
	}
	return false
}

// IsLiability says an asset of this kind is owed, and subtracts.
func (self AssetKind) IsLiability() bool {
	switch self {
	case AssetKindCreditCard, AssetKindLoan, AssetKindMortgage, AssetKindOtherLiability:
		return true
	}
	return false
}

// ValuationSource is where a valuation came from. An asset's own is the
// one its values normally come from; a valuation's is the one it did.
type ValuationSource string

// The four valuation sources: a finance account's balance at a sync, the
// agent reading an account through a connected server, the person, and
// the agent estimating from the web where the person allowed it.
const (
	ValuationSourceFinanceSync   ValuationSource = "finance_sync"
	ValuationSourceAgentReading  ValuationSource = "agent_reading"
	ValuationSourceManual        ValuationSource = "manual"
	ValuationSourceAgentEstimate ValuationSource = "agent_estimate"
)

// IsValid says the source is one of the four.
func (self ValuationSource) IsValid() bool {
	switch self {
	case ValuationSourceFinanceSync, ValuationSourceAgentReading, ValuationSourceManual, ValuationSourceAgentEstimate:
		return true
	}
	return false
}

// Asset is anything that counts toward net worth, what is owed included.
type Asset struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	AssetName string    `json:"assetName"`
	AssetKind AssetKind `json:"assetKind"`

	// IsLiability is set from the kind: a liability's values subtract.
	IsLiability  bool   `json:"isLiability"`
	CurrencyCode string `json:"currencyCode"`

	// FinanceAccountID is the finance account whose balance values it,
	// empty for anything else.
	FinanceAccountID string `json:"financeAccountId,omitempty" graphapi:"nullable"`

	ValuationSource ValuationSource `json:"valuationSource"`

	// EstimateDescription is what the person allowed the agent to search
	// the web with, and IsEstimateAllowed whether they allowed it.
	EstimateDescription string `json:"estimateDescription,omitempty" graphapi:"nullable"`
	IsEstimateAllowed   bool   `json:"isEstimateAllowed"`

	// ClosedOn is the day it was sold or paid off, "2006-01-02": it counts
	// until that day and not after. Empty while open.
	ClosedOn string `json:"closedOn,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// LatestValuation is the valuation that counts today, when the asset
	// was read with it; nil otherwise and when it has none.
	LatestValuation *AssetValuation `json:"latestValuation,omitempty" graphapi:"nullable"`
}

// AssetValuation is one value of one asset on one day.
type AssetValuation struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	AssetID string `json:"assetId"`

	// ValuedOn is the day, "2006-01-02".
	ValuedOn string `json:"valuedOn"`

	// Value is a decimal, the size of the thing; the asset's IsLiability
	// gives the sign. Negative only for an account that is overdrawn.
	Value           string          `json:"value"`
	CurrencyCode    string          `json:"currencyCode"`
	ValuationSource ValuationSource `json:"valuationSource"`

	// An estimate's range, what it rests on, and the pages it read.
	EstimateLow   string   `json:"estimateLow,omitempty" graphapi:"nullable"`
	EstimateHigh  string   `json:"estimateHigh,omitempty" graphapi:"nullable"`
	ValuationNote string   `json:"valuationNote,omitempty" graphapi:"nullable"`
	EvidenceURLs  []string `json:"evidenceUrls"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// NetWorthPoint is net worth in one currency on one day: the winning
// valuations of the assets open that day, liabilities subtracted.
// Currencies are never added together here; converting is the caller's.
type NetWorthPoint struct {
	// NetWorthOn is the day, "2006-01-02".
	NetWorthOn     string `json:"netWorthOn"`
	CurrencyCode   string `json:"currencyCode"`
	NetWorthAmount string `json:"netWorthAmount"`
}
