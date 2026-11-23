# Parity: calls and notifications

The iPhone scope of this area, item by item, with the Android port. Status: `done`, `partly` (what is missing),
`not ported` (why). `iOS file` is relative to `ios/`.

## Kit (`com.teanode.kit.calls`, `com.teanode.kit.notifications`)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Call, member, transcript, signal, record models, tolerant decoding | TeaNodeKit/ChatCalls.swift | `calls/ChatCalls.kt` | done |
| Call documents (copied verbatim) and every operation: start, join, decline, leave, join media, answer media, video, featured, muted, ask transcription, list going, read going, list, read, update phone ringing | TeaNodeKit/ChatCalls.swift | `calls/ChatCalls.kt` | done |
| Event and signal feeds (ChatCallEvents, ChatCallSignals) | TeaNodeKit/ChatCallFeed.swift | `calls/ChatCallFeed.kt` | done |
| Duration text, line kind, line from view or record, transcription status, stage (featured), grid columns and rows | TeaNodeKit/ChatCallWords.swift | `calls/ChatCallWords.kt` | done |
| Ledger of calls the phone rings for, places or is on; push decision (ring, stop, report and end); reactions to events; reconcile | TeaNodeKit/ChatCallRinging.swift | `calls/ChatCallRinging.kt` | done |
| Signaling order (join first, offers one at a time) and SDP sections | TeaNodeKit/ChatCallSignaling.swift | `calls/ChatCallSignaling.kt` | done |
| Notification content, buttons, payload key (AES-256-GCM, nonce first), keyring per account with single-key migration, relay registration (platform android, alert and voip) | TeaNodeKit/Notifications.swift | `notifications/Notifications.kt` | done |
| App update: channel android, newer-than rule, version text, dismissal key, GetAppUpdate | TeaNodeKit/AppUpdate.swift | `notifications/AppUpdate.kt` | done |
| Phone registration (RegisterAgentPhone with phonePlatform android), UpdateAgentPhone (kinds, ringing) | TeaNodeKit/Operations.swift | `notifications/PhoneRegistration.kt` | done |
| Notification categories as data: which buttons each kind has, a question's buttons, journal mood levels, the info a shown notification carries | TeaNode/NotificationCategories.swift | `notifications/NotificationActions.kt` | done |
| Tests: ChatCallTests (all that do not need chat messages), AppUpdateTests, NotificationTests, PayloadKeyringTests, the content decoding tests of journal, location, shopping | TeaNodeKit/Tests | `kit/src/test/.../calls`, `.../notifications` | done |

## Push and notifications (app)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Firebase Messaging only when `google-services.json` exists; guarded by the default FirebaseApp having initialized | AppDelegate.swift | `notifications/PushAvailability.kt` | done |
| Ask permission to send notifications (POST_NOTIFICATIONS on 13+) after connecting and at each start | AppModel.setUpNotifications | `notifications/PushRegistrar.kt` | done (the permission prompt and the token are not verified on a phone) |
| Register the token with the relay (alert and voip, platform android, same token) and give the sealed registration and the account's payload key to the server of every account; a new token registers again | AppDelegate, AppModel.register | `notifications/PushRegistrar.kt`, `TeaNodePushService.kt` | done (the relay and a real server are not verified) |
| Phone listed on the server even when notifications are refused, with the key | AppModel.setUpNotifications | `notifications/PushRegistrar.kt` | done |
| Open `sealed` with each account's key in turn; the key that opens it names the account; unreadable ones are shown as they came | NotificationService.swift | `notifications/PushHandler.kt` | done |
| Data message `pushKind` `call` rings, `notification` shows | VoIPPushRegistrar.swift | `notifications/PushHandler.kt` | done |
| Notification channels (one per group of kinds), per-kind buttons: approval (Do it, Don't), message and chat (Reply with text, Open), question (up to four buttons, reply, open), skill share call, circle, location, shopping, coding question (Open), journal evening question (five mood buttons without opening the app) | NotificationCategories.swift | `notifications/NotificationChannels.kt`, `NotificationBuilder.kt` | done (the row of four or five buttons is a row of its own in the notification, checked by a Robolectric test, not on a phone) |
| Buttons: approve and deny need the phone unlocked; mood buttons do not | NotificationCategories.swift | `notifications/NotificationBuilder.kt` | done |
| Press handling: open switches to the account and goes to the place (circle, location with person, shopping store or list, coding session, journal day, conversation); approve, deny, reply (question answer or chat message), answer buttons, mood; unknown account says so; failures are posted as a notification | AppModel.perform | `notifications/NotificationActionReceiver.kt`, `NotificationRouting.kt` | done (the other parts read the place from `NotificationOpening`; it is theirs to use) |
| Shown while the app is open | AppDelegate willPresent | `notifications/PushHandler.kt` | done (Android shows a notification while the app is open by itself) |
| Update card (Update available, TeaNode version is ready to install, Open the install page, Not now), asked hourly, dismissal remembered per update, Settings keeps offering | AppUpdatePrompt.swift | `notifications/AppUpdate*.kt` | partly (Settings keeps offering it only once its agent lists `AppUpdateModel.update`) |
| Home screen actions (Start recording) | AppDelegate.swift | `shortcuts.xml` static shortcut opens recordings | done (Start recording opens the recorder, `recordings/capture`; the widget's record button too) |
| Intents: AskAgentIntent (Siri) | Intents.swift | not ported: Siri and Shortcuts have no counterpart; an app shortcut set (call, record, ask) is offered instead | not ported (Siri and Shortcuts have no counterpart; the icon menu has Ask your agent, Call your agent and Start recording) |
| Intents: CallAgentIntent, StartRecordingIntent, OpenSectionIntent, OpenConversationIntent | Shared/OpeningIntents.swift | deep links `teanode://app/...` used by widgets and shortcuts | done (deep links, and the extra the call with the agent reads from `OpeningRequests`) |

## Calls (app)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Call manager: follow ChatCallEvents with backoff, read calls going at start and after a drop, rings for a ringing call when the app is in front | ChatCallManager.swift | `calls/CallCenter.kt` | done |
| Start a call (voice or video, everybody or chosen people), join one going, answer a ring, decline, leave, hang up | ChatCallManager.swift | `calls/CallCenter.kt` | done |
| Problems: microphone refused, camera refused, already on a call, not signed in, could not be started, lost its connection | ChatCallManager.swift | `calls/CallCenter.kt` | done |
| System call integration: a foreground service of the phone call type, full-screen incoming call with Answer and Decline, ongoing call notification with Hang up and Mute, lock screen | CallKit use | `calls/CallService.kt`, `IncomingCallActivity.kt` | done (a foreground service, a full-screen incoming call, and each call hosted as a self-managed Telecom call with androidx.core:core-telecom for headset, car and watch controls; not verified on a phone or a car) |
| WebRTC media: no ICE servers, microphone, video transceiver with q and f layers, offers and answers in order, tracks by stream id, camera on and off by replacing the track, flip camera, mute | ChatCallMedia.swift | `calls/WebRtcCallMedia.kt` | done (not verified on a phone against a real media server) |
| Audio route: phone, speaker, headphones, Bluetooth, car; button shows the route and chooses another | ChatCallAudioRoute.swift | `calls/CallAudioRoute.kt` | done |
| Speaker chosen by itself when the camera goes on | ChatCallManager.swift | `calls/CallCenter.kt` | done |
| Call screen: header with minimize, title and status (timer once answered, Connecting, Reconnecting, Calling), tiles, grid by count, featured person above a row from five people or when one is chosen (tap to feature, tap again to follow the speaker), own tile first, ringing people dimmed with Ringing, muted mark, pin mark | ChatCallViews.swift | `calls/CallScreen.kt`, `CallTiles.kt` | done |
| Controls: mute, camera, flip camera (needs camera on), audio route, transcribe (confirmation, disabled once asked), hang up | ChatCallViews.swift | `calls/CallScreen.kt` | done |
| Transcription notice (who turned it on, you turned it on, closes to a small Transcribing mark) | ChatCallViews.swift | `calls/CallScreen.kt` | done |
| Minimized call bar over the app with return, mute and hang up | ChatCallViews.swift | `calls/CallBar.kt` | done |
| Presenter: screen over the app, bar when minimized, problems as a snackbar | ChatCallViews.swift | `calls/CallPresenter.kt` | done |
| Call button in a chat bar: menu of voice call, video call, choose who to call (groups); disabled when the chat cannot be written | ChatCallViews.swift | `calls/ChatCallViews.kt` | done (the chats part places the button) |
| Who to call sheet: everybody chosen at first, the others not rung, footer, voice or video, disabled with nobody chosen | ChatCallViews.swift | `calls/ChatCallViews.kt` | done (the chats part shows the sheet) |
| Banner of a chat with a call going: how many are on it and Join; hidden while on that call | ChatCallViews.swift | `calls/ChatCallViews.kt` | done (the chats part places the banner) |
| A call's line in a conversation: symbol and words by kind, who called or who was on it, Join while going, Transcript when done | ChatCallViews.swift | `calls/ChatCallViews.kt` | done (the chats part places the line) |
| Transcript screen: speaker, time, text; no transcript, cannot read; open the recording | ChatCallViews.swift | `calls/CallTranscriptScreen.kt` | done (the person's own recording opens `recordings/detail/<id>`, one shared with them `recordings/shared/<id>`) |
| Demo: banner, call screen with three people, minimized, featured with five, lines (ended with transcript, missed, going), transcript | ChatCallDemo.swift | `calls/CallsDemo.kt` | done |
| Live Activity for a call (lock screen mute and hang up) | Shared/CallActivity.swift | the ongoing call notification has Mute and Hang up | done (the ongoing call notification has Mute and Hang up; no lock screen widget) |
| Agent voice call: microphone to the server socket, answers read back, meter, cut in, mute, still here, silence watch, route to the speaker, problems | VoiceCall.swift | `calls/AgentVoiceCall.kt` | partly (started from the icon, a widget or a button in the main conversation and read out from the feed; the microphone, speaker and socket are not verified on a phone; the words of its state on the chat screen are not drawn) |
| Turn activity (a turn left running) as an ongoing notification: Working on it, Waiting for you, Done, Stopped with a problem | Shared/TurnActivity.swift | `notifications/TurnNotification.kt` | done (shown from the conversation's feed while the app is not in front) |
| Recording activity on the lock screen | Shared/RecordingActivity.swift | the recording foreground service notification belongs to photos-recordings | not ported here |
| Widgets: Your agent (ask, call, record), Waiting for you, Up next | TeaNodeWidgets | `widgets/` (Glance) | done (not verified on a launcher) |
| Control Center controls, Dynamic Island | TeaNodeWidgets | not ported: iOS only; the widgets and shortcuts cover the same actions | not ported (iOS only; the widgets and the icon menu cover the same actions) |
| TurnSideways UI test helper | TeaNodeTurn | not ported: iOS screenshot CI helper | not ported (the iOS screenshot CI helper) |


## Where the other parts hook in

- Chats: the call button, the banner and the line of a call are placed in `ConversationScreen` through `chats/ConversationCalls.kt`
  (a small, additive edit of `ConversationScreen.kt`; the placeholder `ChatCallLine` was removed).
- Chats, wired in round 2 (small additive edits in `chats/ConversationScreen.kt` and `chats/ConversationModel.kt`: `ConversationCallEffects`, `ConversationAgentCallButton`, and `ConversationModel.onEvent`): `OpeningRequests.isCallRequested` (the icon menu and the widget ask for a call with the agent; the chats part starts
  `AgentVoiceCall` and hands it to `CallCenter.beginAgentVoice`), `TurnNotification.show/end` from the conversation's feed, and the notification
  reply surface (`ios` is used as the typed surface, as the chats part does).
- Every part that a notification opens reads the detail (a conversation, a person, a store or list, a session, a day) from
  `NotificationOpening.destination` and calls `consume()`; the shell goes to the part by itself.
- Settings: `PushSystem.registrar` (`wants`, `setNotificationKind`, `phonesByAccount`) is the phone's notification kinds, and
  `PushSystem.appUpdates.update` is the update Settings keeps offering.
- Shared edits: `AppShell` (the calls' routes, the update card over the chats, a pressed notification's part), `AppRoot` (the call presenter and
  the push effects), `DemoServer` (the calls' answers), `TeaNodeApplication` (the push system, notification channels), `MainActivity`
  (what a pressed notification, the icon menu and the widgets send), `AndroidManifest.xml`, `libs.versions.toml` and `app/build.gradle.kts`.
- Demo mode: a debug build opened with `--ez com.teanode.app.extra.DEMO true --es com.teanode.app.extra.ROUTE calls/demo/<name>` shows `screen`,
  `featured`, `chosen`, `ringing`, `reconnecting`, `incoming`, `bar`, `banner`, `lines`, `who` or `transcript`.
