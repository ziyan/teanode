# A browser tab the agent drives with the person's session is attached only by that person, only while they watch

- Status: accepted
- Date: 2026-09-10
- Deciders: Ziyan Zhou

## Context

Some of what a person asks for lives behind a web page with no API — a
carrier's tracking page, a portal only they can sign into. The agent can
drive a browser in two ways: a headless one the operator runs beside the
server, which starts every run as a clean visitor with no cookies; or the
person's own tab, attached through a small browser extension, which carries
their signed-in session.

The second is the one that can do real work — download a statement, read a
message centre — and the one that can do real harm, because it holds
credentials the headless browser never has.

## Decision

A tab is attached by the person, from their own browser, to their own
agent, and stays visible on their screen for as long as
it is attached; they can detach at any time. Only an interactive
conversation may use it: a scheduled run or a processing run with nobody
present never sees an attached tab. The extension itself — not the model —
reads storage only for the site of the tab the actions are going to. The operator can keep tab attachment off for the
whole server.

## Consequences

The safety of the attached tab does not depend on the model doing as it is
told: the refusals are in the extension, where a page's text cannot reach
them. The cost is that the extension must be installed, kept in step with
the server, and trusted by the person's browser; it is served by the server
itself and pins the server it came from.

Anything the agent does with the person's session is in the conversation
the person is looking at, one step at a time. There is no unattended use of
a signed-in browser on this server.

## Since (2026-09-11)

The extension used to refuse outright to type into a password or a card
field, and to submit a form that looked like a payment. That is gone. It is
the person's own agent, in their own session, doing what they asked; an
assistant that cannot fill in a form the way they would is not much of an
assistant, and the refusal was being applied to the person rather than on
their behalf. What stands in front of an act they cannot undo is the
confirmation card, which they can put in front of every browser call by
listing `browser` in their own confirm list, and an operator can do the same
for the whole server.

One thing was kept, because it answers a different question: a password
already filled in on the page is still not read back into the conversation.
Typing a secret in is the agent doing its job; copying one out is the
conversation keeping it, and being sent onward with it.
