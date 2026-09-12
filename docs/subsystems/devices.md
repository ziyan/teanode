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
code, and whether either was cut. `filesystem` reads, writes, appends, edits by
exact text, lists, copies, moves, deletes, makes directories, globs for files,
and greps.

Bounds it applies itself:

| | |
| --- | --- |
| command timeout | 120s, 600s at most |
| output kept per stream | 256 KiB |
| largest file read | 4 MiB |
| largest file fetched whole | 32 MiB |
| requests at once | 4 |

A fifth request is refused rather than queued. A command that starts something
long is killed as a process group, so a shell that spawned a server does not
leave it holding the pipes.

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
reach another machine, change what runs on its own, write over storage, change
the disks, change accounts, redirect into a whole path, or change the system's
settings. Anything else runs.

For the filesystem, reading and searching are silent, deleting and moving always
ask, and writing asks when the path is one of the shapes that change what runs
or who may get in: shell startup files, `.ssh`, `.gnupg`, autostart and service
directories, `/etc`, git hooks, the password files.

A run with nobody present cannot reach a computer at all, so the card is never
the thing standing between a scheduled run and the machine.

**The rule is the server's alone.** The program runs what it is sent. An
attached computer trusts its server the way a terminal trusts the person at it.

## The attached tab

A Chrome extension the person installs. They sign in through the same
authorization page the command line uses, press the button on a tab, and that
tab becomes something the agent can drive, carrying their session.

The agent can navigate, take a snapshot of the page as an interactive tree with
numbered references, screenshot, click, hover, select, type, press a key,
scroll, wait, go back, evaluate an expression, fetch from the page's own origin,
read the page's local storage, and open, list, switch and close tabs. Tabs it
opens sit in a group named after the server, on the person's screen, where they
can see them.

It can also speak the **DevTools protocol** to that tab. A page script can be
told to click and type, but what it dispatches is not a real event and a site
can tell; the protocol dispatches input the way the person's own mouse and
keyboard do, and it is the only way to see what a page asks the network for.
`cdp` sends a method — `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`,
`Network.enable`, anything the protocol has — `cdp_events` reads back what the
page has done since, and `cdp_stop` lets go. While it is attached Chrome shows
its own bar saying so, which is the person's sign that it is happening; it is
let go when the tab closes, when the socket does, or after ten quiet minutes.

It is the person's own session, so the agent acts as them: it can fill in a
password or a card number, because an assistant that cannot is not much of
one. What stands between it and an act they cannot undo is the confirmation
card, which they can widen to every browser call by putting `browser` in their
own confirm list. A password already on the page is not read back into the
conversation, which is a different question from typing one in.

A run with nobody present never touches the tab.

## The headless browser

When the operator configures one, the server drives a Chrome beside it over the
debugging protocol. Each turn gets its own isolated context, discarded when the
turn ends, with a cap on how many exist at once.

The difference that matters is the guard. Every request the page makes is
paused and checked against the same address rule that guards fetching a page,
so a page cannot reach the server's own network; downloads are denied outright;
and a context that cannot be guarded is not opened at all.

A run with nobody present may only read: navigate, snapshot, screenshot, click,
hover, select, scroll, wait, back, and the tab actions. Typing, pressing and
evaluating need somebody there.

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
- **Fetching and reading storage are not in the tool's published action list**,
  so the model has no documented way to find them.
- A computer's fifth concurrent request fails rather than waits, so one long
  command can fail other calls in the same round.
