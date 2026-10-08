# Devices: a computer and a browser tab

Two things a person can attach to their agent, and the headless browser the
operator can run instead of the second one.

`internal/computer/`, `internal/agent/device.go`, `computer.go`, `tab.go`,
`internal/browser/`, `web/extension/`,
`internal/api/v1api/apigraph/agent_computer.go`, `agent_tab.go`.

## The shape both share

A device holds a websocket open to the server and waits. The server sends
`act` with a number and an action; the device answers `result` with the same
number. Requests in flight are kept in a map, each with its own channel; an
answer, a timeout, or the device going away closes exactly one of them. Sixty
seconds is the default wait, and every error names the device: "the computer
work-laptop did not answer within 2m0s".

A device is attached per person, not per conversation. Attaching a second
computer of the same name replaces the first, and whoever was waiting on it is
told it was detached rather than given the new one's answer.

Every answer from a device is marked untrusted. It is data the model read, never
words the person said.

## The computer

`teanode computer start` runs a small program on the person's own machine. It
signs in with their own token, not the server's, and the command refuses the
local profile for exactly that reason.

It offers two actions. `shell` runs a command and returns both streams, an exit
code, and whether either was cut. What is kept of a stream is its first 64 KiB
and its last 192 KiB, with a line saying how much was left out between them. `filesystem` reads, writes, appends, edits by
exact text, lists, says what one file is, copies, moves, deletes, makes
directories, globs for files, and greps.

Two of its actions carry whole files rather than text, in either direction, and
neither passes the bytes through the model. `fetch` sends a file to the server,
which is how `share_file` hands the person something off their own machine.
`put` writes a file of the conversation onto the machine: the model names an
attachment and a path, the server reads the bytes out of storage and sends them
across. That is what makes a document nobody here can parse useful — a PDF or a
spreadsheet lands under the person's home, `shell` runs whatever they have that
reads it, and `share_file` brings the answer back.

It can also be where an OAuth authorization comes back to, for a connected
server whose service sends one only to a loopback address. Asked with
`authorization_forward`, it listens on the loopback interface, gives out a
`localhost` address, and sends the browser that comes back on to the dashboard
address the server named, carrying only the authorization's own parameters. The listener outlives the request and the
connection, so a network that drops while the person signs in loses nothing,
and closes after the redirect or fifteen minutes; four wait at most.

Bounds it applies itself:

| | |
| --- | --- |
| command timeout | 120s, 600s at most |
| output kept per stream | 256 KiB |
| largest file read | 4 MiB |
| largest file fetched whole | 32 MiB |

There is no bound on how many requests, sessions or background commands run
at once: every one is answered, and the person's machine decides how much it
can take.

A command that is killed is killed as a process group, so a shell that
spawned a server does not leave it holding the pipes.

**There is no directory it is confined to.** `~` and a relative path are
resolved against the person's home because that is convenient, and an absolute
path anywhere on the machine is honoured. It is their machine, and the agent is
them on it.

### What asks first

The guard is a card, not a boundary. Before a command is sent, it is read as
text and classified. Grave shapes — removing the root or a home directory,
formatting or writing over a disk, a fork bomb — ask with the reason spelled
out. So do commands that remove or move files, run as another user, change
permissions or ownership, stop processes, turn the machine off, install or
remove software, push or rewrite a git history, pipe a download into a shell,
reach another machine, change what runs on its own, remove or stop containers,
write over storage, change the disks, change accounts, redirect into a whole
path, or change the system's settings. Anything else runs.

For the filesystem, reading and searching are silent, deleting and moving always
ask, and writing — `write`, `append`, `edit`, `put` — asks when the path is one
of the shapes that change what runs
or who may get in: shell startup files, `.ssh`, `.gnupg`, autostart and service
directories, `/etc`, git hooks, the password files.

Any run reaches the computer, with or without the person there: a scheduled
turn, a goal, a night. What stands between such a run and the machine is not a
card, which cannot be shown to an empty room, but the refusal a call gets when
it needs the person's word: anything the rule above classifies as asking is
refused with nobody present, and the run is told to say what it would have
done, unless the person allowed that kind of action for when they are not
there (speaking for them, money, what cannot be undone, giving access, or the
tools on their own list; never the operator's list, and never in a run that
reads mail from strangers).
`docs/decisions/20261007-the-person-chooses-what-the-agent-does-alone.md` says
how. A run that has a conversation to wake may leave a command running in the
background and is woken there when it ends; a run that is a transcript of its
own, such as a night's, runs commands in the foreground.
`docs/decisions/20261006-the-agent-uses-the-persons-devices-whoever-is-present.md`
says why.

**The rule is the server's alone.** The program runs what it is sent. An
attached computer trusts its server the way a terminal trusts the person at it.

### Background commands

A command still running when its call's wait runs out is not killed: it goes
on in the background, and the answer says so with its id and what it printed
so far. The agent can also start one there on purpose (a build, a server, a
loop that waits for something), and `shell` reads one's progress, lists them
and stops one. Any number may run; the program remembers sixty-four,
forgetting the oldest that ended first. Each is stopped after twenty-four
hours, and an ended one can be read for a day. The person sees them, with their output, in the drawer and on
the agent page, and can stop one there too.

The program holds them, not the connection: a network that drops or a server
that restarts does not end a build, and stopping the program does. When one
ends, the program says so without being asked, carrying the origin the server
gave it: the agent and the conversation that started it. It says so again
after every reconnect until the server acknowledges it, so nothing about them
is kept in the database.

An ending wakes that conversation: a turn opens with a message marked
`[background command]`, saying how it ended and carrying the end of its output
as untrusted data, and the drawer draws that line muted, as it does a goal's
check-in. Endings that arrive within two seconds of each other are one turn,
and the ending is acknowledged when that turn is over. A run with nobody
present never leaves a command running, and a conversation takes at most
twenty woken turns before the person writes there again. Why, and what it
costs:
`docs/decisions/20260923-a-background-command-wakes-the-conversation-that-started-it.md`.
A survey or a subagent the agent did not wait for wakes the conversation the
same way, under the same count (`the-ask-loop.md`).

A program that predates background commands does not name them in its hello,
and the server asks it for none: its commands are killed at the end of their
wait, as before.

## A program held open

Everything above is one request to one answer, which is all the shell and the
filesystem needed: a command runs and returns, a file is read and that is
that. A program that stays open needs two things that shape does not have —
output arriving when the program feels like it, and input written to
something started earlier. So a session has an identifier of its own, and what
is said about it arrives unasked and is filed under that name. `pending` is
one entry per request in flight; sessions outlive the request that made them.

Two things sit on that.

**A terminal the agent drives.** `terminal` opens a pty on the computer, runs a
program in it (the person's shell if none is named), types, presses keys by
name, and reads **the screen** — what the program is showing at this moment,
with the cursor — never the bytes that drew it. A program that redraws says
the same thing a hundred times over in escape sequences; a model handed all of
that learns nothing, and a model handed the screen learns what a person
looking at it would. The screen is kept on the computer, so none of the
redrawing crosses the network and reading it is one request however busy the
program has been. `wait` watches the screen until it stops changing, so a
model need not guess how long a program takes to answer. Opening a terminal
is judged the way `shell` judges a command line -- the program with its
arguments, or the script a shell is handed with `-c` -- so a listing or a
build opens without a card, and what the policy would ask about still asks.
A bare shell asks, because the keys after it could type anything and the keys
never ask: the program the person said yes to is what they go to.

**A connected server that runs there.** A server declared with `location:
computer` under `agent.mcp.servers` runs its command on the person's attached
computer, as them, and its standard input and output reach the server through
a session. The protocol is unchanged; only where the process is. Such a server
is offered only while a computer is attached, never to a run with nobody
present, and cannot be marked headless.

The bounds: 256KB of unread output kept per session, no bound on how many
are open, thirty minutes idle before the server closes one, and a session
belongs to the connection that opened it — when the person stops the program
or the network goes, the processes it started are killed, and a reader is told
the session ended rather than left waiting.

## The attached terminal

`teanode terminal` attaches the terminal the person is sitting in. Their shell
runs in a pty and they use it as they would have anyway; their agent can read
that same screen and type into it, with `terminal`'s `attached` action. It is
one shell with two people at it, and either can take over.

It is built on what a launched terminal already is: the program connects as a
computer with one pty session already open and says so in its hello, and the
server adopts that session rather than starting one. The difference between a
terminal the agent opened and one the person is sitting in is who else is
looking, which is a fact about the session, not a kind of connection. Because
somebody is watching, the prompt says so while one is attached: the agent is
told to say what it is about to type before it types it, and never to close
the terminal, since it is theirs.

## Herdr

Herdr is a terminal workspace manager for coding agents. When the person runs
it, the program on each of their computers watches it (`internal/computer/herdr.go`)
and the agent works in the Claude Code and Codex sessions in its panes, beside
them: the `herdr` tool, the Herdr sessions card on the agent page, and
`teanode computer herdr` do the same things through the same actions, listed
in `HerdrActions` and checked by `TestHerdrParity`. The decision is
`docs/decisions/20261008-the-agent-works-in-the-persons-herdr-sessions.md`.

A pane is shown by the name the person finds it under in herdr: the
workspace's label, the tab's when the workspace has several, and the agent's
name, or which agent it is, when a tab holds more than one. Every action takes
that name or the pane's id.

The program speaks to herdr's socket (`~/.config/herdr/herdr.sock`) and looks
at every pane every three seconds. It decides each session's state itself, in
this order: a question recognized on the screen (or one Codex asked in its
history file and nobody has answered), TeaNode's own hooks when the person put
them in, Codex's history file, the screen's "esc to interrupt", and herdr's
state last. A session is `asking` exactly when a question was recognized.

When a question comes or goes, or a watched session finishes, the program
says so unasked (`type: herdr`), and says it again after every reconnect until
the server acknowledges it, as background commands' endings are. A question is
written into the person's main conversation under `[herdr question]`, which
opens the drawer, shows its options as buttons, and goes on to their chat
apps; a finished watch wakes the conversation that watched, under
`[herdr session]`, through the same waker and bounds as background commands.

An answer is pressed as the person would: the option's number for a form, the
numbers then right for one that takes several, the number then the text then
enter for one that takes text, and the answer typed as the next message for a
question Codex asked in its history. It carries the question's fingerprint and
is refused when the question on screen is no longer that one.

`setup` puts a script into `~/.local/share/teanode/` and registers it in
`~/.claude/settings.json` for six events, beside what is there; it appends
each event to `~/.local/state/teanode/herdr-events.jsonl` and decides
nothing. A copy of the settings is kept as `settings.json.before-teanode` the
first time. Codex has no hooks here: its history file says the same as it
happens.

## The attached tab

A Chrome extension the person installs. They sign in through the same
authorization page the command line uses, press the button on a tab, and that
tab becomes something the agent can drive, carrying their session.

The agent can navigate, take a snapshot of the page as an interactive tree with
numbered references, screenshot, click, hover, select, type, press a key,
scroll, wait, go back, evaluate an expression, fetch from the page's own origin,
read the page's local storage, and open, list, switch and close tabs. Tabs it
opens sit in a group called TeaNode, on the person's screen, where they can see
them.

It can also speak the **DevTools protocol** to that tab. A page script can be
told to click and type, but what it dispatches is not a real event and a site
can tell; the protocol dispatches input the way the person's own mouse and
keyboard do, and it is the only way to see what a page asks the network for.
`cdp` sends a method — `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`,
`Network.enable` and the rest — `cdp_events` reads back what the page has done
since, and `cdp_stop` lets go. While it is attached Chrome shows its own bar
saying so, which is the person's sign that it is happening; it is let go when
the tab closes, when the person detaches, and when the connection drops.

A handful of methods are refused, not because the agent is not trusted with
the tab but because they are not about the tab: every site's cookies rather
than this one's, the browser's own settings, other targets, and holding or
rewriting the page's own requests. What the page itself holds stays reachable
through `storage`, `fetch` and `evaluate`.

It is the person's own session, so the agent acts as them: it can fill in a
password or a card number, because an assistant that cannot is not much of
one. What stands between it and an act they cannot undo is the confirmation
card, which they can widen to every browser call by putting `browser` in their
own confirm list. A password already on the page is not read back into the
conversation, which is a different question from typing one in.

A run with nobody present reads the tab but does not act in it: a click or
a keystroke there acts as the person, signed in, and no card can be shown to
an empty room to ask first.

## The headless browser

When the operator configures one, the server drives a Chrome beside it over the
debugging protocol. Each turn gets its own isolated context, discarded when the
turn ends, with a cap on how many exist at once.

The difference that matters is the guard. Every request the page makes is
paused and checked against the same address rule that guards fetching a page,
so a page cannot reach the server's own network; downloads are denied outright;
and a context that cannot be guarded is not opened at all.

A run with nobody present may only read: navigate, snapshot, screenshot, click,
hover, select, scroll, wait, back, and listing or switching tabs. Typing,
pressing, evaluating and closing a tab need somebody there.

When a tab is attached and somebody is present, a call that names no target goes
to the tab. They attached it to be used, and a call that forgets to say so
should not land in a browser signed in as nobody.

## Switches

| | |
| --- | --- |
| `agent.features.computer` | whether a computer may be attached at all |
| `agent.features.browser` | the headless browser and tab attachment together |
| `agent.browser.enabled` | whether a headless Chrome is used |
| `agent.browser.cdpEndpoint` | where it is |
| `agent.browser.attachTabs` | whether people may attach their own tab |
| `agent.browser.allowPrivateAddresses` | networks the page guard admits |
| `agent.browser.idleTimeout` | when an unused context is closed |
| `agent.browser.maxContexts` | how many exist at once |

The tool families `computer` and `browser` can be named in the operator's
disabled or confirm lists, and a person can add either to their own.

## Caveats

- **The shell is `/bin/sh -c`**, or `cmd /C` on Windows, not the person's login
  shell. Aliases and shell startup files do not apply.
- **Moving a file is a rename**, so it fails across filesystems.
- **A tab is attached per person**, so any conversation of theirs can drive it.
- **Reading local storage reads the current tab**, which after the agent opened
  one is a tab the agent chose rather than the one the person attached.
- A computer's fifth concurrent request fails rather than waits, so one long
  command can fail other calls in the same round.
- **A terminal is read as a screen**, so what scrolled off the top is gone. A
  program that prints a great deal is better run with its output to a file.
- **`teanode terminal` attaches under its own name** (`--name`, the host name by
  default); running it on a machine that also runs `teanode computer` under
  the same name replaces that attachment. Give one of them a name.
- **Herdr's own state can be wrong**; `herdr agent explain <pane>` says which
  of its rules decided it. The state shown here is the program's own, with
  herdr's beside it as `herdrAgentStatus`.
- **A question is recognized from the layouts the coding agents draw now.** A
  form drawn differently is not recognized, and the session shows idle or
  unknown with its screen still readable.
- **A restart of the program on the computer loses nothing herdr-wise.** A
  question still waiting keeps its fingerprint and is not told again, and a
  watch is kept and fires at the first look if its session finished while
  the program was down (`~/.local/state/teanode/herdr-appearances.json` and
  `herdr-watches.json`). What is lost is an event said but not yet
  acknowledged when it stopped.
- A server on the computer is as available as the computer is: gone when the
  person stops the program, back when they start it. With several computers
  attached it runs on the first by name; there is no way yet to say which.
