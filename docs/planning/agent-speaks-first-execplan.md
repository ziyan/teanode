# The agent speaks first: onboarding, memory checks and tips

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

Today the agent only speaks when spoken to, or when a schedule or a goal the person set tells it to. Three things it should do on its own need it to start a conversation.

The first is onboarding. A person who has just switched their agent on meets an empty chat and a settings page. After this plan, the agent greets them in the main conversation, asks what they would like to call it, what it should call them, which language to talk in, and what they want help with, saves the answers where the settings page would have, and offers to connect a mailbox or a source.

The second is the memory check. Whether the agent's memory answers questions correctly can be measured today, but only by hand: somebody writes questions with the right answers in a JSON file and runs `teanode agent memory answers <file>` (see `docs/evaluation/README.md`). That is how the first measurement was made: an assistant with database access drafted questions from one person's memory, the person corrected them in a chat, the assistant wrote the file, and the command graded the agent's answers from memory, from the raw sources and from both. After this plan the agent does that itself. Every so often it asks, in the main conversation, whether it may check a few things it remembers; the person says right, corrects, or skips, one question at a time, and tells it what has changed lately. The confirmed questions and answers are kept, privately, as the person's evaluation set, and a weekly run grades the agent against them and keeps the scores.

The third is tips. A person uses a small part of what the agent can do. After this plan, when the person has the dashboard open and has gone quiet, the agent occasionally says one useful thing they have not tried, chosen from what they actually do: "you asked me three times this week what came from the build server; a schedule can mail you that every morning".

Whenever the agent speaks first in the main conversation, the chat drawer opens on it by itself if the dashboard is open, so the message is seen. The dashboard is secondary: the Memory tab shows the questions and answers on record and the scores, and the agent's settings let the person switch memory checks and tips off.

To see it working: create an agent for a new account and open the dashboard; within a minute the drawer opens with the agent's greeting. Answer its questions and the agent's name and yours are saved. Later, with memory checks on and some memory, the drawer opens with a request to check a few things; answer three, one with a correction, and the Memory tab lists three questions. Leave the dashboard open and idle, and on some day the drawer opens with one tip.

## Progress

- [x] (2026-09-24) Wrote this plan, after a first memory check done by hand: 48 questions drafted from one person's memory and corrected by them in chat, and a baseline of 46% from memory, 30% from sources and 58% from both (see `docs/planning/memory-write-invariants-execplan.md`, Outcomes).
- [x] (2026-09-24) Milestone 1: the agent can speak first: an unprompted turn in the main conversation, the rules for when, presence from the dashboard, and the drawer opening on it.
- [x] (2026-09-24) Milestone 2: onboarding.
- [x] (2026-09-24) Milestone 3: memory check questions stored per agent, with the tool the agent uses to draft, ask and record, and an import of an existing question file.
- [x] (2026-09-24) Milestone 4: the memory check conversation. The check-in asks the first question in the same message as the request, rather than asking whether now is good and waiting: the overlay then carries the check from its first reply, and "not now" is one word either way.
- [x] (2026-09-24) Milestone 5: the evaluation runs as a job over the stored set, and its scores are kept. The per-source scores are one jsonb column on the run rather than a column per source and verdict.
- [ ] Milestone 6: the Memory tab shows the questions, the answers and the scores.
- [x] (2026-09-24) Milestone 7: tips.

## Surprises & Discoveries

- Observation: questions drafted from memory mostly test what memory already holds. In the hand-made check, the questions that found real gaps were the ones the person supplied: where their parents live, a family trip nobody had written down, an internship before a job. The drafting has to ask the person for these, not only confirm what it has.
  Evidence: the questions the person answered with new information all scored "missed" from every source.
- Observation: the person's corrections are facts memory should learn, and learning them makes their questions easy. A score that rises because the answers were filed after the questions were written measures the questions, not memory.
- Observation: "not known" is most of what goes wrong. Memory held the fact and recall did not carry it in 22 of memory's 25 failures, so the useful signal is which questions were missed, per source, over time.
- Observation: the dashboard hears nothing from the main conversation while the chat drawer is closed: the drawer subscribes to a conversation's events only while it is open (`web/src/components/agentDrawer.tsx`, the effect that calls `subscribe` with `AgentConversationEvents`). A message the agent writes on its own is invisible until the person opens the drawer, which is why the drawer has to be told.

- Observation: a headless turn's overlays are sent with the conversation, not in the system prompt, so a test looking for the introduction's overlay has to search the whole request.

- Observation: the facts a check can ask about are chosen by page path (`self`, `people/`, `things/`, `places/`), which leaves work pages out even when they sit under `self/`: the path `self/work` is the person's own and is included, so a check can still ask about a job.

## Decision Log

- Decision: presence is a mutation, `ReportAgentPresence(isVisible, idleSeconds)`, sent by each tab every minute and on visibility change, rather than a subscription that also carries a `spokeFirst` event back. The drawer instead follows the main conversation's events (`AgentConversationEvents` with an empty conversation id) whenever it is not already showing it, and opens on an `asked` event whose surface begins `speak_first:`.
  Rationale: both halves already existed. A second subscription would have duplicated what the conversation feed says, and a mutation needs no connection bookkeeping on the server.
  Date/Author: 2026-09-24, agent.

- Decision: the rule "not within ten minutes of the person closing the drawer on a spoken-first message" was left out.
  Rationale: the server already allows one unprompted message a day and none within thirty minutes of the person's last message, which bounds how often the drawer can open by itself; the extra browser state was not worth it until somebody finds the drawer opening too often.
  Date/Author: 2026-09-24, agent.

- Decision: the tip is chosen by the server, not the model: the first entry of the catalog the person neither uses nor was told, recorded as given when the turn starts. The model only words it and ties it to what the person did lately.
  Rationale: a model asked to choose and then report its choice will sometimes not report it, and the same tip comes round again; the catalog is short and ordered by usefulness, so the first candidate is the right one anyway.
  Date/Author: 2026-09-24, agent.

- Decision: one tool, `agent_profile`, carries both the profile (name, language, a line added to the instructions) and the person's word about speaking first (`onboarding_done`, `not_now`, `no_more_tips`, `no_more_memory_checks`). It is a core tool, so its overlay is present in every main-conversation turn while the introduction is open.
  Rationale: one small tool costs fewer prompt tokens than two, and all five actions are the person saying something about how the agent should behave.
  Date/Author: 2026-09-24, agent.

- Decision: one mechanism for everything the agent says unprompted, with onboarding, memory checks and tips as its three uses.
  Rationale: all three need the same things: a turn in the main conversation that nobody asked for, rules about when that is welcome, knowing whether the person is there, and the drawer opening. Built three times they would disagree about the rules, and the person would be interrupted three times as often.
  Date/Author: 2026-09-24, agent.

- Decision: the conversation is the primary interface and the dashboard is secondary.
  Rationale: the person asked for it, and it matches how the first memory check went: a back and forth, a question at a time, with corrections in the person's own words, is easy in a chat and tedious in a form. The dashboard is for looking at what was recorded and fixing it.
  Date/Author: 2026-09-24, the person.

- Decision: the drawer opens by itself on a message the agent wrote unprompted in the main conversation.
  Rationale: the person asked for it; a message nobody sees might as well not be written.
  Date/Author: 2026-09-24, the person.

- Decision: at most one unprompted message a day across memory checks and tips, never while a turn runs, never within thirty minutes of the person's last message, and only while the person has the dashboard open. Onboarding is exempt from the daily limit, since it happens once.
  Rationale: an agent that interrupts often gets switched off. Speaking only while the person is looking means the drawer opening is seen rather than piling up messages for later.
  Date/Author: 2026-09-24, agent.

- Decision: a memory check correction is filed into memory when the person agrees, and the question records that its answer was filed after the question was written.
  Rationale: the agent should know what the person told it; the flag keeps that knowledge from inflating the score. Runs report the score with and without such questions.
  Date/Author: 2026-09-24, agent.

- Decision: the evaluation uses the grading that `teanode agent memory answers` already does (`internal/agent/evaluate_answer.go`), unchanged.
  Rationale: it exists, it is tested, and the baseline was taken with it; a second grader would make the new scores incomparable with the first.
  Date/Author: 2026-09-24, agent.

## Context and Orientation

TeaNode is a mail server with a personal agent. The agent's settings are a row of table `agent` (model `models.Agent`, `internal/models/agent.go`): its name, standing instructions, languages, dream hours. The person's own name, language and time zone are on their account (`models.User`); the agent can change those with its `account_update` tool (`internal/agent/tools/account/account.go`), but no tool sets the agent's own name or instructions.

The agent's memory is a graph of pages (table `agent_node`, model `models.AgentNode` in `internal/models/graph.go`), each holding numbered facts (table `agent_fact`, model `models.AgentFact`). A page's path says what it is about: `self` is the person, `people/<name>` somebody they know, `things/<name>` an object or an account. Facts come from conversations (the "remember" run, `internal/agent/remember.go`) and from documents read at night (the "dream", `internal/agent/dream*.go`).

A turn is one exchange in a conversation, produced by the loop in `internal/agent/ask.go` (`Agent.Ask`). The main conversation is the one conversation every agent has (`models.AgentConversationMain`). A headless turn is one the agent takes with nobody present, from a job: a schedule's turn does this (`runSchedule` in `internal/agent/schedule.go` calls `Agent.Ask` with `Headless: true`, a check-in message and a `Surface` naming where it came from), and so does a goal's check-in (`runGoal` in `internal/agent/goal.go`). Both mark the check-in message as the agent's own instruction rather than the person's, because a check-in written from what the agent read must not carry a stranger's words as the person's request.

A tool is something the model can call in a turn, registered in a package under `internal/agent/tools/` (for example `internal/agent/tools/todo/todo.go`). A tool can have an overlay: a short text shown to the model at every round of a turn, like the todo tool's list of open steps (`todoOverlay`). That is how state survives from one turn to the next without the model having to remember it.

`ask_user` (`internal/agent/tools/askuser/askuser.go`) asks the person a question and waits, but only within a turn the person is present for; a headless turn is told nobody is there. An unprompted conversation therefore does not wait inside one turn: it asks in its message and ends; the person's reply starts an ordinary turn, and the overlay carries the thread.

A job is a row in `agent_job` run by the worker (`internal/agent/agent.go`, `execute`); a sweep is code on the worker's tick that queues jobs when due, such as `queueDreaming` in `internal/agent/dream_schedule.go`, which reads the person's hours and `LastAgentPersonWordAt`, the time of their last message.

The dashboard's chat drawer is `web/src/components/agentDrawer.tsx`. It is mounted on every dashboard page, opens on a conversation, and while open follows it through the GraphQL subscription `AgentConversationEvents` (resolver in `internal/api/v1api/apigraph/agent_ask.go`). While closed it follows nothing.

The memory grading exists. `Agent.EvaluateAnswer` in `internal/agent/evaluate_answer.go` answers one question from memory (`RecallForQuestion`), from the documents (`indexed.Search`) or from both, with the conversation model and no tools, then grades the answer against the expected one as `correct`, `partial`, `not_known`, `missed`, `stale`, `invented` or `wrong`. Both calls are runs of kind `evaluate` (`models.AgentJobEvaluate`). The command `teanode agent memory answers <file> --from memory,sources,both` (`internal/cmd/agent_graph.go`) runs a JSON question set through it; the shape is in `docs/evaluation/README.md`.

Database changes are migrations in `internal/db/migrations/`, each a pair of `NNNN_name.sql` and `NNNN_name.reverse.sql` (see `docs/coding/database-migrations.md`). The next free number when this plan was written is 0109.

## Plan of Work

Milestone 1, speaking first. Add a job kind `speak_first` whose subject names the reason: `onboarding`, `memory_check` or `tip`. Its handler, `runSpeakFirst` in a new `internal/agent/speak_first.go`, takes a headless turn in the main conversation, as a schedule does, with the reason's check-in message (each milestone below writes its own) marked as the agent's own instruction and `Surface` set to `speak_first:<reason>`. Add to table `agent` (migration 0109) `spoke_first_at` (the last unprompted message), `onboarded_at`, `memory_check_enabled` and `tips_enabled` (both default true), and `speak_first_snoozed_until`.

Presence: add the subscription `AgentPresence`, which the dashboard opens once per tab while the agent is on, whatever page is shown. The browser sends, every minute and when it changes, whether the tab is visible and how long since the person's last keypress, click or scroll (`idleSeconds`). The server keeps the latest report per person in memory (no table), and treats a person as present while a visible tab reported within the last two minutes. The same subscription carries back one event, `spokeFirst`, with the conversation's id, when a `speak_first` turn starts; the drawer opens on the main conversation when it gets one, unless the person has closed the drawer on a spoken-first message in the last ten minutes.

The sweep, `queueSpeakingFirst`, runs on the worker's tick. For each agent that is on and whose person is present, it asks in order whether onboarding is due, then a memory check, then a tip (each milestone below says when), and queues the first that is due, provided the common rules hold: no `speak_first` job is open for the agent; no turn is running in the main conversation; the person's last message was at least thirty minutes ago; `speak_first_snoozed_until` has passed; and for anything but onboarding, `spoke_first_at` is at least a day ago. The person can say "not now" to any of them (snoozed for a day), and the check-in messages say so.

Acceptance: a test queues a `speak_first` job for a present person and none for an absent one or one who wrote ten minutes ago; a test with a fake model shows the turn's message written in the main conversation with its surface; in the browser, with the drawer closed on another page, a `speak_first` turn started from the command line (`teanode agent speak-first --reason tip`, added for testing and for the person) opens the drawer on it.

Milestone 2, onboarding. Due when `onboarded_at` is empty and the main conversation has no message from the person. Add the tool `agent_profile`, which sets the agent's name, its standing instructions (appended to, never replaced without asking) and its conversation language, and marks onboarding done. The check-in message asks the agent to greet the person, say briefly what it can do, and ask, a question at a time: what to call it, what to call them, which language to talk in, and what they want help with first; to save each answer as it comes (`agent_profile` for its own name, instructions and language, `account_update` for the person's name); and to end by offering to connect a mailbox or a source, with a link to the page. An overlay, while onboarding is open, lists what is still unasked. Onboarding is done when all are answered, when the person says to skip it, or after three days, and `onboarded_at` is set either way. Acceptance: a test with a fake model runs the greeting and three replies and ends with the agent's name, the person's name and the instructions saved; the sweep does not queue onboarding again.

Milestone 3, memory check questions on record. Add table `agent_evaluation_question` (migration 0110) with `id`, `agent_id` (cascade on delete of the agent), `created_at`, `modified_at`, `question_kind` (one of `direct`, `paraphrase`, `changed`, `multihop`, `abstain`, the kinds the question file already uses), `question_text`, `expected_answer`, `outdated_answer` (empty unless the question is about something that changed), `question_state` (`asked`, `confirmed`, `corrected`, `dropped`, `unsure`), `source_fact_ids` (the facts a drafted question came from, empty for one the person supplied), `is_answer_filed_after`, `conversation_id` and `answered_at`, with model `models.AgentEvaluationQuestion` and methods in a new `internal/db/database_evaluation.go`.

Add the tool `memory_check` in `internal/agent/tools/memorycheck/`. `draft` returns up to five facts to ask about, from the person's own pages first (`self`, `people/…`, `things/…`, `places/…`), across stated facts, dated events and superseded facts, skipping facts already asked about and facts the model inferred; the model writes each question and the answer it believes. `ask` records a question as it is put to the person. `record` records the reply: `confirmed`, `corrected` (with the right answer, and for a change the outdated one), `dropped` or `unsure`. `add` records a question the person supplied, typically something that changed. `list` returns what was asked and not yet answered. Add the API `ListAgentEvaluationQuestions` and `UpdateAgentEvaluationQuestion` (agent:use), and `teanode agent memory check list` and `teanode agent memory check import <file>`, which stores each question of a file in the existing shape as `confirmed`. Acceptance: database tests for the tool's actions and the import; importing three invented questions lists three.

Milestone 4, the memory check conversation. Due, as a `speak_first` reason, when `memory_check_enabled` is on, the person's own pages hold at least 100 stated facts, onboarding is done, and the last check was at least fourteen days ago, or seven while fewer than thirty questions are confirmed. The check-in message asks the agent to say it would like to check a few things it remembers and ask if now is good; if yes, to ask one question at a time with `memory_check`, give the answer it believes, and record the reply; after three or four, to ask what has changed lately and add those; and to stop at about five, or when the person wants to. A correction is offered for filing ("shall I remember that?") and filed with the memory tool only when the person agrees, which sets `is_answer_filed_after`. The overlay lists the open question and how many have been asked. "Stop asking" turns `memory_check_enabled` off. Acceptance: a test with a fake model runs the check-in and two replies and ends with two recorded questions, one corrected.

Milestone 5, the scores. Add tables `agent_evaluation_run` (`id`, `agent_id`, `started_at`, `finished_at`, `question_count`, `cost`, and per source the score and the count of each verdict) and `agent_evaluation_answer` (`run_id`, `question_id`, `answer_from`, `answer_verdict`, `verdict_reason`, `answer_text`, `cost`). The `evaluate` job kind, until now only a name for runs, becomes a queued job: queued weekly, and after a check that confirmed at least five new questions, never while a dream runs, and within the agent's budget (`RequireBudget`). It answers every confirmed or corrected question from memory, sources and both with `EvaluateAnswer`, stores each answer, and writes the run with its scores, with and without the questions whose answers were filed after they were written. `teanode agent memory answers --stored` runs the stored set now. Acceptance: a run over three stored questions with a fake model writes three answers per source and a score per source.

Milestone 6, the Memory tab. Add a "Memory check" section to the agent's Memory tab (`web/src/pages/agent.tsx`): the questions on record (kind, question, the answer on record, the outdated answer for a change, state, when answered, a mark on those whose answer was filed afterwards), each editable or droppable; the scores of the last runs per source as a small line chart; and a run's answers, filterable to the missed and wrong ones. A button "Check with me now" starts a check at once, outside the sweep's rules, as Dream now does for dreams. The agent's settings gain the two switches, memory checks and tips. Checked at desktop and phone widths. Acceptance: the section shows imported questions, a fixed answer is saved, and a run's scores appear after an evaluation.

Milestone 7, tips. Due, as a `speak_first` reason, when `tips_enabled` is on, onboarding is done, and the person is present and idle: a visible tab with no input for at least five minutes. A tip is chosen by the agent in its turn, from two things the check-in message carries: the features the person has not used, from a catalog in `internal/agent/tips.go` where each entry says what the feature is, where it lives in the dashboard or which tool it is, and how to tell whether it has been used (a schedule exists, a source of a given kind exists, a skill is installed, a goal was set, a memory page was edited by hand, and so on); and what the person did lately (their last conversations' titles and the tools they used). The prompt asks for one tip, in two sentences, tied to something the person actually did where it can be, never a tip already given. Add table `agent_tip` (migration with Milestone 7) with `agent_id`, `tip_key`, `given_at`, `conversation_id`, so none repeats. "No more tips" turns `tips_enabled` off. Acceptance: a test with a catalog of two invented features, one used, gives a tip about the other and records it; a second tip that day is not queued.

## Concrete Steps

Work from the repository root. Each milestone is one pull request with its tests. The database tests need a PostgreSQL test database (`TEANODE_TEST_DATABASE_HOST`, see `docs/reference/local-development.md`):

    TEANODE_TEST_DATABASE_HOST=<test database> go test ./internal/agent/... ./internal/db/... ./internal/api/... ./internal/cmd/...
    make lint-ci
    make check-secrets

To try Milestone 1 by hand on a development server, with the dashboard open on some page and the drawer closed:

    teanode agent speak-first --reason tip

The drawer opens on the main conversation with the agent's message.

To try Milestone 3 with an invented set:

    teanode agent memory check import docs/evaluation/memory-questions.json
    teanode agent memory check list

## Validation and Acceptance

Each milestone is accepted by the behavior its paragraph names. The plan as a whole is accepted when a new person, who never touches the command line, is greeted and has their agent named on their first visit; after two weeks of ordinary use has a question set of their own and a score that moves when memory changes; and has been told, one tip at a time and never twice, about something they had not tried, each time in a drawer that opened by itself.

## Idempotence and Recovery

The migrations are additive, each with a reverse. The sweep queues at most one `speak_first` job per agent, deduplicated by subject as other jobs are, and a turn that fails leaves nothing half-written: onboarding's answers are saved as they come, and a memory check's questions keep their states and are picked up with `list`. An import skips a question whose text is already on record. Dropping a question hides it from runs and keeps the row; deleting the agent deletes its questions, runs, answers and tips. Presence is kept in memory only, so a restart forgets it until each open tab reports again, within a minute.

## Artifacts and Notes

The hand-made check this plan replaces kept its questions on the person's machine, outside any repository, because they are about the person. Stored questions are the same kind of data: private to the person, never included in anything shared, and shown only on their own Memory tab. Examples in code, tests and documentation are invented.

## Interfaces and Dependencies

In `internal/models`:

    type AgentEvaluationQuestion struct {
        ID, AgentID                  string
        CreatedAt, ModifiedAt        time.Time
        QuestionKind                 string // direct | paraphrase | changed | multihop | abstain
        QuestionText, ExpectedAnswer string
        OutdatedAnswer               string // empty unless the question is about a change
        QuestionState                string // asked | confirmed | corrected | dropped | unsure
        SourceFactIDs                []string
        IsAnswerFiledAfter           bool
        ConversationID               string
        AnsweredAt                   *time.Time
    }

On `models.Agent`: `SpokeFirstAt`, `OnboardedAt` (`*time.Time`), `IsMemoryCheckEnabled`, `IsTipsEnabled` (`bool`), `SpeakFirstSnoozedUntil` (`*time.Time`).

In `internal/agent`: `queueSpeakingFirst` (the sweep), `runSpeakFirst` (the job), `Presence` (the in-memory record of who is present, fed by the `AgentPresence` subscription), and `runEvaluation` (Milestone 5) built on `EvaluateAnswer`. New tools: `agent_profile`, `memory_check`. In `internal/db`: the question, run, answer and tip methods. No new third-party libraries.
