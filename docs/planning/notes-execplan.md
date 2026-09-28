# Notes: the phone's Notes app, kept in the mailbox, which the agent can help write

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The Notes app on a phone can keep its notes in a mail account over IMAP. Each note is a message in a folder of that account, usually named "Notes", and this server already stores those messages. Today they show up as mail: a folder of messages from yourself, each edit of a note a new message, the old ones still counted. Nobody can read them as notes, and the agent cannot help write one.

After this plan a person's notes are notes everywhere this program shows them. The dashboard's notes folder shows a list of notes with their titles, opens one to read and edit it, and makes new ones; the command line has `teanode note list|show|add|edit|remove`; the agent has a `note` tool that lists, reads, writes and changes notes ("add the packing list to my notes", "tidy up my recipe note"). A note written here appears in the Notes app on the phone at its next sync, and one written on the phone appears here. Notes are no longer treated as mail: the agent's sorting, search and summaries leave them alone.

To see it working: on a phone with the mail account's Notes switched on, write a note; open the dashboard's Notes folder and see it by its title; change a line in the dashboard and see the change on the phone; ask the agent to add a note and see it on the phone.

## Progress

- [x] (2026-09-28) Looked at a real note on the server and surveyed every path that reads mailbox items (see Surprises).
- [ ] Milestone 1: storage. A note is recognized when it is appended, older notes are recognized once, and the dashboard stops listing and counting messages flagged deleted.
- [ ] Milestone 2: one set of operations: GraphQL, `teanode note`, and the agent's `note` tool; notes left out of the mail pipelines.
- [ ] Milestone 3: the dashboard's notes view.
- [ ] Milestone 4: docs, deploy, a check with a phone.

## Surprises & Discoveries

- Observation: iOS writes a note as a single `text/html` part with `X-Uniform-Type-Identifier: com.apple.mail-note`, `X-Universally-Unique-Identifier` (the note's identity), `X-Mail-Created-Date` and a Subject that is the note's first line. The Message-ID is not the note's identifier.
  Evidence: the headers of a note on the development server.
- Observation: an edit on the phone appended a new message with the same `X-Universally-Unique-Identifier` and set `\Deleted` on the old one without expunging it. The dashboard lists and counts items flagged `\Deleted`, so one note showed as two messages.
  Evidence: two `mailbox_item` rows in the Notes folder, one with `deleted = true`, both with the same identifier.
- Observation: a message appended over IMAP keeps a CRLF at the end of each stored header line, while mail received over SMTP does not. `rawView` in `internal/api/v1api/apimail/apimail.go` joins headers with CRLF, so an appended message downloads with a blank line after every header and does not parse. IMAP itself is unaffected: `joinMessage` trims first.
- Observation: nothing filters by folder kind "custom", so a note is sorted by the agent's triage backfill (`ListMailWithoutInsight`), embedded, found by `mail_search` across the mailbox, and counted, like any message.
- Observation: the IMAP LIST reply uses the same `ListFolders` counts as the dashboard, so hiding `\Deleted` items has to be the dashboard's choice, not the database's.

## Decision Log

- Decision: a note is a `mail` row of a new kind, `note`, with a new nullable column `mail.note_identifier` holding `X-Universally-Unique-Identifier`. The note is the newest item not flagged `\Deleted` among the items of that mailbox with that identifier.
  Rationale: the kind is how every pipeline that should not treat it as mail can leave it out with one condition, and the identifier is how versions of one note are told apart from different notes without reading the stored headers.
  Date/Author: 2026-09-28.
- Decision: the notes folder is found by what it holds, not by its name: the folder of the mailbox holding the most notes, else a folder named "Notes", else one made named "Notes" when the first note is written here.
  Rationale: the person worried that the name differs by language; the header does not.
- Decision: saving a note writes a new message with the same identifier and creation date and removes the previous version (flag and expunge in one transaction), the way the phone replaces one, so the phone's next sync sees one message per note.
- Decision: the operations take a note by its identifier (`noteId`, the UUID), which stays the same across edits, so an agent or a script holding it can edit twice.
- Decision: the dashboard hides items flagged `\Deleted` in every list and count it shows; IMAP still reports them until they are expunged.
- Decision: the word is note in every name this program chooses. `com.apple.mail-note` and the `X-` header names are the phone's contract and stay as written.

## Context and Orientation

Mail is stored as a `mail` row (`internal/models/mail.go`: kind, subject, from, message ID) with its header lines and body in storage (`Storage.Put(ctx, mailId, headers, body)`), and a `mailbox_item` row per folder it is in (`internal/models/mailbox.go`: folder, UID, flags including `Deleted`, which is IMAP's `\Deleted` awaiting EXPUNGE). IMAP APPEND is `session.Append` in `internal/imap/server.go`, building the row in `mailFromMessage` (`internal/imap/selected.go`); it sets kind `outgoing` or `draft`. Removing an item for good is `tx.DeleteItems` (what EXPUNGE calls), which marks the mail unreferenced for later scavenging.

The closest existing write is saving a draft: `SaveMailboxDraft` in `internal/api/v1api/apigraph/mailbox_compose.go` composes with `mailer.ComposeInTransaction` and calls `mailbox.Commands.SaveDraft` (`internal/mailbox/save_draft.go`), which creates the mail, puts it in storage, adds the item, indexes it for search and removes the previous version, all in one transaction. A note save is the same shape with kind `note`, the notes folder, and the note headers (`mailer.Message.Headers`).

Agent tools reach GraphQL through `operator.Execute` with documents in `internal/client`, checked against the schema by `TestClientDocumentsMatchTheSchema`; `internal/agent/tools/reminder/reminder.go` is a recent example of the pattern. Mailboxes granted to the agent are listed by `GrantedMailboxes` in `internal/agent/tools/mailbox/mailbox.go`.

## Plan of Work

Milestone 1, storage. Migration `0117_mail_note.sql` adds `mail.note_identifier varchar(64)` and an index on `(note_identifier)` where it is not null. `models.MailKindNote = "note"` and `Mail.NoteIdentifier`. `mailFromMessage` sets kind `note` and the identifier when the headers say `com.apple.mail-note`. A one-time pass at startup (idempotent, recorded so it runs once, and cheap because it reads only items in custom folders of kind `outgoing`) reads the stored headers of those mails and marks the notes among them. `rawView` trims each header before joining. The dashboard's folder counts and item and thread lists leave out items flagged `\Deleted` (an option on the db calls the GraphQL layer sets; IMAP does not set it).

Milestone 2, operations. In `internal/api/v1api/apigraph/note.go`: `ListNotes(mailboxId)` (identifier, title, a short plain text preview, created and modified times, folder), `GetNote(mailboxId, noteId)` (with `html` and `text`), `SaveNote(mailboxId, noteId?, html?, text?)` (text is turned into the phone's HTML: a `div` a line, blank lines as `<div><br></div>`, lines starting "- " as a list), `DeleteNote(mailboxId, noteId)`. The title is the first non-empty line and becomes the Subject, as on the phone. Permissions: `mail:read` to read, `mail:write` to write, on the person's own mailbox. `internal/client/note.go` holds the documents; `internal/cmd/note.go` adds `teanode note list|show|add|edit|remove` (`add` and `edit` read the text from an argument or `--file -`); `internal/agent/tools/note/note.go` adds the `note` tool (list, read, write, edit, remove; `edit` replaces the text, `append` adds to the end) for mailboxes granted to the agent. Notes are left out of `ListMailWithoutInsight`, `ListMailWithoutEmbedding`, and mail search and threads unless the folder asked for is the notes folder.

Milestone 3, the dashboard. When the folder open in the mailbox page is the notes folder (it holds notes), the page shows notes instead of messages: a list of titles with preview and date, newest first, and beside it (under it on a phone) the open note in the rich text editor the compose page uses, saved on a button, with New note and Delete. The rail keeps the folder where it is.

Milestone 4: `docs/subsystems/notes.md`, a row in `docs/reference/command-line.md`, an end-to-end task, deploy, and a check with a phone.

## Validation and Acceptance

Milestone 1: an APPEND test with an iOS-shaped note stores kind `note` with its identifier; the one-time pass marks an existing one; a downloaded appended message parses; the dashboard folder count of a folder holding one note and its flagged older version is 1. Milestone 2: a note saved through GraphQL has the note headers, the same identifier and creation date as the version it replaces, and the old version is gone from IMAP after a FETCH; the CLI round trip and the tool's list, write, edit and remove work against the server. Milestone 3: the notes folder shows notes at 390 and 1400 pixels, light and dark, and editing one changes it. Milestone 4: a note edited in the dashboard shows the change on the phone.

## Idempotence and Recovery

The migration adds a nullable column; its reverse drops it, and the notes read as mail again, which is how they were. The one-time pass only sets the kind of mails whose headers say they are notes, and can run again.

## Interfaces and Dependencies

    // internal/models/mail.go
    const MailKindNote MailKind = "note"
    // Mail.NoteIdentifier string: X-Universally-Unique-Identifier of a note

    // GraphQL
    type NoteView struct { ID (the identifier), MailboxID, FolderID, Title, Preview, HTML, Text string; CreatedAt, ModifiedAt time.Time }
    ListNotes(mailboxId) []NoteView            // HTML and Text left empty
    GetNote(mailboxId, noteId) NoteView
    SaveNote(mailboxId, noteId, html, text) NoteView
    DeleteNote(mailboxId, noteId) bool

No new libraries.
