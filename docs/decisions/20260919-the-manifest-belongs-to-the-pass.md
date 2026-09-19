# The manifest belongs to the pass, and a pass sees one tree

- Status: accepted
- Date: 2026-09-19
- Deciders: Ziyan Zhou
- Beside: `20260918-the-history-is-part-of-the-manifest.md` and
  `20260919-a-share-of-the-history-on-every-page.md`, which settle *what*
  a pass offers. Nothing of that changes here. This says when the tree it
  offers is worked out.

## Context

Every page of a scan built the whole manifest and then used 256 entries
of it. The manifest is a walk over the tree, and for every checkout met
on the way `git ls-files`, `git status --porcelain` twice, `rev-parse`,
`describe`, `remote -v`, and a `git log` over the entire history for the
authors its profile carries. The caller sliced out its page and threw the
rest away, and the next page built it again.

On the owner's machine the tree holds 1,122 checkouts — one directory in
it, `controller`, holds 113,677 tracked files on its own, and there are
twenty-five more beside it. That is on the order of nine thousand git
processes for every page of 256 entries. Measured in-process over a
smaller part of the same tree, 333 checkouts, forty pages took over five
hundred seconds: about twelve seconds a page, nearly all of it git,
before one file had been read. A complete pass over the whole tree is
hundreds of pages, so the night was spent on git and not on reading.

The history had the same shape of problem. Since a share of every page
belongs to the commits, every page ran a bounded `git log` per checkout
that has a share, and took the dozen its share of the page had room for.

## Decision

The manifest is built once for a pass and lent to every page of it: the
paths, the profile of every checkout, which checkouts are kept to their
profile, and the commits the pass will offer.

A pass is named by the tree, by what the server said about it, and by
`KnownID`, which is the name the server already makes for a pass and
changes whenever one starts at the top of a tree. The first page of a
pass — the page with no cursor behind it — builds the manifest, whatever
is held under that name, so a later pass is never served an older one's
tree. A page with a cursor and nothing held builds one and goes on from
its cursor, because a pass forced to start over would never finish a tree
of this size; that is what a pass resumed after a restart, after half an
hour of idleness, or after somebody unpaused the source does. At most
four trees are held, the oldest goes when a fifth arrives, and one
nothing has asked for in half an hour is not served.

A pass therefore sees one tree: the tree as it was when the pass began.

## Consequences

Measured over a synthetic tree of 200 checkouts, a pass of seventeen
pages takes 2.3 seconds where it took 32.8: the per-page cost falls from
1.93 seconds to 0.13, and what is left is reading files. The saving grows
with the number of pages, because the part removed was paid once a page
and is now paid once a pass.

A pass seeing one tree is the part worth arguing about, and it is an
improvement on what was there. Before, every page saw the latest tree,
and a file written halfway through a pass was offered or skipped
depending on where the cursor happened to be when it appeared — offered
twice if it was renamed backwards across the cursor, never seen if
forwards. Now a pass offers one set, which is what the sweep that follows
a completed pass is entitled to assume, since a document the pass did not
name is taken as gone.

It costs a pass of lag at the edges. A file created while a pass is
running waits for the next pass. A file deleted while a pass is running
is still offered, comes back refused as unreadable, and so keeps its
document one pass longer than it used to. A checkout that appears
mid-pass gets its profile next time; a profile says "clean" if the
checkout was clean when the pass began. Passes over a tree with more to
read are twenty seconds apart, so a pass late is minutes, not days.

A pass resumed onto a rebuilt manifest is the one case that sees two
trees — the old one behind its cursor and the new one in front of it,
which is what every page did before this. It is correct in the way that
matters, which is that the pass still reaches its last page and still
names everything, and it is the exception rather than the rule.

What has to stay true: the server must keep making a new `KnownID` when a
pass starts at the top of a tree, and the manifest must stay a function
of the tree and of what the request says about it, so that everything
that narrows it — the globs, the person's addresses, the bar for a
checkout being theirs, the pace of the history — is part of the pass's
name. A setting changed mid-pass then takes effect on the next page, the
way it did when every page built its own.
