# Files in the conversation: what the agent hands the person, and what it can look at

This ExecPlan is a living document. The sections below are kept current
as the work proceeds.

## Purpose / Big Picture

The agent could take a photo from the person and look at it, and could
make a page or a drawing, but it could not hand the person anything: not
the photo attached to a message, not a file from their computer, not a
video. An answer carried no attachment, a video was always a download,
and a chat app never saw a picture from the agent because the agent had
no way to make or fetch one. After this, the agent hands a file to the
person with one tool — from a message's attachments, from an attached
computer, or one already in the conversation — and the drawer shows a
picture inline, plays a video or a sound, and offers anything else as a
download; a chat app gets a photo, a video or a document; and the agent
can look at a picture it fetched, which is how it answers "what is in
the photo Maria sent".

## Progress

- [x] (2026-09-11 18:40Z) Milestone 1 — `internal/agent/tools/share`,
      the daemon's `fetch` (base64, up to 32 MB), `Result.Images` and
      the turn's `lookingAt`, video and sound inline, `FileCard` in the
      drawer, the chat turn sending shared files, Telegram's `sendVideo`
      and `sendAudio` (and a document for a picture over 10 MB).
- [x] (2026-09-11 19:10Z) Milestone 2 — tests for the tool (a file
      from the computer, one already there, look on a picture and not
      on a document, the computer's refusal relayed), the daemon's
      fetch, Telegram's video and oversized photo; the changelog. On the
      dev server: a picture uploaded to the conversation, "hand it back
      and say what colour it is" — the model called share_file with
      look, answered "orange", and the drawer showed the picture under
      the tool line with its name and size.

## Surprises & Discoveries

- (none yet)

## Decision Log

- Decision: one tool, `share_file`, with a source — `mail`, `computer`,
  `conversation` — rather than a flag on `mail_read` and on the
  filesystem tool.
  Rationale: what the person sees is the same whatever the source: a
  file of the conversation, under the tool line, sent to the chat; one
  tool means one place the model learns that from.
  Date/Author: 2026-09-11, Claude with the owner.
- Decision: a shared file is a row of `agent_attachment` marked
  `shared` in its message id, as an artifact is marked `artifact`, with
  the bytes copied into the conversation's storage (a mail part, a file
  from the computer) or the existing row named (a file already there).
  Rationale: the drawer, the chat apps and the retention sweep already
  know attachments of a conversation; a copy means the file the person
  was shown stays what it was, whatever happens to the message or the
  computer later.
  Date/Author: 2026-09-11, Claude.
- Decision: the model looks at a picture by asking for it (`look: true`
  on the tool): the picture goes to it as an image part on a user turn
  right after the tool's result, for this turn only.
  Rationale: a tool's result is text to every provider; an image is a
  user turn's. It is not stored: the transcript keeps the tool line, and
  the next turn can ask again.
  Date/Author: 2026-09-11, Claude.
- Decision: a video or a sound is served inline, with its own type, as a
  picture is; everything else stays a download.
  Rationale: a browser plays a video and runs nothing in it; the danger
  a download guards against is a page or a script, and those are still
  never served as themselves unless they are an artifact in its sandbox.
  Date/Author: 2026-09-11, Claude.

## Outcomes & Retrospective

Done 2026-09-11. One tool hands over a file from three places and the
drawer, the chat apps and the model all see it. What held up: the
artifact's shape — a row of the conversation, an id and an address in
the tool's answer, a card under the tool line — took the new kind
without changes to the drawer's transcript or the chat's turn. What was
learned: a provider takes a picture only on a user turn, and the
Anthropic client already folds a user turn into the tool results before
it, so a picture the model asked for arrives in the same request.
Left open: a photo inside a message is still listed, not seen, unless
the agent asks for it; letting `mail_read` show pictures on its own
would cost tokens on every read, so it stays an ask.

## Context and Orientation

`internal/agent/tools/artifact` is the pattern: a tool that stores a
file of the conversation and answers with its id and address, which the
drawer (`ArtifactCard` in `web/src/components/agentDrawer.tsx`) draws
under the tool line and the chat turn (`internal/channel`) sends. A
person's uploads reach the model as image parts in `userTurn`
(`internal/agent/attachment.go`). A mail part is read the way the
reader's attachment view does it (`internal/api/v1api/apimail`):
`storage.Get` and `mailparse.PartAt` by the index the content view
lists. The daemon (`internal/computer`) reads text files; a binary file
needs a way across as bytes.

## Plan of Work

### Milestone 1 — the tool and everything that shows what it hands over

- `internal/computer`: a `fetch` action on the filesystem: the file's
  bytes as base64 with a content type, up to `fetchBytes`.
- `internal/agent/tools/share`: `share_file` with `source`, `item_id`
  and `attachment` (name or number) for mail, `computer` and `path` for
  a computer, `attachment_id` for the conversation, `look` and
  `caption`. Answers `attachment_id`, `name`, `content_type`, `size`,
  `url`; with `look`, the picture rides on `Result.Images`.
- `internal/agent/ask.go`: images a tool answered with go to the model
  as a user turn after that round's results.
- `mail_read` numbers the attachments it lists, so a name or a number
  picks one.
- `agent_attachment.go`: video and audio inline.
- The drawer: a `FileCard` under the tool line — picture, video, sound,
  or a download — with the name and an open button, in the artifact's
  box.
- The chat turn: a shared file goes as a photo, a video or a document;
  Telegram gets `sendVideo` and `sendAudio`.

### Milestone 2 — checks

Tests for the tool (conversation source, look, refusals), the daemon's
fetch, Telegram's video; `make test`, `make lint-ci`, `make lint`; a
turn on the dev server that shares a photo from a message; the
changelog and the command-line reference where it lists tools.

## Concrete Steps

As the milestones say; `make test` needs Docker.

## Validation and Acceptance

"Show me the photo Maria sent" shows the photo under the tool line in
the drawer and answers what is in it; in Telegram the photo arrives as a
photo. "Send me ~/Videos/clip.mp4 from gen7" plays in the drawer and
arrives in Telegram as a video.

## Idempotence and Recovery

No migration. A shared file is a row and a stored file, swept with the
conversation's other attachments.

## Interfaces and Dependencies

`tools.Result.Images`, `computer.Of(run, name)`, the daemon's `fetch`
action (protocol unchanged: an action the older daemon answers with an
error the tool relays).
