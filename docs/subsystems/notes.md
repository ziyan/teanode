# Notes

A phone's Notes app can keep its notes in a mail account instead of its own
cloud. It does that over IMAP: each note is a message in a folder of the
account, usually called "Notes". This server stores those messages like any
other, and reads them back as notes: on the dashboard's notes folder, with
`teanode note`, and through the agent's `note` tool.

## What a note is

The phone writes a note as one `text/html` part with three headers of its
own: `X-Uniform-Type-Identifier: com.apple.mail-note` says it is a note,
`X-Universally-Unique-Identifier` is the note's identity, and
`X-Mail-Created-Date` is when it was first written. The Subject is its first
line. The Message-ID is not the note's identity.

An edit on the phone does not change the message. It appends a new one with
the same identifier and sets `\Deleted` on the old one, and may never
expunge it. So a note is the newest item not flagged deleted among the items
of a mailbox with that identifier. Newest by when the item was filed here,
not by the date on the message, which the writing client chooses. A copy in
the trash or junk is not a note: it is not listed, read or changed.

Stored, a note is a `mail` row of kind `note` with `note_identifier` set.
IMAP APPEND recognizes one by its header (`internal/notes`). Messages stored
before that column existed are read once at server start: `note_identifier`
is null for a message never looked at, empty for one looked at that is not a
note, and the identifier for a note, so the pass reads each old message once.

## Which folder

The folder is found by what it holds, not by what it is called, since the
name may differ by language: the folder holding the most notes, else a
folder named "Notes", else one made named "Notes" when the first note is
written here, never the trash or junk. The dashboard's folder list carries
`noteCount` beside `total` (IMAP does not count it), and the dashboard shows
the notes view for a custom folder whose items are all notes, or an empty
one named "Notes". A folder where notes sit among mail stays mail, so none of
the mail is hidden behind the notes view.

## Writing one

`SaveNote` writes a new message with the note's identifier and creation date
and removes the previous versions in the same folder in the same
transaction, flagged and expunged as EXPUNGE does, so a phone's next sync
sees one message for the note. A copy of the note in another folder is left
alone. Text is written as the phone writes it: a `div` a line, a blank line
as `<div><br></div>`, and lines starting "- " as a list. HTML from the
dashboard is cleaned before it is stored.

A note whose message has more than one part (a picture or a file the phone
kept with it) or whose HTML shows a picture is read here but not changed:
`isEditable` is false, `SaveNote` refuses it, and the dashboard shows it
read-only. A new version is written from HTML alone and would lose them.

A note's `modifiedAt` is when its current version was filed here. A save
may pass it back as `expectedModifiedAt`; when the note has changed since
(compared to the second), the save is refused as a conflict rather than
written over the other change. The dashboard and the agent's `edit` and
`append` pass it, and the dashboard shows the other change when refused;
`teanode note edit` does not, and the last writer wins.

The HTML a read returns has no style blocks, classes, ids or anything that
runs, since the dashboard puts it into its own page; the dashboard also
passes it through `quotableHtml` before the editor takes it.

## Not mail

A note is left out of what treats messages as mail: the agent's sorting and
its embeddings, and the dashboard's mailbox-wide lists and search. The
search index reads only plain text parts, so `SaveNote` adds the note's text
itself; a note appended by the phone is indexed by its subject.

The dashboard also leaves out items flagged `\Deleted` from its lists and
counts, which is how one note stopped showing as two. IMAP still reports
them until they are expunged, as the protocol requires.

## Three doors

`ListNotes`, `GetNote`, `SaveNote` and `DeleteNote` are the whole of it. The
dashboard's notes folder, `teanode note list|show|add|edit|remove` and the
agent's `note` tool (list, read, write, edit, append, remove) all call them.
The tool reaches only mailboxes granted to the agent, and asks before
removing a note.
