# Spending categories are the person's

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

Budgets are amounts per category per month, so the categories under them
have to hold still. The providers do not help much. Plaid assigns its own
categories and sometimes revises them. SimpleFIN assigns none; at most a
card transaction carries the card network's merchant category code. And
households disagree with providers and with each other: the same warehouse
store is groceries to one and home goods to another.

## Decision

A transaction's category is the person's, from their own list, which
starts from a default set when they link their first bank. It is assigned
in this order: the person's own choice, then the person's rules (a merchant
or description match, optionally narrowed by account and amount), then the
provider's category through a fixed mapping, then the agent, in batches,
choosing only from the person's categories. The provider's category is kept
beside it as a hint.

A person's own choice is never overwritten by a rule, a sync or the agent.
Correcting a transaction offers to make a rule, and a new rule applies to
the past except where the person chose by hand.

Transfers between the person's own accounts, and card payments, are marked
as transfers and count as neither spending nor income.

## Consequences

A table of categories and a table of rules per person, and a column on
every transaction for the category and for who assigned it. The rules are
their own table because the only rules TeaNode had are mailbox rules,
which match mail headers.

The agent's batches cost model calls, bounded by how many transactions the
rules and the mapping leave uncategorized; the more rules a person makes,
the fewer calls. What the agent is sent is a merchant, a description, an
amount and an account kind, never an account number.

Changing a default category or the mapping changes nothing already
assigned, so improving the mapping helps only new transactions and ones
nobody has categorized.
