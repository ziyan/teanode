package models

import "time"

// AgentBilledPrice is one price the daily check set beside the provider's
// bill: what the configuration says for a model and kind of token, and what
// the bill implies.
type AgentBilledPrice struct {
	Model     string `json:"model"`
	TokenKind string `json:"tokenKind"`

	// ConfiguredPerMillion and BilledPerMillion are in the configured
	// currency per million tokens.
	ConfiguredPerMillion float64 `json:"configuredPerMillion"`
	BilledPerMillion     float64 `json:"billedPerMillion"`

	// DayCount is how many days of the bill the billed price rests on.
	DayCount int `json:"dayCount"`

	// IsPattern says the configured price comes from an entry whose model
	// is a pattern covering other models too. Such an entry is reported
	// and never changed.
	IsPattern bool `json:"isPattern"`
}

// AgentPriceChange is one price the check set from the bill.
type AgentPriceChange struct {
	Model     string `json:"model"`
	TokenKind string `json:"tokenKind"`

	BeforePerMillion float64 `json:"beforePerMillion"`
	AfterPerMillion  float64 `json:"afterPerMillion"`
}

// AgentBillingCheck is what the last check of one provider's prices against
// its bill did.
type AgentBillingCheck struct {
	ProviderName string    `json:"providerName"`
	CheckedAt    time.Time `json:"checkedAt"`

	// ErrorMessage is why the bill could not be read, such as a refused
	// key. Empty when it was read.
	ErrorMessage string `json:"errorMessage"`

	Compared []AgentBilledPrice `json:"compared"`
	Changed  []AgentPriceChange `json:"changed"`
}

// AgentBilledDay is what a provider billed for one model on one day (UTC).
// Model is TeaNode's name for it, without a snapshot date.
type AgentBilledDay struct {
	ProviderName string    `json:"providerName"`
	BilledOn     time.Time `json:"billedOn"`
	Model        string    `json:"model"`

	// BilledAmount is in CurrencyCode and is not rounded to cents.
	BilledAmount float64 `json:"billedAmount"`
	CurrencyCode string  `json:"currencyCode"`
}
