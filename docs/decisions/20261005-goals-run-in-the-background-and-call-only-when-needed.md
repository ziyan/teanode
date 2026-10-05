# Goals run in the background and call the person only when they are needed

- Status: accepted
- Date: 2026-10-05
- Deciders: the-owner

## Context

An always-on agent is asked for things that take days: keep some mail out of
the inbox, watch for a refund, follow up if a reply does not come. A goal used
to be columns on a conversation, and every turn the agent took toward it was
written into that conversation's transcript, often the main one the person
chats in. A goal that looked every half hour filled the chat with "nothing new
yet", and each of those turns was also relayed to the person's chat apps.
Nothing recorded what a goal had done or made beyond its last note.

## Decision

A goal is a conversation of its own, of the kind `goal`, where its turns run
out of the person's sight. It has a title, a description, a one-line status,
an activity log (`agent_goal_activity`) and the things it made: schedules and
background work found by its conversation, and mail rules, reminders and alert
mutes noted in `agent_goal_artifact`. A goal's turn may start background work,
which wakes the goal's conversation.

The main conversation hears from a goal only when it needs the person: when a
turn waits for them, or the goal stalls. One exchange is written there, under
`[goal needs you]` with the goal's id, once per wait. The person answers there,
and the agent passes the answer on with the goal tool's `tell`; or they answer
on the Goals tab or in the goal's own conversation.

Goals are no longer put on conversations the person chats in. Alerts,
schedules made in the main conversation, speaking first and statement imports
keep posting to the main conversation as before.

## Consequences

The person's chat carries only what needs them, and the Goals tab carries the
rest: what each goal is doing, what it did, and what it made. A goal's
progress is read on the tab, not announced. Starting a goal from the drawer's
goal menu or the API now makes a new conversation rather than changing the
current one. The goal machinery (the job, the caps, the back-off, resuming) is
unchanged; only where its turns run and what it records are new.
