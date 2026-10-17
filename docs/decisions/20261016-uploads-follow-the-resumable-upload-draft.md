# Uploads follow the resumable upload draft

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

A recording goes up in numbered 8 MiB parts over a protocol of our own
(`PUT .../parts/{n}`). That works for an app that sends every part itself.
An iPhone backing up a photo library cannot: the app is not running most of
the time. Since iOS 26.1 the Photos framework can upload a person's library
in the background for an app, but it sends each file as one request and
only resumes an interrupted one when the server speaks the IETF resumable
upload draft (draft-ietf-httpbis-resumable-upload): it checks with
`OPTIONS` and expects a `104 Upload Resumption Supported` interim response
carrying the upload's `Location`. `URLSession` speaks the same draft on its
own for an app's background uploads.

## Decision

Photo files go up through one endpoint that implements the draft:
`OPTIONS` answers with `Upload-Limit`, `POST` creates an upload and sends
`104` with its `Location`, `HEAD` reports the offset stored so far, `PATCH`
appends and `DELETE` gives up. What a file is (which photo, which of its
files, when it was taken) travels in request headers, because the
background uploader sends nothing but the file as the body. The dashboard
and the command line use the same endpoint with their own small client.

The server stores the body as it arrives in parts of at most 8 MiB, so the
offset it reports is always bytes that are durable, and assembles and
checks them when the upload completes.

A phone's background uploads sign in with a device password that grants
photos (`sync_credential.has_photos_access`), the same kind of password a
phone uses for its calendar, because a background upload may run days after
it was queued, long after a session token has expired.

## Consequences

A proxy in front of the server that drops 1xx responses turns resumable
uploads into plain ones; they still work, and restart from the beginning
when cut.

An upload that has not finished holds bytes that are not photos yet, so the
bytes of an account's unfinished uploads count towards its limit, and
starting an upload again for the same photo and file ends the earlier try, so
that restarts do not pile up. An upload has at most twice as many parts as a
file of the largest size needs, and a type that is not kept is refused before
a byte is read. A client that has sent every byte waits, asking where the
upload is, while the server reads the parts back and stores the file, instead
of sending the last part again.

Recordings keep their own protocol for now.
