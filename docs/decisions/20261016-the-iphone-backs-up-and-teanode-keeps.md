# The iPhone backs up, and TeaNode keeps what it has

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Two-way sync between an iPhone's photo library and the server can mean
mirroring, where deleting on either side deletes on the other, or backing
up, where the server keeps what it received. Photos on a phone are deleted
to make room far more often than because they are unwanted, and the Photos
framework asks the person to confirm every deletion an app makes, so a
deletion on the server could never silently follow to the phone anyway.

Writing the server's photos into the phone's library would also put them in
iCloud, using the person's iCloud storage, and would need care so they are
not uploaded back.

## Decision

An iPhone backs up: every photo and video in its library goes to TeaNode,
and deleting one on the phone leaves TeaNode's copy alone. Deleting on
TeaNode is done on TeaNode, into a trash kept for 30 days.

TeaNode's photos reach the iPhone through the app's own library: the
timeline, albums, people and albums shared with the person, with previews
from the server and originals fetched when opened. A person can save any of
them to the phone's Photos library, one or a selection at a time.

## Consequences

The app needs full photo library access to back up, and asks for it only
when the person turns backup on. A photo saved from TeaNode to the phone is
backed up again like any other photo of the phone. When the file the phone
sends has the same bytes as the original TeaNode already holds, the upload
joins that photo under the phone's name, so the photo is in the timeline once
and counted against the quota once; a file the phone has changed, such as a
re-encoded copy, is a photo of its own.

Backup goes on after the person leaves the app, as far as iOS allows. The app
hands the background session a long queue (up to 200 files and 10 GB, never
more than a quarter of the phone's free space), and iOS sends it with the app
closed. When iOS wakes the app because uploads finished, the app queues more
files that are on the phone already, in the few seconds it has. What waits for
the app to be opened or for the night, while the phone charges, is the rest of
the library, files that are in iCloud (they must be downloaded first), and
files that did not fit in the queue. The backup screen says how many are queued
and how many wait.

Keeping an album on the phone automatically, and mirroring the phone's own
albums, are left out of this design.
