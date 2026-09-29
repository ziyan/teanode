# The agent tells the person what their mail says they should know

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

The agent read and sorted every message and said nothing unless asked. A
notice about an illness in a child's class, or a run of sign-in codes
nobody requested, was found when the person next opened their mail, if at
all; the second looks routine one message at a time. Nothing the agent did
on its own reached the chat app the person had linked, only the turns a
chat message started.

## Decision

The agent tells the person, unasked, when their mail shows something they
should know now, in a short message of its own in the main conversation,
which the drawer shows and the linked chat app receives.

What might be worth telling is a candidate: a message the sorting marks
worth telling, or a burst of messages alike that a program counts. A
separate job gathers candidates for two minutes and decides on them in one
call with the person's memory in hand, because deciding to interrupt
somebody needs to know whose school it is, and three messages about one
incident are one alert.

The model decides what to say; the code keeps the bounds: a few a day,
nothing in the person's night unless it cannot wait, nothing twice in a
week, no web address in what is said, and nothing the person muted. The
mail is fenced as untrusted data in the prompt, and the job has no tools.

It is on by default wherever the agent sorts mail. The person can switch it
off for the agent or for one mailbox, move the night and the day's most,
and mute a sender, a domain, a subject or a kind of alert from the
dashboard, the command line or the conversation, all through the same API
operations. A mute is a row the candidate step and the job read, not a
memory the model is shown.

## Consequences

An alert is written from a stranger's text and read as the agent's word,
so the bounds must stay in code: a prompt can be argued with by the message
it reads, a daily count and a stripped link cannot. That is also why a mute
is a row rather than a memory: "stop" holds however the next message is
worded, and a mute can be listed and taken back.

On by default means a person may be told something they did not want
before they knew the feature existed; the switches and the mute are there
for that, and the bounds keep the cost of it to a few messages.

Each decision costs one call on the synthesize model, gathered so that a
busy inbox is a call every couple of minutes at most, and nothing at all
when nothing is a candidate.

Delivery to the chat app goes through the relay that now carries every turn
of the agent's own; web push is left for later, so a person with neither
the dashboard open nor a chat app linked reads the alert when they next
look.
