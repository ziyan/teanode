# Contacts and CardDAV

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log` and `Outcomes & Retrospective` must
be kept up to date as work proceeds. It is written to the requirements in
`~/.claude/PLAN.md`; this repository has no PLANS.md of its own.

It is plan B of `docs/planning/active/20260910-personal-agents-roadmap.md`.
Plan A, personal agents, is finished and merged; nothing here depends on
reading it.

## Purpose / Big Picture

After this change a person using this server has an address book: a real one,
that they edit in the dashboard and that their phone and their desktop keep in
step with over CardDAV. They open Contacts on an iPhone, add the account with
their mail address and the app password they already made for mail, and the
contacts they keep here appear. They add somebody on the phone; it is here a
moment later. They correct a name here; the phone has it next time it looks.

That is the whole of the user-visible outcome, and it is how you will know the
work succeeded. Concretely, at the end you can run this against a running
development server and see a contact go out and come back:

    curl --user "$ADDRESS:$APP_PASSWORD" \
      -X PROPFIND -H 'Depth: 1' \
      --data '<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getetag/></d:prop></d:propfind>' \
      https://localhost:10443/dav/USERID/contacts/default/

and see a 207 listing one `.vcf` href per contact, each with an ETag.

Today the server already learns addresses from traffic: every address a
mailbox has written to or heard from is remembered in the table
`mailbox_contact`, which is what the compose page completes from and what the
mailbox rule "sender is known" asks about. Those stay exactly as they are.
They are a different thing from an address book — a list of people you have
corresponded with, not a list of people you chose to keep — and this plan adds
the second without disturbing the first. A learned address becomes a contact
only when somebody presses "save to contacts".

## Progress

- [x] (2026-09-12 12:50Z) Researched the library question with a working spike;
      findings recorded in `Surprises & Discoveries` below.
- [x] (2026-09-12 13:05Z) Wrote this plan in full from the outline.
- [ ] Milestone 1: schema, model, database layer, API, dashboard.
- [ ] Milestone 2: the DAV mount, sign-in, discovery, read-only CardDAV.
- [ ] Milestone 3: writing, ETags, conflicts.
- [ ] Milestone 4: discovery niceties, DNS advisories, CLI, documentation.
- [ ] Milestone 5: the address book as an agent source.

## Surprises & Discoveries

Everything in this section came out of a throwaway program written against
`github.com/emersion/go-webdav` v0.7.0 before any of this plan's code existed.
It is recorded because three of these would otherwise be discovered the hard
way, as a confusing 404 or a silent 405.

- Observation: the CardDAV **server** in go-webdav does not implement
  `sync-collection`, the REPORT by which a client asks "what changed since
  the token you gave me last time". Only the client side has it.
  Evidence: a `sync-collection` REPORT against the library's own handler:

        REPORT /dav/alice/contacts/default/ depth=""
        sync-collection: 400 Bad Request: carddav: unsupported REPORT root
          "DAV:" "sync-collection"

  This shapes milestone 3; see the Decision Log entry "No sync-collection".

- Observation: the enumeration clients fall back to when there is no
  sync-collection — `PROPFIND` with `Depth: 1` on the address book, returning
  every object's ETag — works correctly.
  Evidence: from the same spike, after the layout fix below:

        PROPFIND /dav/alice/contacts/default/ depth="1"
        propfind depth 1: 207 lists the card: true with an etag: true

- Observation: the library decides **what kind of resource a URL is purely by
  counting path segments** after a configured prefix. One segment is the
  principal, two is the address-book home set, three is an address book, four
  is a contact. The layout is therefore not ours to choose, and
  `carddav.Handler.Prefix` must be set to the mount point or every resource is
  misread by however deep the mount is.
  Evidence: the function, quoted from the library:

        func (b *backend) resourceTypeAtPath(reqPath string) resourceType {
            p := path.Clean(reqPath)
            p = strings.TrimPrefix(p, b.Prefix)
            if !strings.HasPrefix(p, "/") { p = "/" + p }
            if p == "/" { return resourceTypeRoot }
            return resourceType(len(strings.Split(p, "/")) - 1)
        }

  Before the prefix was set, a `PROPFIND` on the address book answered `404`,
  because four segments made the library treat the collection as a contact and
  ask the backend for a card by that name. With the prefix set and the layout
  below it answers `207`.

- Observation: a trailing-slash redirect **silently downgrades a DAV request
  to a GET**. Go's `http.ServeMux` answers `PROPFIND /dav` with a 301 to
  `/dav/`, and Go's HTTP client turns a 301 into a `GET`, which arrives at the
  principal handler as an unsupported method.
  Evidence: the request log from the spike before both paths were registered:

        [PROPFIND /dav depth="0"]
        [GET /dav/ depth="0"]
        principal:  405 Method Not Allowed: unsupported method

  This matters here specifically: `internal/web/server.go` builds its router
  with `mux.NewRouter().StrictSlash(true)`, which performs exactly this
  redirect. The DAV routes must be registered so that no redirect can happen.

- Observation: `carddav.Filter` with an empty query matches **nothing**, not
  everything, because an "any of" test over zero filters is false.
  Evidence: `[backend query path=… had=1 kept=0 err=<nil>]`. Real clients
  always send a filter or use multiget, so this is a trap only for tests.

- Observation: re-encoding a vCard from the library's parsed representation is
  lossless for the things that would matter. The spike put a card carrying
  grouped properties, custom `X-` properties, typed and preferred values, and
  a long folded line, read it back, and found all of them.
  Evidence:

        round trip keeps "X-CUSTOM-THING:kept?" true
        round trip keeps "item1.X-ABADR:uk"     true
        round trip keeps "TYPE=work"            true
        round trip keeps "PREF=1"               true
        round trip keeps "long note"            true
        round trip keeps "+1-555-0100"          true

  This is what makes the storage decision below safe: the library hands the
  backend a parsed card rather than the bytes that arrived, so what we store
  is necessarily a re-encoding.

- Observation: a stale `If-Match` is refused with `412 Precondition Failed`
  by the library's PUT handling, given a backend that compares the ETag.
  Evidence: `stale If-Match answered: 412`.

- Observation: the principal resource is **not** served by the CardDAV
  handler. It has its own helper, `webdav.ServePrincipal`, and both must be
  mounted or discovery stops at the first step.

## Decision Log

- Decision: use `github.com/emersion/go-webdav` for the CardDAV protocol,
  with `github.com/emersion/go-vcard` for the card format, both vendored.
  Rationale: the spike above proves the server side does what this plan needs
  for reading, writing, discovery and conflicts. They are MIT licensed, by the
  author of the IMAP library this repository already vendors, and in the same
  style. The fallback considered was writing WebDAV over `net/http` by hand;
  it is not warranted given the spike.
  Date/Author: 2026-09-12, Claude with Ziyan Zhou.

- Decision: **No sync-collection.** Clients will keep in step by listing the
  address book with `PROPFIND Depth: 1` and comparing ETags, which is the
  mechanism every CardDAV client must implement anyway. We will not advertise
  a `DAV:sync-token` property, so no client will try the REPORT that the
  library cannot answer.
  Rationale: the library has no server-side support, and adding it means
  intercepting REPORT and PROPFIND in front of the library's handler and
  writing the XML ourselves. The cost of not having it is one listing per
  poll: for an address book of a few hundred contacts that is a few tens of
  kilobytes, which is nothing. Deletions are still discovered, because a
  client treats an href that has stopped being listed as deleted. Adding it
  later is purely additive — advertise the property and answer the REPORT —
  so nothing here forecloses it.
  Consequence: the `contact_tombstone` table and the per-collection change
  counter sketched in the original outline are **not** built. Machinery with
  no reader is worse than no machinery. If sync-collection is added later it
  brings its own tombstones.
  Date/Author: 2026-09-12, Claude.

- Decision: the address book belongs to the **account**, not to the mailbox,
  while signing in is per mailbox.
  Rationale: `docs/decisions/20260910-dav-signs-in-with-app-passwords.md`
  settles that a client signs in with a mailbox address and one of that
  mailbox's app passwords, and says the books it then sees are "the ones
  belonging to the account that owns that mailbox". A person with two
  mailboxes has one address book, reachable through either. This also matches
  how the agent is owned in plan A: the agent belongs to the person, and a
  mailbox is a source they grant it.
  Date/Author: 2026-09-12, Claude.

- Decision: store the **canonical re-encoded vCard text**, not the bytes that
  arrived, and compute the ETag from that text.
  Rationale: the library parses before the backend ever sees the request, so
  the original bytes are not available without wrapping the handler. The spike
  shows re-encoding preserves the properties, groups, parameters and folding
  that matter. Deriving the ETag from the stored text rather than from the
  request makes the ETag a property of what we hold, so the value a client
  gets from a listing and the value it gets from a fetch cannot disagree.
  Date/Author: 2026-09-12, Claude.

- Decision: the URL layout is `/dav/{userId}/contacts/{bookId}/{contactId}.vcf`.
  Rationale: the segment count is forced by the library, as the discovery above
  shows. Within that, the opaque account id is used rather than the mail
  address, so the path is stable when an address changes and no `@` has to be
  escaped. `contacts` is the fixed home-set segment, leaving room for
  `calendars` beside it when plan C arrives.
  Date/Author: 2026-09-12, Claude.

## Outcomes & Retrospective

To be written at the end of each milestone. Nothing to report yet: no code has
been written.

## Context and Orientation

This section assumes you know nothing about this repository. Read it before
the plan of work.

**What this program is.** TeaNode is a mail server written in Go with a web
dashboard written in React. `AGENTS.md` at the repository root is the
orientation document. The Go code lives under `internal/`, one directory per
subsystem; the dashboard lives under `web/src/`.

**How a request reaches Go code.** `internal/cmd/server/run.go` builds the
HTTP server near line 869. It constructs a list of `web.Component` values and
hands them to `web.NewServer`. A `web.Component` is anything with one method,
defined in `internal/web/web.go`:

    type Component interface {
        AddRoutes(*mux.Router) error
    }

`web.NewServer` in `internal/web/server.go` makes a router with
`mux.NewRouter().StrictSlash(true)` — note the `StrictSlash`, which the
discovery above warns about — and calls `AddRoutes` on each component. The
router is `github.com/gorilla/mux`. Existing components are the v1 API
(`internal/api/v1api`) and the static dashboard files
(`internal/web/static.go`).

Around the whole server, `run.go` wraps a chain of middlewares. The one that
matters here is `web.MakeAuthenticationMiddleware`, in
`internal/web/auth_middleware.go`. Read it before writing the DAV mount: it
establishes who the caller is from a session cookie, and then **only refuses
paths beginning with `/api/`**. Everything else is passed through so the
dashboard can load its own login form. That is why a `/dav/` mount can
authenticate callers its own way without changing the middleware.

**How somebody signs in from a mail program.** Not with their account
password, ever. They make an *app password*: a secret shown once, stored as a
hash, revocable on its own, belonging to one mailbox. The model is
`models.MailboxAppPassword` in `internal/models/mailbox.go`. The IMAP server
checks one in `internal/imap/server.go` around line 199 by calling

    access.AuthenticateAppPasswordWithID(tx, username, password)
        (*models.Mailbox, *models.MailboxAppPassword, error)

in `internal/access/`. The username is one of the mailbox's addresses. Every
way of being wrong returns the single error `access.ErrInvalidAppPassword`,
and the function deliberately performs a password hash even when refusing, so
that a wrong guess cannot learn from how long the refusal took. **Use this
function; do not write another.**

**What a mailbox is.** `models.Mailbox` in `internal/models/mailbox.go`. It
has an `ID`, a `UserID` naming the account that owns it, and `Addresses`.

**The learned contacts that already exist.** `models.MailboxContact`, stored
in the table `mailbox_contact` created by
`internal/db/migrations/0015_mailboxes.sql`:

    CREATE TABLE "mailbox_contact" (
        "mailbox_id"      character varying(32)  NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
        "address"         character varying(320) NOT NULL,
        "name"            character varying(256) NOT NULL DEFAULT '',
        "last_seen_at"    timestamp with time zone NOT NULL,
        "count"           integer                NOT NULL DEFAULT 1,
        "auto_replied_at" timestamp with time zone,
        PRIMARY KEY ("mailbox_id", "address")
    );

The database methods are declared in `internal/db/database_mailbox.go` around
line 93 (`TouchContact`, `ListContacts`, `GetContact`, `SaveContact`,
`DeleteContact`) and implemented around line 1738. The dashboard page is
`web/src/pages/mailboxContacts.tsx`. **None of this changes in this plan**
beyond the addition described in milestone 5.

**How the database layer is written.** `internal/db/` holds one file per area.
Each defines a GORM row type with a `TableName()` method, a conversion to and
from the `models` type, and methods on `*transaction`. Every method a
transaction offers is also declared in the big interface in
`internal/db/database_memory.go`, and the compiler will tell you if you forget.
Writes that should appear in the administrative audit log go through
`self.applyMutation(resourceType, resourceId, action, before, after, write)`,
defined in `internal/db/database_audit.go`; writes that should not, do not.

**How migrations work.** One SQL file per change in
`internal/db/migrations/`, named `NNNN_short_name.sql`, each with a
`NNNN_short_name.reverse.sql` beside it that undoes it. They are discovered by
`//go:embed *.sql` in `internal/db/migrations/migrations.go` and applied in
name order, so there is nothing to register. The highest number in the tree
today is `0047_agent_skill_scope.sql`; this plan adds `0048`.

**How the dashboard talks to the server.** A single GraphQL-shaped endpoint
under `/api/`, built by reflection over Go interfaces in
`internal/api/v1api/apigraph/`. You declare a method on an interface with a
doc comment, implement it on `*graph`, and the machinery exposes it. Arguments
come in a struct named after the method. Permissions are checked inside the
implementation with `self.requirePermission(ctx, models.PermissionX)` or, for
things belonging to the signed-in person, helpers like `requireAgentPerson`.

**How the command line reaches the same operations.** `internal/cmd/` holds
the `teanode` command, and `internal/client/` holds a small typed client that
sends the same GraphQL documents. A new feature is expected to appear in all
three places — dashboard, command line, and the agent's tools — and
`CONTRIBUTING.md` says so.

**Terms used in this plan, in plain language.**

*vCard* is the file format an address book entry is written in: lines like
`FN:Ada Lovelace` between `BEGIN:VCARD` and `END:VCARD`. One contact is one
vCard.

*CardDAV* is a way of keeping address books in step over HTTP, defined in
RFC 6352. It is WebDAV — itself HTTP with extra methods — with rules about
address books. The methods beyond ordinary HTTP that matter here are
`PROPFIND`, which asks for properties of a URL and, with a `Depth: 1` header,
of everything directly inside it; and `REPORT`, which runs a named query.

*ETag* is a short opaque string naming a version of a resource. A client sends
it back in an `If-Match` header when writing, so the server can refuse with
`412 Precondition Failed` if somebody else wrote first.

*Principal* is the CardDAV word for "the signed-in person", exposed as a URL.
*Home set* is the collection holding that person's address books.

*multiget* is the REPORT by which a client asks for many named cards at once,
having learned from a listing which ones it needs.

## Plan of Work

The work is five milestones. The first builds the address book with no
protocol at all, so it can be seen working in the dashboard before any DAV
exists. The second serves it read-only to a real client. The third lets a
client write. The fourth makes it pleasant to set up. The fifth connects it to
the agent.

### Milestone 1 — the address book itself

At the end of this milestone a person can open the dashboard, add a contact
with a name, some email addresses and some phone numbers, edit it, and delete
it; and none of it is visible over any protocol yet. There is no CardDAV in
this milestone, deliberately: the storage and the editing can be got right and
seen working without it.

*Schema.* Add `internal/db/migrations/0048_addressbook.sql` and its reverse.
Two tables. The first is the address book:

    CREATE TABLE "addressbook" (
        "id"          varchar(32)  NOT NULL,
        "user_id"     varchar(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "name"        varchar(200) NOT NULL DEFAULT '',
        "description" text         NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE INDEX "addressbook_user" ON "addressbook" ("user_id");

The second is the contact. It holds the vCard text as the single source of
truth, and beside it the few fields we search and sort on, extracted when the
card is written so that no query has to parse vCards:

    CREATE TABLE "contact" (
        "id"             varchar(32)  NOT NULL,
        "addressbook_id" varchar(32)  NOT NULL REFERENCES "addressbook" ("id") ON DELETE CASCADE,
        "created_at"     timestamptz  NOT NULL,
        "modified_at"    timestamptz  NOT NULL,
        "uid"            varchar(255) NOT NULL,
        "etag"           varchar(64)  NOT NULL,
        "card"           text         NOT NULL,
        "name"           varchar(256) NOT NULL DEFAULT '',
        "organization"   varchar(256) NOT NULL DEFAULT '',
        "emails"         text         NOT NULL DEFAULT '',
        "phones"         text         NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "contact_uid" ON "contact" ("addressbook_id", "uid");
    CREATE INDEX "contact_name" ON "contact" ("addressbook_id", "name");

`uid` is the vCard's own `UID` property, which is how a client names a person
across devices; the unique index on it is what stops the same person being
added twice. `emails` and `phones` are newline-joined lists, kept for search
and for the dashboard's list view; the card remains authoritative.

*Model.* Add `models.AddressBook` and `models.Contact` to a new file
`internal/models/contact.go`. Keep them plain: ids, times, the card text, the
extracted fields. Do not put vCard parsing in `models`.

*Database layer.* Add `internal/db/database_contact.go` with the row types,
the conversions, and methods on `*transaction`:
`ListAddressBooks(userId)`, `GetAddressBook(id)`, `CreateAddressBook(book)`,
`UpdateAddressBook(book)`, `DeleteAddressBook(id)`, `ListContacts(bookId,
query, limit)`, `GetContact(id)`, `GetContactByUID(bookId, uid)`,
`PutContact(contact)`, `DeleteContact(id)`. Declare every one of them in the
interface in `internal/db/database_memory.go`. Writes to contacts are *not*
audited: an address book is the person's own data, changed constantly by their
phone, and an audit row per contact edit would drown the administrative log
that exists to show what operators did. Creating and deleting an address book
*is* audited, as a change to the account's shape.

*vCard handling.* Add a new package `internal/contacts/` holding everything
that knows the card format, so that neither `db` nor `api` has to. It needs:

    package contacts

    // Parse reads one vCard and returns what the columns beside it hold.
    func Parse(card []byte) (*Parsed, error)

    // Encode writes a card back out in the canonical form this server
    // stores, which is what its ETag is computed over.
    func Encode(card vcard.Card) ([]byte, error)

    // ETag names a version of a card: the same bytes always give the same
    // answer, and different bytes practically never do.
    func ETag(card []byte) string

    type Parsed struct {
        UID          string
        Name         string
        Organization string
        Emails       []string
        Phones       []string
        Card         []byte
    }

`Parse` must supply a `UID` when the card has none, because a card written by
the dashboard will not have one and the protocol needs one: generate the same
kind of identifier the rest of the repository uses and write it into the card
before encoding. `ETag` is the hex of a SHA-256 over the canonical bytes,
truncated to 32 characters, which is what the spike used.

*API.* Add `internal/api/v1api/apigraph/contact.go` with a query interface and
a mutation interface: `ListAddressBooks`, `ListContacts(addressBookId, query,
first)`, `GetContact`, `SaveContact(addressBookId, id, card)` and
`DeleteContact`. The person's own data, so gate on the signed-in account
owning the book rather than on an operator permission. The dashboard sends
whole vCard text for `SaveContact`; the server parses, validates, canonicalizes
and stores. A dashboard that had to assemble vCard text by hand would be
unpleasant, so also accept the fielded form — name, organization, emails,
phones — and build the card server-side when no card text is given. Both paths
end at the same `contacts.Parse`.

*Dashboard.* `web/src/pages/mailboxContacts.tsx` today lists learned contacts.
Give the page two sources: the address book proper, and the learned addresses
as before, under a heading that says what they are. Adding, editing and
deleting act on the address book. Reuse `DataTable`, `FormDialog`,
`ConfirmDialog` and `useToast` exactly as the page already does — and note the
house rule, which the repository is strict about: **every success and every
error is reported with a toast**, never as inline text near the form. Every
user-visible string needs an entry in all three of `web/src/i18n/en.ts`,
`zh.ts` and `ja.ts`; `make lint-ci` fails if they disagree.

*Creating the first address book.* Do not ask people to make one. When
`ListAddressBooks` finds none for an account, create one named "Contacts" and
return it. That keeps the URL layout populated for milestone 2 without a setup
step.

Acceptance: start a development server with `make dev`, sign in, open the
contacts page, add a contact with two email addresses, see it listed, edit the
name, see the change, delete it, see it gone — each with a toast. Then
`psql` the development database and see one `addressbook` row and the
`contact` rows, each with a `card` column containing `BEGIN:VCARD`.

### Milestone 2 — a real client can read it

At the end of this milestone somebody adds a CardDAV account on a phone or in
a desktop contacts program, using their mail address and an app password, and
their contacts appear. They cannot yet change them from the device.

*Vendor the libraries.* From the repository root:

    go get github.com/emersion/go-webdav@v0.7.0
    go get github.com/emersion/go-vcard@v0.1.0
    go mod tidy
    go mod vendor

Note the repository builds with `-mod=vendor`. Also note, from the project's
notes to contributors, that `make test` leaves vendored files rewritten by
`gofmt`; run `git checkout -- vendor` before committing and stage files by
name rather than with `git add -A`.

*The package.* Add `internal/dav/`. It exposes one constructor returning a
`web.Component`:

    func New(database db.Database, configuration config.Store) (web.Component, error)

and registers routes in `AddRoutes`. Wire it into the component list in
`internal/cmd/server/run.go` beside the API component.

*Routing, with the traps from the discoveries above.* The mount is `/dav`. Four
things must be served, and no request may be answered with a redirect, because
a redirect turns a `PROPFIND` into a `GET`:

    /dav            and /dav/                  the service root: answers
                                               current-user-principal
    /dav/{userId}/                             the principal
    /dav/{userId}/contacts/                    the home set
    /dav/{userId}/contacts/{bookId}/           an address book
    /dav/{userId}/contacts/{bookId}/{id}.vcf   a contact

Register both `/dav` and `/dav/` explicitly. Because the repository's router
is built with `StrictSlash(true)`, register each collection path in both its
bare and its trailing-slash form, or use a single `PathPrefix("/dav")` route
and dispatch inside the package; the second is simpler and is what to do.

The root and the principal are served by `webdav.ServePrincipal`, not by the
CardDAV handler:

    webdav.ServePrincipal(writer, request, &webdav.ServePrincipalOptions{
        CurrentUserPrincipalPath: "/dav/" + userId + "/",
        HomeSets: []webdav.BackendSuppliedHomeSet{
            carddav.NewAddressBookHomeSet("/dav/" + userId + "/contacts/"),
        },
        Capabilities: []webdav.Capability{carddav.CapabilityAddressBook},
    })

Everything under `/dav/{userId}/contacts/` goes to
`&carddav.Handler{Backend: ..., Prefix: "/dav"}`. **The prefix is not
optional**; without it the library miscounts segments and answers 404.

*Sign-in.* A `PROPFIND` arriving with no `Authorization` header is answered
`401` with `WWW-Authenticate: Basic realm="TeaNode"`. With one, decode it and
call `access.AuthenticateAppPasswordWithID`. Refuse over plain HTTP unless the
server is running without TLS for development. Put the resolved mailbox and
account on the request context; the CardDAV backend reads them from there,
because the library's `Backend` methods take only a `context.Context`.

Two protections, both of which already exist in this repository and should be
reused rather than reinvented: the sign-in rate limiter that `run.go` builds
as `self.authLimiter(configuration)` and hands to the API, so that a stolen
address cannot be used to grind app passwords; and `TouchAppPassword`, so the
app-password page shows when a device last used it.

A person may only ever reach their own principal. A request for
`/dav/{other}/…` where `{other}` is not the account that signed in is answered
`403`, not `404`: there is no secret in the fact that other accounts exist, and
a 404 would send a client into a retry loop.

*The backend.* Implement `carddav.Backend` in `internal/dav/carddav.go`,
reading from the database layer of milestone 1. For this milestone the four
writing methods — `CreateAddressBook`, `DeleteAddressBook`,
`PutAddressObject`, `DeleteAddressObject` — return
`webdav.NewHTTPError(http.StatusForbidden, …)`. That is a complete, honest
read-only server, and it is worth having as its own step because a client that
can read is proof the discovery chain, the sign-in and the path layout are all
right.

Acceptance, and this is the milestone's whole point: on a phone, add a CardDAV
account with the server's address, the mail address as username and an app
password, and see the contacts from milestone 1. If a phone is inconvenient,
the same thing from the command line, which must also work:

    curl --user "$ADDRESS:$APP_PASSWORD" -X PROPFIND -H 'Depth: 0' \
      --data '<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>' \
      https://localhost:10443/dav/

expecting a `207` naming `/dav/{userId}/`, and then `Depth: 1` on the address
book expecting one href per contact, each with a `getetag`.

### Milestone 3 — the device can write

At the end of this milestone a contact added on the phone appears in the
dashboard, an edit on either side reaches the other, and two devices editing
the same contact at once cannot silently overwrite one another.

Implement `PutAddressObject` and `DeleteAddressObject`. The put receives a
parsed `vcard.Card` and a `*carddav.PutAddressObjectOptions` carrying the
conditional headers. The order of work is: canonicalize the card through
`contacts.Encode`, compute the ETag, look for an existing contact at that
path, and then decide.

    if options.IfMatch.IsSet() {
        matched, err := options.IfMatch.MatchETag(existing.ETag)
        if err != nil || !present || !matched {
            return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, …)
        }
    }
    if options.IfNoneMatch.IsWildcard() && present {
        return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, …)
    }

`If-Match` with a stale ETag means somebody else wrote first: refuse with 412
and let the client re-read and merge. `If-None-Match: *` means "only if it is
not there": refuse with 412 if it is. The spike confirms the library turns
these into the right status code.

Deleting is a delete of the row. Nothing is tombstoned, per the decision above:
a client discovers the deletion because the href stops appearing in the
listing.

Two smaller things. The filename in the URL is the client's to choose, and
clients do not agree on what it should be; map it to a contact by the `id`
column, and when a client puts a card to a path this server has never seen,
create a contact whose id is taken from that filename if it is a valid
identifier and generated otherwise. And give `AddressBook.MaxResourceSize` a
real value — a megabyte — so a client knows not to send a ten-megabyte
photograph, and enforce it, refusing a larger card with `507`.

Acceptance: add a contact on the device, see it in the dashboard within a
refresh; edit it in the dashboard, see the device pick the change up; then
prove the conflict is caught with two `curl` calls, the second carrying the
now-stale ETag:

    curl --user "$ADDRESS:$APP_PASSWORD" -X PUT -H 'If-Match: "STALE"' --data-binary @card.vcf \
      https://localhost:10443/dav/USERID/contacts/default/ID.vcf
    → HTTP/1.1 412 Precondition Failed

### Milestone 4 — easy to set up, and documented

At the end of this milestone somebody can type only their mail address into a
contacts app and have it find the server.

Serve `/.well-known/carddav` as a `301` to `/dav/`, which is how RFC 6764 says
a client bootstraps from a bare domain. Serve `/.well-known/caldav` the same
way, answering `404` for now behind it, so plan C has the route waiting.
Remember that the path must not be caught by the dashboard's catch-all static
handler; register it before that component, or inside the DAV component which
is registered earlier.

Add advisory checks for the `_carddavs._tcp` SRV record in `internal/dns`,
beside the checks already there, so the domain page can tell an operator that
publishing one would let clients find the server from an address alone. They
are advisory: nothing breaks without them.

Extend the command line: `teanode mailbox contact list|add|edit|remove`
reaching the same API, because the repository expects parity between the
dashboard and the command line. Add a row to the table in
`docs/reference/command-line.md`.

Write `docs/subsystems/contacts.md` in the style of the documents already in
that directory: what an address book is, where it lives, how a client signs
in, the URL layout and why it is forced, what the ETag means, and — honestly —
that there is no sync-collection and why that is all right. Add the
operator-facing account to `CHANGELOG.md` under Unreleased. Update
`AGENTS.md` and `docs/reference/project-structure.md` to mention
`internal/dav/` and `internal/contacts/`.

Acceptance: in a contacts app, choose "add CardDAV account", type only
`alice@example.com` and the app password, and have it find the server.

### Milestone 5 — the address book as something the agent knows

At the end of this milestone the agent can look somebody up in the person's
real address book, and a learned address can be promoted into it.

Plan A gave each person an agent that reaches the server through the same
operations the dashboard uses, with their permissions. Its tools live in
`internal/agent/tools/`, one directory per family, and `contact` is already
one of them, reading the learned addresses. Give those tools the address book
as their first source and the learned addresses as a second, clearly labelled
as suggestions rather than as contacts the person keeps.

Add "save to contacts" to the learned list in the dashboard, which creates a
contact from an address and a name. Add the corresponding agent tool action,
which is a write and therefore asks first under the tool policy plan A
established.

Acceptance: ask the agent "what is Ada's email address" and have it answer
from the address book; ask it to save a learned address, confirm the card it
offers, and see the contact appear in the dashboard and then on the phone.

## Concrete Steps

Run everything from the repository root, `/home/ziyan/projects/ziyan/teanode`.

Before starting, confirm the tree is clean and the tests pass, so that any
failure later is yours:

    make test        # starts a PostgreSQL container; needs Docker
    git checkout -- vendor
    make lint-ci

`make test` prints a line like `DONE 1129 tests in 5.401s` and exits 0.

For milestone 1, after writing the migration, apply it by starting the
development server, which migrates on startup:

    make dev

and check it landed:

    docker compose -f dev/docker-compose.yml exec -T postgres \
      psql -U teanode -c '\d contact'

For milestone 2, after vendoring, confirm the build still works with the
vendor directory rather than the module cache, which is what CI uses:

    go build -mod=vendor ./...

The `curl` invocations in each milestone's acceptance are the observable
proof; keep their transcripts in `Artifacts and Notes` below as you go.

After every milestone: `make test`, then `git checkout -- vendor`, then
`make lint-ci`, then `make lint`. Commit with the staged files named
explicitly. Do not run `git add -A`: the repository's notes record that a
previous session nearly committed a directory of private keys that way.

## Validation and Acceptance

Each milestone states its own acceptance in terms of what a person can do,
above. Beyond those, the following tests must exist and must fail before the
change and pass after.

In `internal/contacts/`, table tests over awkward cards: a card with grouped
properties and `X-` properties survives `Parse` then `Encode` with all of them
intact; a card with no `UID` comes back with one; the same card encoded twice
gives the same ETag and a changed card gives a different one; a card that is
not a vCard at all is refused with an error rather than stored.

In `internal/dav/`, tests against `httptest` with a real database from
`dbtest`, never the network: an unauthenticated `PROPFIND` is answered 401
with a `WWW-Authenticate` header; a wrong app password is answered 401; a
correct one reaches the principal; a request for another account's principal
is answered 403; `PROPFIND Depth: 1` on the address book lists one href per
contact with an ETag; `PUT` with a stale `If-Match` is answered 412; `PUT`
with `If-None-Match: *` over an existing contact is answered 412; a card over
the size limit is answered 507.

One test deserves singling out because it encodes a discovery that cost real
time: assert that `PROPFIND` on the address book **without** a trailing slash
is answered `207` rather than a redirect. If somebody later re-registers these
routes in a way that lets `StrictSlash` redirect, that test fails and says why.

The whole suite: `make test` and expect it to pass, `make lint-ci` and
`make lint` to report no issues.

## Idempotence and Recovery

Every step here is safe to repeat. The migration is additive: it creates two
tables and touches nothing that exists, and its reverse drops exactly those two
tables. Re-running `make dev` re-applies nothing already applied. Vendoring is
idempotent. If a milestone goes wrong, `git checkout --` the files it touched;
nothing outside the repository has changed.

The one irreversible thing in the whole plan is a person's address book
content, and only from milestone 3, when a device can delete a contact. A
delete is a row delete with no tombstone. That is deliberate — see the
Decision Log — but it means a client that goes wrong can remove contacts. The
ordinary database backup described in `docs/reference/deployment.md` is the
recovery path, and the documentation written in milestone 4 should say so
plainly rather than pretend otherwise.

## Artifacts and Notes

The spike that produced the discoveries above is not checked in; it was a
scratch program. Its output, which is the evidence quoted throughout:

    principal: /dav/alice/ <nil>
    home set: /dav/alice/contacts/ <nil>
    books: 1 <nil>
      book "Contacts" at /dav/alice/contacts/default/ (max 1048576 bytes)
    put etag: 60ebf1a7e3609d8ac3b7109aa545db1c <nil>
    round trip keeps "X-CUSTOM-THING:kept?" true
    round trip keeps "item1.X-ABADR:uk"     true
    round trip keeps "TYPE=work"            true
    round trip keeps "PREF=1"               true
    round trip keeps "long note"            true
    round trip keeps "+1-555-0100"          true
    stale If-Match answered: 412
    propfind depth 1: 207 lists the card: true with an etag: true
    sync-collection: 400 Bad Request: carddav: unsupported REPORT root
      "DAV:" "sync-collection"

## Interfaces and Dependencies

New dependencies, both MIT licensed, both vendored:
`github.com/emersion/go-webdav` v0.7.0 and `github.com/emersion/go-vcard`
v0.1.0. The first pulls in `github.com/emersion/go-ical`, which is unused
until plan C.

In `internal/contacts/`, define:

    func Parse(card []byte) (*Parsed, error)
    func Encode(card vcard.Card) ([]byte, error)
    func ETag(card []byte) string

    type Parsed struct {
        UID          string
        Name         string
        Organization string
        Emails       []string
        Phones       []string
        Card         []byte
    }

In `internal/models/contact.go`, define:

    type AddressBook struct {
        ID          string
        UserID      string
        CreatedAt   time.Time
        ModifiedAt  time.Time
        Name        string
        Description string
    }

    type Contact struct {
        ID            string
        AddressBookID string
        CreatedAt     time.Time
        ModifiedAt    time.Time
        UID           string
        ETag          string
        Card          string
        Name          string
        Organization  string
        Emails        []string
        Phones        []string
    }

In `internal/db/database_contact.go`, define on `*transaction`, and declare in
the interface in `internal/db/database_memory.go`:

    ListAddressBooks(userId string) ([]*models.AddressBook, error)
    GetAddressBook(addressBookId string) (*models.AddressBook, error)
    CreateAddressBook(book *models.AddressBook) (*models.AddressBook, error)
    UpdateAddressBook(book *models.AddressBook) (*models.AddressBook, error)
    DeleteAddressBook(addressBookId string) error
    ListContacts(addressBookId, query string, limit int) ([]*models.Contact, error)
    GetContact(contactId string) (*models.Contact, error)
    GetContactByUID(addressBookId, uid string) (*models.Contact, error)
    PutContact(contact *models.Contact) (*models.Contact, error)
    DeleteContact(contactId string) error

Note that `ListContacts`, `GetContact` and `DeleteContact` collide by name with
the learned-contact methods already on `*transaction`
(`internal/db/database_mailbox.go`, around line 93). Rename the older ones to
`ListLearnedContacts`, `GetLearnedContact`, `SaveLearnedContact`,
`DeleteLearnedContact` and `TouchLearnedContact` as the first commit of
milestone 1, before adding anything: it is a mechanical rename the compiler
checks completely, it says what those methods are, and it avoids two things
called `ListContacts` meaning different things forever after.

In `internal/dav/`, define:

    func New(database db.Database, configuration config.Store) (web.Component, error)

and a backend type satisfying `carddav.Backend` and
`webdav.UserPrincipalBackend`.

---

*Revision note (2026-09-12, Claude):* written in full from the outline that
previously occupied this file. The substantive departures from that outline
are recorded in the Decision Log and are: sync-collection is not implemented
and the tombstone table and change counter it would have needed are not built;
the URL layout is forced by the library rather than chosen; and the address
book belongs to the account rather than the mailbox. The milestones were
re-cut from five to five with different boundaries, so that the address book
is demonstrably working in the dashboard before any protocol exists.
