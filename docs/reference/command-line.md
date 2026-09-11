# The command line

TeaNode is two programs. `teanode-server` is the mail server, and the few
things only its own host can do. `teanode` is the client: it administers a
server over the API, from anywhere, and is what an operator has open.

Everything the client changes goes through the running server rather than
into the database directly, so that a change made from the shell behaves
exactly like the same change made in the dashboard — validated the same way,
with the same side effects. See
`docs/decisions/20260818-the-cli-goes-through-the-api.md` for why, and
`docs/decisions/20260903-two-binaries.md` for why there are two programs.

## teanode-server

| Command | What it does |
| --- | --- |
| `teanode-server run` | run the server |
| `teanode-server config env` | write a starter environment file |
| `teanode-server config init` | migrate the database and store what the environment describes |
| `teanode-server config show\|validate` | inspect and check the stored configuration |
| `teanode-server config import\|export` | load a `teanode.yaml` into the database, or write one out |
| `teanode-server tls self-signed` | a certificate for local development |
| `teanode-server user list\|add\|password\|remove\|reset` | recover the accounts without going through the server |
| `teanode-server password` | hash a password for an exported configuration |

These read the environment the server reads (`TEANODE_DATABASE_URL` and the
rest), so they run where the server runs: in its container, or with its env
file in the shell. `teanode-server user` edits the stored configuration
directly and exists for a server that will not start or that nobody can log
into; day to day, accounts are managed with `teanode user`, through the
server.

## teanode

### Signing in

From anywhere, sign in once:

    teanode auth login --url https://mail.example.com

That opens the dashboard in a browser. Sign in there if you are not already,
press Authorize, and the token comes back to the command over a loopback
connection on your own machine — nothing passes through the clipboard or the
shell history. The token is saved as a *profile* in
`~/.config/teanode/profiles.json`, readable only by you, and becomes the one
every command talks to.

If the browser cannot reach the command — a remote desktop, a locked-down
browser — the page shows the whole command to paste instead. A token issued
some other way can be pasted directly:

    teanode auth login --url https://mail.example.com --token -

Several servers are several profiles. `auth list` shows them, `auth switch`
changes which one is active, `--profile NAME` (or `TEANODE_PROFILE`) picks
another for one command, and `auth logout` revokes the profile's token on the
server and forgets it.

    teanode auth login --url https://staging.example.com --name staging
    teanode --profile staging domain list
    teanode auth switch staging
    teanode auth status

A script that would rather not have a file sets `TEANODE_URL` and
`TEANODE_TOKEN`, which bypass profiles. Given `--url` and no token, a saved
profile for that server lends its token.

A profile can be *read-only*: every change is refused on this machine, before
anything is sent, and reads go through as they would otherwise. That is the
profile to hand to a script or an agent that should be able to look but not
touch. The token itself is unchanged — the server would accept the change,
and this profile does not ask it to.

    teanode auth login --url https://mail.example.com --read-only
    teanode auth set-read-only mail.example.com true
    teanode auth set-read-only mail.example.com false

`--read-only` on any command, or `TEANODE_READ_ONLY=1` in the environment,
does the same for one command or one shell, whatever the profile says. There
is no flag in the other direction: something handed the variable cannot talk
its way out of it. A refused change exits with code 3 and says which of the
three switches to undo. `auth logout` still revokes the profile's token,
because forgetting a profile and leaving its token live is the worse outcome.

Signing in again to a saved profile — `auth login` with no `--url`, which
means the profile `--profile` names or else the active one, or with the
`--url` or `--name` of one — replaces its token and revokes the old one on
the server, and says so. It keeps the profile's read-only and certificate
settings unless told otherwise. On a read-only profile the old token is left
alone and named, for revoking by hand.

**On the server itself**, nothing has to be set up. With the server's
environment in the shell — which a container already has — the client reads
the server secret from the stored configuration, mints a token signed with
it, and connects over the loopback interface:

    docker compose exec teanode teanode user list

    set -a; . /opt/teanode/.env; set +a
    teanode domain list

This is the *console*: it is not an account, so the operations that belong to
one — tokens, sessions, passkeys — need `--user` where they take it, or a
real sign-in. `--profile local` reaches the console from a shell that also
has profiles.

Which server a command talks to is decided in this order: `--url`, then
`--profile`, then the active profile, then the console. Explicit beats saved,
and saved beats ambient, so a script that sets the variables is never
surprised by whatever somebody last logged in to.

### Commands

One group per resource, each with `list`, `get`, `create`, `update` and
`delete` where the API has them, plus the verbs particular to the resource.
Tables by default; `--json` on every command prints the same thing as JSON,
so one command serves a person and a script.

| Group | What it covers |
| --- | --- |
| `auth` | signing in, and the saved profiles |
| `domain` | the domains this server accepts mail for, their DNS records, and the `logo` they publish |
| `alias` | where mail for a domain goes; `alias match` says what an address would hit |
| `credential` | SMTP credentials for sending through this server |
| `dkim` | the keys that sign outgoing mail, and the record to publish |
| `user` | the accounts on this server; `user rescue` on `teanode-server` makes one an administrator |
| `group` | who may do what, and over which domains: members, roles, domains |
| `role` | the named sets of permissions a group holds; `role permissions` lists what may be given |
| `audit` | the log of administrative changes, with filters |
| `mailbox` | a mailbox and everything in it: `folder`, `rule`, `contact`, `subscription`, `device`, `autoreply`, `programs` |
| `token` | API tokens; `token create --user` on the console issues somebody's first |
| `session` | the browsers signed in to the dashboard |
| `passkey` | the passkeys registered to your account; registering one needs the dashboard |
| `settings` | the optional integrations; `settings set <section> key=value` |
| `server` | the running instance: `status`, `restart`, `addresses`, `identity` |
| `upgrade` | the newest release, and installing it: `status [--check]`, `apply` |
| `mail` | handled mail: `list` with filters, `get`, `content`, `download`, `opens`, `count`, `send` |
| `delivery` | what happened on the way out, and `delivery pending`, the queue |
| `report` | DMARC aggregate reports received about your domains |
| `template` | a domain's message templates, with `render` |
| `layout` | the frames templates are rendered inside |
| `api` | everything else, straight from the schema |

### A mailbox from the shell

`teanode mailbox` is the dashboard's mailbox, without the dashboard. Most
people have one mailbox, so `--mailbox` is needed only when there are
several, and a folder is named rather than identified:

    teanode mailbox folder create GitHub
    teanode mailbox rule add GitHub --when from:contains:@github.com --move GitHub --stop
    teanode mailbox rule apply

A condition is `field:operator:value`, repeatable, and every one must match.
The fields are `from`, `to`, `subject`, `header`, `score`, `sender-known`,
`category`, `priority`, `needs-reply` and `any`; the operators are
`contains`, `equals`, `matches` (a regular expression), `above` and `below`.
A header condition names the header: `--when header:List-Id:contains:golang`.
Three of the fields ask nothing of a value and are written alone: `--when
sender-known`, `--when needs-reply` and `--when any`. `category`, `priority`
and `needs-reply` read what the agent decided about a message, so a rule
with one of them runs once the agent has sorted the message rather than at
delivery: `--when category:equals:newsletter --move Reading`. The actions are flags: `--move`,
`--mark-read`, `--flag`, `--forward`, `--delete`, and `--stop` ends the run
after this rule.

A rule files the mail that arrives after it is written. `rule apply` runs the
stored rules over what is already in a folder, moving, marking, flagging and
deleting as arrival would have; forwarding is not repeated, because old mail
is not sent again. `rule test` says what would happen and changes nothing.

The rest of the group is the rest of the mailbox. `mailbox list` names the
mailboxes you can open, and `--all` every mailbox on the server with its
owner; `show` and `update` read and change a mailbox's name and signature.
`folder list|create|rename|move|pin|unpin|delete` is the tree in the rail.
`rule list|add|remove|enable|disable|test|apply` is the filing.
`contact list|add|remove` is the addresses it has learned,
`subscription list|show|mail|unsubscribe` the mailing lists it receives,
`device list|add|remove` the app passwords a mail program signs in with,
`autoreply show|set|off` the out-of-office reply, and `programs` the hosts
and ports to type into a mail program.

A subscription is not a stored thing but a grouping of stored things: every
message that named the same list, keyed by the identifier the list publishes
for itself or the address it sends from. So there is nothing to create, and
the key is what `subscription list` prints:

    teanode mailbox subscription list
    teanode mailbox subscription mail <key>
    teanode mailbox subscription unsubscribe <key>

Leaving is a request made to somebody else, and only one of the three ways
finishes at the command line: a one-click request is sent, a message is sent
to the address the sender named, and a sender offering only a page has the
page printed for a person to open. Mail already in the mailbox stays either
way; what stops is what has not been sent yet.

### The mark a domain publishes

`teanode domain logo show|publish|remove` is the BIMI logo this server hosts
for one of its own domains:

    teanode domain logo publish example.com mark.svg
    teanode domain check example.com          # prints the record to publish
    teanode domain logo remove example.com

The file is checked before it is stored, against the restricted profile a
mark has to satisfy — no script, no animation, nothing fetched from
elsewhere, square — and a refusal names the rule that refused it, because a
receiver refuses the same file silently and the sender never learns why.
`domain check` prints the record to publish once there is a logo to point at,
and says underneath when something would stop a published record having any
effect, most often a DMARC policy of none.

Removing stops serving the file. Take the record down as well, or receivers
keep fetching an address that answers nothing.

### Two things that are not in the schema

Because they are bytes rather than JSON. The
files of a draft go up as `multipart/form-data`, one `file` part each, to
`PUT /api/v1/mailbox/drafts/{itemId}/attachments` (or
`POST /api/v1/mailbox/{mailboxId}/drafts/attachments` for a draft that does
not exist yet), with the same bearer token. `curl -F file=@report.pdf` does
it; the reply is the draft as stored, with every part's index.

A domain's logo is the other: `POST /api/v1/domains/{domainId}/logo` with a
`file` part, which is what `domain logo publish` sends. Reading and removing
one are ordinary operations in the schema; only sending it is not.

Some examples:

    teanode domain create example.com
    teanode alias create example.com --pattern '^hello$' --kind email --email me@example.org
    teanode alias create example.com --pattern '^you$' --kind mailbox --mailbox <mailbox id>
    teanode api call ListMailboxItems folderId=01... first=10
    teanode alias match example.com hello
    teanode settings set antispam enabled=true host=127.0.0.1 port=783
    teanode server status
    teanode mail list --domain example.com --status rejected --first 20
    teanode mail send example.com --from hello@example.com --to ann@example.org \
        --template welcome --variable name=Ann
    teanode template render example.com welcome --variable name=Ann
    teanode delivery pending

Things are named the way a person names them: a domain by its name, a
template by its domain and name, an alias or a credential by the identifier
its list prints. Anything that cannot be undone asks first; `--force` skips
the question. The `--status` and `--kind` filters are checked before
anything is sent, so a typo is an error rather than an empty list; `mail
count --by` is checked by the server, which knows every field. A list that
stopped at `--first` says so on standard error, so a page is never mistaken
for the whole.

### From a script, or an agent

The same commands serve a script, with three differences that matter when
nobody is watching.

A question is only asked of somebody who can answer it. When standard input
is not a terminal, a command that would confirm refuses at once with a
`--force` hint instead of printing a prompt nobody sees. `TEANODE_FORCE=1`
answers every such question for a shell that has already decided.

`--json` applies to failure as well as success: the error goes to standard
error as `{"error": "...", "exitCode": N}`, so a caller parses both the same
way. `teanode api` always prints JSON, and so are its errors.

The exit code says what kind of thing went wrong:

| Code | Meaning |
| --- | --- |
| `0` | it worked |
| `1` | something else went wrong; the message says what |
| `2` | the command was called wrongly: an argument missing, a flag that does not exist, a confirmation with nobody to ask, a value that is not one of the choices |
| `3` | a change refused by a read-only profile, `--read-only`, or `TEANODE_READ_ONLY`; nothing was sent |
| `4` | the server has no such thing |
| `5` | the server refused the token; sign in again |
| `6` | the server could not be reached at all |

Shell completion comes from the binary itself:

    source <(teanode completion bash)
    source <(teanode completion zsh)

`settings set` is generic: the keys and their types come from the server's
own schema, and `settings describe <section>` lists them. A value of `-` is
read from the terminal without echoing, for a secret.

### Reaching the whole API

The groups above cover what the server offers today. `teanode api` covers
every operation in the schema, including any added since, because it works
off the schema the server reports rather than a hand written list:

    teanode api list                    # every operation
    teanode api list domain             # the ones about domains
    teanode api describe CreateDomain   # arguments, input shapes, return fields

    teanode api call ListDomains
    teanode api call GetDomain domainId=example.com
    teanode api call CreateDomain domainParameters:='{"domain":"example.com","subdomain":"mail"}'

Arguments are `name=value`. Use `name:=<json>` for a number, boolean, list or
object. Values whose type the schema declares as a number or boolean are
converted for you, so `first=10` arrives as `10`.

The reply carries every field that can be asked for without arguments, three
levels deep. `--depth` changes that, and `--select` replaces the generated
selection entirely:

    teanode api call ListDomains --select "{ id domain }"

For anything the generated query cannot express, write the query:

    teanode api graphql '{ ListDomains { id domain records { records { type name verified } } } }'
    teanode api graphql --file query.graphql --variables '{"domainId":"example.com"}'

`teanode api` always prints JSON.

### Working without a running server

On the console, reads fall back to the stored configuration when the server
is not running, because a read cannot lose anybody's change and the stored
configuration is current either way. This is what makes the first run work:
`teanode dkim show example.com` prints the DNS record to publish before the
server has ever started.

Writes do not fall back. When the server is down the command fails and says
so, rather than making a change the server would overwrite the next time
anything was saved from the dashboard. The exceptions live in the server's
own program: `teanode-server user` for accounts, and `teanode-server config
import` for a whole configuration.

### teanode agent

Your own agent, and — for an operator — everybody's. Every command goes
through the API, so a change made here is the change the Agent page would
have made.

| Command | What it does |
| --- | --- |
| `teanode agent ask <message \| ->` | say something to your agent and print what it answers; `--new` starts a named conversation, `--conversation` continues one, `--attach FILE` (repeatable) hands it a file — a picture is shown to it, a text file read to it, anything else named — `--json` streams every event, `--quiet` prints the answer alone. A tool that needs your word asks on the terminal, y or n — never a flag |
| `teanode agent chat` | the same, turn by turn, until an empty line |
| `teanode agent conversation list\|show\|new\|rename\|delete` | the main conversation and the named ones; `list --query` finds one by words in its title or in what was said; `delete` asks first and takes the files that came with it |
| `teanode agent run list\|show` | what the agent did on its own: the transcripts of its sorting, summaries and replies |
| `teanode agent tools` | the tools your agent has, as you may use them, with the risk class and whether it asks first |
| `teanode agent memory list\|add\|remove` | what your agent remembers about you; `add "The accountant" "Maria does the books" --applies-to triage,reply` addresses a memory to the runs that read it |
| `teanode agent schedule list\|add\|remove\|run` | what it does on its own at set times: `add Morning "0 8 * * 1-5" "what needs me today?" --deliver mail`, a cron line in your zone |
| `teanode agent feedback` | the corrections recorded from what you did, which the agent is shown as examples |
| `teanode agent channel list\|set\|unlink\|remove` | the chat apps you talk to your agent from: your own Telegram or Discord bot. `set telegram --token -` reads the bot's token from standard input; `list` shows the code a chat sends the bot as `/link CODE` to become the linked one, and whether the bot runs; `unlink` draws a new code |
| `teanode agent mcp list\|connect\|disconnect` | the connected servers the operator declared and your connections to them; `connect tracker --credential -` reads your credential from standard input, and an authorizing server prints the address to open |
| `teanode agent settings show\|set` | your agent: `set enabled=true name=Bertie instructions=-` reads the long value from standard input; keys are listed by `set --help` |
| `teanode agent settings categories add\|remove` | your own categories beside the fixed ones |
| `teanode agent settings forget` | delete the agent and everything it learned; asks first |
| `teanode agent source list\|grant\|revoke\|set` | the mailboxes the agent may reach and what it does in each: `set --mailbox work triage=true auto-reply=true auto-reply.scope=known` |
| `teanode agent usage [--since] [--by day\|kind\|mailbox\|model]` | your tokens |
| `teanode agent draft <item-id> [--say "…"]` | have the agent write a reply to a message, printed for you to use; nothing is saved or sent |
| `teanode agent replies [--status held\|sent\|cancelled\|refused\|failed] [--mailbox]` | the replies the agent wrote for you and what became of each, with the reason when it left a message alone |
| `teanode agent replies cancel <reply-id>` | cancel a held reply; the draft goes and nothing is sent |
| `teanode agent admin usage\|list\|limit\|disable\|enable\|dead-letters\|retry` | everybody's agents, needing `agent:audit`: tokens by day, kind, mailbox, model or agent; each person's sources and today's spend; a per-person limit in tokens or, with `--cost`, in money; the switch-off; the jobs the worker gave up on |

Every command sends the shell's time zone and language with the request,
the way the dashboard sends the browser's, so a person who lives in the
terminal is placed as well as one who lives in the browser.

### teanode computer

Your own computer, attached to your agent. While the program runs, the
agent has two more tools — `shell`, which runs a command here, and
`filesystem`, which reads, edits, writes, copies, lists, searches and greps your files —
as you, anywhere on the machine, the way a terminal of yours would. Only a
conversation you are present in may use them: a scheduled run, a sorting
run, anything with nobody watching, never sees your computer. A command
that changes the machine or reaches out of it (removing, moving,
installing, sudo, pushing, ssh, and the graver shapes) asks you first, on
the card in the drawer or on the terminal, and so do a file moved or
deleted and a write into what the machine runs on its own (a shell's
startup file, keys, autostart); nothing is refused on your behalf — your
yes is the last word. The card is the server's: the program runs what
the server sends, so it trusts the server the way a terminal trusts the
person at it. The program signs in as you, with the active profile's
token, never as the server. Several computers can be attached at once,
told apart by name.

| Command | What it does |
| --- | --- |
| `teanode computer start [--name NAME]` | run the program in the background; `--name` is what to call this computer (the host name by default). Its log is `~/.config/teanode/computer.log` |
| `teanode computer status` | whether the program runs here, and which computers of yours the server sees |
| `teanode computer stop` | end the program |
| `teanode computer daemon [--name NAME]` | the same program in the foreground, reconnecting when the connection drops, until interrupted — for a terminal, or a service manager |

The operator can keep computers off for the whole server with
`agent.features.computer`.
