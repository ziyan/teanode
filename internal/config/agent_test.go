package config

import "testing"

// A model is priced by its own entry where it has one, by a pattern that
// covers it next, and by its provider's prices otherwise.
func TestPricingPerModel(t *testing.T) {
	agent := &Agent{Providers: []AgentProvider{{
		Name:    "openai",
		Pricing: AgentPricing{Input: 1, Output: 2, CacheRead: 0.5},
		ModelPricing: []AgentModelPricing{
			{Model: "gpt-5.6-terra", Input: 10, Output: 40, CacheRead: 5},
			{Model: "gpt-5*", Input: 3, Output: 9, CacheRead: 1},
		},
	}}}
	// A million of each, so the numbers are the prices themselves.
	if cost := agent.CostOf("openai:gpt-5.6-terra", 1_000_000, 0, 0); cost != 10 {
		t.Fatalf("the model's own entry: %v", cost)
	}
	if cost := agent.CostOf("openai:gpt-5.6-luna", 0, 1_000_000, 0); cost != 9 {
		t.Fatalf("the pattern that covers it: %v", cost)
	}
	if cost := agent.CostOf("openai:whisper", 0, 0, 1_000_000); cost != 0.5 {
		t.Fatalf("the provider's own prices: %v", cost)
	}
	if cost := agent.CostOf("elsewhere:thinker", 1_000_000, 0, 0); cost != 0 {
		t.Fatalf("a provider that is not configured prices nothing: %v", cost)
	}
	// The first entry that matches wins, so order is the operator's.
	agent.Providers[0].ModelPricing = []AgentModelPricing{{Model: "gpt-5*", Input: 3}, {Model: "gpt-5.6-terra", Input: 10}}
	if cost := agent.CostOf("openai:gpt-5.6-terra", 1_000_000, 0, 0); cost != 3 {
		t.Fatalf("the first match wins: %v", cost)
	}
}
