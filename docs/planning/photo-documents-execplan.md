# New photos are read as they arrive, and a receipt or a statement in one is imported

This ExecPlan is a living document. The sections Progress, Surprises &
Discoveries, Decision Log and Outcomes & Retrospective are kept up to date as
the work goes on.

## Purpose / Big Picture

A person photographs a receipt at the till, or screenshots a card's list of
charges, and then has to remember to send the picture to their assistant.
Until now a photo in the library was described only when somebody looked at it
(docs/decisions/20261016-a-photo-is-described-when-somebody-looks-at-it.md),
and a receipt or a statement in it was recorded only when the person handed the
picture over by hand.

After this change every new picture in a person's library, taken in the last
thirty days, is described as soon as its previews are made: the same
description and words read in it that a search finds it by. The description
also says whether the picture is a receipt, a statement of an account, or a tax
document. A receipt is then read line by line and recorded, matched to its
charge and to the shopping lists, exactly as an uploaded receipt is; a
statement's rows are imported into the account they are of; a paystub or a W-2
is read by the tax document job. A receipt recorded already (mailed, or
uploaded by hand) is not recorded a second time.

To see it: back up a photo of a receipt from the iPhone. Within a few minutes
`teanode photo read <id>` shows a description and "document: receipt", and
`teanode finance receipts` lists the receipt. `teanode photo reading` says
whether the person's new photos are read, and `teanode photo reading off`
stops it.

## Progress

- [x] (2026-10-31) Plan written.
- [x] (2026-10-31) Migration 0265: the person's switch, the photo's document
  kind and when it was queued, the attachment's photo.
- [x] (2026-10-31) Server: the sweep that queues new photos, the description's
  document kind, the receipt, statement and tax document paths, the duplicate
  check; agent tests with a fake model.
- [x] (2026-10-31) GraphQL, command line, the photo tool.
- [x] (2026-10-31) Dashboard: the switch among the preferences beside face
  recognition, the document kind in a photo's details.
- [ ] iPhone: the switch in the photo settings, the document kind in the viewer.
- [ ] Docs, review, deploy, TestFlight.

## Surprises & Discoveries

- Observation: a statement's rows of an institution the person never imported
  would make a new account of imported statements, from any picture of a
  bank's screen.
  Evidence: `PreviewTransactionRows` plans a new account when none matches.
  The job imports only when the plan names an existing account.

## Decision Log

- Decision: new photos are described as they arrive; older ones still only
  when somebody looks at them.
  Rationale: the person asked for it. "New" is a picture of the person's own
  library (not a video, not a photo shared with them) whose previews were made
  and that was taken, or added when it has no date, within the last thirty
  days, so that importing an old archive of thousands of pictures pays for
  none of them. A new decision record supersedes the old one for new photos.
  Date/Author: 2026-10-31, the person and agent.
- Decision: the person has a switch, on by default, beside face recognition.
  Rationale: on by default, easy to stop; a description is a model call per
  picture, and some people will not want it.
  Date/Author: 2026-10-31, agent.
- Decision: the description job answers the document kind too (receipt,
  statement, taxDocument or none), kept on the photo.
  Rationale: the model is looking at the picture already; a second call to
  classify it would pay twice.
  Date/Author: 2026-10-31, agent.
- Decision: a document photo is handed to the existing jobs as an agent
  attachment made from its large preview, which remembers the photo; one
  attachment per photo.
  Rationale: the receipt and tax document jobs already read attachments, with
  their checks, matching and cleanup; a receipt's source stays one of the
  three it is. The attachment is a copy of the 2048-pixel preview, which reads
  as well as the original and is a JPEG any model takes.
  Date/Author: 2026-10-31, agent.
- Decision: a receipt from a photo is not recorded when one with the same
  store, day and total is recorded already.
  Rationale: the same purchase often arrives twice: a mailed receipt and a
  photo of the paper one, or a photo the person also sent to the chat.
  Date/Author: 2026-10-31, agent.
- Decision: a statement photo is imported only where the server is sure which
  account it is; otherwise nothing is imported and the run says why.
  Rationale: the rules for pictures of transactions already refuse an account
  they cannot tell; nobody is there to ask, so a refusal stays a refusal. A
  new account is not started either: the person starts one by sending the
  picture to their agent.
  Date/Author: 2026-10-31, agent.
- Decision: the words read in any photo have taxpayer numbers masked.
  Rationale: a photo of a W-2 read in the background would otherwise keep a
  social security number in the searchable text.
  Date/Author: 2026-10-31, agent.

## Outcomes & Retrospective

(To be written at the end.)

## Context and Orientation

- `internal/agent/photo_job.go`: the `describe_photo` job. It reads the large
  preview, renders `prompts/describe_photo.txt` and stores the description with
  `SetPhotoDescription`.
- `internal/agent/receipt_job.go`: the `read_receipt` job reads a mail or an
  agent attachment and records through `RecordReceipt`
  (`internal/agent/finance_receipt.go`).
- `internal/agent/finance_tax.go`: `QueueTaxDocumentReading` marks an
  attachment a tax document and queues `tax_document`.
- `internal/agent/finance_statement.go`: `ImportTransactionRows` imports rows
  read off pictures, checked by `finance.CheckTransactionRows`.
- `internal/agent/agent.go`: `tickAt` runs the sweeps that queue work.
- `internal/db/database_photo_face.go`: the face recognition switch on "user",
  the pattern for the new one.

## Plan of Work

Migration 0265 adds:

- `"user".is_photo_reading_enabled` (default true)
- `photo.photo_document_kind` ('' until described, then none, receipt,
  statement or taxDocument)
- `photo.reading_queued_at`
- `agent_attachment.photo_id`, unique per agent where set

A sweep in `tickAt` queues, for each active agent whose person has the switch
on, the new photos not yet described or queued, a few at a time, marking each
queued.

The description prompt asks for `documentKind`. After it is stored, a document
photo of a person with the switch on is copied into an attachment (once) and
queued: a receipt to `read_receipt`, a tax document to `tax_document`, a
statement to the new `read_statement_photo` job. That job reads the account and
its rows with `prompts/read_statement_photo.txt` and imports them through
`ImportTransactionRows`.

`runReadReceipt` drops a receipt read from a photo when a receipt with the same
store key, day and total is recorded.

GraphQL gains `SetPhotoReading(isEnabled)` and `isPhotoReadingEnabled` on
`ReadPhotoLibrary`, and `photoDocumentKind` on a photo. The command line gains
`teanode photo reading [on|off]`. The photo tool gains the `reading` operation.
The dashboard and the iPhone show the switch beside face recognition and the
document kind in a photo's details.

## Validation and Acceptance

- Agent tests with a fake model:
  - a new receipt photo is described, its kind kept, and a receipt recorded
  - a second photo of it records nothing
  - an old photo is not queued
  - the switch off queues nothing
  - a statement photo imports its rows
  - a tax document photo is queued to the tax job
- DB tests for the sweep's query and the attachment's uniqueness.
- On the server: the receipts the person photographed this week are found
  already recorded, not twice.

## Idempotence and Recovery

The migration only adds columns with defaults. Its reverse drops them. A photo
is queued once (`reading_queued_at`); its document is handed on once (the
attachment's unique photo).
