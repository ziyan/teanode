# A file can be sent to the person's main chat

- Status: accepted
- Date: 2026-11-01
- Deciders: the-owner

## Context

`share_file` handed the person a file in the conversation the run was in. A
program over MCP is in no conversation, so a file it handed over was kept
where nobody looked, and a coding session that drew a chart or recorded the
screen left the person to go and find it on the machine. A reminder, a goal or
a job had the same trouble: its run's transcript is not where the person
reads. A program also had no way to send bytes at all: the MCP body is capped
at 1 MiB, its token works only at the tools endpoint, and a file passed as an
argument would be read by its model as text.

## Decision

`share_file` takes `to: "main_chat"`, which posts the file into the person's
main chat, the conversation the dashboard and the phone open on, as a file the
agent handed over: a line marked `[file sent]` saying who sent it, the call,
the file as its answer, and the caption. A program names its file with
`source: "upload"` and sends the bytes with one PUT to the address answered,
signed for that file and good for an hour. The main chat is asked for, never
the default, because over MCP `share_file` is also how a program fetches a
file for itself. The command line does the same with `teanode agent
send-file`.

Any run may send there without asking, a run nobody is present for included:
the file goes only to the person, and the line says where it came from. A file
waits while a turn runs in the main chat and is posted when it ends. A file
may be up to 100 MB (`agent.limits.maxSentFileBytes`). A page sent this way is
shown in place in the artifact sandbox, and a PDF in the browser's viewer;
handed over in a conversation, both still download.

## Consequences

A coding session can put what it made in front of the person, on their phone
with a notification, and a linked chat app is sent the file too. The same
shape as a handed over file meant no new drawing on any surface.

A run steered by something it read (a mail, a page) can now put a file in the
person's main chat unasked. It reaches nobody else, it says who sent it, and a
page in it runs in the sandbox with no network and no session, so the worst
it can do is show the person something misleading under a line that names
its source.

A file waiting for its upload holds a row with no bytes; one never uploaded is
swept with the uploads nobody sent, after a day.
