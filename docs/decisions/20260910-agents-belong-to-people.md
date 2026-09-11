# An agent belongs to a person, is opt-in, and is granted sources one at a time

- Status: accepted
- Date: 2026-09-10
- Deciders: Ziyan Zhou

## Context

This server is about to talk to language models on people's behalf: to sort
their mail, summarize it, answer it, and act for them. Two questions had to
be settled before any of that was built. Whose is the agent — the
mailbox's, or the person's? And whose mail may a model see?

A mailbox-shaped agent was the obvious first cut, because the mailbox is
where the mail is. It gives a person with a work mailbox and a personal one
two agents, two standing conversations and two sets of instructions, and it
makes a calendar or an address book — which belong to a person, not a
mailbox — an awkward fit later.

On privacy, a server-wide switch would have been simpler: the operator
configures a provider and every mailbox is processed. It would also have
meant that the operator's decision sent everyone's mail to a third party.

## Decision

One agent per account, made when the person turns it on, holding what is
about them: name, instructions, voice, language, memory, conversations,
schedules, notifications, connected servers. A mailbox is a **source** the
person grants it, each with its own processing policy, because a work
address is not handled like a personal one. Calendars and address books
become sources the same way when they exist.

The agent is off by default. An operator configuring a model provider
enables nothing for anybody. A person turns their agent on, then grants it
sources; nothing from a source that has not been granted is ever sent to a
model, and revoking a source stops its processing at once. An operator may
switch a person's agent off, cap it, and decide which features a deployment
offers at all, but cannot turn anyone's agent on.

## Consequences

Two stored things instead of one: an `agent` row per account and an
`AgentMailbox` policy on each granted mailbox. Everything the agent does is
measured, limited and audited against the person, with the source recorded
beside it.

A person must take two steps — turn it on, grant a mailbox — before anything
happens. The page makes them one step for a person with one mailbox.

Usage and limits are per person. An operator who wanted to budget per
mailbox cannot; the usage view breaks tokens down by source, which is the
question they actually have.

Deleting the account deletes the agent and everything it holds. Deleting a
mailbox forgets that source and keeps the agent.
