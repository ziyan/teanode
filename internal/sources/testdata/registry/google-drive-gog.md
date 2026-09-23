---
name: google-drive-gog
description: A Google Drive's folders and files, read with the gog command line tool; Google documents exported, other files fetched to be read.
requires: [gog]
# Google counts calls a minute for each person; a steady pace stays under it.
pace: 250ms

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
  # Two folders of one name in one folder are told apart by the start of
  # their identifiers.
  - walk:
      start: {id: "{{settings.folder | or 'root'}}", path: "My Drive"}
      branch: "{{item.mimeType}} == application/vnd.google-apps.folder"
      child: {id: "{{item.id}}"}
      step: "{{item.name | path-step}}"
      distinct: "{{item.id | first 8}}"
    command: [gog, --account, "{{settings.account}}", --json, drive, ls, --parent, "{{folder.id}}", --query, "trashed = false", --max, "1000", "{{settings.sharedDrives | flag '--all-drives' '--no-all-drives'}}"]
    parse: {json: {items: files}}
    paging: {token: {field: nextPageToken, flag: --page}}
    name: "{{folder.path}}.jsonl"
    fields: {folder: "{{folder.id}}", path: "{{folder.path}}"}

records:
  - command: [gog, --account, "{{settings.account}}", --json, drive, ls, --parent, "{{container.folder}}", --query, "trashed = false", --max, "1000", "{{settings.sharedDrives | flag '--all-drives' '--no-all-drives'}}"]
    parse: {json: {items: files}}
    paging: {token: {field: nextPageToken, flag: --page}}
    skip: "{{item.mimeType}} in [application/vnd.google-apps.folder, application/vnd.google-apps.shortcut]"
    # A Google document's words are its text, fetched once for each time
    # it changed.
    detail:
      when: "{{item.mimeType}} == application/vnd.google-apps.document"
      command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, txt, --out, "{{output}}"]
      text: "{{detail.text | newlines | trim}}"
    record:
      id: "{{item.id}}"
      kind: "{{'page' | if detail.text | or 'file'}}"
      title: "{{item.name | or item.id}}"
      url: "{{item.webViewLink}}"
      at: "{{item.modifiedTime}}"
      modifiedAt: "{{item.modifiedTime}}"
      version: "{{item.modifiedTime}}"
      private: true
      # What is free to know: what the file is called, what it is, how
      # large, where it sits and when it changed. It makes a Drive
      # searchable whether or not a byte of it was fetched.
      text: "{{item.name | or item.id}} — {{item.mimeType | file-kind item.name}}{{', ' | if item.size}}{{item.size | size}}, in Google Drive in {{container.path}}{{', changed ' | if item.modifiedTime}}{{item.modifiedTime | first 10}}.\n\n{{detail.text}}"
    metadata:
      folder: "{{container.path}}"
      mimeType: "{{item.mimeType}}"
      bytes: "{{item.size}}"
      drive: google
    attachments:
      # A sheet, a deck or a drawing has no file of its own; it is
      # exported to one.
      - when: "{{item.mimeType}} == application/vnd.google-apps.spreadsheet"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, xlsx, --out, "{{output}}"]
        name: "{{item.name}}.xlsx"
      - when: "{{item.mimeType}} == application/vnd.google-apps.presentation"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, pdf, --out, "{{output}}"]
        name: "{{item.name}}.pdf"
      - when: "{{item.mimeType}} == application/vnd.google-apps.drawing"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --format, png, --out, "{{output}}"]
        name: "{{item.name}}.png"
      # Other files only of the kinds something can read, and not when
      # they are larger than anything reads.
      - when: "{{item.mimeType}} matches ^(image/.*|text/.*|application/(pdf|json|rtf|msword|vnd\\.ms-excel|vnd\\.ms-powerpoint|vnd\\.openxmlformats-officedocument\\..*|vnd\\.oasis\\.opendocument\\..*))$ && {{item.size | or 0}} <= 26214400"
        command: [gog, --account, "{{settings.account}}", drive, download, "{{item.id}}", --out, "{{output}}"]
        name: "{{item.name}}"
    maxBytes: 26214400
---

# Google Drive

Reads a Google Drive through `gog`, which is signed in on the person's computer. Every folder under the starting folder is a container, named by its path; every file in it is a record whose text says what it is, how large, where it sits and when it changed, which makes a Drive searchable whether or not a byte of it is fetched. A Google document's words become its text; a sheet, a slide deck or a drawing is exported (to a workbook, a PDF, a picture) and a file of a kind TeaNode can read is fetched, up to 25 MB, each only when the file changed; TeaNode reads what came back as it reads any attachment.

## Document identifiers

A folder is the file `My Drive/<folders>.jsonl` and a file is its Drive identifier within it. A file moved to another folder is a new document in that folder and leaves the old one.

## Checked against the tool

`gog drive ls --json` (a `files` list with `id`, `name`, `mimeType`, `size`, `modifiedTime`, `webViewLink`, and `nextPageToken`), `--parent`, `--query`, `--page`, `--all-drives` and `download --format --out` were read from `gog` 0.11, which names an exported file with its own extension.
