---
name: google-drive-gog
description: A Google Drive's folders and files, read with the gog command line tool; Google documents exported, other files fetched to be read.
requires: [gog]

settings:
  - name: account
    description: the Google account, as gog knows it; empty for gog's default
    type: string
    pattern: "^([^-][^ ]*@[^ ]+)?$"
    default: ""
  - name: folder
    description: the folder to start from, by identifier; empty starts at the top of My Drive
    type: string
    pattern: "^([A-Za-z0-9_-]+)?$"
    default: ""
  - name: sharedDrives
    description: read shared drives as well as My Drive
    type: boolean
    default: false

containers:
  # Every folder under the start, each a container named by its path. The
  # walk lists a folder, follows the folders in it, and pages each listing.
  - walk:
      start: {id: "{{settings.folder | or \"root\"}}", path: "My Drive"}
      branch: "{{item.mimeType}} == application/vnd.google-apps.folder"
      child: {id: "{{item.id}}", path: "{{folder.path}}/{{item.name}}"}
    command: [gog, --account, "{{settings.account}}", --json, drive, ls, --parent, "{{folder.id}}", --max, "1000", "{{settings.sharedDrives | flag \"--all-drives\" \"--no-all-drives\"}}"]
    parse: {json: {items: files}}
    paging: {token: {field: nextPageToken, flag: --page}}
    name: "{{folder.path}}.jsonl"
    fields: {folder: "{{folder.id}}", path: "{{folder.path}}"}

records:
  - command: [gog, --account, "{{settings.account}}", --json, drive, ls, --parent, "{{container.folder}}", --max, "1000"]
    parse: {json: {items: files}}
    paging: {token: {field: nextPageToken, flag: --page}}
    skip: "{{item.mimeType}} == application/vnd.google-apps.folder"
    record:
      id: "{{item.id}}"
      kind: file
      title: "{{item.name}}"
      url: "{{item.webViewLink}}"
      modifiedAt: "{{item.modifiedTime}}"
      version: "{{item.modifiedTime}}"
      private: true
      text: "{{item.name}}, in {{container.path}}"
    attachments:
      # A Google document has no file of its own; it is exported to one.
      - when: "{{item.mimeType}} == application/vnd.google-apps.document"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, txt, --out, "{{output}}"]
        name: "{{item.name}}.txt"
      - when: "{{item.mimeType}} == application/vnd.google-apps.spreadsheet"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, xlsx, --out, "{{output}}"]
        name: "{{item.name}}.xlsx"
      - when: "{{item.mimeType}} in [application/vnd.google-apps.presentation, application/vnd.google-apps.drawing]"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, pdf, --out, "{{output}}"]
        name: "{{item.name}}.pdf"
      # Other files only of the kinds something can read.
      - when: "{{item.mimeType}} matches ^(application/pdf|text/.*|image/.*|application/vnd.openxmlformats-officedocument.*|application/msword|application/vnd.ms-.*)$"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --out, "{{output}}"]
        name: "{{item.name}}"
    maxBytes: 26214400
---

# Google Drive

Reads a Google Drive through `gog`, which is signed in on the person's computer. Every folder under the starting folder is a container, named by its path; every file in it is a record whose text says what and where it is. A Google document, sheet or slide deck is exported (to text, a spreadsheet, or a PDF) and a file of a kind TeaNode can read is fetched, up to 25 MB, both only when the file changed; TeaNode reads what came back as it reads any attachment.

## Document identifiers

A folder is the file `My Drive/<folders>.jsonl` and a file is its Drive identifier within it: the names the records script this replaces used, so a source switched to this type keeps what it has read. A file moved to another folder is a new document in that folder and leaves the old one.

## Checked against the tool

`gog drive ls --json` (a `files` list with `id`, `name`, `mimeType`, `modifiedTime`, `webViewLink`, `parents`, and `nextPageToken`), `--parent`, `--page`, `--all-drives` and `download --format --out` were read from `gog` 0.11. Not yet checked: the export format names `download --format` accepts, and whether `--parent root` lists My Drive's top.
