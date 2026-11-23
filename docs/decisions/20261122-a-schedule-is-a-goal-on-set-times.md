# A schedule is a goal on set times

- Status: accepted
- Date: 2026-11-22
- Deciders: the-owner

## Context

Two things made the agent act with nobody asking: a schedule (a name, a cron
line or the mail it waits for, a prompt, and where the answer goes) and a goal
(a conversation of its own that the agent keeps at until it is met). Each had
its own table, tool, operations, command group and screens. Where they met they
duplicated each other: a goal whose work was on a clock owned a schedule, took
no turns of its own while it had one, and two rows said when the agent looked
next. The goal tool had `schedule_ids` and `take_schedule` only to keep them in
step, and most schedules on a working server ended up owned by a goal.

## Decision

There is one thing, the goal. A goal takes its turns at a pace of its own, as
before, unless it has times (`goal_times`: cron lines in the person's zone, or
`@at` and a moment) or mail it waits for (`goal_mail_conditions`). Then the
worker applies them: the next turn is the soonest time whatever the turn asked
for, a moment is dropped once it has had its turn, and the day's caps do not
apply, since the person chose the cadence. A goal can be paused (`paused`), and
whoever wrote it (`goal_written_by`) decides whether its turn reads its
description as the person's words and whether it may use Passwords, as a
schedule's writer did. What it tells the person goes to the feed; mail as a
place for answers is gone. Migration 0281 makes every schedule a goal and drops
`agent_schedule`. The daily brief and the asset estimates are goals their
switches start.

A time the person chose stays a rule the worker applies rather than something
the model works out each turn. The rule for what is stored is what the platform
must apply without a model, or what the person must see and change; "weekdays
at 7:30" is both, and a brief that came at 8:12 because a model counted wrong
would be a regression a person notices.

## Consequences

- One list on every screen, one tool, one set of operations, one command group.
- A schedule that answered in the conversation it was made in now answers in
  the feed, and its turns run in its goal's own conversation, so its turns see
  the ones before.
- The forty-eight and twenty-four turn caps bound only goals at their own pace.
  Replacing them with the spending limit is a separate change.
- A goal that owned two schedules waiting for mail keeps the first one's
  conditions; the others' words stay in its description.
