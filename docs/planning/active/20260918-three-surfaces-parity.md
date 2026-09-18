# Three surfaces, one set of things a person can do: the command line, the agent's tools and the dashboard

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. `~/.claude/PLAN.md` describes the form this
document takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260915-memory-that-learns-the-person.md`, which
describes the memory graph, the knowledge sources and the night's reading,
and on `docs/planning/done/20260917-goals-per-conversation.md`. Everything
this plan needs from either is repeated here.

## Purpose / Big Picture

The agent's memory is reached three ways: by the person at the command line
(`teanode agent memory …`, `teanode agent knowledge …`, `teanode agent
conversation …`), by the agent itself through its tools (`memory`,
`knowledge`, `todo`, `schedule`), and by the person in the dashboard (the
Agent page, the Knowledge page under Settings, and the conversation drawer).
All three go through the same GraphQL API, and the rule the owner set on
2026-09-17 is that anything that can be done on one of them can be done on
the other two. An audit on 2026-09-18 found eighteen places where that is
not so. This plan closes them.

After this plan a person can merge two pages, move a fact, edit a fact
without losing which audiences it applies to, set a page's aliases, re-read
a source, and archive a conversation from the dashboard; can stop a run,
read older messages, filter the run list, see a conversation's todos, set
and pause a schedule, and archive a conversation from the command line; and
the agent can pause a source rather than remove it, correct a fact in place,
and read a page's history. The one large gap, searching and reading what
was indexed from anywhere but the agent's own tool, is the last milestone.

## Progress

- [x] Milestone 1: the command line (gaps 3, 8, 10, 11, 14, N2, N3) — PR #107
- [x] Milestone 2: the agent's tools (gaps 5, 6, 12, N4) — PR #108
- [x] Milestone 3: the dashboard (gaps 2, 4, 5, 7, 13, N3, and the archived list of 3) — PRs #109 and #110, checked in Chrome on root@server
- [x] Milestone 4: a source can be edited, and a question's recall shown (N1, 9) — PRs #111 and #112, checked in Chrome on root@server
- [ ] Milestone 5: what was indexed can be searched and read from the API, the command line and the dashboard (1)
- [ ] Milestone 6: todos can be changed by the person (8, N5) — API and command line in PR #111; the drawer's ticking still to do

## The gaps, as the audit found them

Each is what is missing and where; the surfaces that have it are named so
the implementation copies them.

1. Searching and reading indexed documents exists only in the knowledge
   tool (`internal/agent/tools/knowledge/knowledge.go`, `searchAction`,
   `readAction`). No API query, no client, no command, no dashboard view.
2. Editing a fact drops its audiences. The API's `SaveAgentFact` rewrites
   `Audiences` from the argument on every edit; the dashboard's mutation
   never sends the field, so editing the text resets the fact to `ask`.
   The command line does the same when `note --number` is given without
   `--applies-to`.
3. Archiving a conversation is API and client only (`UpdateAgentConversation`).
   No command; the drawer's update mutation carries no `archived`, and its
   list asks for unarchived ones only.
4. Merging pages and moving a fact are API, client, command line and tool.
   The dashboard has neither.
5. A source's `rootPath`, `cron` and `mailboxId` are settable by the API and
   the command line. The dashboard's add dialog sends none of them; the
   knowledge tool's `add` takes `cron` and not the other two.
6. Pausing and resuming a source is command line and dashboard. The
   knowledge tool can only remove one, which forgets what was indexed.
7. Re-reading documents (`RereadAgentDocuments`) is API, client and
   command line. The dashboard's dream card has no button for it.
8. A conversation's todos are written by the `todo` tool and shown in the
   drawer. The command line's `conversation show` does not select them.
   Nothing but the tool can change one: there is no todo mutation.
9. What a question would recall (`RecallAgentMemory`) is used only by
   `agent memory evaluate <file>`. No single-question command, no
   dashboard panel.
10. The run list filters by kind and by words in the API and dashboard;
    the command line's `run list` has neither.
11. `conversation show` reads from offset zero and has no `--offset`.
12. The memory tool's `note` always adds; it cannot edit a fact by number
    as the API, command line and dashboard can.
13. The drawer starts a conversation without a goal; the API and command
    line take one at the start.
14. `StopAgentRun` has a client method and no command.
- N1. An existing source can be edited only through the raw API: no
  `knowledge set` command, and the dashboard sends only `enabled`.
- N2. Schedules cannot be edited, enabled or disabled at the command line;
  the tool and the dashboard can.
- N3. Page aliases are settable by the API, client and tool; not by
  `memory page` nor by the dashboard's page editor.
- N4. Page history is on the command line and dashboard; the memory tool
  has no `history` action.
- N5. The drawer shows todos as static text. Same cause as 8.

## Surprises & Discoveries

- The audit that first found these gaps was written on 2026-09-17 in a
  session whose record was compacted away; only the gap names survived, and
  the list above was re-derived from the code on 2026-09-18.
- The API's `SaveAgentFact` and the memory tool's edit both read a missing
  kind as "fact" and a missing date as "now". On the API that is what every
  caller sends; in the tool a correction is nearly always to the words
  alone, so the tool keeps both unless the call gives new ones.
- `npm run lint` fails before reading a file: eslint 10 refuses the old
  `.eslintrc.json`. CI never runs it (the Dashboard job is install and
  typecheck), so nothing noticed. Migrating the config is its own change.
- The CI lint refuses `/home/<name>/…` in tests and the local one does not;
  the tool batch failed once on it.
- `SaveAgentKnowledgeSource` changes only the fields that arrive non-empty,
  so a box the person empties in the edit dialog keeps its old value; the
  dialog says so rather than pretending. Clearing a field needs a change
  to the resolver's argument shape and is not in this plan.
- The Dreams tab's "days left" is measured over the last few finished
  dreams, and after the model bake-off three of those were trials that
  read five thousand documents in a quarter hour, so it read "about 3
  days" while the station model's pace says weeks. It corrects itself as
  station dreams push the trials out of the window.

## Decision Log

- Small gaps first, grouped by surface, one pull request per group, so each
  can be reviewed and released on its own. The dashboard group is checked
  in Chrome before it is opened.
- Editing a fact leaves its audiences alone when none are given. The
  alternative, every surface always sending them, repeats the mistake in
  every new caller.

## Outcomes & Retrospective

Not yet.
