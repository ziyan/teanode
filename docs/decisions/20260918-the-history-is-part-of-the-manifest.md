# The history is part of the manifest, at a pace the source sets

- Status: accepted
- Date: 2026-09-18
- Deciders: Ziyan Zhou
- Amended by: `20260919-a-share-of-the-history-on-every-page.md`, which
  moves the history off the end of a pass and gives it a share of every
  page. What a pass offers, and that every pass offers the same, is as
  written below.

## Context

A commit is the only document that carries an author. The night is shown
each document as a heading of title, author and date, so a commit is the
one thing in the graph that can say *this person worked on this code*;
files say only that the code exists.

There were none. On the deployment this was written for, 553,185
documents held zero of kind `commit`, and had from the beginning. Three
things had to go right for one to be offered and they rarely did. The
commits were read from the scanned root alone and only when that root was
itself a checkout, which a folder of a hundred and thirty-seven checkouts
is not. They were offered only on a page that ended a pass, and only out
of the room that page had left over after its files and every checkout's
profile — for a tree of any size, none. And a commit the server already
held was left out of the page altogether, so the few that did get filed
were swept by the next pass that finished, because a document a completed
pass was not shown is taken as gone.

The scale is why this cannot simply be turned on. That tree holds on the
order of 340,000 commits across some 500 authors. Filed in one night they
would bury the graph and the person's embedding budget.

## Decision

The history is part of what a pass offers, not an afterthought at the end
of one. A pass walks the files of the tree and then its commits in the
same sequence of pages: past the files the cursor names a commit instead
of a path, so a pass interrupted anywhere resumes where it stopped and a
pass does not have to reach the end in one page to offer any.

Commits come from **every** checkout in the tree that is the person's own
work, not from the outermost alone. An authorship map covering one
repository is not a map. A checkout that
`20260918-a-checkout-with-none-of-your-commits-is-somebody-elses.md`
keeps to its profile gives no commits either: its history is somebody
else's work as much as its files are.

What a pass carries is **bounded per pass** and shared out among those
checkouts, shortest history first, each giving its newest commits and
what it cannot use going back into the pot. Two thousand unless the
source says otherwise, in `specification.commitsPerPass`.

Every pass offers the same set, and a commit the server already holds is
named with its text left out, the way an unchanged file is. That is what
keeps the commits: a pass that offered the next slice of history instead
would file a slice and have the following pass sweep it.

## Consequences

The graph holds the newest commits of the person's checkouts, bounded by
the pace, and not the whole history. A commit falls out of that window
when enough newer ones arrive, and the sweep removes it on the next
completed pass; facts already learned from it stay, as they do for any
swept document. Somebody who wants more history says so on the source,
and pays for it in documents, chunks and embeddings.

An older daemon does not know the argument and reads at its own pace; an
older server does not send it and the daemon uses its own. Neither breaks
while the other has not been updated.

The pace is a number and not a date. "Everything since last year" would
be the other shape, and it was not chosen because a pass has to offer
what the graph already holds or lose it, and a date says nothing about
how much that is.
