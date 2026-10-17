# Faces are found on the server, and named from contacts

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

"Photos of Grandma" needs faces: where they are in each picture, and which
faces belong to the same person. Sending every picture to a hosted service
would put the most private part of a library somewhere else. The server is
one static Go binary with no C dependencies, and its readers of media
(ffmpeg, ffprobe, pdftotext, pdftoppm) are separate programs in the image
that it runs and reads the output of.

The address book is already the only list of people
(`20260913-the-address-book-is-the-only-list-of-people.md`).

## Decision

A small C++ program, `teanode-faces`, built against OpenCV with its YuNet
face detector and SFace face recognizer, reads one picture as raw pixels
and prints the faces it found and a vector for each. Reading raw pixels keeps
OpenCV's image decoders, and the libraries they bring, out of the image. It
sits in the image beside ffmpeg, and the server runs it the way it runs
ffmpeg. The Go code stays pure Go.
When the program is not installed, faces are reported as unavailable and
everything else works.

Faces whose vectors are close are put in one face group. A face group is
nobody until the person names it, and naming it means choosing a contact
from the address book, or making one. The person can merge two groups and
take a face out of one.

Faces are found for every photo of a person who has them switched on, which
is the default; switching them off deletes every face and group the person
has.

## Consequences

The image grows by about 80 MB: 42 MB of OpenCV libraries and 39 MB of
models. The detector and recognizer are MIT and Apache 2.0 licensed
respectively.

Faces in a photo shared with somebody else are the owner's: a reader sees
the names the owner gave, and never has the reader's own faces found in it.
