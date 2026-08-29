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
  planning doc under `docs/planning/active/` and the record just points at it.

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
`docs/planning/active/20260818-open-source-restructure.md`.

`20260902-mail-is-composed-in-the-dashboard.md` covers what came after:
sending from the dashboard, and templates in more than one language. Its
narrative is `docs/planning/done/20260902-compose-and-templates-in-the-dashboard.md`.
