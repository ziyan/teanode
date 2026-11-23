# Goals spend a share of the day, and fewer switches

- Status: accepted
- Date: 2026-11-23
- Deciders: the-owner

## Context

After schedules became goals (`20261122-a-schedule-is-a-goal-on-set-times.md`),
the rest of the plan to make the work the agent does unasked one idea looked
at what was left beside the goal: the caps on goal turns, the skill watches,
the ideas, the reasons the agent speaks first, the switches for them, and the
feed's columns. Each was read against the code and against what a working
server does with it.

- A goal at its own pace stopped at forty-eight turns a day, and a person
  could have twenty such goals. Neither number said what the person cares
  about, which is what the work costs; and every goal turn already stops at
  the daily budget, which it shares with the person's own conversations.
- Skill watches list a skill's new items with the skill's own tools and judge
  each with one small model call, and only what deserves it becomes an alert.
  On a working server they look at about a thousand items a week and send
  hundreds of alerts, most of them chat mentions.
- Ideas carry a catalog, the evidence a personal idea rests on, a rank and a
  page of their own, and already reach the person as cards of the feed.
- Four of the six reasons the agent speaks first already post a card; the
  other two, the introduction and the phone tour, ask questions and belong in
  a conversation.
- Ideas and memory checks had a switch each, for one choice: whether the
  agent brings things up on its own.
- A goal that came to need the person, or finished, was always written into
  the main conversation, whatever the person chose as the place they are
  told things.

## Decision

- The goals at their own pace spend at most a share of the daily budget
  between them (`agent.limits.goalShare`, half when unset). One that finds
  the share spent goes on tomorrow, or when the person writes. This replaces
  `goalTurnsPerDay` and `goalsInProgress`, which an older configuration may
  still carry and which are now reported and dropped. The turns a goal takes
  without a word from the person (`goalTurnsAlone`) stay: they are about a
  goal nobody can meet asking for attention, not about cost.
- One switch, Suggestions (`is_suggestions_enabled`, migration 0282), replaces
  the ideas switch and the memory check switch. A person who had switched
  either off keeps both off.
- A goal that needs the person, or is done, is a card of the feed when the
  feed is where they are told things, with a way to answer; otherwise it is
  written into the main conversation as before.
- Kept as they are, with the reason:
  - Skill watches. They are a filter that costs one small call an item;
    folding them into goals would put a turn behind every chat mention.
  - Ideas. What they store is what makes the Ideas page; a goal would carry
    none of it.
  - The introduction and the phone tour, which are conversations.
  - The feed's columns. The ones that look derivable (the stack's title, the
    outcome pending) are set by posts that are not a goal's, or are read on
    every list of the feed; deriving them would move work to every read.

## Consequences

- Work nobody watches can no longer use more than its share of a day, however
  many goals there are; an operator who wants more gives it more.
- The agent may keep as many goals as it likes; what bounds them is cost.
- Clients that still send `isIdeasEnabled` or `isMemoryCheckEnabled`, or read
  the two caps from the server settings, need the new build.
