# Subsystems

How the parts of the personal agent actually work: the design, the algorithm,
the numbers, and the places where a reader would otherwise guess wrong.

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

Two conventions hold throughout. *Rules* are the mailbox's rules and nothing
else. The words for the agent's own text are fixed in `AGENTS.md`: the
*conduct* is the prompt shipped with a release, *instructions* are the
person's standing words, *house instructions* the operator's, and *guidance*
is the text inside an auto-reply policy.
