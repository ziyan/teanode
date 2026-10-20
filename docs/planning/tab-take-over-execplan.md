# Taking over a browser tab from the dashboard

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

The research behind it is `docs/planning/remote-control-research.md`; this plan is the first piece of that note, cut to what the person asked for.

## Purpose / Big Picture

The agent can drive the person's own Chrome tab through the TeaNode extension, signed in as them. Sometimes it gets stuck on a step only the person can do: a sign-in, a code sent to their phone, a "prove you are human" page, a choice that is theirs. Today the person has to be sitting at that computer to do it.

After this change the person can open a page on the dashboard, from any browser including a phone's, and see the tab live: the tabs the agent may use in their browser, the one it is acting in, moving as it moves. They can press **Take over**, click and type in the tab from wherever they are, and press **Hand back** to give it back to the agent. While they hold it, the agent can still read the tab but cannot act in it. The agent can also ask for this: when it is stuck it raises a card that says what it needs, which reaches the person the way a question does (in the conversation, on their phone, in their chat apps), and the card opens the live view. When the person hands back, the agent carries on, in the same turn if it is still waiting, or in a new turn if it is not.

The same live view and hand-off work for the server's own headless Chrome, the browser the agent uses when no tab is attached, signed in as nobody.

To see it working: attach a tab with the extension, ask the agent to open a page that needs a sign-in, and when it asks you to take over, press the card's button. The tab is shown live on the dashboard page `/browser`; take over, type, hand back, and the agent reads the page again and goes on.

The iPhone app gets its own native viewer later, once this one has been used and vetted (the person's decision). Until then the iPhone shows the card as a question, which a typed reply answers.

## Progress

- [x] (2026-10-20) Measured that Chrome sends screencast frames for a tab behind another tab and in a minimized window when focus emulation is on (see Surprises & Discoveries).
- [x] (2026-10-20) Wrote this plan.
- [x] (2026-10-20) Milestone 1: the extension streams a tab's picture and takes input; the server relays both to a viewer socket, and holds who controls the tab.
- [x] (2026-10-20) Milestone 2: the dashboard page `/browser` shows the tabs live, with Take over and Hand back.
- [x] (2026-10-20) Milestone 3: the agent asks to be helped (`ask_to_take_over`), is refused while the person holds the tab, and carries on when they hand back.
- [ ] Milestone 4: the same for the server's headless Chrome. Deferred to its own change (see Decision Log).
- [x] (2026-10-20) Milestone 5: documentation, the command line (`teanode agent tab`, `tab hand-back`, and a take-over prompt in `agent ask`), and a run against a real Chrome with the extension (see Outcomes).
- [ ] Review rounds until one finds nothing.

## Surprises & Discoveries

- Observation: Chrome stops sending screencast frames for a tab that is not painted: one behind another tab in its window, or in a minimized window. Turning on focus emulation for the tab makes it paint again, and frames flow at the normal rate in both cases; screenshots of a tab behind another are live either way. So the person's real screen never has to change for them to see and control a tab remotely.
  Evidence (Chromium 154 under Xvnc, an animated test page, three seconds each, 2026-10-20):

      A in front                              frames in 3s: 179
      A behind another tab                    frames in 3s: 1
      input to A while behind                 "typed while behind"
      A in front, window minimized            frames in 3s: 0
      focus emulation, A behind another tab   frames in 3s: 180
      focus emulation, behind and minimized   frames in 3s: 180
      two screenshots 0.3s apart while behind different (live)

  Not measured: a locked screen or a sleeping display on a real machine, and a machine with a GPU.

- Observation: focus emulation is not enough on its own. Chrome sends a screencast frame only when the tab repaints, so a page that does not change sends nothing once the stream has started, and a stream started on a still page behind another tab sent no frame at all in two of three runs. The animated test page above hid this. The extension now sends a screenshot (`Page.captureScreenshot`, live in every case measured) as the first picture, and again whenever the stream has been quiet for two seconds, skipping one identical to the last picture sent, so a still page costs one picture.
  Evidence: the end-to-end run below, with the extension instrumented: one `cast` on, the debugger attached, zero frames in fifteen seconds, before the change; a picture within a second, every run, after.

- Observation: the extension socket's CSRF sign-in, which the plan copied for the viewer socket, never works: nothing in TeaNode sets a `csrftoken` cookie (the GraphQL socket's own comment says so, and signs a session in by checking that the page's Origin is this server). The viewer socket now does what the GraphQL socket does: a token in the hello, or the session cookie from a page of this server (`fromThisServer`).
  Evidence: the page stayed at "Connecting..." in the end-to-end run while a hello with a token was answered at once.

## Decision Log

- Decision: control the person's real browser, not a separate display.
  Rationale: the person said so: the tab they are signed in to is on their real screen. Focus emulation (above) means taking over never changes what their screen shows.
  Date/Author: 2026-10-20, the person.

- Decision: dashboard first; the iPhone's native viewer after this one is vetted.
  Rationale: the person's decision. The dashboard page works on a phone's browser in the meantime.
  Date/Author: 2026-10-20, the person.

- Decision: the person is told only when the agent says it needs them, but can open the live view and take over at any time.
  Rationale: the person's decision. Watching is always available; interrupting is the agent's call.
  Date/Author: 2026-10-20, the person.

- Decision: no terminal and no X11 display in this work.
  Rationale: the person asked only for intervening in a browser tab.
  Date/Author: 2026-10-20, the person.

- Decision: the server's headless Chrome is included (Milestone 4).
  Rationale: the person asked whether it could be used. The viewer, the control and the hand-off are the same; only where frames come from differs, and the server already speaks the DevTools protocol to it directly.
  Date/Author: 2026-10-20, the person and agent.

- Decision: the words are "take over" (the person takes control of the tab) and "hand back" (they give it back). The agent's action is `ask_to_take_over`, the card's interaction kind is `take_over`.
  Rationale: one name per idea, in the person's own words. "Intervene" was the person's word for the goal, not a control in the interface.
  Date/Author: 2026-10-20, agent.

- Decision: watching is passive. Opening the page shows the tab and changes nothing; only Take over stops the agent.
  Rationale: the person may want to look over the agent's shoulder without stopping it.
  Date/Author: 2026-10-20, agent.

- Decision: control falls back to the agent thirty seconds after the person's last viewer goes away without handing back.
  Rationale: a phone that locks or loses its network must not leave the agent refused forever. Thirty seconds rides out a reconnect.
  Date/Author: 2026-10-20, agent.

- Decision: handing back is the GraphQL mutation `HandBackAgentTab(note)`, not a viewer socket message.
  Rationale: handing back also answers the agent's open take-over cards, which goes through the API's own answering (it crosses instances and resumes a turn that ended), and that needs the signed-in person's API context, which the resolvers have and a raw websocket does not. The command line needs the same mutation anyway.
  Date/Author: 2026-10-20, agent.

- Decision: the server's headless Chrome (Milestone 4) is left for its own change.
  Rationale: a headless page belongs to one turn and is closed when the turn ends, so a hand-off that outlives the turn needs the page's ownership changed, which is a change of its own; the person narrowed this round to intervening in their tab. The viewer, the room and the lock are written so a second source of pictures can be added beside the extension.
  Date/Author: 2026-10-20, agent.

- Decision: the command line's `agent ask` shows a take-over request as such and hands back on Enter (with anything typed as the note) through `HandBackAgentTab`; with no terminal to read, it waits for the hand-back on the dashboard instead of answering with nothing.
  Rationale: found in the end-to-end run, where it printed the request as a question and, reading nothing, answered it with an empty reply.
  Date/Author: 2026-10-20, agent.

- Decision: the hand-off card is a stored interaction, like `ask_user`, so it survives the turn ending, resumes the work in a new turn when answered late, reaches the phone as a notification and chat apps as a question, and shows on the iPhone (which does not know the kind) as a question card.
  Rationale: all of that exists for questions (`docs/planning/durable-interactions-execplan.md`); a new kind reuses it rather than building a second path.
  Date/Author: 2026-10-20, agent.

## Outcomes & Retrospective

Milestones 1, 2, 3 and 5 are built. Checked end to end on 2026-10-20 against a dev server built from the branch, with Chrome 154 (headless, a throwaway profile, the extension loaded unpacked over a debugging pipe since branded Chrome no longer takes `--load-extension`), a scripted model, and the Browser page open in a second tab of the same window, so the attached tab was behind it the whole time:

    - picture shown: 1280x813
    - page says: Test form Yours Your agent is here Your agent is using this tab Take over
    - taken over
    - a click in the picture focused the form field in the real tab
    - keys typed in the picture reached the real tab: "482"
    - the picture changed after typing, with the form behind the Browser page
    teanode agent tab: you have taken the tab over: your agent can read it but not act in it until you hand it back
    the agent's click: "the person has taken over their tab from the dashboard, so click is not done; ..."
    - handed back from the page
    teanode agent tab: your agent holds the tab
    agent ask "please sign in again": The agent needs you in your browser tab: Sign in to the test page, then hand the tab back
    teanode agent waiting: take_over  Sign in to the test page, then hand the tab back
    teanode agent tab hand-back Typed the code: handed back
    the tool's answer: {"isHandedBack":true,"next":"The page has changed: take a snapshot before you act.","note":"Typed the code"}

The run found the two problems in Surprises & Discoveries and the command line's empty answer; all three are fixed. Left: the headless browser (Milestone 4), the iPhone's native viewer, a zoom for the picture on a phone (a 1280-pixel tab is drawn small at 390 pixels), and the locked-screen case on a real machine.

## Context and Orientation

TeaNode is a Go server (`cmd/teanode-server`, code under `internal/`) with a React dashboard (`web/src`), a command-line client (`internal/cmd`, `internal/client`), an iPhone app (`ios/`) and a Chrome extension (`web/extension`). Each person has an agent: a model that runs turns in a conversation and calls tools. `docs/subsystems/devices.md` describes the attached tab in full; what matters here follows.

**A device** is something on the person's side that keeps a websocket open to the server. The extension is one. Its socket is served by `tabView` in `internal/api/v1api/apigraph/agent_tab.go` at the path `api.PathAgentTab` (`/api/v1/agent/tab`). The extension sends `hello` (with its protocol number and the token it signed in with), then `update` when its tab changes, `ping` every twenty seconds, and `result` for each answer. The server sends `welcome`, `refused`, `pong` and `act`. An `act` carries a number, an action name and arguments; the extension does it and answers `result` with the same number. The server side of that is `deviceLink` in `internal/agent/device.go` (`AskFor` sends and waits) and `attachedTab` in `internal/agent/tab.go`, kept per agent in `Agent.tabs` behind `Agent.tabsMutex`. One browser connects per person; within it, the actions go to one tab at a time: the tab the person attached (their "own" tab) or one the agent opened, which the extension puts in a tab group named TeaNode. The extension's `tabs` action lists exactly those tabs.

**The extension** is `web/extension/src/background.js` (a Manifest V3 service worker), with `panel.js` and `options.js`. It already attaches Chrome's debugger API (`chrome.debugger`) to a tab for the agent's `cdp` action, through `debuggerFor(tabId)`, and keeps the DevTools protocol events it hears in `debugging` (a map of tab to recent events). It speaks protocol 3; the server takes 2 and 3 (`tabProtocol`, `tabProtocolOldest`).

**The DevTools protocol** (often "CDP") is how a program drives Chrome: JSON commands such as `Page.navigate` and events such as `Page.loadEventFired`. Three parts of it matter here. `Page.startScreencast` makes Chrome send `Page.screencastFrame` events, each a JPEG of the tab (base64) with metadata saying the page's size in CSS pixels; each frame must be acknowledged with `Page.screencastFrameAck` before Chrome sends the next, which is how a slow receiver gets fewer frames rather than a backlog. `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent` and `Input.insertText` deliver input as a real mouse and keyboard would, at CSS-pixel coordinates in the tab's viewport. `Emulation.setFocusEmulationEnabled` makes a tab behave as if focused and visible, which keeps it painting when it is behind another tab or minimized.

**The browser tool** is `internal/agent/tools/browser/browser.go`. `runBrowserOnTab` handles the person's tab: it refuses acting actions in a run with nobody present, and sends everything else to the extension through `askTab`. `browserWritingActions` lists the actions that act (click, type, press, scroll, evaluate and the rest). The headless Chrome beside the server is `internal/browser` (a `Browser` holding `Context`s, one per turn, each with one page, every request checked against an address guard), used from `internal/agent/browser.go`.

**Stored interactions** are the cards the agent raises and waits on. `models.AgentInteraction` (`internal/models/insight.go`) has an `InteractionKind` (`question` or `approval`), the text, choices, and the answer once given. `AskRun.Ask` in `internal/agent/ask.go` raises one through `raiseInteraction`, emits an `EventQuestion` event the dashboard's drawer draws as a card, waits a few minutes, and if nobody answers ends the turn leaving the card open (`ErrLeftOpen`). `AnswerAgentQuestion` (`internal/api/v1api/apigraph/agent_memory.go`) answers it, live or late; a late answer starts a new turn through `ResumeInteraction` in `internal/agent/interaction.go`. `notifyInteraction` in `internal/agent/phone_push.go` tells the person's phones. The dashboard reads open cards with `ListAgentInteractions` and draws them in `web/src/components/agentDrawer.tsx`. The iPhone's `Transcript.swift` draws any kind that is not `approval` as a question.

**More than one server instance** is possible, but attached tabs live in the memory of the instance holding the extension's socket, and a run on another instance cannot see them (`docs/subsystems/streaming-and-instances.md`). The viewer socket has the same limit: it must reach the instance holding the tab, which session affinity gives. This plan does not change that.

## Plan of Work

### Milestone 1: the picture and the input, relayed

At the end of this milestone a test client can open the viewer socket, see frames of the person's tab, take over, send a click, and hand back; and the server knows at every moment whether the person or the agent controls the tab.

In `web/extension/src/background.js`, raise `PROTOCOL` to 4 and add three things, all on the existing socket. A `cast` message from the server (`{"type":"cast","tab":N,"on":true,"maxWidth":W,"maxHeight":H,"quality":Q}`) starts a screencast of tab N, which must be the person's own tab or one the agent opened (`pickTab`); `on:false` stops it. Starting attaches the debugger through `debuggerFor`, turns on `Emulation.setFocusEmulationEnabled`, and sends `Page.startScreencast` with `everyNthFrame: 2`. In the `chrome.debugger.onEvent` listener, a `Page.screencastFrame` is not kept in the events list: it is sent to the server as `{"type":"frame","tab":N,"data":"<base64 jpeg>","metadata":{...}}` and acknowledged to Chrome only when `socket.bufferedAmount` is under 512 KiB, checked again every 50 milliseconds until it is, so a slow connection slows Chrome down instead of filling the worker's memory. Stopping sends `Page.stopScreencast` and turns focus emulation off. A screencast stops by itself when its tab closes, when the socket closes (`letEveryDebuggerGo` already lets the debugger go), and when the agent's own `cdp_stop` lets the debugger go. While casting, the debugger's idle deadline is renewed with each frame. A `pointer`, `key`, `text` or `wheel` message from the server (shapes in Interfaces) becomes `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent` or `Input.insertText` on the named tab, through the same debugger attachment; these are not `act` requests and have no answer, because a viewer's input is a stream and an answer per mouse movement would only add delay. The extension also reports its tabs whenever they change, with an `update` that now carries `tabs` (the same list the `tabs` action returns), so the server can tell a viewer what there is without asking.

In `internal/api/v1api/apigraph/agent_tab.go`, take protocol 4 (`tabProtocol = 4`, still welcoming 2 and 3), read the new `frame` message and the `tabs` field of `update`, and hand both to the agent: `worker.TabFrame(agentId, socket, tab, data, metadata)` and `worker.UpdateTabList(agentId, tabs)`. Frames from a connection that is not the attached one are dropped, as late results are.

In `internal/agent/tab.go`, give `attachedTab` its viewers and its controller. A viewer is a value with a `Send` method and a buffer of one frame: a new frame replaces an unsent one, so a slow viewer sees the latest picture rather than a queue. `AddTabViewer(agentId, viewer, watchedTab)` registers a viewer and, if it is the first watching that tab, sends the extension `cast` on; `RemoveTabViewer` unregisters, sending `cast` off when the last watcher of a tab goes. `TakeOverTab(agentId, viewer)` makes the person the controller; `HandBackTab(agentId)` makes the agent the controller again; a controller who is a person and has no viewer left for thirty seconds is turned back into the agent by a timer. `TabInput(agentId, viewer, input)` forwards input to the extension only when that viewer's person holds control, and drops it otherwise. Every change of controller and of the tab list is sent to every viewer as a `state` message. `IsTabTakenOver()` on `attachedTab` (and on `tools.Tab`, the interface the browser tool sees) says whether the person holds it.

A new websocket, `api.PathAgentTabView` (`/api/v1/agent/tab/view`), served by `tabViewerView` in a new file `internal/api/v1api/apigraph/agent_tab_view.go`. It authenticates as the GraphQL socket does: a `hello` with a token, or a dashboard session whose cookie came with the upgrade from a page of this server (`fromThisServer`), then `agentOfPerson`. It is refused when attaching tabs is off, as `tabView` is. Then it relays: `watch`, `take_over` and input from the viewer (handing back is the mutation `HandBackAgentTab`, see Milestone 3); `state`, `frame` and `refused` to it. The read limit is 64 KiB (a viewer sends small messages); the write side is the viewer's one-frame buffer, written by one goroutine per viewer with a ten-second deadline.

Proof: Go tests in `internal/agent/tab_viewer_test.go` with a fake extension connection and fake viewers: a frame reaches every viewer of its tab and none of another; a second frame replaces an unsent first; the first viewer sends `cast` on and the last one off; input is dropped unless the sender's person holds control; control returns to the agent thirty seconds after the last viewer goes (the timer is a field the test can shorten); a frame from a replaced connection reaches nobody. Then a test of `tabViewerView` with `httptest` and a real websocket client: a hello without a token is refused, a good one gets `state`.

### Milestone 2: the dashboard page

At the end of this milestone the person opens `/browser` and sees their tab live, and can take over and hand back.

A new page `web/src/pages/browser.tsx`, routed at `/browser` in `web/src/app.tsx`. It reads `ReadAgentTab` for whether the browser is connected (and says how to connect the extension when it is not), then opens the viewer socket. It lists the tabs the extension reported (the person's own first, marked as theirs, then the agent's), with the one the agent is acting in marked; choosing one watches it. The tab is drawn as an `<img>` whose source is each frame's JPEG, scaled to fit the page's width and keeping its shape, on phone widths as on a desktop. Above it, one line says who is in control ("Your agent is using this tab" or "You have control") and holds one button: **Take over** or **Hand back**. Hand back opens a small field for an optional note to the agent ("Signed in; the code was 123456" is a bad example, since a code should not be repeated, so the placeholder is "Anything the agent should know"). While the person has control, the picture takes input: pointer down, move and up become `pointer` messages at the tab's CSS-pixel coordinates (the picture's position scaled by the frame metadata's `deviceWidth` and `deviceHeight`); the wheel becomes `wheel`; keys pressed while the picture has focus become `key` messages; pasted text becomes `text`. On a phone, a keyboard button focuses a hidden text field whose typed characters become `text` and whose Enter and Backspace become `key`. The coordinate mapping and the key mapping live in `web/src/pages/browserInput.ts` as pure functions, so they are tested without a browser. The page reconnects as the GraphQL socket does (one second to thirty, with jitter), and says "Connecting…" meanwhile. Strings go in `web/src/i18n/en.ts`, `zh.ts` and `ja.ts`.

The drawer's tab menu (`TabMenu` in `web/src/components/agentDrawer.tsx`) gains a row **Watch the tab** that opens `/browser`. The sidebar does not get an entry: the page is reached from the tab menu and from the card, where a tab exists.

Proof: vitest for `browserInput.ts` (a click at the middle of a 640-pixel-wide picture of a 1280-CSS-pixel page lands at x 640; Enter and Backspace carry their key codes; a printable key carries its text) and for the page with a fake socket (frames are drawn; Take over sends `take_over`; input is sent only after `state` says the person has control; Hand back calls `HandBackAgentTab` with the note). Then the page in Chrome at 390 pixels and at desktop width, light and dark, against a dev server with the extension attached.

### Milestone 3: the agent asks, and waits its turn

At the end of this milestone the agent can ask the person to take over, is refused while they hold the tab, and goes on when they hand back.

Add `InteractionTakeOver InteractionKind = "take_over"` in `internal/models/insight.go`. Add an action `ask_to_take_over` to the browser tool, with `text` saying what the person should do; it is not in `browserWritingActions` (it acts in nothing). It needs somebody who can answer: in a run that cannot ask (`run.CanAsk()` false, or a mail, schedule or research surface), it fails with "nobody is present to take over the tab; say in your answer what they need to do there". Otherwise it calls a new `AskToTakeOver(ctx, callId, text, target)` on the run (added to the run interface in `internal/agent/tools`, implemented by `AskRun` in `internal/agent/ask.go` and refused by `directRun` in `internal/agent/direct.go`). `AskToTakeOver` raises a `take_over` interaction with `ToolName` "browser" and the text, emits `EventQuestion` with the kind on the event so the drawer can draw the right card, and waits as `Ask` does. It is answered by the person handing back (the mutation `HandBackAgentTab` gives the tab back and answers every open `take_over` card of theirs with `models.InteractionHandedBack` and the note) or by a typed reply on any surface (`AnswerAgentQuestion`, which already answers any open card by run and call). The tool returns `{"isHandedBack": true, "note": "...", "next": "Take a snapshot before you act: the page has changed."}`, or, if the turn ended first, the same left-open note `ask_user` gives. `ResumeInteraction` gains the `take_over` case: the new turn's message is the request and what the person said, with the line "They took over the tab and handed it back after your turn ended. Take a snapshot before acting."

In `runBrowserOnTab`, an action in `browserWritingActions` while `tab.IsTakenOver()` fails with "The person has taken over this tab. Wait for them to hand it back (ask_to_take_over waits for that) or do something else; reading the page still works." Reading actions go ahead.

In the drawer, a `take_over` card shows the agent's text and a button **Take over the tab** that opens `/browser`; it can still be answered by typing, like a question. Its interaction type in the drawer's TypeScript becomes `'question' | 'approval' | 'take_over'`. The phone notification for it is titled "Your agent needs you" with the text as the body and the Reply button; chat apps already relay open questions and get the text plus "Open /browser on the dashboard to take over." The iPhone draws it as a question card, unchanged.

Proof: Go tests: a writing browser action is refused while the tab is taken over and a snapshot is not; `ask_to_take_over` in a run that cannot ask fails with the words above; a hand-back answers the open card and the tool returns `isHandedBack`; a late hand-back resumes a turn whose message carries the request and the note (beside the existing resume tests in `internal/agent/resume_test.go`). vitest: the drawer draws a `take_over` card with its button.

### Milestone 4: the server's headless Chrome

At the end of this milestone the same page shows the headless browser's page while a turn uses it, and the same hand-off works there.

In `internal/browser`, add `Context.StartScreencast(maxWidth, maxHeight, quality, onFrame)`, `StopScreencast()` and `Input(input)`, speaking the same DevTools methods the extension does, on the context's page. Headless Chrome paints without a screen, so no focus emulation is needed. In `internal/agent/browser.go`, a headless context in use by a turn of the person's is offered to the viewer as one more tab, named "Server browser", with the same viewers, controller and input handling as the attached tab; the code for those in `tab.go` is written once and shared (a small `tabView` holder that both use), not copied. A context that has an open `take_over` card is kept when its turn ends, for as long as the card is open and at most thirty minutes, and the turn that resumes from the card uses it again instead of opening a fresh one; otherwise contexts are discarded at the end of their turn as now. Input from the person goes to the page directly; the page's own requests still pass the address guard, so the person cannot use the server's browser to reach the server's network any more than the agent can.

Proof: Go tests with the test Chrome `internal/browser` already uses in its tests: a screencast delivers at least one frame of a test page; a click at a button's coordinates clicks it; a context with an open card survives its turn's end and is reused by the resuming turn.

### Milestone 5: documentation, the command line, review, and a real run

`docs/subsystems/devices.md` gains a section "Taking over a tab" describing the view, the control, the hand-off and the thirty-second fallback, and its Caveats gain the instance limit and the screen-lock finding. `docs/reference/command-line.md` and `internal/cmd` gain `teanode agent tab`, which prints whether the browser is connected, its tabs, and who controls them, and `teanode agent tab hand-back [--note TEXT]`, which hands back from a terminal (taking over needs the picture, so it is the dashboard's). Both go through new GraphQL fields: `ReadAgentTab` gains `tabs` and `isTakenOver`, and a mutation `HandBackAgentTab(note)`; the command-line parity test checks the names.

Then review the whole diff, fix what is found, and review again until a round finds nothing. Then run it for real: deploy to a dev server, load the extension unpacked into a Chrome with a test profile, attach a tab, and from a phone-width window take over, type into a form, and hand back. Check the frames with the Chrome window minimized, behind another tab, and with the screen locked; record what happens in Surprises & Discoveries.

## Concrete Steps

All commands run from the repository root.

Go tests for the touched packages:

    go test ./internal/agent/ -run 'TabViewer|TakeOver|Resume' -count=1
    go test ./internal/api/v1api/apigraph/ -run 'TabView' -count=1
    go test ./internal/browser/ -count=1

Dashboard checks:

    cd web && npx tsc --noEmit -p . && npx eslint --max-warnings 0 src/pages/browser.tsx src/pages/browserInput.ts src/components/agentDrawer.tsx && npx vitest run src/pages/browser src/components/agentDrawerChat.test.tsx

The whole tree, as CI runs it, with new files staged first so the secret check reads them:

    set -o pipefail; make lint-ci

## Validation and Acceptance

With a dev server running and the extension (from `web/extension`, loaded unpacked) signed in to it and attached to a tab showing any page:

1. Open `/browser` on the dashboard. The tab is listed and drawn live; scrolling the real tab moves the picture within a second.
2. Ask the agent in the drawer to scroll that page. The picture scrolls, and the line above it says the agent is using the tab.
3. Press Take over. The line says you have control. Ask the agent to scroll again: its answer says the person has taken over the tab, and the page does not move.
4. Click a text field in the picture and type. The text appears in the real tab.
5. Press Hand back with a note. Ask the agent to scroll: it does.
6. Ask the agent to "open a page that needs me to sign in and ask me to do it". A card appears with Take over the tab; it opens `/browser`; take over, hand back, and the agent's turn continues with a snapshot.
7. Repeat 6 and close the dashboard before handing back; reopen it later and hand back: a new turn starts and carries on.
8. Minimize the Chrome window and put another tab in front of the attached one: the picture keeps updating.

## Idempotence and Recovery

Nothing is stored except the interaction rows the card already uses; no migration. A viewer that disconnects leaves nothing behind after thirty seconds. An extension speaking protocol 3 keeps working for the agent; the page says "Update the TeaNode extension to watch this tab" when the extension cannot cast, which the server knows from the protocol in its hello. Reverting the change leaves open `take_over` cards that nothing resumes; they can be closed by typing a reply, which answers them as questions.

## Interfaces and Dependencies

No new dependencies. Messages, all JSON text frames.

Viewer to server: `{"type":"hello","token":"…"}`, or `{"type":"hello"}` from a dashboard page, whose session cookie came with the upgrade; `{"type":"watch","tab":"N"}` (N from `state`, as text); `{"type":"take_over"}`; handing back is the mutation `HandBackAgentTab(note: String)`; `{"type":"pointer","event":"down|move|up","x":X,"y":Y,"button":"left|right|middle|none","clickCount":C,"modifiers":M}`; `{"type":"wheel","x":X,"y":Y,"deltaX":DX,"deltaY":DY}`; `{"type":"key","event":"down|up","key":"Enter","code":"Enter","keyCode":13,"text":"","modifiers":M}`; `{"type":"text","text":"…"}`; `{"type":"ping"}`. X and Y are CSS pixels in the tab's viewport; M is the DevTools modifier bit field (Alt 1, Control 2, Meta 4, Shift 8).

Server to viewer: `{"type":"state","isConnected":true,"canCast":true,"tabs":[{"tab":"N","title":"…","url":"…","isOwn":true,"isCurrent":true}],"watching":"N","controller":"agent|person"}`, with `watching` left out while nothing is watched; `{"type":"frame","tab":"N","data":"<base64 jpeg>","width":W,"height":H}` where W and H are the page's CSS size from the frame metadata; `{"type":"refused","reason":"…"}`; `{"type":"pong"}`.

Server to extension (protocol 4): `{"type":"cast","tab":N,"on":true,"maxWidth":1280,"maxHeight":1280,"quality":60}` and the `pointer`, `wheel`, `key` and `text` messages above with a `tab` field added. Extension to server: `{"type":"frame","tab":N,"data":"…","metadata":{"deviceWidth":W,"deviceHeight":H,…}}`, from a screencast frame or a screenshot still (whose metadata carries only the two sizes, from `Page.getLayoutMetrics`), and `update` and `hello` with `tabs` (`[{"tab":N,"title","url","own","current"}]`).

Go, in `internal/agent` (`tab_viewer.go`): `AddTabViewer(agentId string, viewer TabViewer)`, `RemoveTabViewer(agentId string, viewer TabViewer)`, `WatchTab(agentId string, viewer TabViewer, tab string)`, `TakeOverTab(agentId string)`, `HandBackTab(agentId string) bool`, `IsTabTakenOver(agentId string) bool`, `TabInput(agentId string, viewer TabViewer, input TabInputMessage)`, `TabFrame(agentId string, connection TabConnection, tab, data string, width, height float64)`, `UpdateTabList(agentId string, tabs []TabListing)`, `TabList(agentId string) []TabListing`, `SetBrowserCanCast(agentId string, connection TabConnection, canCast bool)`; `type TabViewer interface { Send(message []byte) error; SendFrame(message []byte) }`. In `internal/agent/tools`: `Tab.IsTakenOver() bool`, and `TakeOverAsker`, an optional interface of a run with `AskToTakeOver(ctx context.Context, callId, request string) (string, error)`, which `AskRun` implements. In `internal/models`: `InteractionTakeOver` and `InteractionHandedBack`. GraphQL: `ReadAgentTab` gains `tabs` and `isTakenOver`; `HandBackAgentTab(note)`.
