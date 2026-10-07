# Voice input: talking to the agent in the drawer, transcribed by the provider, sent as an ordinary turn

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

It is the first part of voice mode, the ground issue #345 asks for; #346 (transcription context), #347 (barge-in) and #348 (spoken answers) build on it.


## Purpose / Big Picture

Today a person types to their agent in the drawer. After this plan they can press a microphone button in the drawer and talk: what they say streams to the server as they speak, the server streams it on to the provider's realtime transcription, the drawer shows the words as they are recognized, and each finished utterance is sent as an ordinary turn, exactly as if they had typed it and pressed send. The agent's tools, memory, confirmation cards, steering and conversation history are the ones a typed turn uses, because it is a typed turn as far as the agent is concerned, except that it knows the person spoke it.

To see it working: the operator turns on voice under the server's agent settings, naming an OpenAI provider. In the drawer a microphone button appears beside send. Pressing it asks the browser for the microphone; saying "what is on my calendar tomorrow" shows the words in a caption above the box as they arrive, and a second after the person stops talking the line appears in the conversation and the agent answers it. Pressing the button again stops listening.


## Progress

- [x] (2026-10-07) Read #345 to #348 and the code they name; surveyed the WebSocket endpoints, AskAgent, providers, settings, the drawer and the instance rules; probed OpenAI's realtime transcription with the server's key (see Surprises).
- [ ] Milestone 1: `agent.voice` in the configuration, its settings on the server's agent page and in `teanode settings`, and a `voice` surface.
- [ ] Milestone 2: `internal/voice`: the provider session (dial, configure, relay audio, read events) and the turn ordering, tested against a fake provider.
- [ ] Milestone 3: the `/api/v1/agent/voice` socket: sign-in, the person's agent, the budget, relay both ways, usage recorded; `ReadAgentVoice` for the drawer.
- [ ] Milestone 4: the drawer: microphone capture through an AudioWorklet at 24 kHz PCM16, the socket, the caption, final transcripts sent through the drawer's own send; the Permissions-Policy that allows the microphone.
- [ ] Milestone 5: tests, docs, a run with a real voice on a dev server, deploy.


## Surprises & Discoveries

- Probing the provider (2026-10-07, `wss://api.openai.com/v1/realtime?intent=transcription`, Bearer key, no beta header): a `session.update` with `session.type: "transcription"`, `audio.input.format {type: "audio/pcm", rate: 24000}`, `audio.input.transcription {model: "gpt-4o-transcribe", prompt}` and `turn_detection {type: "server_vad"}` is acknowledged with `session.updated`. Speech produces `input_audio_buffer.speech_started` (with `item_id`, `audio_start_ms`), then `speech_stopped` and `committed` (with `item_id` and `previous_item_id`), transcript `delta`s, and `conversation.item.input_audio_transcription.completed` about one second after speech stopped, with token `usage`.
- `semantic_vad` with eagerness auto did not end a turn within three seconds of trailing silence; `server_vad` with 500 ms of silence ended it promptly.
- A transcription prompt naming the agent turned "T-Note" into "TeaNode".
- A pause inside one spoken request produced two utterances 2.5 seconds apart. In TeaNode the second becomes a steering message into the running turn, which is how a typed follow-up behaves.
- `gpt-live-transcribe` and `gpt-realtime-whisper` require turn detection off and commits from the application, so they cannot be used with the provider's speech detection this plan relies on.
- The dashboard sends `Permissions-Policy: microphone=()`, which blocks the microphone outright, and its CSP allows scripts only from the server itself, so the AudioWorklet is a file the server serves, not a blob.


## Decision Log

- Decision: the provider detects speech and ends turns (`server_vad`), and its speech-start events reach the browser as soon as they arrive, for the barge-in of #347. The application does not run its own detector.
  Rationale: the person asked for VAD and barge-in to be the provider's; the probe shows it works in a transcription session with `gpt-4o-transcribe`. Models that need application commits are refused by validation rather than silently losing turn detection.
  Date/Author: 2026-10-07.
- Decision: the voice socket transcribes and nothing more; the drawer sends each final transcript through its ordinary send, with the `voice` surface.
  Rationale: in TeaNode a turn is the drawer calling AskAgent with what the person is viewing, its own optimistic line, conversation adoption and run following. A turn the server started on its own would lose the viewing context, and would behave differently from a typed one across instances (AskAgent is load-balanced like any request; a turn started inside the instance holding the socket is not). Sending it from the drawer makes a spoken turn a typed turn in every respect.
  Date/Author: 2026-10-07.
- Decision: the socket orders finished utterances itself: transcripts are released in the order the provider committed the audio (`previous_item_id`), and each utterance's id travels with it, so the drawer sends each once, in order, even when the provider completes them out of order.
  Date/Author: 2026-10-07.
- Decision: a new surface, `voice`, tells the agent the words were transcribed (a name may be misheard) and to answer plainly and briefly; the answer is still shown in the drawer as Markdown.
  Date/Author: 2026-10-07.
- Decision: the configuration is the operator's (`agent.voice`): on or off, which OpenAI provider (its key), the model, and the silence that ends a turn. It is shown on the server's agent settings and in `teanode settings`. The drawer learns only whether voice is available, through `ReadAgentVoice`, which needs `agent:use`.
  Date/Author: 2026-10-07.
- Decision: the transcription prompt names the agent and the person ("A person talking to their personal agent, Bertie."); the richer, refreshed context of #346 is later work.
  Date/Author: 2026-10-07.
- Decision: no voice in the browser extension's drawer for now; it is hidden there.
  Rationale: the extension's page is another origin with its own microphone permission, which is a separate piece of work.
  Date/Author: 2026-10-07.


## Outcomes & Retrospective

(To be written when the work is done.)


## Context and Orientation

- `internal/api/v1api/apigraph/websocket.go`: the GraphQL socket, whose sign-in (a token in the first message, or the session cookie from the same origin) the voice socket copies; `agent_tab.go` for `agentOfPerson`.
- `internal/api/path.go`: paths, and `PublicPaths`, which every socket that signs in by message is in.
- `internal/agent/surface.go`: the surfaces; `internal/agent/usage.go`: `RecordUsage`.
- `internal/config/agent.go`, `internal/api/v1api/apigraph/settings_agent.go`, `web/src/pages/settings/agentSettings.tsx`, `internal/client/settings.go`, `docs/configuration.md`: an operator setting, end to end.
- `web/src/components/agentDrawer.tsx`: the composer and `send`; `web/src/api.ts`: how the dashboard opens a socket.
- `internal/web/middlewares.go`: the Permissions-Policy header.


## Plan of Work

The socket's protocol, between the drawer and the server, at `/api/v1/agent/voice`: the drawer's first message is `{"voiceEvent": "hello", "authorization"?: "Bearer …"}`; the server answers `{"voiceEvent": "ready", "sampleRate": 24000}` or `{"voiceEvent": "refused", "errorMessage": …}` and closes. After that the drawer sends binary messages, each mono 16-bit little-endian PCM at 24 kHz, at most 64 KB, and `{"voiceEvent": "stop"}` to end. The server sends `speechStarted` and `speechStopped` (with `utteranceId`), `transcriptDelta` (`utteranceId`, `transcriptText`), `transcriptFinal` (`utteranceId`, `transcriptText`, `utteranceSequence`) in commit order, `transcriptFailed` (`utteranceId`, `errorMessage`), and `error` (`errorMessage`) before closing.

`internal/voice` holds the provider session, behind an interface so tests use a fake provider served by `httptest`: dial with the provider's base URL turned into `wss://…/realtime?intent=transcription`, send the session configuration, wait for `session.updated`, then relay audio as `input_audio_buffer.append` and read events into the protocol above, holding finished transcripts until every earlier committed utterance is finished or failed.

The socket handler signs the person in, finds their agent, checks voice is on and the agent within its budget, opens the provider session, and relays both ways until either side closes or 30 minutes pass; each completed transcription's usage is recorded against the agent with the kind `voice`.

The drawer: a microphone button, shown when `ReadAgentVoice` says voice is available and the drawer is not the extension's; pressing it asks for the microphone (echo cancellation, noise suppression), loads the worklet, opens the socket and streams 100 ms frames; a caption shows the utterance being heard; a final transcript is sent through `send` with the `voice` surface, once per utterance id, in order. Errors are toasts. Switching conversation, closing the drawer or pressing the button again stops everything.


## Validation and Acceptance

`make test`, `make lint-ci` and the dashboard's tests pass. With voice on and a key on the dev server, Chrome started with a fake microphone playing a recorded request sends exactly one turn per utterance, in order, and the agent answers it; the time from the end of speech to the final transcript and to the first answer is recorded here. With voice off, no button appears and the socket refuses.


## Idempotence and Recovery

No migration. Voice is off until the operator turns it on. A dropped socket ends the session: what was heard but not finished is lost, which the drawer says, and the person presses the button again.
