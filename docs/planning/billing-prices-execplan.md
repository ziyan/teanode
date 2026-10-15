# The agent's model prices follow what the provider bills

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows `~/.claude/PLAN.md` (the ExecPlan
guide); everything a contributor needs is repeated here.

## Purpose / Big Picture

TeaNode works out what the agent costs from prices the operator types in:
for each model, a price per million tokens of input, output, cached input,
cache writes and audio input. Nobody types them in correctly for long. A
model the agent starts using has no price, so it is costed at the
provider's fallback price; a kind of token the operator never filled in
(cache writes, audio) costs nothing; a provider that changes its prices is
not followed. The budgets, the usage page and every cost the agent reports
are then wrong, sometimes by a factor of ten: on one server a month's bill
was about $296 while a third of it, cache writes, was costed at zero.

After this change an operator can give an OpenAI provider a billing key, an
OpenAI organization admin key, which can read the organization's cost and
usage reports but is not used for any model call. Once a day the server
reads the last two weeks of those reports, works out what each model and
kind of token was billed per million tokens, and, where the configured
price is missing or differs, sets it. Each change is written to the audit
log and told to the operators in their main conversation ("gpt-5.6-luna
cache writes: $0 → $0.25 per million tokens, from the provider's bill").
What the provider actually billed each day is kept beside TeaNode's own
estimate, so the usage page shows both.

To see it working: on a server with a billing key set, run
`teanode agent prices check` (or press "Check prices now" on the
provider in Manage › Agents › Models, or in the iPhone app's provider
screen). The answer lists each price it compared, the ones it changed, and
when the bill was last read. `teanode audit list --resource configuration`
shows the change; the usage tab shows "Billed" beside "Estimated".

## Progress

- [x] (2026-10-15) Milestone 1: the billing key on a provider (config, sealing,
  settings API, docs, dashboard, iPhone app).
- [x] (2026-10-15) Milestone 2: reading the bill and working out prices
  (`internal/llm/billing.go`), tested against canned reports.
- [ ] Milestone 3: the daily check that sets prices, audits and tells the
  operators; `teanode agent prices check`, the `settings` tool action,
  the dashboard and iPhone "Check prices now".
- [ ] Milestone 4: what was billed, kept per day and shown beside the
  estimate (usage tab, `teanode agent usage --server`, `agent_usage`
  tool, iPhone usage screen).
- [ ] Deploy, set the billing key on the server, run a check, confirm the
  audit rows and the told message, screenshots of every changed screen.

## Surprises & Discoveries

- Observation: a single day's cost divided by that day's tokens is not
  the list price. Costs and token counts are bucketed by different
  clocks, and some days are discounted (free daily tokens early in the
  month).
  Evidence, gpt-5.6-luna input, dollars per million tokens, one row per
  day from the organization reports: 0.0000, 0.0003, 0.0140, 0.1974,
  0.1959, 0.1978, 0.1977, 0.1894, 0.1644, 0.1812, 0.1581, 0.1990. The
  list price is 0.20. Cache writes on the same days: 0.2469, 0.2387,
  0.2452, 0.2409, 0.2338 (list 0.25).
- Observation: transcription models (gpt-4o-transcribe,
  gpt-4o-transcribe-diarize) and speech (gpt-4o-mini-tts) appear in
  `/v1/organization/usage/completions`, with `input_audio_tokens`; the
  `/v1/organization/usage/audio_transcriptions` report came back empty
  for the same days.
- Observation: the usage report names some models with a snapshot date
  (`gpt-5.4-mini-2026-03-17`), the cost report's line items name them the
  same way ("gpt-5.4-mini-2026-03-17, input"), while TeaNode records
  usage under the configured name (`openai:gpt-5.6-luna`).
- Observation: a project key (`sk-proj-…`) and a service account key
  (`sk-svcacct-…`) are both refused by the reports with "Missing scopes:
  api.usage.read"; an organization admin key (`sk-admin-…`) is answered.

## Decision Log

- Decision: the price of a model and kind of token is the highest daily
  rate that at least one other day comes within five percent of, rounded
  to two significant figures. Any day with at least a thousand tokens of
  that kind counts.
  Rationale: discounts only lower a day's rate, and clock skew puts a busy
  day a few percent under the list price, never over it in the bills seen,
  so the highest day is closest to the list price and rounding lands on it
  (0.2469 becomes 0.25). Requiring a second day within five percent stops
  one odd bucket from setting a price. The bill's amounts are not rounded
  to cents, so a small day's rate is as exact as a busy one's.
  Date/Author: 2026-10-15, Claude. Supersedes the first rule (highest rate
  among days with a million tokens or more), which priced gpt-5.6-terra
  output at 8.6 instead of 12: all its busy days were discounted and its
  days billed in full were small. A second try, the rate most days agree
  on, priced gpt-5.6-luna cache writes at 0.24 instead of 0.25, because
  busy days sit a few percent under the list price.
- Decision: a configured price is replaced only when it differs from the
  worked-out one by more than 5 percent, or is zero while the provider
  billed for that kind.
  Rationale: rounding and skew stay under 5 percent; a smaller threshold
  would flip a price back and forth.
- Decision: prices are set only for models TeaNode itself used in the
  window (they appear in `agent_usage` under that provider), matched to
  the report's name exactly or with a trailing snapshot date
  (`-YYYY-MM-DD`) removed. A new price entry is written under TeaNode's
  name for the model, never the dated one.
  Rationale: the organization's bill also covers other programs on the
  same account; pricing models the agent never calls adds noise to the
  settings. A dated name would not match TeaNode's usage rows (an exact
  `ModelPricing` entry is matched by `path.Match`, and
  `gpt-5.4-mini` does not match `gpt-5.4-mini-2026-03-17`).
- Decision: an automatic change edits the matching `ModelPricing` entry
  in place, or appends one, through the configuration store; it never
  rewrites the provider list through `UpdateSettings`.
  Rationale: the store's `Update` keeps every other field and every
  secret as it is; `keepSignIns` in `internal/cmd/server/agent.go` is the
  precedent for the server editing provider configuration itself.
- Decision: a price is not changed where the operator's entry is a
  pattern (contains `*`, `?` or `[`) that also covers other models; the
  check reports the difference instead.
  Rationale: editing a pattern's price changes models the bill did not
  speak about.
- Decision: costs stay worked out at read time from the current prices
  (no stored cost per usage row). What the provider billed is stored per
  day separately (Milestone 4).
  Rationale: the point of the change is that wrong prices were wrong for
  the past too, so repricing history is the wanted effect; the billed
  amount is the independent record.
- Decision: the billing key is its own field, `billingApiKey`, on an
  `openai` provider, sealed like `apiKey`, never sent to a model call.
  Rationale: an admin key can manage the whole organization; keeping it
  out of the request path limits what a leak of the calling key exposes,
  and the operator can see at a glance which provider is checked.

## Outcomes & Retrospective

(To be written at each milestone.)

## Context and Orientation

TeaNode is a Go server (module `github.com/ziyan/teanode`) with a React
dashboard in `web/`, a command line in `cmd/teanode` and `internal/cmd`,
and an iPhone app in `ios/`. The agent is a personal assistant that calls
model providers. Paths below are from the repository root.

Configuration. `internal/config/agent.go` defines `AgentProvider` (a
named model service: `Name`, `Kind` such as `openai`, `BaseURL`, `APIKey`
with the `secret:"true"` tag, `Pricing AgentPricing`, and `ModelPricing
[]AgentModelPricing`). `AgentModelPricing` has `Model` (a name or a
`path.Match` pattern), `Input`, `Output`, `CacheRead`, `CacheWrite`,
`AudioInput`, each dollars (or the configured currency) per million
tokens; zero means unknown. `(*AgentProvider).PricingFor(model)` returns
the first entry whose `Model` matches, else `Pricing`.
`(*Agent).CostOf(model, prompt, audioInput, completion, cacheRead,
cacheWrite)` prices a usage row; it is called when usage is read (usage
page, budgets, the per-turn note), not when it is written. Configuration
is stored as YAML rows in the `configuration` table
(`internal/config/dbstore.go`), and changed with `store.Update(func(c
*config.Configuration) error)`; secrets are sealed at rest
(`internal/config/seal.go`) and blanked when read through the API
(`internal/config/redact.go`, `config.Redacted`). Every field tagged
`yaml:"…"` in `internal/config/agent.go` must be documented in
`docs/configuration.md` (checked by `scripts/check-config-docs.bash`,
part of `make lint-ci`), and every field whose name says key, token or
secret must carry `secret:"true"` (`TestEverySecretIsTagged`).

The settings API. `internal/api/v1api/apigraph/settings_agent.go` has the
read view (`AgentProviderSettings`, with `HasAPIKey` rather than the key)
and the input (`AgentProviderParameters`, where a blank `APIKey` keeps
the stored one), applied in `applyAgentSettings`, which rebuilds the
provider list and carries secrets over from the previous entry of the
same name.

Usage. `agent_usage` rows (`internal/db/migrations/0031_personal_agents.sql`)
hold `model` (the configured `provider:model` name), `kind`, a time
bucket, and `values bigint[]`: 0 prompt, 1 completion, 2 cache read, 3
cache write, 4 calls, 5 audio input (`internal/models/agent.go`).
`QueryAgentUsageByModel` in `internal/db/database_agent.go` reads them.

Background work. `internal/agent/agent.go` `tickAt` runs every tick on
every server instance; self-throttled work (see `scavenge`, which returns
unless an hour has passed) is the pattern for a daily job, and a
PostgreSQL advisory lock (see `fetchLockKey` in
`internal/finance/rates/rates.go`) keeps two instances from doing it at
once. HTTP to OpenAI goes through `internal/llm/provider.go` (`doJSON`)
with the provider's `BaseURL` (default `https://api.openai.com/v1`).

Audit log. `internal/db/database_audit.go` `applyMutation(resourceType,
resourceId, action, before, after, write)` writes an `audit_event` in the
same transaction; `models.AuditResourceConfiguration` exists but nothing
uses it yet. Settings changes made by people are not audited today, only
logged; this plan audits the automatic ones.

Telling the operators. `tellMailImport` in
`internal/agent/finance_statement.go` appends a pair of messages (an
alert-marked user line and the agent's note) to a person's main
conversation and publishes them, and phones are pushed by
`pushOwnTurns`. A variant taking an agent id tells each operator.

The organization reports. `GET {base}/organization/costs?start_time=<unix>
&bucket_width=1d&group_by=line_item&limit=31` answers `{data: [{start_time,
results: [{line_item: "gpt-5.6-luna, cache writes", amount: {value,
currency}}]}], has_more, next_page}`. `GET
{base}/organization/usage/completions?start_time=<unix>&bucket_width=1d
&group_by=model&limit=31` answers buckets of `{model,
input_uncached_tokens, input_cached_tokens, input_cache_write_tokens,
output_tokens, input_audio_tokens, output_audio_tokens, input_text_tokens,
output_text_tokens}`. Both page with `page=<next_page>`. Line items seen:
"<model>, input", "<model>, cached input", "<model>, cache writes",
"<model>, output", "<model> audio, input", "<model> text, input",
"<model> text, output", "<model> audio, output", and a bare
"<embedding model>". The key goes in `Authorization: Bearer`.

## Plan of Work

Milestone 1 adds `BillingAPIKey string \`yaml:"billingApiKey,omitempty"
secret:"true"\`` to `AgentProvider`, documents it in
`docs/configuration.md` beside `apiKey`, validates that only an `openai`
provider takes it, and threads it through the settings API exactly as
`APIKey` is: `HasBillingAPIKey` on the view, `BillingAPIKey` on the input
(blank keeps, `config.Redacted` keeps), carried over on rename, cleared
when the kind changes away from `openai`. The dashboard's provider editor
(`web/src/pages/settings/agentSettings.tsx`) gets a password field
"Billing key (OpenAI admin key)" with a hint saying what it is for, under
the API key; the iPhone provider form (`AgentProviderForm` in
`ios/TeaNode/Sources/ManageServerViews.swift`, model in
`ios/TeaNodeKit/Sources/TeaNodeKit/ManageServer.swift`) gets the same. The
CLI needs nothing new: `teanode settings set agent` takes it as it takes
`apiKey`, with `-` reading a secret from standard input.

Milestone 2 adds `internal/llm/billing.go` with a client that reads both
reports for a window, following `next_page`, and a pure function that
turns them into observed prices:

    type BilledRate struct {
        Model        string  // as the report names it
        TokenKind    string  // input, cachedInput, cacheWrite, output, audioInput
        PerMillion   float64 // rounded to two significant figures
        DayCount     int     // well-used days it rests on
        BilledAmount float64 // over the window, for the report
    }
    func ReadBilling(ctx context.Context, client *http.Client, baseURL, billingKey string, since time.Time) (*Billing, error)
    func (self *Billing) Rates() []BilledRate
    func (self *Billing) DailyAmounts() []BilledDay // Milestone 4

Line items are parsed by splitting at the last comma: the left part is the
model, with a trailing " audio" or " text" telling the medium; the right
part ("input", "cached input", "cache writes", "output") the kind. "audio,
input" maps to audioInput; "text, input" to input; a bare model name to
input (embeddings). Each line item's daily amount is divided by the
matching token count of the same model and day (input: uncached text
input; cachedInput: cached input; cacheWrite: cache writes; output: text
output; audioInput: input audio). Kinds TeaNode cannot price (audio
output, 12-hour cache writes) are left out of `Rates`.

Milestone 3 adds `internal/agent/billing_prices.go`: `checkProviderPrices(ctx,
now, isForced)` called from `tickAt`, returning at once unless a day has
passed since the last check (kept in memory and, for restarts, in the
`agent_billing_check` row below) or `isForced`; it takes the advisory
lock, reads fourteen days for each `openai` provider with a billing key,
lists the models that provider served TeaNode in the window (distinct
`agent_usage.model` with the provider's prefix), matches report names to
them (exact, or with `-\d{4}-\d{2}-\d{2}` removed), compares with
`PricingFor`, and applies the changes through `store.Update`, then writes
one audit event per changed model (`AuditResourceConfiguration`,
resource id `agent.providers/<provider>/<model>`, before and after
prices) and tells every operator (people with the `server:manage`
permission who have an agent) in their main conversation, one message
listing all changes. The result of the last check (when, what was
compared, what changed, any error such as "the billing key was refused")
is stored in a new table `agent_billing_check` (one row per provider) and
read by a new query `AgentBillingCheck(provider)` and mutation
`CheckAgentProviderPrices(provider)` (permission `server:manage`). The
CLI gets `teanode agent prices check [--provider NAME]` and `teanode
agent prices show`; the `settings` tool gets an action `check_prices`;
the dashboard provider editor and the iPhone provider screen show "Prices
checked against the bill <time>: N changed" with a "Check prices now"
button.

Milestone 4 stores each day's billed amount per provider and model in
`agent_billed_day (provider, day, model, billed_amount, currency)` from the
same read, keeps 400 days, and adds `billedAmount` to the server usage
query by model and day so the Usage tab (`web/src/pages/agentAdmin.tsx`),
`teanode agent usage --server`, the `agent_usage` tool (operators only)
and the iPhone usage screen (`AgentUsageView`) show "Billed" beside
"Estimated".

## Concrete Steps

From the repository root:

    make test                       # needs Docker for PostgreSQL
    make lint-ci
    cd web && npx tsc --noEmit -p . && npx vitest run

Single Go package with a database: start `pgvector/pgvector:pg17` with
`POSTGRES_DB/USER/PASSWORD=teanode` and trust auth, export
`TEANODE_TEST_DATABASE_HOST=<container ip>`, then `go test -mod=vendor
./internal/agent/ -run Billing`.

## Validation and Acceptance

Unit: `internal/llm/billing_test.go` serves canned reports from an
`httptest.Server` (the shape in Context, two pages each) and expects
gpt-5.6-luna input 0.2, cache writes 0.25, output 1.2, a transcription
model audio input 6, a model with one well-used day left out, and a
discounted day not lowering the price. `internal/agent/billing_prices_test.go`
expects: a zero cache-write price set; a price within 5 percent left
alone; a model the agent never used left alone; a dated report name
matched to TeaNode's undated name and written undated; a pattern entry
reported, not changed; one audit event per change; one message to each
operator; a refused key recorded as the check's error and nothing
changed; a second check the same day doing nothing unless forced.

End to end, on the deployed server with the billing key set: `teanode
agent prices check` prints the comparison; the dashboard provider editor
shows the time of the check; `teanode audit list` shows the changes;
the Usage tab shows Billed beside Estimated for the last days.

## Idempotence and Recovery

A check writes nothing when every price is within 5 percent, so running
it again is harmless. A failed read (refused key, timeout) changes no
price and records the error on the check row. The billed-day rows are
upserted by (provider, day, model). To undo an automatic change, set the
price back in the provider editor; the audit event holds the old value.
Removing the billing key stops the checks; existing prices stay.

## Artifacts and Notes

Daily rates seen for gpt-5.6-luna (see Surprises) are the reason for the
rule in the Decision Log. Run against thirty days of a real organization's
reports, `Rates()` gives, per million tokens: gpt-5.6-luna input 0.2,
cached input 0.02, cache writes 0.25, output 1.2; gpt-5.6-terra input 2,
cached input 0.2, cache writes 2.5, output 12; gpt-4o-transcribe and
gpt-4o-transcribe-diarize text input 2.5, audio input 6, output 10;
gpt-4o-mini input 0.15, cached input 0.075. Every one is the provider's
list price.

## Interfaces and Dependencies

No new library. `internal/llm/billing.go` uses `net/http` with a
30-second context deadline per request, like `internal/upgrade`.
