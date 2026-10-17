# Skills: tools that arrive without a release

Everything else the agent can do was compiled in. A skill is a file of
declarations, fetched from a registry, whose tools join the catalog while the
server is running.

`internal/skills/`, `internal/agent/tools_skill.go`,
`internal/api/v1api/apigraph/agent_skill.go`, `internal/cmd/agent_skill.go`.

## What a skill is

Markdown with a YAML header. The header is the whole of what the server reads;
the prose under it is for whoever opens the file.

The header names the skill, describes it, and declares tools. Each tool has a
name, a description — which is what the model reads to decide whether to call
it, and so the most important line in the file — a JSON Schema for its
parameters, and one of three ways of being carried out:

- **http**, one request;
- **shell**, one command, as a list of words rather than a line, because a
  line invites the quoting to be got wrong;
- **workflow**, a sequence of those, each named, where a later step may use
  what an earlier one selected.

A step may carry an `if`, and runs only when it holds. A condition is one
value — true unless it is empty, false or zero — or two values compared with
`==` or `!=`; anything else is refused when the skill is read. A name that
stands for nothing is empty rather than an error, so "only if the last step
found one" is what the field is for.

A workflow can also **route**: one tool with an `actionField` naming one of its
own parameters, and a list of steps per value of it. That is how a skill offers
six operations as one tool rather than six.

Two more things a skill may declare. **Secrets** are values it needs and does
not carry, reached as `{{secret:KEY}}`. **Authentication profiles** are named
ways of authenticating — bearer, basic or an API key — that several steps
share, so a token is written once.

A secret says whose it is — see **Secrets**, below.

## Templating

`{{name}}` is one of the tool's own parameters. `{{steps.place.lat}}` is the
value `lat` that the step called `place` selected. `{{secret:KEY}}` is a
declared secret. A bar adds a filter, and there is one: `{{enabled|json}}`
writes the value as JSON rather than as text, so a number or a `true` lands in
a body unquoted.

Nothing else is understood. No expressions, no arithmetic, no function calls.

**Every reference is checked when the skill is read**, not when it runs: one
naming a parameter the tool does not take, a step that has not run yet, or a
secret the skill never declared, is refused at install. So are three other
things: a parameter schema that could not be sent to a model as JSON, which
would otherwise break every request from every person on the server; a
condition the interpreter cannot work out; and a step that sends a credential
to a host filled in when the tool is called, which would let whoever calls it
be handed the operator's secret. The host must be written into the skill, or
come from a secret settled by somebody at least as trusted as the credential
travelling with it: an operator's secret always, a person's own only when
nothing the operator's is being sent. A reference that
resolved to nothing would otherwise become an empty string in an address, and a
skill that quietly fetches the wrong thing is worse than one that will not
install.

A value written into an address is escaped, so a place name with a space in it
reaches the service and a value carrying a `?` or an `&` sits in the address
rather than adding to it. An address that is one reference and nothing else is
a link an earlier step found, and is passed on exactly as it came.

## Selecting

`result: json` parses the answer; `select` then names values by a path — keys
separated by dots, with a number or `[number]` for a place in a list, so
`properties.periods[0].shortForecast` and `0.lat` both work. `result: text`
keeps the answer as text under the name `text`. A step with no `select` hands
back the whole parsed answer under `json`.

`result: image` is a picture, and it does not go into the answer at all. The
bytes are handed to the agent, which shows them to the model as a picture and
files the same picture in the conversation, where the person sees it under the
tool line; what the step returns is a line saying what was fetched, how large
it was, and where the person's copy is. That is the whole point of the kind: a
JPEG read as text is a few hundred thousand characters that tell a model
nothing about what is in the picture. Only the sorts a model can be shown and
a browser can draw are accepted — PNG, JPEG, GIF and WebP — and a step that is
sent something else, an empty answer, or more bytes than it reads is refused
rather than handed on in half. Such a step reads 2 MB before `maxBytes`, since
a camera's snapshot is routinely larger than the reading default.

`result: file` is the same for bytes nothing here reads — a clip, a document,
an archive. The person is handed it in the conversation and the model is told
what it is, not shown it; the answer carries the attachment's id, which is
what `filesystem put` takes to write the file onto the person's own computer.
That is the whole path for anything a model cannot read: fetch it, put it on
their machine, and let their own programs work on it. Such a step reads 32 MB,
the same bound the agent puts on handing a file over.

## Braces of the skill's own

`{{{{` and `}}}}` pass through as a single `{{` and `}}`, the way a doubled
brace does in a format string. That is how a skill talks to a service whose
own payload is written in braces — a Home Assistant template, a dashboard's
query, a webhook's body. Without it such a payload would be read as values
the skill was meant to provide, and the file refused for naming values
nothing knows. The two can be mixed freely: a fixed template with one value
filled in from the call reads

    {% for s in states[{{domain|json}}] %}{{{{ s.entity_id }}}}{% endfor %}

where `{{domain|json}}` is this server's and the doubled braces are the
service's. A skill file carrying a zero byte is refused, since that is what
the escape hides behind while the references are read.

## Signing in

The steps of one run share their cookies. A step that posts a name and a
password is therefore followed by steps that are signed in, which is the only
way into a good deal of equipment: a session handed out at one address, and
everything else answered only to that session. Nothing needs to be declared
for it. The cookies belong to that one run — a later call starts with none —
and the jar sends each one back only to the host that set it, so a step
pointed at another address cannot carry a session out with it.

## Secrets

A skill carries none of the credentials it needs. It declares the keys, and
each one says whose value it is.

**`scope: operator`**, the default, is one value for the whole server, filled
in under `agent.skillSecrets`. That is right for something the deployment has:
the address of a camera system, a licence, a key the organisation bought.

**`scope: person`** is a value of each person's own. It is filled in on their
own agent page, or with `teanode agent skill secret set`, and kept in a row of
that person's own, its value sealed with the server's secret — the same way a
connected server's credential is. The seal is over the value alone, not bound
to the agent; what keeps one person's value out of another's requests is the
row's key, and what a database copied without `server.secret` is worth is
nothing. That is right for a credential that is theirs: their account with a
service, their own key.

The skill's author decides by default, because they know what the value is.
Getting it wrong in one direction shares one person's account with everybody;
in the other it asks everybody for a value the operator already has.

**The operator can overrule them**, for a whole skill at a time, because the
author cannot know the deployment. The same camera skill serves a household
with one console — one address, one token, filled in once — and an office where
twenty people each have their own. On the Skills card, in `teanode agent skill
scope <name> operator|person|skill`, and through the agent's `skill` tool, an
operator says which this is; `skill` leaves it to the declaration. The choice
survives an update, and applies to every secret the skill declares, which is
what keeps the host rule below from being anything to think about: settle on
one answer and the host and the credential are always scoped alike.

Three rules follow from the split:

- A person-scoped key is **never** taken from the operator's list, even when a
  value is written there. Falling back would defeat the point of scoping it.
- A tool whose person-scoped secret is unset **refuses before it runs**, naming
  the key and where that person sets it, rather than making a request with a
  hole in it. Only the keys *that tool* uses are waited on, so a skill whose
  other tools want other keys still works.
- A host written as `{{secret:…}}` may be a person's own only when nothing the
  operator's is sent with it. Otherwise one person would be choosing where
  everybody's credential goes, which is the same hole as a host named by a
  caller — see below.
- Taking a skill off the server forgets what everybody filled in for it, so a
  sealed value cannot outlive the skill that asked for it and come back under a
  later version that means something else by the same key.
- The agent can ask which of a person's keys are unset, and is told never to
  ask for the value. A secret typed into a conversation is kept in the
  transcript and sent to a model.

Only a key an installed skill actually asks for can be set, so the table cannot
be used as a general place to keep secrets. Nothing ever reads a value back
out: the API says whether it is set, and no more.

## Trust

Nothing is trusted because of where it came from.

The registry publishes an index in which each entry carries an **Ed25519
signature** over the skill's name, version, address and content hash together —
all four, so an entry cannot be pointed at another file, and another version
cannot be passed off as this one. The public half of the key is **built into
the server**, not fetched: a key taken from the same place as the thing it
vouches for proves nothing.

After the signature checks out, the file is downloaded and its **SHA-256** must
be the one that was signed for. Then it must parse, and call itself what the
registry calls it. Only then is it stored — the whole file, so a server that
can no longer reach the registry still has everything it needs and an operator
can read exactly what is installed.

## Where a skill's steps run

An **http** step is made by the server through the same address guard the
`web_fetch` tool uses, so a skill cannot use the mail server as a way into the
network it sits in. A private or loopback address is refused.

With one exception the operator writes: `agent.allowPrivateAddresses` lists
equipment on their own network — an address, a range, or a name — that a
skill's step may reach. It exists because a skill is pointed at something the
operator chose, which is not the case the guard is defending against: a
camera controller or a printer with an API and no public name is a reasonable
thing to give a skill, and the address of it is theirs rather than a
stranger's. The same list widens the headless browser, and nothing else. What
goes to an address out of somebody else's mail — the remote image proxy, the
one-click unsubscribe, `web_fetch` — is not widened by it, because an agent
that has just read a message is precisely what the guard is for.

A **shell** step is carried to the person's own attached computer and run
there, through the same relay the `shell` and `filesystem` tools use. It never
runs on this server. Each word is quoted before it travels — for `/bin/sh`,
which is why a skill's commands are refused outright on a Windows computer:
`cmd` gives those quotes no meaning and a value would become another command.

That choice has consequences the tool's own description states, so the model
plans around them: a skill that runs commands needs a computer attached, and a
run with nobody present reaches it like any other tool, with what would ask
first refused.

A skill's command is held to the same rule as the `shell` tool, which runs any
command on the person's computer as a write that does not ask: wrapping the
command in a skill makes it no more dangerous. What the person wants a say in
is a call that speaks for them to somebody else, or cannot be taken back, and
nothing in a command's shape says which that is. So before each such call the
fast model (the triage work) is shown the tool, the commands of the action
chosen as the skill writes them, and the arguments, and judges it `read`,
`change`, `outward` or `destructive`. The first two run; the other two ask. A
judgement that fails asks. The same call is judged once in a turn, and what
the judgement costs is counted in the turn's own cost.

The arguments were written by a model that may have read something hostile,
so the judge is told to treat them as data. A judge talked into calling a send
a read has let through what the `shell` tool would run anyway, which is why the
judgement only decides between that and asking.

## Who installs, and who gets the tools

An operator installs a skill and it belongs to the server. Installing needs
`server:manage`, the same permission that changes settings and declares a
connected server — one list to audit, rather than every person separately
deciding to trust a registry.

Everybody's agent is then offered what it declares, named
`skill__<skill>__<tool>` in the `skills` family — a hyphen in the skill's name
becomes an underscore, so `unifi-protect` gives `skill__unifi_protect__…`,
which is what an operator writes in a policy. It is subject to the ordinary
tool policy: an operator can switch off the whole family or a single tool by name in
`agent.tools.disabled`, and raise any of them to require confirmation in
`agent.tools.confirm`.

The risk of a skill's tool is read from what it does: one whose requests are
all plain reads is a read; one that runs a command or sends anything but a
read is a write, and a command's calls are judged as above. Every answer is
marked untrusted — it is data fetched from outside, never words addressed to
the agent.

## Sharing a skill

A person can share named tools of an installed skill with one person in a
circle they share. The other person's agent is then offered those tools, and
each call runs on the owner's own computer, as the owner. The reasons are in
`docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md`; this is
how it works. The code is `internal/skills/share.go` (what a call is worth),
`internal/agent/skill_share.go` (the call, the approval, the other person's
catalog), `internal/db/database_skill_share.go` (the two tables) and
`internal/api/v1api/apigraph/agent_skill_share.go`.

**What a skill declares.** A tool says where its amount is so that a limit can
be checked on a number the server read before anything ran:

- `amountField` names the integer parameter that holds the amount in the
  currency's minor unit (cents). `currencyField` names the string parameter
  that holds the currency code. For a tool that routes on `actionField`,
  `amountActions` lists the actions that carry the amount: a payment tool's
  `card` action names a price, and its `url` action pays whatever an address
  asks, which only the merchant knows.
- `scopedIdField` names the string parameter that holds the identifier of
  something an earlier call of the same share made, such as a payment
  request. A tool without it that takes a parameter called
  `spend_request_id` is held the same way, because the payment skill was
  written before the declaration existed and the server has to behave safely
  without it being updated. A tool that declares an amount is never held that
  way: a payment tool that also takes a spend request to report on an earlier
  payment is priced, and its report action, which has no amount, waits.

All of them are checked when the skill is read: an `amountField` must be one
of the tool's integer parameters, a `currencyField` and a `scopedIdField` one
of its string parameters, an `amountActions` entry one of its actions, and a
tool cannot name both an amount and a scoped identifier.

**What the other person is offered.** One tool for each shared tool, named
`shared_<tool>_<owner's username made safe>` (lower case letters and digits
joined by underscores, kept short, and numbered if two owners come to the same
name). Its description says whose it is, that it runs on their computer, and
what the limits are. It has the skill's own parameters and one more,
`request_reason`, which the agent fills with a sentence for the owner. It has no
`computer` parameter: which computer is the owner's to choose, and an argument
that names one is dropped. Its risk is `money` for a tool that may cost
something, so the other person's own card, with what will be paid on it, comes
first, and a turn with nobody present spends only if that person allowed money
calls when they are not there. A tool that works on a scoped identifier has
risk `write`. The card the person answers before the agent shares a skill is
built from the arguments and says what is granted: who, which tools (or "some
of its tools" when none are named), the limit for one payment and for a month
with their currency, and that anything above waits, or that every call waits.
The tools are read again for every turn, so taking a share back,
leaving the circle, switching the owner's agent off or removing the skill
takes them from the next turn; and every call is checked again when it is made.

**What happens to a call.** `Agent.RunShared`, in this order:

1. The share must still exist, the two must still share a circle, the tool
   must be one shared, the skill must be installed and switched on, and the
   owner's agent must be on. Otherwise the call answers that it is not
   available, and nothing is recorded.
2. The server works out what the call pays from the arguments and holds it to
   the share, with the share's row locked so that two calls at once take turns.
   A call waits for the owner when the share asks every time; when the tool
   declares no amount, or the action is not one of `amountActions`, or the
   amount is missing, not a whole number above zero, or not in the share's
   currency (all of which are cases the server cannot price); when it is above
   the limit for one payment; or when it would take the calendar month (UTC)
   past the monthly limit. The month's total is the calls that ran and the calls
   that failed, in the share's currency, counted from when they ran, and nothing
   else. It is counted by who the calls were between, the owner's agent, the
   other person and the skill, and not by the share: a call carries those
   itself, so a share made again after the first ended starts the month where
   it was. A failed call counts because a failure after the command acted cannot be
   told from one before it, and the limit is the owner's bound on what could have
   been spent. A tool that declares an amount waits too on a share with no limit
   of either kind that is not set to ask every time: there is nothing a call
   could be within.
3. A tool with a scoped identifier is never priced. Its call is turned back
   unless it names an identifier that appears, as a whole word, in the output
   of an earlier call of the same share that ran (and has at least six
   characters), so `retrieve` and `cancel` work on a request the share made and
   the listings of the owner's other requests are never shared. A command prints
   one piece of text, so the identifier is looked for inside it; a word that is
   followed by a colon is the name of a field and does not count.
4. A call that runs a command is judged, once, by the same fast model that
   judges the owner's own commands, with the owner's name and the owner's agent's
   name. Reading, changing and spending go ahead, because the share is the
   owner's word on spending. Speaking for the owner, giving somebody access,
   something that cannot be undone, and a judgment that could not be had make
   the call wait.
5. A call that goes ahead is written down as `ran` and then carried out, by
   calling the very runner the owner's own agent uses for that tool, with a run
   made for the owner's agent. That is the whole of "on the owner's computer as
   the owner": their secrets, their reach for the skill, their attached computer and
   the skill's own rules. It is written before it is carried out so that a call
   still in flight counts toward the month, and changed to `failed` if it
   errors. With the owner's computer not connected the call answers that and
   leaves no row at all.
6. A call that waits is stored with its arguments exactly as asked, the
   amount the server read, and the reason the other person's agent gave, for a
   day. A share has at most twenty waiting at once; the next is turned back
   until some are answered, so an agent that asks again and again cannot bury
   the one they meant to answer.

**The approval.** The owner gets a card in their main conversation (a note of
the kind `skill_share_call_waiting`) with who asked, the tool, what it was given, the
amount, and the reason their agent gave, shown as that person's words and never
as an instruction; and a notification of the kind `skill_share_call` on their phones,
whether or not they are looking at a screen, which only opens the app. They
answer with `AnswerSkillShareCall`, from the card, the agent page or the
iPhone app; `teanode agent skill calls` lists what waits, with where to
approve it, and `decline` says no. Approving runs exactly the stored call with the
stored arguments, skipping the limits and the judge, since the approval is their
word on this call, and it needs their computer connected first: with it away the
approval is refused and the call keeps waiting. The answer is a conditional
update from `waiting_for_approval`, so two answers at once, from the phone and
the drawer, change the row once and run the call once. A call nobody answers in
a day expires, in the worker's sweep.

Approving, and lending money, are a person's tap, and the server holds that.
`AnswerSkillShareCall` with `isApproved` and `ShareSkill` (making a share,
raising its limits or its tools, or turning off asking every time) are accepted
only from the dashboard's session, which has passed the check against forms on
other sites, or from the iPhone app's own sign-in. A command line token, an API
token, the console and the agent's own operations are refused, with "approve it
on the dashboard or in the iPhone app". That is the whole guard, because an
agent can drive the owner's computer (the `shell`, `terminal`, `claude_code`,
`codex` and `herdr` tools reach it, and their saved command line sign-in is
there), and anything it types arrives as their token.

How the server tells the phone app's token from a command line token: the
token row has a `client_kind`, `app` or empty, and `Principal.CredentialKind`
carries it (`session`, `app` or `token`) for the operations that need a tap
(`Principal.IsPersonsTap`). The app's sign-in marks its token, by a connect code
or by its own consent, only when the code or the approval was made where a
person was tapping, that is on the dashboard or in an app already marked. A code
made with a command line token (`teanode agent phone pair`, or a card an agent
shows) is not marked: its phone works for everything but these two things, and
connecting it again from the dashboard's Connect page marks it. A renewal keeps
its token's kind. Tokens the app held before the column existed are marked when
the migration runs.

What only takes away stays open to every sign-in of the owner: declining a call,
taking a share back, lowering a share's limits or tools in the same currency,
and turning asking every time on. The agent's own operations may do the last two
and not the others, and are refused declining and approving both, since an
agent speaking for the owner is the thing the answer is not. The `skill` tool
has actions to share, unshare, list shares and list calls, and none that
answers a call; the parity test fails if one is added. Its `share` action is a
granting call on a card the person answers, and shares only so that every call
asks (`is_approval_required` true, no limits): the card is answered by a request
the tool cannot see the kind of, so a share that lets calls through unasked is
left to the dashboard and the app, which the server enforces. Nothing the tool
shares runs without the owner's approval.

On top of that is a courtesy: a `shell` call, or a `terminal` start or typed
text, whose words look like `teanode agent skill approve` or `decline`
(`tools.AnswersSkillShareCall`: the words agent, skill and approve or decline in
that order, with a few between) asks them first, as money, and `personsAnswer`,
which nobody can allow, keeps it from running in a turn with nobody present.
It is a guess over words, not a parse of the shell, and it does not have to
hold: the server refuses the approval however the command was written.

**When a share ends,** because the owner took it back or because the two stopped
sharing a circle, its calls stay. A call carries the owner's agent, the other
person, the skill and the tool it was a call of, and loses only the share it
was made through; the ones that were waiting are expired, since nobody can
approve a call of a share that is gone. The owner lists the calls of ended
shares (`ListSkillShareCalls` with `isShareEnded`, the dashboard's "Shares that
ended", `teanode agent skill calls --ended`, the tool's `calls` with
`is_share_ended`), and the other person lists their own.

**Telling the other person.** The conversation the call came from is woken with
how it came out (approved and ran, approved and failed, declined, expired), the
owner's note and what it answered fenced as data, through the same waker finished
background work uses and under the same bounds. The row says when it was told,
and the telling is claimed first, so the instance that decided the call and the
sweep never both wake it. A call made while the agent was answering in a chat
wakes the person's main conversation instead, whose turns are not posted to the
room, and the woken turn is told which chat it came from. Their phone is told too, unless they are looking at a
screen.

**Saving a skill again.** Installing a newer version of a skill checks the
shares of it against the rule that applied when they were made
(`Skill.CheckShareable`, in `holdSharesToSkill`): a tool that now declares an
amount, shared with no limit and without asking every time, or one the skill no
longer declares, would otherwise let the other person spend without bound. Each
share that no longer qualifies is set to ask every time, in the same
transaction, and its owner is told in their main conversation which shares and
why (`Agent.TellSkillSharesNowAsk`). Nothing else about the share changes.

**Leaving a circle** deletes the skill shares between two people who no longer
share any circle, in the same transaction, beside the calendar and page shares,
and keeps their calls as above. Taking a skill off the server deletes the shares
of its tools.

| | |
| --- | --- |
| how long a call waits for the owner | a day |
| calls of one share waiting at once | 20 |
| the month the monthly limit counts over | the calendar month, UTC |
| shortest identifier a scoped call may name | 6 characters |
| how often the sweep looks for expired and untold calls | once a minute |

## Constants

| | |
| --- | --- |
| steps in a workflow | 10 at most |
| seconds per step | 30, or what it asks for, 120 at most |
| read from one answer | 256 KiB, or its own `maxBytes`, 4 MiB at most |
| index and skill file | 1 MiB each |
| how long the tools are kept before the rows are read again | 30 seconds |

## From a terminal

    teanode agent skill search            what the registry offers
    teanode agent skill install weather   install it, checking the signature
    teanode agent skill list              what is installed, and what it brings
    teanode agent skill update [name]     a newer version of one, or of each
    teanode agent skill disable weather   keep it, stop offering its tools
    teanode agent skill enable weather    offer its tools again
    teanode agent skill remove weather    take it away
    teanode agent skill scope unifi-protect person
                                          each person's own values

    teanode agent skill secret list       what the skills ask you for
    teanode agent skill secret set news NEWSAPI_KEY
                                          your own value, read without echo
    teanode agent skill secret clear news NEWSAPI_KEY

    teanode agent skill share payments ben --tools pay --ask-every-time
                                          let ben ask to use pay on your computer
    teanode agent skill shares            what you shared, and what is shared with you
    teanode agent skill calls <share-id> [--waiting]
                                          what was asked, and where to approve it
    teanode agent skill calls --ended     the calls of shares that ended
    teanode agent skill decline <call-id> [--note ...]
    teanode agent skill unshare <share-id>

Sharing with limits, which lets a call through unasked, and approving a call are
done on the dashboard's agent page or in the iPhone app; from a terminal the
server accepts only asking every time, and lowering what a share allows.

## Caveats

- **A skill is only as trustworthy as the registry's key.** The signature says
  the file is the one that publisher published; it says nothing about what the
  file does. An operator should read what they install — the whole file is
  stored, and the dashboard says which tools it brings and whether any of them
  runs commands.
- **A skill that stops parsing is left out**, logged, and shown in the
  dashboard as unreadable rather than silently missing.
- **An operator's secret is one value for everybody.** A skill that needs one
  and does not have it fails when its step runs, naming the key. A
  person-scoped one fails before the step runs, naming the key and where that
  person sets it.
- **The agent can see which of a person's keys are unset and never asks for
  the value.** A secret typed into a conversation is kept in the transcript and
  sent to a model, so the tool says where to set it instead.
- **The tools are read again at most twice a minute**, so a skill installed
  from another instance takes up to half a minute to appear on this one.
