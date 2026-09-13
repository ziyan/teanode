# Subsystems

How the parts of this server actually work: the design, the algorithm, the
numbers, and the places where a reader would otherwise guess wrong. Most of it
is the personal agent; the last two are the things a person keeps beside their
mail, and their phone synchronizes.

These are evergreen. A plan under `docs/planning/` says what was built and
when; a decision under `docs/decisions/` says why one road was taken over
another. These say what the code does today. When the code changes, they
change with it.

| Document | What it covers |
| --- | --- |
| `agents.md` | What an agent is, what it may reach, the worker, the kinds of run |
| `providers-and-models.md` | Provider clients, the registry, model choice, prices, budgets |
| `the-ask-loop.md` | A turn: rounds, tool calls, confirmation, questions, failure |
| `context.md` | The prompt's layers, the overlays, history, attachments, compaction |
| `streaming-and-instances.md` | Events, the conversation feed, what crosses instances |
| `conversations.md` | Conversations, run transcripts, titles and summaries, retention |
| `memory.md` | What an agent remembers, how it is recalled, and by what meaning |
| `jobs-and-schedules.md` | The queue, claiming, retries, and work at a time somebody chose |
| `devices.md` | A person's own computer and their own browser tab |
| `skills.md` | Tools installed from a signed registry, and how they are carried out |
| `contacts.md` | The address book, CardDAV, and what a phone may do to a card |
| `calendar.md` | The calendar, CalDAV, free-busy, and invitations by mail |

Two conventions hold throughout. *Rules* are the mailbox's rules and nothing
else. The words for the agent's own text are fixed in `AGENTS.md`: the
*conduct* is the prompt shipped with a release, *instructions* are the
person's standing words, *house instructions* the operator's, and *guidance*
is the text inside an auto-reply policy.
