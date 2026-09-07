# The command line reaches mailboxes and access control by name


## Why this matters


TeaNode's client, `teanode`, has one command group per resource the server
manages: `domain`, `alias`, `user`, `mail` and so on, each with `list`,
`create`, `update` and `delete`. The mailbox work of September 2026 (folders,
rules, contacts, app passwords, out-of-office) and the access-control work
(groups, roles, permissions, the audit log) added a dozen API operations that
the client reaches only through `teanode api call`, the generic escape hatch
that takes GraphQL argument names and prints JSON. That works, but a person
who wants a folder called GitHub and a rule that files GitHub's mail into it
has to know the mutation names, spell a JSON list of rules by hand, and
remember that an input field the server treats as optional must still be
sent as an empty string.

After this plan, the same person types:

    teanode mailbox folder create GitHub
    teanode mailbox rule add GitHub --when from:contains:@github.com --move GitHub --stop
    teanode mailbox rule apply

and sees the folder, the rule, and how many messages were filed. An
administrator lists groups and roles, adds a user to a group, or reads the
audit log with commands shaped like the ones that already exist. Every new
command prints a table, or the same thing as JSON with `--json`.


## Orientation


The client lives in two Go packages. `internal/client` holds one file per
resource with plain functions such as `ListAliases(ctx, connection,
domainId)` that send a GraphQL document with `connection.Execute` and decode
the reply into a struct; `internal/client/alias.go` is the model to copy.
`internal/cmd` holds one file per command group with a `NewXCommand()` that
returns a `*cli.Command` from `github.com/urfave/cli/v3` and `runXY` action
functions; `internal/cmd/alias.go` and `internal/cmd/user.go` are the models.
Groups are registered in `cmd/teanode/main.go`. Shared helpers in
`internal/cmd`: `openClient(command)` opens the connection for the profile or
`--url`; `printTable(headers, rows)` and `printFields(pairs)` print tables and
records; `PrintJSON(value)` and `JSONFlag()` serve `--json`; `confirm(command,
warning)` asks before something destructive unless `--force`; `usage(message)`
is a usage error; `describeError` and `describeNotFound` turn a server error
into a sentence.

The server side is `internal/api/v1api/apigraph`, where every resolver is a
method on `graph` listed in an interface and turned into GraphQL by reflection
(`internal/util/graphapi`). A struct field of an argument type is required
unless tagged `graphapi:"nullable"`. `internal/mx/exchange_rules.go` runs a
mailbox's rules when a message arrives, and `RuleMatches` there is the one
matcher; the dry run `TestMailboxRules` in `apigraph/mailbox_rules.go` calls
it too.

The documentation to keep in step is `docs/reference/command-line.md`
(the table of command groups and the "Reaching the whole API" section) and
`CHANGELOG.md` under Unreleased.


## Decisions


Mailbox selection. Most people have one mailbox, so every command that acts
on a mailbox takes `--mailbox <id or name>` and, when the flag is absent and
the caller has exactly one mailbox, uses that one. With several and no flag
the command says which ones there are and stops. Folders, likewise, are named
by their id or by their name within that mailbox, case-insensitively; a name
that matches two folders (the same name under two parents) is refused with
both ids shown.

Rules are edited whole on the server (`UpdateMailbox` takes the full list),
so `mailbox rule add`, `remove`, `enable` and `disable` read the mailbox,
change the one rule, and write the list back. A rule is named by its name.
`--when` is repeatable and reads `field:operator:value`, or
`header:Name:operator:value` for a header condition, or `sender-known` and
`any` alone; every condition must match. The actions are flags: `--move
<folder>`, `--mark-read`, `--flag`, `--forward <address>`, `--delete`, and
`--stop` ends the run after this rule.

Applying rules to mail already in a folder is a new server mutation,
`ApplyMailboxRules(mailboxId, folderId, first)`, because the client cannot do
it faithfully: forwarding needs the server. It runs the stored rules over the
folder (the Inbox by default), performs the move, mark-read, flag and delete
actions exactly as arrival does, and skips forward, which would resend old
mail; the reply says how many messages matched and were changed. It needs
`mail:write`, and `mail:send` is not consulted because nothing is sent.

Input fields the web form always sends as empty strings become nullable in
the schema, so that `teanode api call` and any other client may omit them:
a rule's `header`, `value`, `folderId` and `address`, and an out-of-office
reply's `subject`, `text` and `html`.

Groups and roles are named by id or name. `group add` and `group remove` take
`--user`, `--role` and `--domain`, repeatable, and change membership one step
at a time; `group create` and `group update` take the same flags to set the
whole list. Roles take `--permission`, repeatable; `role permissions` lists
what exists. The audit log is `audit list` with the filters the API has.

Every GraphQL document the new client files send is a package-level string
registered in `client.Documents`, and a test in `apigraph` parses and
validates each one against the schema the server builds, so that a renamed
field breaks the build rather than the command.


## Milestones


Milestone one, the server. Tag the four rule fields nullable; add
`ApplyMailboxRules` with a database-level test that a rule moving mail files
what is in the Inbox and leaves the rest; run `make test`.

Milestone two, the client package. Add `mailbox.go` (views, folders, rules,
out-of-office, contacts, app passwords, mail program settings, the directory
of all mailboxes), `group.go`, `role.go` and `audit.go` with typed functions
and registered documents; add the schema-validation test.

Milestone three, the commands. Add `mailbox`, `folder`, `contact`,
`app-password`, `group`, `role` and `audit` groups; register them; write the
reference table and the changelog; unit-test the `--when` parser and the
mailbox and folder name resolution.

Milestone four, proof. Against the running server: create a folder, add a
rule, apply it, list contacts, list groups and roles, read the audit log, each
as a table and as JSON, then remove what the proof created.


## Progress


- [x] The folder and the rule the plan opens with exist on the owner's
      mailbox, made with `teanode api call`, and the three GitHub messages
      already in the Inbox were moved by hand (2026-09-07).
- [x] Milestone one: the rule input fields are nullable, and
      `ApplyMailboxRules` runs the stored rules over a folder, counting what
      it did and skipping forwards.
- [x] Milestone two: `internal/client/mailbox.go` and `access.go`, every
      document validated against the schema by
      `TestClientDocumentsMatchTheSchema`.
- [x] Milestone three: `teanode mailbox` (folder, rule, contact, device,
      autoreply, programs), `teanode group`, `teanode role`, `teanode audit`;
      the reference and the changelog say so; the condition parser, the
      folder lookup and the membership arithmetic are unit-tested.
- [ ] Milestone four: the proof against the running server.

Beyond the plan, at the owner's ask: who may do what is its own row in the
rail rather than four tabs inside the server's page, and the accounts and the
groups are one page there — choosing a group narrows the people to its
members, and membership is edited from either side.


## Surprises


- `MailboxRuleCondition.header` and `MailboxRuleAction.address` were required
  in the GraphQL input types because nothing tagged them nullable; the web
  form never noticed because it always sends every field.
