# A person sees their brokerage holdings as assets, their trades as trades, and their dividends as income

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It builds on three plans that are done and checked
in: `docs/planning/finance-accounts-execplan.md` (finance sources, finance
accounts, finance transactions, the `finance` tool and the `teanode finance`
command group), `docs/planning/net-worth-execplan.md` (assets, valuations,
net worth) and `docs/planning/budgets-execplan.md` (spending categories,
transfers, budgets). What this plan needs from them is repeated under
Context and Orientation.

## Purpose / Big Picture

A person who links a brokerage through Plaid today gets its cash accounts
and nothing else: Plaid Link only offers the accounts that support the
products a link asks for, and the server asks for transactions alone, so a
brokerage account never appears. After this change, linking a brokerage
brings in its investment accounts, and each position in them (for example
twelve shares of one fund) becomes an asset with a value, a quantity, a
price and a cost basis for every day, so net worth follows the market. The
account's own asset keeps only its cash, so nothing is counted twice. Each
buy and sell becomes a **trade**, listed on the position's page: it swaps
cash for a security inside the person's own account and is neither spending
nor income. Each dividend, interest payment, fee, deposit and withdrawal
becomes a finance transaction like any other: dividends and interest are
income, fees are spending, and a deposit from the person's bank pairs with
the bank's withdrawal as a transfer.

How to see it working: with Plaid's investments product turned on in the
operator settings, link an institution that has a brokerage account. The
Finance page's Accounts section lists the investment account; Net worth
lists one asset per position and one for the account's cash, whose values
add up to the account's balance; a position's page lists its trades;
Transactions lists the dividends; the spending chart does not move when a
buy lands. `teanode finance assets`, `teanode finance trades` and `teanode
finance transactions` show the same, and the agent answers "what do I hold"
and "what did I buy this year" through the `finance` tool.

## Names used in this plan

One name per thing, as in the plans this builds on. Added here:

- **security**: something that can be held in an investment account: a
  share, a fund, a bond, a coin, cash. Stored once per agent per provider
  in `agent_finance_security`.
- **security kind**: `cash`, `cryptocurrency`, `derivative`, `equity`,
  `etf`, `fixed_income`, `loan`, `mutual_fund`, `other`.
- **holding**: a position, one security held in one finance account. It is
  an asset with a `finance_security_id`; there is no separate holding
  table.
- **held quantity**: how much of the security a holding had on a day.
- **unit price**: the price of one unit of a security.
- **cost basis**: what the person paid for a holding, as the provider
  reports it.
- **trade**: a buy, a sell, a cancelled trade or a move of a security into
  or out of an account, stored in `agent_finance_trade`. Not a finance
  transaction.
- **trade kind**: `buy`, `sell`, `cancel`, `transfer`. Its **trade
  subkind** is the provider's finer word (for example a split or a
  merger), kept as text.
- **traded quantity**: how much of the security a trade moved.
- **trade amount**: the cash a trade moved in the account, negative when
  cash left it (a buy). **Fee amount**: what the trade cost in fees.
- **investment transaction**: Plaid's word for everything that happens in
  an investment account. The client splits them: trade kinds become
  trades; Plaid's `cash` and `fee` types become finance transactions.

## Progress

- [x] (2026-09-30) Wrote this plan.
- [x] (2026-09-30) Revised: trades moved out of finance transactions into
  their own table.
- [ ] Milestone 1: Plaid asks for investments where the institution has
  them, and reads holdings, trades and investment cash movements.
- [ ] Milestone 2: the migration, securities, holdings as assets and trades
  in the database.
- [ ] Milestone 3: the API, the command line, the tool and the dashboard.
- [ ] Milestone 4: deploy, turn investments on, relink the brokerage, and
  check it in production.

## Surprises & Discoveries

- Observation: Plaid's `/transactions/sync` leaves investment accounts out
  of its `accounts` list, so their balances never reached the server even
  for a link that had them.
  Evidence: Plaid's description of the endpoint; the owner's brokerage link
  showed only its two cash accounts.

- Observation: the SimpleFIN Bridge also sends a `holdings` list per
  account, already kept in provider metadata. Reading it is left for later;
  a SimpleFIN investment account keeps its whole balance on one asset.
  Evidence: the net worth plan's Surprises section.

## Decision Log

- Decision: holdings are assets, one per security per finance account,
  valued at every sync. Two new tables (`agent_finance_security`,
  `agent_finance_trade`) and new columns on `agent_asset` and
  `agent_asset_valuation`; `agent_finance_transaction` is unchanged.
  Rationale: the owner's model ("holdings are assets"). An asset is
  already "something owned with a value on each day", which a position is;
  its history then shows how each position did, and net worth, savings
  targets and the charts need no new path.
  Date/Author: 2026-09-30, the owner and the coding agent.

- Decision: buys, sells, cancels and security transfers are trades in
  their own table, not finance transactions. Dividends, interest, fees,
  deposits and withdrawals are finance transactions.
  Rationale: a first draft put trades among finance transactions, marked
  as transfers with a new origin so spending left them out, and excluded
  them from spending rules and transfer matching. A trade swaps cash for a
  security inside one account; it is not money coming or going, so it has
  no place in spending, budgets or cash flow, and keeping it out of that
  table needs no exception anywhere. Its own facts (security, quantity,
  price, fees) are columns there rather than empty columns on every
  transaction. The owner agreed ("if you think separating trades from
  transaction is cleaner design, i'd go for it"). The cash-moving kinds
  are real money in and out and belong where spending and transfers are
  already worked out.
  Date/Author: 2026-09-30, the owner and the coding agent.

- Decision: an investment account's own asset is valued at its balance
  less the value of its holdings, which is its cash. Plaid's cash holdings
  (security kind `cash`) are not made into assets; they are that cash.
  Rationale: Plaid's balance of an investment account is the whole account,
  holdings included, so valuing both would count every position twice.
  Date/Author: 2026-09-30.

- Decision: when holdings cannot be read on a sync, no valuation is
  recorded that day for that source's investment accounts or holdings;
  earlier values carry forward.
  Rationale: recording the whole balance on the account while the holdings
  carry forward would count them twice for that day.
  Date/Author: 2026-09-30.

- Decision: a holding that is no longer reported is valued at zero on the
  day of that sync and closed on that day; one reported again is opened
  again.
  Rationale: the zero stops the old value carrying forward into the days
  between a sale and a later purchase of the same security; closing keeps
  the list of open assets to what is held.
  Date/Author: 2026-09-30.

- Decision: investments and liabilities are sent to Plaid as optional
  products, and transactions as the one required product.
  Rationale: a required product Plaid Link cannot find at an institution
  hides the institution; an optional one is added where the institution and
  the chosen accounts support it, so a bank without investments still
  links.
  Date/Author: 2026-09-30.

- Decision: the investment transactions that become finance transactions
  are given a Plaid personal finance category from Plaid's type and
  subtype: dividends and interest `INCOME_DIVIDENDS` and
  `INCOME_INTEREST_EARNED`, fees `BANK_FEES_OTHER_BANK_FEES`, deposits and
  contributions `TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS`, withdrawals
  and distributions `TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS`, and
  anything else of type `cash` by its sign as a transfer in or out.
  Rationale: the provider category mapping, transfer matching and the
  categorize model then treat them like any other transaction, with no new
  rule.
  Date/Author: 2026-09-30.

- Decision: Plaid's finance source cursor becomes a JSON object holding the
  transactions cursor and the last day investment transactions were read
  through; a plain string is still read as the transactions cursor.
  Investment transactions are read for 730 days the first time and from
  thirty days before the last read after that.
  Rationale: Plaid has no cursor for investment transactions, only a date
  range, and investments may become ready a few syncs after the link.
  Date/Author: 2026-09-30.

- Decision: trades are read with one new operation, `FinanceTrades`
  (`teanode finance trades`, tool operation `trades`), filtered by finance
  account, security and a day range, paged like transactions. The
  dashboard shows a holding's trades on its asset page.
  Rationale: parity across the three; a person asks about trades per
  position far more than as one list.
  Date/Author: 2026-09-30.

## Outcomes & Retrospective

To be written.

## Context and Orientation

The server is Go and the dashboard is React in `web/`. Paths are from the
repository root.

A **finance source** is an agent source of kind `finance` holding one link
to a provider (Plaid or SimpleFIN). `runFinanceSync` in
`internal/agent/ingest_finance.go` calls the provider's `Sync(ctx,
credential, cursor)` (interface `finance.Provider` in
`internal/finance/provider.go`), which answers a `finance.SyncResult`
(accounts, transactions added, removed, the next cursor), and hands it to
`ApplyFinanceSync` in `internal/db/database_finance.go`. That upserts each
`agent_finance_account`, makes one `agent_asset` for each new finance
account (`createFinanceSyncAsset`), upserts each `agent_finance_transaction`
(`upsertFinanceTransaction`), and records each account's balance as its
asset's valuation for the day (`recordFinanceSyncValuation`). After the
sync, transfers are detected (`DetectFinanceTransfers`), spending rules and
the provider category mapping (`internal/finance/spending_categories.go`,
`MapProviderCategory`) run, and the rest is categorized.

The Plaid client is `internal/finance/plaid.go`. `CreateLinkToken` builds
the request Plaid Link opens with, from the operator setting
`agent.finance.plaid.products` (`internal/config/agent.go`,
`AgentPlaid.Products`), which may hold `transactions`, `investments` and
`liabilities`. `Sync` pages through `/transactions/sync`. Its tests run
against a fake Plaid server in `internal/finance/plaid_test.go`.

Money is `numeric(19,4)` held as decimal strings; negative is money leaving
an account. A **transfer** is a finance transaction with `is_transfer`
true; `transfer_marked_by` says what marked it.

An **asset** (`agent_asset`, migration `0134_agent_net_worth.sql`) has a
kind, a currency, an optional `finance_account_id` (today unique: one asset
per finance account) and a `valuation_source`; its **valuations**
(`agent_asset_valuation`) are one value per day per valuation source. Net
worth sums each open asset's latest valuation on each day
(`NetWorthSeries` in `internal/db/database_net_worth.go`). Deleting a
finance source closes its assets (`DetachAssetsOfSource`), and linking the
same account again takes the closed asset back (in
`createFinanceSyncAsset`).

Migrations live in `internal/db/migrations/`, numbered, each with a
`.reverse.sql`. Production has applied `0136`, so this plan's is `0137`.

Every operation of the finance area is a field of `FinanceQuery` or
`FinanceMutation` in `internal/api/v1api/apigraph/agent_finance*.go`, a
subcommand in `internal/cmd/finance.go` and an operation of the `finance`
tool in `internal/agent/tools/finance/finance.go`; parity tests check the
three agree. The dashboard's Finance page is `web/src/pages/financePage.tsx`
with sections in `web/src/pages/finance/`.

## Plaid, as this plan uses it

`/link/token/create` takes `products` (required) and `optional_products`.
Plaid refuses a token naming a product the operator's Plaid account is not
enabled for with the error code `INVALID_PRODUCT`; the client then asks
again without the optional products and logs that it did, so linking keeps
working.

`/item/get` answers the item's `products`, `billed_products` and
`consented_products`. Investments are read only when `investments` is in
`products` or `billed_products`, which Plaid sets when the product was
added at the link.

`/investments/holdings/get` takes the access token and answers `accounts`
(with balances, as in `/transactions/sync`), `holdings` (`account_id`,
`security_id`, `quantity`, `institution_price`, `institution_price_as_of`,
`institution_value`, `cost_basis`, `iso_currency_code`) and `securities`
(`security_id`, `name`, `ticker_symbol`, `type`, `close_price`,
`close_price_as_of`, `iso_currency_code`, and more that is kept as provider
metadata). The error codes `PRODUCT_NOT_READY`, `NO_INVESTMENT_ACCOUNTS`,
`NO_INVESTMENT_AUTH_ACCOUNTS`, `PRODUCTS_NOT_SUPPORTED` and
`ADDITIONAL_CONSENT_REQUIRED` mean "no holdings this time", not a failed
sync.

`/investments/transactions/get` takes `start_date`, `end_date` and
`options.count` (at most 500) and `options.offset`, and answers
`investment_transactions` (`investment_transaction_id`, `account_id`,
`security_id`, `date`, `name`, `quantity`, `amount`, `price`, `fees`,
`type`, `subtype`, `iso_currency_code`), `securities`, `accounts` and
`total_investment_transactions`. Its `amount` is positive when cash leaves
the account, so it is negated like every Plaid amount. Types `buy`, `sell`,
`cancel` and `transfer` become trades; `cash` and `fee` become finance
transactions.

## Plan of Work

### Milestone 1: Plaid reads investments

In `internal/finance/provider.go`, add to `SyncResult` a list of
`Security`, a list of `Holding`, the provider account ids whose holdings
were read (`HoldingsReadAccountIDs`, empty when none were), and a list of
`Trade` (see Interfaces). `Transaction` is unchanged.

In `internal/finance/plaid.go`: `CreateLinkToken` sends `transactions` as
`products` and the other configured products as `optional_products`, and
retries without them on `INVALID_PRODUCT`. `Sync` reads the JSON cursor,
runs the transactions sync as today, then calls `/item/get`; when the item
has investments, it calls `/investments/holdings/get` and
`/investments/transactions/get` (paging by offset), adds the investment
accounts to the result's accounts, the holdings and securities, the trades
to `Trades`, and the cash and fee investment transactions to `Added` with
their personal finance category set as the Decision Log says, and writes
the new read-through day into the cursor. A "no holdings this time" error
leaves `HoldingsReadAccountIDs` empty and adds a provider warning.

Tests in `internal/finance/plaid_test.go` against the fake server: the link
token's two product lists and the retry; a sync of an item with
investments returns the investment account, the holdings (not the cash
one), the securities, the trades with signs flipped, and the dividends as
transactions with categories set; an item without investments makes no
investments call; `PRODUCT_NOT_READY` is a warning; the old plain cursor
still works.

### Milestone 2: the database

Migration `internal/db/migrations/0137_agent_finance_investments.sql`:

    agent_finance_security
      id, agent_id (cascade), provider_kind, provider_security_id,
      ticker_symbol, security_name, security_kind (checked), currency_code,
      close_price numeric(24,8), close_price_on date,
      provider_metadata jsonb, created_at, modified_at
      unique (agent_id, provider_kind, provider_security_id)

    agent_finance_trade
      id, agent_id (cascade), finance_account_id (cascade),
      finance_security_id (on delete set null), provider_trade_id,
      traded_on date, trade_kind varchar(20) (checked), trade_subkind text,
      traded_quantity numeric(24,8), unit_price numeric(24,8),
      trade_amount numeric(19,4), fee_amount numeric(19,4),
      currency_code, description, provider_metadata jsonb,
      created_at, modified_at
      unique (finance_account_id, provider_trade_id)

    agent_asset
      + finance_security_id references agent_finance_security on delete set null
      the unique finance_account_id becomes unique (finance_account_id,
      finance_security_id) nulls not distinct

    agent_asset_valuation
      + held_quantity numeric(24,8), unit_price numeric(24,8),
        cost_basis numeric(19,4), all nullable

The reverse drops the holdings' assets first, then the columns and the
tables, and restores the old constraint.

In `internal/db/database_finance.go`, `applyFinanceSync` upserts the
securities, then for each account whose holdings were read: upserts one
asset per non-cash holding (taking back a detached one for the same
security, opening a closed one), records its valuation with held quantity,
unit price and cost basis, values the account's own asset at its balance
less its holdings, and values at zero and closes any holding asset of that
account the provider no longer reports. An investment account whose
holdings were not read gets no valuation that day.
`recordFinanceSyncValuation` and `createFinanceSyncAsset` look only at the
account's own asset (`finance_security_id IS NULL`). Trades are upserted
by provider trade id, like transactions; deleting the finance source
deletes its finance accounts and so its trades.

Tests against PostgreSQL in `internal/db/database_finance_test.go` and
`database_net_worth_test.go`: holdings become assets and the account asset
holds the cash, with net worth equal to the account balance; a sold
holding goes to zero and closes, and comes back open; a trade is stored
once however often it is synced and never appears in spending; a dividend
maps to income; the reverse migration runs.

### Milestone 3: the API, the command line, the tool and the dashboard

One operation is added, `FinanceTrades` (`teanode finance trades`, tool
operation `trades`), taking a finance account, a security, `from`, `to`
and `after`. `Asset` gains `financeSecurity` (ticker symbol, security
name, security kind); `AssetValuation` gains `heldQuantity`, `unitPrice`
and `costBasis`. The command line shows them in `assets` and
`asset-history` (and in `--json`); the tool's answers include them. The
dashboard shows a holding's quantity and price beside its value, and on
its asset page the history columns and its trades. The parity tests gain
the new operation. The operator settings text for Plaid's products says
investments and liabilities are asked for where the institution has them.
`docs/subsystems/finance.md` and `docs/reference/command-line.md` describe
it.

### Milestone 4: production

Deploy on top of what the server runs, turn `investments` on in the
operator's Plaid products, delete the brokerage's finance source and link
it again (its two cash assets are taken back by name), sync, and check that
the investment accounts, holdings and trades appear, that net worth rose by
the holdings' value, and that spending did not move.

## Concrete Steps

From the repository root:

    go test ./internal/finance/ -run Plaid -count=1
    TEANODE_TEST_DATABASE_HOST=<test database address> go test ./internal/db/ -run 'Finance|NetWorth' -count=1
    npx --prefix web vitest run --root web
    make lint-ci

## Validation and Acceptance

The tests named in each milestone fail before and pass after. In
production, after the relink: `teanode finance accounts` lists the
investment account; `teanode finance assets` lists its cash and one asset
per position, whose values sum to the account's balance; `teanode finance
trades --finance-account <id>` lists its trades; `teanode finance
transactions --finance-account <id>` lists its dividends; `teanode finance
cash-flow` shows no spending from the trades.

## Idempotence and Recovery

Syncs upsert by provider ids, so running one twice changes nothing. The
migration is additive apart from widening one constraint; its reverse
removes holdings' assets, the new columns and the new tables. Turning
investments off in the settings stops new links asking for it; existing
links keep syncing their holdings until linked again.

## Artifacts and Notes

To be filled as milestones land.

## Interfaces and Dependencies

No new library. In `internal/finance/provider.go`:

    type Security struct {
        ProviderSecurityID string
        TickerSymbol       string
        SecurityName       string
        SecurityKind       string
        CurrencyCode       string
        ClosePrice         string // decimal, "" when unknown
        ClosePriceOn       string // "2006-01-02", "" when unknown
        ProviderMetadata   json.RawMessage
    }

    type Holding struct {
        ProviderAccountID  string
        ProviderSecurityID string
        HeldQuantity       string
        UnitPrice          string
        HoldingValue       string
        CostBasis          string
        CurrencyCode       string
        ProviderMetadata   json.RawMessage
    }

    type Trade struct {
        ProviderTradeID    string
        ProviderAccountID  string
        ProviderSecurityID string
        TradedOn           string // "2006-01-02"
        TradeKind          string // buy, sell, cancel, transfer
        TradeSubkind       string
        TradedQuantity     string
        UnitPrice          string
        TradeAmount        string // decimal, negative is cash out of the account
        FeeAmount          string
        CurrencyCode       string
        Description        string
        ProviderMetadata   json.RawMessage
    }

Revision, 2026-09-30: trades moved out of finance transactions into their
own table after the owner agreed it was cleaner; the reasons are in the
Decision Log.
