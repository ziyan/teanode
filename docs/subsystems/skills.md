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

A workflow can also **route**: one tool with an `actionField` naming one of its
own parameters, and a list of steps per value of it. That is how a skill offers
six operations as one tool rather than six.

Two more things a skill may declare. **Secrets** are values it needs and does
not carry, named as keys the operator fills in under `agent.skillSecrets` and
reached as `{{secret:KEY}}`. **Authentication profiles** are named ways of
authenticating — bearer, basic or an API key — that several steps share, so a
token is written once.

## Templating

`{{name}}` is one of the tool's own parameters. `{{steps.place.lat}}` is the
value `lat` that the step called `place` selected. `{{secret:KEY}}` is a
declared secret. A bar adds a filter, and there is one: `{{enabled|json}}`
writes the value as JSON rather than as text, so a number or a `true` lands in
a body unquoted.

Nothing else is understood. No expressions, no arithmetic, no function calls.

**Every reference is checked when the skill is read**, not when it runs: one
naming a parameter the tool does not take, a step that has not run yet, or a
secret the skill never declared, is refused at install. A reference that
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
runs on this server. Each word is quoted before it travels.

That choice has consequences the tool's own description states, so the model
plans around them: a skill that runs commands needs a computer attached, asks
the person first, and is never reached by a run with nobody present.

## Who installs, and who gets the tools

An operator installs a skill and it belongs to the server. Installing needs
`server:manage`, the same permission that changes settings and declares a
connected server — one list to audit, rather than every person separately
deciding to trust a registry.

Everybody's agent is then offered what it declares, named
`skill__<skill>__<tool>` in the `skills` family, subject to the ordinary tool
policy: an operator can switch off the whole family or a single tool by name in
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
| read from one answer | 256 KiB, or its own `maxBytes` |
| index and skill file | 1 MiB each |
| how long the tools are kept before the rows are read again | 30 seconds |

## From a terminal

    teanode agent skill search            what the registry offers
    teanode agent skill install weather   install it, checking the signature
    teanode agent skill list              what is installed, and what it brings
    teanode agent skill update            install every newer version there is
    teanode agent skill disable weather   keep it, stop offering its tools
    teanode agent skill remove weather    take it away

## Caveats

- **A skill is only as trustworthy as the registry's key.** The signature says
  the file is the one that publisher published; it says nothing about what the
  file does. An operator should read what they install — the whole file is
  stored, and the dashboard says which tools it brings and whether any of them
  runs commands.
- **A skill that stops parsing is left out**, logged, and shown in the
  dashboard as unreadable rather than silently missing.
- **Secrets are the operator's to fill in.** A skill that needs one and does
  not have it fails when its step runs, naming the key.
- **The tools are read again at most twice a minute**, so a skill installed
  from another instance takes up to half a minute to appear on this one.
