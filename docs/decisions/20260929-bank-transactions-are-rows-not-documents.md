# Bank transactions are rows, not documents

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

A person is about to be able to link their bank accounts, and the server
will fetch their transactions a few times a day. Everything the agent has
read so far (mail, notes, files, chat archives) goes into the memory graph
as text documents, split into chunks and embedded for similarity search.
Reusing that path would have needed no new tables.

The questions people ask about money are not similarity questions. "How
much went on groceries in August", "what is this charge", "did the rent go
out" are sums and filters over dates, amounts, merchants and categories.
Memory cannot add, and a model asked to add up a few hundred retrieved
chunks gets it wrong.

## Decision

Transactions, and the accounts and connections they belong to, are rows in
tables of their own: `agent_bank_connection`, `agent_bank_account` and
`agent_bank_transaction`, each owned by an agent and deleted with it.
Amounts are exact decimals with a currency code, and a negative amount
always means money leaving the account, whichever provider it came from.

The agent reads them through a read-only tool that searches and totals in
SQL. Nothing is copied into memory. A summary written into memory, or an
alert about a charge, would be a later decision.

This is the kind of data
`20260818-postgres-for-high-volume-data-only.md` keeps in PostgreSQL: it
grows without bound.

## Consequences

A second storage path beside memory, with its own migration, database
layer, source reader and tool. The schedule, the retries and the
credential are the agent source's, as for every other source. Recall does not find transactions: a person who
asks the agent about a purchase gets an answer only if the agent thinks to
call the tool, so the tool's description has to make that obvious.

Every row also keeps the provider's whole object as it arrived, as
`jsonb`. Providers send more than the columns hold, some of it only
discovered in use, and SimpleFIN keeps only about 90 days of history, so
a field not kept at sync time cannot be fetched again later. A later
feature that needs one reads it from what is stored. The cost is
storage (a transaction's object is a few hundred bytes to a kilobyte) and one more
place where data the person did not look at is kept on their behalf.

Totals by category are only as right as the categories. Plaid assigns
its own; SimpleFIN assigns none. Whose categories count is decided in
`20260929-spending-categories-are-the-persons.md`.
