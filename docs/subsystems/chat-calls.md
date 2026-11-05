# Calls in chats

A person in a chat with other people, direct or a group, can call it, as a
voice call or a video call; anyone on it turns their own camera on and off.
Every other member of the chat is rung, on their phones and in their open
dashboards, except anyone who set that chat's notifications to off (Only
mentions still rings). In a group the caller may choose who is rung instead;
the others are not rung but can still join. Whoever has their microphone off
is marked beside their name on the call. Anyone in the chat can join while the call goes, and
an open chat shows a banner to join it. The call ends when the last person
leaves, or as missed when nobody answers within `calls.ringSeconds`.

## The media server

Everybody on a call sends their microphone, and their camera while it is on,
to the server once, and the server forwards each to everybody else without
mixing it (a selective forwarding unit, `internal/call`, on pion/webrtc). A
phone sends its microphone once and its camera in two sizes however many
people are on the call, and receives one of each from everybody else.

It listens on one UDP port and one TCP port (`calls.udpPort`,
`calls.tcpPort`, 3478 by default), which must be reachable from the internet:
forwarded by the gateway when the server is behind one, and published from
the container. TCP is for networks that let no UDP through. The address it
gives devices is `calls.publicAddress`, or what the dashboard's name resolves
to. The server is an ICE-lite agent: a device needs no STUN or TURN server and
sends it no candidates of its own; the server answers whatever reaches it.

### Two sizes of each camera, and who is featured

A camera is sent twice (simulcast): whole, the layer named `f`, and a third
of its size, `q` (`internal/call/video.go`). Each person is sent one layer of
each camera: the whole picture of the person they feature and the small one
of the rest, unless three or fewer of the others' cameras are on, when every
picture is whole. Whom a device features is its own choice
(`SetChatCallFeatured(callId, userId)`), or whoever is speaking when it chose
nobody (`userId` null). A switch of layers waits for a whole picture (a key
frame) of the new layer, asked for at once, and the packets the person gets
carry on in one unbroken sequence. The codecs are VP8 and H.264, whose key
frames the server can find; there is no resending, so a lost picture is asked
for whole again. A device that sends one layer only is sent as it is.

Who is speaking comes from the loudness each microphone packet carries (the
audio level header extension, `internal/call/speaker.go`): the person with the
largest share of speech over about the last second, who keeps it for a moment
while somebody else talks over them. A device that joins with
`shouldTellSpeaker: true` is told each change on `ChatCallSignals`
(`speakingUserId`, with no offer). The dashboard and the iPhone feature one
person above a row of the rest once five are on the call, or once the person
taps somebody; tapping the one chosen goes back to following whoever speaks.

A call lives in the memory of the instance that started it (`chat_call.instance`).
A server that stops ends the calls it held as failed when it starts again.

## What is kept

`chat_call` is a call: who started it, when somebody else first answered,
when and how it ended (`ended`, `missed`, `declined`, `failed`).
`chat_call_member` is each person's part: rung and what came of it (`ringing`,
`answered`, `declined`, `missed`, or `none` for whoever started it), when they
were on, whether their camera is on, and whether they are muted. The
call's line in the chat is a message with no words whose `callId` names the
call; a client draws it from the call (`call` on the message), as it goes and
after.

## A device on a call

The operations are in `internal/api/v1api/apigraph/chat_call.go`.

1. Start or answer: `StartChatCall(conversationId, isVideoOn, userIds)` starts
   one, ringing only `userIds` when given (or returns the one going in the
   chat, to join), `JoinChatCall(callId)` answers
   a ringing call or joins a going one. `DeclineChatCall(callId)` says no.
2. Open `ChatCallSignals(callId)` before connecting: the server's offers come
   there.
3. Make a peer connection with no ICE servers, add the microphone track and a
   video transceiver (`sendrecv`, with the camera's track when it is on and
   none when it is off, and two send encodings, `q` scaled down by 3 and `f`
   whole), create an offer, wait for the local addresses to be gathered, and
   send it with `JoinChatCallMedia(callId, offerSdp, shouldTellSpeaker)`. Set the
   returned `answerSdp` as the remote description.
4. Answer each offer from `ChatCallSignals` with `AnswerChatCallMedia(callId,
   answerSdp)`, one at a time and in order. An offer can arrive before the
   answer to the join has been set: keep it until then. After the join, only
   the server offers.
5. Each track received belongs to the person whose user id is its stream id;
   its kind says audio or video. Tracks come and go with offers as people join,
   leave, and turn cameras on.
6. Turn the camera on or off by replacing the video sender's track (no new
   offer is needed) and tell the others with `SetChatCallVideo(callId,
   isVideoOn)`. Muting stops sending the microphone's sound the same way, and
   `SetChatCallMuted(callId, isMuted)` tells the others.
7. `LeaveChatCall(callId)` takes the person off. The call ends when nobody is
   left.

`ChatCallEvents` tells every tab and phone of every member of the chat each
change to the call, as the member sees it: a call ringing them
(`askerRingState` is `ringing`), somebody joining or leaving, a camera, the
end. `ListGoingChatCalls` is what a device that opens reads to ring or to go
back to a call; `ReadGoingChatCall(conversationId)` is the banner a chat shows.

## Ringing

An iPhone registers a second token with the push relay, for PushKit
(`voipPushRegistration` on `RegisterAgentPhone`), and is rung with a VoIP push
of kind `call` whose sealed content names the call (`callId`) and the chat
(`conversationId`). The app reports it to CallKit at once, as iOS requires.
When the person answers or declines on another device, or the call stops
ringing them, their phones get a VoIP push of kind `call_ended` for the same
call, and stop ringing. Only phones that may still ring are sent one: iOS has
an app show a call for every VoIP push, so one sent to a phone not ringing
flashes a call on its screen. The phone that answers or declines names itself
(`agentPhoneId` on `JoinChatCall` and `DeclineChatCall`) and is not told; a
member who joins without having been rung, or an answered person when the
call ends, is told nothing. A phone whose app registered no VoIP token gets an alert instead. A
dashboard tab rings from `ChatCallEvents`.

## Transcripts

Anyone on a call can turn transcription on (`AskChatCallTranscription`), for
everybody on it: nobody is asked, and everybody is told. Each device shows a
notice that names who turned it on (`transcriptionAskedByUserId`), which the
person closes, and a mark that stays while the call lasts; whoever joins later
sees the same notice. From then on the media server keeps the microphone of
everybody on the call in files of their own (`call.Recorder`), under the data
directory (`call-recordings/<call id>`), so a restart keeps them.

When the call ends it becomes a recording (`docs/subsystems/recordings.md`)
owned by whoever turned transcription on (`recording.chat_call_id`,
`chat_call.recording_id`; `internal/agent/chat_call_transcript.go`): one
original per stretch of one person's microphone, at its offset from the
answer, naming its speaker (`recording_media.speaker_user_id`,
`speaker_name`). An original whose speaker is known is transcribed with the
voice settings' model and no speaker separation, every passage under the
speaker, whose name is then confirmed (the owner's own track as the account
owner); no voice samples are cut from it. Each member who was on the call and
shares a circle with the owner is given it at the transcript level. When all
of it is transcribed, a summary is posted into the chat by the owner's agent,
which pays for both, as for any recording. `ReadChatCall` gives the
transcript, read from the recording, to its owner and to a member it is
shared with, and names the recording to open. The sweep picks up a call a
stopped server left with its files on disk, and a recording ready for its
summary.

## Surfaces

The dashboard and the iPhone app start, ring, answer and show calls. A call
can be put away while it goes, to use the rest of the app: on the dashboard to
a small window in a corner, on the iPhone to a bar at the top of the app with
mute and hang up; either brings the call back. The
command line lists a chat's calls (`teanode agent conversation calls`) and
shows one with its transcript and the recording it became (`teanode agent
conversation call`). The agent reads them with its chat tool
(`list_calls`, `read_call`); starting a call is not an agent action.
