# The agent reaches the calendar and the address book (outline)

An outline, to be written in full as an ExecPlan when its turn comes. It is
plan D of `20260910-personal-agents-roadmap.md` and needs A, B and C.

## Purpose

With a calendar and an address book beside the mailbox, the agent becomes
what a personal assistant is: it notices an appointment in a message and
offers to add it, proposes times from free-busy when somebody asks for a
meeting, fills in a contact from a signature, and sends a briefing each
morning of what the day holds and what is waiting for an answer.

## Shape

New tool families over the sources from B and C — `calendar_*` and the
fuller `contact_*` — with the same risk classes and confirmation protocol as
the rest of the catalog. New run kinds: `extract` (an event or a contact out
of a message, offered in the reader), and the daily briefing as a schedule
template a person turns on with one switch. Triage's `invitation` category
becomes actionable and `scheduling` joins the vocabulary.

Mail as a surface: an alias of kind `agent` delivers into the person's main
conversation and the answer comes back by mail, with the same confirmation
rules — a destructive or outward step answered by mail is a reply the person
sends, never a header the model sets.

## Milestones

1. Calendar and contact tools in the catalog; the situation layer lists them
   as sources.
2. Extract: events and contacts out of mail, offered in the reader.
3. Proposed times and the meeting request.
4. The daily briefing.
5. Mail as a surface.
