# Skill watches: a skill says what is worth watching, and the agent watches it with the skill's own tools

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

It supersedes the Gmail-only watch of `docs/planning/watched-mail-execplan.md`, whose design it generalizes.


## Purpose / Big Picture

The agent watches the person's Gmail for mail worth telling them about, but the code that does it names the gmail skill's tools and reads the JSON one program prints. If the skill is republished with a renamed tool the watch breaks while the skill keeps working, and nothing else a skill reaches (a bank's transactions, chat mentions, notifications from a code host) can be watched without more code here.

After this plan a skill declares its watches in its own file, beside its tools: what to look at, which of its tools lists what arrived since a moment, which reads one item, and how to judge an item. The agent runs every watch of every installed, enabled skill on its own cadence, through the skill's own tools on the computer the person chose for it, sorts what is new, and hands what deserves it to the alert job, where quiet hours, the daily most and mutes apply. Four skills get watches: Gmail (new mail, archived included), Link by Stripe (new transactions), Mattermost (new mentions) and GitHub (new notifications).

To see it working: install the new versions of the four skills; within ten minutes the agent's activity shows runs titled like `Watching link: "…"`; a charge you did not expect, a mention asking you something, or a review request becomes an alert in the main conversation.


## Progress

- [x] (2026-10-06) Read every skill in the registry and what each program prints (gog, link-cli, mm, gh); decided which deserve a watch.
- [x] (2026-10-06) Milestone 1: the `watches:` block in the skill format, parsed and validated.
- [x] (2026-10-06) Milestone 2: the generic watch job, migration 0152, the alert job reading the renamed candidate.
- [x] (2026-10-06) Milestone 3: gmail 1.1.0, link 1.1.0, mattermost 1.2.0 and github 2.1.0 with their watches, signed, merged (teanode-skills#9) and installed; link, mattermost and github pointed at the computer gmail already used, since three computers are attached and a watch needs to know which.
- [x] (2026-10-06) Milestone 4: tests, docs, deployed; every watch has looked, and Gmail's judged its first new message.


## Surprises & Discoveries

- A first look that found nothing left no record, so the next look was a first look again and would have noted, not judged, the first item to arrive. A first look now records itself.
- Starting each look from the newest item's date sent Link's looks further back each time: transactions are dated the day they were made and appear days later, so the second look reached days before the first and judged as new what the first had not reached. Looks now start from when the watch last looked, less the overlap.
- An `lookBack` setting was dropped for the silent first look: what is there when a watch starts is not news, whatever its age.
  Evidence: the first looks on the maintainer's server, 2026-10-06.


## Decision Log

- Decision: a list tool prints a JSON array of items with fixed keys, `id` (required), `version`, `at`, `from`, `title`, `text`, `url`; a skill shapes its program's output into it, with `jq` where needed, as skills already do. The watch code reads only those keys.
  Rationale: a mapping language in the skill file would be a second template language for one feature; `jq` is already how skills shape output, and a fixed shape is one thing to document and test.
  Date/Author: 2026-10-06.
- Decision: the watch passes the moment to look from as the list tool's own parameters, whichever of `since` (RFC 3339), `since_epoch` (seconds) and `since_date` (YYYY-MM-DD, UTC) the tool declares, plus the fixed arguments the watch names. A read tool gets the item's `id`.
  Rationale: no templating on this side; a tool that declares the parameter says exactly what it takes.
  Date/Author: 2026-10-06.
- Decision: an item is new when its `id` and `version` have not been looked at: a thread whose message count changed is looked at again.
  Date/Author: 2026-10-06.
- Decision: a watch's `kind` is `mail` (sorted with the prompt hosted mail is sorted with) or `item` (judged with the watch's own `guidance`, a few sentences in the skill file on what is worth telling).
  Rationale: mail has years of tuning in the triage prompt (phishing, junk, codes); a transaction or a mention needs different judgement, which the skill's author knows best.
  Date/Author: 2026-10-06.
- Decision: no watch for the other skills now. Weather, dictionary, news, git and Confluence have nothing that arrives for the person; GitLab and Gitea list per project, so a watch would need the person to name projects; Home Assistant, Homebridge and UniFi Protect have their own automations and alerts, and a ten-minute poll of a house is the wrong tool; Google Drive could watch files newly shared with the person, which is worth a later version.
  Date/Author: 2026-10-06.


- Review, 2026-10-07: a watch ran a skill's commands with none of the judgement a turn's call gets. A watch now runs only when its tools make no request that is not a read and the command judge says its commands neither send nor destroy anything, judged once per command and kept. The review also found that a look which failed on one item failed every look after it, that a list printed past the size of an answer stalled the watch, that the 25-item cap dropped the newest, and that the item was not fenced in its prompt; each is fixed: an unjudgeable or unreadable item is noted and passed, an unreadable list counts its look, the newest are judged first and the rest noted, and the item goes inside the untrusted-data fence.

## Outcomes & Retrospective

Four skills declare watches and the code names none of them. On the server each look takes a few seconds; Gmail judged its first new message twenty minutes after the deploy. What remains possible: Google Drive could watch files newly shared with the person, and a skill that needs a person's secret cannot be watched yet, because a look has nobody to fill one in.


## Context and Orientation

- `internal/skills/skill.go`: `Skill`, `Parse`, `validate`.
- `internal/agent/watched_mail.go`: the Gmail-only watch this replaces.
- `internal/agent/alert.go`, `alert_mute.go`: how a watched candidate is decided, muted and listed.
- `internal/db/database_watched_mail.go`, migrations 0150: what has been looked at.
- The registry: the public `teanode-skills` repository, `skills/<name>/skill.md`, `index.json` signed with `make sign`.


## Plan of Work

Milestone 1: `Watch` with `name`, `description`, `kind`, `guidance`, `every` (default 10m, at least 1m), `lookBack` (first look, default 2h), `list {tool, arguments}`, `read {tool, arguments}`; validation that the tools exist and that a read tool declares `id`.

Milestone 2: migration 0152 renames `agent_watched_mail` to `agent_watched_item` with `watch_name` and `watched_item_version`, and the candidate's `watched_message_*`, `watched_subject` and `watched_mail_category` columns to `watched_item_*`, `watched_title` and `watched_category`, adding `watched_watch_name`. `internal/agent/watched_item.go` replaces `watched_mail.go`: `queueWatching` per skill and watch; `runWatch` lists, dedupes, reads, judges (`TriagePrompt` for mail, a new `watched_item.txt` prompt for items) and makes candidates.

Milestone 3: in the registry, gmail gains `gmail_new` and a `thread_text` read; link gains `link_new_transactions`; mattermost gains `mattermost_new_mentions`; github gains `github_notifications`; each gains `watches:`. Versions bumped, README documents the block, index signed.

Milestone 4: tests (parsing, the item contract, a watch end to end with a fake computer and model), docs, deploy, install the skills, read the first looks on the server.


## Validation and Acceptance

`make test` and `make lint-ci` pass. On the server, each of the four watches runs and records what it looked at; an item judged `soon` or `now` becomes an alert.


## Idempotence and Recovery

Migration 0152 only renames and adds. A skill without watches is untouched. An older server reading a skill with watches ignores the block.
