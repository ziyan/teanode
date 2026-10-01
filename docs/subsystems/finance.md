# Finance

A person links the institutions they hold money with (a bank, a card issuer,
a brokerage, a lender), and the server keeps their accounts, transactions and
balances in tables of its own. On top of those rows sit net worth over time,
the person's spending categories, budgets and savings targets. The agent reads
and changes all of it through one tool, `finance`; the dashboard shows it on
the Finance page, `/finance`, and sets it up on the agent page's Finance tab;
the command line is `teanode finance`.

The designs and the reasons for them are in
`docs/planning/finance-accounts-execplan.md`,
`docs/planning/net-worth-execplan.md`, `docs/planning/budgets-execplan.md` and
`docs/planning/finance-investments-execplan.md`, and in the decision records
they cite. This document says how it works now.

## Names

One name per thing, everywhere: the tables, Go, GraphQL, the command line, the
tool and the dashboard.

- **provider**: Plaid or SimpleFIN, the outside service that signs in to
  institutions for us.
- **institution**: a bank, card issuer, brokerage or lender.
- **finance source**: one person's link to one login at one institution
  through one provider. It is an agent source of the kind `finance`.
- **finance account** and **finance transaction**: what a finance source
  reports.
- **credential**: the secret a finance source keeps to reach its provider.
- **sync**: one run of a finance source.
- **provider category** (the provider's) and **spending category** (the
  person's) are different things.
- **asset** (anything that counts toward net worth, including what is owed),
  **liability** (an asset whose value subtracts), **valuation** (one value of
  one asset on one day) and **valuation source** (`finance_sync`,
  `agent_reading`, `manual`, `agent_estimate`).
- **finance security** (something an investment account can hold: a share,
  a fund, a bond, a coin), **holding** (an asset that is one security in one
  finance account) and **trade** (a buy, a sell, a cancelled trade or a
  security moved in or out).
- **spending rule**, **budget**, **budget pace** (`under`, `on_track`,
  `at_risk`, `over`), **savings target**, **reporting currency**,
  **exchange rate**, **categorize model**.

## Providers

`internal/finance` holds the provider clients behind one interface,
`finance.Provider` (`Sync`, `Remove`), and nothing else: no database, no jobs.

**Plaid** needs the operator's client id and secret (`agent.finance.plaid`,
the secret sealed like every other provider key). A person links through
Plaid's own window, which runs on the dashboard page `/finance/link`; that
page alone has a security policy that lets it load Plaid's script, frame
Plaid's page and reach Plaid's API. The server exchanges the one-time token
the window returns for the credential and keeps it as a secret of the new
finance source. Syncing pages `/transactions/sync` from the stored cursor.
Plaid signs amounts with money out positive; the client flips them, so a
negative amount is money out everywhere in TeaNode. When the institution
needs the person to sign in again, Plaid answers `ITEM_LOGIN_REQUIRED`; the
finance source stops syncing and asks for **Sign in again**, which repairs the
same finance source rather than linking a new one (a Plaid Trial counts every
link against ten, for good).

**SimpleFIN** needs nothing from the operator. The person makes a setup token
on the SimpleFIN Bridge and pastes it on the agent page's Finance tab or into
`teanode finance link-simplefin`; the server claims it once for the credential, a URL with its
own basic credentials. The bridge caps a request at 90 days and keeps about 90
days of history, so a first sync reads two 45-day windows and later syncs read
from fourteen days before the newest posted transaction, replacing pending
rows in that window. The bridge refuses a request without a `User-Agent`. A
403 means the credential was revoked; the finance source says to delete it and
link again.

**Bringing a connection in.** Instead of linking again, a person can hand
over the credential of a connection made elsewhere (`ImportFinanceCredential`,
"Bring an existing connection" in the dashboard's linking dialog, `teanode
finance import-credential`): a Plaid credential of a link made with the
operator's client id, which saves a Plaid Trial slot, or a SimpleFIN
credential already claimed. The provider is asked first (Plaid's
`/item/get`; one balances-only read of SimpleFIN, whose address must pass
safefetch's checks), a provider reference the person already has is
refused, and the finance source is made the way a link makes one. A failure
never ends the connection at the provider, since it was the person's before.

Every finance account and finance transaction keeps the provider's whole
object, as it arrived, in `provider_metadata`. The columns are what the code
reads; the metadata is there so a field nobody mapped, or a mapping mistake,
can be dealt with from stored data. The tool never returns it.

## Syncing

A finance source is a row of `agent_source` with kind `finance`: its schedule
(`0 */6 * * *`), cursor, last error, on and off switch, sealed secrets, sync
now and deletion are the ones every source has. It does not go through the
document passes: `runIngest` hands it to `runFinanceSync`
(`internal/agent/ingest_finance.go`) before anything about documents or the
knowledge feature switch.

A new finance source is due at once, so its first sync starts within a
minute of the link. A provider can say it is still gathering the history (Plaid
does, in `transactions_update_status`, for a while after a link); the source
then syncs again in five minutes rather than at its next scheduled time, for
its first day at most, and the one pass over the whole history for transfers
waits until the history is complete.

A sync opens the credential, asks the provider for what changed, and writes it
in one database transaction (`ApplyFinanceSync` in
`internal/db/database_finance.go`): finance accounts upserted, finance
transactions upserted by provider id (never overwriting a spending category or
transfer mark the person set), removals deleted, pending rows replaced, an
asset made for each new finance account, and one valuation per finance account
per day from its balance. Then, outside that transaction: transfers are
detected, spending rules applied, the provider category mapping applied to
what is still uncategorized, the categorize job queued for the rest, and
budget alert candidates written.

Deleting a finance source removes it at the provider first (best effort), keeps
its assets' history by turning them into manual assets closed on the day before
the delete, in the person's time zone, and then deletes the source, which
removes its finance accounts and finance transactions. Closing them stops net
worth carrying the last balance forward for an account nothing values any more,
from the day of the delete: an institution moved to the other provider and
linked again that day counts each account once. An asset the person had
already closed keeps its day. Linking the institution again takes back and opens each asset whose account comes back under the same
name, kind, side and currency, when exactly one detached asset matches; any
other account starts a new asset, and none is counted twice. Deleting an agent
removes its assets with everything else.

## Currencies

Every amount carries its currency, and amounts are never added across
currencies without converting. Exchange rates are the European Central Bank's
daily reference rates, kept in the server-wide `exchange_rate` table and
fetched by `internal/finance/rates` when a conversion needs a day the table
lacks: the full history the first time, the 90-day file or the latest day
after that, one fetch at a time across servers. A weekend or holiday uses the
latest earlier rate, and the answer says which day it is from. A currency the
ECB does not publish stays unconverted, and totals name it as left out.

Each person chooses a reporting currency (the agent row's
`reporting_currency_code`; empty means the currency of their first finance
account). Totals are given per currency and, converted at each amount's own
day, in the reporting currency.

## Net worth

`agent_asset` holds everything that counts, and `agent_asset_valuation` its
value per day per valuation source. Net worth for a day is the sum of each open
asset's winning valuation on or before that day (the person's own entry beats
a finance sync or an agent reading, which beat an estimate), liabilities
subtracting; a value carries forward until a newer one replaces it. A
liability's value is the size of what is owed: providers disagree on its sign,
so the sync stores its absolute value.

Accounts reachable only through a connected server are read by the agent on a
daily schedule the person creates, which records the value with the `finance`
tool. Houses and cars are estimated from the web only where the person allowed
it for that asset, with a range and the pages used. Setting an asset to
`agent_estimate` with estimates allowed makes the schedule "Estimate asset
values" (the first of each month, the person's own words, delivered to the
drawer) when there is none, and runs it at once, so the asset has a value the
same day. The schedule is found by name, like the daily brief's: renamed, it is
the person's, and a new one is made beside it; switched off, it stays off
and nothing is estimated until the person switches it on again. Its prompt searches with each asset's estimate description
and nothing else.

## Investments

A Plaid link asks for transactions, and for investments as an optional
product when the operator turns it on (`agent.finance.plaid.products`).
Plaid adds it where the institution and the accounts the person chose have
it, so one link can hold both a checking account and a brokerage account. A
sync reads holdings and investment transactions only for a finance source
whose link has the product (Plaid's `/item/get` says), since reading them for
one without it would start billing for it. A link made before investments
were turned on gets them by being linked again.

Holdings are assets: one per security per finance account, valued at every
sync with the day's quantity, unit price and cost basis on the valuation
(`heldQuantity`, `unitPrice`, `costBasis`), and linked to its security
(`financeSecurityId`, with ticker symbol, name and kind). The finance
account's own asset holds only its cash, valued at the account's balance less
its holdings, because Plaid's balance of an investment account already counts
the holdings. Plaid's cash holdings are that cash, not assets of their own. A
holding no longer reported is valued at zero and closed on that day. When the
holdings cannot be read on a sync, nothing is recorded that day for that
finance source's investment accounts, and the earlier values carry forward.
So net worth, savings targets measured by assets, and each position's history
come from the same valuations as any other asset.

A trade swaps cash for a security inside one account, so it is its own row
in `agent_finance_trade` (`FinanceTrades`, `teanode finance trades`, the
tool's `trades`), never a finance transaction: it is not spending or income,
and it takes no part in budgets, cash flow, spending rules or transfer
matching. The cash that does come and go in an investment account (dividends,
interest, fees, deposits and withdrawals) is a finance transaction like any
other, with Plaid's categories, and goes through the same spending
categories and transfer detection.

## Spending categories, budgets and savings targets

A finance transaction's spending category is the person's, from their own
list, and is assigned in order: the person, a spending rule, the provider
category mapping (`internal/finance/spending_categories.go`: Plaid's
categories and merchant category codes), then the categorize model. The
categorize model is `agent.models.categorize`: a decision model such as Jev
answers one decision per transaction, kept when its confidence is at least
0.6, with the unsure rest sent to a chat model; a chat model answers in
batches of fifty. The model is sent merchant, description, amount, currency,
account kind and provider category, never account numbers or provider
metadata.

Transfers between the person's own finance accounts, and card payments, are
marked and count as neither spending nor income.

Spending means one thing everywhere it is shown (budgets, the day-by-day
chart, cash flow, the Spending section's month chart and summary): money out
less money in for a spending category that is not income, so a refund lowers
the spending it refunds, plus money out with no spending category. Income is
what income categories took in, plus money in with no spending category.

A budget is an amount per spending category per month, changed by adding a
row effective from a month. `BudgetStatus` (`internal/agent/budget_status.go`)
converts spending into the budget's currency, projects the month's end with
fixed monthly charges counted before they land (`budget_pace.go`), and names
the budget pace. Savings targets are measured by cash flow or by what chosen
assets are worth.

Budget and savings target alerts are candidates of kind `budget` in the alert
path every agent alert uses, so they share its daily limit, quiet night hours,
repeat rule and mutes. Each crossing (80 percent of a budget, at risk, over, a
savings target behind) is written once per month, and a spending category can
be muted on its own.

## The three ways in

Every operation is a GraphQL operation in `FinanceQuery` or `FinanceMutation`
(`internal/api/v1api/apigraph/agent_finance.go`), and the dashboard, the
`teanode finance` subcommands and the `finance` tool call it. The tool
operation is the GraphQL name in snake_case with a leading `Finance` dropped,
and the subcommand the same name in kebab-case; parity tests in
`internal/cmd/finance_test.go` and the tool's package check it. Two operations
span two GraphQL calls (`link_plaid` and `repair`, which need Plaid's window
in a browser; the command line and the tool hand the person the page's
address). Two are deliberately missing from the tool: a SimpleFIN setup token
and a credential brought in are refused in conversation, because they would
stay in the transcript and go to the model provider; `link_simplefin` and
`import_credential` only say where to give them.

## In the dashboard

Two places, split by how often a person goes there. The **Finance page**
(`web/src/pages/financePage.tsx`, `/finance/<section>`) is an item in the
mailbox's rail after Knowledge, under the calendar (the account's rail for a
person with no mailbox), shown when the person's agent is on and a
provider is offered or a finance source exists. Its sections are Spending,
Transactions, Accounts, Budgets, Net worth and Savings targets: a row of tabs
on a wide screen, one full-width list to choose from on a phone. `/finance`
alone opens Spending; with nothing linked yet the page says so and links to
the setup.

The **agent page's Finance tab** (`web/src/pages/agentFinance.tsx`,
`/settings/agent/finance`) is the setup: the finance sources (link, repair,
bring an existing connection in, sync, switch, delete) and the settings (the
reporting currency and the converter), in one scroll. `/finance/link`, the
page Plaid's window runs on, comes back to it.
