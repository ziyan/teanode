---
name: journal
description: A folder of the person's own dated notes - a file a day or a month, or one file with date headings - read by TeaNode's own journal reader.
reader: journal

settings:
  - name: path
    description: the folder of notes, as the person writes it on that computer (~/notes/journal)
    type: string
    pattern: "^[^-].*$"
---

# Journal

Reads a folder of notes the person wrote themselves, dated by their file names or by headings inside them, with the reader built into `teanode computer`. Each day's entry is a document dated that day, which is what lets the agent answer "what was I doing that week".

Only for the person's own notes: an export of anything else (a wiki, a chat, a tracker) has a shape of its own and is read by a type for that service.

## Document identifiers

An entry is its file and its date.
