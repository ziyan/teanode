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
  **statement import address** is where they are mailed. **Transaction
  rows** are one account's transactions sent instead of a file, such as
  what the agent read off screenshots, imported into the same source.
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
- **other category**: the built-in spending category, one per agent, for
  what fits none of the others. It is what the person (or the categorize
  model) picks for "fits nothing", and it is counted the way no spending
  category was: money out in it is spending, money in it is income.
  **Uncategorized** (no spending category) is not a choice but the state
  of a transaction not decided yet, shown as "Needs a category".
- **mirrored copy**: the same charge reported again on another investment
  account of the same Plaid finance source, a **duplicate** of the
  **counted copy**. What decided it is `mirror_detection` or the `person`.
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
leaves the account's balance at the newer one. A statement with no ledger
balance, which some banks export with the transactions only, leaves the
balance and records no valuation.

**Categories.** A statement's transaction has the OFX transaction type as
its detailed provider category (`ofx:DEBIT`, `ofx:PAYMENT`) and the side of
the account as its primary (`ofx:creditcard`, `ofx:bank`). The mapping takes
a `PAYMENT` on a card as a transfer (the person paying their own card), on a
bank account as spending; `XFER` as a transfer; `FEE` and `SRVCHG` as fees;
interest as a fee on a card and income on a bank account; cash out as the
other category.
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
amount between files, which would swallow genuine same-day duplicates.
Against transactions read off pictures there is one, for the reasons
under **Already there** below.

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

**Read off pictures.** An institution whose app shows the transactions but
exports nothing is imported from screenshots. The person sends them to the
agent in the drawer; the pictures reach the model as image parts in that
turn (`docs/subsystems/context.md`), the agent reads each row itself and
sends them, one account per call, to the `finance` tool's
`import_transactions`, which calls `ImportTransactions` (`teanode finance
import-transactions` takes the same arguments as a JSON file). The
arguments are the account as the list shows it (institution, name or
label, account number, kind `bank`, `card` or `other`, currency, the bank's
code where shown), the rows (day, description as shown, signed amount, the
transaction kind where the list says, the running balance where it shows
one, and the month heading for a list grouped by statement month), the
balance the newest picture shows with its day and zone (the person's when
left out), and a card's monthly totals as shown.
`finance.CheckTransactionRows` checks them, `finance.ChooseTransactionRowsAccount`
chooses the account, `finance.NewTransactionRowsImport` builds an
`ofx.Statement` from them and turns it into what a sync writes exactly as a
file's statement is, and `finance.PlanTransactionRowsImport` leaves out the
rows the account holds already (`ImportTransactionRows`,
`planTransactionRows` and `importStatements` in
`internal/agent/finance_statement.go`), so the account lands in the
statement source, the balance follows the newer-balance rules above, and
transfer pairing, spending rules, the provider category
mapping (from the transaction kind: `payment` is `PAYMENT`, `interest`
`INT`, `fee` `FEE`, `deposit` `DEP`, a purchase or a refund left to the
categorize model) and the categorize job run as after any import. The import
is recorded as the last import with the origin `transaction_rows`.

**The account's number.** The account is keyed like a statement's account:
its kind (`other` is keyed as a bank account, since OFX has no third kind,
and is of kind `other`), the bank code as `BANKID` and the digits of the
number as `ACCTID`, so a later OFX file with that `BANKID` and `ACCTID`, or
later rows with the same digits, land in the same account. The number
takes digits only: spaces, dashes and dots between groups are dropped,
masked digits (`****1234`, `•••1234`) are left out and mark the number
partial, and a letter is refused, since a word in place of a number (a
card's brand, say) would make a mask nobody can read and an account no file
would ever match. A new account made from a partial number is keyed by the
digits shown: later screenshots showing those digits find it, an OFX file
with the whole number is a separate account, and the tool says so. A new
account is named for the institution and its label, and the identifier is
kept only as the four-character mask, as for a file.

**Which account.** A screenshot rarely shows what the account's key was
made from: often only the last digits, sometimes no number, and a card's
OFX export may know the card by an opaque identifier that is not its
number at all, so keying the rows alone would make a second account beside
the one a file made and count every overlapping transaction twice.
`finance.ChooseTransactionRowsAccount` takes, in order:

- the account `financeAccountId` names, which must be one of the agent's
  accounts of imported statements in the rows' currency and of their kind
  (a card for card rows); the number may then be left out. A provider's
  account is refused, since its sync brings its transactions, and an id
  that is not the person's is not found;
- the account the rows' number keys, as a file's or earlier rows' with the
  same number and bank code;
- the one account at the same institution, of the same kind and currency,
  whose mask the shown digits end with (the shorter of the two ending the
  longer; the stored mask, which is the person's digits when they gave
  some). Institutions are compared NFKC, lower case, letters and digits
  only, and one name holding the other matches ("Example Bank" and
  "EXAMPLE BANK, N.A."). Two such accounts are refused, naming each with
  its id. So is one when `isNewAccount` is given: the person said the rows
  are of a new account about the accounts an earlier refusal named, and
  one whose last digits match is more likely the account they are of, so
  a partial number never makes a new account beside it; the agent asks,
  and its id imports into it;
- a new account, when no account of that kind and currency exists at the
  institution or at one not known. An account at the institution whose
  mask does not match is refused, named with its id, rather than passed
  over (the opaque card identifier again), unless `isNewAccount` says the
  person confirmed the rows are of an account not imported before.

An account whose institution is not known (a file without its optional FI
block names none) is no evidence the rows are of it, so an empty name
matches only an empty one and such an account is never chosen by its
mask. It is not passed over either, since nothing says it is at another
institution and passing it over would make a second account beside it: it
is named in the refusals above, one whose mask matches even with
`isNewAccount`, one whose mask does not unless `isNewAccount` is given.

Rows imported into an existing account leave it as it is but for its
balance: its name, mask, kind and metadata are the file's or the person's,
and a balance given is recorded as a statement's is. The tool's recipe is
to call `accounts` first and pass the id of the imported account the
pictures show, and to ask the person when unsure; the refusals are the net
under it.

**Checked before it is written.** Nothing is written unless the rows add
up, and a refusal names the first row or month that does not, by the row's
number as sent, so the agent can read that picture again or ask the
person. Rows may come oldest first or newest first, as a list shows them;
rows whose days go both ways are refused, and when every row is on one day
the balances are tried both ways. Running balances chain: the first row
showing one fixes the opening balance (its balance less the amounts up to
it), and every later one must be the one before plus the amounts since,
exactly, in the currency's minor units (an amount with more places than
its currency has, such as yen with a fraction, is refused, and so is a
thousands separator). A mismatch between two rows that both show a balance
is a misread amount or balance, or rows missing between two screenshots
that do not overlap, and the refusal says how far off it is; one across
rows that show none names both ends of the gap. A card's monthly totals
match when the month's rows come to the total in either sign (a list shows
spending as a positive total over negative rows); a total with no rows, and
rows of a month with no total once totals are given (most often a month cut
off at the bottom of a screenshot), are refused. A description is NFKC and
trimmed (`finance.NormalizeTransactionDescription`): half-width katakana
becomes full-width with its sound marks joined, full-width letters and
digits plain ones, kanji as they are. A day after the person's today, a day
that is not a day and an unknown transaction kind are refused. The tool
runs the same check before its confirmation card; rows that do not add up
are not put to the person (the call is judged a read and answers with the
refusal), and the API checks again, with the person's day, before writing.
All of this is over every row sent, before any is left out as already
there.

**Known by what they say.** A row has no FITID, so a new one is stored the
way a file's transaction without one is: under a hash of its day, amount
and normalized description, numbered by its occurrence among all the rows
sent, so each keeps the identifier it would have if every row were new.
Sending each transaction once per import, overlaps removed, is the agent's
part.

**Already there.** That identifier cannot be what finds a row the account
holds already: a file with FITIDs stored its transactions under the
institution's ids, and a file without them, or earlier screenshots, under
a hash of a description the app may write otherwise (a space, a cut-off
name, full-width letters). So before anything is written the rows are
matched against every transaction the account holds from their first day
to their last, whatever wrote it, by posted day and exact amount (in the
currency's minor units), as multisets: of k rows with a day and amount
that s stored transactions have, the account already holds min(k, s) and
the other k - s are new. Descriptions are not compared. Which of the k are
taken as present changes no count, only which are written: first a row
whose identifier is stored (rows sent before), then one whose description
is a stored one's, then in order. Only the new rows are written; the rows
already there are counted as already here in the import's answer. An
account made by the rows themselves holds nothing, so every row is new.

The same holds the other way: a file imported after rows. Its
transactions are matched by identifier first, as always, so a file
imported again finds its own transactions, and a row whose identifier the
file has is that transaction (updated in place, and the file's from then
on). A file transaction whose identifier is not stored is then matched
against the account's stored rows over the file's days, by posted day and
exact amount, as multisets, after the identifier matches have taken their
rows: of k such file transactions with a day and amount that s rows have,
min(k, s) are already there and are not written, and the import counts
them as already here (`finance.LeaveOutStoredTransactionRows`). A file
transaction left out is not stored, so importing the file again leaves it
out again the same way; a file with two charges of one day and amount
where the rows had one adds exactly one. Only transactions written by
rows are matched this way, never a file's: a row is told by its provider
metadata, where every transaction written by rows carries
`statementImportOrigin` `transaction_rows`, which a file's transaction
never does (it needs no migration, since every transaction keeps its
metadata already). Rows imported before the field was written carry no
such mark and are matched by identifier only. The identifier a file's
transaction without a FITID is given is deliberately not changed to
normalize its name as a row's is: that would change the identifier of
everything already imported and count it twice on the next import.

The day is matched exactly, deliberately. A tolerance of a day either side
would take a genuine repeat charge (the same fare or coffee on consecutive
days) for one already stored, and drop it silently. The cost is the other
way: a source that dates a transaction differently from the file (a card's
app listing the day of use where the export gives the day it posted, a
purchase late at night on either side of a day boundary) leaves the row
looking new. The statement source holds no pending transactions, so the
pending-to-posted switch a provider makes does not arise; the dating
difference is between an app and an export. Such rows are not matched but
pointed out: a new row with an unmatched stored transaction of the same
amount within three days (`finance.NearbyStoredTransactionDays`) is
marked, and the confirmation card says how many there are, for the person
to look at before confirming. The tool's recipe asks for the posted day
where a list shows both. The other limit of matching by day and amount: a
genuine second charge of the same amount on the same day as one already
stored, sent in a later import, is taken as the stored one.

**A dry run.** `PreviewImportTransactions` takes the same arguments and
does all of the above but write (`PreviewTransactionRows`): not even the
statement source is made when there is none. It answers the account (its
id and name, or the name a new one would have), how it was found
(`finance_account_id`, `account_number`, `account_mask`, `new_account`),
the new rows and the rows already there (each by its number as sent, with
`hasNearbyStoredTransaction`), the days and money in and out of the new
rows, what was checked and the balance given, and refuses what the import
would. The import makes the same plan before it writes, then again under
the statement source's lock, so two imports of the same rows at once
cannot both find them new. A refusal the second time is answered as the
first is, and leaves the source's last import and last run as they were,
since nothing was imported. The tool's confirmation card is this preview:
the account, existing (and how it was found) or new, "N new, M already
there", the first three new rows and how many more, the new rows' days and
money in and out, rows near a stored transaction, what was checked and the
balance; an account the server refuses makes a card that says it will be
refused. `preview_import_transactions` gives the agent the same preview,
and `teanode finance import-transactions --dry-run` prints it.

**Renaming and deleting.** An account of imported statements can be
renamed (`RenameStatementAccount`, `teanode finance rename-statement-account`,
the tool's `rename_statement_account`, the pencil on its row under
Accounts): the name is kept in the account's metadata
(`personAccountName`), which every later import, a file's or rows', carries
over in place of the name it would give, and the account's own asset takes
the name too unless the person renamed it. The same operation takes the
last digits of the account's number (`accountMask`, `--account-number`,
the tool's `account_mask`, the dialog's second field), for a source that
gives a word in place of the number, or nothing: 4 to 8 digits, trimmed,
anything else refused. They become the account's `account_mask` and are
kept in its metadata as `personAccountMask`, which every later import, a
file's or rows', carries over in place of the mask it would give, while
the statement's own stays in the metadata as `accountMask`. Since the
account's choice for rows reads the stored `account_mask`, screenshots
showing `****` and those digits find it. An empty value takes the
person's digits back and puts the statement's mask in place again; a
name or digits, at least one, must be given, and the audit row has the
mask before and after. It can be deleted
(`DeleteStatementAccount`, `teanode finance delete-statement-account`, the
trash on its row, after a confirmation): its transactions, its spending
rules limited to it and its place in savings targets go with it, and so do
the assets that value it, history and all. Kept, they would be detached and
still counted in net worth from their last value on, and the account this
is for, one imported under a wrong identifier, would count the same money
twice beside the right one. Pairing keeps no link between a transfer's two
sides, so a transaction on another account that transfer detection paired
with a deleted one is found the way pairing found it: the marked
transactions near the deleted ones are paired again, one to one and
closest first, and a transaction of the opposite amount within the
pairing days of a deleted one is let go of only when it pairs with a
deleted one or with nothing. Checking that paid 500 to one card on one
day and 500 to another the next keeps its second payment paired when the
first card is deleted. What is let go of is uncategorized and
judged again in the same transaction (pairing, spending rules, the provider
category mapping, then the categorize job). Both take the statement
source's lock, as an import does, and both refuse a provider's account,
which its next sync would bring back as it was. Deleting is the person's
alone: the tool's `delete_statement_account` says where to do it and does
nothing, since a model that misread which account a screenshot was of
would delete the right one along with its history. Both are audited as
`finance_account`.

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
budget alert candidates written. Mirrored copies are decided inside the
sync's own transaction, see below.

A posted transaction that replaces a pending one takes, in the same
database transaction and before the pending one is deleted, what was kept
on the pending one (`carryPendingToPosted`): the person's spending category
and duplicate decision, the annotation and who wrote it unless the posted
one has its own, every receipt match with its source, confidence and
matched amount, and the receipt uploads made to the pending charge. A
receipt matcher's match of one receipt to one charge for the whole pending
amount becomes the whole posted amount when the charge posts for a
different amount, but never more than the receipt's total (a tip the
receipt does not print stays unexplained); the person's matched amount is
otherwise kept. When the
charge posts for less than its matches explain (a hold released for less),
they are cut to fit it (`capCarriedReceiptMatches`): the person's first, then
the oldest, each explaining at most what the ones before it left, and one
left with nothing is taken off, its receipt unmatched. Plaid names
the pending transaction on the posted one. SimpleFIN names nothing, so a
replaced pending transaction holding any of these goes to a posted one the
sync inserted on the same account, with the same amount and currency,
within five days, equal charges taken in order.

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

## Annotations and receipts

An annotation is what the person or their agent writes on a finance
transaction ("party supplies"), with `annotatedBy` saying which. The person
replaces or removes any annotation. The agent replaces or removes the
person's only when the person asks in a conversation they are there for: the
finance tool sets `isAskedByPerson` on `AnnotateTransaction` from the run
(one that can put a card to the person, not one started by mail, a schedule
or research, and not a call let through by what the person allows while
away), never from the model's arguments. In a run with nobody present it is
refused. A call from an MCP client is a direct run, which can always put a
card (the person at the harness made the call), so it counts as the person
asking and may replace their annotation, as `agent_profile` treats direct
runs. What the agent writes is `annotatedBy agent` either way. The write
is guarded in its own statement, so a person's annotation written while the
agent's was waiting stands, and the agent's is answered as not written with
no audit event.

A receipt is one merchant's record of one purchase, stored line by line as
printed and checked against its printed totals (`internal/finance/receipt.go`).
A receipt whose lines miss is described by sign, "the lines come to 2.20 USD
less than printed". Receipts are matched to the charges they explain through
`agent_finance_receipt_match`, with the amount of the charge each explains.

The matcher (`ProposeReceiptMatches`) looks at money out in the receipt's
currency posted from three days before the purchase to seven after, and
leaves out a charge its receipts already explain in full. It matches without
asking only the one charge of the exact amount (or the one on the card the
receipt prints, when several have it), and only when that charge is on the
printed card or shares a word of the merchant, and no other receipt is
matched to it. An exact amount alone is a candidate for the person. So an
order email and its shipping email, or a photo and the email of the same
purchase, never both explain one charge on their own; the person may still
match the second by hand for what is left. A receipt recorded before its
charge arrives waits, unmatched: the sync or statement import that brings
the charge weighs it again by the same rule (`matchWaitingReceipts`), and
matches it only to a charge seen for the first time in that sync, the posted
one a pending charge became included. A receipt a match was taken off by
hand is left to the person (`is_left_to_person`), and no sync matches it on
its own again, since a provider that does not link a pending charge to its
posted one (SimpleFIN) brings the posted one as new. The receipt job's prompt records
only a purchase paid with a card or bank account, nothing for a newsletter,
a quote, a bill still to pay or an order paid some other way.

Every match is refused, by hand or by the matcher, when the charge is in
another currency than the receipt, is not money out, the amount has more
places than the currency, the matches from every receipt on that charge
would explain more than it took, or the receipt's matches across all its
charges would explain more than its total. The receipt's row and then the
charge's are locked while the amounts are added up. A match by hand with no
amount explains what other receipts leave of the charge, or what the
receipt's other matches leave of its total when that is less.

`ReadReceipt` refuses an upload to money in before anything is read. A
receipt the job reads from an upload whose match to its charge is refused
(other receipts already explain the charge, or it is in another currency,
which is known only once the receipt is read) is recorded anyway, unmatched
with its candidates, and the run's title says why it was not matched to that
charge. A matcher's match refused because another receipt was matched to the
charge first (two receipt jobs at once) leaves the receipt recorded and
unmatched too. `RecordReceipt` from the API, where the person can correct
the call, still refuses the whole receipt.

`FinanceReceipts` pages as `FinanceTransactions` does: `limit` (at most
200, 50 by default), `offset`, or `after` with the `nextCursor` of the page
before, answering `{ financeReceipts, nextCursor, totalCount }`. The newest
purchase comes first and receipts that print no day come after every dated
one, so with no range every receipt is reached; `isUndated` lists only those,
and cannot go with `from` or `to`, which leave them out.
`text` keeps the receipts whose merchant, receipt number or any line's
description holds the words, in any case, so a receipt is found by what was
bought on it.

A receipt's photo or PDF is an ordinary agent attachment. Every file a
conversation turn carries is named to the model with its
`agent_attachment_id`, so a receipt photographed into the chat is recorded
from that upload, never asked for again. The sweep of
uploads never sent leaves one a receipt was read from, and one uploaded to a
finance transaction and waiting to be read while that transaction exists.
Deleting a conversation leaves a receipt's photo to the receipt
(`DetachReceiptAttachments`); deleting the receipt deletes it unless a
message holds it.

## Mirrored copies

Some providers report one charge on every account of a connection: an
account-level fee from a brokerage reaches each of its accounts, the same
day, the same amount and the same description, each with its own provider
transaction id. It was charged once, so counting each would count it once
per account.

**The rule.** Within one Plaid finance source, finance transactions on two
or more different finance accounts, every one of them an investment
account, with the same day, the same amount and currency, and the same
description (trimmed, in any case) are mirrored copies. One is the counted
copy; each other one is a duplicate of it (`duplicate_of_transaction_id`
on `agent_finance_transaction`, migration 0144, with
`duplicate_decided_by`). Never across finance sources, and never two on
one account: a fee charged twice on one account is two charges, so the
n-th of a day's repeats on one account goes with the n-th on each other
account. A description that is empty groups nothing.

Only investment accounts within one Plaid connection are grouped, nothing
else. Only Plaid: one SimpleFIN credential can reach accounts at several
institutions, and the statement source's accounts are from different
institutions, whose same-day fees are separate charges. Only investment
accounts: a checking and a savings account of one Plaid item can each be
charged the same monthly fee for real, so a set with any other kind of
account in it is not mirrored at all.

The rule cannot tell a mirrored copy from a genuinely identical charge on
two such accounts: two retirement accounts of one connection each charged
the same fee on the same day are marked too, one counted and the other a
duplicate. "Count this one" (below) is the recourse. Copies posted on
different days are not matched, and each counts.

A pending copy is grouped with posted ones as well as pending ones. The
copies of one charge post on different syncs: when one account's copy
posts, the provider replaces its pending row with a posted one, and a
pending copy on another account grouped only with pending ones would be
alone and count beside the posted one until it posted too. Grouped
together, the charge counts once at every sync while its copies post.

**The counted copy** is a posted one before a pending one, so while the
copies post the posted one counts and the pending ones, which the provider
is about to replace, are its duplicates. Among posted copies, or among
pending ones, it is the one stored first, so a copy that arrives later
never takes over; among those one sync stored together, the one on the
oldest finance account (then the lowest ids), so a source's fees land on
the same account month after month. Which account's balance moved would be
better, but nothing a provider sends says so.

**When it runs.** `DetectMirroredFinanceTransactions` decides every
finance transaction of the source afresh in one statement, at the end of
`ApplyFinanceSync`, inside the sync's transaction, so a statement import
runs it too. Deciding the whole source rather than only what the sync
wrote is what lets a set be judged again when a member goes: a counted
copy the provider removed is no longer there to say which rows were its
copies. So a copy whose set no longer holds (a member deleted, or its
amount, description or day changed) counts again, and when the counted
copy goes another member becomes counted. The foreign key sets a copy's
reference to null when its counted copy is deleted, and detection, later
in the same transaction, decides it again. Migration 0144 marked the
copies already stored by the same rule, so nothing waited for a sync.

**The person.** "Count this one" (`CountTransaction`, `teanode finance
count-transaction`, the tool's `count_transaction`) says a duplicate is a
real charge of its own: it counts, `duplicate_decided_by` is `person`,
and detection leaves it out of every set from then on, across syncs that
send it again or change it, and to the posted row that replaces a pending
one it was said of. Counting a transaction that is not a
duplicate is refused, since it counts already and taking it out of its set
would make one of its copies count too. `UndoCountTransaction` forgets
the person's word, and detection decides again at once. There is no "this
is a duplicate of that" from the person: detection is the only thing that
marks a copy.

Detection and the person's word take turns per finance source: each locks
the source's `agent_source` row first (a sync holds it already), so a sync
never writes over a "count this one" made while it ran, and two
detections never wait on each other's rows. Detection's update also
checks again, on the row as it is when written, that the person has not
decided it.

A duplicate is left out exactly where a transfer is (below): spending,
income, cash flow, budgets and their pace and repeat charges, the saving
summary, the spending summary in every grouping and its conversion, and
so the tool's and the command line's summaries. Transfer pairing does not
take one either: the money moved once, on its counted copy. A listing
leaves it out too unless duplicates are asked for (see Listing, below);
asked for, it can be given a spending category, which counts for nothing
while it is a duplicate, so it is neither listed as uncategorized nor
handed to the categorize model.

## Currencies

Every amount carries its currency, and amounts are never added across
currencies without converting. Exchange rates are the European Central Bank's
daily reference rates, kept in the server-wide `exchange_rate` table and
fetched by `internal/finance/rates` when a conversion needs a day the table
lacks: the full history the first time, the 90-day file or the latest day
after that, one fetch at a time across servers. A converter (one per
request or computation) checks the table against today once, the first
time it converts between two currencies, and then reads what is stored, so
a cash flow converting every day of twenty years asks once rather than once
a day. A weekend or holiday uses the
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
accounts have no edit of their own beyond an imported account's name.

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

A spending rule matches words within a transaction's merchant, or its
description when it has none, in any case, and may be limited to one
finance account and to amounts; the first by `rule_priority` wins. The rule
made from one transaction (`CategorizeTransaction` with
`shouldCreateSpendingRule`) matches that transaction's whole merchant, or
description, as it is. A rule matching exactly what one already matches
(the same words in any case, the same account, the same amount limits) is
that rule: saving it again adds no copy, and saving it with another
category gives the one there that category, since a later copy would
never apply. Migration 0145 deleted the copies saved before this.

Several transactions are categorized together with `CategorizeTransactions`
(at most 500 ids): one statement in a savepoint, every one the person's
choice as if chosen one by one, all or none, and an id that is not the
caller's refuses the whole call. Mirrored copies can be among them, as they
can be categorized one by one.

Spending rules for them are proposed first and saved only as confirmed.
`ProposeSpendingRules` (read-only, at most 5000 ids, so a whole selection
is proposed once rather than per piece of 500) takes each distinct match
text among the transactions (merchant, else description, compared in any
case), each its own proposal: a short one never absorbs a longer one, since
a card processor's prefix would then recategorize every charge through it.
A mirrored copy proposes nothing. A match text is left out, and counted,
when it has fewer than four letters or more digits than letters
(`tooGenericMatchTextCount`), or holds a run of six digits or more or a
date (`changingNumberMatchTextCount`), since a rule for a per-charge number
matches nothing again; numbers are never stripped to make a rule, so the
single-transaction rule is unchanged. Coverage follows rule order: a match
text is covered, and left out, only when the rule that applies first to
each of its transactions (`FirstMatchingSpendingRules`, the same matching
`ApplySpendingRules` uses, accounts and amounts included) already sends it
to the chosen spending category; a later rule that would also send it
there does not cover it. Otherwise the proposal names the existing rule it
goes ahead of (`aheadOfSpendingRule`): the earliest rule that now wins for
one of its transactions and sends it elsewhere, or none, after every rule,
when no rule matches them. Each proposal also says how many other
transactions it would recategorize (`changedTransactionCount`,
`CountSpendingRuleChanges`): those it matches that no earlier rule wins
for, whose spending category would change, leaving out the person's
choices, transfers something other than a rule gave, and the selected
transactions themselves, which become the person's choice. At most 50 are
proposed, the most matched first, and the rest counted
(`overLimitMatchTextCount`).

`CategorizeTransactions` takes the confirmed rules (`spendingRules`: match
text, spending category, the rule it goes ahead of) and saves exactly
those; it never proposes again. Each is checked again before anything is
written: the same letter and number limits, at most 50, none twice, and
its spending category the one being given, so a proposal for another
category is refused. In the same savepoint, after the categorizations,
each new rule is placed ahead of the rule it names (`ErrConflict` when that
rule is gone, so the person proposes again): existing rules keep their
priorities where there is room and are pushed back only as far as needed,
in the same order, two that shared a priority still sharing one
(`SetSpendingRulePriorities`, which records each move and does not apply
the rules), and a new rule never shares a priority with a neighbour, since
ties are broken by id. The rules are then applied once after the last is
saved (`CreateSpendingRules`), so the person's choices stand and the rules
reach their other transactions.

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
the categorize model leaves it uncategorized until the person picks one
(migration 0147 later moved every such choice to the other category);
leaving it to the model instead would also let pairing mark it a transfer
again, against what the person said. A transaction the person categorized
that a rule or pairing had marked a transfer becomes a transfer by that
rule or pairing, and the person's spending category is lost. And an agent
with both a `transfer` (in any case) and a `transfer between own accounts`
of its own fails the unique name index, so the migration does not run
until one of them is renamed.

Other is a built-in spending category too: the agent's **other category**
(`is_other` on `agent_spending_category`, migration 0147), one per agent
(a partial unique index), made with every agent and found by its flag,
never its name. It holds what fits none of the person's other spending
categories. It takes a budget like any of them, but it is counted the
way money with no spending category always was: money out in it is its
spending, and money in it is income, not a refund that lowers that
spending. No income budget counts that income, since it belongs to no
income category; cash flow and the saving summary do. It cannot be
deleted, be an income category,
have a parent or have children (`validateSpendingCategory`, with checks
in the table for income and a parent); it can be renamed or hidden. It is named
`other`, shown in the reader's language only while it is the flagged one
under that name, or `anything else` for a person who has an `other` of
their own that could not become it (income, under a parent, or with
children), which stays theirs; with both names taken, a few characters
follow the second. The word `other`, in any case, given to the tool, its
confirmation cards or the command line where a spending category goes,
is the other category whatever it is called, ahead of a person's own
`other` (still theirs by its id). The provider category mapping's other
(cash out at a machine, Plaid's `GENERAL_SERVICES`) goes to it by the
flag.

No spending category is not something a person chooses. A finance
transaction with none (`categorized_by` empty) is one not decided yet:
the categorize model and the rules work on it, and the dashboard's filter
for it and its rows say "Needs a category". The person's "fits nothing"
is the other category. `CategorizeTransaction` and
`CategorizeTransactions` refuse an empty spending category with a message
naming the other category (`isOther`), as do
`SetTransactionCategorization` and `CategorizeTransactionsByPerson` for
the person; the tool refuses `categorize_transaction` with no spending
category or the word none before the person is asked to confirm it, and
the command line refuses `none`. The categorize model is offered the
other category, marked "for what fits none of the others" whatever it is
called, and told it is the answer for a transaction it can read but not
place, rather than leaving it out; leaving one out is for a transaction
it cannot read at all. A hidden other category is not offered, like any
hidden spending category.

Migration 0147 made each agent's default `other` (by the name the
defaults store it under, exactly, top-level, not income, with no
children) the other category, and made one for every agent without, so
the default's transactions, rules and budget stay where they were. Every
transaction the person had given no spending category (`categorized_by`
`person`, no spending category) moved to the other category, still the
person's choice; what was not decided yet stayed uncategorized. The
reverse drops the flag and its checks and keeps the category as an
ordinary one, the one 0147 made included, with the transactions moved
into it, since they cannot be told from ones the person filed there
themselves.

What that move changes in the totals, which a test checks against the
totals before it: money out the person had filed under nothing counted as
spending with no spending category and counts as spending in the other
category, so spending is unchanged and only moves from the uncategorized
group to other, where the budget pace can now find a repeat charge in it.
Money in the person had filed under nothing counted as income and still
does, since the other category counts money in as no spending category
did. Nothing else changes.

Spending means one thing everywhere it is shown (budgets, the day-by-day
chart, cash flow, the Spending section's month chart and summary): money out
less money in for a spending category that is neither income nor the
transfer category, so a refund lowers the spending it refunds, plus money
out in the other category or with no spending category. Income is what
income categories took in, plus money in in the other category or with no
spending category. The transfer category is in
neither, and every query that leaves transfers out (spending and income
per day, cash flow, budget pace and repeat charges, the saving summary, the
spending summary in every grouping, its currency conversion and the tool's
and the command line's summaries) does so by the category. Each leaves out
mirrored copies beside it, by `duplicate_of_transaction_id`; a test checks
the totals with the copies equal the totals with them deleted.

A budget is an amount per spending category per month, changed by adding a
row effective from a month. Setting one drops the later rows that then say
nothing (`dropRepeatedBudgets`), up to the first real change: rows that
repeat the new amount, and, when an existing row was edited in place, rows
that repeated the amount just before them. So moving a budget's start
earlier with the same amount, or changing its amount at its start month,
changes all of it, while a return to an earlier amount after a different
one, or a restart after a budget was ended, is kept. Changes to one
category's budget are serialized by a lock on the category. The Budgets
list names the month a budget's current amount began (a scheduled change
names its own month), and changing a budget opens at that month, saying
when that month is already past that every month since changes. `BudgetStatus` (`internal/agent/budget_status.go`)
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

**A year.** `BudgetStatus` and `SavingSummary` take a calendar year
(`year: "2026"`, `teanode finance budget-status --year 2026`, the tool's
`budget_status` and `saving_summary` with `year`) instead of a month, and
refuse both at once. A year is built from its months
(`internal/agent/budget_year.go`): each month that has begun is that
month's own `BudgetStatus` or `SavingSummary`, so the year never says
another number for a month than the month does, and each month still to
come is the budgets in force for it as they stand today. A budget counts
each month at the amount it had that month, so one raised in July is six
months at each amount, and one ended in May is January to April; its
spending (or income) is that of the months it was in force, and
`budgetedMonthCount` says how many, `firstBudgetedMonth` and
`lastBudgetedMonth` which. `budgetToDateAmount` is the budget for
the days so far: the months that are over whole and the month in progress
spread over its days (a month's status has it too). The projection
(`ProjectSpendingCategoryYear`) is each month that is over as it ended, the
month in progress as its own projection (repeat charges and its pace), and
each budgeted month still to come at the average of those months: the year
carried on the way it has gone, rather than assumed to land on its budget.
With no month begun there is nothing to carry on, and a month to come
counts at its budget. In its first week the month in progress counts at
its budget, or at its spending when that is more already, instead of its
straight line: one dinner on the first projects a month of dinners, and the
year would carry that on into every month to come. The year's saving
counts the month in progress the same way. An income budget's year
(`ProjectIncomeCategoryYear`) counts what came in for the months that are
over, the month in progress's projection, and the income expected of each
month to come; expected by today is the months over and the month in
progress's share by its days. The paces are a month's thresholds over the
year, with the first week of the budget's first month to begin as the
settling days, not the first week of January: a budget that starts in
October may say `at_risk` or `behind` from October 8, as its month may, and
in that week the year's pace is never worse than the month's. A year's rows
are in the currency of the budget's latest month begun, a month in another
currency converted at its as-of day's rate. The year's saving counts only
the months in which at least one budget, income or spending, was in force,
and says which (`budgetedMonths`, `budgetedMonthCount` and
`budgetedMonthsElapsedCount`, the ones begun): expected, actual and
projected all cover exactly those months, so budgets that began in September
are compared with income and spending from September on, never with the
year from January. Within them it is its months' expected, actual and
projected saving added up the same way, the projected spending of the
budgeted months to come at the average of the budgeted months begun, and its
pace waits a week after the first budgeted month begins. A year with no
budget in any month counts all its months, expects nothing, and has no
difference and no pace to speak of (`savingPace` stays `on_track`); the
panel, the CLI and the finance tool all say no budgets were set rather than
comparing with zero. A month's summary names itself in `budgetedMonths`
when it has a budget. The counts are of the spending categories with a
budget in any month. Each month of a year is read in turn, so a year costs
about twelve months' queries.

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
address). One name stands for two operations: `categorize-transaction` and
the tool's `categorize_transaction` call `CategorizeTransaction` for one id
and `CategorizeTransactions` for several (`<id>... <category>` on the
command line, `finance_transaction_ids` in the tool), so a person or a
model categorizing several needs no second word for it. With rules asked
for (`--create-spending-rule`, `should_create_spending_rule`), both call
`ProposeSpendingRules` and send back exactly what it proposed as
`spendingRules`, so the command line and the tool save what the dashboard
would after the person confirmed; the command line prints what it saved and
what was left out, and `propose-spending-rules` shows the list beforehand
with the rule each goes ahead of and how many other transactions each
changes. The tool's confirmation card says how many, names three, and lists
the same proposals with those numbers and what was left out. Two are deliberately missing from the tool: a SimpleFIN setup token
and a credential brought in are refused in conversation, because they would
stay in the transcript and go to the model provider; `link_simplefin` and
`import_credential` only say where to give them. A third is the person's
alone: deleting an account of imported statements, which the tool's
`delete_statement_account` only says where to do. The tool's
`import_statement` takes a message (`mailbox_item_id`) in a mailbox the
person granted the agent, as `mail_read` reads, and never an uploaded file,
whose id the model is not shown; `import_transactions` takes the rows the
agent read off pictures, with the rows' and totals' fields in snake case
like every argument.

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
a heading saying it is neither spending nor income, and the other category
just before it, after the rest of the spending. No list offers no spending
category: a transaction's row, its details and the bar over chosen rows
show "Needs a category" in place of a choice while one is waiting, and the
filter for those reads the same. The list of spending categories marks
the transfer and other categories built in and has no delete for them,
and their dialog offers only the name and whether it is hidden. The
set-a-budget dialog leaves the transfer category out and offers other.

**Listing.** `FinanceTransactions` and `FinanceTrades` answer a page,
newest first, by `offset` and `limit` (at most 200), with `totalCount`,
how many match the filters on every page: one `COUNT` with the same
`WHERE` as the page, without the offset or the cursor. The cursor
(`after`, `nextCursor`) still works beside it and is what the server's own
reads that walk every row use, since a row arriving between two pages does
not move it; the dashboard pages by number, so a page can be linked to and
gone back to. The command line takes `--offset` and prints which rows of
how many it shows; the tool takes `offset` and is told the same in a hint,
to say "50 of 1,234" rather than leave the rest unmentioned.

`FinanceTransactions` leaves the mirrored copies out unless
`isDuplicateIncluded`, as every total leaves them out, and says how many it
left out in `leftOutDuplicateCount` (a second `COUNT`, without the
exclusion). Asking for a counted copy's duplicates (`duplicateOfTransactionId`)
or for transactions by id (`financeTransactionIds`) includes them, which
is how a transaction's details reach them. `teanode finance transactions`
takes `--is-duplicate-included` and says how many it left out; the tool
takes `is_duplicate_included`.

The Transactions list (`web/src/pages/finance/financeTransactions.tsx`) is
a `DataTable` the server pages: the page and its size are in the address
beside the filters, a filter changed starts again at the first page, and
the line above the table says how many match and how many duplicates are
hidden. Show duplicates, among the filters and in the address like them,
lists the mirrored copies, muted, with a Duplicate tag and the amount
struck through. A copy's details say "Duplicate of" the counted copy's
account and day, which opens it (asked for by its id, with
`FinanceTransactions(financeTransactionIds:)`), and offer Count this one;
one the person counted says so and offers to check for copies again. The
counted copy's details name its duplicates the same way. Both actions
answer with a toast, and read again the page shown and what the open
details show. A holding's trades, on its asset's page under Net worth,
page the same way.

A transaction's details end with an icon, Ask the agent, at the far end of
the row with Close (`otherAction` on `ConfirmDialog`), which points the
agent at it the way the mailbox's reader points it at a thread: the dialog
closes, so its scrim does not cover the drawer, and the drawer opens with a
chip such as "Transaction: Jun 9, Corner Grocer, -$42.17", or the agent page
opens when there is no drawer. The chip is a reference of its own kind
(`financeTransactionId` on `AgentReference`, with `postedOn`, `amount`,
`currencyCode`, `merchantName` and `description` for the chip, which the
server fills in again from the stored row). The turn it is sent with
carries a description of the transaction, read with the asking agent's id
and refused or dropped when it is not that agent's, with its id for the
`finance` tool to act on (`docs/subsystems/context.md`, A person's
message).

Transactions are chosen with a box at the start of each row
(`web/src/pages/finance/financeTransactions.tsx`, through `DataTable`'s
selection): shift chooses the run of rows shown since the last box
clicked, and the header's box every row on the page shown. A row clicked
anywhere else still opens its details. The choice is kept by id from page
to page, counted across them, and let go of when a filter changes. Select
all chooses every transaction the filters match, on every page, by reading
their ids alone (`FinanceTransactions` with the cursor, 200 at a time);
more than 5000, the most spending rules can be proposed for, is refused
with a toast rather than cut short. While any are chosen, the row above the table
(`financeTransactionSelection.tsx`) says how many, offers the same list of
spending categories as a row, a box to save them as spending rules, Apply,
and an icon that lets go of them. Apply sends `CategorizeTransactions` 500
at a time; what worked is shown and let go of, and a piece that failed
stays chosen, with a toast saying how many. With the box ticked it first
asks `ProposeSpendingRules` once for the whole selection (more than 5000
chosen is refused with a toast) and lists every rule in a confirmation,
scrolling rather than hiding any: its words, how many of the chosen it
matches, how many other transactions it also changes, and "goes ahead of:
zoomly eats → Dining" when it is placed ahead of an existing rule; then what
was left out and why. Confirming sends those rules once, with the first
piece of 500; if that piece fails they are not saved and its transactions
stay chosen. When nothing is left to propose it categorizes without asking
and the toast says why (existing rules already file them there, or what was
left out). Saved rules can change other rows, so the page shown is read
again.

The saving summary is a panel on Spending, for the month chosen there, above
the month's budgets (spending budgets, then income budgets under a heading
of their own), and heads Budgets for this month, where the list and the
"Set a budget" dialog group income categories under Income.

Spending shows a month or a year. Month | Year and the period, a menu
between a step back and a step forward (`SpendingPeriodPicker` in
`web/src/pages/finance/financeSpendingYear.tsx`), are one row above the
panels, at the right on a wide window and the full width on a phone, in the
same place in both modes so the control never moves. The month menu lists
the months back to the first with cash flow and the year menu the years
with any; both come from one cash flow read over the last twenty years
(`useSpendingHistory`, grouped on the client by `cashFlowYears`), so no
query was added. That read is long for the server (each day converted
where an account is in another currency), so Month mode makes it only once
the person reaches for the menu, and Year mode as it opens. When the first
month read already has cash flow there may be more before it, and the first
year is labelled as where the reading starts ("from Jan, earlier not
shown") rather than where the money began. The period is in the address,
`?month=` or `?year=` (`spendingPeriodFromSearch` in `financeFilters.ts`),
so a year can be linked to; a year still to come, one before the twenty
years read, or one the server refuses (it answers for 1900 through ten
years past this one, and refuses year zero as an invalid argument) falls
back to this month; changing Month to Year or back is a step in the browser's
history and Back returns to it, while choosing another month or year
replaces the address as the month always has. Year to month lands on the
year's latest month begun. In Year mode the section is every year's cash
flow a group a year, from the first year with any to this one (marked as so
far), the chosen year highlighted and each group a button that chooses its
year, with the chosen year's income, spending and what was left
(`yearCashFlowTotals`); the year's saving, naming the months with budgets
it counts; the year's budgets, the months most of them covered said once in
the description and a note only on a row that covered a different number,
with the budget to date and an explanation of the year's projection; and
the spending summary over the year, each group opening Transactions over
the year's days. The current year reads from January 1 to today, and says
so in the chart's caption and the panels' hints. The day-by-day chart is a
month's only, and has no headline of its own: the month's spending is the
cash flow chart's, one card up.

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
with a copy button, how to export from a phone's wallet, that screenshots
can be sent to the agent instead of a file, changing the address behind a
confirmation, uploading a file, and the last import. Accounts from
statements are marked so in the table, and their rows end in a pencil that
renames the account and a trash that deletes it after a `ConfirmDialog`
saying what is lost; both answer with a toast and read the accounts and the
credit usage again.

The **agent page's Finance tab** (`web/src/pages/agentFinance.tsx`,
`/settings/agent/finance`) is the setup: the finance sources (link, repair,
bring an existing connection in, sync, switch, delete) and the settings (the
reporting currency and the converter), in one scroll. `/finance/link`, the
page Plaid's window runs on, comes back to it.
