# Agents

An agent is one person's. One row in `agent` per account, made the moment they
turn it on, holding what is about them: its name, their standing
*instructions*, the language and voice it writes in, their own categories,
how they want to be told things, and the operator's limits on them.

Nothing about a mailbox reaches a model until two switches are on. An operator
configures a provider and turns `agent.enabled` on, which by itself enables
nothing for anybody; then each person turns their own agent on, and grants it
each mailbox separately. `docs/decisions/20260910-agents-belong-to-people.md`
says why the agent is the person's rather than the mailbox's.

## What it may reach

A *source* is something the person has granted. Today a source is a mailbox:
`Mailbox.Agent` is an `AgentMailbox`, and `Granted` is the switch. Beside it
sits that mailbox's own policy, because a person may want sorting on one
address and answering on another:

    Granted       the agent may read this mailbox and act in it
    Triage        sort what arrives: category, priority, needs a reply
    Summaries     a summary per conversation, rewritten as it grows
    DraftReplies  a draft the person sends themselves
    Search        vectors for search by meaning
    Research      a read-only run that gathers context before a reply
    AutoReply     a policy: scope, categories, hold, caps, quiet days

Calendars and address books are meant to become sources the same way.

## Three switches, in order

A feature has to pass all three:

1. **The deployment.** `agent.features.*` — `triage`, `summaries`,
   `draftReplies`, `search`, `research`, `autoReply`, `ask`, `schedules`,
   `browser`, `connectedServers`, `computer`, `chatApps`. Unset means on.
   `agent.enabled=false` is the kill switch: queued work waits and held
   replies are cancelled.
2. **The permission.** `agent:use` lets a person have an agent, grant it a
   mailbox and talk to it; it is on the Member role by default. `agent:audit`
   is the operator's view of everybody's agents — state, usage, limits, the
   switch-off — and never their content. It is on the Operator role.
3. **The person.** Their agent's own switch, and the policy on each source.

An operator can also switch one person's agent off (`OperatorDisabledAt`),
which they cannot undo themselves, and give them a budget of their own.

## Runs

Everything a model does happens inside a *run*. There are two shapes.

**A turn** is a person talking: `Agent.Ask` starts an `AskRun`, which streams
events back to whoever is watching and can stop to ask a question or for
permission. `docs/subsystems/the-ask-loop.md` is the whole of it.

**A job** is work nobody is watching: a row in `agent_job` claimed by one
instance and run to completion. The kinds are `triage`, `research`,
`summarize`, `embed`, `reply`, `send`, `schedule`, `backfill` and `noop`.
`docs/subsystems/jobs-and-schedules.md` covers the queue, and the transcript
each job leaves is a conversation of kind `run`
(`docs/subsystems/conversations.md`).

A job is queued inside the transaction that delivered the mail — the delivery
hook `Agent.OnMailboxDelivery` does nothing else. **No model is ever called
inside the SMTP transaction.** A provider being slow or down must never bounce
mail, and a job that waits is a job, not a failure.

## What the person sees

The drawer on every dashboard page, `teanode agent ask` and `teanode agent
chat` from a terminal, and — where the operator allows it — their own Telegram
or Discord bot carrying the same primary conversation. All of them watch the
same events (`docs/subsystems/streaming-and-instances.md`), so a turn started
in one is visible in the others as it runs.

## Where to read next

    the-ask-loop.md            a turn, round by round
    context.md                 what the model is actually sent
    providers-and-models.md    which model, at what price, under what budget
    memory.md                  what it remembers between conversations
    jobs-and-schedules.md      the work nobody is watching
    devices.md                 the person's own computer and browser tab
    streaming-and-instances.md how it all looks live, on more than one server
