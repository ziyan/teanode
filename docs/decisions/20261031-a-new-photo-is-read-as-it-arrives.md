# A new photo is read as it arrives, and a document in it is recorded

- Status: accepted
- Date: 2026-10-31
- Deciders: the-owner
- Supersedes, for new photos: 20261016-a-photo-is-described-when-somebody-looks-at-it.md

## Context

People photograph receipts at the till and take screenshots of a card's
charges, and then have to remember to hand each picture to their assistant.
Until now a photo was described only when somebody looked at it, so a
receipt in the library was neither found by its words nor recorded.

## Decision

A new picture in the person's own library is described as soon as its
previews are made. New means taken (or added, when it carries no date) in the
last thirty days, so that importing an archive pays for none of it. Photos
saved from somebody else's share are not read. Older photos are still
described only when somebody looks at them.

Videos were left out at first, which left a library of short clips with
nothing a search could find them by. A new video is read too, from up to six
frames spread across it, in the same one call; it is never taken for a
document.

The description also says whether the picture is a receipt, a statement of an
account, or a tax document, and such a picture is handed to the job that
records that kind of document: the receipt job, a job that imports a
statement's rows into the account of imported statements they are of, or the
tax document job. A receipt already recorded from another source is not
recorded again. Rows the server cannot place in an account imported before are
not imported, since nobody is there to say which account a picture shows.

The person has a switch, on by default, and turning it off keeps what was
read.

## Consequences

Each new picture costs one call to the scan model, and a document one more.
The words read in any photo have taxpayer numbers masked, since a tax form
read in the background would otherwise keep one in the searchable text.
