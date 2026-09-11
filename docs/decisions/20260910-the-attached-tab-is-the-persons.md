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
agent's main conversation, and stays visible on their screen for as long as
it is attached; they can detach at any time. Only an interactive
conversation may use it: a scheduled run or a processing run with nobody
present never sees an attached tab. The extension itself — not the model —
refuses to type into a password or card-number field, requires a
confirmation card before submitting a form that is about payment,
credentials or account settings, and reads cookies or storage only for the
site the person attached. The operator can keep tab attachment off for the
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
