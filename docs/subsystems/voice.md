# Voice

A person can talk to their agent in the drawer. They press the microphone
beside send; what they say streams to this server while they say it, the
server streams it on to the provider's realtime transcription, and each
utterance the provider finishes hearing is sent as an ordinary turn, the way
a typed message is. Answers are written for the ear. Spoken answers and being
able to cut in on them are later work (issues #347 and #348).

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

**The socket only transcribes.** It signs the person in (a token in the
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

## Configuration

`agent.voice` in `docs/configuration.md`: on or off, which provider of kind
`openai`, the model (`gpt-4o-transcribe` by default) and the pause that ends a
turn. It is on the server's agent settings page and in `teanode settings`. The
drawer asks `ReadAgentVoice` whether to show the microphone.

## What it does not do yet

- Speak the answer, or let the person cut in on it (#347, #348).
- Refresh what the transcription is told as the conversation moves on (#346).
- Voice in the browser extension's drawer, whose page has a microphone
  permission of its own.
- Keep listening across a dropped connection: the drawer says it stopped, and
  the person presses the button again.
