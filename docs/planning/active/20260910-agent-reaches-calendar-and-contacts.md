# The agent reaches the calendar and the address book

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log` and `Outcomes & Retrospective` must
be kept up to date as work proceeds, in accordance with `~/.claude/PLAN.md`.

It is plan D of `20260910-personal-agents-roadmap.md`. Plans A, B and C are
done and in `../done/`: A built the agent, its runs and its tool catalog; B
built the address book and CardDAV; C built the calendar, CalDAV and
invitations by mail. This plan is what makes those three one thing.

## Why this matters

Today the agent has hands for the calendar and the address book and they only
move when spoken to. A person can open the drawer and say "what does Tuesday
look like" and get an answer; nothing else happens on its own. The message
that says "shall we say Thursday at four?" is sorted into Work and left
there. The signature with a new phone number in it is read by nobody. The
person who asks "are you free Thursday?" is answered by a draft that has not
looked at the diary.

After this plan:

- Sorting a message and drafting a reply are runs that can **look things up**
  — the thread, the sender's history, the diary, the address book — and take
  more than one turn to do it, instead of being one shot at one prompt.
- A message that carries an appointment or a person's details in prose is
  **offered in the reader**: a card above the message saying "this looks like
  an appointment — add it?" with the fields filled in, and "Add to calendar"
  or "Save contact" putting it where it belongs.
- The calendar and the address book are **sources the agent is granted**, one
  switch each, exactly as a mailbox is; the agent's prompt says which it has.
- **A daily brief** arrives by mail at a time the person picks, from one
  switch rather than from writing a schedule by hand.
- **Mail is a surface**: writing to the agent's own address reaches the main
  conversation and the answer comes back by mail.

And before any of that: the catalog is **82 tools**, which is more than a
model reads carefully and more than a person can hold in their head. This
plan starts by consolidating it to about fifty-five without losing a single
capability.

## How to see it working

After the whole plan, on a development server (`make dev`, see
`docs/reference/local-development.md`):

    # the catalog is smaller and nothing is missing
    teanode agent tools --json | jq 'length'         # about 55, was 82
    teanode agent ask "list my domains"              # one tool named domain

    # sorting can look things up
    teanode agent run list --kind triage             # a run with tool calls in it

    # an appointment offered rather than missed
    swaks --to you@example.com --server 127.0.0.1:10025 \
      --header "Subject: lunch" --body "Shall we say Thursday at 1pm at Nadia's?"
    # then open the message in the dashboard: a card above it offers the event

    # the brief
    teanode agent brief on --at 07:30
    teanode agent brief now                          # sends it immediately

    # mail as a surface
    swaks --to agent@example.com --server 127.0.0.1:10025 \
      --from you@example.com --body "what needs me today?"
    # the answer arrives in the mailbox, in the main conversation's voice

## Orientation: the parts this plan touches

A reader new to this repository needs five places.

`internal/agent/` is the agent: `agent.go` holds the worker that claims jobs
from the `agent_job` table and dispatches them by kind; `triage.go`,
`research.go`, `reply.go`, `summarize.go`, `schedule.go` are the handlers for
those kinds; `ask.go` is the conversation loop — the thing that talks to a
model with tools in a loop until it answers, emitting events as it goes.
`prompts/` holds the text templates each of those renders.

`internal/agent/tools/` is the catalog. Every tool is a `tools.Tool` value —
a name, a family, a risk class, a JSON schema for its arguments, the
permissions a person needs to be offered it, and a `Run` function. Each
package registers its tools from its own `init()` through `tools.Register`;
`tools/all` imports them all so a single import wires the catalog up. A tool
reaches the run it belongs to through the context (`tools.RunFrom`), never
through the loop's types.

`internal/models/` is the shape of everything stored, and `internal/db/` the
storage: `migrations/NNNN_name.sql` with a matching `.reverse.sql` for every
change (see `docs/coding/database-migrations.md`).

`internal/api/v1api/apigraph/` is the API the dashboard and the command line
both speak — GraphQL built by reflection over Go types.

`web/src/` is the dashboard: `pages/mailbox.tsx` is the reader,
`pages/agent.tsx` the agent's own page.

## Milestones

### Milestone 0 — one tool per thing, not one per verb

**Scope.** The catalog has grown to 82 tools, and the growth is not in what
the agent can do: it is in how finely each thing is sliced. Domains alone are
seventeen tools — `domain_list`, `domain_get`, `domain_add`, `domain_update`,
`domain_remove`, `domain_dns_check`, and the same five again for aliases and
four for credentials. Every one of those is a paragraph in the request, and a
model choosing between 82 near-identical names chooses worse than one
choosing between 55 distinct ones.

The repository already has the better shape and uses it in six places:
`contact_book`, `folder_manage`, `schedule`, `connected_server`, `skill` and
`mail_act` each take an `action` enumeration and do several related things.
This milestone applies that shape to the rest.

What exists at the end: the same capabilities, reachable through fewer names.

    before                                          after
    domain_list get add update remove dns_check  →  domain      (list|get|add|update|remove|dns)
    alias_list add update remove match           →  alias       (list|add|update|remove|match)
    credential_list create update remove         →  credential  (list|create|update|remove)
    queue_list queue_retry                       →  queue       (list|retry)
    rule_list add update remove apply test       →  rule        (list|add|update|remove|apply|test)
    user_list add update remove                  →  user        (list|add|update|remove)
    group_list group_manage                      →  group       (list|manage)
    role_list role_manage                        →  role        (list|manage)
    calendar_agenda free add edit remove         →  calendar    (agenda|free|add|edit|remove)
    mail_audit_search get content mark           →  mail_audit  (search|get|content|mark)
    account_get account_update                   →  account     (get|update)
    settings_get settings_update                 →  settings    (get|update)

Eleven families lose 45 names and gain 12: the catalog goes from 82 to 49,
plus the four this plan adds later, so about 53 at the end.

**Two rules the merge must not break.**

*Risk is per action, not per tool.* `calendar_remove` is destructive and
`calendar_agenda` is a read; merged, the tool's declared `Risk` is the
gentlest of its actions and a `RiskOf` function raises it for the rest, which
is exactly how `mail_act` already distinguishes a move from a
delete-for-ever. The confirmation gate reads `RiskOf` against the *settled*
arguments (see `tools.Tool.RiskFor`), so this is safe against a sloppily
written call.

*Permission is per action too.* `group_list` is offered to anyone who may
manage groups **or** users, while `group_manage` needs group management. A
merged tool's `Permissions` field is the union — it decides only whether the
tool is offered at all — and each action re-checks its own permission inside
`Run`, answering with a refusal the model can read. A test asserts that a
person holding only `user:manage` can list groups and cannot manage them.

**Work.** For each merged tool: one new file in the existing package holding
the merged definition and a `Run` that switches on `action`; the old
definitions deleted; the callers of the old helper functions left alone,
because the merge is at the tool boundary and the code underneath it does not
move. `internal/agent/tools/tools_test.go` gains a test that every action of
every merged tool answers, and `docs/subsystems/agent-tools.md` is rewritten
to list the new names.

**Acceptance.** `teanode agent tools` lists about 49 tools and every family
still names what it did before. `teanode agent ask "what domains are on this
server"` answers, having called `domain` with `action: list`. A person with
only `mail:read` is offered neither `domain` nor `user`. `make test` and
`make lint-ci` pass.

### Milestone 1 — sorting and answering can look things up

**Scope.** `runTriage` and `runReply` each build one prompt, make one call to
a model with `JSONObject: true`, and parse what comes back. They cannot read
the thread the message belongs to, cannot see whether this sender has written
before, cannot look at the diary before saying "yes, Thursday works". This
milestone makes both of them runs of the conversation loop, with a small
read-only tool set and a cap on how many turns they may take, exactly as
`runResearch` already is.

What exists at the end: a triage run that can call `mail_search` before
deciding a category, and a reply run that can call `calendar` before
proposing a time — and a transcript in the Activity view showing it did.

**How.** `research.go` is the model to copy. It creates a `run` conversation,
renders a prompt, calls `self.Ask(&AskSettings{... ReadOnly: true, Headless:
true, Allow: <set>, MaxRounds: n ...})`, subscribes to the turn's events,
and takes the last `EventMessage` as the answer.

Triage's answer must still be the JSON object `TriageAnswer` describes. The
loop returns prose, so the prompt ends with the instruction to answer with
the object and nothing else, and the result goes through
`llm.Extract[TriageAnswer]`, which already strips code fences and repairs
trailing commas and single quotes (`internal/llm/structured.go`). If
extraction fails, the run falls back to the single-shot call with
`JSONObject: true` — the code path that exists today — so a model that cannot
be talked into clean JSON still sorts mail. That fallback is not a nicety: it
is what keeps a mailbox being sorted when somebody points the server at a
small local model.

Tool sets, deliberately small, because these runs happen on every message:

    triage: mail_read, mail_search, contact_book, calendar, memory, datetime
    reply:  mail_read, mail_search, contact_book, calendar,
            memory, datetime, web_fetch, web_search

`calendar` and `contact_book` appear only when the agent has been granted
that source (milestone 2), and `web_*` only where the operator allows them.
Both sets are read-only: `ReadOnly: true` strips every tool that changes
anything, so `calendar` in a triage run can read the agenda and cannot add to
it.

Rounds: `agent.limits.maxRoundsPerTriage`, new, default **3**;
`maxRoundsPerReply` already exists and defaults to 6.

**Cost, which is the reason to be careful.** Triage runs on every message
that arrives. A loop costs more than a single call even when it takes one
turn, because the conversation loop's system prompt is five layers where the
triage prompt was two. Three things hold that down: the prompt's short
conduct variant (`ask.txt` renders one paragraph instead of the full rules
when `Short` is set — a new `AskSettings.Short` field, set by both runs), a
tool set of six rather than fifty, and an instruction in `triage.txt` to
answer at once unless something is genuinely unclear. The milestone is not
finished until the token cost of sorting a plain newsletter is measured
before and after and written into `Surprises & Discoveries`.

**Acceptance.** Deliver a message from an unknown sender that refers to an
earlier conversation; the triage run's transcript in the Activity view shows
a `mail_search` call and the insight names the right category. Deliver a
message asking for a meeting to a mailbox with drafts on; the draft proposes
a time the diary actually has free. `teanode agent run get <id>` shows the
tool calls. The fallback is covered by a test that makes the model answer
prose and asserts the insight is still written.

### Milestone 2 — the calendar and the address book are sources

**Scope.** The rule this program was built on is that nothing from a source
the person has not granted is ever sent to a model
(`docs/decisions/20260910-agents-belong-to-people.md`). A mailbox has that
switch. The calendar and the address book do not: their tools are reachable
on the person's own permissions alone. This milestone gives each collection
the same grant, says so in the prompt, and refuses the tools without it.

What exists at the end: a Sources card on the agent's page listing the
person's mailboxes, calendars and address books, each with its own switch;
and a system prompt that says which the agent may reach.

**Work.** Migration `0062_agent_source_grants.sql` adds
`agent_granted boolean NOT NULL DEFAULT false` to `calendar` and to
`addressbook`, with the matching `.reverse.sql` dropping both columns.
`models.Calendar` and `models.AddressBook` gain `AgentGranted bool`. The
`calendar` and `contact_book` tools filter to granted collections and answer
"the person has not given you their calendar" when there are none — a
refusal, in words the model can relay, not an error. `AskRun.situation`
(`internal/agent/ask.go`) gains two lines after the mailboxes:

    Calendars you may reach:
    - "Calendar" (id 01K...): 34 events in the next 30 days
    Address books you may reach:
    - "Contacts" (id 01K...): 212 people

Granting is `GrantAgentSource(kind, id, granted)` in
`apigraph/agent_source.go`, one mutation for all three kinds, replacing
nothing — `GrantAgentMailbox` stays, because a mailbox grant carries a policy
and these do not. The CLI gains `teanode agent source grant calendar <name>`
alongside the mailbox form it already has.

**Acceptance.** With the calendar not granted, `teanode agent ask "what is on
tomorrow"` answers that it has not been given the calendar. Flip the switch
on `/settings/agent`; ask again; it reads the diary. The situation layer's
new lines are covered by a golden-prompt test in
`internal/agent/testdata/prompts/`.

### Milestone 3 — an appointment in prose is offered, not missed

**Scope.** C reads an invitation that arrives as a `text/calendar` part. Most
appointments do not arrive that way: they arrive as "shall we say Thursday at
four", and most new phone numbers arrive in a signature. This milestone adds
a run that reads a message the agent has already sorted and proposes what it
found, and a card in the reader that puts it where it belongs with one press.

What exists at the end: a new job kind `extract`; proposals stored on the
insight; a card above the message offering "Add to calendar" or "Save
contact"; and nothing written anywhere without the person pressing it.

**How.** Triage's answer gains one field, `extract: true`, the way `research`
already works: the model says when a message looks like it carries an
appointment or somebody's details. The worker then enqueues an `extract` job
whose subject is the message. The extract run is a conversation-loop run
(read-only, headless, `maxRoundsPerExtract` default 4) with `mail_read`,
`calendar`, `contact_book` and `datetime`, whose prompt
asks for a JSON object:

    {"events": [{"summary": "...", "starts": "2026-09-17T16:00", "ends": "...",
                 "location": "...", "because": "the line it came from"}],
     "contacts": [{"name": "...", "emails": ["..."], "phones": ["..."],
                   "organization": "...", "because": "..."}]}

It writes them to `mail_insight.proposals` (a new `jsonb` column, migration
`0063_mail_insight_proposals.sql`) and nothing else. The reader draws
`ProposalCard` above the message for each proposal, the fields editable, with
the sentence it came from quoted under it; "Add to calendar" calls the
existing `SaveCalendarEvent` mutation and "Save contact" the existing
`SaveContact`, and dismissing marks the proposal dismissed so it does not
come back.

Why a proposal rather than a write: putting an appointment in somebody's
diary because a message mentioned a day is how a calendar becomes untrusted,
and a stranger's message is untrusted input. The agent proposes; the person
decides. This is the same reasoning as the held reply's hold window.

**Acceptance.** Deliver the "shall we say Thursday at 1pm at Nadia's" message
above; within a worker tick the reader shows a card with Thursday's date, the
time and the place, and the line it came from; pressing Add puts it in the
calendar and the card says so. Deliver a message with a signature carrying a
new number for a known contact; the card offers to save it, and the existing
contact is offered as the one to update rather than a duplicate. A message
with neither produces no card and no `extract` job.

### Milestone 4 — the daily brief

**Scope.** A schedule that says "every weekday at half past seven, tell me
what the day holds and what is waiting for an answer, and send it to me" is
three fields in a form the person has to think up. This milestone makes it
one switch.

What exists at the end: a Brief card on the agent's page with a switch, a
time and the days; and `teanode agent brief on|off|now`.

**How.** No new machinery: the switch writes an ordinary `agent_schedule` row
whose `name` is "Daily brief", whose cron is built from the time and days,
whose `deliver` is `mail`, and whose prompt is the brief's own template
(`prompts/brief.txt`, new) — the agenda for the day from the granted
calendars, what arrived overnight that needs a reply, what is held, and
anything the person's memories say they want in it. Because it is an ordinary
schedule, it appears in the Schedules card, can be edited by hand afterwards,
and runs through the machinery that already exists. The switch is a
convenience that writes a row, and the plan says so in the card's own words.

**Acceptance.** Turn the switch on for 07:30 on weekdays; the Schedules card
shows "Daily brief" with `30 7 * * 1-5`; `teanode agent brief now` sends the
brief immediately and it arrives in the mailbox, naming the day's events and
the mail waiting for an answer. Turning the switch off disables the schedule
and leaves it listed, so a person who edited the prompt does not lose it.

### Milestone 5 — mail is a surface

**Scope.** Every other surface the agent has — the drawer, the phone, a chat
app, the command line — needs something installed or opened. Mail does not.
This milestone makes an address into a way to talk to the agent.

What exists at the end: an alias of kind `agent`; a message to it becoming a
turn of the person's main conversation; the answer coming back by mail.

**How.** `models.AliasKindAgent = "agent"` joins the kinds in
`internal/models/domain.go`. `matchAliases` (`internal/mx/exchange_utils.go`)
gains the case: it enqueues an `ask` job (new kind) whose subject is the
message and whose target is the alias's agent, inside the delivery
transaction, and delivers the message nowhere else. The `ask` run builds the
person's message from the mail's text, asks in the **main** conversation with
`Surface: "mail"` and `Headless: true`, and sends the answer back with
`internal/mailer` the way `deliverSchedule` already does.

**Who may drive it, which is the whole security of this milestone.** Only the
person the agent belongs to. The rule is the one the third security review
wrote for chat apps after a linked group chat turned out to speak with the
owner's voice: the sender is checked, not the channel. A message to an agent
alias is acted on only when its `From` is one of the owner's own addresses
**and** the message proved where it came from — DMARC passed, or this server
took it from an authenticated submission (`models.Mail.SubmittedHere`).
Anything else is delivered to the owner's Inbox with a note saying somebody
wrote to the agent's address, and no run happens. A headless run already
refuses every tool that would ask for confirmation, so nothing destructive or
outward can be done by mail even by the owner; the answer says what it would
have needed.

**Acceptance.** `swaks --from you@example.com --to agent@example.com --body
"what needs me today?"` produces a reply by mail within a worker tick, and
the turn appears in the main conversation in the drawer. The same message
`--from stranger@example.net` produces no run and lands in the Inbox with the
note. A message asking it to send mail to somebody is answered with what it
would have needed and does not send.

## Validation

Every milestone: `make test` (Docker, starts PostgreSQL) and `make lint-ci`,
both green, and `make lint` for the naming check where it is installed.

End to end, on a development server, in the order a person would meet them:
grant the calendar; deliver a message proposing a time; watch the triage run's
transcript show a lookup; see the proposal card; accept it; ask the agent by
mail what the day holds; receive the brief.

Tests that must exist by the end:

- every merged tool: one test per action, plus a permission test that a
  person holding one permission of the union gets the actions it covers and
  is refused the others;
- triage with tools: a fake provider that calls `mail_search` and then
  answers, asserting the insight; and one that answers prose, asserting the
  single-shot fallback still writes the insight;
- the grant: a test that `calendar` refuses when the collection is not
  granted and answers when it is;
- extract: a message whose text carries a date produces a proposal and no
  calendar object; accepting it produces the object;
- the brief: turning the switch on writes an enabled schedule with the cron
  the time implies;
- mail as a surface: a message from the owner runs; the same message from a
  stranger does not, and is filed instead.

## Progress

- [x] ExecPlan written
- [x] M0 tool consolidation — 84 tools to 54
- [x] M1 triage and reply with tools and rounds
- [x] M2 calendar and address book as granted sources
- [x] M3 extract and the proposal card
- [x] M4 the daily brief
- [ ] M5 mail as a surface — **postponed** at the owner's word (2026-09-13);
      the plan for it stays below for whoever picks it up

## Surprises & Discoveries

**Two tools already took an action of their own, and the merge would have
dispatched to the wrong half.** `folder_manage` takes `action: rename|delete`
and `group_manage` takes `action: update`; merged into a `folder` or a `group`
tool, the merged tool's own `action` field sits on top of theirs and every
call goes to the wrong place. Nothing failed loudly: the first sign was a test
asserting that deleting a folder asks first, which stopped asking.

    --- FAIL: TestCatalogIsWellFormedAndFiltered
        tool_test.go:68: deleting a folder should ask

`Merge` now refuses to build such a tool at all, which turned the second
collision into a panic at startup rather than a silent misdispatch:

    panic: tools: group_manage takes an action of its own and cannot be an
    action of group

So `folder_list`/`folder_manage`, `group_list`/`group_manage` and
`role_list`/`role_manage` keep their names. The catalog is 54 rather than the
49 the plan estimated, and the five that did not merge are the five that were
already action-shaped.

**What the loop costs sorting, measured.** The plan said this milestone was
not finished until the cost of sorting a plain message was measured before
and after. Counting the characters of everything sent, at four characters to
a token:

    before (one call):   5,413 characters  ~1,350 tokens
    after  (first round of the loop, six tools):   ~3,040 tokens
    after  (first round of the loop, four tools):  ~2,490 tokens

The difference between the last two is the tool definitions: six were 5,690
characters of schema, four are 3,905. So the set was cut to `mail_read`,
`mail_search`, `contact_book` and `datetime` — the calendar came out
because whether Thursday is free does not change what a message *is*, and
`memory` came out because what the person said about sorting is already in
the prompt, carried by the layer that holds their memories.

What is left is about 1,150 tokens per message more than the single call, of
which the system prompt's extra layers are a third and the tool definitions
two thirds. The system layer is marked as a cache breakpoint, so a provider
that caches prompts charges for it once; the message itself is never the
same twice and never cached either way.

## Decision Log

- **The catalog is consolidated before anything is added to it** (2026-09-13).
  Four tools arrive in this plan; adding them to 82 and then merging would
  mean writing them twice.
- **Merged tools keep per-action risk and per-action permission**
  (2026-09-13). The alternative — a merged tool taking the strictest risk of
  any of its actions — would ask the person to confirm reading their own
  diary, and a catalog that asks about everything teaches people to say yes.
- **Triage keeps a single-shot fallback** (2026-09-13). The loop is better
  when the model is good enough to end with clean JSON; an operator pointing
  this server at a small local model must still get sorted mail.
- **A proposal is not a write** (2026-09-13). An event a stranger's message
  implied is offered, never added. A calendar nobody trusts is worse than no
  calendar.
- **Mail as a surface checks the sender, not the address** (2026-09-13).
  Taken straight from SEC-51, where a linked group chat spoke with the
  owner's voice because the channel was the whole of the check.
- **Milestone 5 is postponed** (2026-09-13), at the owner's word, after the
  plan was written and before it was started. Its milestone stays as written
  so that whoever picks it up has the reasoning, particularly the sender
  check.
- **A tool that already takes an action cannot be an action** (2026-09-13).
  Found by building it; see Surprises. The guard is a panic while the catalog
  is being built, because a capability that quietly dispatches to the wrong
  half is worse than a server that will not start.

## Outcomes & Retrospective

Five of the six milestones are done; mail as a surface was postponed by the
owner before it was started, and its milestone stays above for whoever picks
it up.

What a person can do now that they could not before: give their agent a
calendar and an address book, one switch each, and see in the prompt which it
has; have sorting and drafting look things up instead of guessing, and read
the transcript of it doing so; be offered an appointment or a person's
details that a message carried in its words, with the line it came from
quoted, and add it with one press; and have a brief each morning from one
switch. And the catalog they all run through is fifty-four tools rather than
eighty-four.

Three things worth carrying forward.

**The merge found two bugs by refusing to build.** Making `Merge` panic when
a member already takes an `action` turned a silent misdispatch into a startup
failure, and that is how `group_manage` was found after `folder_manage` had
already been found the hard way — by a test that stopped asking for
confirmation before deleting a folder.

**Measuring the prompt changed the design.** Sorting was going to have six
tools; the measurement said two thirds of the added cost was tool definitions,
so it has four. The calendar came out of the set because whether Thursday is
free does not change what a message *is*.

**The fallback is what makes the loop safe to ship.** Triage and reply end in
prose when a small model cannot be talked into clean JSON, and the single call
each of them used to make is still there behind the loop. Without it this
change would have quietly stopped sorting mail for anybody running a local
model.
