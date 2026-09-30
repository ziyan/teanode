# A person links their bank accounts, and the agent can read the transactions

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows the ExecPlan conventions the other files
in `docs/planning/` follow: self-contained, prose first, and revised in place
as the work teaches us things.

## Purpose / Big Picture

Today a TeaNode agent knows what a person's mail says about money (a receipt,
a card alert) and nothing else. It cannot answer "how much did we spend on
groceries last month", notice a charge that does not look like the person's,
or tell them a balance before a large payment goes out.

After this change, an operator who wants to offer bank connections turns on
one or both of two providers in the agent settings. For **Plaid** they paste
the client id and secret of a Plaid account they hold; the secret is sealed
before it is stored and is never shown again. **SimpleFIN** needs nothing
from the operator. Then any person whose agent is on opens their agent page,
chooses **Link a bank**, and either signs in to their bank in Plaid's window
or pastes a SimpleFIN setup token. From then on the server fetches that
bank's accounts, balances and transactions a few times a day into tables of
their own, and the person's agent has a read-only `bank` tool that lists
accounts, searches transactions and totals spending. Unlinking removes the
connection at the provider and deletes every row it brought in.

How to see it working, once the milestones below are done: in a development
server, configure Plaid with sandbox keys, link the sandbox bank as a person
with the sandbox credentials `user_good` / `pass_good`, wait for the first
sync, and ask the agent "what did I spend in the last 30 days, by category".
The answer comes from the `bank` tool and matches
`teanode api call BankTransactions` for the same range.

Two words recur and are defined here. A **provider** is the outside service
that talks to banks for us: Plaid, or SimpleFIN. A **connection** is one
person's link to one login at one bank through one provider. A connection
holds one or more **accounts** (checking, savings, a card), and each account
has **transactions**.

## Progress

- [x] (2026-09-29) Researched providers and how a self-hosted install can use
  them (summarized under Context and Orientation).
- [x] (2026-09-29) Surveyed the code this touches: operator secrets, person
  secrets, migrations, tools, jobs, routes, dashboard, CSP, CLI.
- [x] (2026-09-29) Wrote this plan and the decision records it depends on.
- [ ] Milestone 1: provider clients and a prototype against Plaid sandbox and
  the SimpleFIN demo token; answer the open questions under Surprises.
- [ ] Milestone 2: operator settings for both providers (config, GraphQL,
  dashboard form, CLI).
- [ ] Milestone 3: tables, database layer and the sync job.
- [ ] Milestone 4: linking and unlinking from the dashboard, including the
  Plaid Link page and its narrow CSP.
- [ ] Milestone 5: the `bank` agent tool and the read queries behind it.
- [ ] Milestone 6: documentation, security review entry, release notes.

## Surprises & Discoveries

Questions the research could not settle. Milestone 1 answers them, and each
answer goes here with its evidence.

- Observation: SimpleFIN's protocol document states no limit on how far back
  `start-date` may reach, nor on how often `/accounts` may be called. The
  public bridge is known to refresh from banks about once a day. Whether it
  refuses wide date ranges, or rate limits, is unknown.
  Evidence: to be filled by the Milestone 1 prototype.

- Observation: whether a SimpleFIN transaction keeps its `id` when it moves
  from pending to posted is not stated. The sync design below does not rely
  on it (it replaces pending rows inside a window), but the answer decides
  whether the agent can say "this pending charge posted".
  Evidence: to be filled.

- Observation: the exact origins Plaid Link needs in a Content Security
  Policy (script, frame, connect) must be read from Plaid's current
  documentation and confirmed by loading Link under the new policy and
  watching the browser console for refusals.
  Evidence: to be filled.

- Observation: a Plaid "Trial" plan (free, US and Canada, teams created on
  or after 2026-04-15) allows ten production Items for the life of the team,
  and removing an Item does not give its slot back. An Item is Plaid's word
  for what this plan calls a connection. This shapes the unlink wording and
  the reauthentication design (update the existing Item, never create a new
  one), not the code paths.
  Evidence: Plaid's billing documentation, read 2026-09-29.

## Decision Log

- Decision: support two providers from the start, Plaid and SimpleFIN,
  behind one Go interface; neither is special in the tables or the tool.
  Rationale: they cover the same banks from opposite ends. Plaid needs an
  operator who holds a developer account; SimpleFIN needs only the person,
  who pays the bridge about fifteen dollars a year and holds their own token.
  A design built around one would bend when the other arrives. The
  interface also leaves room for a European provider later.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: the operator holds the Plaid client id and secret; the person
  holds each bank connection. The Plaid secret is a field of `config.Agent`
  tagged `secret:"true"`, sealed like the web search API key. Each
  connection's credential (a Plaid access token, or a SimpleFIN access URL)
  is sealed per row with its own label.
  Rationale: a self-hosted install cannot ship a shared Plaid key in the
  binary, and one would cover every install. The operator is the only party
  who can sign Plaid's terms. The bank login, the consent and the data are
  the person's, which matches
  `docs/decisions/20260910-agents-belong-to-people.md`. Recorded as
  `docs/decisions/20260929-the-operator-holds-the-provider-keys-the-person-holds-the-bank-link.md`.
  Date/Author: 2026-09-29.

- Decision: transactions are rows in a table of their own, not documents in
  the memory graph.
  Rationale: the questions people ask are sums over dates, categories and
  merchants. Memory stores text split into chunks for similarity search and
  cannot add. `docs/decisions/20260818-postgres-for-high-volume-data-only.md`
  says PostgreSQL holds what grows without bound, and transactions do.
  Recorded as
  `docs/decisions/20260929-bank-transactions-are-rows-not-documents.md`.
  Date/Author: 2026-09-29.

- Decision: the server polls; it does not accept Plaid webhooks in this
  plan.
  Rationale: many self-hosted servers are not reachable from the internet,
  SimpleFIN has no webhooks, and one polling path is simpler than two.
  Plaid checks banks one to four times a day, so a six-hour poll loses
  little. A webhook route can be added later behind `api.PublicPrefixes()`
  with Plaid's signature check.
  Date/Author: 2026-09-29.

- Decision: amounts are stored as `numeric(19,4)` with an ISO 4217 currency
  code, and a negative amount always means money leaving the account.
  Rationale: exact decimal arithmetic in SQL sums, without a table of
  currency exponents in Go. SimpleFIN already signs amounts this way; Plaid
  signs them the other way (positive is an outflow), so the Plaid client
  negates them once, at the edge.
  Date/Author: 2026-09-29.

- Decision: Plaid Link runs on a page of its own, `/bank-link`, with a
  Content Security Policy that adds only Plaid's origins; the rest of the
  dashboard keeps its policy unchanged.
  Rationale: the dashboard renders untrusted mail, and
  `internal/web/middlewares.go` treats its strict policy as a defense for
  that. The command line page and the drawer already set their own
  policies by path; this is a third case of the same pattern.
  Date/Author: 2026-09-29.

- Decision: which providers the server offers is a list,
  `banking.offeredProviders`, rather than a boolean per provider.
  Rationale: SimpleFIN needs no settings of its own, so a list says what is
  offered without inventing an empty settings block for it. Plaid is offered
  only when it is listed and its keys are set.
  Date/Author: 2026-09-29.

- Decision: in this plan the agent reads bank data only through the `bank`
  tool, in a conversation or a schedule the person set up. Nothing is copied
  into memory, and no alerts are raised from transactions.
  Rationale: linking a bank is consent to store the data on the person's own
  server. Sending it to a model provider happens only when the agent is asked
  something that needs it. Summaries in memory and unusual-charge alerts are
  good follow-ups, and each deserves its own decision.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

### The providers, as far as this plan needs them

**Plaid** is a company that signs in to banks on a person's behalf and
returns their accounts and transactions through an HTTPS API. A developer
account has a `client_id` and a `secret`, and three environments: sandbox
(fake banks, free, unlimited), production (real banks), and the retired
development environment, which this plan does not use. Every API call is a
`POST` of a JSON body that includes `client_id` and `secret`, to
`https://sandbox.plaid.com` or `https://production.plaid.com`.

Linking uses a browser widget called **Plaid Link**, loaded from
`https://cdn.plaid.com/link/v2/stable/link-initialize.js`. The flow is:

1. The server calls `/link/token/create` with an opaque id for the person,
   the product `transactions`, `transactions.days_requested` (up to 730, and
   fixed at link time), the country codes and a display name. It gets back a
   short-lived `link_token`.
2. The browser opens Link with that token. The person picks their bank and
   signs in, either inside Link or, for banks that insist, in the bank's own
   window (a popup on desktop).
3. Link returns a `public_token` and metadata naming the institution and
   accounts. The browser sends them to the server.
4. The server calls `/item/public_token/exchange` and receives an
   `access_token` and an `item_id`. The access token is the long-lived
   credential for that bank login; losing it loses the connection.

After that the server calls `/transactions/sync` with the access token and a
cursor. The response carries `added`, `modified` and `removed` transactions,
the current `accounts` with balances, a `next_cursor` and `has_more`. Paging
continues until `has_more` is false; the cursor is saved; the next sync
starts from it. When a pending transaction posts, Plaid removes the pending
one and adds a posted one whose `pending_transaction_id` names it. When the
bank needs the person to sign in again, calls fail with the error code
`ITEM_LOGIN_REQUIRED`, and Link is opened in "update mode" (a link token
created with the existing access token) to repair the same Item.
`/item/remove` ends the connection and stops Plaid's monthly charge for it.

A Plaid transaction has, among others: `transaction_id`, `account_id`,
`amount` (positive means money out), `iso_currency_code`, `date` (posted),
`authorized_date`, `name`, `merchant_name`, `pending`,
`pending_transaction_id`, and `personal_finance_category` with `primary`
and `detailed` strings.

**SimpleFIN** is an open protocol with one public server, the SimpleFIN
Bridge, which signs in to banks for a flat fee paid by the person. There is
no developer account. The person creates a **setup token** on the bridge's
website: a Base64 string that decodes to a claim URL. The application
`POST`s to that claim URL once, with an empty body, and receives an **access
URL** of the form `https://user:password@host/path`. The setup token cannot
be claimed twice. The access URL is the long-lived credential, and it
carries its own HTTP Basic credentials.

`GET <access URL>/accounts` returns an "account set": `errors` (strings to
show the person), and `accounts`, each with `id`, `name`, `currency`,
`balance`, `available-balance`, `balance-date`, an `org` naming the
institution, and `transactions`. Query parameters: `start-date` and
`end-date` (Unix seconds), `pending=1` to include pending transactions, and
`balances-only=1`. A transaction has `id`, `posted` (Unix seconds),
`amount` (a decimal string, negative means money out), `description`,
`pending` and `transacted_at`. SimpleFIN publishes a demo setup token on its
developer page for testing without a bank.

### The parts of TeaNode this touches

The server is Go; the dashboard is React in `web/`. Paths below are from the
repository root.

**Operator settings and their secrets.** The operator's agent configuration
is the struct `config.Agent` in `internal/config/agent.go`. It is stored in
the database. Any `string` field anywhere under it tagged `secret:"true"` is
sealed on write and opened on read by `internal/config/seal.go` (by
reflection; there is no list to update), using AES-GCM from
`internal/util/secretbox` with a key derived from the server secret. The
model to copy is `AgentSearch` near line 576:

    type AgentSearch struct {
        Kind   string `yaml:"kind,omitempty"`
        APIKey string `yaml:"apiKey,omitempty" secret:"true"`
    }

The operator changes settings through the GraphQL mutation `UpdateSettings`
(`internal/api/v1api/apigraph/settings.go`), which requires the
`server:manage` permission. The agent part of it is in
`internal/api/v1api/apigraph/settings_agent.go`: an input type per section
(`AgentSearchParameters` with `APIKey *string`), applied through
`applySecret` in `settings.go`, where a nil or redacted value keeps the
stored secret and an empty string clears it. The read side never returns a
secret, only a flag (`AgentSearchSettings{Kind, HasAPIKey}`). The dashboard
form to copy is `SearchForm` in `web/src/pages/settings/agentSettings.tsx`:
an empty password input with a placeholder of dots, and a hint that a key is
kept when one is set. The command line reaches the same mutation through
`teanode settings set agent ...` (`internal/cmd/settings.go`), which builds
its arguments from the schema, so no command line code is needed for new
settings.

**Per-person secrets.** Secrets a person gives their agent are sealed with
`Agent.SealSecret` and opened with `Agent.OpenSecret` in
`internal/agent/tools_mcp.go`. The closest model for a credential that is
refreshed and written back is `personToken` in the same file, which opens a
connected server's OAuth tokens, refreshes them, and seals them again. This
plan adds its own seal label (below) rather than reusing that one, so a
bank credential can never be opened by code that expects a connected
server's token.

**Tables.** Migrations live in `internal/db/migrations/`, named
`NNNN_name.sql` with a matching `NNNN_name.reverse.sql`, and are embedded
into the binary. The latest at the time of writing is `0129`; this plan adds
`0130`. Read `docs/coding/database-migrations.md` before writing one. Each
table has a public struct in `internal/models/` and a private gorm struct
with `TableName()` and `toModel()` in `internal/db/database_<name>.go`,
exposing an `<Name>Operation` interface that is embedded in `Transaction`
in `internal/db/db.go`. The per-person table to copy is `agent_alert`
(`0124_agent_alerts.sql`, `internal/db/database_alert.go`,
`internal/models/alert.go`): it belongs to an `agent_id` with a cascading
foreign key, and every query takes the agent id and filters on it. The
security review (`docs/security/security-review.md`, item SEC-13) asks that
code load a row and check it against its own owner, never against a scope
found some other way.

**Jobs.** A background tick (`tickAt` in `internal/agent/agent.go`) calls
`queueIngestion` (`internal/agent/ingest.go`), which asks
`ListDueAgentSources(now, 20)` for rows whose `next_run_at` has passed (the
query locks them with `FOR UPDATE SKIP LOCKED` so two servers do not take
the same row) and enqueues one job per row with
`self.Enqueue(tx, models.AgentJobIngest, agentId, "", source.ID)`. Job kinds
are constants in `internal/models/agent.go`; handlers are registered with
`self.Register(kind, handler)` in `internal/agent/agent.go`. Retries follow
the ladder in `internal/agent/job_policy.go` (one minute, five, fifteen, an
hour, four hours), with a timeout per kind. A unique index on queued and
running jobs stops the same row being enqueued twice.

**Agent tools.** Each tool is a package under `internal/agent/tools/<name>/`
whose `init()` calls `tools.Register`, and which is imported for effect in
`internal/agent/tools/all/all.go`. The struct is in
`internal/agent/tools/tool.go` (name, family, risk, permissions, parameters,
run function). A tool finds the person it runs for with
`tools.RunFrom(ctx)` and normally calls the GraphQL API as that person
through `run.Operations()`, so it can never read more than the person can.
Setting `Result.Untrusted` marks what it returns as text written by
outsiders. The tool to copy is `internal/agent/tools/note/note.go`, with its
query documents in `internal/client/note.go`.

**The API.** The dashboard and the command line both use one GraphQL
endpoint. Its schema is built by reflection from Go interfaces
(`internal/util/graphapi`), one `XxxQuery` and one `XxxMutation` interface
per area, composed in `internal/api/v1api/apigraph/schema.go`. Any new
operation is reachable from the command line immediately through
`teanode api call <Operation>`. Person-scoped resolvers begin with
`requireAgentPerson`, as `internal/api/v1api/apigraph/agent_source_secret.go`
does.

**Outbound requests to addresses a person supplied.** A SimpleFIN setup
token decodes to a URL the person chose, so the claim and every later fetch
go through `internal/util/safefetch`: `ParseTarget` accepts only http and
https, and `Client()` refuses to connect to any address that is not public.
Plaid's hosts are fixed constants and do not need it.

**The Content Security Policy.** `internal/web/middlewares.go` sets a strict
policy on every dashboard page (`default-src 'self'`, `script-src 'self'`
plus hashes of inline scripts, `connect-src 'self'`, `frame-src 'self'`),
and swaps in a different policy for two paths, `/cli` and `/drawer`. Plaid
Link cannot load under the strict policy.

**Internationalization.** Every string the dashboard shows is a key in
`web/src/i18n/en.ts`, with the same key in `ja.ts` and `zh.ts`. A key whose
text may stay English in every language is listed in
`web/src/i18n/sameInEveryLanguage.ts`, or `catalogs.test.ts` fails.

## Plan of Work

### Milestone 1: provider clients and a prototype

Create the package `internal/banking`. It knows how to talk to providers and
nothing about the database, jobs or GraphQL, so it can be tested with
`httptest` servers alone.

In `internal/banking/provider.go` define the shapes every provider returns,
already normalized, and the interface:

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
    }

    type Transaction struct {
        ProviderTransactionID        string
        ProviderAccountID            string
        PostedOn                     string // "2006-01-02", the day it posted
        TransactedAt                 *time.Time
        Amount                       string // decimal, negative is money out
        CurrencyCode                 string
        Description                  string
        MerchantName                 string
        CategoryPrimary              string
        CategoryDetailed             string
        IsPending                    bool
        PendingProviderTransactionID string
    }

    // SyncResult is one sync's worth of change. Removed lists provider
    // transaction ids to delete. ReplacedWindow, when set, means every
    // stored pending transaction of these accounts posted on or after it
    // that is not in Added is gone (SimpleFIN's way of reporting removal).
    type SyncResult struct {
        Accounts         []Account
        Added            []Transaction // upsert by provider transaction id
        Removed          []string
        ReplacedWindow   *time.Time
        NextCursor       string
        InstitutionName  string
    }

    type Provider interface {
        Kind() ProviderKind
        Sync(ctx context.Context, credential string, cursor string) (*SyncResult, error)
        Remove(ctx context.Context, credential string) error
    }

    var ErrLoginRequired = errors.New("the bank needs the person to sign in again")

`internal/banking/plaid.go` implements the Plaid client over `net/http` with
small request and response structs. No Plaid SDK is added: the six calls
this plan uses (`/link/token/create`, `/item/public_token/exchange`,
`/transactions/sync`, `/item/get`, `/item/remove`,
`/institutions/get_by_id`) are a page of code, and the repository vendors
its dependencies. The client takes an environment (`sandbox` or
`production`), a client id and a secret. Besides `Sync` and `Remove` it has
`CreateLinkToken(ctx, personReference string, accessTokenForUpdate string)
(string, error)` and `ExchangePublicToken(ctx, publicToken string)
(accessToken, itemId string, err error)`. `Sync` pages `/transactions/sync`
until `has_more` is false, negates every amount, maps Plaid's error code
`ITEM_LOGIN_REQUIRED` to `ErrLoginRequired`, and, if Plaid answers
`TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION`, restarts from the cursor it
began with, as Plaid's documentation says to.

`internal/banking/simplefin.go` implements SimpleFIN. `Claim(ctx,
setupToken string) (accessURL string, err error)` decodes the token, checks
the URL with `safefetch.ParseTarget`, and posts to it with
`safefetch.Client()`. `Sync` takes the access URL as the credential and the
cursor as the Unix time of the newest posted transaction seen so far. On the
first sync (empty cursor) it walks backward from now in 60-day windows until
two windows in a row come back empty or two years are covered. After that it
asks for `start-date` fourteen days before the cursor with `pending=1`, and
sets `ReplacedWindow` to that start, so pending transactions that vanished
are deleted and ones whose ids changed on posting are not left behind as
duplicates. `Remove` does nothing at the provider (SimpleFIN has no revoke
call); the dashboard tells the person they can also revoke the app on the
bridge's website. Errors in the account set's `errors` list are returned as
text to show the person.

Prototype, then keep the tests: write `internal/banking/plaid_test.go` and
`simplefin_test.go` with `httptest` servers that return recorded response
shapes (invented values, never real ones), covering paging, the sign flip,
pending-to-posted, `ErrLoginRequired`, and the SimpleFIN window walk. Then,
by hand and outside the test suite, run a throwaway `main` in the
scratchpad against Plaid sandbox (with sandbox keys and the sandbox
institution `ins_109508`, whose test login is `user_good` / `pass_good`,
using `/sandbox/public_token/create` to skip the browser) and against the
SimpleFIN demo token. Record the answers to the open questions under
Surprises & Discoveries. Acceptance: `go test ./internal/banking/` passes,
and the prototype prints accounts and a transaction count from both.

### Milestone 2: operator settings

In `internal/config/agent.go` add to `config.Agent`:

    // Banking is which bank providers people may link through, and the
    // operator's keys for those that need them.
    Banking AgentBanking `yaml:"banking,omitempty"`

    type AgentBanking struct {
        // OfferedProviders lists the providers a person may link through:
        // "plaid", "simplefin". Empty offers none.
        OfferedProviders []string `yaml:"offeredProviders,omitempty"`
        Plaid AgentPlaid `yaml:"plaid,omitempty"`
    }

    type AgentPlaid struct {
        Environment string `yaml:"environment,omitempty"` // sandbox or production
        ClientID    string `yaml:"clientId,omitempty"`
        Secret      string `yaml:"secret,omitempty" secret:"true"`
        CountryCodes []string `yaml:"countryCodes,omitempty"` // default ["US"]
    }

Validation, beside the existing checks in the same file: every listed
provider is known; if `plaid` is listed, `environment` is `sandbox` or
`production`, and a client id and secret are either both set or both empty.
A listed Plaid with no keys is not an error (the operator may add them
later) but is not offered to people.

In `settings_agent.go` add `AgentBankingParameters` (offered providers as
`*[]string`, `Plaid *AgentPlaidParameters` with `Secret *string` applied
through `applySecret`) and `AgentBankingSettings` for the read side, which
returns `hasSecret` and never the secret. In `agentSettings.tsx` add a
`BankingForm` built like `SearchForm`: two checkboxes for the providers, and
under Plaid the environment, client id and secret fields. Add the strings
to all three catalogs. Add a line to `docs/configuration.md`.

Acceptance: `teanode settings set agent banking:='{"offeredProviders":["plaid","simplefin"],"plaid":{"environment":"sandbox","clientId":"...","secret":"-"}}'`
prompts for the secret without echo; `teanode settings get agent` shows
`hasSecret: true` and no secret; the stored configuration row holds
ciphertext for the secret (check with `psql` that the value does not match
the plaintext); the dashboard form shows the dots and the kept hint. A test
in `internal/config` asserts `AgentPlaid.Secret` is reported by
`IsSecretField`.

### Milestone 3: tables, database layer and sync

Migration `internal/db/migrations/0130_agent_bank.sql` creates three tables.

`agent_bank_connection`: `id` (text primary key, the same id style as
`agent_alert`), `agent_id` (references `agent`, on delete cascade),
`provider_kind` (text), `institution_id` and `institution_name` (text),
`sealed_credential` (text, never selected into a GraphQL type),
`sync_cursor` (text), `connection_state` (text: `active`,
`login_required`, `failing`), `last_error_message` (text),
`last_synced_at`, `next_sync_at`, `created_at`, `updated_at`
(timestamptz). Index on `(next_sync_at)` for the due query and on
`(agent_id)`.

`agent_bank_account`: `id`, `agent_id` (cascade), `connection_id`
(references the connection, cascade), `provider_account_id`,
`account_name`, `account_mask`, `account_kind`, `currency_code`,
`current_balance` and `available_balance` (`numeric(19,4)`, nullable),
`balance_at`, timestamps. Unique on `(connection_id, provider_account_id)`.

`agent_bank_transaction`: `id`, `agent_id` (cascade), `account_id`
(references the account, cascade), `provider_transaction_id`, `posted_on`
(date), `transacted_at` (timestamptz, nullable), `amount`
(`numeric(19,4)`), `currency_code`, `description`, `merchant_name`,
`category_primary`, `category_detailed`, `is_pending` (boolean),
`pending_provider_transaction_id`, timestamps. Unique on `(account_id,
provider_transaction_id)`; index on `(agent_id, posted_on desc)`.

`agent_id` is repeated on accounts and transactions, although it could be
reached through the connection, so every read can filter on the owner
directly, as SEC-13 asks. The reverse migration drops the three tables.

Add `internal/models/bank.go` with `BankConnection`, `BankAccount`,
`BankTransaction` and the state constants, and `internal/db/database_bank.go`
with `BankOperation`: create and delete a connection, get one by id for an
agent, list an agent's connections, `ListDueBankConnections(now, limit)`
with `FOR UPDATE SKIP LOCKED`, `ApplyBankSync(connectionId, result)` which in
one transaction upserts accounts, upserts added transactions, deletes
removed ones, deletes pending rows in the replaced window that were not
added, stores the cursor and sets the next sync time, and the read queries
Milestone 5 needs. Every function that takes a row id also takes the agent
id and filters on both. Create, delete and state changes write audit rows
that name the provider and institution and never the credential.

Add a seal label in `internal/agent/tools_mcp.go`, beside the existing one:

    const bankCredentialSealLabel = "teanode agent: bank credentials"

with `SealBankCredential` and `OpenBankCredential` methods on `Agent`.

Add the job kind `AgentJobBankSync` in `internal/models/agent.go`, a
five-minute timeout in `job_policy.go`, and `queueBankSyncs` beside
`queueIngestion`, called from the same tick. The handler
`runBankSync` in a new `internal/agent/bank.go` loads the connection for its
agent, opens the credential, builds the provider from the current operator
settings (if the provider is no longer offered, or Plaid's keys are gone, it
marks the connection `failing` with a message saying so and does not
retry), calls `Sync`, and applies the result. `ErrLoginRequired` sets
`login_required` and stops polling that connection until it is repaired.
Other errors keep the retry ladder. The next sync is six hours after a
success.

Acceptance: with a connection row created by hand in a test (its credential
sealed with a sandbox access token from the prototype, or a fake provider
injected in unit tests), the job fills accounts and transactions; running it
twice adds nothing new; a fake result that removes a transaction deletes
its row; a pending row outside the replaced window survives. Database tests
run with `make test` (needs Docker) or, for this package alone, against a
standing test database as `docs/reference/local-development.md` describes.

### Milestone 4: linking and unlinking

GraphQL, in a new `internal/api/v1api/apigraph/agent_bank.go` with a
`BankQuery` and `BankMutation` interface added to `schema.go`. Every
resolver starts with `requireAgentPerson` and uses only the caller's agent.

- `BankProviders` returns which providers are offered and usable.
- `CreateBankLinkToken(connectionId optional)` creates a Plaid link token.
  With a connection id it opens that connection's credential and creates an
  update-mode token for repairing a sign-in. The person reference sent to
  Plaid is the agent id, which is opaque outside this server.
- `CompleteBankLink(publicToken, institutionId, institutionName)`
  exchanges the public token, seals the access token, creates the
  connection with `next_sync_at` now, and returns it. If creating the row
  fails after the exchange succeeded, it calls `/item/remove` with the new
  access token before returning the error, so no connection is left at
  Plaid that this server cannot see.
- `CompleteBankRepair(connectionId)` sets a `login_required` connection back
  to `active` and due now.
- `LinkSimpleFIN(setupToken)` claims the token, seals the access URL and
  creates the connection. The token is single use, so on a failure after the
  claim the error says to create a new token.
- `UnlinkBank(connectionId)` calls the provider's `Remove` (best effort,
  logged), then deletes the connection; the cascade removes its accounts and
  transactions.
- `BankConnections` lists the caller's connections with state, institution,
  accounts and last sync, never the credential.

Deleting an agent must also remove its Plaid Items, or the operator keeps
paying for them. In the agent deletion path (the resolver that ends up in
`DeleteAgent` in `internal/db/database_agent.go`), before the delete, list
the agent's connections and call `Remove` on each, logging failures and
carrying on.

The Plaid page. Add `BankLinkPagePath = "/bank-link"` in
`internal/web/middlewares.go` with its own policy: the strict policy plus
Plaid's origins in `script-src`, `frame-src` and `connect-src`, exactly the
list Milestone 1 confirmed and no wildcard beyond what Plaid documents. Add
a small dashboard route for it in `web/src/app.tsx` that asks
`CreateBankLinkToken`, loads Link, and on success calls
`CompleteBankLink`, then closes itself or links back to the agent page. On
the agent page (`web/src/pages/agent.tsx`), add a **Bank accounts** section:
the list from `BankConnections` with each connection's state and last
sync, **Link a bank** (opens `/bank-link` in a new window for Plaid, or
shows a text field for a SimpleFIN setup token, or offers both when both
are offered), **Sign in again** on a `login_required` connection, and
**Unlink**, whose confirmation says the transactions will be deleted and,
for Plaid, that some Plaid plans count a removed bank against their limit.
Errors and successes are toasts, as elsewhere on that page. Strings go in
all three catalogs.

Acceptance: in sandbox, link the sandbox bank from the dashboard and see it
listed as active with its accounts after the first sync; open the browser
console on `/bank-link` and see no policy refusals, and on any other
dashboard page see the unchanged strict header (`curl -sI` both paths);
paste the SimpleFIN demo token and see its accounts; unlink both and see
the rows gone (`teanode api call BankConnections` returns none). Use
Plaid's sandbox call `/sandbox/item/reset_login` on a linked Item to see
the connection turn `login_required` on the next sync, then repair it with
**Sign in again**.

### Milestone 5: the agent tool

Add read queries to `BankQuery`, each scoped to the caller's agent:

- `BankAccounts` returns accounts with balances and their connection's
  institution and state.
- `BankTransactions(from, to, accountId, text, minimumAmount,
  maximumAmount, category, limit, after)` searches, newest first, and caps
  `limit` at 200 with a cursor for the next page.
- `BankSpendingSummary(from, to, groupBy, accountId)` returns sums of
  negative amounts (spending) and positive amounts (income) grouped by
  `category`, `merchant`, `month` or `account`, per currency; amounts in
  different currencies are never added together.

Add `internal/agent/tools/bank/bank.go`, a tool named `bank` in a `bank`
family, risk `RiskRead`, with an `operation` parameter (`accounts`,
`transactions`, `summary`) and the filters above, calling those queries
through `run.Operations()` with documents in `internal/client/bank.go`.
Results set `Untrusted`, because merchant names and descriptions are
written by outsiders and may contain text aimed at the model. The tool
appears only when at least one provider is offered and the person has at
least one connection. Register it in `internal/agent/tools/all/all.go`.

Acceptance: with the sandbox bank linked, ask the agent "what did I spend in
the last 30 days by category"; the tool call appears in the conversation,
and the totals match `teanode api call BankSpendingSummary` with the same
range. A unit test in the tool package checks the parameters are passed
through and `Untrusted` is set. An API test checks that one person cannot
read another person's connection, account or transaction by id.

### Milestone 6: documentation

Write `docs/subsystems/banking.md` (providers, the tables, the sync, the
tool, what is sealed with which label). Add an entry to
`docs/security/security-review.md`: the two credentials and their seal
labels, the fact that the server secret sits in the same database (so a full
database dump opens them), the CSP exception scoped to one page, SimpleFIN
URLs going through `safefetch`, and the tool's untrusted results. Add the
settings to `docs/configuration.md`, the operations to
`docs/reference/command-line.md` if it lists areas, and the new page to
`docs/coding/frontend-design.md` if it lists pages. The changelog entry goes
in the pull request description, not in `CHANGELOG.md`.

## Concrete Steps

All commands run from the repository root.

    go test ./internal/banking/
    go test ./internal/config/
    make test          # starts a PostgreSQL container; needs Docker
    make lint-ci       # what CI runs
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

A development server for the end-to-end checks is described in
`docs/reference/local-development.md`. Use Plaid sandbox keys there, never
production ones: a production link on a Trial plan uses a slot for good.

## Validation and Acceptance

The feature is accepted when, on a development server with Plaid sandbox
keys and SimpleFIN offered:

1. An operator sets the Plaid keys from the dashboard or the command line,
   and no read of the settings returns the secret.
2. A person with the agent turned on links the sandbox bank and the
   SimpleFIN demo, sees both connections active with accounts, and sees
   transactions appear after the first sync.
3. Syncing again adds nothing; a Plaid sandbox login reset turns the
   connection to `login_required`, and **Sign in again** repairs the same
   connection.
4. The agent answers a spending question with the `bank` tool, and its
   totals match the API.
5. A second person cannot see or unlink the first person's connections
   through any API call.
6. Unlinking deletes the connection, its accounts and transactions, and the
   Plaid Item is removed (the sandbox `/item/get` for its access token
   answers `ITEM_NOT_FOUND`).
7. Every page other than `/bank-link` sends the unchanged security policy.

## Idempotence and Recovery

The migration is additive and its reverse drops only the three new tables.
Syncs are idempotent: transactions are upserted by provider id, and a
repeated Plaid page from the same cursor produces the same rows. A job that
dies halfway leaves the old cursor in place, because the cursor is written
in the same database transaction as the rows. If a Plaid token exchange
succeeds and the row is not written, the Item is removed at once (Milestone
4). A SimpleFIN token claimed but not stored cannot be recovered; the person
makes a new one, which is free.

## Artifacts and Notes

The expected shape of a stored transaction, with invented values:

    posted_on        2026-09-12
    amount           -42.1700
    currency_code    USD
    description      CORNER GROCER 0412
    merchant_name    Corner Grocer
    category_primary FOOD_AND_DRINK
    is_pending       false

## Interfaces and Dependencies

No new third-party Go modules. `internal/banking` depends only on the
standard library and `internal/util/safefetch`. The interface
`banking.Provider` and the constructors `banking.NewPlaid(environment,
clientId, secret string, countryCodes []string)` and `banking.NewSimpleFIN()`
must exist after Milestone 1; `db.BankOperation` after Milestone 3;
`BankQuery`, `BankMutation` and the `bank` tool after Milestones 4 and 5.
On the dashboard, Plaid Link is loaded from Plaid's CDN at run time on the
`/bank-link` page only; no npm package is added.
