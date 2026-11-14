# Chats: what the iPhone app does and where the Android app does it

The Android port of `ios/TeaNode/Sources` (Chats), `ios/TeaNodeKit` (the conversation logic) and `ios/TeaNodeShare`.
Status is `done`, `partly` (what is missing is said) or `not ported` (why). Nothing here needs a server to be drawn: the
demo answers (`ChatsDemo.kt`) reach every screen and state, and `ChatsScreenshotTest` photographs them.

Not verified on a phone or a server: the microphone (voice messages, dictation), the camera, the file and photo
pickers, playing sound, reading aloud, the location answer, the share sheet receiving from another app, push, and the
live feed. All of those are written against the platform's APIs and the iPhone app's protocol, and none could be run
here.

## Kit (`android/kit`, package `com.teanode.kit.chats`)

| iOS file | Android file | Status |
| --- | --- | --- |
| `Models.swift` (the conversation parts) | `Models.kt` | done |
| `Chats.swift` | `Chats.kt`, `ChatDocuments.kt`, `ChatOperations.kt` | done |
| `Transcript.swift` | `Transcript.kt` | done |
| `ConversationFeed.swift` | `ConversationFeed.kt` (on the foundation's `Connection.subscribe`) | done |
| `ConversationDraft.swift` | `ConversationDraft.kt` (+ `DraftStore` in the app) | done |
| `EmojiReaction.swift` | `EmojiReaction.kt` (code point tables, since `Character.isEmojiPresentation` needs Java 21) | done |
| `JSONValue.swift` | not needed: `kotlinx.serialization.json.JsonElement` | done |
| `LinkClean.swift`, `LinkOpening.swift`, `LinkText.swift` | `LinkClean.kt`, `LinkOpening.kt`, `LinkText.kt` (the screen draws the links) | done |
| `LinkPreview.swift` | `LinkPreview.kt` (the model, the call and the batching loader) | done |
| `Markdown.swift`, `PlainText.swift`, `TerminalText.swift` | `Markdown.kt`, `PlainText.kt`, `TerminalText.kt` | done |
| `Media.swift` | `Media.kt` (`AgentFile`, `picturePath`, `download`, `shareAddress`) | done |
| `Status.swift`, `Waiting.swift` | `Status.kt`, `Waiting.kt` | done |
| `Voice.swift` | `Voice.kt` (`VoiceAvailability`, `SpokenText`, `VoiceEvent`); the read-aloud calls are in `SpeechReading.kt` | done |
| `Glance.swift` | not ported: it reads calendar and mail models of other parts and feeds the widgets (calls-notifications) | not ported |
| `Operations.swift` (the conversation calls) | `ChatOperations.kt` | done |

Tests ported to JUnit 5 (`android/kit/src/test/kotlin/com/teanode/kit/chats`): `ChatTests`, `SignInAndTranscriptTests`
(transcript, media, markdown, voice), `ConversationHistoryTests`, `ConversationSendTests`, `CodingQuestionHeadlineTests`,
`PresenceTests`, `ReactionTests`, `RunAnswerTests`, `AgentReferenceTests`, `EmojiReactionTests`, `LinkCleanTests` (the
cases shared with the server), `LinkOpeningTests`, `LinkPreviewTests`, `LinkTextTests` (the span finding; the
`AttributedString` parts are drawn by the screen), `PlainTextTests`, `TerminalTextTests`, `ConversationDraftTests`,
`GlanceTests` (not ported with Glance).

Two shapes are Android's own: `ChatCallRecord` is defined here (as much as a message's call line needs) and the calls
part may replace it; `CodingQuestion` is defined here because the transcript reads the "herdr_asked" note.

## Screens (`android/app`, package `com.teanode.app.chats`)

| iOS | Android | Status |
| --- | --- | --- |
| `TeaNodeApp.swift` `ConversationsList` (the Chats tab's list) | `ConversationsList.kt` | done |
| `ConversationModel.swift` | `ConversationModel.kt`, `ChatsEnvironment.kt` (the parts of `AppModel` conversations read) | done, minus calls (below) |
| `ConversationView.swift` | `ConversationScreen.kt`, `Bubbles.kt`, `Lines.kt`, `Cards.kt`, `Composer.kt`, `MarkdownView.kt` | done, minus calls and the Live Activity |
| `ChatViews.swift` | `ChatSheets.kt`, `Lines.kt` (avatars, read marks, typing line, frame), `Composer.kt` (mention menu) | done |
| `MediaViews.swift` | `Media.kt` | done: pictures open a zoomable full-screen viewer (photos has no viewer for a URL or single-photo read), audio and video have "Save as recording" (recordings import route); no Quick Look (files open in the system's viewer) |
| `LinkViews.swift`, `LinkPreviewDemo.swift` | `LinkCards.kt`, `ChatsDemo.kt` | done |
| `VoiceMessageRecorder.swift`, `VoiceMessageViews.swift` | `VoiceMessages.kt`, `Bubbles.kt`, `Composer.kt` | done (not run on a phone) |
| `Dictation.swift` | `Dictation.kt` (AudioRecord to the same voice socket) | done (not run on a phone) |
| `MessageReader.swift` | `MessageReader.kt` (server voice, then the phone's own) | done (not run on a phone) |
| `StatusViews.swift` | `StatusSheet.kt` | partly: the usage sheet and background commands are done; `CallBar` and `CallMeter` belong to calls |
| `MessagePresentations.swift` | not ported as a type: the artifact is drawn inline (`Media.kt`); recording import, the photo viewer and the call transcript belong to other parts | partly |
| `TurnActivities.swift` | not ported: a Live Activity; the ongoing notification for a running turn is calls-notifications' | not ported |
| `InMemoryViews.swift` | not ported: the knowledge page chips belong to the knowledge part | not ported |
| `WeekdayChecklist.swift` | not ported: used by the calendar's repeat screen only | not ported |
| `ios/TeaNodeShare` | `ShareReceiverActivity.kt` (text, links, pictures and files to the main conversation) | done: destinations agent, tax documents, statements and recording (the file is handed to the agent and Recordings opens its import screen) |

### Screens and states

- List: the main chat row; People (chats with unread count, mute mark, pictures); Side chats (summary, waiting mark,
  last used); Archived chats (opens on tap); search across what was said (250 ms after the last key); pull to refresh and
  a read every 30 s; new side chat, message someone, new group (menu on the button); rename, make main, archive,
  unarchive, delete with confirmation, mute level (menu on each row); loading, error with "Try again", empty.
- Conversation: bubbles on each side; date dividers; runs of one author named once; avatars; read marks; typing and
  thinking lines; the agent's three dots; reactions (quick bar and any emoji), edited and deleted marks, quoted replies,
  references (mail, page, recording, photo, transaction); mentions marked, the reader's own highlighted; voice messages
  (play, waveform, transcript, transcribe); attachments (picture, file chip, removed file); tool files (file cards with
  open, share, copy link; artifacts: pages in a web view, documents inline); pictures in an answer; link cards; long
  messages (show all); suggestions; ideas on an empty conversation; the agent's steps; approval, question and coding
  question cards (and their answered records); check-in, note and tool lines with the switches for what shows; usage on
  the last answer; banners (acting as, run record, goal, call, cannot write); goal sheet; earlier messages; jump to the end.
- Message menu: reactions, reply, edit, copy, read aloud, select, share, delete.
- Composer: files (photos, camera, files), dictation, hold to talk, the @ menu, the reply and reference strips, stop,
  send with progress, drafts kept for each conversation.
- Chat menu: members (with admins, add, remove, notification level, the two agent switches), rename, notification level,
  leave. The people sheet (message, group, add).
- Usage and devices: budget, the three switches, attached tab and computers, background commands (output, stop).

### Not possible or left to other parts

- Calling the agent by voice (`VoiceCall`, `CallBar`), and calls in chats (`ChatCall*`): calls-notifications. A call's
  line in a chat is drawn as a one-line banner (`ChatCallLine`) until that part replaces it.
- A person's card from a picture or a name (`ChatsHooks.openPerson`): mail-contacts sets it; until then a tap does
  nothing.
- Siri, App Intents, Live Activities: no Android equivalent here; notifications are calls-notifications'.
- The badge on the bar while Chats is not open: it is set when Chats reads (on opening and every 30 s while it is open);
  keeping it fresh elsewhere needs the push of calls-notifications.
- The turn surface is "android" and "android_voice" (`internal/agent/surface.go`).

## Demo ids

`demo-main` (the main chat), `demo-cards`, `demo-empty`, `demo-long`, `demo-goal`, `demo-run`, `demo-sam` (a direct
chat), `demo-group` (a group with agents). Open one with
`--es com.teanode.app.extra.ROUTE agent/conversation/demo-cards` (the main chat is `agent/main`).

## Shared files this part touched

`AndroidManifest.xml` (microphone, location permissions; the file provider; the share activity) and
`res/xml/chat_file_paths.xml`, `res/values/chats_themes.xml`. Nothing else outside the part's own packages and files.

## Round 2

- `ChatsHooks.askAbout(navigator, reference)`: opens the main conversation with a reference strip (used by finance's
  transaction detail, "Ask the agent about this"). The reference is `AgentReference.transaction/mail/knowledgePage/...`.
- Terminal text: one parser, `com.teanode.kit.chats.terminalRuns`; coding uses it.
- Server: surfaces `android` and `android_voice` (`internal/agent/surface.go`, `ask.go`), with Go tests.
- The chats list's rows share a leading column of one width, so titles line up with a group's overlapped pictures.
