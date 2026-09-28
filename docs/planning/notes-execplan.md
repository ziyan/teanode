# Notes: the phone's Notes app, kept in the mailbox, which the agent can help write

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The Notes app on a phone can keep its notes in a mail account over IMAP. Each note is a message in a folder of that account, usually named "Notes", and this server already stores those messages. Today they show up as mail: a folder of messages from yourself, each edit of a note a new message, the old ones still counted. Nobody can read them as notes, and the agent cannot help write one.

After this plan a person's notes are notes everywhere this program shows them. The dashboard's notes folder shows a list of notes with their titles, opens one to read and edit it, and makes new ones; the command line has `teanode note list|show|add|edit|remove`; the agent has a `note` tool that lists, reads, writes and changes notes ("add the packing list to my notes", "tidy up my recipe note"). A note written here appears in the Notes app on the phone at its next sync, and one written on the phone appears here. Notes are no longer treated as mail: the agent's sorting, search and summaries leave them alone.

To see it working: on a phone with the mail account's Notes switched on, write a note; open the dashboard's Notes folder and see it by its title; change a line in the dashboard and see the change on the phone; ask the agent to add a note and see it on the phone.

## Progress

- [x] (2026-09-28) Looked at a real note on the server and surveyed every path that reads mailbox items (see Surprises).
- [x] (2026-09-28) Milestone 1: storage. Migration 0117, `MailKindNote` and `Mail.NoteIdentifier`, APPEND recognizes a note (`internal/notes`), a pass at start marks the notes stored before (`internal/cmd/server/notebackfill.go`), `rawView` trims headers, and the dashboard's folder counts, item lists and thread lists leave out items flagged deleted (`db.FolderOptions`, `ItemOptions.Deleted`); IMAP still counts them.
- [x] (2026-09-28) Milestone 2: `ListNotes`, `GetNote`, `SaveNote`, `DeleteNote` (`apigraph/note.go`, commands in `internal/mailbox/notes.go`), `internal/client/note.go`, `teanode note list|show|add|edit|remove`, and the agent's `note` tool. Notes are left out of `ListMailWithoutInsight`, `ListMailWithoutEmbedding`, and mailbox-wide item and thread lists (`ItemOptions.ExcludeMailKinds`). The `note` tool is the 61st in the catalog; the limit in `TestTheCatalogStaysShort` went from 60 to 61, since it is a new thing rather than a new verb.
- [x] (2026-09-28) Milestone 3: the dashboard's notes view (`web/src/components/notes.tsx`), shown for a custom folder that holds notes or is named "Notes".
- [x] (2026-09-28) Review fixes: trash and junk left out of notes, current version by arrival with a conflict check, notes with pictures read-only, dashboard-safe HTML, `noteCount` deciding the notes view, `folderId` for a new note, the 0117 indexes and a backfill paged by id, deleted items left out of `GetMailboxThread`.
- [ ] Milestone 4: docs, deploy, a check with a phone.

## Surprises & Discoveries

- Observation: iOS writes a note as a single `text/html` part with `X-Uniform-Type-Identifier: com.apple.mail-note`, `X-Universally-Unique-Identifier` (the note's identity), `X-Mail-Created-Date` and a Subject that is the note's first line. The Message-ID is not the note's identifier.
  Evidence: the headers of a note on the development server.
- Observation: an edit on the phone appended a new message with the same `X-Universally-Unique-Identifier` and set `\Deleted` on the old one without expunging it. The dashboard lists and counts items flagged `\Deleted`, so one note showed as two messages.
  Evidence: two `mailbox_item` rows in the Notes folder, one with `deleted = true`, both with the same identifier.
- Observation: a message appended over IMAP keeps a CRLF at the end of each stored header line, while mail received over SMTP does not. `rawView` in `internal/api/v1api/apimail/apimail.go` joins headers with CRLF, so an appended message downloads with a blank line after every header and does not parse. IMAP itself is unaffected: `joinMessage` trims first.
- Observation: nothing filters by folder kind "custom", so a note is sorted by the agent's triage backfill (`ListMailWithoutInsight`), embedded, found by `mail_search` across the mailbox, and counted, like any message.
- Observation: the IMAP LIST reply uses the same `ListFolders` counts as the dashboard, so hiding `\Deleted` items has to be the dashboard's choice, not the database's.
- Observation: marking a note needs a record of which old messages were already looked at, so the pass does not read them again at every start. `mail.note_identifier` is null for a message stored before the migration and never looked at, empty for one looked at that is not a note, and the identifier for a note. `CreateMails` writes empty, so the pass reads only old rows, and only once. The indexes are one on `note_identifier WHERE kind = 'note'`, for the note queries, and one on `id WHERE note_identifier IS NULL`, which the pass walks by id.
- Observation: the search index (`mx.SearchDocument`) reads only `text/plain` parts, and a note is HTML alone. `SaveNote` adds the note's text to the search document itself. A note appended by the phone is indexed by its subject only.
- Observation: `ListNotes` reads each note from storage to build its preview and read its creation date. That is one read per note, which is fine for a few hundred notes on the filesystem and slower on S3.
- Observation: the agent's tool catalog has a fuse at 60 tools (`TestTheCatalogStaysShort`), and `note` is the 61st. The fuse is now 61: it is there to stop new verbs for an existing thing, and a note is a new thing.

## Decision Log

- Decision: a note is a `mail` row of a new kind, `note`, with a new nullable column `mail.note_identifier` holding `X-Universally-Unique-Identifier`. The note is the newest item not flagged `\Deleted` among the items of that mailbox with that identifier.
  Rationale: the kind is how every pipeline that should not treat it as mail can leave it out with one condition, and the identifier is how versions of one note are told apart from different notes without reading the stored headers.
  Date/Author: 2026-09-28.
- Decision: the notes folder is found by what it holds, not by its name: the folder of the mailbox holding the most notes, else a folder named "Notes", else one made named "Notes" when the first note is written here.
  Rationale: the person worried that the name differs by language; the header does not.
- Decision: saving a note writes a new message with the same identifier and creation date and removes the previous version (flag and expunge in one transaction), the way the phone replaces one, so the phone's next sync sees one message per note.
- Decision: the operations take a note by its identifier (`noteId`, the UUID), which stays the same across edits, so an agent or a script holding it can edit twice.
- Decision: the dashboard hides items flagged `\Deleted` in every list and count it shows; IMAP still reports them until they are expunged.
- Decision (review): notes in the trash or junk are not notes. `ListNoteItems` leaves them out, `NotesFolder` never picks those folders, and a save expunges only the versions in the folder of the current version, never a copy elsewhere.
  Date/Author: 2026-09-28.
- Decision (review): the current version is the one filed last (`mailbox_item.added_at`, then id), not the one with the newest message date, which the appending client chooses; `modifiedAt` is that filing time. `SaveNote` takes an optional `expectedModifiedAt` and refuses with a conflict when the current version's differs to the second. The dashboard and the agent tool's `edit` and `append` pass it; the CLI does not, and the last writer wins.
  Date/Author: 2026-09-28.
- Decision (review): a note whose message is multipart or whose HTML has an image is `isEditable: false`: readable everywhere, refused by `SaveNote`, read-only in the dashboard. Writing it from HTML alone would drop the pictures and attachments.
  Date/Author: 2026-09-28.
- Decision (review): the note HTML a read returns is made fit for the dashboard's own page on the server (no style elements, class, id, scripts or handlers), and the dashboard also passes it through `quotableHtml`.
  Date/Author: 2026-09-28.
- Decision (review): the dashboard decides the notes view from the folder list, not from `ListNotes`: `noteCount` is counted on the dashboard's `ListFolders` path only (not IMAP's), and a custom folder is shown as notes when all its items are notes, or it is empty and named "Notes". A folder mixing notes and mail stays mail. `SaveNote` takes an optional `folderId` for a new note, used when it is a folder of the mailbox other than trash and junk.
  Date/Author: 2026-09-28.
- Decision (review): migration 0117 was edited in place, since it had run on one development server only, and made safe to run again. The note backfill pages by id.
  Date/Author: 2026-09-28.
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
    type NoteView struct { ID (the identifier), MailboxID, FolderID, Title, Preview, HTML, Text string; IsEditable bool; CreatedAt, ModifiedAt time.Time }
    ListNotes(mailboxId) []NoteView            // HTML and Text left empty
    GetNote(mailboxId, noteId) NoteView
    SaveNote(mailboxId, noteId?, html?, text?, folderId?, expectedModifiedAt?) NoteView
    DeleteNote(mailboxId, noteId) bool

No new libraries.
