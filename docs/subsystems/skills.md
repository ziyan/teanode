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
plans around them: a skill that runs commands needs a computer attached and
is never reached by a run with nobody present.

A skill's command is held to the same rule as the `shell` tool, which runs any
command on the person's computer as a write that does not ask: wrapping the
command in a skill makes it no more dangerous. What the person wants a say in
is a call that speaks for them to somebody else, or cannot be taken back, and
nothing in a command's shape says which that is. So before each such call the
fast model (the triage work) is shown the tool, the commands of the action
chosen as the skill writes them, and the arguments, and judges it `read`,
`change`, `outward` or `destructive`. The first two run; the other two ask. A
judgement that fails asks. The same call is judged once in a turn.

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
