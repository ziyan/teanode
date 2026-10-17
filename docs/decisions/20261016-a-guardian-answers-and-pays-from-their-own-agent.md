# A guardian answers a child's request, and pays from their own agent

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Payments today go through the `link` skill, which runs `link-cli` on the
person's own computer, signed in to Link as them. The skill never asks for
card numbers and never approves on the person's behalf: every spend
request also waits for the person in the Link app. A card raised by an
agent can only be answered by the account that owns the agent.

A parent wants a child to be able to ask for something to be bought, and
to say yes or no to it, without handing the child their card.

The literal reading, giving the child's agent the parent's Link, was
rejected. The skill runs on the parent's computer as the parent, so the
child's agent would act with the parent's whole Link account, and every
check the skill makes would be the parent's checks run on the child's
words.

## Decision

A child asks; a guardian answers; the guardian's own agent pays.

An **approval** is a request from a child in a family circle to its
guardians, of one **approval kind**: `purchase` first, and then the other
things a child may not do alone (joining a circle, sharing outside the
family, widening what their agent does unattended). A child makes one from
any surface, or their agent makes one for them on a card the child
answers. Each guardian sees it as a card in their main conversation and on
their phone, and any one of them answers it.

A guardian answers a purchase in one of three ways: decline, with a note;
approve and buy it themselves; or approve and have their agent buy it. The
last starts a turn in the guardian's main conversation, with the approval
as its message, using the guardian's own tools on the guardian's own
devices: the Link skill as it is, which Link then asks the guardian about
again. The child's agent hears the outcome in the conversation the request
came from, as background work does.

An approval is answered by a guardian and never by an agent, the same rule
that keeps the unattended setting on a card the person answered. The
guardian's unattended allowances do not answer an approval.

A guardian may give a child a monthly **allowance**: an amount the child's
approved purchases are counted against, shown to both. It does not
approve anything by itself in this design.

## Consequences

One table, `circle_approval`, its own operations on every surface, and a
new card in the guardian's conversation that is not a tool call of the
guardian's agent: it is answered with `AnswerCircleApproval`, not
`ResolveAgentConfirmation`.

The child never sees how the guardian pays, and the server never holds a
card. A guardian without a computer running the Link skill can still
approve and buy it themselves.

Payment is only as automatic as the guardian's Link makes it. Approving
inside TeaNode and then again inside Link is two taps, and is the price of
never letting the server, or a child's agent, pay alone.
