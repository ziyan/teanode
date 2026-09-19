# A share of every page is the history's, not the leftovers of the last one

- Status: accepted
- Date: 2026-09-19
- Deciders: Ziyan Zhou
- Amends: `20260918-the-history-is-part-of-the-manifest.md`, which settled
  that the history is part of what a pass offers. This says when within
  the pass.

## Context

That record put the history in the same sequence of pages as the files
and after all of them: past the files the cursor names a commit instead
of a path. On a tree of any size that is too late to be reached.

The scan was run over the owner's tree and watched page. Forty pages,
256 entries each, 9,522 files read, and the cursor was still inside one
checkout's source directory. Zero commits and zero repository profiles
in those forty pages, because both are offered only on the page that
ends a pass. The tree holds 519 checkouts — 232,309 files in the ones
kept to their profile alone — and a complete pass over it is hundreds of
pages, each costing minutes: the manifest is rebuilt every page and
`git ls-files` runs once per checkout, 333 times a page.

So the one document that carries an author sat behind every file in the
tree, and the graph held no commit all night. Not because the commit
code was wrong — it was never reached.

## Decision

A fixed share of every page belongs to the history: one entry in eight,
taken before the page's files rather than out of what they leave. They
leave nothing — a page of source fills its byte budget every time, which
is how the commits were lost the first time and would have been lost the
second.

Before the files rather than after them, and a share rather than the
whole page. A page of commits alone would be a night spent not reading
the tree, which is what the source is for; an eighth spends a budget of
two thousand inside the first sixty or so pages instead of trickling it
over the nine hundred a large tree takes, and a night that stops early
stops with an authorship map in it.

The cursor says both places, because a page now stops in both: the file
it stopped at, a unit separator, and where the history got to. The two
shapes the build before this one wrote keep their meaning — a path alone
is the files part way with the history not begun, `commit:<hash>` is the
files done with the history part way — so a pass begun by that build and
taken up by this one goes on from where it stopped and still offers the
whole of what a pass offers. A third shape says the history of this pass
is finished while the files are not, so the hundreds of pages left do
not ask git for it again.

Nothing else of the older record changes. The budget is still per pass
and shared out shortest history first, a commit the server holds is
still named with its text left out, and a checkout kept to its profile
still gives no commits.

## Consequences

A pass carries about an eighth fewer files a page while its history
lasts, which on a tree whose history is bounded at two thousand is the
first sixty pages of several hundred.

Every page now asks git for the history, where before only the last page
of a pass did: one bounded `git log` per checkout that has a share.
Beside what the manifest already costs — a full `git log` per checkout
on every page, for the authors in its profile — that is a few per cent,
and it stops altogether once the cursor says the history is done.

A daemon *downgraded* in the middle of a pass is the case this does not
cover. The older build reads the new cursor as a path, matches no file,
offers nothing, and ends the pass — and a finished pass sweeps what it
was not shown. Upgrading is covered because that is the direction that
happens; a downgrade mid-pass costs a source its documents until the
next pass files them again.
