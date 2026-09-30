# A person tracks their net worth over time, from finance sources, their own entries and the agent's estimates

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It builds on
`docs/planning/finance-accounts-execplan.md` (finance sources, finance
accounts, exchange rates, the `finance` tool and the `teanode finance`
command group), which must be done at least through its Milestone 4. What
this plan needs from it is repeated under Context and Orientation.

## Purpose / Big Picture

Tracking spending needs finance transactions accumulated; the finance
accounts plan does that. Tracking net worth needs something else: what each
thing a person owns or owes was worth on each day, kept as a history. A
balance fetched today adds a point; it overwrites nothing.

What a person owns and owes is valued in one of four ways, and the design
treats them as one list:

1. **Finance sync**: a finance account's balance, recorded at every sync
   (checking, savings, cards, brokerage and retirement accounts, loans and
   mortgages, whichever the provider reports).
2. **Agent reading**: an account reachable only through a connected server
   (an MCP server the person added to their agent, such as a brokerage's),
   read by the agent on a daily schedule.
3. **Manual**: things only the person can value, such as a car, a house, a
   collection, or a private loan. The person enters a value and adjusts it
   when they choose.
4. **Agent estimate**: a house from its address and description, a car from
   its make, model, year and mileage, estimated by the agent from the web
   on a schedule, with sources and a range, only where the person allowed.

After this change, the agent page's **Finance** tab has a **Net worth**
section: a chart over time in the reporting currency, the list of assets
with each one's latest value, its date and valuation source, and a history
per asset. The same is available from `teanode finance` subcommands and
`finance` tool operations, so the agent can answer "what is my net worth",
"how has it changed since January", and "what is the house worth now".

How to see it working: link Plaid's sandbox institution (finance accounts
plan), add a car by hand at 18,000, add a house and allow estimates for it,
and run the jobs by hand. The tab shows assets of three valuation sources,
each with a value dated today, and a net worth line with a point per day,
matching `teanode finance net-worth --since 30d`.

## Names used in this plan

One name per thing, as in the finance accounts plan, whose names list
applies here too. Added here:

- **asset**: anything that counts toward net worth, including what is owed.
- **liability**: an asset whose value subtracts (`is_liability`): a loan, a
  mortgage, a card balance. Not a separate kind of row.
- **valuation**: one value of one asset on one day.
- **valuation source**: where a valuation came from: `finance_sync`,
  `agent_reading`, `manual`, `agent_estimate`. An asset's own
  `valuation_source` is the one its values normally come from; a
  valuation's is the one it did come from. Same name, same values.
- **net worth**: the sum of the latest valuations of open assets on a day.

## Progress

- [x] (2026-09-29) Wrote this plan and its decision record.
- [x] (2026-09-29) Revised: operations moved onto the shared `finance` tool;
  charts reuse the dashboard's components.
- [x] (2026-09-29) Revised: "bank" renamed "finance"; one name per thing
  (`valuation_method` merged into `valuation_source`); totals converted to
  the reporting currency; parity across dashboard, command line and tool.
- [ ] Milestone 1: the two tables, the database layer and the net worth
  query.
- [ ] Milestone 2: finance sync valuations, including investment and loan
  accounts.
- [ ] Milestone 3: assets and manual valuations in the dashboard, the command
  line and the tool.
- [ ] Milestone 4: agent readings through connected servers.
- [ ] Milestone 5: agent estimates for houses and cars.
- [ ] Milestone 6: the net worth chart and documentation.

## Surprises & Discoveries

- Observation: the SimpleFIN Bridge sends an undocumented `holdings` list
  per finance account, which the finance accounts plan keeps in provider
  metadata. Positions per holding are a later feature that can read it
  from there.
  Evidence: the finance accounts plan's SimpleFIN probe, 2026-09-29.

- Observation: to measure early: rows per person per year (roughly twenty
  assets times 365, about seven thousand), and whether Plaid sandbox
  returns investment holdings and liabilities on Trial sandbox keys.
  Evidence: to be filled.

## Decision Log

- Decision: net worth is computed from a history of valuations, one row per
  asset per day per valuation source, never from a stored total. Recorded
  as `docs/decisions/20260929-net-worth-is-a-history-of-valuations.md`.
  Rationale: a stored total is wrong the moment one asset is corrected for
  a past day; summing the latest valuations gives the right answer for any
  day from the same rows.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: a value is carried forward until a newer valuation replaces
  it, and every value shows its date.
  Rationale: a house valued in March is still worth about that in June;
  leaving it out of June would drop net worth by the price of a house.
  Date/Author: 2026-09-29.

- Decision: when one asset has valuations from more than one valuation
  source on the same day, `manual` wins, then `finance_sync` and
  `agent_reading`, then `agent_estimate`. All rows are kept.
  Rationale: the person knows what they sold the car for.
  Date/Author: 2026-09-29.

- Decision: accounts behind a connected server are valued by the agent on
  a daily schedule the person creates, which calls that server's tools and
  records the value through the `finance` tool. There is no server-side
  mapping from a particular server's output to a value.
  Rationale: connected servers are arbitrary and their output is not ours
  to depend on. It costs one short agent turn a day per person who uses it,
  which the person sees and can stop.
  Date/Author: 2026-09-29.

- Decision: the agent estimates an asset only when the person allowed it
  for that asset, from the description the person entered for it.
  Rationale: an estimate sends a home address to a search provider and to
  web pages. That is the person's choice per asset.
  Date/Author: 2026-09-29.

- Decision: each asset has one currency, and net worth is reported per
  currency and, converted with the finance accounts plan's exchange rates
  at each day's rate, in the reporting currency. An asset in a currency
  with no rate is shown unconverted and left out of the converted total,
  which says so.
  Rationale: the maintainer asked for multiple currencies with conversion.
  Converting each day at that day's rate makes the history honest: a
  foreign account's value in the reporting currency moves with the rate.
  Date/Author: 2026-09-29.

- Decision: `valuation_source` is one name for the asset's usual source and
  the valuation's actual source.
  Rationale: the earlier draft called the first `valuation_method` with the
  same values, two names for one vocabulary.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

The server is Go and the dashboard is React in `web/`. Paths are from the
repository root.

**From the finance accounts plan.** The table `agent_finance_account` holds
each finance account with `account_kind` (depository, credit, loan,
investment, other), `currency_code`, `current_balance` and
`available_balance` (`numeric(19,4)`), refreshed at each sync by
`readFinanceSource` in `internal/agent/ingest_finance.go`, which calls
`ApplyFinanceSync` in `internal/db/database_finance.go`. A finance source is
an agent source of kind `finance`. Balances are as the provider reports
them, which for a card or loan is the amount owed, positive.
`ExchangeRate(from, to, on)` in `internal/db/database_exchange_rate.go`
gives the rate for a day (or the latest earlier one), and the agent row has
`reporting_currency_code`. The `finance` tool
(`internal/agent/tools/finance/finance.go`), the `teanode finance` command
group (`internal/cmd/finance.go`), the **Finance** tab on the agent page,
and the parity tests that check every `FinanceQuery` and `FinanceMutation`
operation has a subcommand and a tool operation all exist.

**Tables and ownership.** Migrations are in `internal/db/migrations/`,
numbered, each with a reverse file. Rows belong to an `agent_id` with a
cascading foreign key and every query filters on it (item SEC-13 in
`docs/security/security-review.md`). The model to copy is `agent_alert`.

**Schedules.** A schedule is a name, a cron line, a prompt and where the
answer goes (`docs/subsystems/jobs-and-schedules.md`). At its time, the
agent takes a turn with that prompt and its tools. Readings and estimates
use schedules; nothing new is scheduled by this plan's code.

**Connected servers.** A person can add an MCP server to their agent
(`docs/subsystems/mcp.md`); its tools appear to the agent under a prefix,
and what they return is marked untrusted.

**Charts.** `web/src/components/usageChart.tsx` draws a column per day
across a range and ranked bars, without a charting package.

## Parity

    what                        GraphQL            dashboard (Finance tab)     subcommand / tool operation
    net worth over time         NetWorth           Net worth chart             net-worth / net_worth
    list assets                 Assets             Assets                      assets
    one asset's history         AssetHistory       asset page                  asset-history / asset_history
    create an asset             CreateAsset        Add an asset                create-asset / create_asset
    change an asset             UpdateAsset        asset page                  update-asset / update_asset
    close an asset              CloseAsset         asset page, Close           close-asset / close_asset
    delete an asset             DeleteAsset        asset page, Delete          delete-asset / delete_asset
    record a valuation          RecordValuation    asset page, Add a value     record-valuation / record_valuation
    delete a valuation          DeleteValuation    history row, Delete         delete-valuation / delete_valuation

Names follow the finance accounts plan's rule (the GraphQL name in
snake_case for the tool, kebab-case for the subcommand).

Create, change, close and record are risk `write`; the two deletes are
`destructive`; the rest `read`. `record_valuation` from the tool sets the
valuation source to `agent_reading` or `agent_estimate`, and from the
dashboard and the command line to `manual`. The parity tests from the
finance accounts plan cover these operations with no new exceptions.

## Plan of Work

### Milestone 1: tables and the net worth query

Migration `0133_agent_net_worth.sql` (after the finance accounts plan's
`0131` and `0132`; take the next free numbers when the work starts):

    agent_asset
      id                   text primary key
      agent_id             text not null, references agent, on delete cascade
      asset_name           text not null
      asset_kind           text not null   cash, investment, retirement, property, vehicle,
                                            other_asset, credit_card, loan, mortgage, other_liability
      is_liability         boolean not null   set from the kind
      currency_code        text not null
      finance_account_id   text unique, references agent_finance_account, on delete set null
      valuation_source     text not null   finance_sync, agent_reading, manual, agent_estimate
      estimate_description text             what the person allowed the agent to search with
      is_estimate_allowed  boolean not null default false
      closed_on            date             sold or paid off; counts until this day
      created_at, updated_at timestamptz

    agent_asset_valuation
      id               text primary key
      agent_id         text not null, references agent, on delete cascade
      asset_id         text not null, references agent_asset, on delete cascade
      valued_on        date not null
      value            numeric(19,4) not null   the size of the thing, positive; is_liability gives the sign
      currency_code    text not null
      valuation_source text not null
      estimate_low     numeric(19,4)
      estimate_high    numeric(19,4)
      valuation_note   text
      evidence_urls    text[]
      created_at, updated_at timestamptz
      unique (asset_id, valued_on, valuation_source)
      index (agent_id, valued_on)

`internal/models/net_worth.go` and `internal/db/database_net_worth.go` add
`NetWorthOperation`: create, update, close and delete assets,
`RecordValuation` (upsert on the unique key), delete a valuation, list an
asset's valuations, and `NetWorthSeries(agentId, from, to,
reportingCurrencyCode)`. The series, for each day in the range, sums over
assets open on that day the valuation that wins (the latest `valued_on` on
or before the day; among that day's rows, by the precedence in the Decision
Log), subtracting liabilities, per currency; then converts each currency's
daily total with that day's exchange rate into the reporting currency and
lists the currencies it could not convert. Write the per-currency part as
one SQL query with a generated series of days and a lateral join per asset;
do the conversion in Go with rates fetched once per range.

Acceptance: database tests prove values carry forward, a late correction
changes past days, a closed asset stops counting after `closed_on`,
currencies are never added without converting, and a currency without a
rate is reported and left out of the converted total.

### Milestone 2: finance sync valuations

Each finance account gets an asset, made in the same database transaction
that first inserts the finance account: `valuation_source` `finance_sync`,
kind from the account kind (depository to `cash`, investment to
`investment`, credit to `credit_card`, loan to `loan`, or `mortgage` when
the provider says so), `is_liability` for the last three, and the finance
account's currency. The person can rename it or change its kind.

At the end of every successful sync, `ApplyFinanceSync` records a valuation
per finance account dated the day of the sync in the person's time zone,
from `current_balance`. Four syncs a day leave one row per day.

Investment and loan balances from Plaid need products beyond
`transactions`. Add `finance.plaid.products` to the operator settings
(default `["transactions"]`, also allowed `investments` and
`liabilities`, in the dashboard form and documented), pass them when
creating link tokens, and for a finance source with `investments` read
`/investments/holdings/get` once a day and use the account's total value as
its balance; for `liabilities`, `/liabilities/get` for the balance owed.
Plaid charges a monthly fee per finance source for each on paid plans, which
is why the operator chooses. SimpleFIN reports investment balances like any
other.

Deleting a finance source sets `finance_account_id` to null on its assets
and their `valuation_source` to `manual`, keeping the history. The person
deletes the asset explicitly if they want the history gone.

Acceptance: with the sandbox institution linked, after a sync each finance
account has an asset and a valuation dated today, and a second sync the same
day leaves one row per finance account.

### Milestone 3: assets and manual valuations everywhere

GraphQL, added to the finance accounts plan's `FinanceQuery` and
`FinanceMutation` in `internal/api/v1api/apigraph/agent_finance.go`, every
resolver starting with `requireAgentPerson`: `Assets`, `AssetHistory`,
`NetWorth(from, to, currencyCode)`, `CreateAsset`, `UpdateAsset`,
`CloseAsset`, `DeleteAsset`, `RecordValuation`, `DeleteValuation`, as in
the Parity table. Audit rows for every change.

Then, in the same milestone, the three ways in: the subcommands in
`internal/cmd/finance.go`, the tool operations in
`internal/agent/tools/finance/finance.go` (results that carry text the
person or a web page wrote are untrusted), and on the Finance tab an
**Assets** table (it stays a table on a phone and scrolls sideways), an
asset page with its history and estimate settings, and **Add an asset**.
Strings in all three catalogs.

Acceptance: create a car at 18,000 dated a month ago and 16,500 today from
the command line; `teanode finance net-worth --since 30d` shows 18,000
until today and 16,500 today; the dashboard and the agent say the same; the
parity tests pass.

### Milestone 4: agent readings through connected servers

When the person asks, for example, "track my brokerage account's value
every day", the agent creates the asset with `valuation_source`
`agent_reading` if it does not exist, and a daily schedule whose prompt is
to call that connected server's portfolio tool and record the account's
total value with `record_valuation`. This recipe goes in the `finance`
tool's description, naming no particular server. A failed reading leaves
the previous value carried forward, and its date shows it is getting old.
`record_valuation` refuses an asset whose valuation source is
`finance_sync`, since those come from the sync.

Acceptance: with a fake connected server in a test (the MCP test helpers in
`internal/mcp` serve one) whose tool returns a portfolio total, a scheduled
turn records a valuation for today with `agent_reading`.

### Milestone 5: agent estimates for houses and cars

For an asset with `is_estimate_allowed`, the agent estimates when asked,
and on a schedule the person accepts when they allow estimates (monthly for
a house, quarterly for a car). The recipe, in the tool's description: search
the web with the web search tool for the `estimate_description`; read two to
four pages that give a value or comparable sales with the fetch tool;
record `agent_estimate` with a low, a high and a middle value, the pages as
`evidence_urls`, and a note saying what the estimate rests on.
`record_valuation` refuses an estimate for an asset without
`is_estimate_allowed`.

Acceptance: with a stubbed search and fetch in a test, a turn for a house
records one `agent_estimate` with a range and evidence URLs; with estimates
not allowed the tool refuses and says why.

### Milestone 6: the chart and documentation

On the Finance tab, a **Net worth** chart of `NetWorth` in the reporting
currency, built on `usageChart.tsx` (pull the shared drawing into a
component both use if that is what it takes), with a note listing any
currency left out for want of a rate.

Documentation: add net worth to `docs/subsystems/finance.md`, the new
subcommands to `docs/reference/command-line.md`, and to the security
review: estimates send the entered description to the search provider and
to the pages read, only for assets where the person allowed it.

## Concrete Steps

From the repository root:

    go test ./internal/db/ -run NetWorth
    go test ./internal/agent/tools/finance/ ./internal/cmd/
    make test
    git add <new files by name>
    make lint-ci
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

## Validation and Acceptance

Accepted when, on a development server with the finance accounts plan
working in Plaid sandbox:

1. Finance accounts appear as assets with a valuation for each day they
   synced, and cards and loans subtract.
2. A car entered at two values on two days shows both in its history and
   the right one in each day's net worth.
3. A correction for a past day changes that day's net worth and the days
   after it until the next valuation.
4. An asset in another currency is converted at each day's rate in the
   reporting currency total, and one without a rate is shown and named as
   left out.
5. A daily schedule records a connected server's account value.
6. An estimate records a range with sources, only when allowed.
7. Deleting a finance source keeps its assets' history as manual assets.
8. The dashboard, the command line and the agent report the same numbers,
   and the parity tests pass.
9. One person cannot read or change another's assets through any API call.

## Idempotence and Recovery

The migration is additive; its reverse drops the two tables. Valuations are
upserted on asset, day and valuation source, so repeated syncs, schedule
runs and entries for the same day replace rather than duplicate. A failed
reading records nothing and the previous value carries forward.

## Artifacts and Notes

One day's net worth, with invented values, reporting currency USD:

    cash         checking      finance_sync, today          USD     4,210.55
    cash         savings       finance_sync, today          EUR     3,000.00   = USD 3,351.60
    investment   brokerage     agent_reading, today         USD    52,380.00
    property     house         agent_estimate, 12 days ago  USD   410,000.00
    vehicle      car           manual, 40 days ago          USD    16,500.00
    credit_card  card          finance_sync, today          USD    -1,944.20
    mortgage     mortgage      finance_sync, today          USD  -287,600.00
    net worth                                               USD   196,897.95

## Interfaces and Dependencies

No new Go modules and no npm packages. By the end of Milestone 3:
`db.NetWorthOperation`, the net worth operations in `FinanceQuery` and
`FinanceMutation`, their subcommands and tool operations.

Revision notes, 2026-09-29: the separate `net_worth` tool became operations
on the shared `finance` tool and the chart reuses the dashboard's
components, after the maintainer asked that the plans reuse existing
concepts. Then, at the maintainer's request: "bank" became "finance";
`valuation_method` merged into `valuation_source` so one vocabulary has one
name; totals convert to the reporting currency at each day's rate; and a
parity table and tests cover the dashboard, command line and tool.
