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

Calendars and address books are meant to become sources the same way. Until
then the agent reaches the person's own calendar through the same permission a
person needs for it (`calendar:use`): it can read the agenda, say when they are
free, and put something in, change it or take it out. Anything that would send
mail — inviting people, or telling the ones already invited that a meeting has
moved or is off — is *outward*, so the person is asked first, and an event with
guests on it is refused outright until the call says they are to be told.
`docs/subsystems/calendar.md` has the reasoning.

## One tool per thing

The catalog is the request: every tool in it is a paragraph the model reads
before it decides anything. It grew to eighty-four names, of which seventeen
were the operator's domains — a verb apiece for domains, addresses,
credentials and the queue — and a model choosing between eighty-four
near-identical names chooses worse than one choosing between fifty.

So a tool is one thing, and its first argument is the action:

    domain      list, get, add, update, remove, dns
    alias       list, match, add, update, remove
    credential  list, create, update, remove
    queue       list, retry
    rule        list, add, update, remove, test, apply
    user        list, add, update, remove
    calendar    agenda, free, add, edit, remove
    mail_audit  search, get, content, mark
    account     get, update
    settings    get, update

Two things survive the merge, and they are the two that matter. **The risk is
the action's own**: reading a diary is a read and taking an appointment out of
it cannot be undone, so the tool's declared class is the gentlest of its
actions and the call is priced by the one in hand. A call nobody can parse
takes the strictest class any of its actions carries, so that writing a call
badly is not a way past the question. **The permission is the action's own
too**: the catalog offers the tool to anybody one of its actions would admit,
and each action checks its own before it runs.

A tool that already takes an `action` of its own — `folder_manage`,
`group_manage`, `mail_act` — cannot be an action of another, because the two
fields would collide; the code says so and refuses to build such a catalog.

`internal/agent/tools/merged.go` is the whole of it, and
`tools.Renamed` maps every old name to what it became, so an operator's
policy written before the merge still means something.

## Sorting, and the two words that earn a rule

Triage answers with a category from a fixed list -- personal, work,
newsletter, notification, receipt, promotion, social, invitation, phishing,
junk, other -- plus whatever the person has added. Fixed so the chips
translate and a rule written today still means the same thing next year.

Two of them are there for one purpose. **Phishing** is a message written to
take something by pretending to be somebody it is not, and the tell is the
sender against the claim: a password notice about an address at one domain,
sent from another. **Junk** is mail nobody asked for from somebody with no
reason to write, which is not the same as a promotion -- a promotion comes
from a sender the person actually deals with, however much of it there is.

Neither is a verdict from a check. The spam filter and DMARC run first and
refuse what they can; what reaches the agent is the mail that passed both and
is still a lie. And neither files anything by itself: the agent says what a
message is, and a **rule** says what to do about it, because filing belongs
where the person can see it, change it and turn it off.

Every mailbox starts with that rule, called **Phishing and junk**:

    category matches ^(phishing|junk)$  →  move to Junk, mark read, stop

It is switched on from the first day, and it is safe to ship switched on
because nothing but an agent ever sets the category it asks about — on a
mailbox with no agent it matches nothing at all. A mailbox that already had a
rule of its own about these categories kept it. Like any other rule it sits in
the list, says what it does, and can be changed or switched off.

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
