# A schedule answers in the conversation it was made in

- Status: accepted
- Date: 2026-09-23
- Deciders: the-owner

## Context

A schedule that answered in the drawer ran its turn in a transcript of its
own and copied the answer into the main conversation under a note. The
agent's reminder therefore knew nothing of the conversation it was asked for
in, and its answer arrived in a different place from the question. The goal's
check-in and the wake of a background command already take their turns in the
person's own conversation, marked as not being the person, and read as part of
it.

## Decision

A schedule remembers the conversation it was made in. When one that answers
in the drawer is due, it takes a headless turn there, opened by a message
marked `[schedule]`, and what the agent says is the answer, where it is read.
One made from the dashboard or the command line, or whose conversation has
since gone, uses the main conversation. A schedule that answers by mail keeps
its own transcript, since its answer is read in the mail.

## Consequences

A reminder set in a side conversation about invoices comes back in that
conversation, with its context. The marker is added to the ones that are not
the person's last word, so a woken turn does not count as the person writing.

The turn is headless as before: nobody is present, and anything needing the
person's confirmation is refused, so a schedule does no more in a
conversation than it did in its own transcript.

A schedule's turn in a busy conversation waits for the turn in progress there,
because a conversation takes one turn at a time.
