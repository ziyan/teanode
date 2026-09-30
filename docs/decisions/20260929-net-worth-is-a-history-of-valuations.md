# Net worth is a history of valuations

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

A person tracking net worth needs to know what each thing they own or owe
was worth over time, not only today. Some of those things a provider can
read every day (bank accounts, cards, brokerage accounts, loans). Some only
the person can value (a car, a private loan). Some the agent can estimate
from the web (a house from its address, a car from its description). The
values arrive at different rates, from different places, and are sometimes
corrected after the fact.

Storing a net worth figure per day was the simple option. It is wrong as
soon as one item is corrected for a past day, and it cannot say which item
moved.

## Decision

Every thing that counts is an asset, and a liability is an asset whose
value subtracts. An asset's value is recorded as a valuation: one value,
on one day, from one source (the bank sync, the person, an agent reading
through a connected server, or an agent estimate with its range and
evidence). Net worth for any day is the sum, per currency, of each open
asset's latest valuation on or before that day, where the person's own
entry beats a provider's, and a provider's beats an estimate.

Values are carried forward until replaced, and every value shows its date
and source. The agent refreshes readings and estimates through ordinary
schedules the person can see and stop. It estimates an asset only when
the person turned that on for the asset, because an estimate sends the
address or description to a search provider and to web pages.

Nothing is converted between currencies.

## Consequences

One row per asset per day per source. A household with twenty assets makes
about seven thousand rows a year, which PostgreSQL does not notice, but a
series query over years has to be written with care.

A value carried forward can be stale without anyone noticing, so staleness
is shown rather than hidden, and a reading that fails leaves the old value
standing rather than dropping the asset from the total.

Readings through a connected server cost one short agent turn a day for
the person who asks for them. The alternative, a mapping from one
particular server's output to a number, would break whenever that server
changed.

A person with money in two currencies sees two totals until exchange
rates are added.
