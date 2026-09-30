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
layer, sync job and tool. Recall does not find transactions: a person who
asks the agent about a purchase gets an answer only if the agent thinks to
call the tool, so the tool's description has to make that obvious.

The provider's own fields beyond the normalized ones are not kept. If a
later feature needs one, it is a migration and a fresh sync, which the
providers allow.

Totals are only as right as the categories the provider assigns. Plaid
categorizes; SimpleFIN does not, so its transactions have no category until
something assigns one.
