# Decision records

A decision record explains **why** the code is the way it is. Anything a future
reader would look at and ask "why on earth is it like this?" belongs here.

## Rules

- One decision per file, named `<YYYYMMDD>-<kebab-slug>.md`, dated the day the
  decision was made.
- A record is **immutable once merged**. If the decision changes, write a new
  record that supersedes it and add a line to the old one pointing at the new.
  Never edit a decision to make history look tidier; the wrong turns are the
  useful part.
- Record the decision, the reasoning, and what it costs. A record with no
  consequences section is usually a record that has not been thought through.
- Keep them short. If it needs more than a page, the design belongs in a
  planning doc under a decision under `docs/decisions/` first.

## Format

    # Title, as a statement of what was decided

    - Status: accepted | superseded by <file> | reversed
    - Date: YYYY-MM-DD
    - Deciders: who agreed

    ## Context

    What was true before, and what problem forced a choice.

    ## Decision

    What was chosen, in the present tense.

    ## Consequences

    What this costs, what it rules out, and what has to be true for it to keep
    working.

## Index

The records below cover the restructure from a hosted service into a
self-hostable open-source server. The full narrative is in
`the decision that introduced it`.

`20260902-mail-is-composed-in-the-dashboard.md` covers what came after:
sending from the dashboard, and templates in more than one language. Its
narrative is `the decision that introduced it`.

The five records dated 2026-09-10 belong to the personal agent —
`20260910-agents-belong-to-people.md` (an agent per person, opt-in, granted
sources one at a time), `20260910-embeddings-without-pgvector.md`,
`20260910-stdio-servers-are-the-operators.md`,
`20260910-the-attached-tab-is-the-persons.md`, and
`20260910-dav-signs-in-with-app-passwords.md`, which the calendar and
contacts plans will act on. Their narrative is
`the decision that introduced it`; the roadmap that follows
it is `the decision that introduced it`.

`20260911-the-computer-is-a-device-the-person-runs.md` adds the person's
own computer beside the attached tab: reached through a program they run,
signed in as them, only while they talk. Its narrative is
`the decision that introduced it`.

`20260918-a-checkout-that-is-barely-yours-is-somebody-elses.md` says
what a source reads of a folder of checkouts: the ones a share of whose
history is the person's own, in full, and the rest as a profile and
nothing more. It also says why that is decided from how much of a
checkout's history is theirs rather than from a list of names, why one
commit is not enough of it, and why the device applies the rule while the
server supplies it.

`20260918-the-history-is-part-of-the-manifest.md` says how the commits of
those checkouts reach the graph: in the same sequence of pages as the
files, from every checkout that is the person's own work, and bounded per
pass at a pace the source sets, because a commit is the only document
that says who wrote something and there are a third of a million of them.
`20260919-a-share-of-the-history-on-every-page.md` amends it with when
within a pass that happens: a fixed share of every page, taken before the
files, because a pass over a real tree is hundreds of pages and never
reached the end where the history used to wait.

`20260919-the-manifest-belongs-to-the-pass.md` says when the tree a pass
offers is worked out: once, on the pass's first page, rather than on
every page of it. It also says what that buys — a page of a real tree
cost twelve seconds of git before a file was read — and what it costs,
which is that a pass sees the tree as it was when the pass began.
