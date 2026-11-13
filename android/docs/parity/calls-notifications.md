# Parity: calls and notifications

The iPhone scope of this area, item by item, with the Android port. Status: `done`, `partly` (what is missing),
`not ported` (why). `iOS file` is relative to `ios/`.

## Kit (`com.teanode.kit.calls`, `com.teanode.kit.notifications`)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Call, member, transcript, signal, record models, tolerant decoding | TeaNodeKit/ChatCalls.swift | `calls/ChatCalls.kt` | todo |
| Call documents (copied verbatim) and every operation: start, join, decline, leave, join media, answer media, video, featured, muted, ask transcription, list going, read going, list, read, update phone ringing | TeaNodeKit/ChatCalls.swift | `calls/ChatCalls.kt` | todo |
| Event and signal feeds (ChatCallEvents, ChatCallSignals) | TeaNodeKit/ChatCallFeed.swift | `calls/ChatCallFeed.kt` | todo |
| Duration text, line kind, line from view or record, transcription status, stage (featured), grid columns and rows | TeaNodeKit/ChatCallWords.swift | `calls/ChatCallWords.kt` | todo |
| Ledger of calls the phone rings for, places or is on; push decision (ring, stop, report and end); reactions to events; reconcile | TeaNodeKit/ChatCallRinging.swift | `calls/ChatCallRinging.kt` | todo |
| Signaling order (join first, offers one at a time) and SDP sections | TeaNodeKit/ChatCallSignaling.swift | `calls/ChatCallSignaling.kt` | todo |
| Notification content, buttons, payload key (AES-256-GCM, nonce first), keyring per account with single-key migration, relay registration (platform android, alert and voip) | TeaNodeKit/Notifications.swift | `notifications/Notifications.kt` | todo |
| App update: channel android, newer-than rule, version text, dismissal key, GetAppUpdate | TeaNodeKit/AppUpdate.swift | `notifications/AppUpdate.kt` | todo |
| Phone registration (RegisterAgentPhone with phonePlatform android), UpdateAgentPhone (kinds, ringing) | TeaNodeKit/Operations.swift | `notifications/PhoneRegistration.kt` | todo |
| Notification categories as data: which buttons each kind has, a question's buttons, journal mood levels, the info a shown notification carries | TeaNode/NotificationCategories.swift | `notifications/NotificationActions.kt` | todo |
| Tests: ChatCallTests (all that do not need chat messages), AppUpdateTests, NotificationTests, PayloadKeyringTests, the content decoding tests of journal, location, shopping | TeaNodeKit/Tests | `kit/src/test/.../calls`, `.../notifications` | todo |

## Push and notifications (app)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Firebase Messaging only when `google-services.json` exists; guarded by the default FirebaseApp having initialized | AppDelegate.swift | `notifications/PushAvailability.kt` | todo |
| Ask permission to send notifications (POST_NOTIFICATIONS on 13+) after connecting and at each start | AppModel.setUpNotifications | `notifications/PushRegistrar.kt` | todo |
| Register the token with the relay (alert and voip, platform android, same token) and give the sealed registration and the account's payload key to the server of every account; a new token registers again | AppDelegate, AppModel.register | `notifications/PushRegistrar.kt`, `TeaNodePushService.kt` | todo |
| Phone listed on the server even when notifications are refused, with the key | AppModel.setUpNotifications | `notifications/PushRegistrar.kt` | todo |
| Open `sealed` with each account's key in turn; the key that opens it names the account; unreadable ones are shown as they came | NotificationService.swift | `notifications/PushHandler.kt` | todo |
| Data message `pushKind` `call` rings, `notification` shows | VoIPPushRegistrar.swift | `notifications/PushHandler.kt` | todo |
| Notification channels (one per group of kinds), per-kind buttons: approval (Do it, Don't), message and chat (Reply with text, Open), question (up to four buttons, reply, open), skill share call, circle, location, shopping, coding question (Open), journal evening question (five mood buttons without opening the app) | NotificationCategories.swift | `notifications/NotificationChannels.kt`, `NotificationBuilder.kt` | todo |
| Buttons: approve and deny need the phone unlocked; mood buttons do not | NotificationCategories.swift | `notifications/NotificationBuilder.kt` | todo |
| Press handling: open switches to the account and goes to the place (circle, location with person, shopping store or list, coding session, journal day, conversation); approve, deny, reply (question answer or chat message), answer buttons, mood; unknown account says so; failures are posted as a notification | AppModel.perform | `notifications/NotificationActionReceiver.kt`, `NotificationRouting.kt` | todo |
| Shown while the app is open | AppDelegate willPresent | `notifications/PushHandler.kt` | todo |
| Update card (Update available, TeaNode version is ready to install, Open the install page, Not now), asked hourly, dismissal remembered per update, Settings keeps offering | AppUpdatePrompt.swift | `notifications/AppUpdate*.kt` | todo |
| Home screen actions (Start recording) | AppDelegate.swift | `shortcuts.xml` static shortcut opens recordings | todo |
| Intents: AskAgentIntent (Siri) | Intents.swift | not ported: Siri and Shortcuts have no counterpart; an app shortcut set (call, record, ask) is offered instead | todo |
| Intents: CallAgentIntent, StartRecordingIntent, OpenSectionIntent, OpenConversationIntent | Shared/OpeningIntents.swift | deep links `teanode://app/...` used by widgets and shortcuts | todo |

## Calls (app)

| Item | iOS file | Android | Status |
| --- | --- | --- | --- |
| Call manager: follow ChatCallEvents with backoff, read calls going at start and after a drop, rings for a ringing call when the app is in front | ChatCallManager.swift | `calls/CallCenter.kt` | todo |
| Start a call (voice or video, everybody or chosen people), join one going, answer a ring, decline, leave, hang up | ChatCallManager.swift | `calls/CallCenter.kt` | todo |
| Problems: microphone refused, camera refused, already on a call, not signed in, could not be started, lost its connection | ChatCallManager.swift | `calls/CallCenter.kt` | todo |
| System call integration: a foreground service of the phone call type, full-screen incoming call with Answer and Decline, ongoing call notification with Hang up and Mute, lock screen | CallKit use | `calls/CallService.kt`, `IncomingCallActivity.kt` | todo |
| WebRTC media: no ICE servers, microphone, video transceiver with q and f layers, offers and answers in order, tracks by stream id, camera on and off by replacing the track, flip camera, mute | ChatCallMedia.swift | `calls/WebRtcCallMedia.kt` | todo |
| Audio route: phone, speaker, headphones, Bluetooth, car; button shows the route and chooses another | ChatCallAudioRoute.swift | `calls/CallAudioRoute.kt` | todo |
| Speaker chosen by itself when the camera goes on | ChatCallManager.swift | `calls/CallCenter.kt` | todo |
| Call screen: header with minimize, title and status (timer once answered, Connecting, Reconnecting, Calling), tiles, grid by count, featured person above a row from five people or when one is chosen (tap to feature, tap again to follow the speaker), own tile first, ringing people dimmed with Ringing, muted mark, pin mark | ChatCallViews.swift | `calls/CallScreen.kt`, `CallTiles.kt` | todo |
| Controls: mute, camera, flip camera (needs camera on), audio route, transcribe (confirmation, disabled once asked), hang up | ChatCallViews.swift | `calls/CallScreen.kt` | todo |
| Transcription notice (who turned it on, you turned it on, closes to a small Transcribing mark) | ChatCallViews.swift | `calls/CallScreen.kt` | todo |
| Minimized call bar over the app with return, mute and hang up | ChatCallViews.swift | `calls/CallBar.kt` | todo |
| Presenter: screen over the app, bar when minimized, problems as a snackbar | ChatCallViews.swift | `calls/CallPresenter.kt` | todo |
| Call button in a chat bar: menu of voice call, video call, choose who to call (groups); disabled when the chat cannot be written | ChatCallViews.swift | `calls/ChatCallViews.kt` | todo |
| Who to call sheet: everybody chosen at first, the others not rung, footer, voice or video, disabled with nobody chosen | ChatCallViews.swift | `calls/ChatCallViews.kt` | todo |
| Banner of a chat with a call going: how many are on it and Join; hidden while on that call | ChatCallViews.swift | `calls/ChatCallViews.kt` | todo |
| A call's line in a conversation: symbol and words by kind, who called or who was on it, Join while going, Transcript when done | ChatCallViews.swift | `calls/ChatCallViews.kt` | todo |
| Transcript screen: speaker, time, text; no transcript, cannot read; open the recording | ChatCallViews.swift | `calls/CallTranscriptScreen.kt` | todo |
| Demo: banner, call screen with three people, minimized, featured with five, lines (ended with transcript, missed, going), transcript | ChatCallDemo.swift | `calls/CallsDemo.kt` | todo |
| Live Activity for a call (lock screen mute and hang up) | Shared/CallActivity.swift | the ongoing call notification has Mute and Hang up | todo |
| Agent voice call: microphone to the server socket, answers read back, meter, cut in, mute, still here, silence watch, route to the speaker, problems | VoiceCall.swift | `calls/AgentVoiceCall.kt` | todo |
| Turn activity (a turn left running) as an ongoing notification: Working on it, Waiting for you, Done, Stopped with a problem | Shared/TurnActivity.swift | `notifications/TurnNotification.kt` | todo |
| Recording activity on the lock screen | Shared/RecordingActivity.swift | the recording foreground service notification belongs to photos-recordings | not ported here |
| Widgets: Your agent (ask, call, record), Waiting for you, Up next | TeaNodeWidgets | `widgets/` (Glance) | todo |
| Control Center controls, Dynamic Island | TeaNodeWidgets | not ported: iOS only; the widgets and shortcuts cover the same actions | todo |
| TurnSideways UI test helper | TeaNodeTurn | not ported: iOS screenshot CI helper | todo |
