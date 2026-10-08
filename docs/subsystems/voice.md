# Voice

A person can talk to their agent in the drawer, like a phone call. They press
the telephone beside send; what they say streams to this server while they say
it, the server streams it on to the provider's realtime transcription, and each
utterance the provider finishes hearing is sent as an ordinary turn, the way a
typed message is. The answer is written for the ear and read aloud as it is
written, and the person can cut in on it.

## The path

```
microphone ── AudioWorklet (24 kHz PCM16, 100 ms frames)
   │  binary frames over /api/v1/agent/voice
   ▼
voice socket (internal/api/v1api/apigraph/agent_voice.go)
   │  input_audio_buffer.append
   ▼
provider realtime transcription (internal/voice)
   │  speech started/stopped, committed, transcript deltas and completions
   ▼
voice socket ── speechStarted, transcriptDelta, transcriptFinal … ──▶ drawer
                                                                      │
                               AskAgent, surface "voice" ◀────────────┘
```

**The provider detects speech.** The session asks for `server_vad`: the
provider says when somebody starts and stops talking and commits each
utterance itself; nothing in TeaNode decides where a turn ends. That is why
`gpt-live-transcribe` and `gpt-realtime-whisper`, which need the application
to commit each turn, are refused by the configuration. Speech-start events
reach the drawer as soon as the provider sends them, which is what cutting in
on a spoken answer will be built on.

**The socket transcribes and speaks; it starts no turns.** It signs the person in (a token in the
first message, or the session cookie from a page of this server), finds their
agent, checks the agent's budget, opens the provider's session with the
operator's key, which never leaves the server, and relays both ways. Each
transcription's usage is recorded against the agent with the kind `voice`, so
it counts toward the day like any model call. A session lasts at most 30
minutes and ends after a minute without audio.

**The drawer sends the turn.** A final transcript goes through the drawer's
own `send`, with what the person is viewing, exactly as if typed, except that
its surface is `voice`. So a spoken turn steers a running turn, answers an open
question card, and crosses instances the way a typed one does. A pause longer
than `agent.voice.silenceMS` in the middle of a request makes two utterances,
and the second reaches the running turn as a follow-up.

**Order and duplicates.** The provider commits utterances in the order they
were spoken (each names the one before it) but may finish transcribing them in
any order. `internal/voice` holds each finished transcript until every earlier
one is finished or has failed, numbers them, and tells each once; the drawer
sends each utterance id once.

**What the agent is told.** The `voice` surface says the words were
transcribed, so a name may be misheard, and asks for answers written for the
ear: the answer first, a few short spoken sentences, no tables, headings or
code, numbers said as a person says them, at most one question, and a check
before acting on a misheard name, sum or day. The transcription itself is told
whom the person is talking to, which is what gets the agent's name spelled
right.

**Speaking the answer.** The drawer follows the conversation's events as it
always does; in voice mode it also cuts each turn's text into sentences as the
deltas stream in (the first sentence alone, so the answer starts at once, then
a few at a time), leaves out code blocks and what only means something on a
screen (Markdown marks, web addresses), and asks the socket to speak each
piece (`speakAnswer`). The socket calls the provider's text-to-speech
(`gpt-4o-mini-tts` by default) with the operator's key and streams the audio
back (`answerAudio`, then `answerAudioDone`), and the drawer plays the pieces
in order in the same audio context the microphone is read in, asking for the
next few while one plays. The stored `message` event repeats what the deltas
said, so only what they missed is spoken, and an event replayed after a
reconnection is never spoken twice. A question card is read out; a
confirmation card says it needs approving on screen. A turn is spoken when
voice mode saw it start, or from the moment the person spoke into it. The
speaker button in the panel turns reading aloud off on that device. Each
person chooses the voice on their agent's settings, with a sample to listen
to, or tells the agent mid-call (`agent_profile`, `speech_voice`); the next
sentence is read in it. Empty is the server's `speechVoice`.

**Cutting in.** When the provider says somebody started talking while an
answer plays, the answer drops to a murmur at once and pauses where it is if
they keep on for more than 0.6 seconds. What they said then decides: if its
words follow the answer's own, it is the answer heard back through the
microphone, so it is dropped and the answer goes on from where it paused; a
noise that transcribes to nothing also lets it go on. Anything else is the
person: the answer ends, the audio still being made is cancelled, the rest
of the message being written at that moment is not spoken, and the turn they
said goes with `interruptedAnswer`, what was played to them (a piece cut off
counts only up to its last whole word) and what was not. The agent reads
that ahead of their words for that turn, so it never assumes they heard what
they did not. Their turn steers the running turn as a typed one would; the
stop button stops it.

**Voice mode in the drawer.** Pressing the telephone puts the drawer in
voice mode: the box is replaced by a meter of five bars that follows how loud
the microphone or the answer is, green while the provider says it hears speech,
in the accent while the answer plays, and breathing slowly while the agent
works; beside it the words heard so far, stop for a running turn, the speaker
button, and the red handset that hangs up. The conversation stays visible above it. The box comes back only
when the person hangs up, or listening stops on its own (a lost
connection, a refused microphone).

**Safari on a phone.** The audio context is made and resumed in the same tap
that asks for the microphone, before anything is waited on, and the capture
node is connected to the speakers through a gain of zero, since WebKit only
runs nodes that reach them. Nothing is played.

**Echo.** The answer would be heard back through the microphone and taken
for the person. Three things stand against it: the browser's echo
cancellation, asked for and reported in the first message (the server logs
what was granted); the answer playing in the same audio context as the
capture, which is what the browser cancels; and the check above, which drops
an utterance whose words follow what was just played. On a phone, Safari is
asked for its play-and-record audio session, so the answer comes out of the
speaker with the phone's own echo cancellation.

## Configuration

`agent.voice` in `docs/configuration.md`: on or off, which provider of kind
`openai`, the model (`gpt-4o-transcribe` by default) and the pause that ends a
turn. It is on the server's agent settings page and in `teanode settings`. The
drawer asks `ReadAgentVoice` whether to show the microphone.

## What it does not do yet

- Approve a confirmation card by voice: it is read out, and approved on
  screen.
- Measure interruption latency and false cut-ins across devices and rooms
  (#347); headphones avoid echo altogether.
- Refresh what the transcription is told as the conversation moves on (#346).
- Voice in the browser extension's drawer, whose page has a microphone
  permission of its own.
- Keep listening across a dropped connection: the drawer says it stopped, and
  the person presses the button again.
