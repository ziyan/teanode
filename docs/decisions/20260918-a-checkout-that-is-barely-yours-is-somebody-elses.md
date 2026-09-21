# A checkout that is barely any of your work is somebody else's

- Status: accepted
- Date: 2026-09-18
- Changed: 2026-09-19 — the bar was **any** commit of the person's and is
  now a share of the history. Everything else in this record stands: the
  profile is still kept, the device still applies the rule, the server
  still supplies it, and there is still no list of names anywhere in it.
- Deciders: the-owner

## Context

A `files` source is usually pointed at the directory a person keeps their
work in, and that directory holds other people's work too: everything they
have ever cloned to read, to build, or to fix one line of. Read as a tree of
files it all looks the same, so the agent indexed all of it and spent its
nights filing facts about somebody else's source onto the person's own
pages. A single source can hold tens of thousands of indexed files with most
of them from a handful of checkouts nobody there has ever committed to.

The obvious fix is a list — of directory names, of project names, of the
places dependencies are usually kept. This program had exactly such a list
and took it out (migration 0082): a name cannot carry the judgement, the
list never names every case, and reading as a promise it cannot keep is
worse than not making it. A second one would fail the same way.

There is better evidence already on hand. The scan computes a profile for
every checkout it finds, and the profile carries every address in that
checkout's history and how many commits each one has; the server already
knows which addresses are the person's, from the card they marked as
themselves.

### Why the bar moved

The first version of this rule asked for **one** commit, and that is not
authorship, it is a visit. A work tree was censused after the rule shipped —
every `.git` in it, remembering that a submodule keeps `.git` as a *file*,
so the obvious `find -type d` census undercounts badly. What it showed:

- Of the checkouts the one-commit rule called "theirs", a substantial
  minority held **exactly one** commit of the person's, and between them
  they admitted something close to two fifths of every file read.
- The largest single offender was a fork of a kernel tree: tens of
  thousands of files admitted on the strength of one commit. Behind it,
  forks of large upstream libraries parked in the same build tree, each
  the same shape.
- Most of the files being read sat in a directory no commit of theirs had
  ever touched.

Which is the thing the rule was written for in the first place, back again
through the door the one-commit bar left open: a kernel tree or a vendored
third-party library is not worth a night's reading. A kernel is not low
value because it is a kernel — no list here says that, and none will — it
is low value to a personal graph because essentially none of it is that
person's work, and the history says so plainly if it is asked how much
rather than whether.

## Decision

A checkout is the person's own work when its history holds enough of
their commits to be their work, and that is one number:

- at least **two** commits of theirs, and
- at least **a fiftieth** of the log, whichever of the two is more,
- capped at **twenty-five** commits, past which the length of the log
  stops mattering, and
- never more than the **whole history**, so a checkout every commit of
  which is theirs is always theirs.

Each of the four earns its place. Two because one commit is a visit, and
because so much of what these sources read came in on single commits. A
fiftieth because two or three commits in a kernel is the same visit
twice. The cap because somebody on a large team owns their monorepo at
half a percent of its history, and a share alone would throw away the
work they are paid to do. The history's own length because a project
initialized last week, committed once, and never touched by anybody else
is not somebody else's code.

A source may say `specification.ownCommitsAtLeast` — `teanode agent
knowledge set <source> --own-commits-at-least` — and that number is then
the bar, flat, with no share added to it. Flat because a setting the
program could overrule on any checkout long enough is not a setting; and
because `1` there is precisely the rule this replaced, for somebody who
liked it.

Everything else is as it was. A checkout that does not clear the bar
keeps its **profile** — what it is, what its readme calls it, where it
lives, its newest tag — so the graph still knows the checkout is there
and "what was that thing I cloned" has an answer. Its files are not read,
and nor is its commit history. A source may still say
`specification.readEveryCheckout` and get all of it, for somebody who does
want a dependency's source read.

Not knowing keeps the files: no addresses from the server, a checkout git
cannot read, a checkout with no commits at all, all read as before.
Silence must never come out as "none of this is yours". Every address of
theirs counts towards the same total, because a person committing from a
laptop and a work machine under two addresses did all of that work.

The rule is applied on the device, and its inputs are sent by the server
with every request. The server decides the policy because who somebody is
lives on their card and changes there — a copy kept on a laptop would go
stale with nobody able to see that it had. The device applies it because a
file that is not going to be filed should not be read, hashed and carried
across a socket first; a file left out of the manifest is never in the
hashes the server holds, so deciding at the far end would mean shipping the
text of every one of those files on every pass, for ever.

What that costs the person to see is on the source, wherever the source is
shown: how many checkouts were kept to their profile and how many files that
was.

## Consequences

**What the new bar costs**, worked out by censusing a work tree again and
applying both rules to it. The new bar drops something like a sixth of the
checkouts the one-commit rule admitted, and with them roughly **a third of
every file the source was reading**. The kernel fork goes: one commit
against a bar of twenty-five, and all of its files with it. So do the forks
of large upstream libraries parked in the same build tree, on a handful of
commits each out of tens of thousands.

Of the checkouts holding exactly one commit of the person's, all but one
kind go: the kind where that commit is the entire history, which is a
project of their own started and not yet worked on. Those are a small
number of checkouts holding very few files, which is exactly why the
"never more than the whole history" clause is worth its place.

**An affected source loses those documents on its next full pass**, through
the ordinary sweep, exactly as it did when this rule first shipped. Facts
already learned from those files are not swept with them — `agent_fact` has
no foreign key to `agent_document` — so a page that says something about
somebody else's source goes on saying it until somebody strikes it.

**A drive-by fix of theirs is now invisible to the graph**, files and
commits alike, where before one bought the whole checkout. That is the
trade and it is the right way round: the graph loses a line about a patch
they sent to a project once, and stops holding tens of thousands of files
of a kernel. Where it is the wrong way round for a particular tree — one
where a handful of commits really is the person's work — the source says
so with `--own-commits-at-least`, and the number is on the source where
anybody can see it.

**A person whose commits are all under an address their card does not
list** looks, to this rule, like somebody who has never committed: every
checkout is kept to its profile and nothing of theirs is read. That is the
same silence that already made their work history empty, it is already
reported as the commit addresses the source could not place, and the fix is
the same one minute of marking a card. The two now sit next to each other on
the source's row, which is the point. Raising the bar makes that worse
rather than better — a card that lists one of their two addresses now has
to clear a bar with half their commits — which is another reason the
unplaced addresses are reported.

**An older daemon** does not know the new bar and goes on using the old
one, offering everything with a commit of theirs in it; the server files
what it is offered, and says nothing it is not asked. The person updates
the program on their machine and the next pass is quiet. Nothing on the
server has to change for that to happen, and nothing breaks while it has
not.
