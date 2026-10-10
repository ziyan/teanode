# An iPhone app for talking to your agent

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the ExecPlan rules the repository's planning documents share (see the other files in `docs/planning/`).

## Purpose / Big Picture

Today a person reaches their TeaNode agent from a phone through the dashboard in Safari, a Telegram or Discord bot, or an MCP client such as the ChatGPT app. The dashboard works, but a browser tab cannot do what a phone is for:

- It cannot notify. When mail shows something that cannot wait, when a coding session stops to ask a question, or when the agent needs approval, nothing on the phone lights up unless a chat bot is linked.
- It cannot receive. A receipt photographed in the Photos app has to be saved, then uploaded through the drawer's paperclip. iPhone photos are HEIC, which the server does not read as a picture at all.
- It cannot keep a call going. A voice call in the drawer stops when the screen locks.
- It cannot be asked from Siri, the Action button, a widget or the lock screen.

After this change, a person installs the TeaNode app on their iPhone, signs in to their own server, and:

- talks to their agent in the main conversation and the named ones, with the same streamed answers, tool lines, approval cards and coding-session question cards as the drawer;
- gets a notification for each alert, approval, coding-session question and goal that needs them, and can answer from the notification itself ("Approve", "Option 2") without opening the app;
- shares a photo, screenshot, PDF, link or text into a conversation from any app;
- calls their agent and keeps talking with the screen locked, through AirPods, and later in the car;
- says "Ask Tea ..." to Siri, or presses the Action button;
- sees waiting approvals in a widget, and a working coding session on the lock screen;
- installs one setup profile that adds their TeaNode mailbox, calendars, contacts and reminders to the iPhone's own Mail, Calendar, Contacts and Reminders apps.

To see it working, once complete: install the app from TestFlight, sign in to the server, lock the phone, and have a coding session on an attached computer ask a question. The phone shows a notification with the options as buttons; pressing one answers the question in the pane.

Mail, calendar, contacts, finance and memory get no screens of their own in the app. Mail, calendars, contacts and reminders already reach the iPhone's own apps over IMAP, CalDAV and CardDAV, which the server serves (Milestone 3 makes that one tap). Finance and memory are reached by asking the agent, or through the dashboard in Safari.

## Progress

- [x] (2026-10-10) Researched what the server offers a native client: sign-in, the GraphQL API and its websocket, cards, attachments, voice, location, where unasked messages are delivered, devices, DAV and IMAP. Chose the scope with the person. Wrote this plan.
- [ ] Milestone 0: a build pipeline with no Mac, and the person's Apple developer setup.
- [ ] Milestone 1: an app can sign in and get a token for the whole API.
- [ ] Milestone 2: phones as devices, and notifications sent to them.
- [ ] Milestone 3: the setup profile.
- [ ] Milestone 4: the app: sign in, conversations, chat, cards, pictures, location.
- [ ] Milestone 5: notifications in the app, and answering from them.
- [ ] Milestone 6: the share extension.
- [ ] Milestone 7: voice calls.
- [ ] Milestone 8: Siri and Shortcuts.
- [ ] Milestone 9: widgets and Live Activities.
- [ ] Milestone 10: TestFlight from CI, documentation, decision record, release.

## Surprises & Discoveries

- Observation: an OAuth token issued today works only on the MCP endpoint.
  Evidence: `agentToolsResource` (`internal/api/v1api/apioauth/oauth.go`) forces the token's resource to `/api/v1/mcp`, and `allowsResource` (`internal/web/session.go`) refuses it on any other path, `/api/v1/graphql` included. The only scope is `mcp` (`apioauth/metadata.go`).

- Observation: registration refuses a redirect address with a custom scheme.
  Evidence: `usableRedirect` (`apioauth/register.go`) allows `https`, or `http` on a loopback host. An iPhone app's sign-in sheet (`ASWebAuthenticationSession`) returns to the app through a scheme of its own, or through an `https` address the app has claimed in its entitlements. A self-hosted server's host cannot be listed in an app's entitlements in advance, so a scheme is the only way back.

- Observation: there is nothing for push notifications anywhere, and computers are devices only in memory.
  Evidence: no APNs, web push or push token code or table exists. `internal/agent/device.go` holds attached computers in memory, and a computer is revoked by deleting the API token it signed in with.

- Observation: the server does not read HEIC as a picture.
  Evidence: `tools.IsImage` (`internal/agent/tools/tool.go`) accepts `image/png`, `image/jpeg`, `image/gif` and `image/webp`. A HEIC upload is stored and named, never shown to the model.

- Observation: everything else a client needs already exists and is used by the drawer. Sections below name each operation.

## Decision Log

- Decision: the app is native, written in Swift with SwiftUI, for iOS 18 and later. It has no third-party Swift dependencies.
  Rationale: notifications with buttons, a share extension, calls that survive a locked screen, Siri, widgets and Live Activities exist only for native apps. A web view inside a native shell would still need native code for each of them, and would make the chat a second copy of the drawer to keep in step. No dependencies matches the repository's habit of vendoring little, and everything needed (HTTP, websockets, JSON, the keychain, audio) is in Apple's frameworks.
  Date/Author: 2026-10-10, agent; scope chosen by the person.

- Decision: the app lives in `ios/` in this repository.
  Rationale: the repository is public, so GitHub's macOS runners build it for free (the person has no Mac). A server change and the app change that uses it land in one pull request, and a test can check the app's queries against the server's schema (Milestone 4).
  Date/Author: 2026-10-10, agent.

- Decision: the app does not re-implement mail, calendar, contacts, finance or memory.
  Rationale: the iPhone's own apps already read mail, calendars, contacts and reminders from the server. Native screens for the rest would be a large surface to keep in parity with the dashboard and the command line for little gain over asking the agent.
  Date/Author: 2026-10-10, the person and agent.

- Decision: an app signs in through the server's existing OAuth, extended with a second scope, `api`, that grants a token for the whole API as the person; and registration accepts a private-use scheme, `com.teanode.app:/oauth` (a reverse domain name with one slash, RFC 8252 section 7.1), as a redirect.
  Rationale: the CLI's loopback sign-in (`teanode auth login`) cannot be used, because an iPhone app cannot rely on a loopback listener while the sign-in sheet is open. Handing a token back through a scheme is safe only with PKCE, which the OAuth flow already requires. A scheme is the only redirect a self-hosted server can use.
  Consequences: a token with scope `api` is listed among the person's tokens with the app's name and can be deleted there, like the CLI's. The consent page must say plainly that this grants everything the person can do. The `mcp` scope keeps its narrow resource. This needs the security review in Milestone 1.
  Date/Author: 2026-10-10, agent. To be confirmed by the person.

- Decision: the server sends notifications directly to Apple's push service (APNs) with a key the operator configures. A relay run by whoever publishes the app comes later, and only if the app is published for servers other than the publisher's.
  Rationale: APNs only accepts notifications for an app from a key belonging to the developer account that publishes it. A person who builds the app under their own account and runs their own server holds both, which is the case now. An App Store app used by other people's servers would need its publisher to run a relay that holds the key, which the server would then call instead of APNs.
  Date/Author: 2026-10-10, agent. To be confirmed by the person.

- Decision: Apple sees no content. Every notification is encrypted for the phone it goes to. The phone generates a key when it registers and hands it to the server, which stores it sealed (like other secrets). A notification carries only the ciphertext and `mutable-content: 1`. The app's notification service extension decrypts it on the phone and fills in the title, body and buttons.
  Rationale: what an alert says is the person's own mail. The same reasoning made the ChatGPT provider send `store: false` (`internal/llm/codex_wire.go`).
  Date/Author: 2026-10-10, agent.

- Decision: the language model gets no tool for phones or the setup profile.
  Rationale: the agent already notifies through alerts, goals and questions, and the phone receives those. Registering a phone or handing out a setup profile is something the person does on the phone itself. A setup profile carries an app password, which the agent must never hold or pass through a conversation. Phones are listed and removed on the dashboard and the command line, which stay in parity with each other.
  Date/Author: 2026-10-10, agent. This narrows the tools, web and command line parity rule, so the person confirms it.

- Decision: the app tells the server which surface it is with three new surfaces in `internal/agent/surface.go`: `ios` for typed turns in the app, `ios_voice` for a call, and `siri` for Siri and Shortcuts. `ios` and `ios_voice` can locate; `siri` asks for one short spoken sentence.
  Rationale: `phone` exists, but the drawer guesses it from the window width. Naming the app lets the prompt say what it can show (pictures, cards, no hover) and lets a location request go to the phone that sent the turn.
  Date/Author: 2026-10-10, agent.

## Context and Orientation

TeaNode is one Go program, `teanode-server`, with a dashboard written in TypeScript and React (`web/src`) built into it, and a command line program, `teanode`. Both the dashboard and the command line talk to the server through one GraphQL API at `/api/v1/graphql`. The schema is generated from Go types in `internal/api/v1api/apigraph`, using the library `github.com/graphql-go/graphql`; `schema_test.go` in that directory builds it. The rule for every capability is parity: what the dashboard can do, the command line can do, and the agent's tools can do, the same way, unless a decision says otherwise.

Terms used below:

- APNs is Apple's push notification service. A server sends a notification by an HTTP/2 POST to `api.push.apple.com` (or `api.sandbox.push.apple.com` for development builds) at `/3/device/<device token>`. It authenticates with a JSON Web Token signed with ES256 by a key from the developer account: a `.p8` file with a key ID and a team ID. The header `apns-topic` is the app's bundle ID, and `apns-push-type` is `alert` or `liveactivity`. Go's standard library can do all of this (`net/http` speaks HTTP/2; `crypto/ecdsa` signs).
- A device token is what iOS gives an app for APNs to address it. It changes now and then, and the app reports each new one.
- A notification service extension is a small program inside the app that iOS runs when a notification with `mutable-content: 1` arrives, before showing it. It can change the title and body, and set a category. A category names the buttons a notification shows.
- An app group is a shared container and keychain group that the app and its extensions use to share the sign-in.
- A Live Activity is a lock screen and Dynamic Island card an app starts. APNs updates it with `apns-push-type: liveactivity` sent to the activity's own push token.
- App Intents are actions an app offers to Siri, Shortcuts and the Action button.
- `ASWebAuthenticationSession` is the sign-in sheet: it opens a web page and returns when the page redirects to the app's scheme.

What the server offers a client today, all of which the drawer uses (`web/src/components/agentDrawer.tsx`, `web/src/api.ts`):

- Conversations: `ListAgentConversations(archived, query)`, `ReadAgentConversation(conversationId, first, offset)` (pages back from the newest message; `first` up to 500), `StartAgentConversation`, `UpdateAgentConversation`, `SetAgentMainConversation`. An empty conversation ID means the main conversation.
- Asking: `AskAgent(conversationId, message, surface, attachmentIds, references, drawerId, effort, readOnly)` returns `{runId conversationId}`.
- Streaming: the subscription `AgentConversationEvents(conversationId)` over a websocket at `/api/v1/graphql` with the `graphql-ws` subprotocol (the old Apollo protocol: `connection_init`, `start`, `stop`; the server sends `connection_ack`, `ka` every second, `data`, `error`, `complete`). A client without a browser `Origin` header authenticates with `payload.Authorization: "Bearer <token>"` in `connection_init` (`apigraph/websocket.go`). On subscribe the server replays the events of unfinished runs (`internal/agent/feed.go`), so a reconnecting client loses nothing; duplicates are dropped by `sequence`. Without a websocket, `ReadAgentRun(runId, after, wait)` long-polls.
- Event kinds (`internal/agent/ask.go`): `asked`, `text` (a piece of the answer), `message` (the whole answer), `tool_call`, `tool_result`, `confirmation`, `question`, `note`, `titled`, `navigate`, `locate`, `done`, `error`.
- Cards: `ResolveAgentConfirmation(runId, callId, approve)`, `AnswerAgentQuestion(runId, callId, answer)`, `AnswerAgentHerdrQuestion(computer, paneId, questionFingerprint, optionNumbers, optionLabels, freeText)`, `StopAgentRun(runId)`. Cards that outlive their turn are listed by `ListAgentInteractions(conversationId)`; they are saved by `raiseInteraction` (`internal/agent/interaction.go`).
- Attachments: `POST /api/v1/agent/attachments`, multipart, field `file`, Bearer token; returns `{attachments:[{id name contentType size}]}`; 25 MB by default (`agent.limits.maxAttachmentBytes`). Downloads at `GET /api/v1/agent/attachments/{id}`.
- Voice: `ReadAgentVoice` says whether voice is on. The websocket `GET /api/v1/agent/voice` takes a first text message `{"voiceEvent":"hello","authorization":"Bearer ...","captureSettings":{...}}`, then binary frames of mono 16-bit little-endian PCM at 24000 Hz, at most 64 KB each. It answers with JSON `voiceEvent` messages (`ready`, `speechStarted`, `speechStopped`, `transcriptDelta`, `transcriptFinal`, `answerAudio` with base64 PCM at 24 kHz, `answerAudioDone`, `error` and others). It starts no turn: the client sends each final transcript with `AskAgent`. It reads an answer aloud when sent `speakAnswer {answerSegmentId, answerText}` (`internal/api/v1api/apigraph/agent_voice.go`, `internal/voice`).
- Location: a `locate` event carries `callId` and the `drawerId` of the client that sent the turn; that client answers with `AnswerAgentLocation(runId, callId, latitudeDegrees, longitudeDegrees, accuracyMeters, measuredAt, errorMessage)` within a minute (`internal/agent/locate.go`).
- Presence: `ReportAgentPresence(isVisible, idleSeconds)` says whether the person is looking.
- Unasked messages: an alert (`deliverAlert`, `internal/agent/alert.go`), a coding-session question (`writeHerdrQuestion`, `internal/agent/herdr.go`) and a goal that needs the person (`surfaceGoal`, `internal/agent/goal_background.go`) are each written into the main conversation, then published with an `asked` event whose `note` names the surface (`alert`, `herdr_question`, `goal_needs_you`; `speak_first:...` for the agent starting a conversation). Telegram and Discord receive them through `chatState.relay` (`internal/channel/relay.go`), which reads `ListAgentOwnTurnAnswers` with a cursor and wakes on `done`.
- Mail programs: IMAP and SMTP submission, and DAV at `/dav` (CalDAV calendars and reminders as to-do items, CardDAV contacts; `/.well-known/caldav` and `/.well-known/carddav` redirect to it). All take a mailbox address and an app password, never the account password. App passwords: `CreateMailboxAppPassword(mailboxId, name)`. The addresses to advertise: `GetMailProgramSettings` (`imapHost imapPort imapsPort submissionHost submissionPort davHost`).
- Sign-in for programs: OAuth in `internal/api/v1api/apioauth` (metadata at `/.well-known/oauth-authorization-server`, `POST /oauth/register`, `POST /oauth/token`, `POST /oauth/revoke`; the consent page is the dashboard's `/oauth/authorize`, `web/src/pages/authorize.tsx`). PKCE with S256 is required. Access tokens last 30 days; refresh tokens rotate.

## Plan of Work

### Milestone 0: a build pipeline with no Mac, and the person's Apple setup

The person has no Mac. At the end of this milestone, a pull request that touches `ios/` builds the app and runs its tests on a GitHub macOS runner, and the person has what Apple requires.

The person, not the agent, does these in their own Apple account; write them as a checklist in `ios/README.md`:

- Join the Apple Developer Program.
- Register the bundle IDs `com.teanode.app`, `com.teanode.app.share`, `com.teanode.app.notification` and `com.teanode.app.widgets`, and the app group `group.com.teanode.app`. A person who builds the app under another account uses their own prefix; every ID is set in one file, `ios/Configuration/Identity.xcconfig`, which is not committed (`Identity.example.xcconfig` is).
- Create an APNs key (`.p8`) and note its key ID and the team ID.
- Create an App Store Connect API key for uploads from CI (Milestone 10).

The agent writes:

- `ios/project.yml`, an XcodeGen description of the project. XcodeGen turns this text file into the `.xcodeproj`, so the project is edited and reviewed as text on Linux; the generated project is not committed. CI installs XcodeGen with Homebrew.
- `ios/TeaNodeKit/`, a Swift package with everything that is not UI: the API client, the websocket protocol, the models, the sign-in, the keychain and the notification decryption. Its tests run with `swift test`.
- A first app target that shows "Hello" so the pipeline has something to build.
- `.github/workflows/ios.yml`: on pull requests touching `ios/`, on `macos-latest` with the current Xcode, run `xcodegen`, `xcodebuild build` for the simulator, and `xcodebuild test`. No signing is needed for a simulator build.

Prototype in this milestone: send one notification from a Go test program on the person's machine to a development build on the person's phone. TestFlight needs Milestone 10, so this prototype uses a development build installed from a rented Mac or from Xcode Cloud. Record which, and what it cost, in Surprises & Discoveries. It proves the key, the topic and the token before Milestone 2 builds on them.

### Milestone 1: an app can sign in and get a token for the whole API

At the end, `curl` can go through registration, authorization and token exchange with the redirect `com.teanode.app:/oauth` and scope `api`, and the token it gets can call `/api/v1/graphql`.

- In `apioauth/register.go`, `usableRedirect` also accepts a private-use scheme: a scheme containing a dot, followed by `:/` and a path, with no host. Any other non-`https` scheme is still refused.
- In `apioauth/metadata.go`, list the scopes `mcp` and `api`.
- In `apioauth/oauth.go`, the requested scope decides the token's resource: `mcp` keeps `/api/v1/mcp`; `api` sets no resource restriction, which is what `allowsResource` already allows for an ordinary API token. Store the scope on the authorization code so it cannot change between the consent page and the exchange.
- On the consent page (`web/src/pages/authorize.tsx`), a request for `api` says "<client name> will be able to do everything you can do on this server, as you", with the redirect's scheme shown. `ReadOAuthAuthorizationRequest` returns the scope so the page can say it.
- An `api` token is listed among the person's tokens (`ListTokens`) under the client's name, and deleting it there ends the app's sign-in. Refreshing keeps the same name.
- Tests in `apioauth`: a private-use scheme registers and anything else odd is refused (`javascript:`, `file:`, a scheme without a dot, a scheme with a host); an `api` token works on GraphQL and an `mcp` token still does not; the scope cannot be changed at the exchange.
- Add the change to `docs/security/security-review.md` as an open item until reviewed, then review it (a subagent review is enough) and close it.

### Milestone 2: phones as devices, and notifications sent to them

At the end, `teanode agent phone list` shows a registered phone, `teanode agent phone test <id>` makes it buzz, and an alert written into the main conversation reaches it.

Configuration (`internal/config/agent.go`, documented like every field): `agent.push.apns` with `keyId`, `teamId`, `topic` (the bundle ID), `key` (the `.p8` contents, sealed like other secrets) and `isSandbox`. With no key, phones can still register; nothing is sent, and the dashboard says why.

Storage, a new migration (`docs/coding/database-migrations.md`): `agent_phones` with `id`, `agent_id`, `name` (as the phone calls itself), `model`, `token_id` (the API token it signed in with), `push_token`, `is_sandbox`, `payload_key` (sealed), `notification_kinds` (which kinds it wants; all by default), `created_at`, `last_seen_at`. Deleting the token deletes the phone, so ending a sign-in ends its notifications.

GraphQL (`internal/api/v1api/apigraph/agent_phone.go`):

- `RegisterAgentPhone(name, model, pushToken, isSandbox, payloadKey)`: called by the app after sign-in and whenever iOS hands it a new push token. It is keyed on the calling token, so registering again updates the same row.
- `ListAgentPhones` returns `{id name model createdAt lastSeenAt notificationKinds isPushConfigured}`.
- `UpdateAgentPhone(phoneId, name, notificationKinds)`.
- `RemoveAgentPhone(phoneId)` deletes the phone and its token.
- `TestAgentPhone(phoneId)` sends "TeaNode can reach this phone".

Command line (`internal/cmd/agent_phone.go`): `teanode agent phone list | rename | notify | remove | test`, the same operations. Dashboard: a "Phones" card in the agent's settings beside the computers, with the same operations, outcomes as toasts.

Sending (`internal/agent/push.go` and `internal/agent/push_apns.go`):

- The APNs client signs a JWT with the key (ES256; the header names the key ID, the claims the team ID and the time), keeps it for 50 minutes, and posts over one HTTP/2 connection. A `410` answer means the token is dead: clear it. A `429` or `5xx` is retried with backoff, at most three times.
- The payload, before encryption: `{kind, conversationId, runId, callId, title, body, buttons, threadId}`. `buttons` is a list of `{label, action}` where `action` is what pressing it does (`approve`, `deny`, `answer:<option number>`, `reply`, `open`). It is encrypted with AES-GCM using the phone's `payload_key`, and sent as `{"aps":{"alert":{"title":"TeaNode","body":"New message"},"mutable-content":1,"thread-id":...},"sealed":"<base64 nonce + ciphertext>"}`. The visible fallback says nothing about the content.
- What sends, by `kind`:
  - `alert`, `herdr_question`, `goal_needs_you`, `speak_first`: in `Agent.publish` (`internal/agent/feed.go`), an `asked` event on the main conversation whose `note` is one of these surfaces calls `self.push.notify(...)` after the messages are written. The body is the text written to the conversation.
  - `confirmation` and `question`: in `raiseInteraction`, when a card is saved and nobody is present (`ReportAgentPresence` says no tab or app is visible). Buttons: Approve and Deny, or the question's options.
  - `answer`: an answer to a turn the person started from the app finished while the app was not visible.
- Present means not notified: if a dashboard tab or the app reported itself visible in the last minute, nothing is pushed for that conversation.
- Tests: a fake APNs server (`httptest` with HTTP/2) checks the JWT, the headers, the encrypted body that the test decrypts again, the retry and the dead token; and each `kind` is sent once, not twice when a tab is visible.

Parity: `TestPhoneParity` (like `TestHerdrParity`) checks that each phone operation exists on GraphQL, the command line and the dashboard. The agent's tools are left out by decision.

### Milestone 3: the setup profile

At the end, the person presses "Set up an iPhone or Mac" on a mailbox page, opens the downloaded file on the phone, installs it in Settings, and the phone's Mail, Calendar, Contacts and Reminders show their TeaNode data.

- `CreateMailboxSetupProfile(mailboxId, deviceName)` creates an app password named after the device (`CreateMailboxAppPassword`), builds the profile, and returns a one-time download address `/api/v1/mailbox/profile/<random>.mobileconfig` that works for ten minutes, once. The profile is never stored.
- The profile (`internal/mailbox/profile.go`) is an Apple property list with three payloads, all from `GetMailProgramSettings`: `com.apple.mail.managed` (IMAP on the TLS port, SMTP submission, the address and the app password), `com.apple.caldav.account` (`davHost`, the principal URL `/dav/<userId>/`), and `com.apple.carddav.account`. It is served as `application/x-apple-aspen-config`. It is not signed, so Settings shows it as "Not verified"; signing it with the server's certificate is a later step.
- Command line: `teanode mailbox profile <mailbox> --device "<name>" --out <file>`. The app: Settings, "Add mail, calendars and contacts to this iPhone" opens the download address in Safari, which hands it to Settings (an app cannot install a profile itself).
- Tests: the plist parses, has the three payloads with the advertised hosts and ports, and the download works once.

### Milestone 4: the app: sign in, conversations, chat, cards, pictures, location

At the end, the person signs in on their phone, picks a conversation, and talks to their agent as in the drawer.

Layout of `ios/`:

- `TeaNodeKit/` (Swift package): `Server` (the address, the token and its refresh, in the keychain shared through the app group), `SignIn` (registration, PKCE, `ASWebAuthenticationSession`, exchange), `GraphQL` (a POST client and the `graphql-ws` websocket with reconnect and replay, following `web/src/api.ts`: back off up to 30 s, treat 15 s of silence as a dead connection, re-read the conversation before subscribing again), `Models` (Codable conversation, message, event, card), `Transcript` (turns messages and events into lines the way `linesOf` and `applyEvent` in `agentDrawer.tsx` do), `Upload` (multipart, converting HEIC to JPEG and shrinking to at most 2048 pixels a side before upload).
- `TeaNode/` (the app): screens for servers, conversations, a conversation, and settings.
- `Operations/`: every GraphQL operation the app sends, one `.graphql` file each.

The app:

- Signs in with the server's address. Several servers, like the command line's profiles.
- Lists conversations, main first. A conversation shows its lines: what the person said, the answer streamed as it is written, tool lines collapsed, notes, pictures, and check-in lines for messages that begin with a marker (`[alert]`, `[herdr question]`, `[goal needs you]` and the rest listed in `agentDrawer.tsx`).
- Sends with `AskAgent(surface: "ios", drawerId: <this install's ID>)`. Attaches from the camera, Photos and Files. Swipe a line left to reply to it and right to copy it, as in the drawer.
- Shows approval cards, question cards and coding-session question cards with buttons, from live events and from `ListAgentInteractions`.
- Answers `locate` events addressed to its `drawerId` with Core Location while in the foreground, and with an error message ("the app is not open") otherwise.
- Reports presence with `ReportAgentPresence` when it comes to the foreground and leaves it.
- Follows the system's light and dark appearance and text size, and speaks English, Chinese and Japanese like the dashboard, from the same catalogs where the strings are the same.

Server side for this milestone: the surfaces `ios`, `ios_voice` and `siri` in `internal/agent/surface.go`, with tests like the existing ones.

Schema check: `TestIOSOperationsMatchTheSchema` in `apigraph` reads every `ios/Operations/*.graphql`, parses it with `graphql-go`'s parser, and validates it against the built schema. A server change that breaks the app fails the server's tests.

### Milestone 5: notifications in the app, and answering from them

At the end, a locked phone shows "Claude Code in greenfinch-site asks: ..." with the options as buttons, and pressing one answers the question.

- The app asks for notification permission after sign-in, registers for remote notifications, and calls `RegisterAgentPhone` with the push token and a new 256-bit payload key, kept in the shared keychain.
- `TeaNodeNotification`, the notification service extension, decrypts `sealed`, sets the title, body and thread, and sets the category. Categories are fixed at launch: `approval` (Approve, Deny), `reply` (a text field), `open`. A question's options are not known in advance, so the extension registers a category for that notification's options just before handing it over (`setNotificationCategories`, adding to the fixed ones). Milestone 0's prototype or this milestone confirms that iOS shows a category registered this late; if not, the fallback is a notification content extension that draws the options as buttons.
- Pressing a button runs in the background without opening the app: `ResolveAgentConfirmation`, `AnswerAgentQuestion`, `AnswerAgentHerdrQuestion` or `AskAgent` (for a reply), using the shared token. Approve requires the phone to be unlocked (`authenticationRequired`).
- Pressing the notification opens the conversation.
- Settings in the app: which kinds to receive (`UpdateAgentPhone`).

### Milestone 6: the share extension

At the end, choosing TeaNode in the share sheet of Photos, Safari or Files sends the item to a conversation, with an optional line of text.

- `TeaNodeShare` shows a small sheet: the conversation (main by default), a text field, Send.
- Photos are converted and shrunk as in Milestone 4; other files are sent as they are, up to the server's limit. Links and text become the message.
- It uploads, then calls `AskAgent(surface: "ios")`, and closes without waiting for the answer. The answer arrives as a notification (`answer`) if the app is not open. A share extension may use at most about 120 MB of memory, so photos are decoded downsampled (`CGImageSourceCreateThumbnailAtIndex`), never at full size.

### Milestone 7: voice calls

At the end, the person starts a call in the app, locks the phone, and keeps talking, with AirPods if they like.

- The call is a CallKit call (`CXProvider`, `CXStartCallAction`), so iOS treats it as a call: it keeps the microphone with the screen locked, shows the call in the status bar and on the lock screen, routes to AirPods and the car, and is interrupted properly by a phone call.
- Audio: `AVAudioEngine` captures, converts to mono 16-bit PCM at 24000 Hz, and sends frames of at most 64 KB on `/api/v1/agent/voice`, following the protocol in Context and Orientation and the drawer's `voiceSession.ts`. Each `transcriptFinal` is sent as a turn with `AskAgent(surface: "ios_voice")`. The answer is spoken with `speakAnswer` a sentence at a time; `answerAudio` is played through a player node; the person talking (`speechStarted`) stops it with `cancelAnswer`, as the drawer does.
- Mute is the call's own mute.
- The app's `Info.plist` asks for the `audio` and `voip` background modes. Only outgoing calls: there is no incoming call, so no PushKit.

### Milestone 8: Siri and Shortcuts

At the end, "Hey Siri, ask Tea what's on tomorrow" answers out loud, and the Action button can be set to talk to Tea.

- `AskTeaIntent(question)`: calls `AskAgent(surface: "siri")` on the main conversation and waits for `done` with `ReadAgentRun(wait)` for up to 25 seconds. If the answer came, Siri says it. If not, Siri says "Tea is still working on it; I'll tell you when it's done", and the answer arrives as a notification.
- `SendToTeaIntent(file, text)`: what the share extension does, for Shortcuts.
- `OpenConversationIntent(conversation)` and `CallTeaIntent`, which starts a call (Milestone 7).
- An `AppShortcutsProvider` gives each phrases, so they work without setup and appear for the Action button.
- The `siri` surface asks for one or two short sentences with no formatting, which is what Siri reads well.

### Milestone 9: widgets and Live Activities

At the end, a home screen widget shows how many approvals and questions are waiting, and a coding session that is working shows on the lock screen until it finishes.

- `TeaNodeWidgets` (WidgetKit): "Waiting for you" (the count of open cards, the newest one's line, a tap opens it) and "Ask Tea" (a button that opens the app ready to type). The widget reads a small summary the app and the notification extension write to the app group whenever a card or notification arrives, and is reloaded through `WidgetCenter`. A new query, `CountAgentOpenInteractions`, gives the app the count across conversations; it is added to the command line (`teanode agent conversation todo` shows the steps; this count goes in `teanode agent interactions`) for parity.
- Live Activities (ActivityKit) for two things: an agent turn the person started from the app that is still running after they leave the app, and a coding session the person asked to be told about (`herdr watch`). The app starts the activity and registers its push token with `RegisterAgentLiveActivity(phoneId, activityKind, target, pushToken)`. The server sends `liveactivity` pushes as the turn or session changes state and ends the activity when it finishes. Live Activity content is visible on the lock screen and cannot be encrypted the way alerts are, so it carries only a state and a short title ("Working", "greenfinch-site"), never content.

### Milestone 10: TestFlight from CI, documentation, decision record, release

At the end, merging to `main` uploads a build to TestFlight, and the person installs it on their phone.

- `.github/workflows/ios.yml` gains a job on `main` that signs with a distribution certificate and profile kept as repository secrets, archives, and uploads with the App Store Connect API key (`xcrun altool` or `xcodebuild -exportArchive` with `destination: upload`). If keeping the certificate in GitHub secrets proves fragile, Xcode Cloud does the same from App Store Connect with Apple holding the signing; record which was used and why.
- `docs/reference/ios-app.md`: what the app does, setting up APNs on the server, the setup profile, and building it yourself with your own IDs.
- `docs/decisions/<date>-the-phone-is-a-device-the-person-carries.md`: the decisions above that the person confirmed.
- `docs/subsystems/` pages that mention surfaces, notifications or devices are updated.
- README: a short paragraph and a screenshot of an invented conversation.

## Concrete Steps

Server work runs from the repository root as everywhere else in this repository (`docs/reference/local-development.md`): `make test`, `set -o pipefail; make lint-ci`. A single package with a database: start PostgreSQL in Docker, set `TEANODE_TEST_DATABASE_HOST`, run `go test -mod=vendor ./internal/<package>/`.

App work, where Swift is installed on Linux:

    cd ios/TeaNodeKit
    swift test

This runs the parts of `TeaNodeKit` that need only Foundation: the models, the transcript, the GraphQL messages. Code that uses Apple-only frameworks (the keychain, `CryptoKit`, `AuthenticationServices`) is behind `#if canImport(...)` and is tested on the macOS runner:

    cd ios
    xcodegen
    xcodebuild -scheme TeaNode -destination 'platform=iOS Simulator,name=iPhone 16' test

## Validation and Acceptance

Each milestone ends with its own check above. The whole is accepted when, on the person's own phone with a TestFlight build and their own server:

1. Sign in with the server's address, and see the main conversation.
2. Ask "what is on tomorrow?" and watch the answer stream.
3. Lock the phone. From a computer, have a coding session ask a question. The phone shows it with the options; press one; the pane goes on.
4. Share a photo of a receipt from Photos to TeaNode. The agent files it.
5. Start a call, lock the phone, ask something, hear the answer.
6. "Hey Siri, ask Tea what time it is in Tokyo." Siri answers.
7. The widget shows a waiting approval; approving it from the notification clears it.
8. Install the setup profile; the Mail app shows the TeaNode inbox.

## Idempotence and Recovery

Registering a phone again updates its row. A dead push token is cleared on Apple's `410` and replaced when the app next registers. Removing a phone or deleting its token stops everything for it. The migration only adds a table; rolling back the server leaves it unused. The setup profile is never stored; a lost one is replaced by making another and deleting the old app password.

## Artifacts and Notes

The notification payload, decrypted:

    {"kind":"herdr_question","conversationId":"...","title":"Claude Code in greenfinch-site",
     "body":"How should the pricing table look on a phone?",
     "buttons":[{"label":"Stacked cards","action":"answer:1"},
                {"label":"A table that scrolls sideways","action":"answer:2"},
                {"label":"Open","action":"open"}],
     "threadId":"herdr:studio:w1:p2"}

## Interfaces and Dependencies

Server, Go:

    // internal/agent/push.go
    type PushNotification struct {
        Kind           string // alert, herdr_question, goal_needs_you, speak_first, confirmation, question, answer
        ConversationID string
        RunID          string
        CallID         string
        Title          string
        Body           string
        Buttons        []PushButton
        ThreadID       string
    }
    type PushButton struct {
        Label  string
        Action string // approve, deny, answer:<option number>, reply, open
    }
    func (self *Agent) notifyPhones(ctx context.Context, notification *PushNotification) error

    // internal/agent/push_apns.go
    type apnsClient struct { /* key, keyId, teamId, topic, isSandbox, cached JWT, http client */ }
    func (self *apnsClient) send(ctx context.Context, pushToken string, pushType string, body []byte) error

GraphQL, new: `RegisterAgentPhone`, `ListAgentPhones`, `UpdateAgentPhone`, `RemoveAgentPhone`, `TestAgentPhone`, `RegisterAgentLiveActivity`, `CountAgentOpenInteractions`, `CreateMailboxSetupProfile`. Changed: OAuth registration and the `api` scope.

App, Swift: Apple frameworks only (SwiftUI, Foundation, AuthenticationServices, Security, UserNotifications, PhotosUI, CoreLocation, AVFoundation, CallKit, AppIntents, WidgetKit, ActivityKit, CryptoKit). Build tool: XcodeGen, on the CI runner only.

## Outcomes & Retrospective

Nothing yet.
