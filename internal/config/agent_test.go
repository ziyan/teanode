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
	if cost := agent.CostOf("openai:gpt-5.6-terra", 1_000_000, 0, 0, 0); cost != 10 {
		t.Fatalf("the model's own entry: %v", cost)
	}
	if cost := agent.CostOf("openai:gpt-5.6-luna", 0, 1_000_000, 0, 0); cost != 9 {
		t.Fatalf("the pattern that covers it: %v", cost)
	}
	if cost := agent.CostOf("openai:whisper", 0, 0, 1_000_000, 0); cost != 0.5 {
		t.Fatalf("the provider's own prices: %v", cost)
	}
	if cost := agent.CostOf("elsewhere:thinker", 1_000_000, 0, 0, 0); cost != 0 {
		t.Fatalf("a provider that is not configured prices nothing: %v", cost)
	}
	// The first entry that matches wins, so order is the operator's.
	agent.Providers[0].ModelPricing = []AgentModelPricing{{Model: "gpt-5*", Input: 3}, {Model: "gpt-5.6-terra", Input: 10}}
	if cost := agent.CostOf("openai:gpt-5.6-terra", 1_000_000, 0, 0, 0); cost != 3 {
		t.Fatalf("the first match wins: %v", cost)
	}
}

// A model name written with a slash is matched by a plain star: it is a
// name, not a path.
func TestPricingMatchesNamesWithSlashes(t *testing.T) {
	agent := &Agent{Providers: []AgentProvider{{
		Name: "router",
		ModelPricing: []AgentModelPricing{
			{Model: "anthropic/claude-*", Input: 3},
			{Model: "*", Input: 1},
		},
	}}}
	if cost := agent.CostOf("router:anthropic/claude-sonnet-4.5", 1_000_000, 0, 0, 0); cost != 3 {
		t.Fatalf("a family under a vendor: %v", cost)
	}
	if cost := agent.CostOf("router:meta/llama-4", 1_000_000, 0, 0, 0); cost != 1 {
		t.Fatalf("a star covers everything, slashes and all: %v", cost)
	}
	if cost := agent.CostOf("router:plain-model", 1_000_000, 0, 0, 0); cost != 1 {
		t.Fatalf("and names without a slash: %v", cost)
	}
	// A question mark is one character, and a pattern that matches
	// nothing still matches nothing.
	narrow := &Agent{Providers: []AgentProvider{{Name: "one", ModelPricing: []AgentModelPricing{{Model: "gpt-?", Input: 5}}}}}
	if cost := narrow.CostOf("one:gpt-5", 1_000_000, 0, 0, 0); cost != 5 {
		t.Fatalf("one character: %v", cost)
	}
	if cost := narrow.CostOf("one:gpt-50", 1_000_000, 0, 0, 0); cost != 0 {
		t.Fatalf("not two: %v", cost)
	}
}

// What a call costs includes writing to the cache, which some services
// bill above the input price and report apart from it.
func TestCostIncludesCacheWrites(t *testing.T) {
	agent := &Agent{Providers: []AgentProvider{{Name: "one", Pricing: AgentPricing{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25}}}}
	if cost := agent.CostOf("one:thinker", 0, 0, 0, 1_000_000); cost != 1.25 {
		t.Fatalf("a million tokens written to the cache: %v", cost)
	}
	if cost := agent.CostOf("one:thinker", 1_000_000, 1_000_000, 1_000_000, 1_000_000); cost != 4.35 {
		t.Fatalf("all four priced apart: %v", cost)
	}
}
