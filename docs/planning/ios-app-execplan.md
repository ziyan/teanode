# An iPhone app for talking to your agent

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the ExecPlan rules the repository's planning documents share (see the other files in `docs/planning/`).

## Purpose / Big Picture

Today a person reaches their TeaNode agent from a phone through the dashboard in Safari, a Telegram or Discord bot, or an MCP client such as the ChatGPT app. The dashboard works, but a browser tab cannot do what a phone is for:

- It cannot notify. When mail shows something that cannot wait, when a coding session stops to ask a question, or when the agent needs approval, nothing on the phone lights up unless a chat bot is linked.
- It cannot receive. A receipt photographed in the Photos app has to be saved, then uploaded through the drawer's paperclip. iPhone photos are HEIC, which the server does not read as a picture at all.
- It cannot keep a call going. A voice call in the drawer stops when the screen locks.
- It cannot be asked from Siri, the Action button, a widget or the lock screen.

After this change, anyone who runs a TeaNode server downloads the TeaNode app from the App Store, connects it to their own server, and:

- talks to their agent in the main conversation and the named ones, with the same streamed answers, tool lines, approval cards and coding-session question cards as the drawer;
- gets a notification for each alert, approval, coding-session question and goal that needs them, and can answer from the notification itself ("Approve", "Option 2") without opening the app;
- shares a photo, screenshot, PDF, link or text into a conversation from any app;
- calls their agent and keeps talking with the screen locked, through AirPods, and later in the car;
- says "Ask Tea ..." to Siri, or presses the Action button;
- sees waiting approvals in a widget, and a working coding session on the lock screen;
- installs one setup profile that adds their TeaNode mailbox, calendars, contacts and reminders to the iPhone's own Mail, Calendar, Contacts and Reminders apps;
- is helped through all of it by their agent: asked in the dashboard to "connect my phone", it shows a code to scan; once the phone is connected, it walks them through notifications, the share sheet, Siri and the setup profile, offering only what their server has turned on.

The app is published once, by the project, and works with every server. Notifications reach it through a push relay the project runs: a small service that holds the app's Apple push key and passes on notifications it cannot read.

To see it working, once complete: install the app, scan the code the dashboard shows, lock the phone, and have a coding session on an attached computer ask a question. The phone shows a notification with the options as buttons; pressing one answers the question in the pane.

Mail, calendar, contacts, finance and memory get no screens of their own in the app. Mail, calendars, contacts and reminders already reach the iPhone's own apps over IMAP, CalDAV and CardDAV, which the server serves (Milestone 4 makes that one tap). Finance and memory are reached by asking the agent, or through the dashboard in Safari.

## Progress

- [x] (2026-10-10) Researched what the server offers a native client: sign-in, the GraphQL API and its websocket, cards, attachments, voice, location, where unasked messages are delivered, devices, DAV and IMAP. Chose the scope with the person. Wrote this plan.
- [x] (2026-10-10) Revised after review by the person: app tokens have full access; the app is for anyone with a server, so notifications go through a push relay from the start; the agent helps the person connect and learn the app.
- [ ] Milestone 0: a build pipeline with no Mac, and the Apple developer setup.
- [ ] Milestone 1: an app can sign in, by OAuth or by a pairing code, and get a token for the whole API.
- [ ] Milestone 2: phones as devices, and notifications sent to them.
- [ ] Milestone 3: the push relay.
- [ ] Milestone 4: the setup profile.
- [ ] Milestone 5: the app: connect, conversations, chat, cards, pictures, location.
- [ ] Milestone 6: notifications in the app, and answering from them.
- [ ] Milestone 7: the share extension.
- [ ] Milestone 8: voice calls.
- [ ] Milestone 9: Siri and Shortcuts.
- [ ] Milestone 10: widgets and Live Activities.
- [ ] Milestone 11: the agent helps the person onboard.
- [ ] Milestone 12: TestFlight and the App Store, documentation, decision record.

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
  Rationale: the repository is public, so GitHub's macOS runners build it for free (the person has no Mac). A server change and the app change that uses it land in one pull request, and a test can check the app's queries against the server's schema (Milestone 5).
  Date/Author: 2026-10-10, agent.

- Decision: the app does not re-implement mail, calendar, contacts, finance or memory.
  Rationale: the iPhone's own apps already read mail, calendars, contacts and reminders from the server. Native screens for the rest would be a large surface to keep in parity with the dashboard and the command line for little gain over asking the agent.
  Date/Author: 2026-10-10, the person and agent.

- Decision: an app signs in through the server's existing OAuth, extended with a second scope, `api`, that grants a token for the whole API as the person; and registration accepts a private-use scheme, `com.teanode.app:/oauth` (a reverse domain name with one slash, RFC 8252 section 7.1), as a redirect.
  Rationale: the CLI's loopback sign-in (`teanode auth login`) cannot be used, because an iPhone app cannot rely on a loopback listener while the sign-in sheet is open. Handing a token back through a scheme is safe only with PKCE, which the OAuth flow already requires. A scheme is the only redirect a self-hosted server can use.
  Consequences: a token with scope `api` is listed among the person's tokens with the app's name and can be deleted there, like the CLI's. The consent page must say plainly that this grants everything the person can do. The `mcp` scope keeps its narrow resource. This needs the security review in Milestone 1.
  Date/Author: 2026-10-10, agent; full access confirmed by the person.

- Decision: a phone can also be connected by pairing, without typing a password on it. The app asks the server for a pairing (OAuth's device authorization grant, RFC 8628): the server answers with a short code, like `KQ7M-4TXD`, valid for ten minutes. The person approves that code where they are already signed in: the dashboard's "Connect a phone" page, `teanode agent phone approve <code>`, or by telling their agent the code. The app, polling the token endpoint, then receives the same `api` token as the OAuth sign-in gives. The reverse also works: the dashboard shows a QR code holding the server's address, so the app never needs it typed.
  Rationale: typing a server address and a strong password, and passing a second factor, on a phone is where people give up. A code approved on the computer they are already signed in to removes all three. The code is not a secret worth stealing: alone it grants nothing, and approving it needs the person's own sign-in and, when the agent does it, their confirmation.
  Date/Author: 2026-10-10, agent, from the person's request that the agent help onboard.

- Decision: the app is published once, on the App Store, by the project, and works with every TeaNode server. Its bundle ID and its Apple push key belong to the project's developer account.
  Rationale: the person's goal: anyone downloads the app and uses it with the server they deployed.
  Date/Author: 2026-10-10, the person.

- Decision: servers send notifications through a push relay the project runs, at `https://push.teanode.com` by default. The relay holds the app's APNs key and nothing else. It keeps no database: what a server holds for a phone is a sealed registration, the phone's device token encrypted and signed by the relay's own secret, which only the relay can open.
  - The app registers its device token with the relay (`POST /v1/registrations`) and gets back the sealed registration. It gives that, never the raw token, to its own server.
  - The server sends `POST /v1/notifications` with the sealed registration and the encrypted payload. The relay opens the registration, posts to APNs, and passes back what APNs said: a `410` tells the server the phone is gone.
  - The relay limits each registration's rate, and keeps only counts.
  - An operator who builds and signs the app themselves can instead give their server their own APNs key (`agent.push.apns`), and it sends directly.
  Rationale: Apple accepts notifications for the App Store app only from the project's key, which cannot be handed to every server. A relay that holds the key and forwards is how other self-hosted projects with App Store apps do it. Sealing the registration means the relay stores nothing, and a server can only reach the phones that registered with it, because nobody else has their sealed registrations.
  Date/Author: 2026-10-10, the person (a relay from the start); design by agent.

- Decision: neither Apple nor the relay sees content. Every notification is encrypted for the phone it goes to. The phone generates a key when it registers and hands it to the server, which stores it sealed (like other secrets). A notification carries only the ciphertext and `mutable-content: 1`. The app's notification service extension decrypts it on the phone and fills in the title, body and buttons.
  Rationale: what an alert says is the person's own mail. The same reasoning made the ChatGPT provider send `store: false` (`internal/llm/codex_wire.go`). The key goes from the phone to its own server only; the relay never has it.
  Date/Author: 2026-10-10, agent.

- Decision: the agent helps the person onboard, and has a `phone` tool for it, in parity with the dashboard and the command line. Its actions:
  - `list`: the person's phones, whether the server can notify them (relay or key configured, last delivery, last error), which kinds each receives, when each was last seen. This answers "why does my phone not buzz?".
  - `pair`: show a card with a QR code and a link that opens the app (or the App Store) with this server's address filled in.
  - `approve`: approve a pairing code the person read out from their phone. Granting risk: it needs the person's confirmation, like any granting call.
  - `rename`, `notify` (which kinds), `test`, and `remove` (destructive, so confirmed).
  - `setup_profile`: hand the person a setup profile for a mailbox (Milestone 4). Granting risk, confirmed.
  Rationale: the person asked for the agent to help onboard. Connecting a phone and learning what it can do is a conversation: "connect my phone", "why no notifications?", "how do I send it receipts?".
  Date/Author: 2026-10-10, the person (the agent helps onboard); actions by agent.

- Decision: what grants access is shown to the person and never to the model. The QR card, the setup profile's download link and the profile's app password are rendered by the drawer and the app as a private card. The tool's result tells the model only that the card was shown and when it expires, and the card is stored with the message but left out when the conversation is sent to the model.
  Rationale: whatever the model reads goes to the model's provider. A download link for a profile is an app password in all but name for ten minutes. The person sees and taps it; the model only needs to know it was offered.
  Date/Author: 2026-10-10, agent.

- Decision: when a phone connects for the first time, the agent speaks first in the app: a short tour of what this server lets the phone do, one thing at a time, built from what is turned on (notifications always; voice calls if voice is available; the coding-session notifications if a computer with herdr is attached; sending receipts if Finance is on; the setup profile if the person has a mailbox). A person new to TeaNode altogether gets the existing introduction (`speak_first_onboarding.go`) first, in the app.
  Rationale: features nobody finds are features nobody uses, and the agent knows what the server has turned on where a static tour would not.
  Date/Author: 2026-10-10, agent.

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
- The device authorization grant (RFC 8628) is OAuth's way to sign in a device by approving it somewhere else: the device gets a short code, the person approves the code where they are signed in, and the device, which has been asking the token endpoint every few seconds, then gets its token. Televisions use it. Here it is called pairing.
- The push relay is a small service the project runs that holds the app's APNs key and forwards notifications from any TeaNode server to the phones registered with it (Milestone 3).

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

### Milestone 0: a build pipeline with no Mac, and the Apple developer setup

The person has no Mac. At the end of this milestone, a pull request that touches `ios/` builds the app and runs its tests on a GitHub macOS runner, and the person has what Apple requires.

The person, not the agent, does these in the project's Apple developer account, which publishes the app; write them as a checklist in `ios/README.md`:

- Join the Apple Developer Program.
- Register the bundle IDs `com.teanode.app`, `com.teanode.app.share`, `com.teanode.app.notification` and `com.teanode.app.widgets`, and the app group `group.com.teanode.app`. A person who builds the app under another account uses their own prefix; every ID is set in one file, `ios/Configuration/Identity.xcconfig`, which is not committed (`Identity.example.xcconfig` is).
- Create an APNs key (`.p8`) and note its key ID and the team ID. It goes to the relay (Milestone 3), not to any TeaNode server.
- Create an App Store Connect API key for uploads from CI (Milestone 12).
- Register the associated domain `teanode.com` for the app, for the links in Milestone 11.

The agent writes:

- `ios/project.yml`, an XcodeGen description of the project. XcodeGen turns this text file into the `.xcodeproj`, so the project is edited and reviewed as text on Linux; the generated project is not committed. CI installs XcodeGen with Homebrew.
- `ios/TeaNodeKit/`, a Swift package with everything that is not UI: the API client, the websocket protocol, the models, the sign-in, the keychain and the notification decryption. Its tests run with `swift test`.
- A first app target that shows "Hello" so the pipeline has something to build.
- `.github/workflows/ios.yml`: on pull requests touching `ios/`, on `macos-latest` with the current Xcode, run `xcodegen`, `xcodebuild build` for the simulator, and `xcodebuild test`. No signing is needed for a simulator build.

Prototype in this milestone: send one notification from a Go test program on the person's machine to a development build on the person's phone. TestFlight needs Milestone 12, so this prototype uses a development build installed from a rented Mac or from Xcode Cloud. Record which, and what it cost, in Surprises & Discoveries. It proves the key, the topic and the token before Milestone 2 builds on them.

### Milestone 1: an app can sign in, by OAuth or by a pairing code, and get a token for the whole API

At the end, `curl` can go through registration, authorization and token exchange with the redirect `com.teanode.app:/oauth` and scope `api`, and the token it gets can call `/api/v1/graphql`. And `curl` can start a pairing, `teanode agent phone approve <code>` approves it, and the polling `curl` receives a token.

OAuth sign-in:

- In `apioauth/register.go`, `usableRedirect` also accepts a private-use scheme: a scheme containing a dot, followed by `:/` and a path, with no host. Any other non-`https` scheme is still refused.
- In `apioauth/metadata.go`, list the scopes `mcp` and `api`.
- In `apioauth/oauth.go`, the requested scope decides the token's resource: `mcp` keeps `/api/v1/mcp`; `api` sets no resource restriction, which is what `allowsResource` already allows for an ordinary API token. Store the scope on the authorization code so it cannot change between the consent page and the exchange.
- On the consent page (`web/src/pages/authorize.tsx`), a request for `api` says "<client name> will be able to do everything you can do on this server, as you", with the redirect's scheme shown. `ReadOAuthAuthorizationRequest` returns the scope so the page can say it.
- An `api` token is listed among the person's tokens (`ListTokens`) under the client's name, and deleting it there ends the app's sign-in. Refreshing keeps the same name.
- Tests in `apioauth`: a private-use scheme registers and anything else odd is refused (`javascript:`, `file:`, a scheme without a dot, a scheme with a host); an `api` token works on GraphQL and an `mcp` token still does not; the scope cannot be changed at the exchange.

Pairing (OAuth's device authorization grant, RFC 8628):

- `POST /oauth/device_authorization` with `client_id` and `scope=api` answers `{device_code, user_code, verification_uri, verification_uri_complete, expires_in, interval}`. The user code is eight characters from an alphabet without look-alikes (no 0, O, 1, I), shown as `KQ7M-4TXD`; it lasts ten minutes; the device code is a long random secret only the app holds. List the endpoint and the grant type in the metadata.
- `POST /oauth/token` with `grant_type=urn:ietf:params:oauth:grant-type:device_code` answers `authorization_pending` until approved, `slow_down` if polled faster than `interval`, `access_denied` if refused, `expired_token` after ten minutes, and the `api` token once approved. A device code is spent when its token is issued.
- Approving: `ApproveAgentPhonePairing(userCode)` as the signed-in person, from the dashboard page `/connect` (which `verification_uri` points at; `verification_uri_complete` fills in the code), from `teanode agent phone approve <code>`, and from the `phone` tool's `approve` (Milestone 11). `ReadAgentPhonePairing(userCode)` says which app and device name asked, so the page and the confirmation card can show "iPhone (TeaNode app) wants to connect as you".
- Wrong codes are counted by the login limiter, like passwords, so a code cannot be guessed.

- Add both changes to `docs/security/security-review.md` as an open item until reviewed, then review them (a subagent review is enough) and close the item.

### Milestone 2: phones as devices, and notifications sent to them

At the end, `teanode agent phone list` shows a registered phone, `teanode agent phone test <id>` makes it buzz, and an alert written into the main conversation reaches it. Until Milestone 3 runs a relay, this milestone is tested against a fake relay and with a direct APNs key.

Configuration (`internal/config/agent.go`, documented like every field): `agent.push.relayUrl`, `https://push.teanode.com` by default; and, for an operator who builds the app themselves, `agent.push.apns` with `keyId`, `teamId`, `topic` (the bundle ID), `key` (the `.p8` contents, sealed like other secrets) and `isSandbox`. A key, when set, is used instead of the relay. `agent.push.isEnabled` turns notifications off altogether.

Storage, a new migration (`docs/coding/database-migrations.md`): `agent_phones` with `id`, `agent_id`, `name` (as the phone calls itself), `model`, `token_id` (the API token it signed in with), `push_registration` (the relay's sealed registration, or the raw device token when the server sends with its own key), `is_sandbox`, `payload_key` (sealed), `notification_kinds` (which kinds it wants; all by default), `created_at`, `last_seen_at`. Deleting the token deletes the phone, so ending a sign-in ends its notifications.

GraphQL (`internal/api/v1api/apigraph/agent_phone.go`):

- `RegisterAgentPhone(name, model, pushRegistration, isSandbox, payloadKey)`: called by the app after sign-in and whenever iOS hands it a new push token (the app registers the new token with the relay first). It is keyed on the calling token, so registering again updates the same row.
- `ListAgentPhones` returns `{id name model createdAt lastSeenAt notificationKinds isPushConfigured lastDeliveredAt lastDeliveryError}`.
- `UpdateAgentPhone(phoneId, name, notificationKinds)`.
- `RemoveAgentPhone(phoneId)` deletes the phone and its token.
- `TestAgentPhone(phoneId)` sends "TeaNode can reach this phone".

Command line (`internal/cmd/agent_phone.go`): `teanode agent phone list | rename | notify | remove | test | approve`, the same operations. Dashboard: a "Phones" card in the agent's settings beside the computers, with the same operations, outcomes as toasts.

Sending (`internal/agent/push.go`, `internal/agent/push_relay.go` and `internal/push/apns.go`):

- `push_relay.go` posts each notification to the relay (Milestone 3 defines the protocol) and maps its answers the same way as APNs's below.
- `internal/push/apns.go` is shared with the relay. It signs a JWT with the key (ES256; the header names the key ID, the claims the team ID and the time), keeps it for 50 minutes, and posts over one HTTP/2 connection. A `410` answer means the token is dead: clear it. A `429` or `5xx` is retried with backoff, at most three times.
- The payload, before encryption: `{kind, conversationId, runId, callId, title, body, buttons, threadId}`. `buttons` is a list of `{label, action}` where `action` is what pressing it does (`approve`, `deny`, `answer:<option number>`, `reply`, `open`). It is encrypted with AES-GCM using the phone's `payload_key`, and sent as `{"aps":{"alert":{"title":"TeaNode","body":"New message"},"mutable-content":1,"thread-id":...},"sealed":"<base64 nonce + ciphertext>"}`. The visible fallback says nothing about the content.
- What sends, by `kind`:
  - `alert`, `herdr_question`, `goal_needs_you`, `speak_first`: in `Agent.publish` (`internal/agent/feed.go`), an `asked` event on the main conversation whose `note` is one of these surfaces calls `self.push.notify(...)` after the messages are written. The body is the text written to the conversation.
  - `confirmation` and `question`: in `raiseInteraction`, when a card is saved and nobody is present (`ReportAgentPresence` says no tab or app is visible). Buttons: Approve and Deny, or the question's options.
  - `answer`: an answer to a turn the person started from the app finished while the app was not visible.
- Present means not notified: if a dashboard tab or the app reported itself visible in the last minute, nothing is pushed for that conversation.
- Tests: a fake APNs server (`httptest` with HTTP/2) checks the JWT, the headers, the encrypted body that the test decrypts again, the retry and the dead token; a fake relay checks the same through the relay; and each `kind` is sent once, not twice when a tab is visible.

Parity: `TestPhoneParity` (like `TestHerdrParity`) checks that each phone operation exists on GraphQL, the command line, the dashboard and the `phone` tool (the tool comes in Milestone 11, and the test lists it as expected from then).

### Milestone 3: the push relay

At the end, the relay runs at `push.teanode.com`, a TestFlight build registers with it, and a server with no Apple key of its own makes that phone buzz.

The relay is a third program built from this repository, `cmd/teanode-push-relay`, small enough to read in one sitting. It has no database and no configuration beyond its APNs key, its own sealing secret and its address.

- `POST /v1/registrations {deviceToken, isSandbox, pushKind}` (from the app) answers `{registration}`: the device token, the environment, the kind (`alert` or `liveactivity`) and the time, encrypted and authenticated with the relay's secret (AES-GCM), base64url. The relay remembers nothing.
- `POST /v1/notifications {registration, pushType, priority, expiration, collapseId, payload}` (from a server) opens the registration, refuses one it did not make, and posts the payload to APNs with the app's topic (`com.teanode.app`, or `com.teanode.app.push-type.liveactivity`). It answers `200`, `410` (the phone is gone: the server deletes it), `413` (over APNs's 4 KB), `429` (over the rate), or `502` with APNs's reason.
- Limits, in memory: per registration, 60 notifications a minute; per calling address, 600. Payloads are already encrypted for the phone; the relay never logs them, nor tokens, nor registrations, only counts per answer.
- A rotated sealing secret: the relay accepts registrations made with the previous secret for 90 days, and phones register again whenever the app starts, so they move over on their own.
- Tests against a fake APNs: a registration it did not make is refused; one made for the sandbox goes to the sandbox host; a `410` from APNs comes back as `410`; the rate limits.
- Deployment: `docs/reference/push-relay.md` describes running it (a container behind TLS). Where the project runs it is the person's call; record it here when made.
- The privacy policy the App Store requires says what the relay sees: the address of the server calling it, and an encrypted payload it cannot read.


### Milestone 4: the setup profile

At the end, the person presses "Set up an iPhone or Mac" on a mailbox page, opens the downloaded file on the phone, installs it in Settings, and the phone's Mail, Calendar, Contacts and Reminders show their TeaNode data.

- `CreateMailboxSetupProfile(mailboxId, deviceName)` creates an app password named after the device (`CreateMailboxAppPassword`), builds the profile, and returns a one-time download address `/api/v1/mailbox/profile/<random>.mobileconfig` that works for ten minutes, once. The profile is never stored.
- The profile (`internal/mailbox/profile.go`) is an Apple property list with three payloads, all from `GetMailProgramSettings`: `com.apple.mail.managed` (IMAP on the TLS port, SMTP submission, the address and the app password), `com.apple.caldav.account` (`davHost`, the principal URL `/dav/<userId>/`), and `com.apple.carddav.account`. It is served as `application/x-apple-aspen-config`. It is not signed, so Settings shows it as "Not verified"; signing it with the server's certificate is a later step.
- Command line: `teanode mailbox profile <mailbox> --device "<name>" --out <file>`. The `phone` tool's `setup_profile` action shows it as a private card (Milestone 11). The app: Settings, "Add mail, calendars and contacts to this iPhone" opens the download address in Safari, which hands it to Settings (an app cannot install a profile itself).
- Tests: the plist parses, has the three payloads with the advertised hosts and ports, and the download works once.

### Milestone 5: the app: connect, conversations, chat, cards, pictures, location

At the end, the person connects the app to their server, picks a conversation, and talks to their agent as in the drawer.

Layout of `ios/`:

- `TeaNodeKit/` (Swift package): `Server` (the address, the token and its refresh, in the keychain shared through the app group), `SignIn` (registration, PKCE, `ASWebAuthenticationSession`, exchange; and pairing: start, show the code, poll), `GraphQL` (a POST client and the `graphql-ws` websocket with reconnect and replay, following `web/src/api.ts`: back off up to 30 s, treat 15 s of silence as a dead connection, re-read the conversation before subscribing again), `Models` (Codable conversation, message, event, card), `Transcript` (turns messages and events into lines the way `linesOf` and `applyEvent` in `agentDrawer.tsx` do), `Upload` (multipart, converting HEIC to JPEG and shrinking to at most 2048 pixels a side before upload).
- `TeaNode/` (the app): screens for servers, conversations, a conversation, and settings.
- `Operations/`: every GraphQL operation the app sends, one `.graphql` file each.

The app:

- Connects on its first screen, three ways:
  - "Scan the code from your dashboard": the camera reads the QR code the dashboard or the agent shows (`https://teanode.com/app/connect?server=<address>`), which fills in the server. The same link opened on the phone opens the app, or the App Store when the app is not installed (the `apple-app-site-association` file on `teanode.com` claims `/app/*` for the app).
  - "Enter your server's address": the address, then the OAuth sign-in sheet.
  - Either way it then offers both sign-ins: "Sign in here" (the OAuth sheet) or "Approve from your computer" (pairing: the app shows the code in large type and waits).
  - "I don't have a server yet" opens the getting-started guide on `teanode.com`. The app does nothing without a server, and says so before asking for anything.
- Checks the server's version (`ReadServerVersion` or the existing version query) and says plainly when the server is too old for the app, naming the version it needs.
- Keeps several servers, like the command line's profiles.
- Lists conversations, main first. A conversation shows its lines: what the person said, the answer streamed as it is written, tool lines collapsed, notes, pictures, and check-in lines for messages that begin with a marker (`[alert]`, `[herdr question]`, `[goal needs you]` and the rest listed in `agentDrawer.tsx`).
- Sends with `AskAgent(surface: "ios", drawerId: <this install's ID>)`. Attaches from the camera, Photos and Files. Swipe a line left to reply to it and right to copy it, as in the drawer.
- Shows approval cards, question cards and coding-session question cards with buttons, from live events and from `ListAgentInteractions`.
- Answers `locate` events addressed to its `drawerId` with Core Location while in the foreground, and with an error message ("the app is not open") otherwise.
- Reports presence with `ReportAgentPresence` when it comes to the foreground and leaves it.
- Follows the system's light and dark appearance and text size, and speaks English, Chinese and Japanese like the dashboard, from the same catalogs where the strings are the same.

Server side for this milestone: the surfaces `ios`, `ios_voice` and `siri` in `internal/agent/surface.go`, with tests like the existing ones.

Schema check: `TestIOSOperationsMatchTheSchema` in `apigraph` reads every `ios/Operations/*.graphql`, parses it with `graphql-go`'s parser, and validates it against the built schema. A server change that breaks the app fails the server's tests.

### Milestone 6: notifications in the app, and answering from them

At the end, a locked phone shows "Claude Code in greenfinch-site asks: ..." with the options as buttons, and pressing one answers the question.

- The app asks for notification permission after sign-in, registers for remote notifications, registers the device token with the relay (`POST /v1/registrations`), and calls `RegisterAgentPhone` with the relay's sealed registration and a new 256-bit payload key, kept in the shared keychain. It registers again with both whenever it starts, so a rotated device token or relay secret heals on its own.
- `TeaNodeNotification`, the notification service extension, decrypts `sealed`, sets the title, body and thread, and sets the category. Categories are fixed at launch: `approval` (Approve, Deny), `reply` (a text field), `open`. A question's options are not known in advance, so the extension registers a category for that notification's options just before handing it over (`setNotificationCategories`, adding to the fixed ones). Milestone 0's prototype or this milestone confirms that iOS shows a category registered this late; if not, the fallback is a notification content extension that draws the options as buttons.
- Pressing a button runs in the background without opening the app: `ResolveAgentConfirmation`, `AnswerAgentQuestion`, `AnswerAgentHerdrQuestion` or `AskAgent` (for a reply), using the shared token. Approve requires the phone to be unlocked (`authenticationRequired`).
- Pressing the notification opens the conversation.
- Settings in the app: which kinds to receive (`UpdateAgentPhone`).

### Milestone 7: the share extension

At the end, choosing TeaNode in the share sheet of Photos, Safari or Files sends the item to a conversation, with an optional line of text.

- `TeaNodeShare` shows a small sheet: the conversation (main by default), a text field, Send.
- Photos are converted and shrunk as in Milestone 5; other files are sent as they are, up to the server's limit. Links and text become the message.
- It uploads, then calls `AskAgent(surface: "ios")`, and closes without waiting for the answer. The answer arrives as a notification (`answer`) if the app is not open. A share extension may use at most about 120 MB of memory, so photos are decoded downsampled (`CGImageSourceCreateThumbnailAtIndex`), never at full size.

### Milestone 8: voice calls

At the end, the person starts a call in the app, locks the phone, and keeps talking, with AirPods if they like.

- The call is a CallKit call (`CXProvider`, `CXStartCallAction`), so iOS treats it as a call: it keeps the microphone with the screen locked, shows the call in the status bar and on the lock screen, routes to AirPods and the car, and is interrupted properly by a phone call.
- Audio: `AVAudioEngine` captures, converts to mono 16-bit PCM at 24000 Hz, and sends frames of at most 64 KB on `/api/v1/agent/voice`, following the protocol in Context and Orientation and the drawer's `voiceSession.ts`. Each `transcriptFinal` is sent as a turn with `AskAgent(surface: "ios_voice")`. The answer is spoken with `speakAnswer` a sentence at a time; `answerAudio` is played through a player node; the person talking (`speechStarted`) stops it with `cancelAnswer`, as the drawer does.
- Mute is the call's own mute.
- The app's `Info.plist` asks for the `audio` and `voip` background modes. Only outgoing calls: there is no incoming call, so no PushKit.

### Milestone 9: Siri and Shortcuts

At the end, "Hey Siri, ask Tea what's on tomorrow" answers out loud, and the Action button can be set to talk to Tea.

- `AskTeaIntent(question)`: calls `AskAgent(surface: "siri")` on the main conversation and waits for `done` with `ReadAgentRun(wait)` for up to 25 seconds. If the answer came, Siri says it. If not, Siri says "Tea is still working on it; I'll tell you when it's done", and the answer arrives as a notification.
- `SendToTeaIntent(file, text)`: what the share extension does, for Shortcuts.
- `OpenConversationIntent(conversation)` and `CallTeaIntent`, which starts a call (Milestone 8).
- An `AppShortcutsProvider` gives each phrases, so they work without setup and appear for the Action button.
- The `siri` surface asks for one or two short sentences with no formatting, which is what Siri reads well.

### Milestone 10: widgets and Live Activities

At the end, a home screen widget shows how many approvals and questions are waiting, and a coding session that is working shows on the lock screen until it finishes.

- `TeaNodeWidgets` (WidgetKit): "Waiting for you" (the count of open cards, the newest one's line, a tap opens it) and "Ask Tea" (a button that opens the app ready to type). The widget reads a small summary the app and the notification extension write to the app group whenever a card or notification arrives, and is reloaded through `WidgetCenter`. A new query, `CountAgentOpenInteractions`, gives the app the count across conversations; it is added to the command line (`teanode agent conversation todo` shows the steps; this count goes in `teanode agent interactions`) for parity.
- Live Activities (ActivityKit) for two things: an agent turn the person started from the app that is still running after they leave the app, and a coding session the person asked to be told about (`herdr watch`). The app starts the activity, registers the activity's push token with the relay as kind `liveactivity`, and gives the sealed registration to the server with `RegisterAgentLiveActivity(phoneId, activityKind, target, pushRegistration)`. The server sends `liveactivity` pushes as the turn or session changes state and ends the activity when it finishes. Live Activity content is visible on the lock screen and cannot be encrypted the way alerts are, so it carries only a state and a short title ("Working", "greenfinch-site"), never content.

### Milestone 11: the agent helps the person onboard

At the end, the person tells their agent in the dashboard "connect my phone", scans the code it shows, approves the connection by saying yes, and the agent greets them in the app with a tour of what this phone can do here.

The `phone` tool (`internal/agent/tools/phone/phone.go`), its actions as in the Decision Log:

- `list` (read): each phone, whether the server can notify it and why not ("notifications are off on this server", "the relay could not be reached: ...", "Apple says this phone is gone"), the kinds it receives, last seen. The agent uses this to answer "why does my phone not buzz?".
- `pair` (read): shows the private connect card: the QR code, the link, and "or open the TeaNode app and enter <server address>". The model is told "the connect card is shown".
- `approve` (granting, confirmed): approves a pairing code the person reads out. The confirmation card names the app and the device that asked, from `ReadAgentPhonePairing`.
- `rename`, `notify` (write), `test` (write), `remove` (destructive, confirmed).
- `setup_profile` (granting, confirmed): makes a setup profile for a mailbox and shows its download link as a private card.

Private cards (`internal/agent/private_card.go`): a tool result may carry a card for the person with `isPrivate`. The drawer and the app draw it (a QR code, a link, a button). It is stored with the tool's message in a field the transcript builder (`internal/agent/ask.go`, where messages become the model's input) skips; the model's copy of the result says only what was shown and until when. A test builds a conversation with a private card and checks that nothing of it reaches the request sent to the model. The dashboard and the app hide a private card once it expires.

The tour (`internal/agent/speak_first_phone.go`), a speak-first reason like `onboardingReason`:

- It fires once per phone, the first time a phone registers, into the main conversation, with the app's surface, so it arrives in the app (and, being unasked, as a notification once notifications are allowed).
- Its instructions list what this server has on: whether notifications reach the phone (`list`), whether voice is available (`ReadAgentVoice`), whether a computer with herdr is attached, whether Finance is on, whether the person has a mailbox, and whether the setup profile was already made. The agent offers one thing at a time and stops when the person says enough: turning notifications on and choosing kinds, then sending a photo or receipt through the share sheet, then "Hey Siri, ask Tea", then the Action button, then a call, then the setup profile.
- A person whose agent was never introduced gets the existing introduction first, in the app.
- `agent_profile` gains `phone_tour_done`, as it has `onboarding_done`, so the tour can be ended and is not offered twice.

The `situation` the agent is given each turn mentions the person's phones (count, and whether notifications reach them), so "connect my phone" and "is my phone set up?" need no guessing.

Parity: `TestPhoneParity` now includes the tool. Command line: `teanode agent phone pair` prints the link and draws the QR code in the terminal. Dashboard: "Connect a phone" in the Phones card shows the same card.

### Milestone 12: TestFlight and the App Store, documentation, decision record

At the end, merging to `main` uploads a build to TestFlight, and a version is on the App Store for anyone.

- `.github/workflows/ios.yml` gains a job on `main` that signs with a distribution certificate and profile kept as repository secrets, archives, and uploads with the App Store Connect API key (`xcodebuild -exportArchive` with `destination: upload`). If keeping the certificate in GitHub secrets proves fragile, Xcode Cloud does the same from App Store Connect with Apple holding the signing; record which was used and why.
- `teanode.com` serves `/.well-known/apple-app-site-association` (JSON, no redirect) claiming `/app/*` for the app, and `/app/connect`, which on a computer explains what to do and on a phone opens the app or the App Store.
- App Store review needs a server to sign in to: a review server with an invented person, mailbox and conversation, and its address and a pairing approved for the reviewer, given in the review notes. Its data is invented throughout.
- The App Store listing: description, screenshots of invented conversations, the privacy details (the app sends what the person types and shares to the server they connect it to; the relay sees encrypted notifications and the calling server's address; no tracking), and a privacy policy page on `teanode.com`.
- Server version: the release notes name the first server version the app works with.
- `docs/reference/ios-app.md`: what the app does, turning notifications on for a server (nothing to do with the relay; `agent.push.apns` only for one's own build), the setup profile, and building the app yourself with your own IDs.
- `docs/decisions/<date>-the-phone-is-a-device-the-person-carries.md`: the decisions above.
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

1. Ask the agent in the dashboard to connect the phone; scan its code; approve; see the main conversation in the app, and the agent's tour.
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

    // internal/push/apns.go, shared by the server (own key) and the relay
    type APNsClient struct { /* key, keyId, teamId, topic, cached JWT, http client */ }
    func (self *APNsClient) Send(ctx context.Context, deviceToken string, isSandbox bool, pushType string, body []byte) error

    // internal/agent/push_relay.go
    type relayClient struct { /* relay address, http client */ }
    func (self *relayClient) send(ctx context.Context, pushRegistration string, pushType string, body []byte) error

GraphQL, new: `RegisterAgentPhone`, `ListAgentPhones`, `UpdateAgentPhone`, `RemoveAgentPhone`, `TestAgentPhone`, `ReadAgentPhonePairing`, `ApproveAgentPhonePairing`, `RegisterAgentLiveActivity`, `CountAgentOpenInteractions`, `CreateMailboxSetupProfile`. Changed: OAuth registration, the `api` scope, and the device authorization grant.

Push relay, HTTP: `POST /v1/registrations`, `POST /v1/notifications` (Milestone 3). A new program, `cmd/teanode-push-relay`, sharing `internal/push` with the server.

Agent tool: `phone`, with `list`, `pair`, `approve`, `rename`, `notify`, `test`, `remove`, `setup_profile`. Private cards.

App, Swift: Apple frameworks only (SwiftUI, Foundation, AuthenticationServices, Security, UserNotifications, PhotosUI, CoreLocation, AVFoundation, CallKit, AppIntents, WidgetKit, ActivityKit, CryptoKit). Build tool: XcodeGen, on the CI runner only.

## Outcomes & Retrospective

Nothing yet.
