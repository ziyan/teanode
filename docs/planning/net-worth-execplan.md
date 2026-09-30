# A person tracks their net worth over time, from linked accounts, their own entries and the agent's estimates

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It builds on `docs/planning/bank-accounts-execplan.md`
(bank connections, their accounts and the sync job), which must be done
first, at least through its Milestone 3. What this plan needs from it is
repeated under Context and Orientation.

## Purpose / Big Picture

Tracking spending needs transactions accumulated; the bank accounts plan
does that. Tracking net worth needs something else: what each thing a
person owns or owes was worth, on each day, kept as a history. A balance
fetched today overwrites nothing; it adds a point.

What a person owns and owes comes from three kinds of place, and the design
treats them as one list with different ways of being valued:

1. **Accounts a provider can read**: bank accounts, credit cards, brokerage
   and retirement accounts, loans and mortgages. Their value is fetched
   every day. Some come through a bank connection; others, such as a
   brokerage reached through a connected MCP server (a server the person
   added to their agent that offers tools, as a trading app might), are
   read by the agent on a daily schedule.
2. **Things only the person can value**: a car, a house, a collection, a
   private loan to a friend. The person enters a value and adjusts it when
   they choose.
3. **Things the agent can estimate**: a house from its address and
   description, a car from its make, model, year and mileage. The agent
   searches the web on a schedule the person set, and records an estimate
   with its sources and a range.

After this change, the agent page has a **Net worth** tab: a chart of net
worth over time, the list of assets and liabilities with each one's latest
value, how old that value is and where it came from, and a history per
item. The agent can answer "what is my net worth", "how has it changed
since January", and "what is the house worth now" from new operations of
the `finance` tool the bank accounts plan adds.

How to see it working: link the Plaid sandbox bank (bank accounts plan),
add a car by hand at 18,000, add a house and turn on estimates for it, and
wait a day or run the jobs by hand. The tab shows three kinds of item, each
with a value dated today and its source, and a net worth line with a point
for each day.

Terms used below. An **asset** here is anything that counts toward net
worth, including things owed; a **liability** is an asset whose value
counts against it (a loan, a mortgage, a card balance). A **valuation** is
one value of one asset on one day, with where it came from.

## Progress

- [x] (2026-09-29) Wrote this plan and its decision record.
- [ ] Milestone 1: the two tables, the database layer and the net worth
  query.
- [ ] Milestone 2: daily valuations from bank connections, including
  investment and loan accounts.
- [ ] Milestone 3: manual assets and valuations in the dashboard and the
  API.
- [ ] Milestone 4: net worth operations on the `finance` tool, and daily
  valuations from connected
  servers through a schedule.
- [ ] Milestone 5: agent estimates for houses and cars.
- [ ] Milestone 6: the Net worth tab with its chart, and documentation.

## Surprises & Discoveries

- Observation: none yet. Two things to measure early: how many rows a year
  of daily valuations makes for a typical person (roughly twenty assets
  times 365 is about seven thousand, which is small), and whether Plaid's
  sandbox returns investment holdings and liabilities on a Trial plan's
  sandbox keys.
  Evidence: to be filled.

## Decision Log

- Decision: net worth is computed from a history of valuations, one row per
  asset per day per source, never from a stored net worth figure.
  Rationale: a stored total is wrong the moment one item is corrected
  backwards (the person enters last month's house value late). Summing the
  latest valuation of each asset on or before a day gives the right answer
  for any day, including past ones, from the same rows. Recorded as
  `docs/decisions/20260929-net-worth-is-a-history-of-valuations.md`.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: an asset's value is carried forward until a newer valuation
  replaces it, and every value shows its date.
  Rationale: a house valued in March is still worth about that in June;
  leaving it out of June's total would drop net worth by the price of a
  house. Showing the date lets the person see what is stale.
  Date/Author: 2026-09-29.

- Decision: when one asset has valuations from more than one source on the
  same day, the person's own entry wins over a provider's, and a provider's
  over the agent's estimate.
  Rationale: the person knows what they sold the car for; an estimate is a
  guess. All the rows are kept, so the history shows the disagreement.
  Date/Author: 2026-09-29.

- Decision: accounts behind a connected MCP server are valued by the agent
  on a daily schedule the person creates, which calls that server's tools
  and records the value with the `finance` tool. There is no
  server-side mapping from a particular MCP server's output to a value.
  Rationale: connected servers are arbitrary and their output shapes are
  not ours to depend on. A model reading "total equity" out of a portfolio
  answer is the general solution. It costs one short agent turn a day per
  person who uses it, which the person sees and can stop.
  Date/Author: 2026-09-29.

- Decision: the agent estimates an asset's value only when the person has
  turned estimates on for that asset, and the address or description used
  is the one the person entered for that purpose.
  Rationale: estimating a house sends its address to a search provider and
  to web pages. That is the person's choice per item, never a default.
  Date/Author: 2026-09-29.

- Decision: net worth is reported per currency; nothing is converted.
  Rationale: conversion needs exchange rates from somewhere, stored over
  time, and a choice of base currency. Most people have one currency. A
  later plan can add rates as another kind of valuation history.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

The server is Go and the dashboard is React in `web/`. Paths are from the
repository root.

**What the bank accounts plan provides.** Tables `agent_bank_connection`,
`agent_bank_account` and `agent_bank_transaction`, owned by an agent. A
bank account row has `account_kind` (depository, credit, loan, investment,
other), `currency_code`, and `current_balance` and `available_balance` as
`numeric(19,4)`, refreshed by `readBankSource` in
`internal/agent/ingest_bank.go`, the reader for agent sources of kind
`bank` (a bank connection is an agent source; its schedule, cursor and
credential are the source's). Providers live in `internal/banking` behind the
`banking.Provider` interface. Amounts in that plan are signed so that
negative is money out; balances are as the provider reports them, which for
a credit card or loan is the amount owed, positive.

**Tables and ownership.** Migrations are in `internal/db/migrations/`,
numbered, with a reverse file each. Each table has a model in
`internal/models/` and a database file in `internal/db/`. Rows belong to an
`agent_id` with a cascading foreign key, and every query filters on it
(item SEC-13 in `docs/security/security-review.md`). The model to copy is
`agent_alert` (`0124_agent_alerts.sql`, `internal/db/database_alert.go`).

**Schedules.** A schedule is a name, a cron line, a prompt and where the
answer goes (`docs/subsystems/jobs-and-schedules.md`). At the time it names,
the agent takes a turn with that prompt and its tools. The person creates
one from the dashboard or by asking the agent. This plan uses schedules for
daily readings from connected servers and for periodic estimates.

**Tools.** Each tool is a package under `internal/agent/tools/<name>/`,
registered in `internal/agent/tools/all/all.go`, with a risk class from
`internal/agent/tools/tool.go` (`read`, `write`, `destructive`, `outward`,
`granting`). A write tool that only changes the person's own records is
`write`. Tools call the GraphQL API as the person through
`run.Operations()`. The tool to copy is `internal/agent/tools/note/note.go`.
The web search and fetch tools the agent already has are what it uses to
estimate a value.

**Connected servers.** A person can add an MCP server to their agent
(`docs/subsystems/mcp.md`); its tools appear to the agent under a prefix.
Everything they return is marked untrusted.

## Plan of Work

### Milestone 1: tables and the net worth query

Migration `0131_agent_net_worth.sql` (after the bank accounts plan's
`0130`) creates two tables.

`agent_asset`: `id`, `agent_id` (cascade), `asset_name` (text),
`asset_kind` (text: `cash`, `investment`, `retirement`, `property`,
`vehicle`, `other_asset`, `credit_card`, `loan`, `mortgage`,
`other_liability`), `is_liability` (boolean, set from the kind and stored
so sums need no list), `currency_code`, `bank_account_id` (references
`agent_bank_account`, on delete set null, unique when not null),
`valuation_method` (text: `bank_connection`, `manual`, `agent_reading`,
`agent_estimate`), `estimate_description` (text: the address, or make,
model, year and mileage, that the person allowed the agent to use),
`is_estimate_allowed` (boolean), `closed_on` (date, nullable: sold or paid
off; the asset stops counting after it), timestamps.

`agent_asset_valuation`: `id`, `agent_id` (cascade), `asset_id` (cascade),
`valued_on` (date), `value` (`numeric(19,4)`, always the positive size of
the thing; a liability's sign comes from `is_liability`), `currency_code`,
`valuation_source` (text: `bank_connection`, `manual`, `agent_reading`,
`agent_estimate`), `estimate_low` and `estimate_high` (nullable),
`valuation_note` (text), `evidence_urls` (text array), timestamps. Unique
on `(asset_id, valued_on, valuation_source)`, so a second reading on the
same day from the same source replaces the first. Index on `(agent_id,
valued_on)`.

`internal/models/net_worth.go` and `internal/db/database_net_worth.go` add
create, update, close and delete for assets, `RecordAssetValuation`
(upsert on the unique key), list valuations for an asset, and
`NetWorthSeries(agentId, from, to)`. The series query, for each day in the
range and each currency, sums over open assets the one valuation that
wins: the latest `valued_on` on or before that day, and among rows of that
day the source ranked manual, then bank connection or agent reading, then
agent estimate. Liabilities subtract. Write it as one SQL query with a
generated series of days and a lateral join per asset; test it with a
fixture of three assets whose valuations interleave, a correction entered
for a past day, and a closed asset.

Acceptance: database tests prove the series carries values forward, that a
late correction changes past days, that a closed asset stops counting the
day after `closed_on`, and that currencies are never added together.

### Milestone 2: daily valuations from bank connections

Every bank account the bank accounts plan creates gets an asset, made in
the same database transaction that first inserts the account:
`valuation_method` `bank_connection`, kind from the account kind
(depository to `cash`, investment to `investment`, credit to
`credit_card`, loan to `loan`; mortgage when the provider says so), and
`is_liability` for the last three. The person can rename it or change the
kind afterwards.

At the end of every successful sync, `ApplyBankSync` records a valuation
for each account dated the day of the sync in the person's time zone, from
`current_balance`. Syncing four times a day leaves one row per day, the
last one.

Investment accounts and loans need Plaid products beyond `transactions`.
Add `plaid.products` to the operator settings (default
`["transactions"]`, allowed also `investments` and `liabilities`), pass
them as additional products when creating link tokens, and for an Item
with `investments`, read `/investments/holdings/get` once a day and take
the account's total holdings value as its balance; for `liabilities`, read
`/liabilities/get` for the loan balance. Plaid charges a monthly fee per
Item for each of these on paid plans, which is why the operator chooses.
SimpleFIN reports investment accounts' balances like any other account, so
it needs no change for the value. It also sends an undocumented
`holdings` list per account, which the bank accounts plan keeps in the
account's `provider_metadata`; positions per holding are a later
feature that can read it from there.

Removing a bank connection sets `bank_account_id` to null on its assets and
changes their method to `manual`, keeping their history: unlinking a bank
does not erase what the person was worth last year. The person can delete
the asset explicitly if they want the history gone.

Acceptance: with the sandbox bank linked, after a sync each account has an
asset and a valuation dated today, and a second sync the same day leaves
one row per account.

### Milestone 3: manual assets

GraphQL, in `internal/api/v1api/apigraph/agent_net_worth.go`, with a
`NetWorthQuery` and `NetWorthMutation` in `schema.go`, every resolver
starting with `requireAgentPerson`:

- `Assets` lists the caller's assets with the latest winning valuation and
  its date and source.
- `AssetValuations(assetId, from, to)`.
- `NetWorthSeries(from, to)`.
- `CreateAsset`, `UpdateAsset` (name, kind, estimate description, whether
  estimates are allowed), `CloseAsset(closedOn)`, `DeleteAsset`.
- `RecordAssetValuation(assetId, valuedOn, value, note)` with source
  `manual`; `DeleteAssetValuation(valuationId)`.

These are reachable from the command line as `teanode api call <Operation>`
without extra code. Audit rows for create, update, close and delete.

Acceptance: create a car at 18,000 dated a month ago and 16,500 dated
today; `NetWorthSeries` for the month shows 18,000 counted until today and
16,500 today.

### Milestone 4: the tool, and readings from connected servers

Add these operations to the `finance` tool
(`internal/agent/tools/finance/finance.go`, from the bank accounts plan):

- `net_worth_summary` (risk `read`): net worth today and a chosen number of days ago,
  per currency, with the three largest changes.
- `assets` (read): the list with latest values, dates and sources.
- `asset_history` (read): one asset's valuations.
- `record_valuation` (risk `write`): record a valuation for an existing asset with
  source `agent_reading` or `agent_estimate`, a note, and for an estimate
  its low, high and evidence URLs. It refuses an asset whose method is
  `bank_connection` (those come from the sync) and an estimate for an asset
  whose `is_estimate_allowed` is false.
- `create_asset` (write): create an asset, for the first reading of an account
  that no bank connection covers.

Results are marked untrusted where they carry text the person or a web page
wrote.

Connected server readings. When the person asks, for example, "track my
brokerage account's value every day", the agent creates the asset with
method `agent_reading` if it does not exist and a daily schedule whose
prompt is to call that connected server's portfolio tool and record the
account's total value with the `finance` tool's `record_valuation`. Put this recipe in the
tool's description, so the agent does it the same way every time, and name
no particular server in it. A reading that fails leaves the previous value
carried forward, and the asset's latest date shows it is getting old.

Acceptance: with a fake connected server in a test (the MCP test helpers in
`internal/mcp` serve one) whose tool returns a portfolio with a total, a
scheduled turn records a valuation for today with source `agent_reading`.

### Milestone 5: estimates for houses and cars

For an asset with `is_estimate_allowed`, the agent estimates when asked and
on a schedule the person sets (monthly is a sensible default for a house,
quarterly for a car; offer it when estimates are turned on). The recipe,
written in the tool's description:

- search the web for the address or the vehicle description, as entered in
  `estimate_description`, using the web search tool;
- read two to four pages that give a value or comparable sales, using the
  fetch tool;
- record `agent_estimate` with a low, a high and a middle value, the pages
  it used as evidence URLs, and a note that says what the estimate rests on
  (for example, three comparable sales in the last six months, or a
  listing site's estimate).

The person sees the estimate as an estimate everywhere: the range, the
sources, and the date. A later manual entry on the same day wins; the
estimate stays in the history.

Acceptance: with a stubbed search and fetch in a test, a turn for a house
records one `agent_estimate` with a range and evidence URLs; with
`is_estimate_allowed` false the tool refuses and says why.

### Milestone 6: the Net worth tab and documentation

On `web/src/pages/agent.tsx` add a **Net worth** tab, built with the
components `docs/coding/frontend-design.md` names: a chart of
`NetWorthSeries` (one line per currency), a table of assets (tables stay
tables on a phone; they scroll sideways), each row with name, kind, latest
value, its date and source, and a way to add a valuation; an asset page
with its history and estimate settings; **Add an asset**. Strings in all
three catalogs.

Documentation: add net worth to the subsystem document written by the bank
accounts plan (`docs/subsystems/banking.md`), and to the security review:
estimates send the entered address or description to the search provider
and to the pages read, and only for assets where the person turned it on.

## Concrete Steps

From the repository root:

    go test ./internal/db/ -run NetWorth
    go test ./internal/agent/tools/finance/
    make test
    make lint-ci
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

## Validation and Acceptance

Accepted when, on a development server with the bank accounts plan working
in Plaid sandbox:

1. Linked accounts appear as assets with a valuation dated each day they
   synced, and credit cards and loans subtract.
2. A car entered by hand at two values on two days shows both in its
   history and the right one in each day's total.
3. A correction entered for a past day changes that day's net worth and the
   days after it until the next valuation.
4. A daily schedule records a connected server's account value.
5. An estimate for a house records a range with sources, only when allowed.
6. Unlinking a bank keeps its assets' history, as manual assets.
7. One person cannot read or write another's assets through any API call.

## Idempotence and Recovery

The migration is additive, and its reverse drops the two tables. Valuations
are upserted on asset, day and source, so repeated syncs, repeated
schedule runs and repeated manual entries for the same day replace rather
than duplicate. A failed reading records nothing and the previous value
carries forward.

## Artifacts and Notes

A day's net worth, with invented values, and how it is built:

    cash           checking (bank_connection, today)       4,210.55
    investment     brokerage (agent_reading, today)       52,380.00
    property       house (agent_estimate, 12 days ago)   410,000.00
    vehicle        car (manual, 40 days ago)              16,500.00
    credit_card    card (bank_connection, today)          -1,944.20
    mortgage       mortgage (bank_connection, today)    -287,600.00
    net worth USD                                         193,546.35

## Interfaces and Dependencies

No new Go modules and no new npm packages. The dashboard already draws charts without a charting package:
`web/src/components/usageChart.tsx` draws a column per day across a range
and ranked bars, and `web/src/components/budgetBar.tsx` draws an amount
against a limit in the color of how near the limit it is, using
`budgetNearness` from `web/src/components/common`. Build on those (pull
the shared drawing into a component both use, if that is what it takes)
rather than adding a charting package.
`db.NetWorthOperation`, `NetWorthQuery`, `NetWorthMutation` and the
`finance` tool's net worth operations must exist by the end of
Milestone 4.

Revision note, 2026-09-29: after the maintainer asked that the plans
reuse existing concepts, the separate `net_worth` tool became operations
on the shared `finance` tool, bank balances are described as coming from
the `bank` source reader, and the chart reuses the dashboard's own chart
components. Schedules were already the mechanism for readings and
estimates.
