package client

import "context"

// The models a provider offers, one entry at a time: the same operations as
// the provider page's "Add model" and a model's own page.

// AgentPricing is what a model costs per million tokens.
type AgentPricing struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	AudioInput float64 `json:"audioInput"`
}

// AgentModelEntry is one model of a provider.
type AgentModelEntry struct {
	// Model is the id the service knows it by.
	Model string `json:"model"`
	// Name is what people read; empty reads as the id.
	Name string `json:"name"`
	// ContextWindow is the override of the provider's window, zero for none.
	ContextWindow int `json:"contextWindow"`
	// ReportedContextWindow is the provider's own figure, zero when unknown.
	ReportedContextWindow int `json:"reportedContextWindow"`
	// Pricing is nil when the entry takes the provider's prices; a value,
	// even all zeros, is the model's own.
	Pricing   *AgentPricing `json:"pricing"`
	IsEnabled bool          `json:"isEnabled"`
	// UsedFor is the roles that point at it.
	UsedFor []string `json:"usedFor"`
}

// AgentProviderModels is one provider and its models, as the settings list
// them.
type AgentProviderModels struct {
	ProviderName string             `json:"name"`
	Models       []*AgentModelEntry `json:"models"`
}

const (
	agentModelEntryFields = `model name contextWindow reportedContextWindow isEnabled usedFor pricing { input output cacheRead cacheWrite audioInput }`

	DocumentAgentProviderModels = `query { GetSettings { agent { providers { name models { ` + agentModelEntryFields + ` } } } } }`

	DocumentSaveAgentModelEntry = `mutation ($providerName: String!, $entry: AgentModelEntryInput!) {
		SaveAgentModelEntry(providerName: $providerName, entry: $entry) { ` + agentModelEntryFields + ` }
	}`

	DocumentDeleteAgentModelEntry = `mutation ($providerName: String!, $model: String!) { DeleteAgentModelEntry(providerName: $providerName, model: $model) }`
)

// AgentProviderModelList is every provider with the models it offers.
func AgentProviderModelList(ctx context.Context, connection *Client) ([]*AgentProviderModels, error) {
	var result struct {
		GetSettings struct {
			Agent struct {
				Providers []*AgentProviderModels `json:"providers"`
			} `json:"agent"`
		} `json:"GetSettings"`
	}
	if err := connection.Execute(ctx, DocumentAgentProviderModels, nil, &result); err != nil {
		return nil, err
	}
	return result.GetSettings.Agent.Providers, nil
}

// SaveAgentModelEntry adds a model to a provider, or changes the one with
// the same id, and answers with it as it is now.
func SaveAgentModelEntry(ctx context.Context, connection *Client, providerName string, entry *AgentModelEntry) (*AgentModelEntry, error) {
	input := map[string]any{
		"model": entry.Model, "name": entry.Name, "contextWindow": entry.ContextWindow, "isEnabled": entry.IsEnabled,
		"pricing": nil,
	}
	if entry.Pricing != nil {
		input["pricing"] = map[string]any{
			"input": entry.Pricing.Input, "output": entry.Pricing.Output, "cacheRead": entry.Pricing.CacheRead,
			"cacheWrite": entry.Pricing.CacheWrite, "audioInput": entry.Pricing.AudioInput,
		}
	}
	var result struct {
		SaveAgentModelEntry *AgentModelEntry `json:"SaveAgentModelEntry"`
	}
	if err := connection.Execute(ctx, DocumentSaveAgentModelEntry, map[string]any{"providerName": providerName, "entry": input}, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentModelEntry, nil
}

// DeleteAgentModelEntry removes a model from a provider; the server refuses
// while a role uses it, and names the role.
func DeleteAgentModelEntry(ctx context.Context, connection *Client, providerName, model string) error {
	var result struct {
		DeleteAgentModelEntry bool `json:"DeleteAgentModelEntry"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentModelEntry, map[string]any{"providerName": providerName, "model": model}, &result)
}
