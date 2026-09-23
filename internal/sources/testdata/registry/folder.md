---
name: folder
description: A folder on the person's computer - notes, documents, and git checkouts with their history - read by TeaNode's own files reader.
reader: files

settings:
  - name: path
    description: the folder, as the person writes it on that computer (~/projects)
    type: string
    pattern: "^[^-].*$"
  - name: include
    description: only files matching these globs, when any are given
    type: array
    items: {type: string}
    default: []
  - name: exclude
    description: files matching these globs are left unread
    type: array
    items: {type: string}
    default: []
  - name: readEveryCheckout
    description: read the files of every checkout, including the ones barely any of whose history is the person's own work
    type: boolean
    default: false
  - name: ownCommitsAtLeast
    description: how many of the person's own commits a checkout needs before its files are read; 0 lets TeaNode work it out from the length of the history
    type: integer
    minimum: 0
    default: 0
  - name: commitsPerPass
    description: how many commits of history one pass reads, over all the checkouts in the folder; 0 is TeaNode's own pace
    type: integer
    minimum: 0
    default: 0
---

# Folder

Reads a folder on one of the person's computers with the reader built into `teanode computer`, not with a command: files of every kind something can read (text, code, PDF, office documents), and every git checkout in it, with the checkout's history and a profile of what it is.

Inside a checkout only the files git tracks, or that git would pick up, are read; `.git`, `node_modules`, `vendor` and `__pycache__` never are.

A checkout too little of whose history is the person's own is kept to its profile (what it is, where it lives, who worked on it) and its files are left unread, so a folder of clones is not taken for the person's own work. How little is too little is `ownCommitsAtLeast`, or, left at 0, two commits for a short history and more for a long one, up to twenty-five; `readEveryCheckout` reads them all. The addresses a commit counts as the person's are the ones on their contact card.

History is read newest first, at `commitsPerPass` commits a pass, so a folder with a deep history fills the agent's memory over several nights rather than in one.

## Document identifiers

A file is its path under the folder; a commit, its hash within its checkout. Moving the folder, or pointing the source at its parent, names everything again.
