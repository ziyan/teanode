# Images in a template, served from the domain that sent them

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file.

## Purpose / Big Picture

Somebody writing a template in the dashboard can put a picture in it — a logo,
a header, a signature — by uploading the file rather than by finding somewhere
on the internet to host it and pasting a URL. The picture is served over HTTPS
from the domain the message is sent from, so a recipient looking at where the
image came from sees only that domain.

The second thing follows from the first, and is the reason the addresses are
per message rather than per image. Each message gets its own address for each
picture in it. When a mail program fetches one, that message has been opened,
and the dashboard says so: when, how many times, and from roughly where.

After this change, an operator can:

- upload a picture while editing a layout or a template, see it in the preview,
  and send it;
- open a sent message in the dashboard and see whether it was opened, and when.

Both are visible without reading any code. Upload a picture, send yourself the
template, open the mail, and the message's page in the dashboard changes from
"not opened" to the time you opened it.

**What "opened" is worth, stated here because the dashboard must not overstate
it.** It means an address unique to that message was fetched. That is weaker
than a person reading the mail, in both directions. Apple's Mail Privacy
Protection fetches every image before the recipient has seen anything, so a
message can be reported opened that nobody has read. Most mail programs refuse
to load remote images at all until the reader asks, so a message that was read
can be reported unopened for ever. Gmail fetches through its own cache, so the
address it fetches from is Google's and the time may be the moment of delivery
rather than of reading. The number is a floor with false positives in it, not a
measurement, and the words in the dashboard have to say so.

## Progress

- [x] (2026-09-03 07:10Z) Milestone one: a media file, stored and served.
- [x] (2026-09-03 07:35Z) Milestone two: uploading one from the editors.
- [x] (2026-09-03 08:05Z) Milestone three: a unique address per message, and the rewrite on send.
- [x] (2026-09-03 09:05Z) Milestone four: recording and showing opens.
- [x] (2026-09-03 15:05Z) Milestone five: rolled out to a live deployment.
  Deployed, migrated, and proved from the public internet: a picture uploaded,
  a template sent to a real mailbox, the address in the delivered message under
  the sending domain, fetched over HTTPS with a certificate a mail program
  accepts, and two fetches counted as two with the first time unmoved.

  Two faults stood between a correct implementation and a picture that loads,
  and neither was visible from the server, because both fail in the reader's
  mail program.

  The address first named the domain's mail host, which is only right when this
  server answers HTTPS on that name. A mail server name can resolve to a
  machine whose port 443 belongs to something else entirely, and then the mail
  is delivered, signed and aligned while every picture in it is broken. Where
  mail arrives and where HTTPS answers are separate questions, and
  `domains[].linkHost` is how a domain answers the second one.

  Then a CDN in front of the server cached one per-message address, which would
  have stopped every open count at one: the first fetch is recorded and every
  one after it is answered by the cache. The address is `no-store`, which it
  had been until the code that writes the bytes set a year of caching over the
  top of it. Fixed, and the deployment test now asserts the caching on both
  addresses — a year at the picture's own address, nothing at the per-message
  one.

## Context and Orientation

This section assumes no knowledge of this repository.

**What this program is.** TeaNode is a mail server: it receives mail for a list
of domains, forwards it, and sends mail of its own. One Go binary with an
embedded React dashboard. `AGENTS.md` at the repository root is the general
orientation.

**What already exists that this builds on.**

`internal/storage/` keeps the raw messages. `Open` in
`internal/storage/filesystem.go` returns storage backed by a directory, and
when `Settings.S3` is set it also constructs a mirror in
`internal/storage/s3.go`. A read tries the local file first and falls back to
the mirror, which is how a message survives the local spool being lost, and how
one instance reads what another wrote. This is the pattern the media files
should follow, and the reason is under Decision Log.

`internal/db/` holds the schema as numbered pairs of `.sql` and
`.reverse.sql` files under `internal/db/migrations/`, the highest being
`0008_translation`. A row type lives in `internal/db/database_*.go` as a gorm
model, and the shape the rest of the program uses lives in `internal/models/`.
`internal/db/database.go` declares the interface every query goes through.

Templates and layouts arrived recently. A template is a subject and a body; a
layout wraps it; both may have translations, added by the `0008_translation`
migration. They are edited at `/domains/<id>/templates` and
`/domains/<id>/layouts` in the dashboard — `web/src/pages/templateEditor.tsx`
and `web/src/pages/layoutEditor.tsx` — and both use
`web/src/components/richText.tsx` to write the body and
`web/src/components/preview.tsx` to see it rendered.

Sending a templated message is `POST /api/v1/send/{domain}/{template}`, in
`internal/api/v1api/apisend/send.go`. It authenticates with an SMTP credential
in the handler, renders through `internal/util/templating`, and hands the
result to `internal/mailer`. The dashboard's compose page sends the same way.

Each domain now has a mail server name of its own — `mx.<domain>` by default,
configurable per domain — and a TLS certificate for it, obtained automatically.
`Configuration.MailHostsFor` in `internal/config/lookup.go` returns those
names. This is what makes an HTTPS address in the domain's own name possible,
and it is new: before it, every domain's mail was served under one name
belonging to a different domain.

The HTTP listeners are built in `cmd/run.go`. `internal/web/auth_middleware.go`
refuses anything under `/api/` without a session, except the paths in
`api.PublicPaths()` and the prefixes in `api.PublicPrefixes()` — the send
endpoint is a public prefix because it authenticates with a credential itself.

**Terms.**

*Media* — a file an operator uploaded to put in a template. Only pictures, for
now; the plan says why under Decision Log.

*Open* — a fetch of the unique address embedded in one sent message. See the
warning above about what it does and does not mean.

*Tracking pixel* — a picture of one transparent dot, put in a message for no
reason other than to be fetched. This plan does not add one: the pictures the
operator already put in the template do the same job, and a message with no
picture in it simply cannot report opens. That is a deliberate limit, under
Decision Log.

## Plan of Work

### Milestone one: a media file, stored and served

A `media` table, `models.Media`, storage, and a public endpoint that serves the
bytes. Nothing in the dashboard yet; the milestone is finished when a file put
in by hand can be fetched over HTTPS.

The migration is `internal/db/migrations/0009_media.sql` with its reverse. The
columns are the identifier, the domain it belongs to, the name it was uploaded
under, its content type, its size, a checksum of the bytes, and the created and
modified times. Not the bytes: those go to storage, for the same reason
messages do — a database that holds every picture is a database nobody can
restore quickly.

`models.Media` in `internal/models/media.go` is the shape the rest of the
program uses, and the gorm row goes in `internal/db/database_media.go`
alongside the queries: create, get by identifier, list by domain, delete.

Storage reuses `internal/storage`, whose `Storage` interface today speaks in
messages — `Put(ctx, id, headers, body)`. Media is not a message and must not
pretend to be. Add a second, smaller interface in the same package for opaque
files, backed by the same directory and the same optional S3 mirror, so that a
deployment with MinIO configured keeps its pictures in MinIO and one without
keeps them on disk, and a read falls back from one to the other exactly as it
does for messages.

The endpoint is `GET /media/{id}` with no session required, because a mail
program fetching a picture has no session and never will. It is not under
`/api/`, so the authentication middleware does not consider it at all. It
answers with the bytes, the stored content type, a long cache lifetime, and
nothing else. An identifier that does not exist is a 404 with no body.

Acceptance: with a row inserted by hand and a file in place,
`curl -sSI https://mx.<domain>/media/<id>` answers 200 with the right
`Content-Type`, and `curl` for an identifier that does not exist answers 404.

### Milestone two: uploading one from the editors

A button in the rich text editor's toolbar that takes a file, sends it, and
inserts an `<img>` for it at the cursor.

The upload is `POST /api/v1/media` — inside `/api/`, so it needs a session, as
it should: uploading is an operator's action. It takes a multipart form with
the file and the domain, checks the type and the size, writes the bytes to
storage, writes the row, and answers with the identifier and the address.

Two limits, enforced on the server and stated in the dashboard. The content
type must be one of `image/png`, `image/jpeg`, `image/gif`, `image/webp` or
`image/svg+xml` — and SVG is refused despite being an image, because an SVG is
a document that can carry script, and this one would be served from the
operator's own domain. The size limit is a megabyte, which is generous for a
logo and small enough that a mistake does not fill a disk.

The type is decided by sniffing the bytes, not by trusting what the browser
said. `http.DetectContentType` reads the first 512 bytes; a file whose sniffed
type is not on the list is refused whatever its name or its declared type says.

Acceptance: in the dashboard, uploading a PNG in the template editor inserts it
into the body, the preview shows it, and the file is fetchable at the address
the response gave. Uploading a `.png` that is really an HTML file is refused
with a message saying what is allowed.

### Milestone three: a unique address per message, and the rewrite on send

Until now the `<img>` in a template points at `/media/<id>`, which is the same
for everybody. On send, each one is rewritten to an address unique to that
message.

A second table, `0010_media_link.sql`: the token, the media it resolves to, the
delivery it belongs to, when it was created, and the columns milestone four
fills in. The token is sixteen random bytes, base32 without padding — not a
ULID, because a ULID carries a timestamp and sorts, and one that can be
guessed from another is one a stranger can enumerate.

The rewrite happens where the message is rendered, in
`internal/util/templating` or immediately after it in the send path: every
`src` naming `/media/<id>` for a media row belonging to this domain becomes
`https://<the domain's mail host>/m/<token>`, with a row created for each.
Anything else in a `src` is left alone — a picture the operator pasted from
somewhere else is theirs to keep pointing where it points.

The host comes from `Configuration.MailHostsFor(domain)`, the same function the
DNS panel and the certificates use, so a message from `example.net` fetches
from `mx.example.net` and names nothing else. This is the whole reason the
addresses are worth having and not merely a way to count opens.

`GET /m/{token}` serves the same bytes `/media/{id}` would, and is also public.

Acceptance: sending a template with a picture to a recipient produces a message
whose HTML contains `https://mx.<domain>/m/<token>` and no `/media/` address;
two sends of the same template produce two different tokens; fetching either
returns the picture.

### Milestone four: recording and showing opens

`GET /m/{token}` records the fetch before it answers: the first time, the most
recent time, how many times, the address it came from and the user agent. The
delivery row gains `opened_at` and `open_count`, so a list of messages can say
which were opened without joining.

A fetch is recorded once per request but the first one is the one that matters,
and the write must not delay the picture: record, then serve, and if the record
fails, serve anyway and log it. A picture that fails to load because a database
was busy is a worse outcome than a count that missed one.

The dashboard shows it on the message's page in the mail list: opened or not,
when first, how many times. The words say what the number means, in the terms
of the warning at the top of this plan, because a dashboard that says "Opened"
beside a message nobody read is lying to its operator. The wording to use:
"Opened" with the time, and underneath, quietly, that a mail program may fetch
pictures before anybody reads the message and many refuse to fetch them at all.

Acceptance: send a template with a picture to yourself, open it in a mail
program that loads images, and the message's page changes from not opened to
the time it was fetched, with a count of one. Open it again and the count goes
up while the first time stays.

### Milestone five: rolled out to the live deployment

Deploy, upload a picture to a real template, send one message, open it, and
watch the dashboard report it. Then check the message's source for the address:
it must name the sending domain and nothing else.

## Concrete Steps

From the repository root.

    make lint-ci        # 0 issues, and the three catalogues must agree
    make test           # unit tests
    make test-deployment   # the whole stack in Docker, end to end

The end-to-end check is where the acceptance for milestones one, three and four
belongs, because all three are about what a fetch over HTTP does.

## Validation and Acceptance

A person can confirm the whole plan without reading code:

Upload a picture in the template editor; it appears in the body and in the
preview. Send that template to yourself. The message arrives with the picture
showing. View the message's source: the address is under the sending domain,
and is not the same address a second send produces. Open the message in a
program that loads images; the dashboard's page for that message says it was
opened, when, and how many times. Read the words underneath and understand why
the number is a floor.

## Idempotence and Recovery

The migrations add two tables and drop them in reverse; neither touches an
existing column. Uploading the same file twice makes two rows, which is
harmless. A media row whose bytes are missing from storage answers 404 rather
than failing the request that renders a template.

The rewrite on send is the only step that changes a message, and it changes
only `src` attributes naming this server's own media. A template with no
pictures sends exactly as it does today.

## Interfaces and Dependencies

No new third-party dependency.

In `internal/models/media.go`:

    type Media struct {
        ID          string
        DomainID    string
        Filename    string
        ContentType string
        Size        int64
        Checksum    string
        CreatedAt   time.Time
        ModifiedAt  time.Time
    }

In `internal/storage/storage.go`, beside the existing `Storage`:

    // Files stores opaque bytes, as distinct from messages, and is backed by
    // the same directory and the same optional mirror.
    type Files interface {
        PutFile(ctx context.Context, id string, content []byte) error
        GetFile(ctx context.Context, id string) ([]byte, error)
        DeleteFile(ctx context.Context, id string) error
    }

In `internal/config`, nothing new: the media host is
`Configuration.MailHostsFor(domain)[0]`, which already exists.

## Surprises & Discoveries

- Observation: the template and layout editors are source editors — a textarea
  of HTML — not rich text. Only the compose page uses the rich text component.
  So the upload could not live in the toolbar: it had to work for a caret in a
  textarea as well, which is why it is a shared button and a hook rather than
  another toolbar command.
  Evidence: `web/src/pages/templateEditor.tsx` and `layoutEditor.tsx` render
  `<textarea className="code-editor">` for the HTML; `RichTextEditor` appears
  only in `web/src/pages/compose.tsx`.

- Observation: the storage layer is not "S3 or disk" but "disk, mirrored to S3",
  and a read falls back from the local file to the mirror.
  Evidence: `Open` in `internal/storage/filesystem.go` requires `Directory` and
  constructs the mirror only when `Settings.S3` is set; `Get` reads the file,
  and on `os.IsNotExist` tries `self.mirror`.

## Decision Log

- Decision: one address per message, not per recipient.
  Rationale: the rewrite happens where the body is composed, which is once per
  message and before it is split into deliveries — every recipient is handed
  the same bytes. Making it per recipient would mean rewriting the HTML again
  for each one, in the delivery path. The cost of not doing it is stated
  plainly: a message to three people that comes back opened says the message
  was opened, not which of them opened it. For the usual case, one recipient,
  there is no difference.
  Date/Author: 2026-09-03, milestone three.

- Decision: a failed rewrite sends the message anyway.
  Rationale: the row is what an address means, so failing to record one and
  sending the address regardless would mean a broken picture. Instead the
  picture falls back to the address that works for everybody and the send goes
  on. A message held back because a logo could not be given a unique address
  would be the wrong trade.
  Date/Author: 2026-09-03, milestone three.

- Decision: media follows the storage pattern messages already use — always on
  disk, mirrored to the object store when one is configured — rather than
  going to MinIO instead of the disk.
  Rationale: the operator asked for MinIO when configured and disk when not,
  and this gives that outcome while being the same shape as the code beside it.
  It is also better than either alone: serving comes off the local disk, the
  mirror is what makes a picture survive the spool being lost, and a read falls
  back to the mirror, which is how one instance serves what another uploaded.
  A second storage policy in one codebase is a thing to explain for ever.
  Date/Author: 2026-09-03, this plan. To be confirmed with the operator, since
  it is a deviation from the words of the request.

- Decision: no tracking pixel.
  Rationale: the pictures the operator put in the template already do it. A
  transparent dot added to every message is a thing put there for no purpose
  the reader would recognise, and a message with no picture reports no opens,
  which is a limit worth having rather than a gap worth filling.
  Date/Author: 2026-09-03, this plan.

- Decision: SVG is refused.
  Rationale: an SVG is a document that can carry script, and this one would be
  served over HTTPS from the operator's own domain, where a script would run
  with that origin. Refusing one format is cheaper than being sure about
  sanitising it.
  Date/Author: 2026-09-03, this plan.

- Decision: the token is random, not a ULID.
  Rationale: identifiers elsewhere in this program are ULIDs, which sort and
  carry a timestamp. A token that can be guessed from another lets a stranger
  fetch pictures meant for somebody else's message and, worse, mark it opened.
  Sixteen random bytes cannot be walked.
  Date/Author: 2026-09-03, this plan.

- Decision: the dashboard says what "opened" is worth, beside the number.
  Rationale: Apple's Mail Privacy Protection fetches pictures before anybody
  reads the message, and most programs refuse to fetch them at all. A dashboard
  that prints "Opened" without saying either is telling its operator something
  that is not true, in both directions.
  Date/Author: 2026-09-03, this plan.

## Outcomes & Retrospective

To be written as milestones complete.
