---
name: gmail-gog
description: A Gmail mailbox's threads, read with the gog command line tool, each thread's messages in full.
requires: [gog]
# Google counts calls a minute for each person; a steady pace stays under it.
pace: 250ms

settings:
  - name: account
    description: the Google account, as gog knows it; empty for gog's default
    type: string
    pattern: "^([^-][^ ]*@[^ ]+)?$"
    default: ""
  - name: query
    description: which threads, in Gmail's own search words
    type: string
    pattern: "^[^-].*$"
    default: "newer_than:2y -category:promotions -category:social"
  - name: attachments
    description: fetch the files the messages came with, so their text is read and pictures can be looked at
    type: boolean
    default: true

containers:
  # One file for the mailbox. A thread can carry several labels, so labels
  # are not containers: the same thread would be filed once per label.
  - fixed: [{}]
    name: threads.jsonl

records:
  # Dates in UTC: without it gog prints local time with no zone.
  - command: [gog, --account, "{{settings.account}}", --json, gmail, search, "{{settings.query}}", --max, "500", --timezone, UTC]
    parse: {json: {items: threads}}
    paging: {token: {field: nextPageToken, flag: --page}}
    # The query is usually a window of time, and a thread that ages out of
    # it has not left the mailbox: keep what is no longer listed.
    unseen: keep
    record:
      id: "{{item.id}}"
      kind: mail
      title: "{{item.subject}}"
      url: "https://mail.google.com/mail/#all/{{item.id}}"
      at: "{{item.date | time}}"
      author: "{{item.from}}"
      version: "{{item.messageCount}}/{{item.date}}"
      private: true
    detail:
      # The thread as text: each message's headers and body, decoded.
      command: [gog, --account, "{{settings.account}}", gmail, thread, get, "{{item.id}}", --full]
      parse: text
      text: "{{detail.text}}"
    # The thread's files, named by gog, which finds them however deep in
    # a message they are. Listed once for each version of the thread;
    # Gmail gives a file a new identifier each time it is asked, so a
    # file is known by its message and its name, and a reply to the
    # thread does not fetch its files again.
    attachments:
      when: "{{settings.attachments}}"
      list:
        command: [gog, --account, "{{settings.account}}", --json, gmail, thread, attachments, "{{item.id}}"]
        parse: {json: {items: attachments}}
      key: "{{each.messageId}}/{{each.filename}}"
      version: "{{each.size}}"
      name: "{{each.filename}}"
      command: [gog, --account, "{{settings.account}}", gmail, attachment, "{{each.messageId}}", "{{each.attachmentId}}", --out, "{{output}}"]
---

# Gmail

Reads the threads of a Gmail mailbox that match a search, through `gog`, which is signed in on the person's computer. A thread is a record, its text every message in it with its headers, read again only when a message is added to it.

This is for a Gmail account the person does not receive through TeaNode itself; mail that arrives at TeaNode is already read where it lands.

The files messages came with are fetched too, unless `attachments` is off: a PDF, an office document or a text file is read on the computer, a scan with OCR where it has tesseract, and a picture waits for the night, which decides whether it is worth looking at. Signature logos and other small inline pictures are among them; the night passes those over.

## Document identifiers

The mailbox is the file `threads.jsonl` and a thread is its Gmail thread identifier within it.

## Checked against the tool

`gog gmail search --json` (a `threads` list with `id`, `date`, `from`, `subject`, `labels`, `messageCount`, and `nextPageToken`), `--max`, `--page`, `gmail thread get --full` printing each message's headers and body as text, `gmail thread attachments --json` (an `attachments` list with `messageId`, `attachmentId`, `filename`, `mimeType` and `size`) and `gmail attachment <message> <attachment> --out` were read from `gog` 0.11.
