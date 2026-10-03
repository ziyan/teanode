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
| `teanode-server config rules import\|show` | the built-in spam filter's pattern rules: import a set, or show what is stored |
| `teanode-server config import\|export` | load a `teanode.yaml` into the database, or write one out |
| `teanode-server tls self-signed` | a certificate for local development |
| `teanode-server user list\|add\|password\|remove\|reset\|rescue` | recover the accounts without going through the server |
| `teanode-server password` | hash a password for an exported configuration |
| `teanode-server evaluate scenario <file>` | feed a memory scenario through filing and dreams in a database of its own, and grade its questions at each checkpoint; see `docs/evaluation/scenarios/` |

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
| `mailbox` | a mailbox and everything in it: `folder`, `rule`, `subscription`, `device`, `autoreply`, `programs` |
| `token` | API tokens: `create`, `list`, `update` (rename, or a new lifetime from now), `revoke`; `token create --user` on the console issues somebody's first |
| `app` | apps authorized to act as you: `list`, `rename` (the name stays when the app renews), `disconnect` (revokes every token it holds) |
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
| `finance` | linked institutions, transactions, trades, net worth, budgets and savings targets; see below |
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
sender-known`, `--when needs-reply` and `--when any`. `sender-known` is true
when the sender is in your address book — somebody you keep, not merely
somebody who has written before. `category`, `priority`
and `needs-reply` read what the agent decided about a message, so a rule
with one of them runs once the agent has sorted the message rather than at
delivery: `--when category:equals:newsletter --move Reading`. The categories
are `personal`, `work`, `newsletter`, `notification`, `receipt`, `promotion`,
`social`, `invitation`, `phishing`, `junk` and `other`, plus any the person
added. The last two are what a standing rule about unwanted mail is written
against, and `matches` takes both at once:
`--when category:matches:^(phishing|junk)$ --move Junk --mark-read --stop`. The actions are flags: `--move`,
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
`subscription list|show|mail|unsubscribe` the mailing lists it receives,
`device list|add|remove` the app passwords a mail program signs in with,
`autoreply show|set|off` the out-of-office reply — `set --same-domain-only`
keeps it to people at the mailbox's own domains — and `programs` the hosts
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

`mail send` prints its submission identifier on standard error before sending.
If the response is lost, repeat the command with `--submission-id` set to that
identifier and the same content, attachments, template and variables. The server
returns the original acceptance instead of accepting another message. Reusing an
identifier with changed parameters is refused; use a new identifier for a new
message. Keep input files unchanged for the retry, including content originally
read from standard input. The identifier is scoped to the account or the local
console, so retry with the same identity. This protects local acceptance, not
exactly-once remote SMTP delivery. The identified command requires a server that
supports `SendMail`'s `submissionId` argument.

Use `mail submission <domain> <submission-id>` to check acceptance without the
original files or template. It returns the accepted mail identifier even after
mail retention removes the stored copy. Run it as the same account or console
that sent the message. With `--json`, it returns the acceptance object or `null`.
A missing result means no acceptance is recorded yet; an in-flight request may
still commit, so retry with the same identifier and unchanged content.

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
| `teanode agent conversation todo list` | the task list a conversation keeps: the agent's own, which it writes as it works through something in several steps and shows itself every round. `todo list <conversation-id>` prints it; only the agent changes it |
| `teanode agent conversation list\|show\|new\|rename\|goal\|main\|archive\|unarchive\|delete` | the main conversation and the named ones; `list --query` finds one by words in its title or in what was said, and `list --archived` lists the ones put away instead of the open ones; `show` prints the goal above the messages when there is one and the task list under them, the done ones ticked, and pages back through a long one with `--first` and `--offset`; `main` makes a named conversation the main one, or starts a fresh main one and keeps the old as a named one; `archive <id>` puts one away with everything in it and `unarchive <id>` brings it back, neither asking first because neither loses anything; `delete` asks first and takes the files that came with it |
| `teanode agent conversation goal [<conversation-id>] [<goal>]` | what the agent keeps working toward in a conversation, across turns of its own, until it says it is met or you clear it: `goal <id> "reply to every mail from the landlord this week; ask me before sending anything"` sets one, `goal <id> --clear` drops it and stops the turn under way, and `goal` alone lists the goals still in progress with their state — working, or waiting for you — and the agent's last word on each, the same list as the Goals tab of the agent's page, and `goal <id> --met` says one is done, which also finishes the idea it was carrying out, if any. `conversation new --goal` starts a conversation with one |
| `teanode agent idea list\|propose\|start\|done\|dismiss\|reopen` | what your agent offers to do for you, and what became of each offer: the same list as the Ideas tab and the agent's `idea` tool. `list` shows the open ones, `--status dismissed` or `--all` the others; `start <id>` opens a conversation named after the idea and prints what to say in it, and `--send` says it; `done`, `dismiss` and `reopen` say what became of one. `propose` keeps one you write, checked as the agent's are: it may only need tools your agent has, must say where it asks first when a tool acts toward somebody else, and names what prompted it with `--evidence message:<item-id>:<what it is>` |
| `teanode agent run list\|show\|stop` | what the agent did on its own: every model call is a run, and `list` pages through them with `--first` and `--offset`; `--kind triage --kind reply`, or `--kind triage,reply`, narrows it to some kinds of run and `--query` to what they were about; `--all` or `--agent <id>` lists everybody's, for an operator with `agent:act`; `stop <run-id>` stops one where it is, keeping what it has already done |
| `teanode agent tools` | the tools your agent has, as you may use them, with the risk class and whether it asks first |
| `teanode agent memory index\|get\|search\|note\|page\|merge\|link\|unlink\|move\|forget\|history\|recall\|learned` | what your agent knows about you, as pages with numbered facts: `get people/alice-chen`, `note people/alice-chen "Does the books" --applies-to triage,reply`, `link people/alice-chen projects/portal --relation works_on`, `move notes/marigold things`, `move --number 3 people/alice-chen projects/portal` puts one fact on another page instead of the whole page, keeping the words it came from, `forget people/alice-chen --number 2`; `forget people/alice-chen` with no number takes the page and everything under it and asks first, `--force` skips the question; `history projects/portal`. `search "<words>"` finds pages and facts by words, `--first` at a time (40 by default); a search that found more says how many more pages and facts, "at least" where it stopped counting, and the `--offset` that prints the next page |
| `teanode agent memory recall "<question>"` | what a question would carry into a turn: the pages recall would expand, each with the facts on it as the page numbers them, printed the way `memory get` prints a page. It is the same recall a turn does, so it answers "why did it not know that?" without asking it again — and it costs nothing: no model is asked anything and nothing is marked as used, so the same graph answers the same twice. `--json` for the pages and facts as they came. `--search "<words>"` (at most two) and `--broad` follow a retrieval plan, as a live turn follows its depth judgement's. `--explain` says why as well: the retrieval mode and plan, each query that ran (the message, each planned search, the linked-pages hop, the broad pass), how many each search of each query found, where each page and fact ranked in each search and once fused, and what carried it or kept it out (the page limit, the limit on pages no fact was found on, the token budget, a retired fact), with the overview section carried and why that one |
| `teanode agent memory overview <path>` | how the thing a page is about works, as the dream last wrote it from the page and the pages under and beside it: what it is, its parts, how it relates to what it is linked to, what has been happening, what stands out. `--rewrite` asks the next dream to write it again; `memory get` prints it too, under the opening, with a theme's reflections. It ends with what the overview covers, counted by the server: how many of the pages under it, of a theme's pages and of its links its prompt shows of how many there are, how many of those had no overview of their own, and whether what it is written from has changed since |
| `teanode agent survey "<question>"` | answer a question about a whole from the overviews: one read-only run a page in scope, several at once, then one that combines them into a report with citations. `--scope themes/<one>` or any page narrows it; with none it covers every top-level theme. Runs of kind survey, a few minutes. It starts the survey on the server and asks every few seconds whether it has finished, so no request is held open that long. A read that fails for a passing reason (the server restarting behind its proxy, a dropped network, a timeout) is tried again, less often each time; after thirty minutes the command stops waiting. Whichever way it ends, it names the survey's id, for `teanode agent background show <id>`; `--no-wait` prints the id and returns, and stopping the command leaves it running. `--json` for the finished work, with the report and the runs |
| `teanode agent background list\|show\|stop` | the surveys and subagents your agent started without waiting for them, and the surveys `agent survey` started: `list` says what is queued, running or finished lately, newest first; `show <id>` prints one's report or answer, and on standard error how it stands and the runs it made; `stop <id>` stops one that is queued or running, which then wakes nothing |
| `teanode agent memory answers <file>` | answer each question of a set that has an `expectedAnswer`, from memory, from the sources or from both (`--from memory,sources,both`; `memory@planned` and `both@planned` follow each question's `plan`), and grade each answer against the expected one: a line a question and source — `memory changed-01 stale gave the old address` — then a score, the verdicts, the cost and the time an answer took, per source. Two model calls a question and source, as runs of kind `evaluate`; nothing is written to a conversation or the graph. `--json` for every answer. See `docs/evaluation/` |
| `teanode agent memory plan <file>` | ask the depth judgement what retrieval plan a live turn would follow for each question, and with `--output <file>` write the question set back with each question's `plan`, for `evaluate --mode planned` and `answers --from memory@planned`. One call to the fast model a question, as runs of kind `evaluate`. `--stored` plans the memory check's confirmed and corrected questions instead of a file's |
| `teanode agent memory evaluate <file>` | replay a set of questions through recall and say, per question, whether the facts it needs would have been carried: one row a question — `direct 03 hit`, `changed 12 miss: carried people/alice-chen saying "berlin"` — then totals per kind and overall, and a non-zero exit when anything missed. No model is asked anything and nothing in the graph is marked as used, so the same graph answers the same twice and the set can be run before and after a dream to see what the dream was worth. `--mode planned` follows the plan each question carries (see `memory plan`), as a live turn would. The file's shape, and a starter set to replace with questions from your own graph, are in `docs/evaluation/` |
| `teanode agent knowledge list\|add\|set\|pause\|resume\|sync\|remove` | the places your agent reads: `add "work" ~/work --computer laptop --under work`, `set work --cron "0 4 * * *" --under projects` changes a source that already exists — its name, its path, where what it finds is filed, how often it is read, the format, the mailbox, whether every checkout under it is read — leaving everything you do not give alone, so correcting the hour does not cost you what removing and adding it again would, `--read-every-checkout` reads the files of every checkout under the path — by default a checkout that is barely any of your work is kept to its profile, so the page says what it is and where it lives and its source is not indexed, and the source's row says how many checkouts that was and how many files, `--own-commits-at-least` says how many commits of your own a checkout needs before its files are read, however long its history (leave it alone and the program works it out: two commits, or a fiftieth of the log, whichever is more, up to twenty-five — and never more than the whole history, so a repository you committed to once and nobody else ever is yours; `1` is the older rule, any commit at all), `--commits-per-pass` says how much of the history one pass over the tree carries, shared among the checkouts in it, newest first (the program's own pace is two thousand), `sync` reads one again now, `pause` keeps what it found, and `remove` forgets everything it found and so asks first, `--force` skipping the question. `--format` says how to read what is there: `files` (a tree of files, git-aware), `journal` (dated notes), or `records` (a folder of JSON lines any script can write, with a `refresh` script beside them the daemon runs before each scan — ask your agent to write one) |
| `teanode agent knowledge search "<words>"` | find passages in what has been indexed — your code, your chat, your notes — without asking your agent to look for you. It is the same search your agent's knowledge tool runs, so what you see is what it sees: an identifier out of a log (`ComputeShippingQuote`) is looked up exactly and printed as the file and line that defines it, then the passages, each under the document it came from, with the identifier that reads that document back. `--first` how many, `--offset` how many of the ranking to pass over, `--source <id-or-name>` narrows it to one source, `--json` prints the rows as they came, each with its score. Where the deployment has no embedding model it says so — found by words alone, which misses a paraphrase that shares no word. A search that found more than it printed ends with how many more passages ("at least" where it stopped counting) and the `--offset` that prints the next page; pages read one after another show every passage once, in the order of the first |
| `teanode agent knowledge read <document-id>` | read one of those documents, by the identifier the search printed; the `<id>#<passage>` out of a citation is taken as well. `--from` is where in the text to start, counted in characters, and `--first` how many to print; a read that does not reach the end says how much is left and prints the command that carries on from where it stopped |
| `teanode agent dream log\|runs\|now\|bootstrap\|reread` | the dreams: the runs that read, file and rehearse; `runs <id>` lists every model call one dream made, each a run `agent run show` opens; `now` starts one within the minute, whatever the agent's hours; `bootstrap on` keeps it running with wider limits until nothing waits to be read, for a first ingest; `reread --minutes 60` puts back what a night marked read in that window |
| `teanode agent schedule list\|add\|set\|enable\|disable\|remove\|run` | what it does on its own at set times: `add Morning "0 8 * * 1-5" "what needs me today?" --deliver mail`, a cron line in your zone; or a single moment, `"@at 2026-09-12 09:00"`, or a distance from now, `"@in 20m"`, which is stored as the moment it means and runs once. `set <id> --cron "0 7 * * 1-5"` changes one in place — `--name`, `--cron`, `--prompt` (`-` reads it from standard input) and `--deliver`, and only what you give changes — and `disable <id>` stops it running without taking it away, `enable <id>` starts it again |
| `teanode agent brief on\|off\|now` | a brief each morning, by mail: what the day holds, what is waiting for an answer, what is being held. `on --at 07:30 --days 1-5` says when; `now` sends one immediately. It writes an ordinary schedule called "Daily brief", so `agent schedule list` shows it and anybody may rewrite what it asks for |
| `teanode agent feedback` | the corrections recorded from what you did, which the agent is shown as examples |
| `teanode agent channel list\|set\|unlink\|remove` | the chat apps you talk to your agent from: your own Telegram or Discord bot. `set telegram --token -` reads the bot's token from standard input; `list` shows the code a chat sends the bot as `/link CODE` to become the linked one, and whether the bot runs; `unlink` draws a new code |
| `teanode contact list\|show\|add\|edit\|remove` | your address book: the people you keep, which your phone and your computer synchronize over CardDAV. `add --name "Ada Lovelace" --email ada@example.com` keeps one; `edit <id> --name "Ada King"` changes only what you give and leaves the rest of the card alone, so correcting a name does not throw away the photograph a phone put there; `--card -` reads a whole vCard from standard input |
| `teanode calendar list\|show\|add\|edit\|remove` | your calendar, which your phone and your computer synchronize over CalDAV. `list --from 2026-09-14 --until 2026-09-21` prints one line for every time something happens, a repeating event once per occurrence; `add --title Standup --starts 2026-09-14T09:30 --repeat FREQ=WEEKLY;BYDAY=MO` puts something in it; `--invite ada@example.com` sends the invitation by mail, and moving or removing the event tells everybody invited; `--all-day` belongs to the day rather than to a time, and its `--ends` is the last day it is on, so the same date at both ends is one day; `--file -` reads a whole iCalendar file from standard input. Times are read and written in the calendar's own zone unless they carry an offset |
| `teanode calendar request <request-id>` | check whether a calendar save completed, even after its event or calendar was deleted. `add`, `edit` and `remove` print a request ID before changing an event; after an uncertain response, look it up here or retry the same operation and fields with `--request-id`. A retried removal checks completion before loading the deleted event. An absent receipt may mean the original request is still in progress. Changed fields require a new ID |
| `teanode calendar stop <request-id>` | resolve an uncertain request before discarding its fields. An uncommitted request is durably stopped; an already completed change is reported and is not undone. A failed stop response is still uncertain, so retry the same request ID |
| `teanode calendar free` | when you are free: the stretches of the working day nothing is booked in, day by day. `--earliest 08:00 --latest 18:00` moves the ends of the day; a whole-day entry does not make a day busy, and neither does anything cancelled. Worked out with the same two functions that answer a phone's free-busy request, so what this prints and what a colleague's client is told cannot disagree |
| `teanode calendar calendars\|set` | the calendars themselves: what they are called, what a client paints them, the zone a new event is written in, and the day a week is drawn from — `set --timezone Europe/Berlin`, `set --week-start monday`. Weeks start on Sunday unless a calendar says otherwise; the five-day view is Monday to Friday either way, because that is what a working week is |
| `teanode reminder list\|add\|edit\|done\|reopen\|remove` | your reminders list, the one beside your calendar that the Reminders app on a phone synchronizes over CalDAV. `add "Buy stamps" --due 2026-09-29` is due on that day, `--due 2026-09-29T15:00` at that time in the calendar's zone; `list --done` shows the ones ticked off and `list --all` both; `done` ticks one off and `reopen` puts it back |
| `teanode note list\|show\|add\|edit\|remove` | your notes: the ones a phone's Notes app keeps in your mail account, which it syncs over IMAP. `add "Packing list"` writes one whose first line is its title, `--file -` reads the text from standard input, and `edit <id>` replaces a note's text; the phone shows the change at its next sync. `--mailbox` picks the mailbox when you have more than one |
| `teanode agent skill list\|search\|install\|update\|remove\|enable\|disable\|scope\|secret` | tools installed from the skill registry, for everyone on this server: `search` says what there is, `install weather` checks the signature and the hash before keeping it, `update` with no name installs every newer version there is. `scope <name> operator|person|skill` settles who fills the skill's secrets in here — one set of values for the whole server, each person's own, or whatever the skill declares. Installing and scoping need `server:manage`. A skill that runs commands runs them on a computer you attached, like the `shell` tool, asks first only for a call the fast model judges would speak for you or cannot be undone, and is never used by a run with nobody watching. `secret list\|set\|clear` is for the values a skill asks *you* for rather than the server: `secret set news NEWSAPI_KEY` reads the value from the terminal without echoing, or from standard input when there is no terminal |
| `teanode agent mcp list\|connect\|disconnect` | the connected servers the operator declared and your connections to them; `connect tracker --credential -` reads your credential from standard input, and an authorizing server prints the address to open; `--loopback` brings the authorization back to this terminal instead, for a service that answers only to a loopback address |
| `teanode agent settings show\|set` | your agent: `set enabled=true name=Bertie instructions=-` reads the long value from standard input; keys are listed by `set --help`. `set alerts=false` stops it telling you about your mail unasked; `alert-quiet-start=22:00 alert-quiet-end=07:00` is the night in which only what cannot wait is said, and `alert-daily-most=5` the most a day |
| `teanode agent alert list\|mute\|mutes\|unmute` | what your agent told you unasked, and what you asked it not to: the same list as the Alerts card on the agent page. `list` shows each alert with the messages it was about; `mute <alert-id>` stops alerts about what it covered (the burst, or the sender of a single message), `--scope subjectKey`, `sender`, `domain` or `kind` on its subject, its senders, their domains or its kind (a burst, or a category such as `notification`), and `mute --target offers@shop.example.com` names one yourself, its scope read from it unless `--scope` says; `mutes` lists them and `unmute <mute-id>` takes one back |
| `teanode agent settings categories add\|remove` | your own categories beside the fixed ones |
| `teanode agent settings forget` | delete the agent and everything it learned; asks first |
| `teanode agent source list\|grant\|revoke\|set\|allow\|deny` | what the agent may reach and what it does in each: `set --mailbox work triage=true auto-reply=true auto-reply.scope=known` for a mailbox, `alerts=false` to hear nothing unasked about what arrives in it; `allow calendar` and `deny addressbook` for the other two kinds of source, which carry a switch and no policy. Nothing from a source you have not granted is ever sent to a model |
| `teanode agent usage [--since] [--by day\|kind\|mailbox\|model]` | your tokens |
| `teanode agent draft <item-id> [--say "…"]` | have the agent write a reply to a message, printed for you to use; nothing is saved or sent |
| `teanode agent replies [--status held\|sent\|cancelled\|refused\|failed] [--mailbox]` | the replies the agent wrote for you and what became of each, with the reason when it left a message alone |
| `teanode agent replies cancel <reply-id>` | cancel a held reply; the draft goes and nothing is sent |
| `teanode agent admin usage\|list\|limit\|disable\|enable\|dead-letters\|retry` | everybody's agents, needing `agent:audit`: tokens by day, kind, mailbox, model or agent; each person's sources and today's spend; a per-person limit in tokens or, with `--cost`, in money; the switch-off; the jobs the worker gave up on |

Every command sends the shell's time zone and language with the request,
the way the dashboard sends the browser's, so a person who lives in the
terminal is placed as well as one who lives in the browser.

### teanode finance

The institutions you linked, their accounts and transactions, net worth,
spending categories, budgets and savings targets: the same operations as the
dashboard's Finance page (and its setup on the agent page's Finance tab) and
the agent's `finance` tool, and named after
them (the tool's `spending_summary` is `teanode finance spending-summary`).
Amounts are printed with their currency; `--json` keeps full precision.
`docs/subsystems/finance.md` says how it works.

| Command | What it does |
| --- | --- |
| `teanode finance providers` | which providers this server offers, and how to link through each |
| `teanode finance link-plaid` | prints the address of the page that opens Plaid's window, for a browser, and waits until the new finance source appears; `--no-wait` returns at once |
| `teanode finance link-simplefin [<setup-token> \| -]` | claims a setup token from the SimpleFIN Bridge; `-` or no argument reads it without echoing. A token can be claimed once |
| `teanode finance import-credential --provider plaid\|simplefin [--institution-name NAME] [- \| <file>]` | brings a connection made elsewhere in as a finance source instead of linking again: a Plaid access token of a link made with this server's Plaid keys (which saves a Plaid slot), or a SimpleFIN access URL already claimed. The credential is read from the file, from standard input, or at a prompt without echoing, never from the command line; the provider is asked before anything is made, and the first sync starts within the minute |
| `teanode finance import-statement <file \| ->` | imports an OFX statement (`.ofx`, `.qfx` or `.qbo`), such as a card's exported transactions, into the finance source of imported statements; a transaction already imported (same account, same FITID) is updated rather than added again, so overlapping files add nothing twice. Prints the account and how many were added, updated and already there |
| `teanode finance import-transactions [--dry-run] <file.json \| ->` | imports one account's transactions from a JSON object of rows instead of a file, such as rows read off screenshots: `financeAccountId` (an imported account the rows are of, from `teanode finance accounts`; the number may then be left out), `isNewAccount`, `institutionName`, `accountName`, `accountNumber` (the digits shown, masked ones as `****1234`), `statementAccountKind` (`bank`, `card` or `other`), `currencyCode`, `bankCode`, `ledgerBalanceAmount`, `ledgerBalanceOn`, `ledgerBalanceTimeZone`, `monthlyTotals` (`[{totalMonth, totalAmount}]`) and `transactionRows` (`[{postedOn, description, amount, transactionKind, runningBalanceAmount, totalMonth}]`), amounts signed with money out negative. Running balances must chain row by row and each month's rows must come to its total, or nothing is sent and the first row or month that does not add up is named. Without `financeAccountId` the rows go into the account their number keys, else the one account at the institution of that kind and currency whose last digits the number ends with; several such accounts, or an account at the institution whose digits do not match, are refused and named, unless `isNewAccount` is true. Only the rows the account does not hold already, matched by day and exact amount, are added. Prints what was imported and what was checked. `--dry-run` writes nothing and prints the account (existing with its id, and how it was found, or new), how many rows are new and how many already there, each new row, and what was checked |
| `teanode finance rename-statement-account <finance-account-id> <name>` | gives an account of imported statements your own name, which later imports keep; its asset takes the name too unless you renamed it |
| `teanode finance delete-statement-account <finance-account-id>` | deletes an account of imported statements with its transactions and the assets that value it (asks first); a provider's account is refused, since its next sync would bring it back |
| `teanode finance statement-import` | your statement import address (mailing an OFX file to it imports it) and what the last import did |
| `teanode finance regenerate-statement-import-address` | gives the import address a new token; the old address stops taking mail at once (asks first) |
| `teanode finance repair <source-id>` | the Plaid page again, to sign in to the same finance source when the institution asks; waits until it syncs again |
| `teanode finance sources\|sync\|disable-source\|enable-source\|delete-source` | your finance sources, one synced now, switched off or on, or deleted with everything it brought in (asks first); each refuses a source that is not a finance source |
| `teanode finance accounts\|transactions\|spending-summary` | finance accounts with their balances; transactions with `--from`, `--to`, `--since 30d`, `--month 2026-09`, `--text`, `--finance-account`, `--is-uncategorized` (those that need a spending category, shown as `(needs a category)`), `--duplicate-of <id>` (the mirrored copies of one transaction), `--is-duplicate-included` (the mirrored copies are left out otherwise, and a note says how many), and `--offset` or `--after` for the next page, a note saying which rows of how many are shown; a mirrored copy is marked `(duplicate of <id>, not counted)`; spending and income grouped by `--group-by spendingCategory\|merchant\|month\|financeAccount\|providerCategory` |
| `teanode finance credit-usage` | what your credit cards owe against their credit limits: the total and the share used in the reporting currency, then each card, highest usage first, with what it owes, its limit, where the limit comes from (`provider`, or `derived` as what is owed plus the credit available, or `unknown`) and its share; the cards left out for want of a limit are counted with what they owe; `--currency` to convert into another currency |
| `teanode finance trades` | buys, sells and securities moved in or out of investment accounts, newest first, with the security, quantity, unit price, amount and fee; `--from`, `--to`, `--since`, `--month`, `--finance-account`, `--finance-security`, and `--offset` or `--after` for the next page, a note saying which rows of how many are shown. Trades are never spending; dividends, interest and fees are transactions |
| `teanode finance exchange-rate\|convert-currency\|reporting-currency\|set-reporting-currency` | the ECB rate between two currencies on a day (the latest earlier one on a weekend, and it says which), an amount converted, and the currency your totals are shown in; `set-reporting-currency --clear` goes back to the default |
| `teanode finance net-worth\|assets\|asset-history\|create-asset\|update-asset\|close-asset\|delete-asset\|record-valuation\|delete-valuation` | net worth per day; everything you own or owe with its latest value, and for a holding its security, quantity, unit price and cost basis (in `asset-history` per day too); `create-asset "Car" --kind vehicle --currency USD --value 18000` adds one with a first value; `record-valuation <asset-id> 16500 --on 2026-09-30` records what it is worth; `--is-estimate-allowed` lets the agent estimate it from the web, which only you can allow |
| `teanode finance spending-categories\|create-spending-category\|update-spending-category\|delete-spending-category` | your own list of what money goes on, and the built-in `transfer` and `other` categories (the `transfer` and `other` columns say which), which can be renamed or hidden but not deleted, made income or given a parent or children |
| `teanode finance spending-rules\|create-spending-rule\|update-spending-rule\|delete-spending-rule` | rules that file transactions whose merchant or description contains some words; a new rule applies to earlier transactions too, except where you chose. `create-spending-rule "online payment" --spending-category transfer` marks what it matches as transfers |
| `teanode finance categorize-transaction <transaction-id>... <spending-category>` | change the spending category of one transaction, or of several at once (up to 500, all or none); `--create-spending-rule` files the merchant that way from now on, and for several saves exactly what `propose-spending-rules` lists, each placed ahead of the rule that would otherwise win, and prints what it saved and what was left out. `categorize-transaction <id> transfer` marks money moving between your own accounts as a transfer, neither spending nor income, and any other spending category takes that away. `categorize-transaction <id> other` files one that fits none of your spending categories under the built-in other category, whatever it is called; there is no `none`, since a transaction with no spending category is one still to be decided |
| `teanode finance propose-spending-rules <transaction-id>... <spending-category>` | the spending rules `categorize-transaction --create-spending-rule` would save for those transactions (up to 5000, at most 50 rules), saving nothing: one per merchant (or description) unless the rule tried first already files it there, with how many of them each matches, how many other transactions it would also change, and the existing rule it goes ahead of (`zoomly eats → Dining`); then how many were left out as too short or generic, or for holding a number that changes each time (a long number or a date) |
| `teanode finance count-transaction\|undo-count-transaction <transaction-id>` | a mirrored copy is the same charge reported again on another investment account of the same Plaid connection (a brokerage's account-level fee reported on every account, say), listed but left out of every total; `count-transaction` says this one is a real charge of its own and counts it, and mirror detection leaves it alone from then on; `undo-count-transaction` takes that back, and detection decides again at once. Counting a transaction that is not a duplicate is refused |
| `teanode finance budgets\|set-budget\|budget-status\|spending-by-day\|cash-flow` | monthly budgets per spending category, and on an income category the income you expect each month (`budgets` says which kind each is); `budget-status` is this month against each budget, with where the month is heading and its pace (under, on track, at risk, over), then each income category against what was expected by today (behind, on track, ahead), and with `--year 2026` a calendar year instead, each month at the budget it had then, with the budget to date, the months budgeted and where the year is heading; spending day by day against last month; income and spending by month |
| `teanode finance saving-summary` | the month's saving in the reporting currency: income budgets less spending budgets, against income less spending so far and where the month is heading, with the difference and its pace (behind, on track, ahead); `--month` for another month, `--year` for a calendar year (the year to date for this one), counting only the months with a budget and saying which, or with no budget that year its income, spending and what was left, `--currency` to convert into another currency |
| `teanode finance savings-targets\|create-savings-target\|update-savings-target\|close-savings-target` | amounts to save by a day, with what they need a month from now on, measured by `--measure cash_flow` (money not spent, the default), `net_worth` (net worth gained since `--started-on`, recorded for you unless `--starting-amount` says otherwise) or `asset_value` (what `--finance-account` and `--asset` are worth; a whole finance account counts its cash and every holding, those bought later too) |

### teanode computer

Your own computer, attached to your agent. While the program runs, the
agent has two more tools — `shell`, which runs a command here, and
`filesystem`, which reads, edits, writes, copies, lists, searches and greps your files —
as you, anywhere on the machine, the way a terminal of yours would. Only a
conversation you are present in may use them: a scheduled run, a sorting
run, anything with nobody watching, never sees your computer. A command runs as you
typed it: nothing here reads it first and decides whether it looks
dangerous. A file moved or deleted asks you first, on the card in the
drawer or on the terminal, because that is the action you asked for rather
than a guess about it. Nothing is refused on your behalf — your yes is the
last word. The card is the server's: the program runs what
the server sends, so it trusts the server the way a terminal trusts the
person at it. The program signs in as you, with the active profile's
token, never as the server. Several computers can be attached at once,
told apart by name. A command runs under `/bin/sh -c` (`cmd /C` on
Windows), not your login shell, so your aliases are not in scope. The
program answers every request as it comes, however many are running.

| Command | What it does |
| --- | --- |
| `teanode computer start [--name NAME]` | run the program in the background; `--name` is what to call this computer (the host name by default). Its log is `~/.config/teanode/computer.log` |
| `teanode computer status` | whether the program runs here, and which computers of yours the server sees |
| `teanode computer stop` | end the program |
| `teanode computer daemon [--name NAME]` | the same program in the foreground, reconnecting when the connection drops, until interrupted — for a terminal, or a service manager |
| `teanode computer background list [--conversation ID]` | the commands the agent's shell left running in the background on your computers, and the ones that ended lately, newest first: one line each with its id, the computer, where it stands (`running`, `exit N`, `stopped`, or `stopped after 24 hours` when it ran as long as a background command may), when it started in your local time, and the command. `--conversation` keeps to the ones one conversation started |
| `teanode computer background read <computer> <id> [--tail BYTES]` | the last of what one of them wrote: its output on standard output, then its errors on standard error, with a line saying when either was cut and a last line saying where it stands. `--tail` is how much of the end of each stream to print, 64 KiB by default and 256 KiB at most |
| `teanode computer background stop <computer> <id>` | end one of them. The agent that started it hears that it ended, as it does of any ending it did not ask for |

The operator can keep computers off for the whole server with
`agent.features.computer`.
