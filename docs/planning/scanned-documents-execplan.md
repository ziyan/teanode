# Reading scanned documents on the person's computer

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A PDF that is a scan has no text layer. `pdftotext` finds nothing in it, the file reaches the server empty, and the night declines it as "not a picture, and only a picture can be read here", so nothing it says is ever searchable. Office files had two smaller gaps: macro formats (`.xlsm`, `.xlsb`, `.pptm`, `.docm`) were not converted at all, and LibreOffice was given thirty seconds whatever the size, which a large workbook or deck does not finish in.

After this change a computer with `pdftoppm` and `tesseract` reads the pages of such a file as pictures, locally, and files the words; a file the server already holds without text is sent again and filed with it. To see it working: install `tesseract-ocr` on a computer that runs a source holding scans, and after its next pass a search for words printed on one of them finds it.

## Progress

- [x] (2026-09-24) Measured one source's unread PDFs: 197, of which 187 have no fonts at all (scans), 6 have fonts but no extractable text, and 4 have text that simply never got read.
- [x] (2026-09-24) OCR in `internal/computer/scan_ocr.go`, used by `textOf` for a PDF with no text layer and by `extractOffice` for a document that prints with none; results cached by content hash and languages.
- [x] (2026-09-24) Macro office formats, and LibreOffice's time limit scaled with the file's size.
- [x] (2026-09-24) Server: a PDF or office attachment with no passages is left out of the hashes a pass is told it holds, and `fileAttachment` files an attachment again when it arrives with text where the stored one had none.
- [x] (2026-09-24) The night's reason for such a file says what would read it.
- [ ] Deploy the server and the daemons, and check that the scans in one source become searchable.

## Surprises & Discoveries

- Observation: attachment text was dropped on the computer's side for anything the server named as held, so a server-side "file it again if it now has text" alone could never fire. The hash is the bytes' hash and the server checks fetched bytes against it, so it cannot carry the text's state either; leaving unread files off the held list is what lets the text through.
- Observation: tesseract reads about one to three seconds a page on the development computer; the unread PDFs of one source are about 1,400 pages, so the first pass costs an hour of that computer's time and later passes read the cache.

## Decision Log

- Decision: OCR on the person's computer with tesseract, not a vision model at night.
  Rationale: a person's scanned papers are often private ones, which then never leave their machine; it costs nothing; it covers Japanese with the `jpn` data. A vision model stays the path for pictures, and can be added for drawings where OCR says little.
  Date/Author: 2026-09-24, the person and agent.

- Decision: at most 30 pages of one file are read, at 200 dpi in grey.
  Rationale: the opening of a long scan is what a search needs to find it, and the bytes are kept for the rest.
  Date/Author: 2026-09-24, agent.

- Decision: unread PDFs and office files are left out of the held list for good, not only once.
  Rationale: a file stays unread until the computer gains a way to read it, which can happen at any time (a program installed, a language added); sending a few hundred small entries without text each pass costs little, and the OCR cache means a file that has been tried is not tried at length again.
  Date/Author: 2026-09-24, agent.

## Context and Orientation

`textOf` in `internal/computer/scan_extract.go` is what every reader on the computer uses to turn a file into text; `attachmentText` in `scan_records.go` calls it for a file a record came with. `ListAgentDocumentHashes` in `internal/db/database_knowledge.go` is what a pass is told the server holds; the computer sends an entry it names without its text. `fileAttachment` in `internal/agent/ingest_attachment.go` files an attachment and keeps its bytes. `unopenable` in `internal/agent/dream_attachment.go` is the night's reason for declining a file it cannot show a model.

## Plan of Work

Done as listed in Progress. CI installs `poppler-utils` and `tesseract-ocr` so the tests of reading a PDF and a scan run there rather than skip.

## Concrete Steps

    go test ./internal/computer/ -run 'AScanIsRead|Office'
    go test ./internal/db/ -run 'UnreadFile'
    go test ./internal/agent/ -run 'Attachment|Unopenable'

## Validation and Acceptance

`TestAScanIsReadFromItsPages` reads an invented scanned receipt (`internal/computer/testdata/scanned-receipt.pdf`, no text layer) and finds its words, then reads the cache on the next call. `TestAnUnreadFileIsNotNamedAsHeld` and the attachment filing test cover the server. On a real source, the count of declined PDFs falls after a pass on a computer with tesseract.

## Idempotence and Recovery

The OCR cache is under `~/.cache/teanode/ocr` and can be deleted; files are then read again. Nothing is deleted on the server; a file filed again replaces its own row.

## Interfaces and Dependencies

Optional on the computer: `pdftoppm` (poppler) and `tesseract` with any languages. New: `HasAgentChunks` in `internal/db`.
