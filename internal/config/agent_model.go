package config

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentModelEntry is one model a provider offers.
type AgentModelEntry struct {
	// Model is the id the service knows it by: gpt-6.1-sol.
	Model string `yaml:"model"`

	// Name is what people read: Sol. Empty reads as the id.
	Name string `yaml:"name,omitempty"`

	// ContextWindow corrects the window the provider reports, in tokens;
	// zero takes the provider's figure (and the unknown default when it
	// reports none).
	ContextWindow int `yaml:"contextWindow,omitempty"`

	// Pricing is what it costs per million tokens. Nil takes the provider's
	// price; a value, even all zeros, is the model's own (all zeros is free).
	Pricing *AgentPricing `yaml:"pricing,omitempty"`

	// Enabled keeps the entry while switching it off. Unset means on.
	Enabled *bool `yaml:"enabled,omitempty"`
}

// MinimumContextWindow is the smallest window an entry may name: below it
// the figure is a mistake (a count of thousands typed as tokens), not a
// model.
const MinimumContextWindow = 1000

// IsEnabled resolves the unset Enabled to on.
func (self *AgentModelEntry) IsEnabled() bool {
	return self.Enabled == nil || *self.Enabled
}

// DisplayName is what people read: the name, else the id.
func (self *AgentModelEntry) DisplayName() string {
	if name := strings.TrimSpace(self.Name); name != "" {
		return name
	}
	return self.Model
}

// HasPricing says the entry has prices of its own, all zeros included.
func (self *AgentModelEntry) HasPricing() bool {
	return self.Pricing != nil
}

// modelWithoutWidth is a model as the provider names it. "text-embedding-3-
// small@512" is that model at a width, which this program names that way to
// keep vectors of different widths apart; it is the same model at the same
// price, and looking the whole name up missed it -- so every embedding was
// charged at the provider's default rate for chat input, seven and a half
// times what it cost, and the dashboard said forty-seven dollars on a day the
// provider billed six.
//
// Only a suffix of "@" and digits at the end is a width: an id that has an
// "@" of its own, such as @cf/meta/llama-3-8b or model@rev, keeps it.
func modelWithoutWidth(model string) string {
	position := strings.LastIndex(model, "@")
	if position < 0 || position == len(model)-1 {
		return model
	}
	for _, character := range model[position+1:] {
		if character < '0' || character > '9' {
			return model
		}
	}
	return model[:position]
}

// ModelEntryIndexFor is the position in ModelEntries of the entry for a
// model, or -1 when the provider has none.
//
// An entry whose id is the whole name wins; only then is a width suffix cut
// off and the entry for the model without it looked for.
func (self *AgentProvider) ModelEntryIndexFor(model string) int {
	if index := self.ExactModelEntryIndexFor(model); index >= 0 {
		return index
	}
	if bare := modelWithoutWidth(model); bare != model {
		return self.ExactModelEntryIndexFor(bare)
	}
	return -1
}

// ExactModelEntryIndexFor is the position in ModelEntries of the entry whose
// id is exactly the given one, or -1.
func (self *AgentProvider) ExactModelEntryIndexFor(model string) int {
	for index := range self.ModelEntries {
		if self.ModelEntries[index].Model == model {
			return index
		}
	}
	return -1
}

// ModelEntryFor is the entry for a model, or nil.
func (self *AgentProvider) ModelEntryFor(model string) *AgentModelEntry {
	if index := self.ModelEntryIndexFor(model); index >= 0 {
		return &self.ModelEntries[index]
	}
	return nil
}

// Offers says the provider has an enabled entry for the model.
func (self *AgentProvider) Offers(model string) bool {
	entry := self.ModelEntryFor(model)
	return entry != nil && entry.IsEnabled()
}

// PricingFor is what a model of this provider costs: its entry's prices
// when it has any, else the provider's own.
func (self *AgentProvider) PricingFor(model string) AgentPricing {
	if entry := self.ModelEntryFor(model); entry != nil && entry.HasPricing() {
		return *entry.Pricing
	}
	return self.Pricing
}

// PricingEntryIndexFor is the position in ModelEntries of the entry that
// holds a model's prices, or -1 when the model has no entry and the
// provider's own prices apply.
func (self *AgentProvider) PricingEntryIndexFor(model string) int {
	return self.ModelEntryIndexFor(model)
}

// EnsureModelEntry adds an entry for a model the provider has none for, and
// reports whether it did. It is for the places that choose a model for
// somebody, such as the assistant setup, so that what they chose is offered.
func (self *AgentProvider) EnsureModelEntry(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || self.ModelEntryIndexFor(model) >= 0 {
		return false
	}
	self.ModelEntries = append(self.ModelEntries, AgentModelEntry{Model: model})
	return true
}

// AgentRoleModel is one place the configuration names a model: a role such
// as default, quick or embedding.
type AgentRoleModel struct {
	// Field is where it is written in the configuration:
	// agent.models.default.
	Field string

	// Role is the short name of the work: default, quick, embedding,
	// choices, voice.askModel.
	Role string

	// Model is "provider:model" as written.
	Model string
}

// RoleModels is every model the configuration assigns to work, in a fixed
// order, empty ones left out.
func (self *Agent) RoleModels() []AgentRoleModel {
	var roles []AgentRoleModel
	add := func(role, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		field := "agent.models." + role
		if role == "voice.askModel" {
			field = "agent.voice.askModel"
		}
		roles = append(roles, AgentRoleModel{Field: field, Role: role, Model: value})
	}
	add("default", self.Models.Default)
	add("decide", self.Models.Decide)
	add("categorize", self.Models.Categorize)
	add("fast", self.Models.Fast)
	add("quick", self.Models.Quick)
	add("embedding", self.Models.Embedding)
	add("transcription", self.Models.Transcription)
	add("triage", self.Models.Triage)
	add("research", self.Models.Research)
	add("voice.askModel", self.Voice.AskModel)
	add("summarize", self.Models.Summarize)
	add("reply", self.Models.Reply)
	add("ask", self.Models.Ask)
	add("schedule", self.Models.Schedule)
	add("compact", self.Models.Compact)
	add("scan", self.Models.Scan)
	add("synthesize", self.Models.Synthesize)
	for index, choice := range self.Models.Choices {
		if strings.TrimSpace(choice) == "" {
			continue
		}
		roles = append(roles, AgentRoleModel{Field: fmt.Sprintf("agent.models.choices[%d]", index), Role: "choices", Model: choice})
	}
	return roles
}

// implicitModels are models the server uses that no role names: the voice
// settings' transcription model on the voice provider, which hears voice
// messages and does the accurate second pass of a recording. It is written
// nowhere as provider:model, so validation does not ask for an entry (a new
// configuration with the defaults would fail it), but the provider offers it
// all the same.
func (self *Agent) implicitModels() []string {
	var names []string
	for _, role := range self.implicitRoleModels() {
		names = append(names, role.Model)
	}
	return names
}

// implicitRoleModels are the implicit models written as roles, so that the
// settings show them as used, deleting one is refused and switching one off
// fails validation, the same as for a role the configuration names.
func (self *Agent) implicitRoleModels() []AgentRoleModel {
	provider := self.VoiceProvider()
	if provider == nil {
		return nil
	}
	return []AgentRoleModel{{
		Field: "agent.voice.transcriptionModel",
		Role:  "voice.transcriptionModel",
		Model: provider.Name + ":" + self.Voice.EffectiveTranscriptionModel(),
	}}
}

// OffersModel says an enabled provider offers a "provider:model": it has an
// enabled entry for it, or has none and the server uses the model without a
// role naming it (see implicitModels).
func (self *Agent) OffersModel(name string) bool {
	providerName, model := SplitModelName(name)
	provider := self.Provider(providerName)
	if provider == nil || !provider.IsEnabled() {
		return false
	}
	if entry := provider.ModelEntryFor(model); entry != nil {
		return entry.IsEnabled()
	}
	return slices.Contains(self.implicitModels(), name)
}

// OfferRoleModels gives every provider an entry for each model a role names
// on it, and for the models the server uses that no role names, that has none, and leaves the entries it has alone, a switched off
// one included. Validation already refuses a role naming a model that is not
// an entry, so on a configuration that passed it this changes nothing; it is
// for a configuration assembled in code that never went through it.
func (self *Agent) OfferRoleModels() {
	for _, name := range self.usedModelNames() {
		providerName, model := SplitModelName(name)
		if provider := self.Provider(providerName); provider != nil {
			provider.EnsureModelEntry(model)
		}
	}
}

// usedModelNames is every provider:model the server uses: the roles' and the
// implicit ones.
func (self *Agent) usedModelNames() []string {
	names := []string{}
	for _, role := range self.RoleModels() {
		names = append(names, role.Model)
	}
	return append(names, self.implicitModels()...)
}

// RolesUsing is the names of the roles that point at a model of a provider,
// each once, in the order of RoleModels, then the implicit ones. A role that
// names a model at a width counts as using the model without it.
func (self *Agent) RolesUsing(providerName, model string) []string {
	roles := []string{}
	seen := map[string]bool{}
	provider := self.Provider(providerName)
	for _, role := range append(self.RoleModels(), self.implicitRoleModels()...) {
		name, rest := SplitModelName(role.Model)
		if name != providerName || seen[role.Role] {
			continue
		}
		if rest != model && (provider == nil || provider.ExactModelEntryIndexFor(rest) >= 0 || modelWithoutWidth(rest) != model) {
			continue
		}
		seen[role.Role] = true
		roles = append(roles, role.Role)
	}
	return roles
}

// UnmarshalYAML reads a provider whose models is either the list of entries
// or the object an older release wrote. The same key changed shape, so the
// decoder cannot be left to choose by itself: the old object is set aside,
// with modelPricing, and converted once the roles are known.
func (self *AgentProvider) UnmarshalYAML(node *yaml.Node) error {
	type plain AgentProvider
	if node.Kind != yaml.MappingNode {
		return node.Decode((*plain)(self))
	}
	outdated := &deprecatedProvider{}
	isOutdated := false
	remaining := *node
	remaining.Content = make([]*yaml.Node, 0, len(node.Content))
	for index := 0; index+1 < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		switch {
		case key.Value == "models" && value.Kind == yaml.MappingNode:
			outdated.Filter = &deprecatedModelFilter{}
			if err := value.Decode(outdated.Filter); err != nil {
				return err
			}
			isOutdated = true
		case key.Value == "modelPricing":
			if err := value.Decode(&outdated.Pricing); err != nil {
				return err
			}
			isOutdated = true
		default:
			remaining.Content = append(remaining.Content, key, value)
		}
	}
	if err := remaining.Decode((*plain)(self)); err != nil {
		return err
	}
	if isOutdated {
		self.deprecated = outdated
	}
	return nil
}

// migrateProviderModels turns what older releases stored under each
// provider's models and modelPricing into entries.
//
// A provider's entries are every model a role names on it, and every priced
// model that is a plain id and not a pattern, each at the price the old list
// gave it. An entry is never made for a model no role used and no price
// named: a pattern that admitted a hundred models offered a hundred, and the
// operator picks the ones they want.
func (self *Agent) migrateProviderModels() {
	used := self.usedModelNames()
	for index := range self.Providers {
		provider := &self.Providers[index]
		outdated := provider.deprecated
		if outdated == nil {
			continue
		}
		provider.deprecated = nil
		before := len(provider.ModelEntries)
		add := func(model string) {
			model = strings.TrimSpace(model)
			if model == "" || provider.ModelEntryIndexFor(model) >= 0 {
				return
			}
			provider.ModelEntries = append(provider.ModelEntries, AgentModelEntry{Model: model, Pricing: outdated.priceFor(model)})
		}
		for _, usedName := range used {
			if name, model := SplitModelName(usedName); name == provider.Name {
				add(model)
			}
		}
		for _, priced := range outdated.Pricing {
			if !strings.ContainsAny(priced.Model, "*?[") {
				add(priced.Model)
			}
		}
		if outdated.Filter != nil && (len(outdated.Filter.Allow) > 0 || len(outdated.Filter.Deny) > 0) {
			log.Warningf("agent.providers[%s].models.allow and deny are no longer used; the provider offers the %d models listed under agent.providers[%s].models", provider.Name, len(provider.ModelEntries), provider.Name)
		}
		log.Warningf("agent.providers[%s]: models as allow and deny patterns and modelPricing are replaced by a list of models; made %d entries from the models the roles use and the prices set for them, saved in the new shape the next time the settings are written", provider.Name, len(provider.ModelEntries)-before)
	}
}

// validateModelEntries judges each provider's entries.
func validateModelEntries(agent *Agent, validator *validator) {
	for index := range agent.Providers {
		provider := &agent.Providers[index]
		seen := map[string]bool{}
		for position, entry := range provider.ModelEntries {
			field := fmt.Sprintf("agent.providers[%d].models[%d]", index, position)
			switch {
			case strings.TrimSpace(entry.Model) == "":
				validator.add(field+".model", "required: the id the provider knows the model by, such as gpt-6.1-sol")
			case entry.Model != strings.TrimSpace(entry.Model):
				validator.add(field+".model", "%q has spaces around it", entry.Model)
			case seen[entry.Model]:
				validator.add(field+".model", "%q is listed twice on provider %q", entry.Model, provider.Name)
			}
			seen[entry.Model] = true
			if entry.ContextWindow != 0 && entry.ContextWindow < MinimumContextWindow {
				validator.add(field+".contextWindow", "must be 0 (the provider's own figure) or at least %d tokens", MinimumContextWindow)
			}
			pricing := AgentPricing{}
			if entry.Pricing != nil {
				pricing = *entry.Pricing
			}
			if pricing.Input < 0 || pricing.Output < 0 || pricing.CacheRead < 0 || pricing.CacheWrite < 0 || pricing.AudioInput < 0 {
				validator.add(field+".pricing", "prices cannot be negative")
			}
		}
	}
}
