# Calls in people chats: voice by default, video when you turn it on

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A person in a chat with other people (a direct chat or a group) can start a call from the chat. Everyone else in the chat is rung: their iPhone shows a full-screen incoming call (CallKit), even when locked, and any open dashboard tab shows a ringing card. A call starts as audio only. Anyone on it can turn their camera on and off, switch cameras, mute, and use the speaker. People can join a call that is already going, and leave it; the call ends when the last person leaves. The chat shows the call as a line: who called, missed or how long it lasted, and who was on it.

Anyone on a call can turn on transcription, for everybody on it: nobody is asked, and each person is told in a notice they close. When the call ends, the transcript (with each line under its speaker's name, since each person's audio arrives separately) and a short summary are posted into the chat.

How to see it working: two accounts that share a circle, one on an iPhone and one in a browser. In their direct chat, tap the phone button. The iPhone rings on the lock screen; answer, talk, turn on video from either side, hang up. The chat shows "Call, 2 minutes". Do the same in a group of three, with one joining late.

## Progress

- [x] Plan agreed with the person (this document), 2026-11-02.
- [x] Server: tables and migration 0269, call model and operations, GraphQL, the account's call feed (2026-11-02).
- [x] Server: the media server (SFU) inside the TeaNode server, its configuration and ports (2026-11-02).
- [x] Server: ringing pushes (VoIP through the push relay, alert as a fallback), and a call_ended push that stops other phones ringing (2026-11-02).
- [ ] Dashboard: start, ring, answer, call screen, video, group grid.
- [ ] iPhone: WebRTC, PushKit and CallKit, call screen, video, group grid.
- [x] Transcription with consent, transcript and summary into the chat (server side, 2026-11-02).
- [x] Transcription without asking anyone: turning it on transcribes everybody on the call and tells them in a notice they close; migration 0270 drops each member's answer (2026-11-02).
- [ ] A transcribed call becomes a recording owned by whoever turned it on, its speakers named from their own tracks, shared with the call's members (migration 0271).
- [x] A call put away while it goes, to use the rest of the app: a bar at the top of the iPhone app with mute and hang up (the dashboard already had its small window) (2026-11-02).
- [x] Command line and agent tool: calls listed, call transcripts read (2026-11-02).
- [ ] Docs, demo data and screenshots, review rounds, deploy, ports opened, tried on two devices.

## Surprises & Discoveries

- A device can be sent the server's first change offer before it has applied the server's answer to its join (the offer is made as soon as the join is answered). Devices therefore take the join, then each offer, strictly one at a time; the SFU test's device works that way and found it.
  Evidence: the race detector in `TestTheMediaServerForwardsEachVoiceToTheOthers` before the device serialized its signaling.
- Chrome works with the pion SFU as planned: two headless Chrome tabs with fake devices connected through it with no ICE servers, heard each other, and a camera turned on mid-call by replacing the video sender's track (no offer from the browser) reached the other tab (about 160 to 230 frames decoded in ten seconds).
  Evidence: a throwaway harness on 2026-11-02, not kept.

## Decision Log

- Decision: calls may have any number of people, through a media server (an SFU) built into the TeaNode server.
  Rationale: the person chose groups of any size. A selective forwarding unit receives each person's audio and video once and forwards it to the others, so a phone sends one stream however many are on the call. pion/webrtc (Go, MIT) runs it inside the server process, so there is nothing new to deploy.
  Date/Author: 2026-11-02, the person.
- Decision: media reaches root@server directly, on a UDP port and a TCP port forwarded by the home gateway (the way 443 is), with ICE over TCP for networks that block UDP.
  Rationale: the person chose the relay on root@server. Since every call goes through the SFU, the SFU's own ports are the relay: no separate TURN server is needed. A TURN relay is added only if testing shows networks that the TCP port does not get through.
  Date/Author: 2026-11-02, the person.
- Decision: calls ring on the iPhone (CallKit, through VoIP pushes) and in open dashboard tabs.
  Date/Author: 2026-11-02, the person.
- Decision: transcription is optional per call, asked of each person on it, and only consenting voices are transcribed; transcript and summary go into the chat.
  Date/Author: 2026-11-02, the person.
- Decision: superseding the one above, nobody is asked: turning transcription on transcribes everybody on the call, and every device shows a notice of who turned it on, which the person closes. The members' `transcriptionConsent` stays in the API, `yes` for everybody while transcribed, for the app build that still reads it; it goes once that build is replaced.
  Date/Author: 2026-11-02, the person.
- Decision: a call can be put away while it goes, so the person can read mail, look at photos or write in the chat during it: on the iPhone to a bar at the top of the app (tap to go back, mute, hang up).
  Date/Author: 2026-11-02, the person.
- Decision: a call starts with audio only; video is each person's own switch.
  Date/Author: 2026-11-02, the person.

- Decision: the person forwards UDP 3478 and TCP 3478 on the gateway to root@server; `calls.udpPort` and `calls.tcpPort` default to 3478.
  Date/Author: 2026-11-02, the person.
- Decision: a call in a group rings every member except those who set that chat's notifications to Off (Only mentions still rings).
  Date/Author: 2026-11-02, the person.
- Decision: a chat with a call going shows a banner at its top, on the dashboard and the iPhone, that joins the call with one tap, for every member whether or not the call rang them; every member of the chat is told of each change to its call, so the banner comes and goes while the chat is open.
  Date/Author: 2026-11-02, the person.
- Decision: a call that comes in while the person is on another TeaNode call rings as a waiting call; answering it leaves the first (CallKit offers hold or end on the iPhone; the dashboard ends the first).
  Date/Author: 2026-11-02, the person.

- Decision: a transcribed call is a recording (the recordings feature), one original per person's track so the speakers are known rather than guessed, owned by whoever turned transcription on and shared at the transcript level with the call's members who share a circle with them.
  Rationale: the person asked to reuse recordings ("transcribe actually means make a recording, and transcribe, with speakers already identified"). It keeps the audio durably (a deploy 30 seconds after a call's end lost its in-memory recordings on 2026-11-02) and brings playback, corrections, search and memory.
  Date/Author: 2026-11-02, the person (choice), Claude (details below).
- Decision: the call's recording goes into its owner's memory like any recording, and no voice samples are cut from it.
  Rationale: the owner heard the call, and everyone was told it is transcribed; the voices belong to other TeaNode users and are named from their tracks, so samples would only put other people's voices among the owner's references.
  Date/Author: 2026-11-02, Claude.

## Outcomes & Retrospective

(At completion.)

## Context and Orientation

TeaNode is one Go program (`cmd/teanode-server`) with a PostgreSQL database, a React dashboard (`web/`) and an iPhone app (`ios/`, SwiftUI, with logic shared in the `TeaNodeKit` Swift package that also builds on Linux).

People chats: a chat is a row in `chat` with members in `chat_member` (at most `models.ChatMembersMost`, 50) and messages in `chat_message` (`internal/models/chat.go`, `internal/db/database_chat.go`). Each member sees the chat through their own `agent_conversation`. A message is posted by `postChatMessage` in `internal/api/v1api/apigraph/chat.go`, which publishes `said` events to the members' conversation feeds through `chat.Hub` (`internal/chat/hub.go`, relayed between server instances over PostgreSQL `pg_notify`) and notifies phones through `chat.Hub.NotifyChatMessage` (`internal/chat/notify.go`). Who may read and write a chat: `requireChatMember` (`apigraph/authorize.go`) and `requireChatWritable` (`apigraph/chat.go`); a direct chat needs the two people to still share a circle. A member's notification level (all, mentions, off) is per chat.

Phones and pushes: a phone registers through `RegisterAgentPhone`; its APNs token is sealed by the push relay (`internal/push`, relay HTTP side in `internal/api/v1api/apipush`) and stored in `agent_phone`. Servers never talk to Apple themselves: they send sealed, end-to-end encrypted content (`push.Content`) to the relay at `agent.push.relayUrl` (mail.teanode.com), which holds the APNs key. The relay sends `alert` and `liveactivity` pushes only; a VoIP push type is refused today.

The iPhone app already makes CallKit calls, outgoing only, for calls with the agent (`ios/TeaNode/Sources/VoiceCall.swift`); `UIBackgroundModes` already has `audio` and `voip`. It has no PushKit and no WebRTC.

Account-wide live feeds exist for location (`LocationEvents`, `apigraph/location_share.go`, hub in `location_events.go`) and shopping (`ShoppingEvents`): one LISTEN per process, fanned out to each viewer. A call's ringing reaches a dashboard tab through a feed of the same shape.

Recordings (`internal/agent/recording*.go`) transcribe one timeline with a diarizing model. A call is different: each person's audio arrives as its own track, so a speaker is known without diarization. Voice messages are transcribed with the `agent.voice` model, paid by the sender's agent (`internal/agent/voice_message.go`).

Terms. WebRTC: the browser and phone standard for real-time audio and video. SFU (selective forwarding unit): a server every participant sends their streams to once, and which forwards each stream to the others without mixing it. ICE: how the two ends find a network path; a candidate is an address to try. TURN: a relay for when no direct path works. CallKit: iOS's system call screen. PushKit VoIP push: a push that wakes the app to ring, which must be reported to CallKit at once.

## Plan of Work

### 1. Data (migration 0269)

`chat_call`: id, chat_id, started_by_user_id, started_at, answered_at (first time a second person joined), ended_at, end_reason (`''`, `ended`, `missed`, `declined`, `failed`), is_transcription_asked, transcription_asked_by_user_id, transcript_state (`''`, `waiting`, `done`, `failed`), summary_message_id. At most one call with ended_at null per chat (a partial unique index).

`chat_call_member`: call_id, user_id, ring_state (`ringing`, `answered`, `declined`, `missed`), joined_at, left_at, is_video_on (last known), transcription_consent (`''`, `yes`, `no`). One row per member of the chat when the call starts, so "missed" is known per person.

`chat_message` gains call_id (nullable): the call's line in the chat is a message whose body is empty and whose call_id names the call; clients draw it from the call. The call's transcript, when there is one, is kept with the call in `chat_call_transcript_segment` (call_id, user_id, start_ms, end_ms, text).

### 2. Operations (GraphQL, with the same names on every surface)

- `StartChatCall(conversationId, isVideoOn)` → ChatCall: needs `requireChatWritable`; refuses when a call is already going in the chat (returns it instead, so a second caller joins it).
- `AnswerChatCall(callId)`, `DeclineChatCall(callId)`, `LeaveChatCall(callId)`.
- `JoinChatCallMedia(callId, offerSdp)` → answerSdp, and `ChatCallMedia(callId)` subscription for the SFU's renegotiation offers, ICE candidates and who is sending what (a small signaling channel per participant).
- `SetChatCallVideo(callId, isVideoOn)`, `SetChatCallMuted(callId, isMuted)`: what the others are told (the tracks themselves start and stop on the device).
- `AskChatCallTranscription(callId)`.
- `ListChatCalls(conversationId, first, before)`, `ReadChatCall(callId)` (with transcript), `ListGoingChatCalls()`, and `ReadGoingChatCall(conversationId)` for the banner a chat with a call going shows.
- `ChatCallEvents` subscription: the account's feed of calls (ringing, answered elsewhere, ended), so any dashboard tab rings and stops ringing.

The call's state changes are also chat events (`call`) on the members' conversation feeds, so an open chat updates its call line live.

### 3. The media server

`internal/call` (new package): one `pion/webrtc` API shared by every call, with a UDP mux on one port (`calls.udpPort`, default 3478) and ICE over TCP on one port (`calls.tcpPort`, default 3478), and the server's public address given to ICE (`calls.publicAddress`, NAT 1:1). Each participant has one PeerConnection to the server: it sends its microphone (Opus) and, when on, its camera (VP8; H.264 also accepted for iPhones); the server adds a sending track to every other participant for each track received and renegotiates over the signaling channel. RTCP keyframe requests are forwarded so a new viewer gets a picture at once. Calls live in the instance that started them; the call row records the instance, and a second instance answers `JoinChatCallMedia` by handing the participant to that instance's address (multi-instance ships later if needed: production runs one instance).

Limits: the SFU sends at most a handful of video streams to a phone (the people speaking most recently) to keep it within a phone's bandwidth; audio is always forwarded. A call with nobody joined for 60 seconds after ringing ends as missed; a call stops when its last person leaves.

Configuration (`calls` section, documented in `docs/configuration.md`): `enabled` (default on), `publicAddress`, `udpPort`, `tcpPort`, `ringSeconds` (60), `videoStreamsMost` (6).

### 4. Ringing

Server: on `StartChatCall`, every other active member whose notification level for the chat is not `off` is rung: a VoIP push to each of their phones (new push type `voip`, kind `call`, sealed content with the caller's name, the chat and the call id), and a `ChatCallEvents` event to their dashboard tabs. A member who is on another TeaNode call is rung all the same, as a waiting call.

Push relay: accepts a `voip` registration (a second token the phone registers from PushKit) and sends `apns-push-type: voip` to the topic `com.teanode.app.voip` with high priority. This needs the relay's own server (mail.teanode.com) deployed first; servers that send through a relay that does not have it fall back to an alert push ("Sam is calling").

### 5. Dashboard

A phone button in a chat's header starts an audio call. A ringing card (top of the window, any page) with Answer, Answer with video, Decline. The call screen: a grid of people (their video, or their picture and name with a ring that lights when they speak), and a bar with mute, camera, switch camera (when there is more than one), speaker output, transcribe, leave. A call can be minimized to a small floating window while using the rest of the dashboard. The call line in the chat: "Call, 4 minutes", "Missed call", with who joined, and a Join button while it is going.

### 6. iPhone

`WebRTC` from `https://github.com/stasel/WebRTC` (Swift package of Google's WebRTC, version 155.0.0). PushKit registers a VoIP token and sends it with the phone's registration. An incoming VoIP push is reported to CallKit at once (iOS requires it); Answer joins the call. Outgoing calls also go through CallKit, so the call is on the lock screen and in the car. The call screen follows `ios/DESIGN.md`: the same grid and bar as the dashboard, the camera switch, picture in picture for video when leaving the app. The chat's toolbar has the call button; the call line in the transcript as on the dashboard.

### 7. Transcription, as a recording

`AskChatCallTranscription` marks the call; everybody on it, and anyone who joins later, is transcribed and told so. The media server writes each person's microphone to Ogg Opus files of their own (`call.Recorder`), under the server's data directory (`<data>/call-recordings/<call id>`), so a restart keeps them.

When the call ends, the call becomes a recording owned by whoever turned transcription on (`recording.chat_call_id`, origin `call`): one original per stretch of one person's microphone, at its offset from the call's start, each naming its speaker (`recording_media.speaker_user_id`, `speaker_name`). It is imported through the recording store as `ImportRecordingAttachment` does, and its transcript runs are queued at once, so the diarization sweep never takes them. A run of an original whose speaker is known is transcribed without speaker separation, with the voice settings' model (as a voice message is), every passage under the speaker's label; when it completes, the label is confirmed as that member's name (the owner's own track as the account owner), so every screen, the summary and a share name the speakers. Voice samples are not cut from these runs: the voices are other people's, and are named already.

After that the recording is what any recording is: playback, the transcript with corrections, the summary (`recording_brief`), search, the owner's memory, the `recording` tool and `teanode recording`. Each call member who shares a circle with the owner is given it at the transcript level; when the summary is written it is posted into the chat by the owner's agent, with a link to the recording, and the call's line opens it. The owner's agent pays, as for any recording. A server that stops before the import picks the files up from the data directory when it starts again.

Migration 0271 adds `recording.chat_call_id`, `recording_media.speaker_user_id` and `speaker_name`, and `chat_call.recording_id`, and drops `chat_call_transcript_segment` (the transcript is the recording's).

### 8. Command line and agent

`teanode chat calls <chat>` lists calls; `teanode chat call <call id>` shows one with its transcript. The agent's chat tool gains `list_calls` and `read_call`, so "what did we decide on yesterday's call" can be answered from the transcript. Starting a call is not an agent action: a call is the person's own voice.

## Open questions for the person

None open: the three below were answered on 2026-11-02 and are in the Decision Log.

## Validation and Acceptance

- Go tests: the call state machine (start, ring, answer elsewhere, decline, missed after ringSeconds, leave, last leaves ends), the migration and its reverse, authorization (a non-member cannot start, join or read; a removed member cannot), pion SFU loopback test with two in-process peers exchanging audio and a third joining late.
- Dashboard tests: the ringing card, the call line in each state, the call screen controls.
- TeaNodeKit tests on Linux: call line text, signaling message decoding.
- CI screenshots: incoming call, the call screen with audio only, with video, a group of four, the call lines in a chat, light, dark, largest text.
- On two real devices after deploy: an audio call from iPhone to browser and back, video on and off from each side, a third person joining, the iPhone ringing while locked, a missed call, and a transcribed call with one person not consenting.

## Idempotence and Recovery

Migration 0269 adds tables and a nullable column; its reverse drops them. A call left open by a server restart is ended as `failed` at start-up (the SFU's state is in memory). A backup of the database is taken before the migration is deployed.

## Interfaces and Dependencies

- Go: `github.com/pion/webrtc/v4` (and its pion dependencies), vendored.
- iOS: `WebRTC` Swift package 155.0.0 (`stasel/WebRTC`).
- Browser: the platform's `RTCPeerConnection`, `getUserMedia`.
- Apple: VoIP pushes with the existing APNs key (token authentication covers the VoIP topic).
