package client

import (
	"context"
	"time"
)

// What the daily check of a provider's prices against its bill did.

// AgentBilledPrice is one price the check set beside the bill.
type AgentBilledPrice struct {
	Model                string  `json:"model"`
	TokenKind            string  `json:"tokenKind"`
	ConfiguredPerMillion float64 `json:"configuredPerMillion"`
	BilledPerMillion     float64 `json:"billedPerMillion"`
	DayCount             int     `json:"dayCount"`
	IsPattern            bool    `json:"isPattern"`
}

// AgentPriceChange is one price the check changed.
type AgentPriceChange struct {
	Model            string  `json:"model"`
	TokenKind        string  `json:"tokenKind"`
	BeforePerMillion float64 `json:"beforePerMillion"`
	AfterPerMillion  float64 `json:"afterPerMillion"`
}

// AgentBillingCheck is the last check of one provider.
type AgentBillingCheck struct {
	ProviderName string              `json:"providerName"`
	CheckedAt    time.Time           `json:"checkedAt"`
	ErrorMessage string              `json:"errorMessage"`
	Compared     []*AgentBilledPrice `json:"compared"`
	Changed      []*AgentPriceChange `json:"changed"`
}

const (
	agentBillingCheckFields = `providerName checkedAt errorMessage
    compared { model tokenKind configuredPerMillion billedPerMillion dayCount isPattern }
    changed { model tokenKind beforePerMillion afterPerMillion }`

	DocumentAgentBillingChecks = `query { AgentBillingChecks { ` + agentBillingCheckFields + ` } }`

	DocumentCheckAgentProviderPrices = `mutation { CheckAgentProviderPrices { ` + agentBillingCheckFields + ` } }`
)

// AgentBillingChecks is what the last check of each provider with a
// billing key did.
func AgentBillingChecks(ctx context.Context, connection *Client) ([]*AgentBillingCheck, error) {
	var result struct {
		AgentBillingChecks []*AgentBillingCheck `json:"AgentBillingChecks"`
	}
	if err := connection.Execute(ctx, DocumentAgentBillingChecks, nil, &result); err != nil {
		return nil, err
	}
	return result.AgentBillingChecks, nil
}

// CheckAgentProviderPrices checks the prices against the bill now.
func CheckAgentProviderPrices(ctx context.Context, connection *Client) ([]*AgentBillingCheck, error) {
	var result struct {
		CheckAgentProviderPrices []*AgentBillingCheck `json:"CheckAgentProviderPrices"`
	}
	if err := connection.Execute(ctx, DocumentCheckAgentProviderPrices, nil, &result); err != nil {
		return nil, err
	}
	return result.CheckAgentProviderPrices, nil
}
