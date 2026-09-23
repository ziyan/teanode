---
name: mailbox-sent
description: What the person sent from one of their TeaNode mailboxes - how they write, to whom, and about what - read by TeaNode's own sent-mail reader on the server.
reader: sent
runs: [server]

settings:
  - name: mailbox
    description: the mailbox whose sent mail is read, by its identifier
    type: string
    pattern: "^[0-9a-z]{26}$"
---

# Sent mail

Reads the mail the person sent from one of their mailboxes on this TeaNode server. Nothing is fetched from anywhere: the mail is already here, and the reader runs on the server itself.

What somebody sends is the best record there is of how they write and who they write to, which is what lets the agent draft a reply in their voice and know who a name in a request is. Only what they wrote is read; the mail they quoted beneath it is left out.

The first pass reads backwards from the newest, a page at a time, so what they sent lately is known first. Sent mail is never deleted from the agent's memory by a pass: mail moved out of the sent folder was still sent.

## Document identifiers

A message is its identifier in the mailbox.
