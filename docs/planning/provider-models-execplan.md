# Providers and their models: one key or sign-in, models listed under it, each with its own settings

## Purpose / Big Picture

Today a provider (`config.AgentProvider`) is one credential, an API key or a
ChatGPT plan sign-in. Under it:
- the models it may use are an allow and deny list of name patterns;
- a model's price is the first matching pattern in `modelPricing`;
- a model's context window is whatever the provider's model list says, with
  no way to correct it.

A role (`agent.models.default`, `quick`, `fast`, `embedding`, ...) names a
model as a free string `provider:model`. To see or change anything about one
model, an operator edits patterns.

After this change the AI providers page reads as it should:
- one card per provider: its key or sign-in, and a ChatGPT plan's usage;
- under it, the models it offers, each a row: its name, then its id
  ("Sol · gpt-6.1-sol");
- "Add model" under each card, which offers the provider's own model list or
  takes a typed id;
- each model has its own page: its name, its id, its context window (the
  provider's figure, shown, with an override), its prices, whether it is on,
  and which kinds of work use it.

The role pickers list these models, "Provider · Model". Nothing stored changes
meaning: a role still names `provider:model`, where model is the id.

## Configuration (fixed; every surface builds on these)

`config.AgentProvider` gains `ModelEntries []AgentModelEntry`, with YAML key
`models` (see below for the old key):

    type AgentModelEntry struct {
        // Model is the id the service knows it by: gpt-6.1-sol.
        Model string `yaml:"model"`
        // Name is what people read: Sol. Empty reads as the id.
        Name string `yaml:"name,omitempty"`
        // ContextWindow corrects the window the provider reports, in tokens;
        // zero takes the provider's figure (and the unknown default when it
        // reports none).
        ContextWindow int `yaml:"contextWindow,omitempty"`
        // Pricing is what it costs per million tokens; zero fields are unknown.
        Pricing AgentPricing `yaml:"pricing,omitempty"`
        // Enabled keeps the entry while switching it off. Unset means on.
        Enabled *bool `yaml:"enabled,omitempty"`
    }

- The provider's old `models: {allow, deny}` filter and `modelPricing` go.
  The list of entries is what a provider offers. A role naming a model that is
  not an entry of an enabled provider fails validation with that said.
  `pricing` on the provider stays only as the fallback for an entry with none.
- **Loading an old configuration** (`config/deprecated.go`, as other renamed
  keys are handled): a provider whose `models` is the old object
  `{allow, deny}` is converted on load.
  - Its entries are every model a role names on that provider, and every
    `modelPricing` entry whose model is a plain id, not a pattern. Each gets
    the price its pattern gave it.
  - The allow and deny lists are dropped, with a warning in the log.
  - On the next save, the stored settings hold the new shape.
  - An entry is never invented for a model no role used and no price named.
- `Registry.ContextLength(name)` returns the entry's `ContextWindow` when set,
  else the provider's figure. Every caller already goes through it.
- `PricingFor(model)` reads the entry, else the provider's pricing.
- Validation:
  - an entry's model is not empty;
  - entries are unique per provider;
  - the context window is zero or at least 1000;
  - prices are not negative.

## GraphQL

- `AgentProvider` settings type: `models: [AgentModelEntry!]!` with `model`,
  `name`, `contextWindow`, `reportedContextWindow` (the provider's own figure,
  read-only, 0 when unknown), `pricing`, `isEnabled`, and `usedFor: [String!]!`
  (the role names that point at it: default, quick, embedding, ...).
- The provider's `allow` and `deny` and `modelPricing` fields are kept one
  release as deprecated: reading answers empty, and writing is ignored.
- Writing goes through the existing settings save, the whole agent section as
  today. Add `SaveAgentModelEntry(providerName, entry)` and
  `DeleteAgentModelEntry(providerName, model)`. A delete is refused while a
  role uses the model, with the role named.
- `ListAgentModels` (the provider's live list, used to offer models in "Add
  model") stays.

## CLI

- `teanode settings agent model list [--provider P]` shows provider, name, id,
  window (the override, or the provider's figure, marked), prices, on or off,
  and what uses it.
- `... model add P MODEL [--name] [--context-window] [--input] [--output]`.
- `... model set P MODEL ...`.
- `... model remove P MODEL`.

Check what the settings CLI is called today and fit in.

## Surfaces

- **Dashboard** (web/src/pages/settings/agentSettings.tsx and the provider
  pages: providerSignIn.tsx, providerPriceCheck.tsx):
  - A provider card lists its models under the credential, and "Add model"
    offers the live list with a filter, or a typed id.
  - A model row opens a model dialog or page: name, id (read-only once
    added), context window (placeholder showing the provider's figure),
    prices, on or off, used for, and Remove (refused while used).
  - The allow and deny and price pattern editors go.
  - Role pickers list the models as "Provider · Model", grouped by provider.
- **iPhone** (ManageServerViews.swift, `AgentProvidersView` near line 2985):
  - The same layout as the reference: a section per provider, with its
    credential row, then its model rows (name over id), then "Add model".
  - A model row pushes a Form: name, id, context window, prices, on, used
    for, and Remove.
  - Demo data, and shoot lines for the providers page and a model page.
- **Android** (settings/manage/AgentProviderScreens.kt): the same. Demo data
  and Roborazzi shots.
- Strings in English, Simplified Chinese and Japanese on every surface.

## Known limits of the conversion

- Pattern prices carry over only for models a role uses or that are priced by
  a plain id. A pattern such as `embed-*` prices the entries made from roles
  and plain ids that it matches; models it would have matched but nobody uses
  get no entry, so they take the provider's price once added.
- An old app's price edits are ignored until it updates: a build from before
  the entry's `pricing` could be null does not know an entry can have no
  price of its own, so update the apps before editing prices from them.
- An entry's `pricing` is a pointer: nil takes the provider's price, a value
  (even all zeros) is the model's own, and an old zero entry converts to a
  value of zero because the first match used to win.

## Validation

- Config:
  - an old configuration loads into entries (roles' models plus plain-id
    prices, with pattern prices applied) and saves in the new shape;
  - validation of entries and roles;
  - `ContextLength` prefers the override;
  - `PricingFor` reads the entry.
- GraphQL save and delete, including delete refused while used. CLI parity.
- Web vitest and tsc. iPhone kit tests and a Mac build. Android tests on
  station.
- Production check after deploy:
  - the existing providers (chatgpt, openai, jev and any others) come up with
    entries for every model their roles use;
  - no role fails validation;
  - the windows are unchanged unless overridden.
