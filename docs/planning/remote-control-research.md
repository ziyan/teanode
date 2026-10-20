# Remote control of a person's computer: research

Research for a feature not yet planned: letting a person see and control their
own attached computer from the dashboard or the iPhone (a terminal, X11 apps,
a Chrome tab), letting the agent use X11 apps the way it uses a tab today, and
handing control back and forth between the two. Nothing here is built. When
the work starts, this becomes an ExecPlan.

The case that started it: the agent is driving an attached Chrome tab and gets
stuck on something only the person can do (a sign-in, a second factor, a
"prove you are human" page, a choice it should not make). Today the person has
to be at that computer. They should be able to do it from their phone, then
hand the tab back and let the agent carry on.

## What TeaNode has today

`docs/subsystems/devices.md` describes it in full. The parts this builds on:

- **A websocket per device.** `teanode computer start` and the Chrome extension
  hold a socket open to the server. The server sends `act`, the device answers
  `result`, and a session (a program held open) gets its own identifier with
  output arriving unasked.
- **Terminals.** The `terminal` tool opens a pty on the computer and reads its
  screen, kept on the computer so redraws never cross the network.
  `teanode terminal` attaches the shell the person is sitting in, and "either
  can take over". There is no way for the person to watch or type into one of
  these from the dashboard or the phone; the Coding page shows a herdr pane's
  screen by reading it every few seconds, not live.
- **The attached tab.** The extension already attaches `chrome.debugger` to the
  tab for the `cdp` action, so it can send any DevTools protocol method. Chrome
  shows its own bar while that is attached.
- **Questions that wait.** `ask_user` and approval cards are stored and survive
  the turn (`docs/planning/durable-interactions-execplan.md`), and herdr
  questions reach the main conversation and chat apps. A hand-off can be one
  more kind of interaction.

Nothing today shows a person a live picture of a screen, and the agent has no
tool for an X11 app.

## How a hosted sandbox service does it

instabox, a hosted service, rents Linux VMs to agents and people, each with
a terminal and a desktop in the browser and a desktop the agent can drive.
Looked at from inside one of its machines and from its web app's code on
2026-10-20, it is built entirely from standard open-source parts. What it does
is the shape we would want.

**The machine.** A Firecracker microVM with its own kernel (Debian userland),
on bare-metal cloud servers. Machines are copies of a booted template's memory
snapshot, so one starts in about a second; idle machines pause with memory and
processes kept. Not something TeaNode needs (the person brings their own
computer), but it is why the service can give everyone a desktop.

**The terminal.**

- The browser draws it with [xterm.js](https://github.com/xtermjs/xterm.js) (with its fit and
  WebGL add-ons) over one websocket:
  `/v1/machines/{id}/terminal?cols=…&rows=…&session=main`. Output arrives as
  binary frames written straight into xterm.js; small JSON text frames carry
  status, and the browser sends its size on open and on resize.
- In the machine, a daemon keeps named sessions alive the way tmux does, and
  each browser tab runs an `attach --name main` client. Closing the tab leaves
  the shell running; opening it again rejoins it.
- A root agent in the machine carries both the terminal and the desktop to the
  host. It listens on no network port, which points at the VM's host socket
  (vsock).
- A reconnect can say not to wake a paused machine (`wake=0`), and terminal use
  counts as activity, so a machine someone is typing in is never paused.

**The desktop.**

- In the machine: TigerVNC's `Xvnc` serving an Xfce session on display `:1` at
  1280x800, listening only on `127.0.0.1:5901` with no VNC password. Nothing
  outside reaches it except through the agent above, so the password would add
  nothing.
- In the browser: [noVNC](https://github.com/novnc/noVNC) (`core/rfb.js`) over a websocket,
  on a page of its own that the dashboard puts in an iframe.
- Viewer links: an API call makes a link to one machine's desktop, valid for
  60 seconds to a day, view-only by default. The token is in the link's
  fragment (`#token=…`), which a browser never sends with the page request, and
  the page passes it in a header. View-only is enforced by the server: keys,
  pointer and clipboard from such a viewer never reach the machine. Revoking
  closes open viewers within seconds.

**The agent on the desktop.** An MCP server,
[open-computer-use](https://github.com/iFurySt/open-codex-computer-use) (MIT,
native builds for Linux, macOS and Windows), with nine tools:

| Reads | Acts |
| --- | --- |
| `list_apps`, `get_app_state` | `click`, `perform_secondary_action`, `scroll`, `drag`, `type_text`, `press_key`, `set_value` |

`get_app_state` returns the window's accessibility tree, from AT-SPI (the
desktop installs `at-spi2-core` and turns toolkit accessibility on), together
with a screenshot. A trimmed example:

```
0 frame home - Files  Frame: {x: 0, y: 0, width: 394, height: 477}
  2 tool bar  Frame: {x: 0, y: 49, width: 394, height: 42}
    6 button Back  Secondary Actions: click  Frame: {x: 4, y: 53, width: 37, height: 35}
    12 button Home  Secondary Actions: click  Frame: {x: 115, y: 53, width: 37, height: 35}
```

The model acts on numbered elements rather than guessing pixels, and
`set_value` fills a field without typing. The screenshot covers apps whose
tree is empty. Underneath, input is `xdotool` and screenshots are `scrot`.
This is the same idea as our tab snapshot with numbered references, applied
to native apps.

**Taking over and handing back.** The service runs the MCP server behind a
small proxy. A person at the viewer can take control; while they hold it, the
agent's input tools (`click`, `type_text` and the rest) are answered with "A
human has taken control of the desktop; wait and retry after they hand back"
and never reach the server, while reading tools still pass. The lock is a file
the viewer renews while it is open, so a viewer that goes away without handing
back gives control back after a timeout. Every tool call is appended to a log
the dashboard shows as the agent's recent actions, with typed text recorded
only as its length.

## What TeaNode would build

Three pieces, smallest and most useful first. Each works the same from the
dashboard and the iPhone, and each is something the agent can use too.

### 1. Hand a stuck tab to the person

No new software on the computer: the extension already holds the tab's
debugger attachment.

- **The picture.** `Page.startScreencast` sends the tab's frames as JPEGs, only
  when something changed, at a size and quality the viewer asks for; each frame
  is acknowledged before the next comes, so a slow phone gets fewer frames
  rather than a backlog. The extension relays frames to the server on its
  socket and the server relays them to the viewer.
- **Input.** The viewer's taps, keys and text become `Input.dispatchMouseEvent`,
  `Input.dispatchTouchEvent`, `Input.dispatchKeyEvent` and `Input.insertText`,
  which the extension already sends for the agent. A phone keyboard maps to
  `insertText` plus a row of special keys (Tab, Enter, Escape, arrows).
- **The hand-off.** A new browser action, say `ask_to_take_over` with a reason
  ("Sign in to the bank; it wants a code from your phone"), raises an
  interaction like `ask_user`: a card in the conversation, a push to the phone,
  a message in chat apps. Opening it shows the live tab with **Done, hand
  back**. While the person holds the tab, the agent's acting browser calls are
  refused with a clear message and reading calls pass. Handing back answers
  the interaction, and the agent gets a fresh snapshot of the page, not a
  replay of what the person did. If the turn has ended by then, the answer
  resumes the work in a new turn as a late answer does today.
- **Without being asked.** The person can open the same view of their attached
  tab from the dashboard or the phone at any time and take over the same way.

What the person types never enters the conversation or the logs, only that
they took over and when they handed back.

### 2. A live terminal for the person

- Stream the pty's output bytes as well as keeping its screen: the computer
  keeps doing what it does for the agent, and a viewer gets the raw stream for
  xterm.js on the dashboard and [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm)
  (MIT) on the iPhone. A viewer that joins late is sent the current screen
  first, which the computer already holds, then the stream.
- Sessions outlive viewers, as they outlive requests now. The agent's
  terminals, the attached terminal and herdr panes are all sessions, so one
  viewer covers all three; the Coding page's screen view could become live.
- The size belongs to whoever is typing, as with tmux: a phone viewer that
  takes over resizes the pty, and the agent reads the screen at the new size.
- Same lock as the tab: while the person types, the agent's `type` and `key`
  are refused and `screen` and `wait` pass.

### 3. X11 apps, for the person and the agent

- **A display of the agent's own, not the person's screen.** `teanode computer`
  starts `Xvnc :N` (or Xvfb with x11vnc) on demand, listening on localhost
  only, with a window manager. X11 apps the agent opens appear there and the
  person's own screen is left alone. Viewing the person's real screen is a
  separate, later choice (x11vnc on `:0`), with its own switch.
- **Viewing.** The computer relays the VNC protocol between that localhost port
  and the server over a session; the dashboard runs noVNC, and the iPhone runs
  noVNC in a web view or a native client. View-only is enforced on the
  computer, which drops input messages from a viewer without control, so a
  link cannot be widened by the viewer.
- **The agent's tools.** An action family on the computer: list windows, read
  a window as an accessibility tree with a screenshot, click an element or a
  point, type, press, scroll, set a value. open-computer-use is MIT and already
  does this on Linux, macOS and Windows; running it as a server on the
  computer (as a `location: computer` MCP server already can) is the quickest
  trial, and our own actions on the computer the longer-term shape, so the
  results follow our snapshot format and the tool is judged like the others.
- **Same lock and same hand-off** as the tab and the terminal.

## Shared pieces

- **The relay.** Frames and keystrokes go device to server to viewer over
  websockets. The device socket carries JSON today; binary frames tagged with
  the session's identifier keep a screen's picture out of JSON, or each live
  view gets a socket of its own beside the control socket so a busy view never
  delays an `act`. The server keeps nothing it relays.
- **Who may look.** Only the person whose device it is, through their own
  session. Never a circle member, never an operator acting as them unless that
  is shown to the person. Links for another device (the phone) are short-lived
  and the token rides in the fragment. Every view and every take-over goes in
  the person's audit log.
- **The lock.** One holder at a time (the agent, or the person), renewed while
  the viewer is open, falling back to the agent after a short timeout. The
  agent always knows which: a refused call says a person has control.
- **Runs with nobody present.** A dream or a scheduled run cannot hand
  anything over; it leaves the interaction open, as approvals do, and the work
  resumes when the person answers.
- **Switches.** An operator switch for each piece (`agent.features` beside
  `computer` and `browser`), and the person's own switch on each computer for
  whether it may be viewed at all.

## Caveats found while looking

- **Wayland.** x11vnc and xdotool do not see a Wayland session's windows. An
  agent display under Xvnc works regardless of what the person's own desktop
  runs; their real screen under GNOME or KDE on Wayland needs the
  xdg-desktop-portal screen-cast and remote-desktop portals, which ask the
  person on the machine itself.
- **macOS and Windows** need their own capture and input (macOS: the Screen
  Recording and Accessibility permissions, granted by the person at the
  machine). open-computer-use covers the agent side on both; viewing would
  need native capture.
- **Accessibility trees vary.** GTK and Qt apps expose good trees; Electron
  apps need accessibility switched on; games and canvases expose nothing, so
  the screenshot is always sent too.
- **A human-check page in a tab** is the person's to answer. The hand-off
  exists so they can; the agent is told to ask, never to try.

## Open questions for the person

1. The agent's own display only at first, or the person's real screen too?
2. On the iPhone: noVNC in a web view (quick, same as the dashboard) or a
   native viewer (better keyboard and gestures)?
3. Should a stuck tab interrupt with a push every time, or only when the agent
   says the person is needed now?
4. One "Live" page per computer showing its tab, terminals and display
   together, or a view opened from each place (the tab card, the Coding page,
   the conversation)?
