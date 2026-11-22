# Storage usage

What the database and the blob store hold, how it grows, and when a disk fills.
For the operator (`server:manage`): the Disk usage tab of the Server page, the
Disk usage row of the iPhone and Android apps' Manage section,
`teanode server storage`, and the `agent_usage` tool with `whose` set to
`storage`. All of them read the one `StorageUsage` query.

## What is measured

**The database** is `pg_database_size`. It is broken down by area from
`pg_total_relation_size` of every table, which counts the table's indexes and
out-of-line storage with it. The table-to-area map is `areaTables` in
`internal/storageusage/areas.go`. The areas are mail, memory, conversations,
photos, recordings, finance, location, health, journal, search (the vector
tables and mail embeddings, which are indexes and are far larger than the rows
they point at) and other. Other is listed on purpose: a test reads the tables
the migrations create and fails for one that is in no area, and for an entry
that no longer exists, so a new table has to be looked at. What is in the
database outside any table is counted under other, so the areas add up to the
database.

**The files** are what the rows say the blob store holds, as sums over size
columns that have one row per file:

| Kind | Source |
| --- | --- |
| photo originals | `photo_blob.blob_size_bytes`, each blob once however many photos name it |
| photo previews | `photo_preview.preview_size_bytes` |
| recording originals | `recording_media.original_size_bytes` of complete uploads |
| recording audio | `recording_blob.blob_size_bytes`: the derived chunks and speaker samples, which the recording quota counts on top of the original, so the two do not overlap |
| mail messages | `mail.size` |
| agent attachments | `agent_attachment.size` |

These are the rows' figures, not a look at the store. A store that moves old
files elsewhere can hold less on its own disk than they say.

**By person** is the files only, from each table's owner column (a photo's and
a recording's `user_id`, a mailbox's, an agent's). A message filed in two
folders of one mailbox is counted once for that owner. Files that no account
owns, and the difference when two accounts hold one photo that is stored once,
are one row with no person. The database is not measured per person: that
would count the rows of the largest tables one by one, which is seconds on a
large server, so there is no estimate either.

## Disks

The server asks `statfs` of its data directory, and of the blob store's
directory when that is on another file system. The label says which:
`dataDirectory`, `fileDirectory`. Files in an object store with no directory
here are `objectStore`, reported as not measured, never guessed. PostgreSQL's
own disk is not asked about: where it shares the data directory's disk, that
measure covers it, and where it is on another machine nothing is said of it.

## History and the projection

`storage_usage_sample` holds one measure per day (UTC): the database, each area,
each kind of file and each measured disk with its total and free bytes. The
agent's tick (`internal/agent/storage_usage.go`) takes today's sample when none
exists, whether or not the agent is on, so the page is not empty after an
upgrade. Two servers sharing a database take it under one advisory lock. Samples
older than 400 days are deleted.

The projection is a least-squares line through the daily totals (database plus
files, the files left out when they are in an object store) of the last thirty
days, or fewer when there is less history, and none below three samples. It
gives growth per day and the size in 30, 90 and 365 days from the newest total.
A measured disk fills on the day its free bytes are used up at that rate; there
is no date when the totals are not growing. All measured disks are projected
against the same growth, which is right where the database, the files and
the data directory share one disk.

## The warning

A disk projected to fill within 30 days is told to every person who holds
`server:manage` and has an agent, in their main conversation, the same way the
daily check of model prices tells them what it changed. It is told again when it
comes within 7 days. `storage_disk_warning` remembers the last one sent for each
disk, so it is not repeated daily, and is cleared when the disk is no longer
projected to fill within 30 days. The Disk usage tab shows a banner for the same
disks, and the apps a row at the top.
