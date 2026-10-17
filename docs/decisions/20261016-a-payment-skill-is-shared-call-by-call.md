# A payment skill is shared call by call, and runs on its owner's computer

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Payments today go through the `link` skill, which runs `link-cli` on the
person's own computer, signed in to Link as them. The skill never asks for
card numbers and never approves on the person's behalf: every spend
request also waits in the Link app for whoever is signed in to that Link
account. A card raised by an agent can only be answered by the account
that owns the agent.

In a household one person often holds the payment account and others
need to pay with it: a spouse buying groceries, a child asking for a book.
The spouse should be able to pay without asking each time; the child
should ask every time.

Handing the other person the whole skill was rejected. The skill runs on
the owner's computer as the owner, so the other person's agent would read
every balance and transaction and pay without limit, and every check the
skill makes would be the owner's checks run on somebody else's words.

## Decision

A person **shares a skill's tools**, by name, with one person in a circle
they share: for the `link` skill, `link_pay`, and `link_spend_requests`
for the spend requests made through the share. Reading balances and
transactions is not shared unless the owner names those tools too. The
other person's agent sees the shared tools as their owner's ("pay with
<name>'s Link"), and each call runs on the owner's computer, judged as the
owner's own call of that skill would be.

Each share has limits the server checks before anything runs: the most
one payment may be, and the most a month. A call within them goes ahead
(and Link still asks in its app). A call above them, or with an amount
the server cannot read from the arguments (paying an address whose price
only the merchant knows), waits for the owner's **approval**. A share may
require an approval for every call, which is what a parent sets for a
child.

The owner approves or declines from a card in their drawer or their
phone. Approving runs exactly the call that was asked, with the arguments
that were shown, on the owner's computer; nothing else is decided when it
is answered. An approval is answered by the owner and never by an agent,
the same rule that keeps the unattended setting on a card the person
answered, and nobody's unattended allowances answer one.

The owner sees every call made through a share, its arguments and its
outcome; the other person sees their own.

## Consequences

Two tables: `skill_share` (owner, skill, tools, grantee, limits) and
`skill_share_call` (each call, its amount, and its approval when it needed
one). A new card in the owner's conversation that is not a tool call of
the owner's agent: it is answered with `AnswerSkillShareCall`, not
`ResolveAgentConfirmation`.

The server never holds a card and never moves money on its own. A share
works only while the owner's computer is connected, because that is where
the skill is signed in. Who approves in the Link app is whoever is signed
in to that Link account there, which is between the household and Link.

Payment is only as automatic as Link makes it. A purchase that needs an
approval is two answers, the owner's in TeaNode and the Link account
holder's in Link, and is the price of never letting the server, or
somebody else's agent, pay alone.
