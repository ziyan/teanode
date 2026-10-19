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
| `user` | the accounts on this server, and `user avatar` for a person's picture; `user rescue` on `teanode-server` makes one an administrator |
| `group` | who may do what, and over which domains: members, roles, domains |
| `role` | the named sets of permissions a group holds; `role permissions` lists what may be given |
| `circle` | the circles you are in and the invitations to join one; see below |
| `audit` | the log of administrative changes, with filters |
| `mailbox` | a mailbox and everything in it: `message`, `invitation`, `folder`, `rule`, `subscription`, `device`, `autoreply`, `programs` |
| `token` | API tokens: `create`, `list`, `update` (rename, or a new lifetime from now), `revoke`; `token create --user` on the console issues somebody's first |
| `app` | apps authorized to act as you: `list`, `rename` (the name stays when the app renews), `disconnect` (revokes every token it holds) |
| `session` | the browsers signed in to the dashboard |
| `passkey` | the passkeys registered to your account; registering one needs the dashboard |
| `settings` | the optional integrations; `settings set <section> key=value` |
| `server` | the running instance: `status`, `restart`, `addresses`, `identity` |
| `services` | which hosted services this server offers: `get`, `set`, and `assistant --kind openai`, which starts the assistant in one step (the key is checked, a model chosen, agents offered and your own agent turned on; `--base-url` for a model server of your own, `--restart` to restart afterwards) |
| `upgrade` | the newest release, and installing it: `status [--check]`, `apply` |
| `mail` | handled mail: `list` with filters, `get`, `content`, `download`, `opens`, `count`, `send` |
| `delivery` | what happened on the way out, and `delivery pending`, the queue |
| `report` | DMARC aggregate reports received about your domains |
| `template` | a domain's message templates, with `render` |
| `layout` | the frames templates are rendered inside |
| `photo` | your library of photos and videos: `list`, `read`, `describe`, `upload`, `download`, `favorite`, `set-date` (correct when a photo was taken), `search-places`, `set-location` and `remove-location` (where it was taken), `trash`, `restore`, `retry`, `save` (keep a photo of a shared album, or a file of a conversation), `delete`, `empty-trash`, `library`, `album` for named sets of photos, with sharing, and `people` and `faces` for the people found in them; see below |
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

Your own messages are `mailbox message`, through the same operations as the
dashboard's list, reader and composer; `teanode mail` is the operator's log of
mail the server handled, not these:

    teanode mailbox message list --unread --from ann
    teanode mailbox message show <id> --thread
    teanode mailbox message move <id>... --folder Receipts
    teanode mailbox message flag <id>... --read --flag
    teanode mailbox message junk <id>... [--not-junk]
    teanode mailbox message send --to ann@example.org --subject Lunch --text -
    teanode mailbox message draft --subject Later --text notes.txt

`list` shows the Inbox unless `--folder` names another or `--everywhere` asks
for the whole mailbox; `--unread`, `--flagged`, `--has-attachment`, `--from`,
`--to`, `--subject`, `--search`, `--since` and `--before` narrow it, and
`--first` with `--offset` (or `--after <id>` within a folder) reads on. `show`
prints the headers and the text, `--html` the HTML part instead, `--thread`
the whole conversation oldest first, and `--save-attachments <directory>`
writes the files out under the names the sender gave them, never outside the
directory. A moved message gets a new identifier, which `move` prints. `send`
sends as the mailbox's only address, or `--from` when it has several;
`--reply-to <id>` keeps the conversation together and marks that message
answered, `--forward <id>` carries a message's attachments, and
`--submission-id` reuses an identifier so that a send whose answer was lost is
not sent twice. `draft` takes the same flags and saves to Drafts; `--draft
<id>` replaces a draft saved before.

A message that carried a calendar invitation has `mailbox invitation show <id>`
(what, where, when, who is asked and what you have answered) and `invitation
answer <id> accept|tentative|decline`, which marks your copy and mails the
organizer, so it needs the right to send as well as to use the calendar. The
agent does the same through `mail_read` with `invitation` and `mail_act` with
`answer_invitation`, which asks you first because it speaks for you.

The rest of the group is the rest of the mailbox. `mailbox list` names the
mailboxes you can open, and `--all` every mailbox on the server with its
owner; `show` and `update` read and change a mailbox's name and signature.
`folder list|create|rename|move|pin|unpin|delete` is the tree in the rail.
`rule list|add|remove|enable|disable|test|apply` is the filing.
`subscription list|show|mail|unsubscribe` the mailing lists it receives,
`device list|add|remove` the app passwords a mail program signs in with,
`profile <device>` a setup profile that adds the mailbox, its calendars and
contacts to an iPhone or a Mac with an app password of its own (it prints an
address to open on the device, once, within five minutes; `--output` saves it
instead, and is the way to make one on a server that has no name set),
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

### A person's picture

`teanode user avatar set|remove` is the picture shown beside a person's name
on the dashboard and in the app, and the initial on a
color of their own when there is none:

    teanode user avatar set portrait.jpg
    teanode user avatar remove
    teanode user avatar set --user personb portrait.jpg    # needs user:manage

It acts on the signed-in account; `--user` names another by username, which
needs `user:manage`, like changing anybody else's name. The file is a PNG or
JPEG of at most 8 MiB and 20 megapixels. The server crops it to the center
square, scales it to 512 pixels and stores it as a PNG, so everything else in
the file, including where a photo was taken, is dropped. A WebP or HEIC photo
is refused with the reason; convert it first. Setting a picture replaces the
one before it. The agent does the same when asked, through the `account` tool's
`set_avatar` action with a picture from the conversation.

### Circles

`teanode circle` is the family, friends or colleagues on this server that you
belong to. A circle is named by its identifier, which `circle list` prints;
people are named by username.

    teanode circle create --kind family "Home"          # --kind is required: family, friends or colleagues
    teanode circle invite <circle-id> personb            # --admin to invite one as an admin
    teanode circle invitations                           # as personb: what is waiting
    teanode circle accept <invitation-id>                # or: decline <invitation-id>
    teanode circle show <circle-id>                      # members, roles, and for an admin who is still being waited for
    teanode circle role <circle-id> personb admin        # or member
    teanode circle people                                # everybody you share a circle with, once each (the username where there is no name), and what each is to you
    teanode circle relate personb father                 # personb is your father; they are told and see you as their son or daughter
    teanode circle unrelate personb                      # take it away, for both of you

`create`, `rename`, `delete`, `invite`, `withdraw`, `role` and `remove` change
the circle and need you to be one of its admins; a member who is not one is
told so. `leave` takes you out, and the last person to leave deletes the circle.
A circle always keeps an admin, so the last admin cannot leave or be made a
member while others remain; make somebody else an admin first, or delete it.

`accept` and `decline` of an invitation that was already answered, or has
expired, say so. `invite` answers the same whether or not the username is an account, and
whether or not that person is in the circle already or may use circles, so it
is not a way to list who is on the server. An invitation lasts thirty days.

An operator with `user:manage` can put an account in a circle without an
invitation, for somebody who will not set it up themselves:

    teanode circle add <circle-id> personb               # --admin for an admin

For an account that does not exist yet, `teanode user create` takes the circles
in the same breath, so the person is in them as soon as they have a password
(`--circle` is repeatable, and they join as members):

    teanode user create --stdin --circle <circle-id> --circle <other-id> personb

A circle that refuses does not undo the account or stop the others; the
error names the circle and the `circle add` that finishes the job. Without
`user:manage`, `circle add` says it needs that permission; it does not answer
that the circle was not found.

`relate` says what somebody you share a circle with is to you, so that "ask my
father" or "where is my wife" reaches the right account. It prints what the two
of you now are to each other: `Sam Lee is your father; they will see you as
their son.` The other person is told (at most once a day), sees you as the
opposite, and can change or remove it, for both of you. Only the two of you
see it, it ends if you stop sharing any circle, and your agent knows it. `teanode circle relate --help` lists the words: the neutral one
first, then the masculine and the feminine (`spouse, husband, wife`;
`parent_in_law, father_in_law, mother_in_law`; `friend`; `colleague`). The
word shown for the other person follows how they say they are described, and
until they say, the word you chose or the neutral one. You say how you are
described yourself with:

    teanode user update robin --pronoun she              # he, she, they, or none to clear it

It is refused for any account but your own, whoever asks, and the people who
share a circle with you see it, as they see your name. That is the word the
other side of a relationship uses for you (`son` rather than `child`) and how
your agent speaks of you; without it, you are spoken of as they and never
guessed at from a name. `circle relate` and `circle
unrelate` need a circle in common with the person, and say that nobody by that
name shares one otherwise.

Who sees whom, and what a circle is for, is in `docs/subsystems/circles.md`.
The agent does the same through its `circle` tool, with an action for each
subcommand under the same word: `list`, `show`, `people`, `relate`, `unrelate`,
`create`, `rename`, `delete`, `invite`, `accept`, `decline`, `withdraw`, `role`,
`remove` and `leave`, and for somebody who manages accounts `add`. Deleting a
circle, removing a member and leaving are asked about as destructive, a role
change, accepting or declining an invitation and inviting as granting, and
`relate` and `unrelate` as outward, since the other person is told, and
renaming and taking back an invitation as an ordinary write. The agent relates people only on your word, never from mail or memory,
and sets your pronoun, with the `account` tool's `update` and its `pronoun`
argument, only on your word too. At the start of each of your turns it is told
how you are described and who in your life is what to you.

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
| `teanode agent ask <message \| ->` | say something to your agent and print what it answers; `--new` starts a named conversation, `--conversation` continues one, `--attach FILE` (repeatable) hands it a file (a picture is shown to it, a text file read to it, anything else named), `--recording RECORDING_ID` (repeatable) points it at a recording, as Ask agent does on the recording's page, `--photo PHOTO_ID` (repeatable) points it at a photo of your library (a photo in the Trash too; one you may not read is refused, and that includes one that is not yours and is in no album shared with you or your circle; for a photo of a shared album the model is told no place unless the share includes places), `--reply-to MESSAGE_ID` answers a message of the conversation (ids from `conversation show --json`), `--json` streams every event, `--quiet` prints the answer alone. A tool that needs your word asks on the terminal, y or n, never a flag |
| `teanode agent chat` | the same, turn by turn, until an empty line |
| `teanode agent react <message-id> <emoji>` | puts your reaction on a message of a conversation, as the drawer's picker does: one or two emoji, replacing the one you had there; `--remove` takes it away. Message ids come from `conversation show --json`, and `conversation show` prints the reactions under each message |
| `teanode agent conversation todo list` | the task list a conversation keeps: the agent's own, which it writes as it works through something in several steps and shows itself every round. `todo list <conversation-id>` prints it; only the agent changes it |
| `teanode agent conversation direct <username>` | open your chat with somebody you share a circle with (`circle people` lists them) and print `conversation <id>: <title>`, the id being your own for it; the same chat every time, but each person has an id of their own for the same chat, so one person's id is not found by the other. `ask --conversation ID <text>` then says something there and prints that it was sent (a message starts no agent unless it addresses one), `ask --conversation ID --ask-agent <text>` also asks your own agent there, `--address <agent id or owner username>` (repeatable) asks another member's agent there that its owner let the chat ask, and `--reply-to <message id>` to an agent's message asks that agent, which answers everybody in the chat under its name while you follow the turn as you would an ordinary ask (what it did to answer stays yours), and `chat --conversation ID` shows what the others say, a couple of seconds after they say it, and sends each line you type (one that starts with `@agent` asks your agent) |
| `teanode agent conversation group <title> --with <username>,...` | start a group chat with you as its admin; `--with` is repeatable or comma-separated and each must share a circle with you |
| `teanode agent conversation members <id>` | who is in a chat and who has left, their role, how far each has read, and what their agent does there (told for your own and for an agent its owner let the others ask) |
| `teanode agent conversation remember <id> on\|off` | let your agent remember a chat from now on, as a knowledge source it reads with the rest of its sources, or stop; what it learned stays, and turning it on again reads from then |
| `teanode agent conversation others-may-ask <id> on\|off` | let the others in a chat ask your agent there, which then answers them limited to the chat and the web and nothing of yours, or stop them. Leaving the chat turns both off |
| `teanode agent conversation add <id> <username>...` | put people in a group chat, who read it from then on; needs an admin of the chat, and each person must share a circle with you. Somebody already in the chat is not added again: the command says they already are, and adds nobody |
| `teanode agent conversation remove <id> <username>` | take a person out of a group chat; needs an admin; asks first |
| `teanode agent conversation leave <id>` | leave a group chat, which you can still read up to now; asks first. A circle's chat follows the circle: leave the circle instead |
| `teanode agent conversation mute <id> [--until TIME\|--off]` | silence the chat's phone notifications for good, or until a time (`2026-11-01 09:00`, RFC 3339, or how long: `8h`, `3d`); `--off` ends it |
| `teanode agent conversation read <id>` | say you have read the chat through its newest message; the mark never moves back |
| `teanode agent conversation edit <message-id> <text>` | change one of your messages in a chat; the others see it was edited |
| `teanode agent conversation delete-message <message-id>` | delete a message of yours in a chat, or any message in a group you are an admin of; asks first |
| `teanode agent conversation list\|show\|new\|rename\|goal\|main\|archive\|unarchive\|delete` | the main conversation, the named ones and your chats with other people (a person whose agent is off lists only the chats); `list --kind chat` lists the chats alone, `list --query` finds one by words in its title or in what was said (a chat by its title and its people), and `list --archived` lists the ones put away instead of the open ones; `show` prints the goal above the messages when there is one and the task list under them (for a chat, who is in it, how many are unread and who wrote each message), `rename` also renames a group chat, the done ones ticked, and pages back through a long one with `--first` and `--offset`; `main` makes a named conversation the main one, or starts a fresh main one and keeps the old as a named one; `archive <id>` puts one away with everything in it and `unarchive <id>` brings it back, neither asking first because neither loses anything; `delete` asks first and takes the files that came with it |
| `teanode agent waiting [--first N]` | the questions and approvals the agent waits on in every conversation, newest first, with how many there are in all (calls other people's agents make through a skill you shared wait for you too and are not listed here: `agent skill calls <share-id> --waiting` lists them); `waiting approve\|deny <id>` and `waiting answer <id> <answer>` answer one, as its card in the chat would |
| `teanode agent goal list [--state working\|waiting\|met\|dropped]` | what the agent keeps at in the background between conversations, the most recently active first: each goal's id, where it stands (tracking, needs you, done, dropped), its title and its one-line status; the same list as the Goals tab of the agent's page |
| `teanode agent goal show <goal-id>` | one goal: what it is for, its status, when it looks again, what it made (schedules, background work, mail rules, reminders, alert mutes) and what happened on it, newest first |
| `teanode agent goal start <title> <description>` | start a goal in the background: a title of a few words, then what it is for and what done looks like. Its first turn runs at once, in its own conversation; you hear from it in your main conversation only when it needs you |
| `teanode agent goal tell <goal-id> <text>` | answer a goal that waits for you, or tell it something it should know; it goes on a minute later |
| `teanode agent goal take-schedule <goal-id> <schedule-id>` | make a schedule the goal's: its runs take place in the goal's conversation as its turns, and the goal takes none of its own while it has one |
| `teanode agent goal done\|drop\|reopen <goal-id>` | mark a goal met, stop it, or take one that was met or dropped up again |
| `teanode agent phone pair` | draw a one-time code in the terminal for the TeaNode app on your phone to scan, connecting it to this server as you, with the link beside it for a phone that opens it instead. It works once, for ten minutes; it signs a phone in as you, so show it to nobody else. The same code as the dashboard's Connect page, except that a phone connected with a code drawn here cannot approve a call that spends your money or lend it: connect it from the Connect page for that |
| `teanode agent phone list\|rename\|notify\|test\|remove` | your phones, as the TeaNode app section of Devices in Settings shows them: `list` says whether this server can notify each and when it was last seen, `rename <phone-id> <name>`, `notify <phone-id> <kind>...` chooses what a phone is told about (`message`, `approval`, `question`, `answer`, `circle`, `chat`, `skill_share_call`, `photo`, or `all`), `test <phone-id>` sends one a notification, and `remove <phone-id>` signs it out, asking first unless `--force` |
| `teanode agent idea list\|get\|propose\|start\|done\|dismiss\|reopen` | what your agent offers to do for you, and what became of each offer: the same list as the Ideas tab and the agent's `idea` tool. `list` shows the open ones, `--status dismissed` or `--all` the others, and `--limit` with `--offset` a page of them; `get <id>` shows one whole, with what you would say to start it and what prompted it; `start <id>` opens a conversation named after the idea and prints what to say in it, and `--send` says it; `done`, `dismiss` and `reopen` say what became of one. `propose` keeps one you write, checked as the agent's are: it may only need tools your agent has, must say where it asks first when a tool acts toward somebody else, and names what prompted it with `--evidence message:<item-id>:<what it is>` |
| `teanode agent run list\|show\|stop` | what the agent did on its own: every model call is a run, and `list` pages through them with `--first` and `--offset`; `--kind triage --kind reply`, or `--kind triage,reply`, narrows it to some kinds of run and `--query` to what they were about; `--all` or `--agent <id>` lists everybody's, for an operator with `agent:act`; `stop <run-id>` stops one where it is, keeping what it has already done |
| `teanode agent tools` | the tools your agent has, as you may use them, with the risk class and whether it asks first |
| `teanode agent memory index\|get\|search\|note\|page\|merge\|link\|unlink\|move\|forget\|history\|recall\|learned` | what your agent knows about you, as pages with numbered facts: `get people/alice-chen`, with `--link-limit` and `--link-offset` to print a page's links a part at a time, `note people/alice-chen "Does the books" --applies-to triage,reply`, `link people/alice-chen projects/portal --relation works_on`, `move notes/marigold things`, `move --number 3 people/alice-chen projects/portal` puts one fact on another page instead of the whole page, keeping the words it came from, `forget people/alice-chen --number 2`; `forget people/alice-chen` with no number takes the page and everything under it and asks first, `--force` skips the question; `history projects/portal`. `search "<words>"` finds pages and facts by words, `--first` at a time (40 by default); a search that found more says how many more pages and facts, "at least" where it stopped counting, and the `--offset` that prints the next page |
| `teanode agent memory links <path>\|backlinks --record <kind>:<id>\|link <path> --record <kind>:<id> --relation <relation>\|unlink <path> --record <kind>:<id>\|confirm <path> --record <kind>:<id>\|contact <path> <contact-id>` | a page's links to your own records, and the other way. A record is written `kind:id`, with kind one of `contact`, `mail`, `calendar_event`, `reminder`, `conversation_message`, `finance_transaction`, `finance_receipt` or `recording_passage`; a passage of a recording is `recording_passage:<recording id>@<start ms>-<end ms>` (add `--transcript-run` to `link` for the transcript the range is counted in). `links people/alice-chen` lists what the page is linked to, each with the record's title and date, a guess marked "perhaps", and a record that is gone marked as no longer available; `--first` and `--offset` read it a part at a time. `backlinks --record mail:<item id>` lists the pages linked to a record, and for a passage the pages whose passage overlaps the range. `link ... --record` states a link (what you do is yours to say; a link the agent made reads as a guess until you do), takes `--relation` (`is_contact`, `wrote`, `received`, `attended`, `organized`, `spoke_in`, `mentioned_in`, `paid`, `paid_by`, `receipt_for` or `about`, which has to fit the kind) and an optional `--note`, and only takes a record that is yours and exists. `unlink ... --record` takes every relation of that record away unless `--relation` names one. `confirm` states a guess the agent made, which is what Confirm does on the dashboard and the phone: of one record with `--record` (and `--relation` when it has several), or every guess on the page with `--all`. `contact` names the card a person's page is about, from your own address books, and links the page to it; `--clear` takes it away. It never makes a card. `memory get` says how many records a page is linked to |
| `teanode agent memory share <path> <username\|circle-id>\|shares\|unshare <share-id>\|shared` | pages of your memory shared with other people, read only. `share people/alice-chen ben` gives `ben`, who must be in a circle with you, the page and everything under it (`people/alice-chen/...`, never `people/alice-chen-two`); a circle by name or identifier shares it with everyone in it you are in (a username is that person even when a circle has the same name; a name two circles have is refused with their identifiers and sizes). The words each fact was read in (your mail, documents) stay private unless you add `--with-quotes`, and those from a chat never are shown; sharing again changes that. `--preview` prints exactly what they would see and how many pages it reaches, and shares nothing. `shares` lists what you shared, with whom and whether the quotes are shown; `unshare` takes one back and the reader loses the page at once, because nothing was copied, as they do when one of you leaves the circle you share. `shared` lists the pages other people shared with you, by who shared them, and `shared --query words [--first N]` finds the words in their names, openings and facts, saying whose each hit is and, when more matched than it shows (the hits of every owner are taken in turn before it stops), that there may be more. `memory get <path> --owner <username>` reads one of those, with its facts numbered as its owner numbers them, where each was read ("from their mail") and the quotes only if they chose to share them (never those of a fact read in a chat), and no overview, which it says is not shared; a page not shared with you is not found |
| `teanode agent memory recall "<question>"` | what a question would carry into a turn: the pages recall would expand, each with the facts on it as the page numbers them, printed the way `memory get` prints a page. It is the same recall a turn does, so it answers "why did it not know that?" without asking it again — and it costs nothing: no model is asked anything and nothing is marked as used, so the same graph answers the same twice. `--json` for the pages and facts as they came. `--search "<words>"` (at most two) and `--broad` follow a retrieval plan, as a live turn follows its depth judgement's. `--explain` says why as well: the retrieval mode and plan, each query that ran (the message, each planned search, the linked-pages hop, the broad pass), how many each search of each query found, where each page and fact ranked in each search and once fused, and what carried it or kept it out (the page limit, the limit on pages no fact was found on, the token budget, a retired fact), with the overview section carried and why that one |
| `teanode agent memory checkout [directory]` | what a Claude Code or Codex session in a directory is shown by `teanode hook`: the pages of the checkout the directory is in, their liveliest facts, and where the last session there stopped. `--prompt "<words>"` prints what that prompt would recall instead, kept to the checkout's project and what links to it (`--everywhere` for all of memory). `--computer` names the computer when it is not this one's host name. Nothing is marked as used |
| `teanode agent memory capture <claude-code\|codex>` | reads that tool's new sessions on a computer now, as the hooks do after each answer, so the latest answer is searchable within a minute and the next session knows where this stopped. `--computer` names the computer when it is not this one's host name. Says so when no enabled source of that tool reads sessions there |
| `teanode agent memory overview <path>` | how the thing a page is about works, as the dream last wrote it from the page and the pages under and beside it: what it is, its parts, how it relates to what it is linked to, what has been happening, what stands out. `--rewrite` asks the next dream to write it again; `memory get` prints it too, under the opening, with a theme's reflections. It ends with what the overview covers, counted by the server: how many of the pages under it, of a theme's pages and of its links its prompt shows of how many there are, how many of those had no overview of their own, and whether what it is written from has changed since |
| `teanode agent survey "<question>"` | answer a question about a whole from the overviews: one read-only run a page in scope, several at once, then one that combines them into a report with citations. `--scope themes/<one>` or any page narrows it; with none it covers every top-level theme. Runs of kind survey, a few minutes. It starts the survey on the server and asks every few seconds whether it has finished, so no request is held open that long. A read that fails for a passing reason (the server restarting behind its proxy, a dropped network, a timeout) is tried again, less often each time; after thirty minutes the command stops waiting. Whichever way it ends, it names the survey's id, for `teanode agent background show <id>`; `--no-wait` prints the id and returns, and stopping the command leaves it running. `--json` for the finished work, with the report and the runs |
| `teanode agent background list\|show\|stop` | the surveys and subagents your agent started without waiting for them, and the surveys `agent survey` started: `list` says what is queued, running or finished lately, newest first; `show <id>` prints one's report or answer, and on standard error how it stands and the runs it made; `stop <id>` stops one that is queued or running, which then wakes nothing |
| `teanode agent memory answers <file>` | answer each question of a set that has an `expectedAnswer`, from memory, from the sources or from both (`--from memory,sources,both`; `memory@planned` and `both@planned` follow each question's `plan`), and grade each answer against the expected one: a line a question and source — `memory changed-01 stale gave the old address` — then a score, the verdicts, the cost and the time an answer took, per source. Two model calls a question and source, as runs of kind `evaluate`; nothing is written to a conversation or the graph. `--json` for every answer. See `docs/evaluation/` |
| `teanode agent memory plan <file>` | ask the depth judgement what retrieval plan a live turn would follow for each question, and with `--output <file>` write the question set back with each question's `plan`, for `evaluate --mode planned` and `answers --from memory@planned`. One call to the fast model a question, as runs of kind `evaluate`. `--stored` plans the memory check's confirmed and corrected questions instead of a file's |
| `teanode agent memory evaluate <file>` | replay a set of questions through recall and say, per question, whether the facts it needs would have been carried: one row a question — `direct 03 hit`, `changed 12 miss: carried people/alice-chen saying "berlin"` — then totals per kind and overall, and a non-zero exit when anything missed. No model is asked anything and nothing in the graph is marked as used, so the same graph answers the same twice and the set can be run before and after a dream to see what the dream was worth. `--mode planned` follows the plan each question carries (see `memory plan`), as a live turn would. The file's shape, and a starter set to replace with questions from your own graph, are in `docs/evaluation/` |
| `teanode agent knowledge list\|add\|set\|pause\|resume\|sync\|remove` | the places your agent reads: `add "work" ~/work --computer laptop --under work`, `set work --cron "0 4 * * *" --under projects` changes a source that already exists — its name, its path, where what it finds is filed, how often it is read, the format, the mailbox, whether every checkout under it is read — leaving everything you do not give alone, so correcting the hour does not cost you what removing and adding it again would, `--read-every-checkout` reads the files of every checkout under the path — by default a checkout that is barely any of your work is kept to its profile, so the page says what it is and where it lives and its source is not indexed, and the source's row says how many checkouts that was and how many files, `--own-commits-at-least` says how many commits of your own a checkout needs before its files are read, however long its history (leave it alone and the program works it out: two commits, or a fiftieth of the log, whichever is more, up to twenty-five — and never more than the whole history, so a repository you committed to once and nobody else ever is yours; `1` is the older rule, any commit at all), `--commits-per-pass` says how much of the history one pass over the tree carries, shared among the checkouts in it, newest first (the program's own pace is two thousand), `sync` reads one again now, `pause` keeps what it found, and `remove` forgets everything it found and so asks first, `--force` skipping the question. `--format` says how to read what is there: `files` (a tree of files, git-aware), `journal` (dated notes), or `records` (a folder of JSON lines any script can write, with a `refresh` script beside them the daemon runs before each scan — ask your agent to write one) |
| `teanode agent knowledge search "<words>"` | find passages in what has been indexed — your code, your chat, your notes — without asking your agent to look for you. It is the same search your agent's knowledge tool runs, so what you see is what it sees: an identifier out of a log (`ComputeShippingQuote`) is looked up exactly and printed as the file and line that defines it, then the passages, each under the document it came from, with the identifier that reads that document back. `--first` how many, `--offset` how many of the ranking to pass over, `--source <id-or-name>` narrows it to one source, `--json` prints the rows as they came, each with its score. Where the deployment has no embedding model it says so — found by words alone, which misses a paraphrase that shares no word. A search that found more than it printed ends with how many more passages ("at least" where it stopped counting) and the `--offset` that prints the next page; pages read one after another show every passage once, in the order of the first. Over a tree of files it also says which directories the passages are in, the most first; `--directory <one of them>` (with `--computer` where several computers have that path) searches inside it, which is how a large tree is searched one part at a time |
| `teanode agent knowledge read <document-id>` | read one of those documents, by the identifier the search printed; the `<id>#<passage>` out of a citation is taken as well. `--from` is where in the text to start, counted in characters, and `--first` how many to print; a read that does not reach the end says how much is left and prints the command that carries on from where it stopped |
| `teanode agent dream log\|runs\|now\|bootstrap\|reread` | the dreams: the runs that read, file and rehearse; `runs <id>` lists every model call one dream made, each a run `agent run show` opens; `now` starts one within the minute, whatever the agent's hours; `bootstrap on` keeps it running with wider limits until nothing waits to be read, for a first ingest; `reread --minutes 60` puts back what a night marked read in that window |
| `teanode agent schedule list\|add\|set\|enable\|disable\|remove\|run` | what it does on its own at set times: `add Morning "0 8 * * 1-5" "what needs me today?" --deliver mail`, a cron line in your zone; or a single moment, `"@at 2026-09-12 09:00"`, or a distance from now, `"@in 20m"`, which is stored as the moment it means and runs once. `set <id> --cron "0 7 * * 1-5"` changes one in place — `--name`, `--cron`, `--prompt` (`-` reads it from standard input) and `--deliver`, and only what you give changes — and `disable <id>` stops it running without taking it away, `enable <id>` starts it again |
| `teanode agent brief on\|off\|now` | a brief each morning, by mail: what the day holds, what is waiting for an answer, what is being held. `on --at 07:30 --days 1-5` says when; `now` sends one immediately. It writes an ordinary schedule called "Daily brief", so `agent schedule list` shows it and anybody may rewrite what it asks for |
| `teanode agent feedback` | the corrections recorded from what you did, which the agent is shown as examples |
| `teanode agent channel list\|set\|unlink\|remove` | the chat apps you talk to your agent from: your own Telegram or Discord bot. `set telegram --token -` reads the bot's token from standard input; `list` shows the code a chat sends the bot as `/link CODE` to become the linked one, and whether the bot runs; `unlink` draws a new code |
| `teanode contact list\|show\|add\|edit\|remove\|remove-picture\|link\|unlink\|suggestions` | your address book: the people you keep, which your phone and your computer synchronize over CardDAV. `add --name "Ada Lovelace" --email ada@example.com` keeps one; `edit <id> --name "Ada King"` changes only what you give and leaves the rest of the card alone, so correcting a name does not throw away the photograph a phone put there; `--card -` reads a whole vCard from standard input; `remove-picture <id>` takes only the picture off a contact (asks first, or `--force`), whoever put it there, and leaves the rest of the card. `link <contact id> <username>` notes that a contact is somebody you share a circle with (it looks in each of your address books for the contact, and with the id in more than one book it refuses until `--address-book <id>` chooses; it refuses your own account, someone outside your circles and someone another contact in that book already is); `unlink <contact id>` takes it back (also with `--address-book`); `show` then prints `In your circles as <username>`. It is your note: that person is not told and sees nothing. `suggestions` lists your unlinked contacts that carry a mailbox address of someone in your circles, each with the `link` command that confirms it (and the address book it is in, when you have more than one), and never says which address matched. A link ends when you and that person stop sharing any circle |
| `teanode calendar list\|show\|add\|edit\|remove` | your calendar, which your phone and your computer synchronize over CalDAV. `list --from 2026-09-14 --until 2026-09-21` prints one line for every time something happens, a repeating event once per occurrence; `add --title Standup --starts 2026-09-14T09:30 --repeat FREQ=WEEKLY;BYDAY=MO` puts something in it, `--repeat` taking an iCalendar rule as the dashboard and the iPhone app write it (`FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR` is every weekday, `FREQ=WEEKLY;INTERVAL=2;UNTIL=20261231T235959Z` every other week until the end of that day in UTC, `UNTIL=20261231` for an all-day event); `--invite ada@example.com` sends the invitation by mail, and moving or removing the event tells everybody invited; `--all-day` belongs to the day rather than to a time, and its `--ends` is the last day it is on, so the same date at both ends is one day; `--private` marks the event private (`--private=false` takes the mark off), so people your calendar is shared with see only that you are busy, and `list` and `show` say which events are private; `--file -` reads a whole iCalendar file from standard input. Times are read and written in the calendar's own zone unless they carry an offset |
| `teanode calendar request <request-id>` | check whether a calendar save completed, even after its event or calendar was deleted. `add`, `edit` and `remove` print a request ID before changing an event; after an uncertain response, look it up here or retry the same operation and fields with `--request-id`. A retried removal checks completion before loading the deleted event. An absent receipt may mean the original request is still in progress. Changed fields require a new ID |
| `teanode calendar stop <request-id>` | resolve an uncertain request before discarding its fields. An uncommitted request is durably stopped; an already completed change is reported and is not undone. A failed stop response is still uncertain, so retry the same request ID |
| `teanode calendar free` | when you are free: the stretches of the working day nothing is booked in, day by day. `--earliest 08:00 --latest 18:00` moves the ends of the day; a whole-day entry does not make a day busy, and neither does anything cancelled. Worked out with the same two functions that answer a phone's free-busy request, so what this prints and what a colleague's client is told cannot disagree |
| `teanode calendar share <username\|circle> --level busy\|titles\|details\|edit [--calendar <id>]`, `shares`, `unshare <share-id>` | who your calendar is shared with, and how much they see. `share ben --level titles` shares with a person you are in a circle with, `share Home --level busy` with everyone in a circle you are in (by name or identifier; a username is that person even when a circle has the same name, and a name two circles have is refused with their identifiers and sizes; the identifier is never ambiguous); sharing again changes the level, `shares` lists them with the identifiers `unshare` takes. `busy` is when you are busy and nothing else, `titles` adds the time, title and place of each event, `details` is everything including guests, `edit` lets them add, change and remove events that have no guests. An event you mark private (`calendar add --private`, written as `CLASS:PRIVATE`) is only ever shown as busy. Someone you share no circle with is refused as if they did not exist |
| `teanode calendar circle <circle-id> [--name <name>]` | make the calendar a circle keeps for itself, which everyone in the circle reads and writes (admins only; a circle has one). Its events cannot have guests |
| `teanode calendar list --owner <username\|everyone>` | what other people shared with you, at the level each was shared at, each line saying whose it is: one person's, or `everyone` for all of them and your circles' own calendars. Time shared only as busy prints as `busy` |
| `teanode calendar free --with ada,ben` | when you and those people are all free: the same day-by-day stretches as `free`, from everybody's busy times, yours included. Each has to have shared their calendar with you at `busy` or more; it says who has not rather than leave them out |
| `teanode calendar calendars\|set` | the calendars themselves, yours, your circles' and the ones shared with you, the last two with whose they are and the level you reach them at; for your own, what it is called, what a client paints them, the zone a new event is written in, and the day a week is drawn from: `set --timezone Europe/Berlin`, `set --week-start monday`. Weeks start on Sunday unless a calendar says otherwise; the five-day view is Monday to Friday either way, because that is what a working week is |
| `teanode location now\|visits\|places\|path\|nearby\|search\|addresses\|save\|forget\|checkin\|erase` | where you are and where you have been. The TeaNode app on your iPhone reports where you go (nothing on the command line reports a position); each visit is to a place, and place names come from OpenStreetMap unless you named the place: your server sends the position of each new place to the place provider to look up its name. `now` is your latest position and the visit in progress; `visits --from 2026-10-10 --until 2026-10-12` lists visits newest first, with a `photo count` column counting the photos you took during each of your own visits (`--json` carries `photoCount` and the first four `photos`), and `teanode photo list --after` and `--before` at the arrival and departure list them (`--place <id>`, `--label home`, `--first` up to 200 and `--offset` to page); `places` lists your places, the ones with a label first (`--label`, `--query`), and `place <place id>` shows one of them by its id; `path --day 2026-10-11` is one day's positions, oldest first. `nearby` is what OpenStreetMap calls the place at a position and your own places there (only your own while the server's place provider is switched off), and `search playground --radius 5000` finds places by words near where you were last reported to be (`--near-place <id>`, or `--latitude` and `--longitude`; `--first` up to 50); searching needs the server's place provider to be switched on. `addresses "6 Rue Example, Exampleton"` lists the addresses matching the words, nearest first to one of your places (`--near-place <id>`), to a position (`--latitude` and `--longitude`), or to where you were last reported to be, with their latitude and longitude and how far each is from there (`--first` up to 20, 8 by default); it needs the place provider too, and the words you type are sent to it. `save <place-id> --name "Grandma's" --label family --radius 100` names, labels and resizes a place (an empty `--name` goes back to OpenStreetMap's, `--label none` takes the label off, labels are home, work, school, family and gym), and without an id `save --latitude 40.0 --longitude -75.0 --name Piano` makes a place. `save <place-id> --address "6 Rue Example, Exampleton"` looks the words up, moves the place to the first match, the one nearest to the place, and keeps that address (the place keeps its label and any name you gave it, and OpenStreetMap does not rename it), then prints the address it chose and the other matches; if the first was wrong, rerun with `--latitude`, `--longitude` and `--address-text` copied from `addresses`. Without an id, `save --address "..." --label home` makes your home, or labels your own place that is already there. `checkin --place <id>` records that you are there now and ends the visit in progress (where you already are, nothing changes); `checkin --latitude 40.0 --longitude -75.0` checks you in at your own place that holds the position, or makes a new place there that is called after its city until OpenStreetMap names it. `forget <place-id>` forgets a place and every visit to it. `erase [--from <time>] [--until <time>]` erases your positions and visits (in the range, or all of them), and the places left with no visit that you did not name or label; places you named or labeled stay, and anything your agent already learned from those days stays in its memory until you remove it. Both ask first, and cannot be undone (`--force` skips the question) |
| `teanode location people\|shares\|share\|unshare` | who sees whose location. `share ben --level current` shares where you are now (they see where you are now and the place you are at) with a person you are in a circle with, and `share Home --level history` shares where you are and where you have been (they also see your visits and the places they were at) with a circle you are in; sharing again changes the level, `shares` lists them with the identifiers `unshare` takes. `people` lists who shares with you and where each is, and `now` takes `--person ben` to read theirs; `visits`, `places` and `path` take it too for somebody who shares their history, and say "ben shares only where they are now" for somebody who does not. Someone you share no circle with is refused as if they did not exist |
| `teanode health types\|summary\|samples\|route` | your Apple Health data, which the TeaNode app on your iPhone copies to this server (nothing on the command line reports it). A type is one kind of data, named as HealthKit names it without its prefix: `stepCount`, `heartRate`, `sleepAnalysis`, `workout`. `types` lists the ones you have with their Health category, unit, the days they cover, how many readings there are and the latest day, and then the facts about you that Health keeps, in words (date of birth with your age, `Female`, blood type `O+`, skin type, wheelchair use `Yes`, or `Not set`), and with nothing kept says where to start: Settings, then Health in the TeaNode app, and the data you allow on Apple's Health permissions screen (there is no command that sets Health up). `summary stepCount --since 7d` adds a type up by day, week or month (`--period day\|week\|month`, a week starts on Monday; `--from`, `--to` and `--since 30d` choose the days, the last 30 days by default): steps and energy are totals, heart rate and weight are averages with the lowest and highest, and sleep and workouts show the time covered in hours and minutes, sleep with the time asleep and in each stage (Deep, Core, REM, Awake). A week or a month also shows the days it holds data for and the average per day (per night for sleep) over those days, next to the total, as the dashboard does. Numbers carry the same words as the dashboard and the iPhone app (the table is at the top of `internal/health/words.go`): `8,412 steps`, `1 step`, `74 bpm`, `16 breaths/min`, `36.6°C`, `5.2 km`, `42 mL/kg·min`, and times as `7 h 12 min`. A day counts the readings of an iPhone and a Watch for the same walk once, as the Health app does. `samples heartRate --since 1d` lists the individual readings, newest first, for whole days in this machine's time zone (`--limit` 50 by default and up to 200, and `--offset` to page; it says on standard error where the next page starts, and how many readings there are in all, or that there are more than 100,000), and `route <health-sample-id>` is the path of a workout that has one (`--json` has every point as seconds from the start, latitude, longitude and altitude). `summary` and `types` take `--person ben` for somebody who shares Health with you, for the categories they share; readings and routes are only ever your own |
| `teanode health settings\|erase\|shares\|share` | what you choose about Health. `settings` shows whether your agent may read your Health data, when the last reading arrived and how many are kept, and the facts about you that Health keeps, and `settings --agent-can-read off` turns it off: it is off until you turn it on, it is separate from whether your phone sends Health to this server, and what your agent reads goes to its model provider, which the command names. Turning it on is refused from a command line, with a sentence saying where to do it (the Settings tab of the Health page on the dashboard, or Health in the iPhone app's Settings), because commands run on your computer, by you or by an agent there, act with your own login. `erase [<type>]` erases the readings, day summaries and routes of one type, or of all of Health, which also tells your phone to stop sending until you turn Health on again; it asks first, and cannot be undone (`--force` skips the question), and what your agent said about your Health in conversations stays in those conversations. Erasing one type erases what the server keeps of it and does not stop the iPhone app: new readings of it keep arriving while Health is on. `shares` lists what you share and what is shared with you, and `share family activity sleep` shares those Health categories (activity, bodyMeasurements, cycleTracking, hearing, heart, mentalWellbeing, mobility, nutrition, respiratory, sleep, symptoms, vitals, other) with a person you are in a circle with or with a circle you are in. They see your day summaries of those categories, never your readings or routes. Sharing again replaces the categories, and `share family` with none stops sharing. Sharing a category the person or circle does not already have is refused from a command line, with a sentence saying where to do it (the dashboard or the iPhone app); fewer categories, or none, works. Your agent's `health` tool has the actions `types`, `summary` and `samples`, which only read your own Health, and only in a conversation with you, never in a dream, a subagent, a background task or any other run nobody is watching (and never what others share with you: a person who shares Health shares it with people, not with their agents' model providers; what others share with you is on the Health page under Showing, on the dashboard and in the app), and `set_up`, which opens the Settings tab of the Health page where the switch is. The switch is also in Health in the iPhone app's Settings. Turning its own access on, erasing, sharing and the route of a workout are yours to do, not the agent's: the tool has no action for them, and the server takes the first and new shares only from your tap on the dashboard or in the iPhone app |
| `teanode reminder list\|add\|edit\|done\|reopen\|remove` | your reminders list, the one beside your calendar that the Reminders app on a phone synchronizes over CalDAV. `add "Buy stamps" --due 2026-09-29` is due on that day, `--due 2026-09-29T15:00` at that time in the calendar's zone; `list --done` shows the ones ticked off and `list --all` both; `done` ticks one off and `reopen` puts it back |
| `teanode note list\|show\|add\|edit\|remove` | your notes: the ones a phone's Notes app keeps in your mail account, which it syncs over IMAP. `add "Packing list"` writes one whose first line is its title, `--file -` reads the text from standard input, and `edit <id>` replaces a note's text; the phone shows the change at its next sync. `--mailbox` picks the mailbox when you have more than one |
| `teanode agent skill list\|search\|install\|update\|remove\|enable\|disable\|scope\|secret` | tools installed from the skill registry, for everyone on this server: `list` names each skill's tools and `list <name>` says what each of that skill's tools does, `search` says what there is, `install weather` checks the signature and the hash before keeping it, `update` with no name installs every newer version there is. `scope <name> operator|person|skill` settles who fills the skill's secrets in here — one set of values for the whole server, each person's own, or whatever the skill declares. Installing and scoping need `server:manage`. A skill that runs commands runs them on a computer you attached, like the `shell` tool, asks first only for a call the fast model judges would speak for you or cannot be undone, and with nobody watching runs what needs no asking and refuses the rest. `secret list\|set\|clear` is for the values a skill asks *you* for rather than the server: `secret set news NEWSAPI_KEY` reads the value from the terminal without echoing, or from standard input when there is no terminal |
| `teanode agent skill share\|shares\|unshare\|calls\|decline` | share a skill's tools with somebody in a circle you share, and see what they ask. `share <skill> <username> --tools pay --ask-every-time` lets their agent ask to use those tools on your computer, as you, each use waiting for your approval. From a command line the server accepts only a share with `--ask-every-time`, or one that gives no more than the share it replaces (the same tools or fewer, the same currency, limits no higher); any other share, with limits (`--payment-limit 100.00 --monthly-limit 400.00 --currency usd`, in the currency's own units) or without, even of a tool that pays nothing, is made on the dashboard's agent page or in the iPhone app, as is approving a call: the server refuses them to a command line token, and the command says so with the address. A payment limit above the monthly limit is refused everywhere. `shares` lists what you shared and what is shared with you, `calls <share-id> [--state ran|failed|refused|waiting|declined|expired]` (repeatable; `--waiting` is `--state waiting`) lists what was asked through one (the state column says `waiting` as `--state` takes it), with the reason their agent gave in quotes and, for a call that waits, where to approve it, `calls --ended` lists the calls of shares that ended, `decline <call-id>` stops a call (it takes `--note`), and `unshare <share-id>` takes the tools away at once. The agent's `skill` tool lists, and shares only so that every call asks; it cannot approve or decline |
| `teanode agent mcp list\|connect\|disconnect` | the connected servers the operator declared and your connections to them; `connect tracker --credential -` reads your credential from standard input, and an authorizing server prints the address to open; `--loopback` brings the authorization back to this terminal instead, for a service that answers only to a loopback address |
| `teanode agent settings show\|set` | your agent: `set enabled=true name=Bertie instructions=-` reads the long value from standard input; keys are listed by `set --help`. `set alerts=false` stops it telling you about your mail unasked; `alert-quiet-start=22:00 alert-quiet-end=07:00` is the night in which only what cannot wait is said, `alert-daily-most=5` the most a day, and `look-around-weekday=thursday look-around-time=17:00` when it looks around your town for what is on. `unattended=outward,destructive` lets it do those kinds of thing, which otherwise wait for your word, when you are not there (`money`, `granting` and `listed` are the others; empty allows none) |
| `teanode agent alert list\|mute\|mutes\|unmute` | what your agent told you unasked, and what you asked it not to: the same list as the Alerts card on the agent page. `list` shows each alert with the messages it was about; `mute <alert-id>` stops alerts about what it covered (the burst, or the sender of a single message), `--scope subjectKey`, `sender`, `domain` or `kind` on its subject, its senders, their domains or its kind (a burst, or a category such as `notification`), and `mute --target offers@shop.example.com` names one yourself, its scope read from it unless `--scope` says; `mutes` lists them and `unmute <mute-id>` takes one back; the follow-up kinds (`follow_up` for all, `bill`, `parcel`, `deadline`, `event`) are kinds to mute too, and so is `nearby`, the weekly look around your town |
| `teanode agent follow list\|add\|stop\|backfill` | the bills, parcels, deadlines and events your agent keeps track of from your mail: the Following list on the Alerts card. `list` shows what is still being watched, the nearest day first (`--all` adds the closed, expired and stopped); `add --kind bill --title "Water bill" --payee "Example Water" --amount 84.20 --currency USD --due 2026-11-03` has it follow something you name (a bill needs `--amount` and `--due`, a deadline or event `--due`, a parcel `--tracking` or just a title, with `--carrier` and `--place` where they fit); `stop <follow-up-id>` stops following one and nothing more is said about it; `backfill` has your agent look back over the last 45 days of mail it already sorted and queue up to 150 messages that look like a bill, a parcel, a form or a dated notice to be read for follow-ups. It runs by itself once for an agent with alerts on after follow-ups ship, so run it yourself if you turned alerts on later or want it to look again; reading a message twice changes nothing, and what it finds shows in `list` as the messages are read, a few seconds apart |
| `teanode agent settings categories add\|remove` | your own categories beside the fixed ones |
| `teanode agent settings forget` | delete the agent and everything it learned; asks first |
| `teanode agent source list\|grant\|revoke\|set\|allow\|deny` | what the agent may reach and what it does in each: `set --mailbox work triage=true auto-reply=true auto-reply.scope=known` for a mailbox, `alerts=false` to hear nothing unasked about what arrives in it; `allow calendar` and `deny addressbook` for the other two kinds of source, which carry a switch and no policy (with no name, `allow calendar` means the calendar of events, not the reminders list, which is also a calendar and is switched by naming it; it asks for a name only when there are two calendars of events). Nothing from a source you have not granted is ever sent to a model |
| `teanode agent prices show\|check` | the model prices checked against the provider's bill, needing `server:manage`: `show` lists, for each openai provider with a billing key, when it was last checked, the error if the bill could not be read, and every price compared (model, kind of token, configured, billed, days it rests on; a price from a pattern entry is marked "pattern, not changed"); `check` does the check now instead of at the daily one, changes the prices that are missing or differ by more than five percent, tells the operators, and lists what it changed. The same as "Check prices now" on the provider in Manage > Agents > Models |
| `teanode agent usage [--since] [--by day\|kind\|mailbox\|model]` | your tokens |
| `teanode agent draft <item-id> [--say "…"]` | have the agent write a reply to a message, printed for you to use; nothing is saved or sent |
| `teanode agent replies [--status held\|sent\|cancelled\|refused\|failed] [--mailbox]` | the replies the agent wrote for you and what became of each, with the reason when it left a message alone |
| `teanode agent replies cancel <reply-id>` | cancel a held reply; the draft goes and nothing is sent |
| `teanode agent admin usage\|list\|limit\|disable\|enable\|dead-letters\|retry` | everybody's agents, needing `agent:audit`: tokens by day, kind, mailbox, model or agent; each person's sources and today's spend; a per-person limit in tokens or, with `--cost`, in money; the switch-off; the jobs the worker gave up on |

Every command sends the shell's time zone and language with the request,
the way the dashboard sends the browser's, so a person who lives in the
terminal is placed as well as one who lives in the browser.

### teanode recording

Your retained meeting recordings: the same operations as the Recordings page
and the agent's `recording` tool, and named after them (the tool's
`temporary-speaker-name` is `teanode recording temporary-speaker-name`). Each
takes `--json`.

| Command | What it does |
| --- | --- |
| `teanode recording record [--input FILE\|-] [--device NAME] [--no-live]` | record a meeting, the way the iPhone and the dashboard do: from the microphone (through ffmpeg, which must be installed), from a file, or from standard input when it is not a terminal. The audio is cut into pieces of up to a minute where somebody stops talking, each uploaded and verified while the recording goes on, and the words are printed as they are spoken unless `--no-live`. Ctrl-C stops it; the pieces still waiting are uploaded first. The recording is named once it is transcribed |
| `teanode recording watch RECORDING-ID` | print a recording's words as they are transcribed, the live words first and the speakers' passages as each piece is done, until its transcription is finished |
| `teanode recording timeline --recording-id ID` | the whole transcript in time order, the latest completed version of each piece of the recording. While a recording is still being captured the phone uploads it in pieces of up to a minute, and each piece shows up here once it is transcribed. A passage from a speaker nobody has named yet carries a label such as `Speaker 1` (`temporarySpeakerLabel`), the same label in every piece of that recording, and the view lists these speakers under `temporarySpeakers`. While a capture goes on the server asks the transcription model whether a new label is a voice that already has one (a request is sent only four samples, so a fifth voice comes back under a fresh letter); when the model says it is, the passage reads as the earlier label, or its name, and carries its own label as `mergedTemporarySpeakerLabel` A sentence that crossed the cut between two pieces appears once, whole: the later piece's run records where it started reading (`handoffStartMS`) and the earlier piece's copy is left out |
| `teanode recording timeline --recording-id ID --unnamed-only` | only the passages whose speaker nobody has named yet, what is left to label: `segmentCount` is how many there are and `--segment-offset` pages through them. A passage given a name, or matched to a voice sample, leaves the list |
| `teanode recording temporary-speaker-name --recording-id ID --speaker-label "Speaker 1" --confirmed-name NAME` | name a speaker of a recording that is still being captured, by the label the timeline shows. The name applies to every piece heard so far and to the ones that follow, and a named speaker is recognized by voice in later recordings as any confirmed speaker is. It takes no generation, since the capture keeps going. `--me` says the speaker is you, and the name then defaults to your display name |
| `teanode recording speaker-separate --recording-id ID --speaker-label "Speaker 9"` | give a label that was merged into an earlier speaker's voice back to itself: the label is the passage's `mergedTemporarySpeakerLabel`. Its passages read as its own label again, and the names that naming the earlier speaker wrote for it are taken off; naming the earlier speaker meanwhile named both. It takes no generation, since the capture keeps going |
| `teanode recording speaker-confirm\|speaker-rename\|speaker-reset` | the same for a finished recording, by a passage or by a speaker of one transcription request; these check `--expected-source-generation` |
| `teanode recording speaker-undo` | takes a naming back whole, given any one of its corrections: every passage it named goes back to the speaker's label (`speaker-reset` takes back one passage); checks `--expected-source-generation` |

### teanode photo

Your library of photos and videos: the same operations as the dashboard's
Photos page and the agent's `photo` tool, and named after them. It needs the
Photos service switched on and the `photos:use` permission. Each command that
prints data takes `--json`.

| Command | What it does |
| --- | --- |
| `teanode photo list [--favorites] [--trash] [--search WORDS] [--after DATE] [--before DATE] [--near LATITUDE,LONGITUDE,KILOMETERS] [--kind picture\|video\|livePhoto] [--album ID] [--face-group ID] [--contact ID] [--person USERNAME] [--limit N] [--continue-after ID]` | the library, newest first (with `--trash`, the photos newest moved to the trash first): id, date (when it was taken, on the clock where the camera was, by the photo's own offset: a photo with a date always has one once it is processed, and a photo not processed yet is shown on UTC), kind, place, size, length, state and file name. `--search` finds photos by words, best match first (at most 500): the place, the file name, the camera, the titles of the albums a photo is in and, for a photo that has been described, its description and the text in the picture; a photo nobody described is found only by the rest. `--near 10.5,20.25,2` keeps the photos taken within 2 kilometers of that point (latitude from -90 to 90, longitude from -180 to 180, a distance above 0). With `--search` and `--after` or `--before` it also says on standard error, when there are some, how many photos in those dates have no description yet. A date is `2025-07-12` (that day in your zone) or an RFC 3339 time. A full page says on standard error how many photos match, and the id to continue after. `--face-group` keeps the photos with a face in one group of faces (the ids are in `people list`) and `--contact` the photos with a face in any group that contact names, so the photos of a person are found by whichever you have; `--person <username>` the photos of the contact you linked to that person in your circles (`teanode contact link`), nothing when there is none |
| `teanode photo read ID` | everything known about a photo: its date (with the offset where it was taken, when known), place, camera, size, files, the albums it is in, how its previews are going, and its description with the text in the picture (or that none was written yet, or is on its way). Its place reads the name of your own Location place when the photo was taken inside one, else the street address when the server looks addresses up, else the city; `--json` adds `locationPlace` and `addressText`. Each face found in it is a line `person`: the contact that names it, or that nobody has named its group yet, with the group and face ids to name or correct it |
| `teanode photo describe ID [--again]` | have your assistant look at a photo and write what is in it, so that a search finds it by that. It answers at once and the description arrives a few seconds later (`read` has it). A photo that has a description is left alone unless `--again`. It needs the assistant to be on and a model that reads pictures; nothing describes photos unasked except the assistant when it looks at one |
| `teanode photo upload FILE-OR-FOLDER...` | add pictures and videos. A folder is walked, leaving out names that start with a dot and files that are not pictures or videos; a file named on the command line is always sent. Each file is hashed (SHA-256, which is the name the library knows the photo by), the library is asked which of the first 200 it already has, and only the rest are sent. One line a file says `uploaded`, `already there` or `failed`, then a summary such as `12 uploaded, 3 already there, 1 failed`; the command fails if any file did |
| `teanode photo download ID [--file original\|edited\|liveVideo] [--output PATH] [--force]` | save one of a photo's files, under its own name here by default, or at the path given, or in the folder given. It never replaces a file that is there unless `--force` says so, and a file that arrives short is not kept |
| `teanode photo favorite ID [--off]` | make a photo a favorite, or take it back |
| `teanode photo set-date ID TIME` | correct when a photo was taken. `TIME` is an RFC 3339 time whose offset is the offset of the place the photo was taken (`2025-07-12T14:30:00+09:00`, or `Z` for a place on UTC); the photo is shown on that clock from then on, as the dashboard's change of date does. A time with no offset, or a day alone, is refused, because it would be read on a clock that is not where the photo was taken. Processing never undoes a date you set |
| `teanode photo search-places WORDS [--near LATITUDE,LONGITUDE] [--limit N]` | find cities by name in the table that names every photo, to pick where photos were taken: place, latitude and longitude, and with `--near` the kilometers from that point. Case, accents and punctuation do not matter. A whole name comes first, then names that start with the words, then names with a word that starts with them, then the words anywhere in "City, Country"; within one kind the nearest to `--near` comes first, otherwise the largest. `--limit` is 1 to 50 (10 by default). Two numbers (`35.0116, 135.7681`) answer that one position, named after the nearest city. Street addresses are not searched |
| `teanode photo set-location ID... (--place WORDS \| --at LATITUDE,LONGITUDE)` | say where photos were taken, at most 500 at once: `--place` uses the best match for the words (`search-places` shows the choices), `--at` a position such as `10.5,20.25`; give exactly one. The photos are named after the nearest city, their date is not changed, and a location you set is not replaced when the file is read again. It says how many photos changed; photos that are not yours or are in the Trash are left alone |
| `teanode photo remove-location ID...` | remove the location from photos, at most 500 at once, after asking (`--force` skips the question). TeaNode keeps no copy of it, and it stays removed when the file is read again; the file itself is not changed, so a download of the original, or a copy already shared, still has what the camera wrote. Setting a location puts one back |
| `teanode photo trash ID...` / `restore ID...` | move photos to the Trash and restore them from it; the Trash is emptied after the days `library` shows |
| `teanode photo retry ID...` | make the thumbnails of photos again (the dashboard's and the iPhone's "Make thumbnails again"), at most 500 at once. A photo whose thumbnails failed is put back in line with a fresh count of attempts and no wait; a photo that is ready is done again, to replace thumbnails that look wrong. A photo that is waiting already, in the trash or not yours is left alone, and the command says how many it queued. The server tries a photo up to five times, waiting 1, 5, 15 and then 60 minutes after a failure that is the file's; a failure of the server's own store or database is not counted. `read` has how it is going |
| `teanode photo delete ID...` / `empty-trash` | delete forever the photos in the Trash, or all of them; both ask first, or take `--force`. The photos leave the trash at once and their files are deleted by the server within minutes. `delete` says how many went, and when some did not, that the others were not in the Trash and were left alone |
| `teanode photo library` | how many photos there are and how many of them are videos (`photos: 120 (4 videos)`: a photo is any item, a picture, a Live Photo or a video, so the videos are among the photos and not a second count), the space used (with the account's limit, or No limit) the largest file and whether faces are being found (on, off by you, or not available and why), in the words of the dashboard's Photos page |
| `teanode photo album list` | the albums you reach, the most recently changed first: yours, your circles' and those shared with you. Each row has its id, title, how many photos are in it, the days they span, whose it is (`you`, a person's name, or `circle` and the circle's name) and your level (`owner`, `contribute` or `view`). A photo in the trash is not counted until it is restored |
| `teanode photo album read ID [--limit N] [--continue-after ID]` | an album and its photos, newest first, in the same columns as `photo list`, with an `owner` column when other people's photos are in it; a full page says on standard error how to continue. A photo that is not yours comes without where it was taken unless the share includes places |
| `teanode photo album create TITLE [PHOTO-ID...] [--circle CIRCLE]` | make an album, empty or with photos in it (at most 500 at once). A title is from 1 to 255 characters once trimmed. `--circle` (an id or a name from `teanode circle list`) makes it the album of a circle you are in: every member adds photos of their own to it, the photos stay theirs, and the circle's admins rename and delete it |
| `teanode photo album rename ID TITLE` | give an album another title |
| `teanode photo album delete ID` | delete an album. Its photos stay in your library. It asks first, or takes `--force` |
| `teanode photo album add ID PHOTO-ID...` / `remove ID PHOTO-ID...` | put photos in an album, or take them out; they stay in your library either way. `add` leaves alone photos already in the album and says how many it added; a photo that is not yours is not found, and then nothing is added |
| `teanode photo album share ID (--person USERNAME \| --circle CIRCLE) [--contribute] [--with-location]` | share one of your albums with somebody who shares a circle with you (`teanode circle people`), or with everyone in a circle you are in. They view it and save its photos; `--contribute` also lets them add photos of their own and take out the ones they added. Without `--with-location` they see no place and get the large preview where they download a photo, a picture made afresh that carries none; with it they see where the photos were taken and get the original files. Sharing again with the same person or circle changes what they have. A circle's own album is not shared further: every member has it |
| `teanode photo album share-update ID SHARE-ID [--level view\|contribute] [--with-location \| --without-location]` | change one share of an album (`album shares` gives the share id), as the dashboard's sheet of shares does. Only what you name changes: a setting you leave out stays as the share has it, so `--level view` stops somebody adding photos and leaves them seeing where the photos were taken. Name at least one of the three. A change that would leave the share as it is sends nothing and says `nothing changed`. The agent's tool calls it `album-share-update` |
| `teanode photo album shares ID` / `unshare SHARE-ID` | who an album is shared with, at what level and whether places are included, with the id `share-update` and `unshare` take. Taking a share back takes with it the photos its people put in the album. Both are for the album's owner, or for a circle's album an admin of the circle |
| `teanode photo save ID` / `save --attachment ATTACHMENT-ID` | `save ID` keeps a photo of a shared album, or a circle's, in your own library. `save --attachment` keeps a file of a conversation with your assistant as a photo (the attachment's id is in `teanode conversation show --json`), as the dashboard's Keep as a photo does; the photo is named by the digest of the file, so keeping it twice finds the photo that is there. Give an id or `--attachment`, not both. The copy of a shared photo is yours, counts against your space and is processed like any new photo. Where the share includes places it is made over the same bytes and stores nothing new; otherwise it is the large preview, which is all you are given. Saving the same photo twice finds the copy that is there |
| `teanode photo album cover ID PHOTO-ID` | choose which photo of the album stands for it. Until you do, and while that photo is in the trash, the album is shown by its newest photo |
| `teanode photo album suggest [--after DATE] [--before DATE]` | trips and events among the photos that are in no album, newest first: a run of at least 12 photos with no gap of more than 6 hours between them. Each shows its days, the places it covers (the one with most photos first), a proposed title in English and how many photos it has. Nothing is created: make the album with `album create`, giving it the photos `photo list --after --before` finds between the exact times `--json` gives |
| `teanode photo people list [--hidden]` | the people found in your photos: id, name, faces, photos and the contact that names the group. The group your own contact names (the card marked as you) has "(you)" after its name, a group whose contact you linked to someone in your circles has "[in your circles as <username>]", and `--json` carries `isOwnContact`, `accountUserId` and `account` on every group. Groups a contact names come first, by name, and then the groups of at least three faces (faces, not photos) nobody has named, the largest first; the groups you hid are left out unless `--hidden`. Faces are found by a program on the server as photos are processed, and a face has to be sure and large enough to be grouped, so a person can take a while to appear |
| `teanode photo people read GROUP-ID [--limit N] [--continue-after ID]` | one person and their photos, newest first, in the same columns as `photo list` |
| `teanode photo people name GROUP-ID CONTACT-ID` | say who a group of faces is, by choosing a contact from your address books (`teanode contact list` gives the ids; `teanode contact add` makes one first). It needs the Contacts service and `contacts:use`, and says to turn on Contacts when they are not there. A contact is the only way to name anybody, and `photo list --contact` then finds their photos in every group the contact names. A contact names one person: naming a group with a contact that already names another of your groups merges the two into the group already named (it keeps its cover) and prints `merged with <name>`; groups named with one contact before that was so were merged once by a migration. A contact with no picture is given one cut from the group's best face (the surest of the larger ones, from your own photos) and the command prints `used a face as their picture`; a picture a contact has is never replaced, and `teanode contact remove-picture` takes it off |
| `teanode photo people unname GROUP-ID` | take the contact off a group; its faces stay grouped |
| `teanode photo people merge GROUP-ID GROUP-ID...` | put groups into the first, for one person found as two (at most 50). The first keeps its own contact, or takes the contact of the first of the others that has one, and the others are deleted. Naming does this for you when the contact already names a group |
| `teanode photo people remove FACE-ID` | "this is not them": take a face out of its group into a group of its own, which stays as you put it and is not grouped again. The face ids are in `photo read` |
| `teanode photo people hide GROUP-ID` / `unhide GROUP-ID` | leave a group out of the list, or list it again. A hidden group goes on taking the faces of its person, so they stay hidden |
| `teanode photo faces on` / `off` | switch finding faces in your photos on (the default) or off. Off deletes every face and group found so far, so it asks first, or takes `--force`; on has the photos read again, a few at a time |

Uploads speak the server's resumable upload endpoint, in parts of 8 MiB: a
photo that fits in one part is one request, and a larger one starts the
upload, then appends each part at the offset the server has. A part is tried
three times, asking the server how much it has stored (`HEAD`) after each
failure, so a dropped connection costs at most the part in flight and a
lost answer is not sent twice. When every byte is there and the server is
still reading the parts back to check the file and store it, which for a large
file takes minutes, the command does not send the last part again: it asks
(`HEAD`) every few seconds, and then every thirty, until the server says the
upload is complete (and which photo it made, or the photo is found by its
name), up to 35 minutes. Each time it also sends the end of the upload again:
an append with no bytes in it, at the offset the file ends at, marked as the
last. If the server's own attempt to complete the upload failed, this is what
completes it, so the command does not wait out the 35 minutes for nothing; if
the server is still busy completing it, it answers 409 and the command goes on
waiting. An upload the server lost starts over. The command
keeps nothing on disk. Run again
after it was interrupted, it starts the interrupted file over (the library
does not list a half-sent upload), and the server deletes the part it was left
with after a week of silence.

### teanode hook

What Claude Code and Codex run, so a coding session on this computer sees
what your agent knows about the checkout it is in. `teanode hook install
claude-code` adds the hooks to `~/.claude/settings.json`, and `teanode hook
install codex` to `~/.codex/hooks.json`, keeping every other hook in the
file; `uninstall` takes out only TeaNode's. The hooks run this program as it
was installed, naming the saved profile it signed in with (a server reached
with `--url` alone is refused: sign in first, so the hook does not depend on
a token in the tool's environment), and the `--computer` it was given. A
hook's own exit status is never passed on: one that fails shows nothing.

| Event | What the session gets |
| --- | --- |
| a session starts, resumes or is compacted | the checkout's page and its liveliest facts, and the last session held there: its title, the person's last requests and the start of its last answer |
| a prompt is typed | what the prompt recalls, and the lessons it brings up, kept to the checkout's project, the pages under it and linked to it, and lessons (`--everywhere` on install for all of memory); a page shown in the last five prompts is not shown again, and a prompt of fewer than three words, or a slash command, recalls nothing |
| an answer ends, the session is compacted or ends | the computer's `claude-code` or `codex` source is asked to read again now, so the session is searchable within a minute and the next one knows where this stopped |

A checkout this computer's sources never profiled is matched by its git
remote, not its path, and the session is told where and at which commit
memory read the project. A hook that cannot reach the server shows nothing
and never stops the tool;
what went wrong is in `hook.log` under the user's cache directory
(`~/.cache/teanode/hooks` on Linux). Codex runs hooks only with `hooks = true`
under `[features]` in `~/.codex/config.toml`, and asks you to trust a new hook
before it first runs. `teanode agent memory checkout` prints what a session
would be shown without starting one, and `teanode agent memory capture` asks for
a computer's sessions to be read now without waiting for a hook.

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
| `teanode finance rename-statement-account [--account-number DIGITS] <finance-account-id> [name]` | gives an account of imported statements your own name, the last digits of its number, or both, which later imports keep; its asset takes the name too unless you renamed it. `--account-number` takes 4 to 8 digits, for statements that show a word or nothing in place of the number; it is shown as the account's number and matched against screenshots' digits, and `--account-number ""` takes yours back so the statement's shows again |
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
| `teanode finance annotate-transaction <transaction-id> <annotation>` | writes what a transaction was for, or `--clear` takes it away; your agent replaces yours only when you ask it to in a conversation |
| `teanode finance receipts` | your receipts, the newest purchase first and those that print no day last, with each one's check and what it is matched to; `--from`, `--to`, `--since`, `--month`, `--finance-transaction`, `--is-unmatched`, `--is-undated` (only those that print no day; not with a range), `--limit`, and `--offset` or `--after` for the next page, a note saying which rows of how many are shown |
| `teanode finance read-tax-document <file>...` | hands paystubs or W-2s (PDFs or photos) to your agent, which reads each in the background and records it as whichever it is; only the last four digits of your taxpayer number are kept, and the file's text is never stored; `--mailbox-item` reads each PDF and picture a message carries instead |
| `teanode finance paystubs\|paystub\|record-paystub\|delete-paystub` | your paystubs, the latest pay date first, with each one's check (adds up, or off by how much); `--tax-year` or `--from`, `--to`, `--since`, `--month`; `paystub <id>` reads one line by line; `record-paystub <file.json>` records one you read yourself; `delete-paystub` deletes one with its file |
| `teanode finance tax-forms\|tax-form\|record-tax-form\|delete-tax-form` | your W-2s, the latest year first, with whether each agrees with itself (Social Security and Medicare against their wages); `tax-form <id>` reads one box by box with those checks |
| `teanode finance tax-year-setting\|set-tax-year-setting` | what a year's estimate takes that no document says: children, dependent care paid, other household income, charitable gifts, property tax, state tax paid besides withholding, and gains realized so far (`--short-term-gain`, `--long-term-gain`, `--clear-gains`) |
| `teanode finance tax-document-address\|regenerate-tax-document-address` | the address to mail or forward paystubs and W-2s to, where each PDF or picture is read and recorded; `regenerate-tax-document-address` ends the old one |
| `teanode finance tax-year [year]`, `set-tax-filing-status <status>` | a tax year from your paystubs and W-2s, per person and employer: wages and withholding so far and by December 31, each W-2 box beside your paystubs, the tax owed on those wages beside what was withheld (paid so far, still to be withheld, owing or refunded at filing, remaining income) for the filing status you keep (`set-tax-filing-status`, `--clear` to forget it) or `--filing-status`, and what is worth knowing (a paystub missing between two others, a W-2 not yet in, Social Security withheld beyond the year's maximum); this year when no year is given |
| `teanode finance budgets\|set-budget\|budget-status\|spending-by-day\|cash-flow` | monthly budgets per spending category, and on an income category the income you expect each month (`budgets` says which kind each is); `budget-status` is this month against each budget, with where the month is heading and its pace (under, on track, at risk, over), then each income category against what was expected by today (behind, on track, ahead), and with `--year 2026` a calendar year instead, each month at the budget it had then, with the budget to date, the months budgeted and where the year is heading; spending day by day against last month; income and spending by month |
| `teanode finance saving-summary` | the month's saving in the reporting currency: income budgets less spending budgets, against income less spending so far and where the month is heading, with the difference and its pace (behind, on track, ahead); `--month` for another month, `--year` for a calendar year (the year to date for this one), counting only the months with a budget and saying which, or with no budget that year its income, spending and what was left, `--currency` to convert into another currency |
| `teanode finance savings-targets\|create-savings-target\|update-savings-target\|close-savings-target` | amounts to save by a day, with what they need a month from now on, measured by `--measure cash_flow` (money not spent, the default), `net_worth` (net worth gained since `--started-on`, recorded for you unless `--starting-amount` says otherwise) or `asset_value` (what `--finance-account` and `--asset` are worth; a whole finance account counts its cash and every holding, those bought later too) |

### teanode computer

Your own computer, attached to your agent. While the program runs, the
agent has two more tools — `shell`, which runs a command here, and
`filesystem`, which reads, edits, writes, copies, lists, searches and greps your files —
as you, anywhere on the machine, the way a terminal of yours would. A
scheduled run, a goal and the nightly run use them too, with nobody
watching; what would ask you first is refused then. The runs that read mail
from strangers (sorting, drafting, research) only read and never see it. A command runs as you
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
| `teanode computer herdr list [--computer NAME]` | the Claude Code and Codex sessions in herdr on your computers: each pane's name as herdr shows it (the workspace, the tab when there are several, and the agent when a tab holds more than one, such as `website › review › Codex`), which every other command takes as `<pane>` as well as its id, then its state (`idle`, `working`, `asking`, `unknown`, as the program on the computer decides it), the directory and the title. Sessions are grouped under a line for each workspace and computer, in workspace order and tab order, and the tab herdr shows on the computer's screen is marked `(on screen)`. Each question waiting with its fingerprint and numbered options |
| `teanode computer herdr read <pane> [--computer NAME] [--turns N]` | a session's last turns from its history file, 10 by default and 100 at most |
| `teanode computer herdr screen <pane> [--computer NAME] [--lines N]` | what the pane shows now, or its last `--lines` lines |
| `teanode computer herdr send <pane> <text> [--attach FILE]... [--computer NAME]` | type the text into the pane and press enter; a session at work reads it as a message in its turn. `--attach` hands it a file (a picture, a document): uploaded, written on the session's computer, and its path typed after the text; repeatable, and enough without text. Refused while the session asks a question |
| `teanode computer herdr wait <pane> [--computer NAME] [--seconds N]` | wait for the session to stop working, 30 seconds by default and 600 at most |
| `teanode computer herdr answer <pane> --fingerprint F --option N [--option N] [--text TEXT]` | answer the question the session waits on, with the options' numbers, or `--text` for the option that takes text. Refused when the question is no longer the one with that fingerprint. Without `--fingerprint` it prints the question and its fingerprint |
| `teanode computer herdr watch <pane> --conversation ID` | wake that conversation when the session next finishes its turn |
| `teanode computer herdr open [claude|codex] [directory] [--pane PANE] [--computer NAME] [--name NAME] [--skip-permissions]` | start Claude Code or Codex in a new herdr pane in that directory; both are needed unless `--pane` is given, which opens beside that pane (a new tab in its workspace, in its directory and with its coding agent unless you name others) (`--skip-permissions` starts Claude Code with `--dangerously-skip-permissions` and Codex with `--yolo`): a new tab of the workspace named after the directory, or a new workspace. One that stops at a question as it starts (whether to trust the folder) is open and asking it |
| `teanode computer herdr focus <pane> [--computer NAME]` | bring the session's tab forward in herdr on its computer, so whoever sits there sees it |
| `teanode computer herdr close <pane> [--computer NAME]` | end the session's coding agent and close its pane; refused while it works. What it said stays in its history |
| `teanode computer herdr setup [--computer NAME] [--remove]` | put TeaNode's reporting hooks into Claude Code's settings on that computer, beside the ones there, or take them out |

The operator can keep computers off for the whole server with
`agent.features.computer`.
