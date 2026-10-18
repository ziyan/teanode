# Contributing to TeaNode

TeaNode is a mail server. Mail is unforgiving: a mistake does not throw an
exception, it silently loses somebody's message or gets a domain's reputation
burned. That shapes most of what follows.

Start with `docs/reference/local-development.md` to get a build and a database.

## Before you send a change

    make format        # gofmt
    make lint          # golangci-lint, then the naming checks if you have them
    make test          # starts a PostgreSQL container automatically
    make build

Merge only on green checks, and only after looking at what they made: for a
change to a screen, the CI screenshots or a running server. A branch that
another open pull request is based on keeps its branch when it merges, or
GitHub closes the one stacked on it.

CI runs `make lint-ci`, which is golangci-lint only. A local check enforces this
project's naming conventions and is local-only, so a contributor without it is
never blocked; run it anyway if you have it.

## Building a feature

A feature is done when all of these hold, not when the code compiles.

- **Plan a large one first.** A feature that adds tables, a subsystem or a
  new surface starts as a plan under `docs/planning/` (`<name>-execplan.md`,
  shaped like its neighbors): the tables it adds, field by field, the
  operations and tools, the screens, what is left out, and the decisions
  somebody had to make. Settle the names there, because renaming after the
  code exists touches every surface. The plan is then built whole, in one
  pull request, rather than as a trail of halves.
- **Coordinate with what is in flight.** Before starting, read the open pull
  requests and the plans under `docs/planning/` for work on the same tables,
  files or screens. Build on it or settle an order instead of colliding.
  Rebase onto `main` before opening, and renumber a migration if `main` took
  its number meanwhile.
- **Every surface, the same behavior.** A capability ships on the dashboard,
  the agent's tools, the command line and the iPhone app together, through
  the same GraphQL operation, with the same names and the same refusals. The
  parity tests check the command line and tool names against the operation's;
  the iPhone app follows `ios/DESIGN.md`.
- **Follow the closest existing feature.** Copy the shape of its tables,
  operations, job, tool entries and pages, and reuse the words it already
  uses for the same idea. A new word for an old idea, or a convention no
  neighbor uses, needs a reason in the pull request.
- **Use it for real.** Run it against a server with a model configured and
  do what a person would: send the message, attach the file, ask the agent
  to do the thing its new tool exists for, on the phone as well as the
  desktop. Most of what a review would miss shows up in the first real use,
  and `docs/evaluation/end-to-end-tasks.md` is where a task worth repeating
  is written down.
- **Look at it.** Walk every screen the change touches on a running server, at
  desktop and phone widths, in light and dark, from real screenshots: spacing,
  alignment, rows that cannot be told apart, text that overflows, a way back.
  Fix what you find before asking for review.
- **Review it, then fix what the review finds.** Read the whole diff as a
  reviewer would, or have one read it, and fix every finding before calling it
  done; a finding left open says why in the pull request. Then review again:
  fixes bring their own findings, and a change is ready when a round finds
  nothing.
- **Replace, do not keep both.** When something is renamed, moved or
  removed, the old one goes in the same change: no redirect from an old
  dashboard path, no alias for an old word, no compatibility branch for a
  shape nobody outside this repository depends on. A migration still carries
  forward data a person would miss.

## Naming

These are not negotiable, and `make lint` checks most of them.

- **Acronyms follow the first letter.** If the identifier starts with a
  capital, acronyms are fully capitalized: `ReferenceURI`, `SessionID`,
  `GetFTPID`, `DKIMResult`. If it starts lowercase, only the first letter of
  the acronym is capitalized: `referenceUri`, `sessionId`, `getFtpId`. Register
  a new acronym in the lint configuration with a comment saying what it stands
  for.
- **Do not abbreviate.** `command`, not `cmd`. `response`, not `resp`.
  `request`, not `req`. Go package names are the exception and should be short.
- **No single-letter variables.** `err` is the one blessed short name, and Go
  errors should be called `err` wherever possible.
- **Struct receivers are `self`.** Consistently, across the whole codebase.
  `.golangci.yml` disables the linters that would object.
- **Name the same thing the same way everywhere.** If it is a `delivery` in the
  database it is not a `send` in the API, the command line, the dashboard or
  a translation. One idea has one name: when two features turn out to be the
  same thing, one name is kept and the other is gone from the code, the words
  on screen and the docs. A new word that collides with one already in use
  for something else (a mailbox rule and an agent's standing instruction,
  say) is renamed before it ships.
- **Table names are singular**: `agent_follow_up`, not `agent_follow_ups`.
- **Words a person reads are plain.** No coined terms, no internal names, and
  the background memory runs are dreams in code, docs and on screen alike.

## The dashboard

The web dashboard has its own conventions — which shared component to reach
for, where a panel's action goes, when a row action is an icon and when it is
a word — in `docs/coding/frontend-design.md`. Read it before adding a page or
a panel. A page that builds by hand what a component already does is the
change most likely to be sent back.

## The iPhone app

`ios/DESIGN.md` is the app's rulebook: system components first, the green
accent, every string in the String Catalog in all three languages.

- **The server goes first.** An app build that reads a new field breaks
  against a server that does not have it yet, and TestFlight hands it out
  within the hour, so the server side is deployed before the app is built.
- **Every screen has a demo.** CI runs the app against `DemoServer` and takes
  a screenshot of each screen listed in `.github/workflows/ios.yml`, in light
  and dark. A new screen adds its demo data and its `shoot` lines, and
  somebody opens the screenshots before the pull request merges: CI passing
  says the screen was drawn, not that it looks right.
- **A symbol is the color of its words.** On a button: green for an action,
  red for one that destroys, grey while it cannot be pressed. On a row that
  opens a page or shows something: the text color, as the side menu's parts
  are. In a form or list row a symbol takes the app's green whatever color
  its words are, so every row symbol is given its color: a button or menu
  with `.actionColor()` (`.actionColor(isDestructive: true)` after the
  `.tint(.red)` of one that destroys), anything else with
  `.foregroundStyle(.primary)`. A destructive button anywhere carries
  `.tint(.red)` straight after it, for swipe actions, menus and toolbars.
  `make check-button-colors` fails on a symbol without its color, and
  `node ios/scripts/check-button-colors.mjs --fix` adds it. Never color the
  words and the symbol separately.
- **A drag that moves the screen fires nothing under it.** The side menu's
  drag slides the screen with the finger, so the touch never leaves the row,
  photo or person it began on, and a tap that is allowed to run beside the
  drag fires when the finger lifts: the menu opened and so did the photo. A
  gesture that moves what is under the finger (the menu's drag, a drag to
  close a viewer) runs beside no other gesture except a scroll view's pan
  that scrolls the other way, and it does not start on something that
  scrolls in its own direction and can still scroll back. A new gesture
  says in its delegate which gestures may run beside it, and a new screen
  is tried with a swipe right that starts on a row, on a picture and on a
  sideways strip.

## Comments

Explain **why**, not what. The what is in the code underneath.

A comment earns its place when it records something the reader cannot recover
from the code: a protocol requirement, a failure that was observed in
production, a deliberate choice among several reasonable ones. `// increment
the counter` above `counter++` is noise. `// Reject before the DATA command so
a spammer pays for the connection` is worth having.

Exported identifiers get a doc comment starting with their name.

## The agent

The agent acts for a person, so most of its rules are about doing what that
person could do, no more and no less, without making them do it twice.

- **Its tools can do what the person can.** Anything the dashboard or the
  command line lets a person do, under their permissions, the agent's tools
  can do too, through the same operation and with the same refusals.
- **Few tools, many actions.** A new capability is an action on the tool
  that owns its subject, with the arguments that action needs, before it is
  a tool of its own. Every tool the model is shown costs every turn, and a
  model choosing among forty near-twins chooses worse than among ten.
- **Long results page.** A tool that can return a lot returns a part of it,
  says there is more, and takes the offset to read on. What was stored is
  never cut to fit: a document is kept whole and read in parts.
- **Limits are code.** A cap on searches, spending or retries is enforced
  where the call is made. A prompt can say why a limit exists; it cannot be
  the limit.
- **No ceremony for what was already allowed.** A person who attached their
  own computer or browser has authorized the agent to use it as them. A
  confirmation card is for spending money, sending as the person and
  deleting what cannot come back, not for reading a directory.
- **Proactive by default, easy to stop.** Something the agent does on its
  own (following a bill, a weekly look around town) is on without setup, and
  each kind of it can be stopped from the place it shows up, with the tools,
  the dashboard, the command line and the iPhone app agreeing on what is
  stopped.
- **The person's language is for the person.** The dashboard's language sets
  the language of conversations with the person, not of the runs nobody
  reads.
- **Prompts stay short, and are tried.** A prompt says what the model cannot
  work out for itself, once, in the file under `internal/agent/prompts/`
  that owns it. A change to one is run against a real model before it
  ships, through the tasks in `docs/evaluation/`, and the pull request says
  what was asked and what came back.
- **A restart can come at any moment.** A deploy restarts the server in the
  middle of a turn, a dream or a job. A job is retried, a turn the restart
  cut short is carried on when the server is back, and new background work
  says what it does when it is interrupted.

## Storage

Rows describe and blobs hold: what is searched, listed or joined lives in
PostgreSQL, and the bytes of a message, an attachment, a photo or a
recording live in the blob store with a row pointing at them. Something a
person deletes is soft deleted first when they might want it back.

## Invariants

Break any of these and something downstream fails in a way that is hard to
trace back.

- **Every migration ships a matching `.reverse.sql`.** The migration runner in
  `internal/db/database_migrate.go` reverts unknown migrations using the
  reverse SQL it recorded when applying them, and panics when it is missing.
  See `docs/coding/database-migrations.md`.
- **Configuration identifiers are stable.** A domain, alias or credential `id`
  in the configuration is generated once and never changed, because stored mail
  and deliveries reference it. Editing a pattern must not regenerate an id.
- **The stored configuration is the source of truth.** It lives in the
  database, as one document, and `internal/config` defines its shape. Do not
  add a settings table of your own, and do not read an environment variable at
  the point of use: the environment says where the database is and seeds a
  first run, and everything else an operator sets belongs in `internal/config`.
- **No cloud dependency in a default code path.** S3, Route53 and GeoIP are
  optional and off. Code guarded by `if settings.Enabled` must not construct a
  client, open a file or dial anything when disabled.
- **The ACME `http-01` handler comes first.** A certificate authority fetches
  `/.well-known/acme-challenge/` over plain HTTP with no credentials. It must
  not meet authentication, a redirect to HTTPS, or a catch-all route.
- **The server secret is generated once.** It signs bounce return paths and
  every SMTP credential's password. Rotating it invalidates all of them.
- **Never commit a secret or anything real.** `make check-secrets` scans
  tracked files and gitleaks scans every branch's history, so a slip is not
  undone by a later commit. Test fixtures use `example.com` and
  `example.net`. Invent every example in a comment, a test, a plan or a pull
  request: no real person, company, host, account number, document id or
  home-directory path, and no account of the incident that motivated a
  change beyond what the engineering reason needs. Deleting an example is
  usually better than renaming it to something that reads as real.

## Tests

Write the test that would have caught the bug. A test that only proves the code
runs is not worth the maintenance.

Tests that need PostgreSQL take it from `internal/db/dbtest`, which skips when
`TEANODE_TEST_DATABASE_HOST` is unset, so `go test` still works without Docker.

**A unit test must not reach the network.** This was learned the hard way: an
early version of `autoacme.Open` started its renewal loop during construction,
so merely building a manager in a test contacted Let's Encrypt production.
Construction and starting are now separate for exactly this reason.

## Commits

Write the message for somebody trying to understand this change a year from
now with no memory of the conversation that produced it.

- Subject in the Conventional Commits form, `<type>(<scope>): <what now
  happens>`, under about 80 characters, saying what is different for whoever
  uses it: "fix(api): attachments answer byte ranges, so a video plays in the
  iPhone chat". The types are `feat`, `fix`, `docs`, `test`, `ci`, `refactor`
  and `chore`; the scope is the part of TeaNode (`agent`, `web`, `ios`,
  `api`, `recording`).
- A pull request is squashed into one commit on `main`, so its title is that
  subject and its description is that body.
- Body explains why the change was needed and what it costs. Describing what
  the diff does is redundant; the diff is right there.
- Note behavior that changed for an operator, and anything that has to be done
  by hand when upgrading.

## Changelog

User-visible changes get a changelog entry, and there are two places to put
one. The Changelog block of the pull request description is where most of them
belong: it is reviewed alongside the change it describes, and the release bot
collects it. Writing straight into `CHANGELOG.md` under Unreleased is the other
way, and is what somebody committing to main does. Internal refactoring that an
operator cannot observe needs neither; say so in the description and label the
pull request `no changelog`.

That entry is also what releases the change. Every push to `main` that leaves
something under Unreleased is cut into a release: the version comes from the
entries — `Added` or `Removed` means a new minor, anything else a new patch —
and the tag it pushes is what builds the binaries and the container image. A
push with nothing under Unreleased releases nothing, which is most pushes.

To see what would happen before pushing:

    .github/scripts/cut-release.sh          # says the version, writes nothing
    .github/scripts/changelog.sh 0.2.0      # the notes that version would carry

On a laptop that reads only `CHANGELOG.md`; it says so, and says why. Reading
the pull requests takes `GITHUB_REPOSITORY` and a token, which the workflow
passes it.

A major version is never inferred. Deciding that a change breaks whoever is
running this is a judgment, so it is the "Major release" workflow, run by
hand, with the word MAJOR typed into it.

## Decisions

If your change makes a choice a future reader would question, write a decision
record in `docs/decisions/` — see the README there. Larger work gets a design
document in a decision under `docs/decisions/` first.

## Reporting a security problem

Do not open a public issue for anything exploitable in mail handling,
authentication or certificate issuance. `SECURITY.md` says where it goes:
GitHub's private vulnerability reporting, on the Security tab.
