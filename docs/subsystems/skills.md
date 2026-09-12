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
come from a secret the operator sets. A reference that
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

## Secrets

A skill carries none of the credentials it needs. It declares the keys, and
each one says whose value it is.

**`scope: operator`**, the default, is one value for the whole server, filled
in under `agent.skillSecrets`. That is right for something the deployment has:
the address of a camera system, a licence, a key the organisation bought.

**`scope: person`** is a value of each person's own. It is filled in on their
own agent page, or with `teanode agent skill secret set`, and kept sealed with
the server's secret against their agent — the same way a connected server's
credential is. That is right for a credential that is theirs: their account
with a service, their own key.

The skill's author decides, because only they know which kind it is. Getting it
wrong in one direction shares one person's account with everybody; in the other
it asks everybody for a value the operator already has.

Three rules follow from the split:

- A person-scoped key is **never** taken from the operator's list, even when a
  value is written there. Falling back would defeat the point of scoping it.
- A tool whose person-scoped secret is unset **refuses before it runs**, naming
  the key and where that person sets it, rather than making a request with a
  hole in it.
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

A **shell** step is carried to the person's own attached computer and run
there, through the same relay the `shell` and `filesystem` tools use. It never
runs on this server. Each word is quoted before it travels — for `/bin/sh`,
which is why a skill's commands are refused outright on a Windows computer:
`cmd` gives those quotes no meaning and a value would become another command.

That choice has consequences the tool's own description states, so the model
plans around them: a skill that runs commands needs a computer attached, asks
the person first, and is never reached by a run with nobody present.

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

The risk of a skill's tool is read from what it does: a tool with any shell
step is destructive and always asks; one whose requests are all plain reads is
a read; anything else is a write. Every answer is marked untrusted — it is
data fetched from outside, never words addressed to the agent.

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

    teanode agent skill secret list       what the skills ask you for
    teanode agent skill secret set news NEWSAPI_KEY -
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
