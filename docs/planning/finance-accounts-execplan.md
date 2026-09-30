# A person links their financial institutions, and the agent can read the accounts and transactions

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows the ExecPlan conventions the other files
in `docs/planning/` follow: self-contained, prose first, and revised in place
as the work teaches us things. Two plans build on it:
`docs/planning/net-worth-execplan.md` and `docs/planning/budgets-execplan.md`.

## Purpose / Big Picture

Today a TeaNode agent knows what a person's mail says about money and
nothing else. It cannot answer "how much did we spend on groceries last
month", notice a charge that does not look like the person's, or say what a
balance is in another currency.

After this change, an operator who wants to offer this turns on one or both
of two providers in the agent settings. For Plaid they paste the client id
and secret of a Plaid account they hold; the secret is sealed before it is
stored and is never shown again. SimpleFIN needs nothing from the operator.
Then any person whose agent is on links an institution (a bank, a card
issuer, a brokerage, a lender) from their agent page, from the command
line, or by asking the agent, and the server syncs that institution's
accounts, balances and transactions a few times a day into tables of their
own. The agent's `finance` tool lists accounts, searches transactions,
totals spending, and converts amounts between currencies at the exchange
rate of any day. Every operation a person can do is available in the
dashboard, in the `teanode finance` command group, and as a `finance` tool
operation, with the exceptions named under Parity.

How to see it working, once the milestones are done: on a development
server, configure Plaid with sandbox keys, link Plaid's sandbox institution
as a person with the sandbox credentials `user_good` / `pass_good`, wait
for the first sync, and ask the agent "what did I spend in the last 30
days, by category, in euros". The answer comes from the `finance` tool and
matches `teanode finance spending-summary --since 30d --currency EUR`.

## Names used in this plan

Every thing has exactly one name in this plan, in the code, in the API, in
the command line and in the dashboard. Where an outside service calls it
something else, that name appears once, here, and nowhere else.

- **provider**: the outside service that signs in to institutions for us.
  There are two: Plaid and SimpleFIN.
- **institution**: a bank, card issuer, brokerage or lender a provider can
  sign in to. SimpleFIN calls it an "org".
- **finance source**: one person's link to one login at one institution
  through one provider. It is an agent source of the kind `finance` (agent
  sources are described under Context and Orientation). Plaid calls it an
  "Item".
- **finance account**: one account a finance source reports: checking,
  savings, a card, a brokerage account, a loan.
- **finance transaction**: one transaction on a finance account.
- **credential**: the secret a finance source keeps to reach its provider.
  For Plaid it is what Plaid calls an access token; for SimpleFIN, what
  SimpleFIN calls an access URL.
- **link**: to create a finance source. **Delete**: to remove one, which is
  the existing deletion of an agent source.
- **sync**: one run of a finance source, fetching what changed. It is what
  the existing `SyncAgentKnowledgeSource` operation starts.
- **provider category**: the category a provider assigned to a finance
  transaction. The budgets plan adds the person's own **spending
  category**, which is a different thing and has a different name.
- **amount**: a signed decimal with a currency code; negative is money
  leaving the account.
- **exchange rate**: the value of one currency in another on one day.
- **reporting currency**: the one currency a person wants totals shown in.
- **provider metadata**: the provider's whole object for a finance account
  or finance transaction, kept as it arrived.
- **categorize model**: the model the budgets plan uses to assign spending
  categories. Named here because its setting lives beside the others.

## Progress

- [x] (2026-09-29) Researched providers and how a self-hosted install can use
  them (summarized under Context and Orientation).
- [x] (2026-09-29) Surveyed the code this touches.
- [x] (2026-09-29) Wrote this plan and the decision records it depends on.
- [x] (2026-09-29) Revised: finance sources reuse agent sources; one
  `finance` tool; provider metadata kept; findings from probing a real
  SimpleFIN finance source recorded.
- [x] (2026-09-29) Revised: "bank" renamed "finance" throughout, since
  brokerages and lenders are not banks; one name per thing; exchange rates,
  conversion and reporting currency added; parity across dashboard,
  command line and tool made a requirement; the categorize model setting
  added, which may name a decision model such as Jev or a chat model.
- [x] (2026-09-30) Milestone 1: provider clients (`internal/finance`) with
  tests; Plaid Link's policy origins read from Plaid's documentation.
  Remaining: SimpleFIN pending id stability, compared once the saved
  pending transactions post.
- [x] (2026-09-30) Milestone 2: operator settings, the categorize model, and
  the Plaid products setting; nested `-` secrets on the command line read
  without echo.
- [ ] Milestone 3: the `finance` source kind, its reader, and the tables
  (completed: tables and database layer for all three plans, migrations
  0132 to 0135; remaining: the source kind and its reader).
- [ ] Milestone 4: exchange rates and the reporting currency (completed:
  the ECB client, the table, stored-rate lookups; remaining: the fetch
  policy and the API).
- [ ] Milestone 5: linking and deleting from the dashboard, the command line
  and the tool.
- [ ] Milestone 6: the read operations on the `finance` tool and the
  command line.
- [ ] Milestone 7: documentation, security review entry, release notes.

## Surprises & Discoveries

- Observation: the SimpleFIN Bridge caps one request at 90 days and warns
  above 45. A wider range is not refused: the bridge answers 200, keeps the
  most recent 90 days of the range, and says so in the account set's
  `errors` list ("Requested date range exceeds limit of 90 days and was
  capped", and above 45 days "exceeds recommended range of 45 days. In the
  future, this may be capped"). History reaches back only about 90 days
  before the finance source was linked: a window 180 to 91 days back held
  one transaction at its very start, and a window two years back held none.
  So the first sync fetches two 45-day windows and stops, and `errors`
  entries are warnings to log, not failures.
  Evidence: prototype requests against a real SimpleFIN finance source,
  2026-09-29 (one account, 82 transactions in 30 days, 3 pending).

- Observation: the bridge answers 403 Forbidden to a request with Python's
  default `User-Agent`, including the one-time claim, and the refused claim
  does not use up the setup token; the same request with an explicit
  `User-Agent` succeeds. The Go client sets its own `User-Agent`
  (`teanode/<version>`) on the claim and on every fetch.
  Evidence: claim refused with 403, then accepted with a `User-Agent`
  header, 2026-09-29.

- Observation: the bridge returns fields the protocol document does not
  list. Accounts carry `holdings` (investment positions). Transactions
  carry `payee` (a cleaned merchant name), `memo`, and `mcc` (the card
  network's merchant category code, a four-digit number). The error list
  is named `errors`, a list of strings, not `errlist` as the protocol
  document describes; accept both. All of it is kept in provider metadata.
  Evidence: key lists printed by the prototype, 2026-09-29.

- Observation: a real Plaid finance source asked for 730 days of history
  returned about 21 months, and Plaid reported the historical load
  complete. Institutions give Plaid what they have; 730 is a ceiling, not a
  promise.
  Evidence: a manual Plaid Trial link, 2026-09-29.

- Observation: whether a SimpleFIN transaction keeps its `id` when it moves
  from pending to posted is not stated. The sync does not rely on it (it
  replaces pending rows inside a window), but the answer decides whether
  the agent can say "this pending charge posted".
  Evidence: a raw 30-day response with three pending transactions was kept
  privately on 2026-09-29; fetch the same window again once they have
  posted and compare ids.

- Observation: Plaid documents its linking window as needing
  `https://cdn.plaid.com/link/v2/stable/link-initialize.js` as a script,
  `https://cdn.plaid.com` as a frame, the API host of the environment
  (`https://production.plaid.com` or `https://sandbox.plaid.com`) to
  connect to, and inline styles, which the dashboard's policy already
  allows. Plaid also lists `script-src 'unsafe-inline'`; the page loads the
  script by URL and does not add it. Both API hosts are allowed because the
  environment is a setting that can change without a restart.
  Evidence: Plaid's Link web documentation, 2026-09-29; the browser check
  on the deployed page is recorded under Outcomes.

- Observation: Plaid reports a card or loan balance as a positive amount
  owed; SimpleFIN leaves the sign to the institution, and most report it
  negative. A liability's valuation is therefore the absolute balance, and
  any other asset keeps its sign (an overdrawn checking account is a
  negative asset).
  Evidence: the provider clients' tests and the database layer's sign rule.

- Observation: SimpleFIN answers 403 when a credential was revoked, which
  signing in again cannot repair. The client returns
  `finance.ErrCredentialRefused`, distinct from `ErrSignInRequired`, and the
  finance source says to delete it and link again.
  Evidence: `internal/finance/simplefin_test.go`.

- Observation: a Plaid Trial plan (free, US and Canada, teams created on or
  after 2026-04-15) allows ten production finance sources for the life of
  the team, and deleting one does not give its slot back. This shapes the
  delete wording and the repair design (repair the existing finance source,
  never link a new one), not the code paths.
  Evidence: Plaid's billing documentation, read 2026-09-29.

## Decision Log

- Decision: a finance source does not go through the document passes of
  the memory graph. `runIngest` branches to the finance sync right after
  it loads an enabled source, before the `knowledge` feature switch; the
  sync uses the source row's schedule, cursor, error and `markSource`, and
  no sweeps, chunks or embeddings.
  Rationale: passes, sweeps of unseen documents and embedding are all
  about documents, which a finance source never files, and whether an
  operator offers memory has nothing to do with whether people may track
  their spending.
  Date/Author: 2026-09-29.

- Decision: the tables follow the repository's column conventions rather
  than the plan's sketches: 32-character ids, `created_at` and
  `modified_at`, optional text as `NOT NULL DEFAULT ''`, and CHECK
  constraints on the closed vocabularies. Plaid amounts are stored in a
  canonical form with four decimal places, rounded half away from zero;
  SimpleFIN amounts are kept as sent after checking they are decimals.
  Rationale: every neighbouring migration does it this way, and the audit
  comparison already ignores `modifiedAt`.
  Date/Author: 2026-09-29.

- Decision: on a provider modification, a spending category that was not
  set by the person is cleared only when the description, merchant or
  provider category changed, a transfer mark not set by the person only
  when the amount changed, and an unchanged row is not written at all.
  Rationale: rewriting identical rows every sync costs writes and would
  throw away categorizations for no reason.
  Date/Author: 2026-09-29.

- Decision: the word is "finance", not "bank": the source kind `finance`,
  the tables `agent_finance_account` and `agent_finance_transaction`, the
  package `internal/finance`, the settings section `finance`, the tool
  `finance`, the command group `teanode finance`.
  Rationale: brokerages, card issuers and lenders are not banks, and a
  brokerage is among the first institutions people link. One word for the
  whole feature keeps every name predictable.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: every thing has one name everywhere (see Names used in this
  plan), including the database, Go, GraphQL, the command line, the tool
  and the dashboard. Where a provider has its own word, the plan says so
  once and never uses it again.
  Rationale: the maintainer's general rule. Two names for one thing make a
  reader wonder whether there are two things.
  Date/Author: 2026-09-29.

- Decision: every feature is reachable from the dashboard, the `teanode
  finance` command group and the `finance` tool. The GraphQL operation is
  the one implementation; the three call it. Exceptions are listed under
  Parity with their reasons.
  Rationale: the maintainer's requirement. It also keeps the three from
  drifting: none has logic of its own.
  Date/Author: 2026-09-29.

- Decision: amounts are never added across currencies without converting,
  and conversion uses the European Central Bank's daily reference rates
  for the day in question, fetched when first needed and kept in an
  `exchange_rate` table. A person chooses a reporting currency; totals are
  shown per currency and, converted, in the reporting currency.
  Rationale: people travel, hold accounts abroad, and hold brokerage
  positions priced in other currencies. The ECB publishes rates for about
  thirty currencies every business day, free, with no key and a full
  history, which suits a self-hosted server. Fetching on demand avoids a
  server-wide schedule, which TeaNode does not have outside the agents.
  A currency the ECB does not publish (and cryptocurrencies) stays
  unconverted and is reported as such.
  Date/Author: 2026-09-29.

- Decision: support two providers from the start, Plaid and SimpleFIN,
  behind one Go interface; neither is special in the tables or the tool.
  Rationale: Plaid needs an operator who holds a developer account;
  SimpleFIN needs only the person, who pays the bridge about fifteen dollars
  a year and holds their own credential. A design built around one would
  bend when the other arrives.
  Date/Author: 2026-09-29.

- Decision: the operator holds the Plaid client id and secret; the person
  holds each finance source. Recorded as
  `docs/decisions/20260929-the-operator-holds-the-provider-keys-the-person-holds-the-finance-source.md`.
  Rationale: a self-hosted install cannot ship a shared Plaid key in the
  binary. The login, the consent and the data are the person's, which
  matches `docs/decisions/20260910-agents-belong-to-people.md`.
  Date/Author: 2026-09-29.

- Decision: a finance source is an agent source of the kind `finance`, not
  a table and a job of its own.
  Rationale: a source already has a cron line and next run time, a cursor
  its reader owns, the last error, an on and off switch, secrets sealed per
  source with an API that never returns them, a sync operation, deletion,
  and a place on the agent page. The maintainer asked that the plans reuse
  what TeaNode has. The cost is that a `finance` source writes rows rather
  than documents.
  Date/Author: 2026-09-29.

- Decision: finance transactions are rows in a table of their own, not
  documents in the memory graph. Recorded as
  `docs/decisions/20260929-finance-transactions-are-rows-not-documents.md`.
  Rationale: the questions are sums over dates, categories and merchants,
  which memory cannot answer.
  Date/Author: 2026-09-29.

- Decision: every finance account and finance transaction keeps its
  provider metadata (the provider's whole object as it arrived) in a
  `jsonb` column beside the normalized columns.
  Rationale: providers send more than the columns hold, some of it only
  discovered in use, and SimpleFIN keeps only about 90 days, so what is not
  kept at sync time is gone. Keeping the whole object means a mapping
  mistake can be repaired from stored data and a later feature can use an
  unmapped field, without a fresh sync. The maintainer asked for it. The
  `finance` tool returns normalized fields only, so provider metadata is
  not sent to a model wholesale; its owner can read it through the API.
  Date/Author: 2026-09-29.

- Decision: the categorize model is a setting of its own,
  `agent.models.categorize`, which may name a decision model (a `typesafe`
  provider's model, such as Jev) or a chat model. Empty falls back to
  `agent.models.decide` if that is set, then to `agent.models.scan`, then to
  `agent.models.fast`.
  Rationale: the maintainer plans to categorize with Jev, and other
  operators will want a cheap chat model, possibly different from the one
  that scans memory. Today a `typesafe` provider may be assigned only to
  `agent.models.decide`; this field is the second place it may go, and the
  validation in `internal/config/agent.go` is changed to say so. The
  budgets plan does the categorizing; the setting is added here so the
  settings milestone is done once.
  Date/Author: 2026-09-29.

- Decision: the server polls; it does not accept Plaid webhooks in this
  plan.
  Rationale: many self-hosted servers are not reachable from the internet,
  SimpleFIN has no webhooks, and one polling path is simpler than two.
  Date/Author: 2026-09-29.

- Decision: amounts are `numeric(19,4)` with an ISO 4217 currency code, and
  a negative amount always means money leaving the account.
  Rationale: exact decimal sums in SQL without a table of currency
  exponents. SimpleFIN already signs amounts this way; the Plaid client
  negates Plaid's amounts once, at the edge.
  Date/Author: 2026-09-29.

- Decision: Plaid Link runs on a page of its own, `/finance-link`, with a
  Content Security Policy that adds only Plaid's origins; the rest of the
  dashboard keeps its policy.
  Rationale: the dashboard renders untrusted mail and its strict policy is
  a defense for that. The command line page and the drawer already set
  their own policies by path.
  Date/Author: 2026-09-29.

- Decision: a SimpleFIN setup token is accepted only in the dashboard and
  on the command line (read without echo), never through the `finance`
  tool.
  Rationale: a token pasted into a conversation stays in the transcript and
  is sent to the model provider. The tool answers a request to link
  SimpleFIN with where to paste the token instead. This is the one
  deliberate gap in parity.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

### The providers, as far as this plan needs them

**Plaid** signs in to institutions on a person's behalf and returns their
accounts and transactions through an HTTPS API. A developer account has a
`client_id` and a `secret`, and two environments this plan uses: sandbox
(fake institutions, free, unlimited) and production (real ones). Every call
is a `POST` of a JSON body that includes `client_id` and `secret`, to
`https://sandbox.plaid.com` or `https://production.plaid.com`.

Linking uses a browser widget called Plaid Link, loaded from
`https://cdn.plaid.com/link/v2/stable/link-initialize.js`. The flow is:

1. The server calls `/link/token/create` with an opaque id for the person,
   the product `transactions`, `transactions.days_requested` (up to 730,
   fixed at link time), the country codes and a display name. It gets back
   a short-lived `link_token`.
2. The browser opens Link with that token. The person picks their
   institution and signs in, inside Link or, for institutions that insist,
   in the institution's own window (a popup on desktop).
3. Link returns a `public_token` and metadata naming the institution and
   accounts. The browser sends them to the server.
4. The server calls `/item/public_token/exchange` and receives the
   credential (`access_token`) and an `item_id`, which becomes the finance
   source's provider reference. Losing the credential loses the finance
   source.

After that the server calls `/transactions/sync` with the credential and a
cursor. The response carries `added`, `modified` and `removed`
transactions, the current `accounts` with balances, a `next_cursor` and
`has_more`. Paging continues until `has_more` is false. When a pending
transaction posts, Plaid removes the pending one and adds a posted one
whose `pending_transaction_id` names it. When the institution needs the
person to sign in again, calls fail with the error code
`ITEM_LOGIN_REQUIRED`, and Link is opened in update mode (a link token
created with the existing credential) to repair the same finance source.
`/item/remove` ends it and stops Plaid's monthly charge for it.

A Plaid transaction has, among others: `transaction_id`, `account_id`,
`amount` (positive means money out), `iso_currency_code`, `date` (posted),
`authorized_date`, `name`, `merchant_name`, `pending`,
`pending_transaction_id`, and `personal_finance_category` with `primary`
and `detailed` strings.

**SimpleFIN** is an open protocol with one public server, the SimpleFIN
Bridge, which signs in to institutions for a flat fee paid by the person.
The person creates a setup token on the bridge's website: a Base64 string
that decodes to a claim URL. The application `POST`s to that URL once,
with an empty body and a `User-Agent`, and receives the credential, a URL
of the form `https://user:password@host/path`. A setup token cannot be
claimed twice.

`GET <credential>/accounts` returns an account set: `errors` (warnings, as
strings) and `accounts`, each with `id`, `name`, `currency`, `balance`,
`available-balance`, `balance-date`, `org` (the institution), `holdings`
and `transactions`. Query parameters: `start-date` and `end-date` (Unix
seconds, at most 90 days apart, 45 recommended), `pending=1` to include
pending transactions, and `balances-only=1`. A transaction has `id`,
`posted` (Unix seconds), `amount` (a decimal string, negative means money
out), `description`, `payee`, `memo`, `mcc`, `pending` and
`transacted_at`. SimpleFIN publishes a demo setup token on its developer
page for testing without an institution.

**The European Central Bank** publishes euro reference rates for about
thirty currencies every business day around 16:00 Central European Time.
Three files matter, all public and without a key, as CSV inside a zip or
as XML: the latest day, the last 90 days, and the full history since 1999,
under `https://www.ecb.europa.eu/stats/eurofxref/`. A rate between two
non-euro currencies is the ratio of their euro rates. There are no rates
on weekends and TARGET holidays; the rate for such a day is the latest
earlier published one.

### The parts of TeaNode this touches

The server is Go; the dashboard is React in `web/`. Paths are from the
repository root.

**Operator settings and their secrets.** The operator's agent configuration
is `config.Agent` in `internal/config/agent.go`, stored in the database.
Any `string` field under it tagged `secret:"true"` is sealed on write and
opened on read by `internal/config/seal.go` (by reflection; there is no
list), using AES-GCM from `internal/util/secretbox` with a key derived from
the server secret. A pending change lets the operator keep the server
secret in a file (`teanode-server --secret-file`) instead of the database
and seals every secret setting, not only the agent's; with the secret in a
file, a database dump opens none of them. Nothing in this plan depends on
that change landing first. The field to copy is in `AgentSearch`:

    type AgentSearch struct {
        Kind   string `yaml:"kind,omitempty"`
        APIKey string `yaml:"apiKey,omitempty" secret:"true"`
    }

The operator changes settings through the GraphQL mutation `UpdateSettings`
(`internal/api/v1api/apigraph/settings.go`, permission `server:manage`).
The agent part is in `internal/api/v1api/apigraph/settings_agent.go`: an
input type per section (`AgentSearchParameters` with `APIKey *string`),
applied through `applySecret` in `settings.go`, where nil or a redacted
value keeps the stored secret and an empty string clears it. The read side
returns only a flag (`AgentSearchSettings{Kind, HasAPIKey}`). The dashboard
form to copy is `SearchForm` in `web/src/pages/settings/agentSettings.tsx`.
The command line reaches the same mutation through `teanode settings set
agent ...` (`internal/cmd/settings.go`), which builds its arguments from
the schema.

**Models.** `config.AgentModels` in `internal/config/agent.go` names a
model per purpose: `default`, `fast`, `scan` (bulk work with nobody present;
empty falls back to `fast`), `decide` (a decision model that scores answers
known in advance instead of writing; only a `typesafe` provider's model may
go there, and the validation near the provider checks enforces that), and
others. `internal/llm/typesafe.go` is the `typesafe` provider, whose model
is Jev; it implements the `llm.Decider` interface and not the chat
interface. `deciderFor` in `internal/agent/dream_decide.go` shows how a
caller asks the decision model and falls back to a chat model when there is
none; `docs/subsystems/providers-and-models.md` describes the registry and
structured answers.

**Agent sources.** An agent source is a row of `agent_source`, the model
`models.AgentKnowledgeSource` in `internal/models/knowledge.go`: a `Kind`
(today `computer`, `archive`, `skill`, `web`, `sent`), a name, a
`Specification` (a JSON object whose `Type` and `Settings` fields hold what
the kind needs), `Enabled`, a `Cron` line, `NextRunAt`, `LastRunAt`,
`LastError`, and a `Cursor` map only the reader for that kind reads and
writes. The GraphQL operations `SyncAgentKnowledgeSource` and
`DeleteAgentKnowledgeSource` (`internal/api/v1api/apigraph/agent_graph.go`)
act on one. Reading one page of a source is `readOnePass` in
`internal/agent/ingest.go`, which switches on the kind. A background tick
(`tickAt` in `internal/agent/agent.go`) queues due sources through
`queueIngestion`, with row locks so two servers never take the same one,
and retries failures on the ladder in `internal/agent/job_policy.go`.

**Source secrets.** Rows of `agent_source_secret` (migration `0104`,
`internal/db/database_source_secret.go`), keyed by source and name, sealed
with `Agent.SealSecret` (`internal/agent/tools_mcp.go`), and set through
`SetAgentKnowledgeSourceSecret`
(`internal/api/v1api/apigraph/agent_source_secret.go`), whose read side
reports only whether a secret is set. A finance source keeps its credential
there under the name `credential`.

**Tables.** Migrations live in `internal/db/migrations/`, `NNNN_name.sql`
with a matching `NNNN_name.reverse.sql`. At the time of writing the latest
on main is `0129`, and a pending change that moves the server secret out of
the database takes `0130` and a plan usage change takes `0131`, so this
plan uses `0132` and `0133`; take the
next free numbers when the work starts. Read `docs/coding/database-migrations.md` first. Each table has a
public struct in `internal/models/` and a private gorm struct in
`internal/db/database_<name>.go`, exposing an `<Name>Operation` interface
embedded in `Transaction` in `internal/db/db.go`. The per-person table to
copy is `agent_alert` (`0124_agent_alerts.sql`,
`internal/db/database_alert.go`). The security review
(`docs/security/security-review.md`, item SEC-13) asks that code load a row
and check it against its own owner.

**Agent tools.** Each tool is a package under `internal/agent/tools/<name>/`
registered in `internal/agent/tools/all/all.go`, with a risk class from
`internal/agent/tools/tool.go` (`read`, `write`, `destructive`, `outward`,
`granting`). A tool calls the GraphQL API as the person through
`tools.RunFrom(ctx).Operations()`, so it can never read more than they can,
and sets `Result.Untrusted` on text outsiders wrote. The tool to copy is
`internal/agent/tools/note/note.go`, with its documents in
`internal/client/note.go`.

**The API and the command line.** The dashboard and the command line use
one GraphQL endpoint whose schema is built by reflection from Go interfaces
(`internal/util/graphapi`), one `XxxQuery` and `XxxMutation` per area,
composed in `internal/api/v1api/apigraph/schema.go`. Every operation is
reachable at once through `teanode api call <Operation>`. A feature also
gets a command group of its own for people, written like
`internal/cmd/note.go` (urfave/cli v3 subcommands, a `--json` flag on each,
listed in `cmd/teanode/main.go` and `internal/cmd/cmd_test.go`).
Person-scoped resolvers begin with `requireAgentPerson`.

**Outbound requests to addresses a person supplied.** A SimpleFIN setup
token decodes to a URL the person chose, so the claim and every fetch go
through `internal/util/safefetch` (`ParseTarget`, `Client()`), which refuse
anything but public addresses over http or https. Plaid's and the ECB's
hosts are constants.

**The Content Security Policy.** `internal/web/middlewares.go` sets a strict
policy on dashboard pages and swaps in another for `/cli` and `/drawer`.
Plaid Link cannot load under the strict one.

**The secrets check.** `scripts/check-secrets.bash` refuses hostnames in
tracked files unless they are on `ALLOWED_HOSTS`. `.plaid.com` is already
there; add `www.ecb.europa.eu` with a comment when the exchange rate client
is written. The check reads tracked files only, so stage new files before
running `make lint-ci`.

**Internationalization.** Every dashboard string is a key in
`web/src/i18n/en.ts`, with the same key in `ja.ts` and `zh.ts`.

## Parity

Each row is one GraphQL operation and the three ways to reach it. The
subcommands belong to `teanode finance`; the tool operations to the
`finance` tool. Where one name is shown it serves both.

The names follow one rule, so the same operation has one name everywhere:
the tool operation is the GraphQL operation's name in snake_case with a
leading `Finance` dropped (`FinanceSpendingSummary` is `spending_summary`),
and the `teanode finance` subcommand is the same name in kebab-case
(`spending-summary`). The parity tests check the names by that rule, not
only that a counterpart exists. The two operations that span two GraphQL
calls are named for what the person does: `link_plaid`
(`CreateFinanceLinkToken`, then `CompleteFinanceLink`) and `repair`
(`CreateFinanceLinkToken` for the finance source, then
`CompleteFinanceRepair`). Operations TeaNode already has for every source
keep their names (`SyncAgentKnowledgeSource`, `DeleteAgentKnowledgeSource`),
and the tool and subcommand names for them are `sync`, `disable_source`,
`enable_source` and `delete_source`.

    what                          GraphQL                        dashboard (Finance tab)    subcommand / tool operation
    which providers are offered   FinanceProviders               Link an institution        providers
    link through Plaid            (two calls, see below)         Link an institution        link-plaid / link_plaid (1)
    link through SimpleFIN        LinkSimpleFIN                  Link an institution        link-simplefin / none (2)
    repair a sign-in              (two calls, see below)         Sign in again              repair (1)
    list finance sources          FinanceSources                 sources list               sources
    sync now                      SyncAgentKnowledgeSource       Sync now                   sync
    switch a source off or on     the existing source update     source switch              disable-source, enable-source / disable_source, enable_source
    delete a finance source       DeleteAgentKnowledgeSource     Delete                     delete-source / delete_source
    finance accounts              FinanceAccounts                Accounts                   accounts
    finance transactions          FinanceTransactions            Transactions               transactions
    spending summary              FinanceSpendingSummary         Spending summary           spending-summary / spending_summary
    exchange rate                 ExchangeRate                   in converted totals        exchange-rate / exchange_rate
    convert an amount             ConvertCurrency                in converted totals        convert-currency / convert_currency
    reporting currency            SetReportingCurrency           Finance settings           set-reporting-currency / set_reporting_currency

(1) Plaid Link needs a browser. The command line prints the address of
`/finance-link` for this person and waits until the new finance source
appears (or `--no-wait`); the tool answers with that address for the person
to open. Both then see the same result as the dashboard.

(2) A setup token is never accepted through the tool (see the Decision
Log); the tool answers with where to paste it.

Delete is risk `destructive` and sync, switch and reporting currency are
`write`; everything else is `read`. A parity test in
`internal/cmd/finance_test.go` lists the operations in `FinanceQuery` and
`FinanceMutation` by reflection and fails if one has no subcommand of the
name the rule gives, and a matching test in the tool package fails if one
has no tool operation of that name. Both know the two operations that span
two calls and the one gap (SimpleFIN in the tool), and nothing else.

## Plan of Work

### Milestone 1: provider clients and a prototype

Create the package `internal/finance`. It talks to providers and nothing
else, so it can be tested with `httptest` servers alone.

In `internal/finance/provider.go`:

    type ProviderKind string

    const (
        ProviderKindPlaid     ProviderKind = "plaid"
        ProviderKindSimpleFIN ProviderKind = "simplefin"
    )

    type Account struct {
        ProviderAccountID string
        AccountName       string
        AccountMask       string // last digits, when the provider gives them
        AccountKind       string // depository, credit, loan, investment, other
        CurrencyCode      string
        CurrentBalance    string // decimal, "" when unknown
        AvailableBalance  string
        BalanceAt         time.Time
        ProviderMetadata  json.RawMessage
    }

    type Transaction struct {
        ProviderTransactionID        string
        ProviderAccountID            string
        PostedOn                     string // "2006-01-02"
        TransactedAt                 *time.Time
        Amount                       string // decimal, negative is money out
        CurrencyCode                 string
        Description                  string
        MerchantName                 string
        ProviderCategoryPrimary      string
        ProviderCategoryDetailed     string
        IsPending                    bool
        PendingProviderTransactionID string
        ProviderMetadata             json.RawMessage
    }

    // SyncResult is one sync's worth of change. RemovedProviderTransactionIDs
    // are deleted. PendingReplacedFrom, when set, means every stored pending
    // transaction of these accounts posted on or after it that is not in
    // Added is gone (SimpleFIN's way of reporting removal).
    type SyncResult struct {
        Accounts                      []Account
        Added                         []Transaction // upserted by provider transaction id
        RemovedProviderTransactionIDs []string
        PendingReplacedFrom           *time.Time
        NextCursor                    string
        InstitutionName               string
        ProviderWarnings              []string
    }

    type Provider interface {
        Kind() ProviderKind
        Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error)
        Remove(ctx context.Context, credential string) error
    }

    var ErrSignInRequired = errors.New("the institution needs the person to sign in again")

`internal/finance/plaid.go` implements Plaid over `net/http` with small
request and response structs; no Plaid SDK is added. The client takes an
environment (`sandbox` or `production`), a client id, a secret and country
codes. Besides `Sync` and `Remove` it has `CreateLinkToken(ctx,
personReference string, credentialForRepair string) (string, error)` and
`ExchangePublicToken(ctx, publicToken string) (credential, providerReference
string, err error)`. `Sync` pages `/transactions/sync` until `has_more` is
false, decoding each page's transactions and accounts first into
`[]json.RawMessage` (kept as provider metadata) and then into typed
structs, negates every amount, maps `ITEM_LOGIN_REQUIRED` to
`ErrSignInRequired`, and on `TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION`
restarts from the cursor it began with.

`internal/finance/simplefin.go` implements SimpleFIN. `Claim(ctx,
setupToken string) (credential string, err error)` decodes the token,
checks the URL with `safefetch.ParseTarget`, and posts to it with
`safefetch.Client()` and a `User-Agent`. `Sync` takes the credential and a
cursor holding the Unix time of the newest posted transaction seen. On the
first sync (empty cursor) it fetches the last 90 days as two 45-day
windows. After that it asks for `start-date` fourteen days before the
cursor with `pending=1`, and sets `PendingReplacedFrom` to that start.
`payee` becomes `MerchantName`; `mcc`, when present, becomes
`ProviderCategoryDetailed` as `mcc:<code>`; `errors` (or `errlist`) become
`ProviderWarnings`. `Remove` does nothing at the provider (SimpleFIN has
no revoke call); the dashboard tells the person they can also revoke the
app on the bridge's website.

Tests in `internal/finance/plaid_test.go` and `simplefin_test.go` use
`httptest` servers with invented values and cover paging, the sign flip,
pending to posted, `ErrSignInRequired`, the SimpleFIN windows, warnings,
the `User-Agent`, and that a field the client does not know survives into
`ProviderMetadata`. By hand and outside the suite, run a throwaway `main`
in a scratch directory against Plaid sandbox (sandbox keys, the sandbox
institution `ins_109508`, `/sandbox/public_token/create` to skip the
browser) and against the SimpleFIN demo token, and record the open answers
under Surprises & Discoveries. Acceptance: `go test ./internal/finance/`
passes and the prototype prints accounts and a transaction count from both.

### Milestone 2: operator settings

In `internal/config/agent.go` add to `config.Agent`:

    // Finance is which providers people may link institutions through,
    // and the operator's keys for those that need them.
    Finance AgentFinance `yaml:"finance,omitempty"`

    type AgentFinance struct {
        // OfferedProviders lists the providers a person may link through:
        // "plaid", "simplefin". Empty offers none.
        OfferedProviders []string   `yaml:"offeredProviders,omitempty"`
        Plaid            AgentPlaid `yaml:"plaid,omitempty"`
    }

    type AgentPlaid struct {
        Environment  string   `yaml:"environment,omitempty"` // sandbox or production
        ClientID     string   `yaml:"clientId,omitempty"`
        Secret       string   `yaml:"secret,omitempty" secret:"true"`
        CountryCodes []string `yaml:"countryCodes,omitempty"` // default ["US"]
    }

and to `config.AgentModels`:

    // Categorize is the model that assigns spending categories to finance
    // transactions: a decision model (a typesafe provider's) or a chat
    // model. Empty falls back to Decide, then Scan, then Fast.
    Categorize string `yaml:"categorize,omitempty"`

Validation: every offered provider is known; with `plaid` offered, the
environment is `sandbox` or `production`, and client id and secret are both
set or both empty (a listed Plaid without keys is valid and not offered to
people). In the model checks, `agent.models.categorize` may name either a
`typesafe` provider or a chat provider; change the message that says a
`typesafe` provider may only be assigned to `decide` so it names both
fields. Document both in `docs/configuration.md` (CI checks that every
field is documented).

In `settings_agent.go` add `AgentFinanceParameters` (with `Secret *string`
applied through `applySecret`), `AgentFinanceSettings` (returning
`hasSecret`, never the secret), and `Categorize` beside the other model
parameters. In `agentSettings.tsx` add a `FinanceForm` like `SearchForm`
(the providers, and for Plaid the environment, client id and secret), and a
categorize picker in the models section listing chat models and decision
models. Strings in all three catalogs.

Acceptance: `teanode settings set agent finance:='{"offeredProviders":["plaid","simplefin"],"plaid":{"environment":"sandbox","clientId":"...","secret":"-"}}'`
prompts for the secret without echo; `teanode settings show agent` shows
`hasSecret: true` and no secret; the stored configuration holds ciphertext
for it; `teanode settings set agent models:='{"categorize":"<typesafe provider>/jev-latest"}'`
is accepted, and naming a `typesafe` model for `ask` is still refused. A
test in `internal/config` asserts `AgentPlaid.Secret` is a secret field and
covers the new validation.

### Milestone 3: the `finance` source kind and the tables

Migration `0132_agent_finance.sql` creates two tables.

    agent_finance_account
      id                  text primary key
      agent_id            text not null, references agent, on delete cascade
      source_id           text not null, references agent_source, on delete cascade
      provider_account_id text not null
      account_name        text not null
      account_mask        text
      account_kind        text not null      depository, credit, loan, investment, other
      currency_code       text not null
      current_balance     numeric(19,4)
      available_balance   numeric(19,4)
      balance_at          timestamptz
      provider_metadata   jsonb not null default '{}'
      created_at, updated_at timestamptz
      unique (source_id, provider_account_id)

    agent_finance_transaction
      id                              text primary key
      agent_id                        text not null, references agent, on delete cascade
      finance_account_id              text not null, references agent_finance_account, on delete cascade
      provider_transaction_id         text not null
      posted_on                       date not null
      transacted_at                   timestamptz
      amount                          numeric(19,4) not null
      currency_code                   text not null
      description                     text
      merchant_name                   text
      provider_category_primary       text
      provider_category_detailed      text
      is_pending                      boolean not null
      pending_provider_transaction_id text
      provider_metadata               jsonb not null default '{}'
      created_at, updated_at timestamptz
      unique (finance_account_id, provider_transaction_id)
      index (agent_id, posted_on desc)

`agent_id` is repeated on both so every read filters on the owner
directly. Deleting the source removes its finance accounts and, through
them, their finance transactions.

Add `SourceFinance AgentKnowledgeKind = "finance"` in
`internal/models/knowledge.go`. A `finance` source's `Specification.Type` is
the provider kind and its `Settings` hold `institutionId`,
`institutionName` and `providerReference`. Its default `Cron` is every six
hours. Its cursor holds `providerCursor` and `isSignInRequired`.

Add `internal/models/finance.go` (`FinanceAccount`, `FinanceTransaction`)
and `internal/db/database_finance.go` with `FinanceOperation`: list an
agent's finance accounts, `ApplyFinanceSync(agentId, sourceId, result)`
which in one database transaction upserts finance accounts, upserts added
finance transactions, deletes removed ones and the pending ones the result
replaced, and the read queries Milestone 6 needs. Every function takes the
agent id and filters on it.

Add a case to `readOnePass` in `internal/agent/ingest.go`:

    case models.SourceFinance:
        return self.readFinanceSource(ctx, run, source, cursor)

`readFinanceSource`, in `internal/agent/ingest_finance.go`, opens the
source's `credential`, builds the provider from the current settings (a
provider no longer offered, or Plaid without keys, is an error that becomes
the source's `LastError`), calls `Sync`, calls `ApplyFinanceSync`, logs
provider warnings, and returns the new cursor. On `ErrSignInRequired` it
sets `isSignInRequired`, returns an error the dashboard shows as "sign in
again", and while the flag is set it returns at once without calling the
provider.

First read `runIngest` and `readOnePass` end to end and confirm a reader
that files no documents is handled (counts of zero, no embedding work, no
pass left wanting more). Record what you find under Surprises &
Discoveries; if a step assumes documents, skip it for `finance` sources in
the one place it happens.

Acceptance: with a `finance` source made in a test and a fake provider,
the ingest job fills finance accounts and finance transactions; running it
twice adds nothing; a removal deletes its row; a pending row before the
replaced window survives; a sign-in error sets the flag and the next run
does not call the provider.

### Milestone 4: exchange rates and the reporting currency

Migration `0133_exchange_rate.sql` creates a server-wide table (rates are
public facts, not anyone's data) and adds a column to `agent`:

    exchange_rate
      rate_on             date not null
      currency_code       text not null      the currency one euro buys
      euro_rate           numeric(20,10) not null
      rate_source         text not null      ecb
      created_at          timestamptz
      primary key (rate_on, currency_code, rate_source)

    agent
      reporting_currency_code text           nullable; empty means the currency
                                              of the person's first finance account

`internal/finance/exchange.go` fetches the ECB files with the standard HTTP
client and a `User-Agent`: the full history the first time the table has
nothing, the 90-day file when the newest stored day is within 90 days, and
the latest-day file otherwise. `internal/db/database_exchange_rate.go`
stores them (upsert) and answers `ExchangeRate(from, to, on)`: the euro
rates of both currencies on the latest stored day on or before `on`, their
ratio, and that day. If the latest stored day is older than the latest
published business day before `on`, the caller fetches first. One fetch
runs at a time, under a database advisory lock, so two servers do not
fetch together. A currency the ECB does not publish returns a
`finance.ErrNoExchangeRate` naming it.

GraphQL, in `FinanceQuery`: `ExchangeRate(fromCurrencyCode,
toCurrencyCode, rateOn)` and `ConvertCurrency(amount, fromCurrencyCode,
toCurrencyCode, rateOn)`, each returning the rate, the day it is from, and
its source. In `FinanceMutation`: `SetReportingCurrency(currencyCode)`.
Every total in Milestone 6 returns amounts per currency and, where every
currency has a rate, their sum in the reporting currency, converted at
each transaction's posted day.

Acceptance: a test with a fake ECB server returns a rate for a weekday, the
previous Friday's for a Sunday, a cross rate between two non-euro
currencies, and `ErrNoExchangeRate` for an unpublished code;
`teanode finance convert-currency 100 USD JPY --on 2026-09-01` prints a converted
amount with its rate and day.

### Milestone 5: linking and deleting

GraphQL, in `internal/api/v1api/apigraph/agent_finance.go` with
`FinanceQuery` and `FinanceMutation` added to `schema.go`. Every resolver
starts with `requireAgentPerson`.

- `FinanceProviders` returns the offered, usable providers.
- `CreateFinanceLinkToken(sourceId optional)` creates a Plaid link token;
  with a source id, an update-mode token that repairs that finance source.
  The person reference sent to Plaid is the agent id.
- `CompleteFinanceLink(publicToken, institutionId, institutionName)`
  exchanges the public token, creates the `finance` source with its
  settings and default cron, stores the credential through the database
  call `SetAgentKnowledgeSourceSecret` uses, and makes it due now. If
  creating the source fails after the exchange, it calls `/item/remove`
  with the new credential before returning the error.
- `CompleteFinanceRepair(sourceId)` clears `isSignInRequired` and makes the
  source due now.
- `LinkSimpleFIN(setupToken)` claims the token and creates the source the
  same way. The token is single use, so a failure after the claim says to
  make a new token.
- `FinanceSources` lists the caller's `finance` sources with institution,
  finance accounts, last sync, error and whether a sign-in is required.
- Sync, switching off and on, and deleting are the existing source
  operations. For a `finance` source, `DeleteAgentKnowledgeSource` first
  opens the credential and calls the provider's `Remove` (best effort,
  logged). Deleting an agent does the same for each of its `finance`
  sources before the delete (the resolver that ends in `DeleteAgent` in
  `internal/db/database_agent.go`), or the operator keeps paying for Plaid
  finance sources nobody can reach.

The Plaid page: `FinanceLinkPagePath = "/finance-link"` in
`internal/web/middlewares.go` with the strict policy plus exactly the Plaid
origins Milestone 1 confirmed, and a small route in `web/src/app.tsx` that
asks `CreateFinanceLinkToken`, opens Link, calls `CompleteFinanceLink`,
and links back to the agent page.

On the agent page (`web/src/pages/agent.tsx`), `finance` sources appear in
the existing list of sources with their institution, finance accounts,
last sync and error, and the existing controls. Add **Link an
institution** (opens `/finance-link` for Plaid, a field for a SimpleFIN
setup token, or both), **Sign in again**, and delete wording that says the
finance transactions will be deleted and that some Plaid plans count a
deleted finance source against their limit. Errors and successes are
toasts. Strings in all three catalogs.

Command line: `internal/cmd/finance.go`, `teanode finance` with
`providers`, `link-plaid`, `link-simplefin` (reads the token without
echo), `repair`, `sources`, `sync`, `disable-source`, `enable-source` and
`delete-source`, each with `--json`. Tool: `internal/agent/tools/finance/finance.go`, named
`finance`, with the operations in the Parity table.

Acceptance: in sandbox, link Plaid's sandbox institution from the dashboard
and from `teanode finance link-plaid`, and see each among the sources with
finance accounts after the first sync; the browser console on
`/finance-link` shows no policy refusals and `curl -sI` on another page
shows the unchanged policy; `teanode finance link-simplefin` with the demo
token works; asking the agent to link SimpleFIN gets the answer to use the
dashboard or command line; `/sandbox/item/reset_login` makes a source ask
for a sign-in, and **Sign in again** repairs the same source; delete
removes the rows.

### Milestone 6: read operations

Add to `FinanceQuery`, each scoped to the caller's agent:

- `FinanceAccounts` with balances, institution and source state, each
  balance also in the reporting currency where a rate exists.
- `FinanceTransactions(from, to, financeAccountId, text, minimumAmount,
  maximumAmount, providerCategory, limit, after)`, newest first, `limit`
  at most 200, with a cursor for the next page.
- `FinanceSpendingSummary(from, to, groupBy, financeAccountId)`: sums of
  money out and money in grouped by `providerCategory`, `merchant`,
  `month` or `financeAccount`, per currency and in the reporting currency.
  (The budgets plan switches `providerCategory` grouping to the person's
  spending categories.)

The `finance` tool gains `accounts`, `transactions` and `spending_summary`
(risk `read`); results set `Untrusted`, because merchant names and descriptions
are written by outsiders. The tool is offered when at least one provider is
offered; with no `finance` source it answers how to link one. The command
line gains `accounts`, `transactions` and `spending-summary`, with the same
filters as flags. The dashboard gains **Accounts**, **Transactions** (a table,
which stays a table on a phone and scrolls sideways) and **Spending
summary** under a **Finance** tab on the agent page.

Acceptance: ask the agent "what did I spend in the last 30 days by
category, in euros"; the totals match `teanode finance spending-summary
--since 30d --currency EUR` and the dashboard; API tests show a second person cannot
read the first person's finance sources, finance accounts or finance
transactions by id; the parity tests pass.

### Milestone 7: documentation

Write `docs/subsystems/finance.md` (providers, the `finance` source kind and
its reader, the tables, exchange rates, the tool, the command group, where
the credential is kept, and the names list above). Add the `finance` kind
where `docs/subsystems/memory.md` lists source kinds. Add to
`docs/security/security-review.md`: the Plaid secret and the credentials
and how they are sealed; that a database dump opens neither when the
operator keeps the server secret in a file (`teanode-server
--secret-file`, added by the change that moves the server secret out of
the database), and opens both when the server secret is still in the
database; that provider metadata holds whatever the
provider sent and is returned only to its owner; the CSP exception for one
page; SimpleFIN URLs going through `safefetch`; the tool's untrusted
results; and that setup tokens are refused in conversation. Add the command
group to `docs/reference/command-line.md`. The changelog entry goes in the
pull request description, not `CHANGELOG.md`.

## Concrete Steps

From the repository root:

    go test ./internal/finance/ ./internal/config/ ./internal/agent/tools/finance/ ./internal/cmd/
    make test          # starts a PostgreSQL container; needs Docker
    git add <new files by name>
    make lint-ci       # what CI runs; reads tracked files only
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

A development server for end-to-end checks is described in
`docs/reference/local-development.md`. Use Plaid sandbox keys there, never
production ones: a production link on a Trial plan uses a slot for good.

## Validation and Acceptance

Accepted when, on a development server with Plaid sandbox keys and
SimpleFIN offered:

1. An operator sets the Plaid keys and the categorize model from the
   dashboard or the command line, and no read returns the secret.
2. A person links Plaid's sandbox institution and the SimpleFIN demo, from
   the dashboard and from the command line, and sees finance accounts and
   finance transactions after the first sync.
3. Syncing again adds nothing; a sandbox sign-in reset makes the source ask
   for a sign-in, and repairing it keeps the same source.
4. The agent answers a spending question in a chosen currency with the
   `finance` tool, and the totals match the command line and the dashboard.
5. Converting between two currencies gives the ECB rate of the requested
   day, or the latest earlier one, with that day shown.
6. A second person cannot read, sync or delete the first person's finance
   sources, finance accounts or finance transactions through any API call.
7. Deleting a finance source deletes its rows and, for Plaid, removes it at
   Plaid (`/item/get` answers `ITEM_NOT_FOUND`).
8. Every page other than `/finance-link` sends the unchanged security
   policy.
9. The parity tests pass.

## Idempotence and Recovery

The migrations are additive; their reverses drop the new tables and the
new column. Syncs are idempotent: finance transactions are upserted by
provider id. The source's cursor is saved after the reader returns, so a
job that dies between writing rows and saving the cursor replays from the
old cursor, which is harmless because every write is an upsert or a delete
of something already gone. If a Plaid exchange succeeds and the source is
not created, it is removed at Plaid at once. A SimpleFIN token claimed but
not stored cannot be recovered; the person makes a new one. Exchange rates
are upserted, and fetching twice changes nothing.

## Artifacts and Notes

A stored finance transaction, with invented values:

    posted_on                   2026-09-12
    amount                      -42.1700
    currency_code               USD
    description                 CORNER GROCER 0412
    merchant_name               Corner Grocer
    provider_category_primary   FOOD_AND_DRINK
    is_pending                  false

## Interfaces and Dependencies

No new third-party Go modules and no npm packages. `internal/finance`
depends on the standard library and `internal/util/safefetch`. By the end
of Milestone 1: `finance.Provider`, `finance.NewPlaid(environment,
clientId, secret string, countryCodes []string)`, `finance.NewSimpleFIN()`.
Milestone 3: `models.SourceFinance`, `readFinanceSource`,
`db.FinanceOperation`. Milestone 4: `db.ExchangeRateOperation`,
`finance.ErrNoExchangeRate`. Milestones 5 and 6: `FinanceQuery`,
`FinanceMutation`, the `finance` tool, the `teanode finance` command group.
Plaid Link is loaded from Plaid's CDN at run time on `/finance-link` only.

Revision notes, 2026-09-29: finance sources became agent sources, reusing the
source's schedule, cursor, error, switch, sealed secrets, sync and
deletion; provider metadata is kept whole; SimpleFIN findings recorded.
Then, at the maintainer's request: "bank" became "finance" throughout,
since brokerages and lenders are not banks; every thing got one name, with
provider words confined to the names list; multiple currencies gained
exchange rates, a reporting currency and conversion; parity across the
dashboard, the command line and the tool became a requirement with tests;
and the categorize model setting was added so categorization can run on
Jev or on a cheap chat model.
