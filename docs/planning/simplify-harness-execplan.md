# A simpler harness: no limits on how often work starts, and goals instead of special pages

## Purpose / Big Picture

The owner's rule, from the same change in their other project: do not
hardcode limits, and do not give people knobs nobody sets. The person's daily
money limit and the server's monthly one are the catch-all. Production sets
$30 a day per person and $500 a month for the server, with the token caps off.
Every other bound on how often or how much the agent's work runs adds nothing
but surprise:
- a goal told to look again in a minute looks in five;
- a goal stalls after 24 turns;
- the goals stop when they have spent half the day;
- a sixth alert in a day never arrives.

And two settings pages are goals in all but name. The Daily brief is a goal
with a page of its own, and the weekly look around is a job with a day and
time picker.

After this change:
- no cap on rounds, turns, wakes, shares of the day, or alerts a day remains;
- a goal waits exactly as long as it asked;
- the Daily brief is an ordinary goal in the Goals list;
- the look around is an ordinary goal;
- the Alerts page is one switch, the follow-ups and the mutes.

## Decisions (the owner's, 2026-11-26)

- Quiet hours go. A card lands in the feed at any hour, and the phone's own
  Focus or Do Not Disturb decides whether it makes a sound.
- The rhythms of background jobs stay, as each job's schedule: a dream at
  most every 6 hours, ideas every 20 hours, reflection and evaluation weekly,
  journal periods daily. Only true caps go.
- Waits that protect someone working stay:
  - the 30 minute quiet before speaking first;
  - the dream's quiet;
  - the minute a goal waits after the person or another turn;
  - the 7 day repeat guard of alerts.
- Technical bounds stay:
  - payload, attachment and prompt sizes;
  - timeouts, concurrency and the browser's contexts;
  - the loop guard that stops a turn calling the same failing tool 3 times;
  - batch sizes;
  - the push relay's abuse limits;
  - `GoalTimesMost`.

## Removed outright (not a flag, not a setting, not a constant moved elsewhere)

### Server

- `goaltool.Soonest` and `Latest` and the clamp in `interval()`: a goal's
  wait is used as given. A wait of zero or less means the next tick.
  `DefaultInterval` stays for a goal that never gave one.
- `goalDoubled`: a turn that made no goal call, or failed, keeps the wait the
  goal had.
- `GoalTurnsAlone` (24) and the stall it causes.
- `GoalShare` (0.5) with `goalShareSpent`, `goalShareSpentNote`, and its part
  in `clearHeldBackGoalNote`.
  `docs/decisions/20261123-goals-spend-a-share-of-the-day.md` is superseded.
- `BackgroundWakesAlone` (20) and the conversation's `BackgroundWakeCount` use.
  The column stays until a later migration.
- `DreamShare` (0.3): a night is bounded by the daily limit like everything
  else.
- `MaxRoundsPerAsk`, `MaxRoundsPerResearch`, `MaxRoundsPerReply`,
  `MaxRoundsPerTriage`, `MaxRoundsPerDream` and `MaxRoundsPerSubagent`, with
  their validators and every use.
  - A turn goes on until the model answers, the budget defers it, the loop
    guard stops it, or the person stops it.
  - Where a call path needs a number for a single-call model (triage takes
    one round by design), keep the design and drop the setting.
- `MaxToolCallsPerRun`: dead already.
- `EmbeddingTokensPerDay`.
- `memberTurnMaximumRounds`, `memberTurnSearches` and `memberTurnFetches` in
  chat_turn.go. `memberTurnsWaitingMost` stays: it is a queue length.
- `AlertDailyMost`, and `AlertQuietStart` and `AlertQuietEnd` with
  `isAlertQuietHour`.
- `speakFirstApart` (24 hours between speaking-first turns). Each subject is
  still raised once.
- `journalRoundsMost` (5), if it caps rounds rather than shaping one call.
- The look around job (`AgentJobNearby`, `queueNearbyRuns`) and
  `LookAroundWeekday` and `LookAroundTime`. It becomes a goal (below).
- The Daily brief operations `SetAgentBrief` and `RunAgentBriefNow`, with
  `findGoalByTitle` and the CLI `teanode agent brief`.
  - The brief stays an ordinary goal, still marked `built_in` (which keeps it
    out of Passwords, since it reads the inbox).
  - The ideas catalog offers "Send me a daily brief" for people who have
    none: "Do it" starts the same goal with the same text, times
    `30 7 * * 1-5`, and `built_in`.
- `config/deprecated.go` gains a line for each removed YAML key ("no longer
  used: the daily money limit bounds this"), so an old configuration still
  loads.

### Kept for one release, read-only, ignored on write

These GraphQL settings fields keep answering, so an older app's settings page
still loads. Mark each description "No longer used." and have clients stop
asking for them:
- `goalShare`, `dreamShare`, `goalTurnsAlone`, `backgroundWakesAlone`;
- `maxRoundsPer*`, `maxToolCallsPerRun`, `embeddingTokensPerDay`;
- the agent's `alertDailyMost`, `alertQuietStart`, `alertQuietEnd`,
  `lookAroundWeekday` and `lookAroundTime`.

### Migration 0285

- For each agent whose look around was not turned off, create a goal titled
  "Look around":
  - written by `built_in`;
  - times: a cron at their day and time, or Thursday 17:00;
  - text: what the look around did. It searches the web for events, openings
    and deadlines in the person's town this week, posts the few worth their
    attention to the feed, and adds a follow-up for anything with a date.
  - Read how nearby.go decides "turned off" and what it reads (the town, the
    person's interests). The goal's text tells the turn to read the same.
  - The goal's own turn must have the tools it needs: `web_search`, the feed
    tool and `agentprofile`'s `add_follow_up`.
- The reverse removes those goals by their marker.
- Columns are not dropped in this migration.

## Surfaces

Strings change in English, Simplified Chinese and Japanese on every surface.

- **Dashboard:**
  - The operator's agent limits form (`web/src/pages/settings/agentSettings.tsx`)
    loses every removed field and keeps the daily token and cost limits and
    the monthly server ones.
  - The agent page loses the Daily brief card (`web/src/pages/agent.tsx`
    `BriefCard`).
  - Its alerts settings lose quiet hours, the daily most and the look around.
- **iPhone:**
  - `ManageServerViews.swift` limits lose the same fields.
  - `DailyBriefView` and its place in `AppPlaces.swift` go.
  - The Alerts page (`AgentUpkeepViews.swift`) keeps the switch, the alerts,
    following and mutes, and loses quiet hours, the daily most and the look
    around.
  - Demo data follows, and so do the shoot lines that showed removed screens.
- **Android:** the same:
  - `settings/manage/AgentSettingForms.kt`;
  - `settings/UpkeepScreens.kt` `DailyBriefScreen` and the `DailyBrief`
    helpers;
  - `settings/AlertsScreens.kt`;
  - demo data and Roborazzi shots.
- **CLI:**
  - `teanode agent brief` goes;
  - the alert settings commands lose the removed flags;
  - the limits printout loses the removed fields.
- **Agent tool:**
  - `agentprofile` loses the `nearby` action and the quiet hours and daily
    most arguments;
  - the goal tool's description no longer mentions 5 minutes or a day.

## Docs

- A decision record, `docs/decisions/20261126-no-limits-on-how-often-work-starts.md`,
  in TeaNode's own words:
  - what was removed, and what stays and why;
  - the budget coverage finding that every kind of work checks the daily
    limit, except the three gaps below.
- Fix those three gaps in the same change, each with a `RequireBudget` or
  `CheckBudget` before its model call, as the other jobs do:
  - query-time embeddings in `graph_embedding.go`;
  - the recording sub-steps `recording_accurate.go` and
    `recording_speaker_merge.go`;
  - `describe.go`.
- Update docs/subsystems/agents.md, goals, the alerts docs,
  docs/reference/configuration and docs/reference/command-line.md.

## Validation

- Server tests:
  - a goal's wait of 1 minute is kept as 1 minute;
  - a turn with no goal call keeps the previous wait;
  - no stall after 24 turns;
  - no share-spent hold;
  - background wakes past 20 still wake;
  - a sixth alert in a day is delivered;
  - no quiet-hour hold;
  - a turn past 150 rounds goes on (use a small fake model);
  - the migration creates the look around goal at the person's day and time
    and skips someone who turned it off, and its reverse;
  - an old YAML with removed keys loads with a warning.
- CLI parity. Dashboard vitest and tsc. iPhone kit tests and a Mac build.
  Android tests on station.
