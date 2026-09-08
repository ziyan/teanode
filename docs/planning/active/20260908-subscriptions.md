# Subscriptions in the mailbox

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log` and `Outcomes & Retrospective` must
be kept up to date as work proceeds. This repository has no `PLANS.md`; the
conventions this document follows are the ones in `~/.claude/PLAN.md`, and the
repository's own writing rules are in `CONTRIBUTING.md`.

## Purpose / Big Picture

Most of what arrives in a mailbox is not a letter. It is a newsletter, a
receipt digest, a "your weekly summary" — mail somebody once agreed to and
now scrolls past. Today TeaNode's mailbox treats each of those as an ordinary
message: they sit in the Inbox between the messages that matter, and the only
way to stop one is to open it, hunt for the word "unsubscribe" at the bottom,
and hope the link works.

After this change a person can:

- Open **Subscriptions** in the mailbox rail and see one row per mailing list
  they receive — who sends it, how many messages it has sent them, when the
  last one arrived, and how many are unread.
- Press one button to unsubscribe. Where the sender supports the one-click
  standard the server does it and reports what the sender answered; where the
  sender only offers an email address the server sends that email from the
  mailbox's own address; where the sender only offers a web page the dashboard
  hands over the link.
- Read one subscription's mail together, newest first, the way a conversation
  is read — so "what has this newsletter sent me" is one page rather than a
  search.
- Unsubscribe from a message they are reading, with one icon button in the
  message's toolbar, without going to the Subscriptions page at all.

You can see it working end to end: send the dev server a message carrying a
`List-Unsubscribe` header, open `http://127.0.0.1:10000/mailbox/subscriptions`,
and the sender appears as a row with a working unsubscribe button.

## Progress

- [x] (2026-09-08 17:10Z) Research: read the mail model, the mailbox item and
      thread queries, the migration rules, the SSRF-guarded fetcher, and the
      mailbox send path. Findings are in `Context and Orientation`.
- [x] (2026-09-08 18:05Z) Milestone 1 — recognise subscription mail as it is
      stored. `internal/util/mailparse/list.go` with ten table cases in
      `list_test.go`; migration `0024_mail_subscription`; five columns on
      `mail`; extraction in `CreateMails`. Verified on the dev server: a
      message with `List-Id` records `weekly.news.example.com`, one with only
      `List-Unsubscribe` records `offers@shop.example.com`.
- [x] (2026-09-08 18:35Z) Milestone 2 — fill in the mail already in the
      mailbox. `ListMailNeedingList` and `SetMailList` in
      `internal/db/database_mail.go`, the batch loop in
      `internal/cmd/server/listbackfill.go`, started every two minutes beside
      the session scavenger. On the dev database one pass examined the 12
      messages stored before this and left nothing unchecked.
- [x] (2026-09-08 19:05Z) Milestone 3 — list the subscriptions over the API.
      `models.MailboxSubscription`, `ListSubscriptions`/`CountSubscriptions`/
      `GetSubscription`/`RecordUnsubscribe` in `internal/db`, migration
      `0025_mailbox_subscription`, and `ListMailboxSubscriptions` /
      `GetMailboxSubscription` on the graph. Answered over the dev server with
      three lists, their counts and their unsubscribe addresses.
- [x] (2026-09-08 20:10Z) Milestone 4 — the Subscriptions page, and reading
      one like a conversation. `web/src/pages/mailboxSubscriptions.tsx`, the
      route, the rail entry, and `ReadMailboxSubscription` returning the same
      view a conversation does so `ThreadMessage` renders both.
- [x] (2026-09-08 20:25Z) Milestone 5 — unsubscribing, all three ways.
      `internal/util/safefetch` extracted from the image proxy first, then
      `UnsubscribeMailboxSubscription`. Proved end to end on the dev server:
      the POST left the machine and the row read "Asking to leave failed: the
      sender answered 404 Not Found".
- [x] (2026-09-08 20:35Z) Milestone 6 — the unsubscribe button on a
      message. In the conversation's toolbar, only when the message named a
      list, behind a confirmation that says what will be sent.
- [x] (2026-09-08 21:30Z) Milestone 7 — changelog written; full Go suite and
      `make lint-ci` pass; deployed to the development server.
- [x] (2026-09-08 21:15Z) Milestone 8 — the sender's logo, from BIMI.
      `internal/bimi` parses and looks up the record; migration
      `0026_bimi_logo` caches what was fetched; a background pass fetches one
      batch every ten minutes through `safefetch`; `/api/v1/logo/{domain}`
      serves it sandboxed; `SenderLogo` shows it, or a monogram. Verified
      against the real records of four well-known senders, all of which
      publish one where example.com does not, and end to end on the dev server
      with a seeded row.

## Surprises & Discoveries

- Observation: message headers are not in the database. `mail.Headers` is
  marked "Raw data, not saved in database" in `internal/models/mail.go`, and
  `GetMailContent` reads them back from object storage with
  `self.storage.Get(ctx, mail.ID)` (`internal/api/v1api/apigraph/content.go`).
  Evidence: `internal/db/database_mail.go` has no header column in
  `mailModel`; the columns it does keep are the ones somebody decided to
  extract at write time — `thread_id`, `from_name`, `attachment_count`.
  Consequence: a subscription list cannot be a query over headers. What the
  list needs has to be extracted when the message is stored, exactly as
  `ThreadID` and `FromName` already are, and existing mail needs a backfill
  that reads storage.

- Observation: there is already an SSRF-safe HTTP client in this repository,
  written for the remote image proxy, in `internal/api/v1api/apimail/remote.go`
  (`remoteClient`, `allowRemoteAddress`, `parseRemoteTarget`). It refuses
  non-public addresses on the address actually dialled rather than on the
  hostname, re-checks every redirect, and caps redirects at five. The one-click
  unsubscribe request needs precisely these guards and should reuse that code
  rather than grow a second copy.

- Observation: a BIMI lookup has to ask the organizational domain too, or it
  finds almost nothing on real mail. Deployed without that fallback, eight
  sending domains from a real mailbox produced no logo at all; with it, the
  ones that publish a mark produce it — a cosmetics retailer's mail comes from
  a `beauty.` subdomain and a home improvement retailer's from an `e.`
  subdomain, and neither publishes a record there because the one above covers
  them. This is the same fallback `internal/util/dmarc/discover.go` makes for a
  policy, and its comment says the same thing about subdomains.
  Evidence: the same eight domains, before and after — six unchanged, two
  turning from "found=false" into a logo and a certificate.

- Observation: BIMI is published by fewer domains than one would guess, and by
  the ones that matter. Looking up four well-known senders — a news site, a
  marketplace, a payment company and a social network — found a record with a
  logo and a certificate on every one; example.com has none. So the feature
  will look empty on a personal mailbox and populated on one that receives
  commercial mail, which is exactly the mail this page is about.

- Observation: the dev server refuses a test newsletter sent from the reserved
  documentation domains. `example.com` publishes a null MX and a DMARC policy
  of reject, and a name under `.test` does not resolve at all, so both are
  refused before they reach a mailbox — which is correct behaviour and a
  nuisance for seeding.
  Evidence: a message from `news@example.com` is stored with status `rejected`
  and `authentication_results.errors` of "Sender address has null MX" and
  "DMARC alignment failed". Sending the same message from an address at a
  large mail provider, whose domain has a real MX and a DMARC policy of none,
  is accepted and reaches the Inbox. Seed with one of those.

- Observation: `internal/web/middlewares.go` defines
  `MakeForwarderMiddleware`, which nothing installs. With an empty key it
  would trust `X-Forwarded-For` unconditionally. It is unrelated to this plan
  but is noted here because a reader grepping for forwarded headers will find
  it and should not copy it.

## Decision Log

- Decision: a message is subscription mail when it carries `List-Unsubscribe`
  or `List-ID`, and its subscription is identified by `List-ID` when there is
  one and by the From address otherwise.
  Rationale: `List-ID` (RFC 2919) is the stable identity a sender publishes
  for exactly this purpose and survives a change of sending address. Plenty of
  senders omit it but do send `List-Unsubscribe` (RFC 2369), and for those the
  From address is what a reader would call "the subscription" anyway. Anything
  with neither header is ordinary mail; guessing from `Precedence: bulk` alone
  would sweep in automated mail nobody subscribed to, such as bounce notices.
  Date/Author: 2026-09-08, this plan.

- Decision: store the extracted list identity on the `mail` row rather than in
  a table of its own.
  Rationale: it is a property of the message, like the conversation it belongs
  to, and the queries that need it are the same queries that already read
  `mail` beside `mailbox_item`. A join table would add a write per message and
  a join per listing for nothing.
  Date/Author: 2026-09-08, this plan.

- Decision: the *user's* side — "I asked to leave this list" — goes in a new
  `mailbox_subscription` table keyed by mailbox and list.
  Rationale: it is per person, not per message: two people receiving the same
  newsletter unsubscribe separately, and the record has to survive every
  message of that list being deleted.
  Date/Author: 2026-09-08, this plan.

- Decision: the server performs the one-click POST; the browser never does.
  Rationale: a request from the browser hands the sender the reader's address
  and user agent, which is what the remote image proxy exists to prevent, and
  RFC 8058's one-click POST is specified for the mail system to perform. It
  also means the request is subject to the same SSRF guards as every other
  outbound fetch.
  Date/Author: 2026-09-08, this plan.

- Decision: BIMI logos are shown only for mail that passed DMARC, and no claim
  of verification is made.
  Rationale: the logo is a brand mark, and showing one for mail that did not
  prove it came from that domain is a phishing aid rather than a feature.
  Verifying the certificate in `a=` — a Verified Mark Certificate — needs a
  trust list and certificate parsing that this milestone does not do, so the
  interface must not use the word "verified".
  Date/Author: 2026-09-08, this plan.

- Decision: mail in Trash and in Junk is not counted as a subscription.
  Rationale: what you threw away is not a list you have, and mail a filter
  caught is not one you agreed to — pressing unsubscribe on it tells a sender
  who guessed your address that a person reads it. Both exclusions are in
  `subscriptionQuery` in `internal/db/database_mailbox.go` with that comment.
  Date/Author: 2026-09-08, this plan.

- Decision: how to leave comes from the newest message of the list, not from
  the oldest or from a stored copy.
  Rationale: a list can change its unsubscribe address between issues, and the
  address in a two-year-old newsletter is the one most likely to be dead.
  Date/Author: 2026-09-08, this plan.

- Decision: nothing unsubscribes on its own. Every request is one a person
  pressed a button for.
  Rationale: an unsubscribe request confirms to the sender that the address is
  read by a human, which is a thing to hand over deliberately. It is also
  irreversible from this side.
  Date/Author: 2026-09-08, this plan.

## Outcomes & Retrospective

All eight milestones are done. What a person can do that they could not before:
see every mailing list their mailbox receives on one page, with how much of it
is there and unread; leave one with a button that does what the sender said to
do; read a list's mail together the way a conversation is read; leave a list
from the message they are reading; and see the sender's published logo beside
it where the sender publishes one and the mail proved it came from them.

What went to plan: the grouping query written in the shape of `ListThreads`
worked first time and reads the same way, which was the point of copying it.
Extracting `internal/util/safefetch` before writing the unsubscribe request
meant the SSRF guards were inherited rather than reimplemented, and its tests
moved with it unchanged.

What did not: the plan assumed a test newsletter could be sent from
`example.com`, and the server correctly refuses that — a null MX and a DMARC
policy of reject. Seeding needs a sender at a domain with a real MX and a
lenient policy. And the plan's Milestone 2 named a table column that was not
in Milestone 1's migration; adding `list_checked` to the same migration is
what it should have said in the first place, because "no list" and "not asked
yet" are different answers and one column cannot hold both.

What remains: the sender's logo is shown on the subscriptions page but not yet
beside a message in the conversation reader, which is a small piece of the same
component. Nothing verifies the certificate a BIMI record names, and the
interface is careful not to claim otherwise.

## Context and Orientation

This section assumes no prior knowledge of the repository.

**What this program is.** TeaNode is a mail server with a web dashboard. One
Go binary (`cmd/teanode-server`) receives mail over SMTP, stores it, serves an
API and serves the dashboard; a second (`cmd/teanode`) is a command line
client for the same API. The dashboard is a React application in `web/`,
built by webpack and embedded in the server binary. `AGENTS.md` at the
repository root describes how a message flows through the program; read it
first if anything below is unfamiliar.

**Where a message lives.** Two things are stored for every message. The row in
the `mail` table (`internal/db/database_mail.go`, model
`internal/models/mail.go`) holds what the server extracted: sender, subject,
size, authentication results, the conversation identifier `thread_id`, and so
on. The message itself — its headers and its body, exactly as they arrived —
goes to object storage, which is the local spool directory or MinIO/S3, behind
the interface in `internal/storage`. Anything a list or a filter needs must be
a column on `mail`; anything else means reading the message back.

**The mailbox.** A mailbox (`internal/models/mailbox.go`) belongs to a user
and has folders. A `mailbox_item` row is one message in one folder: it points
at a `mail` row, carries the IMAP flags (seen, flagged, draft), and has a
`uid`. The same message can be in two folders, so it can have two items. The
mailbox API is `internal/api/v1api/apigraph/mailbox.go`; the SQL behind it is
`internal/db/database_mailbox.go`.

**Conversations, which this feature copies.** `ListThreads` in
`internal/db/database_mailbox.go` (around line 994) turns a folder's items
into one row per conversation. It runs two queries: a `DISTINCT ON
(mail.thread_id)` that picks the newest item of each conversation, then a
second that counts the conversation's messages and collects who wrote them.
The comment above it explains why two queries rather than one. A subscription
listing is the same shape with a different grouping column, and this plan
follows it deliberately so that a reader who understands one understands both.

**The GraphQL API is generated from Go.** There is no schema file. The methods
on the `graph` type in `internal/api/v1api/apigraph/` become queries and
mutations by reflection (`internal/util/graphapi`), argument structs become
input types, and the doc comments become the schema's descriptions. To add an
API call you add a method; there is nothing else to register. The command line
client in `internal/client/` writes its queries by hand and must be updated
separately when it needs the new call.

**Migrations.** `internal/db/migrations/` holds numbered pairs of SQL files:
`0024_something.sql` and `0024_something.reverse.sql`. Both are required — the
loader panics at startup if either is missing — because a server that is
downgraded reverts unknown migrations using the reverse SQL it recorded when
it applied them. `docs/coding/database-migrations.md` is the full explanation.
The highest number today is `0023_mail_thread_backfill`.

**Terms this plan uses.**

*List-Unsubscribe* is a header defined by RFC 2369 that a sender puts on bulk
mail to say how to leave the list. Its value is one or more URLs in angle
brackets, separated by commas, for example:

    List-Unsubscribe: <https://news.example.com/u/abc123>, <mailto:leave@example.com?subject=unsubscribe>

*One-click unsubscribe* is RFC 8058. A sender that supports it adds a second
header:

    List-Unsubscribe-Post: List-Unsubscribe=One-Click

and undertakes that an HTTP POST to the `https:` URL from the
`List-Unsubscribe` header, with the body `List-Unsubscribe=One-Click` and
content type `application/x-www-form-urlencoded`, unsubscribes the recipient
with no further interaction. The POST must carry no credentials and no
cookies. Senders who set this header are required to honour a POST without any
other confirmation, which is why it is the path to prefer.

*List-ID* is RFC 2919, an identifier the sender assigns to the list itself:

    List-Id: Example Weekly <weekly.news.example.com>

The part inside the angle brackets is the identity; the text before it is a
description meant for people.

*SSRF* — server-side request forgery — is what happens when a program fetches
a URL that came from a stranger and that URL points back into the network the
program is running in, for example `http://169.254.169.254/` on a cloud host,
which serves credentials. The guard is to check the IP address actually being
dialled, at every redirect, and refuse anything that is not a public address.

*Spool* is the local directory where raw messages are written when object
storage is the filesystem.

*ULID* is the identifier format used throughout this program: a 26-character
sortable string. `security.NewULID()` makes one.

**Files this plan touches, with what they are for:**

    internal/models/mail.go                     the Mail struct
    internal/db/database_mail.go                the mail table and its columns
    internal/db/migrations/                     the SQL migrations
    internal/util/mailparse/                    header parsing helpers
    internal/models/mailbox.go                  mailbox, folder, item, thread
    internal/db/database_mailbox.go             the mailbox queries
    internal/api/v1api/apigraph/mailbox.go      the mailbox API
    internal/api/v1api/apimail/remote.go        the SSRF-guarded fetcher
    internal/api/v1api/apigraph/mailbox_compose.go   sending from a mailbox
    web/src/pages/mailbox.tsx                   the mailbox reader and list
    web/src/components/sidebar.tsx              the rail
    web/src/app.tsx                             the routes
    web/src/i18n/{en,zh,ja}.ts                  the three message catalogues

**Rules that will bite you.** Every user-visible string goes in all three
catalogues or `make lint-ci` fails, and the three must not be identical.
Comments explain *why*, never *what* — `CONTRIBUTING.md` is explicit about
this and the reviewer will be too. Receivers are named `self`, errors `err`,
acronyms are capitalised as a unit when the identifier starts with a capital
(`ListID`, `URL`) and lowercased entirely when it does not (`listId`, `url`).
Run `make lint-ci` from the repository root before every commit; run
`make test` for the Go tests, which needs Docker because it starts PostgreSQL.

## Plan of Work

### Milestone 1 — recognise subscription mail as it is stored

At the end of this milestone a message that arrives carrying `List-Unsubscribe`
has its subscription identity recorded in the database, and a unit test proves
the parsing against real header shapes. Nothing is visible in the dashboard
yet.

Add a small parser to a new file `internal/util/mailparse/list.go`. It exports
one type and one function:

    type ListInfo struct {
        Key         string   // what identifies the subscription
        Name        string   // what to call it, for a person
        Unsubscribe []string // the URLs from List-Unsubscribe, in order
        OneClick    bool     // the sender promised RFC 8058
    }

    func ParseList(headers []string, from string, fromName string) ListInfo

`ParseList` returns the zero value when the message carries neither
`List-Unsubscribe` nor `List-ID`. Otherwise:

- `Key` is the text inside the angle brackets of `List-ID`, lowercased and
  trimmed. When there is no `List-ID`, it is the From address lowercased. The
  From address here is the address alone, without the display name — use the
  existing helper in `internal/util/mailparse` that the mail indexing already
  uses for `FromName` (`displayNameOf` in `internal/db/database_mail.go` shows
  how the From header is decoded).
- `Name` is the description before the angle brackets of `List-ID` when there
  is one, else the display name from the From header, else the From address.
- `Unsubscribe` holds each URL found between angle brackets in
  `List-Unsubscribe`, in the order they appeared, with whitespace and folding
  removed. Values that are not `http:`, `https:` or `mailto:` are dropped.
- `OneClick` is true when a `List-Unsubscribe-Post` header exists whose value,
  with spaces removed and lowercased, contains `list-unsubscribe=one-click`,
  and at least one `https:` URL is present. One-click over plain `http:` is
  not honoured: the request would be readable on the wire and RFC 8058 expects
  HTTPS.

Write `internal/util/mailparse/list_test.go` covering: a header with both an
https and a mailto URL; a folded header split over two lines; `List-ID` with
and without a description; one-click declared with only a mailto URL, which
must yield `OneClick` false; a message with no list headers at all; and a
`List-Unsubscribe` carrying a `javascript:` URL, which must be dropped.

Add the columns. Create
`internal/db/migrations/0024_mail_subscription.sql`:

    ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_key" TEXT NOT NULL DEFAULT '';
    ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_name" TEXT NOT NULL DEFAULT '';
    ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_unsubscribe" TEXT NOT NULL DEFAULT '';
    ALTER TABLE "mail" ADD COLUMN IF NOT EXISTS "list_one_click" BOOLEAN NOT NULL DEFAULT FALSE;
    CREATE INDEX IF NOT EXISTS "mail_list" ON "mail" ("list_key", "received_at" DESC) WHERE "list_key" <> '';

and its reverse, `0024_mail_subscription.reverse.sql`:

    DROP INDEX IF EXISTS "mail_list";
    ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_one_click";
    ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_unsubscribe";
    ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_name";
    ALTER TABLE "mail" DROP COLUMN IF EXISTS "list_key";

The index is partial — `WHERE "list_key" <> ''` — because most mail is not
subscription mail and there is no reason to index the empty string once per
message.

Add the matching fields to `models.Mail` (`ListKey`, `ListName`,
`ListUnsubscribe`, `ListOneClick`, all `omitempty`) and to `mailModel` in
`internal/db/database_mail.go`, then carry them in
`getMailFromMailModel` and `updateMailModelFromMail`.

Fill them in where `ThreadID` and `FromName` are filled in: in `CreateMails`
in `internal/db/database_mail.go`, inside the loop, guarded by
`len(mail.Headers) > 0` in the same way. One place, for the same reason the
comment there gives about `ThreadID`: six callers store messages and a message
stored without this would simply never appear in the list.

Acceptance for this milestone: `make test` passes, and the new
`mailparse` tests fail before the parser exists and pass after. To see it in
the database, start the dev server (`make dev` from the repository root, which
serves the dashboard on `http://127.0.0.1:10000` and the API on `:10081`),
send a message with the header using the development mail script described
under "Validation" below, then:

    docker exec -i teanode-dev-postgres-1 psql -U teanode -d teanode \
      -c "select list_key, list_one_click from mail order by received_at desc limit 1;"

and expect the sender's list identity rather than an empty string.

### Milestone 2 — fill in the mail already in the mailbox

At the end of this milestone the messages that were already in a mailbox
before Milestone 1 have their list columns filled, so the Subscriptions page
is not empty on a server that has been running for months.

The headers are not in the database, so this reads each message back from
object storage. That is one storage read per message, which is why it is a
background job rather than part of a migration: a migration holds a
transaction and a mailbox may hold a hundred thousand messages.

Add `BackfillMailLists(ctx context.Context, limit int) (int, error)` to the
mailbox maintenance code that already runs on a schedule — find it by looking
at what calls retention; `internal/mailbox/` is where a mailbox's background
work lives if it exists, otherwise put it beside the retention job the server
starts in `internal/cmd/server/run.go`. It selects up to `limit` `mail` rows
that are referenced by a `mailbox_item`, have `list_key = ''` and have not
been examined before, reads each message's headers with `storage.Get`, runs
`mailparse.ParseList`, and writes the four columns.

"Have not been examined before" needs care: a message with no list headers
legitimately has `list_key = ''` and must not be re-read on every pass. Add a
fifth column in the same migration — `list_checked` BOOLEAN NOT NULL DEFAULT
FALSE — set to true whenever the parse has run, whatever it found. New mail
sets it in `CreateMails`. The backfill selects `WHERE list_checked = FALSE`.

Run it in batches of 500 with a pause between them, and log a single line when
a pass does work: how many were read and how many turned out to be
subscriptions. Stop when a pass finds nothing.

Acceptance: on the development database, which has mail already, one pass
fills the columns and a second pass does nothing. Prove it with the count
query above before and after, and with the log line.

### Milestone 3 — list the subscriptions over the API

At the end of this milestone `ListMailboxSubscriptions` answers with one row
per subscription and the dashboard could show it, though it does not yet.

Add to `internal/models/mailbox.go`:

    type MailboxSubscription struct {
        Key          string
        Name         string
        From         string     // the address the newest message came from
        Count        int        // messages in this mailbox
        Unread       int
        LastAt       time.Time  // when the newest arrived
        LastItemID   string     // the newest item, to open it
        OneClick     bool       // the newest message promised RFC 8058
        Unsubscribe  []string   // its List-Unsubscribe URLs
        RequestedAt  *time.Time // when this person asked to leave, if they did
        Method       string     // how: oneClick, mail, link
        Failed       bool       // the request was made and did not succeed
        Error        string
    }

Add `ListSubscriptions(mailboxId string, options *SubscriptionOptions)` to
`internal/db/database_mailbox.go`, written in the shape of `ListThreads`: a
`DISTINCT ON ("mail"."list_key")` over the mailbox's items joined to `mail`,
ordered by `received_at DESC`, to get the newest message of each list; then a
grouped count for the totals. Restrict to items whose folder belongs to the
mailbox and which are not in Trash — a list you deleted every message of is
still a list you receive, but a list whose mail you have thrown away should
not be presented as a live subscription; use the same folder-kind exclusion the
existing item queries use for Trash, and say so in a comment.

Add the API method in `internal/api/v1api/apigraph/mailbox.go`:

    // ListMailboxSubscriptions is every mailing list this mailbox receives:
    // who sends it, how much of it there is, and whether leaving it has been
    // asked for.
    func (self *graph) ListMailboxSubscriptions(ctx context.Context,
        arguments ListMailboxSubscriptionsArguments) (*MailboxSubscriptionPage, error)

taking a `MailboxID` and the usual `First`/`Offset`, requiring
`models.PermissionMailRead` through `self.requireMailbox`, and returning the
rows plus a total. Follow `ListMailboxThreads` for the argument handling and
the permission check.

Write a test in `internal/db/database_mailbox_test.go` (or a new
`database_subscription_test.go` beside it) that stores three messages from two
lists in a mailbox, one of them unread, and asserts two subscriptions with the
right counts and the right newest item. The test database is started by
`make test`.

Acceptance: the test passes, and the query answers over the API. Prove the
second with the command line client against the dev server:

    ./build/teanode --profile dev api 'query { ListMailboxSubscriptions(mailboxId: "<id>") { total subscriptions { name count unread } } }'

If the client has no raw-query command, use `curl` against
`http://127.0.0.1:10081/api/v1/graphql` with a session cookie, or add the call
to `internal/client/` as Milestone 4 needs it anyway.

### Milestone 4 — the Subscriptions page, and reading one like a conversation

At the end of this milestone there is a page in the dashboard listing the
subscriptions, and opening one shows its messages newest first the way a
conversation is shown.

Add the route `/mailbox/subscriptions` in `web/src/app.tsx` and a row in the
mailbox rail in `web/src/components/sidebar.tsx`, beside Contacts, using the
existing `NavLink` pattern there. Create
`web/src/pages/mailboxSubscriptions.tsx`.

The page is a `SettingsSection card` holding one `SettingsRow` per
subscription: the list's name as the title, the sending address and "N
messages, M unread, last on <date>" as the subtitle, and two actions —
an unsubscribe icon button and a chevron that opens the list's mail. Follow
`web/src/pages/access/groups.tsx` for the panel and row shapes and
`web/src/pages/mailboxContacts.tsx` for how a mailbox page is put together.
Dates go through `RelativeTime`, which shows "2 days ago" with the exact
timestamp and its zone in the tooltip; every icon button is wrapped in
`Tooltip` and carries an `aria-label`, which is what the rest of the dashboard
does and what the reviewer will check.

Reading one subscription reuses the conversation reader. Add
`GetMailboxSubscription(mailboxId, key)` to the API returning the same shape
the thread reader consumes — look at `GetMailboxThread` in
`internal/api/v1api/apigraph/mailbox.go` and return the same `entries` list,
capped the same way at 200 with a `truncated` flag. In the dashboard, render
it with the same `ThreadMessage` component `web/src/pages/mailbox.tsx` uses,
so a subscription reads exactly like a conversation: read messages collapsed
to a line, unread ones open.

Acceptance: with the dev server running, open
`http://127.0.0.1:10000/mailbox/subscriptions`, see a row per list, click one,
and read its messages. Check the same page at 390 pixels wide — the audit
described under "Validation" must report no problems.

### Milestone 5 — unsubscribing, all three ways

At the end of this milestone the button works and says what happened.

First, make the SSRF-guarded client reusable. Move `remoteClient`,
`allowRemoteAddress` and `parseRemoteTarget` out of
`internal/api/v1api/apimail/remote.go` into a new package
`internal/util/safefetch`, exporting `Client()`, `AllowAddress(address string)
error` and `ParseTarget(raw string) (*url.URL, error)`. Change `apimail` to
call the new package; its behaviour and its tests must not change. Do this as
its own commit so that the move and the new feature are separable.

Add the mutation in a new file
`internal/api/v1api/apigraph/mailbox_subscription.go`:

    // UnsubscribeMailboxSubscription asks a sender to stop, the way that
    // sender said to ask.
    func (self *graph) UnsubscribeMailboxSubscription(ctx context.Context,
        arguments UnsubscribeMailboxSubscriptionArguments) (*MailboxSubscription, error)

taking `MailboxID` and `Key`, requiring `models.PermissionMailSend` — sending
is what it does, whichever path it takes — and resolving the newest message of
that list to get its `List-Unsubscribe` value. Then, in order:

1. If the newest message promised one-click and has an `https:` URL, POST to
   it through `safefetch.Client()` with body `List-Unsubscribe=One-Click`,
   content type `application/x-www-form-urlencoded`, no cookies, no redirects
   followed beyond the client's own limit, a ten second timeout, and a reply
   body read and discarded up to 64KB. A 2xx is success. Record method
   `oneClick`.
2. Otherwise, if there is a `mailto:` URL, send that message from the
   mailbox's own address using the same path `SendMailboxMessage` uses —
   extract the sending code in `internal/api/v1api/apigraph/mailbox_compose.go`
   into a helper both can call rather than duplicating it. The `mailto:` URL
   may carry `subject` and `body` parameters; honour them, and default the
   subject to `unsubscribe` when it names none. Record method `mail`.
3. Otherwise, record method `link` and return without contacting anybody. The
   dashboard opens the `https:` URL in a new tab so the person can finish it
   themselves, because a page that needs a human cannot be pressed by a server.

Store the outcome in a new table. Migration `0025_mailbox_subscription.sql`:

    CREATE TABLE IF NOT EXISTS "mailbox_subscription" (
      "id"            VARCHAR(32) PRIMARY KEY,
      "created_at"    TIMESTAMPTZ NOT NULL,
      "modified_at"   TIMESTAMPTZ NOT NULL,
      "mailbox_id"    VARCHAR(32) NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
      "list_key"      TEXT NOT NULL,
      "requested_at"  TIMESTAMPTZ,
      "method"        VARCHAR(16) NOT NULL DEFAULT '',
      "failed"        BOOLEAN NOT NULL DEFAULT FALSE,
      "error"         TEXT NOT NULL DEFAULT ''
    );
    CREATE UNIQUE INDEX IF NOT EXISTS "mailbox_subscription_key"
      ON "mailbox_subscription" ("mailbox_id", "list_key");

with the obvious reverse that drops the index and the table. Check the exact
type of `mailbox.id` in an existing migration before writing this one and
match it.

Record an audit event for the request, with resource type
`mailbox_subscription`, so that "who asked to leave this list, and when" is
answerable — `internal/models/audit.go` lists the resource types and
`internal/api/v1api/apigraph/audit.go` resolves their names for the audit
page; add the new type to both.

Write tests: a table-driven test of the URL selection (one-click with https,
one-click with only mailto, no one-click but https present, mailto only, and
nothing usable), and a test of the POST against `httptest.NewServer` asserting
the method, the content type and the body. The SSRF guard is already covered
by the tests that move with it.

Acceptance: unsubscribe from a subscription in the dashboard and see the row
say what happened. For a repeatable demonstration, run a local server that
records the request:

    cd /tmp && python3 -m http.server 8099

then send yourself a message whose `List-Unsubscribe` names
`http://127.0.0.1:8099/leave` — which the SSRF guard will refuse, and that
refusal is itself the thing to observe, because it proves the guard is on the
path. To see a successful one, point it at a public URL you control.

### Milestone 6 — the unsubscribe button on a message

At the end of this milestone a message that carries the header has an
unsubscribe button in the toolbar over the conversation, and pressing it does
what the Subscriptions page does.

The conversation reader is in `web/src/pages/mailbox.tsx`; its toolbar is the
row of `IconAction` buttons built there. The message content component in
`web/src/pages/mailDetail.tsx` already receives the parsed headers, but the
toolbar needs to know before the content loads — so return the list fields on
the mailbox item's mail in the existing query instead: add `listKey`,
`listName` and `listOneClick` to the mail fields the mailbox queries ask for,
and show the button when `listKey` is not empty.

Ask before acting. Pressing it opens a `ConfirmDialog` naming the list and
saying what will happen — a request to the sender, or an email sent from your
address, or a page to open — because it cannot be undone and it tells the
sender the address is live.

Acceptance: open a subscription message in the dashboard, press the button,
confirm, and watch the row on the Subscriptions page change to "asked to
leave".

### Milestone 7 — documentation, changelog, and the deployment check

Add a `CHANGELOG.md` entry under `## [Unreleased]`, in the style of the
entries already there: what a person can now do and why it was worth doing,
not a list of components. Mention the new configuration nothing needs, which
is none.

If `docs/reference/` has a page describing the mailbox, add a paragraph on
subscriptions; otherwise leave the documentation to the changelog. Do not
invent a new documentation page for one feature.

Finally, run the full check — `make lint-ci` and `make test` — and deploy to
the development server the way the other work in this repository is deployed:
`make docker DOCKER_TAG=teanode:mailboxes`, `docker save … | ssh root@server
docker load`, then `docker compose up -d --force-recreate teanode` in
`/opt/teanode`. Watch the log line that names the version and confirm the
migrations applied.

### Milestone 8 — the sender's logo, from BIMI

At the end of this milestone a subscription row, and the line naming who a
message is from, carries the sender's logo where the sender publishes one, and
a coloured monogram of the sender's initial where they do not.

**What BIMI is, in plain language.** Brand Indicators for Message
Identification is a way for a sending domain to publish a logo in DNS so that
a mail program can show it beside the messages that domain sends. The domain
publishes a TXT record at `default._bimi.<domain>` that looks like:

    v=BIMI1; l=https://example.com/logo.svg; a=https://example.com/vmc.pem

`l=` is the logo, `a=` is a certificate that vouches for it, and either may be
empty. A message may name a different selector than `default` by carrying a
header:

    BIMI-Selector: v=BIMI1; s=winter

in which case the record to read is `winter._bimi.<domain>`.

**Why it is safe to show, and when.** The whole point of BIMI is that the logo
is only shown for mail that provably came from that domain. The condition is
that the message passed DMARC — which this server already evaluates and stores
in `mail.authentication_results` — and, in the specification, that the domain's
DMARC policy is enforcing (`p=quarantine` or `p=reject`) rather than `p=none`.
Without that condition a logo is worse than no logo: anybody could publish a
BIMI record on a lookalike domain and borrow a bank's mark. So the rule here
is: show a logo only when DMARC passed and was aligned, and never otherwise.

The `a=` certificate is a Verified Mark Certificate, which is a certificate
issued after somebody checked that the sender owns the trademark. Verifying one
properly means validating a chain against a list of issuers and pulling the
logo out of an extension inside the certificate. That is a substantial piece of
work and this milestone does not do it: the logo from `l=` is shown when DMARC
passed, and whether a certificate vouched for it is not claimed anywhere in the
interface. Say so in the changelog, because "verified" is the word BIMI's
marketing uses and this will not have earned it.

**What the logo file is.** SVG Tiny Portable/Secure: a restricted SVG with no
scripts, no external references, no animation and no embedded raster images.
Restricted precisely because it is going to be rendered inside a mail program.
Nothing enforces that but the sender's word, so it is treated here as hostile
markup either way.

**How to show it without leaking the reader.** Fetching the logo from the
sender's server when a message is opened tells the sender that this address
read this message at this moment — which is what the remote image proxy in
`internal/api/v1api/apimail/remote.go` exists to prevent. The logo is therefore
fetched by the server, through `internal/util/safefetch`, and cached; the
dashboard only ever asks this server for it. And it is served with a content
type of `image/svg+xml` from a URL the page loads as an `<img>` source, never
inlined into the page's own document: an `<img>` cannot run script from its
source, where an inlined `<svg>` element can.

**The work.** Add a resolver call beside the DMARC one in
`internal/dns/record_set.go` that reads the BIMI record for a domain and
selector. Add a small cache — a table `bimi_logo` keyed by domain and selector,
holding the fetched bytes, the content type, when it was fetched and when it
last failed — because a newsletter from one sender is fifty rows in a list and
fifty lookups otherwise. Fetch at most once a day per domain, and never on the
path of rendering a page: a row with no logo yet shows the monogram and the
logo appears on the next visit.

Add an endpoint on the mail API, beside the image proxy, that serves the
cached bytes for a domain. In the dashboard add a `SenderLogo` component that
shows the image when there is one and a monogram otherwise — the first letter
of the sender's name on a colour derived from the address, which is what the
account button in the rail already does and should be shared with it.

Acceptance: a subscription from a domain that publishes BIMI shows its logo; a
subscription from one that does not shows a monogram; the network tab shows
requests only to this server. Prove the DMARC condition by sending a message
that fails DMARC from a domain that does publish a record, and observing the
monogram.

## Validation

**The Go tests.** From the repository root:

    make test

This starts a PostgreSQL container, so Docker must be running. It leaves
`vendor/` reformatted as a side effect — run `git checkout -- vendor` before
committing and stage files by name.

**The linters.** From the repository root:

    make lint-ci

It checks gofmt, that no secrets are committed, that the three message
catalogues agree and differ, and that every configuration field is documented.
It must print `0 issues.`

**The development server.** From the repository root:

    make dev

serves the dashboard on `http://127.0.0.1:10000`, proxying the API on
`:10081`. Sign in with the development account.

**Sending a test message with list headers.** The development server accepts
SMTP on port 10025. From the repository root:

    printf 'From: Example Weekly <news@example.com>\r\n'\
    'To: ziyan@example.com\r\n'\
    'Subject: Issue 41\r\n'\
    'List-Id: Example Weekly <weekly.news.example.com>\r\n'\
    'List-Unsubscribe: <https://news.example.com/u/abc>, <mailto:leave@example.com>\r\n'\
    'List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n'\
    '\r\nHello.\r\n' | \
    curl -s --url smtp://127.0.0.1:10025 --mail-from news@example.com \
      --mail-rcpt ziyan@example.com --upload-file -

Check the recipient address against what the dev mailbox actually receives —
look at the aliases on the dev server's domain — and adjust.

**The layout audit.** This repository's dashboard is checked geometrically
rather than by eye. Load each new page in a frame at 390 and 1400 pixels and
assert: the page does not scroll sideways; no element is wider than the
window unless it is inside a container that declares `overflow-x: auto`; no
row of controls wraps onto a second line unintentionally; no element is
shorter than its own content; nothing is clipped without an ellipsis; every
tap target is at least 24 pixels in both directions on a phone, counting a
wrapping label as the target; no native `title` tooltips and no native
`<select>` elements, because this dashboard draws its own; and no row of
controls sits closer than 8 pixels to whatever is above it. The script that
does this lives in the working notes for this repository rather than in the
tree; rewrite it from this description if it is not to hand.

## Risks and what to do about them

A sender's unsubscribe URL may point inside this network. The SSRF guard
refuses it and the request fails with a message saying so; that is the correct
outcome and the test suite covers it.

A sender may treat the one-click POST as a signal that the address is live and
send more, not less. Nothing can prevent that, and it is why nothing here
unsubscribes without a person pressing the button. Say so in the confirmation
dialog.

The backfill reads every message in the mailbox once. On a large mailbox with
S3 storage that is a lot of small reads. It runs in batches with a pause, it
marks what it has examined so it never repeats work, and it can be left to run
over hours without holding a transaction.

The list identity is heuristic. A sender who changes their From address and
publishes no `List-ID` will appear as two subscriptions. That is visible and
harmless; do not try to merge them by guessing.
