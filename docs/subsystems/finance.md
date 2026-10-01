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
- **statement**: an OFX file (`.ofx`, `.qfx` or `.qbo`) an institution
  exports, which the person mails in or uploads. The **statement source** is
  the finance source that holds what statements bring in, and the
  **statement import address** is where they are mailed.
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
- **transfer category**: the built-in spending category, one per agent,
  whose transactions moved money between the person's own accounts and
  are neither spending nor income. There is no separate transfer mark.
- **spending rule**, **budget**, **budget pace** (`under`, `on_track`,
  `at_risk`, `over`), **income pace** (`behind`, `on_track`, `ahead`),
  **saving summary** and **saving pace** (`behind`, `on_track`, `ahead`),
  **savings target**, **reporting currency**, **exchange rate**,
  **categorize model**.
- **credit limit** (what a card may owe), **credit usage** (what the cards
  owe against their credit limits) and **credit limit source** (`provider`,
  `derived`, `unknown`).

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

## Imported statements

For an account no provider reaches, such as a card whose issuer only lets
its holder export transactions as a file, a person imports OFX statements.
They go into one finance source per agent, the statement source, of provider
kind `statement`: no credential, no schedule and nothing to poll, so it
changes only when a statement arrives (a sync asked of it does nothing). It
is made the first time the person's import address is asked for (a unique
index, migration 0141, keeps it to one), and it is listed among the finance
sources only once it holds an account. Statements need nothing from the
operator and are offered whenever finance is.

**Reading a file.** `internal/finance/ofx` reads OFX 1.x (SGML: a header
block, then leaves that are never closed) and OFX 2.x (XML) with one
tolerant reader: a leaf ends at the next tag, closed or not. It reads bank
statements (`STMTRS`) and card statements (`CCSTMTRS`): `CURDEF`, the
account (`ACCTID`, and `BANKID` and `ACCTTYPE` for a bank), `DTSTART` and
`DTEND`, `LEDGERBAL` and `AVAILBAL`, and each `STMTTRN`'s `TRNTYPE`,
`DTPOSTED`, `TRNAMT`, `FITID`, `NAME`, `MEMO` and `PAYEE`. A transaction's
`CURRENCY` says its `TRNAMT` is in that currency; its `ORIGCURRENCY` says
`TRNAMT` is already in `CURDEF` and only names the currency the purchase was
made in, which is kept in the transaction's metadata with its rate. Dates
take a zone in brackets, signed or not (`[-5:EST]`, `[0:GMT]`); the day kept
is the day the institution wrote, in its own zone. Amounts may use a comma
as the decimal separator. A file over 10 MB is refused, as is one of more
than two million tags (every tag counts, comments and empty ones too), the
reader's work grows in step with the file's size, and a parse stops when the
request or job that asked for it is cancelled. Nothing in a file is run or
shown, only parsed. `finance.NewStatementImport` turns one statement
into what a sync writes, and the import goes through `ApplyFinanceSync` like
any sync, then through what follows a sync (transfers across the whole
history the statement reaches back over, spending rules, the provider
category mapping, the categorize job, budget alert candidates).

**Signs and balances.** OFX signs amounts from the holder's side, which is
this program's convention already: a card purchase is negative, a payment
to the card and a refund positive. A card's ledger balance is negative for
what is owed, so the account says its owed balance is not positive and the
valuation is the amount owed, recorded on the day the balance is as of. A
statement imported after a newer one records its own day's valuation and
leaves the account's balance at the newer one.

**Categories.** A statement's transaction has the OFX transaction type as
its detailed provider category (`ofx:DEBIT`, `ofx:PAYMENT`) and the side of
the account as its primary (`ofx:creditcard`, `ofx:bank`). The mapping takes
a `PAYMENT` on a card as a transfer (the person paying their own card), on a
bank account as spending; `XFER` as a transfer; `FEE` and `SRVCHG` as fees;
interest as a fee on a card and income on a bank account; cash out as other.
A purchase (`DEBIT`) and a refund (`CREDIT`) say nothing about what was
bought and go to the categorize model with the merchant's name, so a refund
lowers the spending it refunds once it is placed.

**The account and deduplication.** A statement's account is identified by
the statement's kind (bank or card), its `ACCTID` and, for a bank, its
`BANKID`, hashed with a key the statement source keeps sealed: for a bank
the identifier is the account number, and neither it nor anything that could
be turned back into it is stored. The institution is left out, because the
block that names it is optional: one export can carry `FID` and the next
only `ORG`, and keying on whichever was there split one account in two. An
account made while the institution was part of the key keeps its id: a
statement is matched against the source's accounts by keying its `ACCTID`
with the institution each account's metadata names, and the account then
records that institution so the next file finds it whatever its block says. The account is named for the institution (`ORG`) and
keeps the last four letters and digits of the identifier as its mask, to
tell two accounts apart. A transaction is its account and its `FITID`, so
importing a file again, or statements that overlap, adds nothing twice, and
a transaction whose fields changed is updated in place. A transaction
without a `FITID` is known instead by a hash of its day, amount and name,
numbered when two are identical on one day so a genuine second charge is
kept; the import says how many were. Whether an institution keeps a
transaction's `FITID` the same across exports is up to the institution: one
that does not would have its transactions counted twice when overlapping
ranges are exported, and there is deliberately no fuzzy merge by day and
amount, which would swallow genuine same-day duplicates.

**By mail.** The statement import address is the person's first mailbox
address with a detail after a plus: `name+statements-<token>@domain`, the
token sixteen random characters kept sealed on the statement source. Nothing
is added to DNS or to the aliases. `mx`'s `matchAliases` asks first whether
a recipient has that shape and, through the agent (`StatementHook`), whether
the token is the mailbox owner's and their statement source is on. If so,
the message is filed in the mailbox's Archive, read, without rules, triage,
the calendar or an out-of-office reply, and a `statement_import` job is
queued in the delivery transaction, once per message: when the mailbox
holds the message already (it was also addressed to the plain address, or
the person sent it and it is in Sent), no second copy is filed and the
import is still queued. The token is after the last `+statements-`, so a
local part with a plus of its own works. An address with the
`statements-` detail whose local part is a person's mailbox address and
whose token is wrong, whose source is off, or on a server with no agent, is
refused rather than matched like any other address, where a catch-all
would take the statement and the token in its address. A message the spam
filter failed, or that failed DMARC under a quarantine policy, goes to that
person's Junk, unread and not imported; one that failed authentication
outright was refused before delivery. An address with the detail whose
local part names nobody's mailbox is matched like any other. Sending from
the person's own mail program through the submission port works the same
way, since a local recipient goes through the same matching. (From the
dashboard, the Sent copy is filed after the recipients are matched, so a
statement sent there to the person's own import address is also filed in
the Archive.) The job reads
the stored message, imports every part named `.ofx`, `.qfx` or `.qbo` and
every part whose content is OFX however it is typed (a phone sends one as
`application/octet-stream`), records the import on the statement source,
and tells the person in their main conversation, the way an alert is said
but without the alert decision: which account, how many transactions were
added, updated or already there, or why nothing was imported. A notice that
meets a running turn waits a minute and is told then, without importing
again: the source's cursor keeps the last 32 mailed imports by the message
they came from, written in the transaction that imports, so a job run again
tells its own import even when an upload or another message was imported in
between. Regenerating the address (`RegenerateStatementImportAddress`) gives a
new token and the old address stops taking mail at once.

The token is a secret only as far as the address is. It is in the
recipient of every statement mailed to it, so anyone who can read the
person's archived statements, or the domain's delivery records (the mail
audit), can read it and mail statements into the person's finances. If it
may have been seen by somebody it should not have been, regenerating the
address is the remedy.

**Uploaded or pointed at.** `ImportStatement` takes a file uploaded to the
agent's attachments (`agentAttachmentId`, how the dashboard and `teanode
finance import-statement` send it) or a message in the person's mailbox
(`mailboxItemId`, how the agent's `import_statement` sends one the person
points it at). `StatementImport` answers the address, whether importing is
on, and the last import (`lastStatementImport` on the source's cursor).
Switching the statement source off refuses mail to the address and imports
nothing; deleting it removes its accounts and transactions like any finance
source's, and the next look at the address makes a new source with a new
token.

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
transactions upserted by provider id (never overwriting a spending category the
person set, the transfer category included), removals deleted, pending rows replaced, an
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

## Credit usage

Credit usage (`CreditUsage`, `internal/api/v1api/apigraph/agent_finance_credit_usage.go`)
is what the credit cards owe against their credit limits. A credit card is
a finance account of kind `credit`, other than a bank's line of credit
imported from a statement (an OFX bank account of type `CREDITLINE`, which
`finance.IsStatementCreditLine` reads from the account's metadata): it is
kind `credit` so net worth counts it as a debt, but its limit is not a
card's, and added to the cards' limits it would make them look less used
than they are. It reads the accounts' stored balances, not the asset
valuations, so it is as fresh as the last sync or statement. A SimpleFIN
card whose name does not say it is a card is of kind `other` and is not
counted.

**What is owed.** A stored balance keeps the provider's sign:
`finance.IsOwedBalancePositive` says Plaid reports what is owed as
positive, and SimpleFIN and imported statements as negative. Owed is the
balance turned to that convention and never less than zero: a card paid
past its balance owes nothing.

**The limit.** The provider's where it gives one: Plaid's
`balances.limit`, kept in the account's `credit_limit_amount` (migration
0142; null, zero or a negative limit is kept as none, and a limit Plaid
sends that cannot be read does not stop the sync). Otherwise it is derived
as what is owed plus the credit still available (`available_balance`),
taken with the balance's sign, so an overpaid card still comes to its real
limit. A derived limit of zero or less, or an available credit of exactly
zero, which a provider with nothing to say sends more often than a card sits
exactly at its limit, leaves the limit unknown. SimpleFIN's
`available-balance` and a statement's `AVAILBAL` are what make a derived
limit possible for them. Nothing sets a limit by hand yet: the finance
accounts have no edit of their own.

**The summary.** Each card has its owed amount, its limit, where the limit
came from, and its usage share (owed over the limit, 0.25 for a quarter,
more than 1 past the limit) in its own currency, and both amounts in the
reporting currency at today's rate, the way net worth converts today's
balances. The totals add up the cards whose limit and balance are both
known and whose currency converts; the cards left out for want of a limit
or a balance are counted (`leftOutCardCount`) with what they owe, and a
currency with no rate is named (`unconvertedCurrencyCodes`). A card in such
a currency is not counted as left out, since what it owes cannot be added
to the rest. Cards are listed highest usage first.

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
So net worth, savings targets measured by net worth or by assets and
accounts, and each position's history come from the same valuations as any
other asset.

A trade swaps cash for a security inside one account, so it is its own row
in `agent_finance_trade` (`FinanceTrades`, `teanode finance trades`, the
tool's `trades`), never a finance transaction: it is not spending or income,
and it takes no part in budgets, cash flow, spending rules or transfer
detection. The cash that does come and go in an investment account (dividends,
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

Transfers between the person's own finance accounts, and card payments,
count as neither spending nor income. A transfer is a spending category:
the agent's **transfer category** (`is_transfer` on
`agent_spending_category`, migration 0143), built in, one per agent (a
partial unique index), made with every agent. A finance transaction is a
transfer exactly when its spending category is that one, so there is one
source of truth and the two can never disagree: setting the transfer
category marks a transfer, and setting any other takes the mark away. What
put it there is `categorized_by`, the same column as for any spending
category: `person`, `spending_rule`, `provider_category_mapping` (a
provider category that says transfer, such as Plaid's `TRANSFER_OUT` or a
card's OFX `PAYMENT`) or `transfer_detection` (a pair of the same amount
leaving one account and arriving in another, see below). The categorize
model is never offered the transfer category, and the database refuses it
one, since what the model cannot place is spending until something surer
says otherwise.

The transfer category is named `transfer`, shown in the reader's language
like the other built-in names, or `transfer between own accounts` for a
person who already had a spending category called transfer, which stays
theirs. It is found by its flag, never its name, so it can be renamed or
hidden. The word `transfer`, in any case, given to the tool, its
confirmation cards or the command line where a spending category goes, is
the transfer category whatever it is called, ahead of a person's own
`transfer` (still theirs by its id); the dashboard shows the built-in name
in the reader's language only for the flagged category, and a person's own
`transfer` as they wrote it. It cannot be deleted, be income, have a parent
or children, or take a budget.

Transfer detection gives it to the posted finance transactions whose
provider category is a transfer, and pairs money out of one account with
the same amount and currency into another within three days, one to one
and closest first. Neither touches a transaction the person categorized;
pairing may take a side the mapping already marked. A spending rule can
target the transfer category like any other (match `ONLINE PAYMENT`, file
under transfer), for past and future transactions, and a rule does not take
over a transfer pairing or the mapping gave, so deleting the rule leaves
those as they were. A pair is dropped when the amount changes, and a
transfer from a rule or the mapping when the amount or the text changes,
so they are judged again after the sync.

Migration 0143 moved every transaction marked a transfer into the transfer
category, keeping what marked it (`detection` became
`transfer_detection`), and every spending rule that marked transfers onto
it. The spending category such a transaction had beside the mark is not
kept: it counted for nothing while the mark stood. A person's "not a
transfer" became their choice of the spending category the transaction
had, so pairing still leaves it alone. Totals are the same either side of
it, which a test checks.

Three cases the migration handles in a way worth knowing, none of which
the data it was first run on had. A person's "not a transfer" on a
transaction with no spending category becomes their choice of none, so
the categorize model leaves it uncategorized until the person picks one;
leaving it to the model instead would also let pairing mark it a transfer
again, against what the person said. A transaction the person categorized
that a rule or pairing had marked a transfer becomes a transfer by that
rule or pairing, and the person's spending category is lost. And an agent
with both a `transfer` (in any case) and a `transfer between own accounts`
of its own fails the unique name index, so the migration does not run
until one of them is renamed.

Spending means one thing everywhere it is shown (budgets, the day-by-day
chart, cash flow, the Spending section's month chart and summary): money out
less money in for a spending category that is neither income nor the
transfer category, so a refund lowers the spending it refunds, plus money
out with no spending category. Income is what income categories took in,
plus money in with no spending category. The transfer category is in
neither, and every query that leaves transfers out (spending and income
per day, cash flow, budget pace and repeat charges, the saving summary, the
spending summary in every grouping, its currency conversion and the tool's
and the command line's summaries) does so by the category.

A budget is an amount per spending category per month, changed by adding a
row effective from a month. `BudgetStatus` (`internal/agent/budget_status.go`)
converts spending into the budget's currency, projects the month's end
(`budget_pace.go`), and names the budget pace. The projection is a sum of
three amounts: the spending so far, plus the repeat charges still to come,
plus the rest of the spending carried on for the days left at the rate it
has come this month. A repeat charge is a merchant that charged the
spending category in each of the last three full months and has not yet
this month, expected again at its median over those months (`fixedCharges`;
the code and the published fields still call them fixed charges, as in
`fixedChargesDueAmount`, though a shop visited every month is not a fixed
charge). What a repeat charge already took this month is left out of the
rate, since rent on the first is not a pace. Each spending row lists the
repeat charges still expected, merchant and amount, in
`expectedRepeatCharges`, so the dashboard, the command line and the agent
can say what a projection counts. The amounts in the budget's currency add
up to `fixedChargesDueAmount`; one with no exchange rate is in its own
currency and in `unconvertedFixedChargesDue`.

A budget on an income spending category is the income expected each month,
set the same way (`SetBudget` takes any spending category; nothing in the
schema ever refused an income one, it only went uncounted).
`BudgetStatus` lists it in `incomeCategories`, apart from the spending
budgets, against what that category and its children without a budget of
their own took in (`ListIncomeCategoryDays`: money in less money taken
back, transfers left out). Income comes in a few large amounts, so it is
not projected on a straight line: the month in progress is projected to
end at the expected income, or at what came in when that is more already,
and a month that is over at what came in. The income pace compares what
came in with the expected income spread evenly over the days so far:
`behind` under ninety percent of that after the first week (or in a month
that is over), `ahead` past the whole month's expected income by more than
ten percent, `on_track` otherwise. Nothing detects recurring income the way
repeat charges are detected, so a salary paid late in the month reads
`behind` in the days before it lands. Budget alerts read only the spending
budgets.

The saving summary (`SavingSummary`, `internal/agent/saving_summary.go`)
is one month in the reporting currency: the expected saving (every income
budget less every spending budget, converted at the day's rate), the
actual saving (income less spending as cash flow counts them, budgeted or
not, each day at its own rate), and the projected saving (income so far
plus what each income budget still expects, less all spending projected
the way one budget's is). The difference is projected less expected, and
the saving pace is `on_track` within a tenth of the spending budgets
either side, `behind` below that after the first week, `ahead` above it.
A month that is over uses its own figures.

A savings target is measured one of three ways (`target_measure`):
`cash_flow`, income less spending since it started; `net_worth`, net worth
today less the net worth it started from, converted per currency at the
day's rate the way the Net worth section converts it; or `asset_value`, what
the chosen assets and finance accounts are worth today less what they held
at the start. A chosen finance account counts every asset whose
`finance_account_id` is that account, read when the progress is read rather
than copied when the target is saved, so a holding bought later counts and a
brokerage is one choice instead of one per position
(`agent_savings_target_finance_account`, beside the chosen assets in
`agent_savings_target_asset`). An asset chosen on its own and through its
account counts once, and a deleted finance account drops out of the targets
that chose it. A `net_worth` target saved without a starting amount records
the net worth on its starting day, and changing a target's measure clears
the starting amount it had, since one measure's start means nothing to
another. Only an `asset_value` target keeps chosen assets and accounts.

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
`import_credential` only say where to give them. The tool's
`import_statement` takes a message (`mailbox_item_id`) in a mailbox the
person granted the agent, as `mail_read` reads, and never an uploaded file,
whose id the model is not shown.

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

A transfer is chosen like any spending category: in a transaction's row, in
its details (where the line under the choice says what made it a transfer:
pairing, the provider, a rule or the person) and as a spending rule's
target. Every list to choose from offers the transfer category last, under
a heading saying it is neither spending nor income. The list of spending
categories marks it built in and has no delete for it, and its dialog
offers only its name and whether it is hidden. The set-a-budget dialog
leaves it out.

The saving summary is a panel on Spending, for the month chosen there, above
the month's budgets (spending budgets, then income budgets under a heading
of their own), and heads Budgets for this month, where the list and the
"Set a budget" dialog group income categories under Income.

Each bar is what has gone so far, and a faint striped band past it runs to
where the month is heading (`MeterBar` in
`web/src/components/budgetBar.tsx`). A band whose forecast passes the end
reaches the end; on a spending budget whose pace is `at_risk` it turns the
bad tone with a solid end. The line under a spending bar gives the
projection as its sum (spent, repeat charges still to come, the rest at
this month's pace), and an info button opens how it is worked out with the
merchants still expected. Income and saving rows have the same button,
saying how their projection is made. A month that is over has no band and
no explanation: it is its own figures.

The Accounts section opens with **Credit usage**
(`web/src/pages/finance/financeCreditUsage.tsx`), shown only when there is
a credit card: the spending ring with a slice per card owed and one for the
credit still available, the usage share in its middle, owed, available,
limit and the share used beside it, and a table of the cards with their
swatches, owed, limit and share. The ring's label for a screen reader says
the share used and each slice by what it owes, not by its share of the
combined limit, which is not the card's own usage. A share is colored by
the common thresholds: under 30% good, 30% to 50% warn, above 50% bad
(`usageTone` in `creditUsage.ts`), judged to the whole percent it is shown
as. A derived
limit is marked and explained under the table, and the cards left out are
said with what they owe. It sits on Accounts rather than Net worth because it
is read from the accounts' balances and limits, and Net worth is about assets
over time.

The Accounts section ends with **Import statements**
(`web/src/pages/finance/financeStatementImport.tsx`): the import address
with a copy button, how to export from a phone's wallet, changing the
address behind a confirmation, uploading a file, and the last import.
Accounts from statements are marked so in the table.

The **agent page's Finance tab** (`web/src/pages/agentFinance.tsx`,
`/settings/agent/finance`) is the setup: the finance sources (link, repair,
bring an existing connection in, sync, switch, delete) and the settings (the
reporting currency and the converter), in one scroll. `/finance/link`, the
page Plaid's window runs on, comes back to it.
