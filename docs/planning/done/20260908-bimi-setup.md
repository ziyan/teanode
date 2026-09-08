# Helping an operator publish their own logo

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log` and `Outcomes & Retrospective` must
be kept up to date as work proceeds. The conventions it follows are in
`~/.claude/PLAN.md`; the repository's writing rules are in `CONTRIBUTING.md`.

## Purpose / Big Picture

TeaNode now shows the logo a sending domain publishes for its mail. This is the
other side of it: helping the person running the server publish one for their
own domains.

Doing that today means knowing four things nobody tells you. That BIMI needs
DMARC at `p=quarantine` or `p=reject`, and that TeaNode's own suggested DMARC
record is `p=none`, so every domain starts unable to use it. That the logo has
to be a particular restricted flavour of SVG, and that a file which breaks the
rules produces a blank space at the receiver with no error anywhere. That it
has to be served over HTTPS from somewhere, which for a person with a mail
server and no web server is its own problem. And that Gmail and Yahoo will not
show it without a certificate that costs about a thousand dollars a year,
which is the single most useful thing to learn before starting rather than
after.

After this change, the operator opens a domain's DNS tab and sees a BIMI row
that either tells them exactly what is stopping them or gives them a record to
paste. They can upload an SVG, be told precisely what is wrong with it if
anything is, and have this server host it at a stable HTTPS address. The row
then verifies the way every other record on that page verifies: resolved,
fetched, and checked.

You can see it working: upload a logo on a domain that has DMARC at `p=none`,
and the page says the policy is what is stopping it and offers the record to
change. Change it, and the same row turns green with the mark drawn beside a
sample message.

## Progress

- [x] (2026-09-08 19:15Z) Research: read the record set checker, the media
      store and its deliberate refusal of SVG, the file storage interface, and
      the public serving routes. Findings are in `Context and Orientation`.
- [x] (2026-09-08 19:45Z) Milestone 1 — the BIMI row, and the DMARC
      prerequisite said plainly. `checkBimi` in `internal/dns/record_set.go`,
      a `Blocked` field on `Record` rendered under the value, and
      `internal/dns/bimi_test.go` over how the policy is read. On the dev
      server the row shows as optional against a domain that publishes
      `p=reject`, which is why no notice appears there.
- [x] (2026-09-08 20:20Z) Milestone 2 — upload a logo, validated, and host it.
      `internal/bimi/logo.go` with sixteen cases in `logo_test.go`; migration
      `0027_bimi_publication`; the upload and the public address in
      `internal/api/v1api/apimedia/logo.go`; the DNS row now offers the real
      address. Checked against four real published marks — all four pass — and
      end to end on the dev server: a valid file is stored and served with the
      right headers, one with a script is refused naming the element.
- [x] (2026-09-08 20:50Z) Milestone 3 — verify it the way the other records
      are verified. The row resolves the record, reads or fetches the logo it
      names, and validates it; the three failures are reported separately. A
      logo this server hosts is read from storage rather than fetched, which
      `internal/dns/bimi_logo_test.go` pins down.
- [x] (2026-09-08 21:35Z) Milestone 4 — say what a certificate is for, and
      show the preview. The domain's page has a card with the uploaded mark
      drawn at both the sizes a reader sees it, what it is called, and a
      paragraph naming the certificate, who requires one and roughly what it
      costs.
- [x] (2026-09-08 21:50Z) Milestone 5 — changelog written; `make lint-ci` and
      the full Go suite pass; the layout audit is clean at 1400 and 390 (the
      one small target it reports is the breadcrumb link, which every page
      has).
- [x] (2026-09-08 22:10Z) A careful UX pass over the card and the DNS row,
      which found four things: the row offered a copyable value containing
      `<upload a logo first>`, the card said "Published" beside a table saying
      "nothing published" about the same domain, the certificate warning was
      the fourth sentence of a paragraph that also explained what an
      acceptable file is, and — the reason the audit paid for itself —
      `publishedLogo` dereferenced a database the DNS tests do not provide, so
      every test in `internal/dns` panicked and the Server check was red.
- [x] (2026-09-08 22:40Z) Milestone 6 — `DeleteDomainLogo`, so a mark can be
      withdrawn; `teanode domain logo show|publish|remove` with an `Upload` on
      the client, the first thing in this program to send a file; `teanode
      mailbox subscription list|show|mail|unsubscribe`; `token revoke --user`.
      `domain check` prints the blocked note, which only the dashboard said.
- [x] (2026-09-08 23:10Z) A review pass, which found that the logo upload
      asked only for a session while removing the same logo asked for
      `domain:manage`. The picture upload beside it had the same hole. Both
      ask properly now, and `internal/api/v1api/apimedia/authorize_test.go`
      reads this package's own source so the next one fails a test — it found
      the picture upload on the day it was written. Also: `ListDomains` asked
      the database once per domain and did it before the permission filter;
      `DeleteDomainLogo` opened a second transaction beside the request's own.
- [x] (2026-09-08 23:40Z) The address the record names is the domain's own
      picture host rather than this node's name, and optional records no
      longer count as missing on the domain list or a domain's overview.
      Rebased on v0.14.0, with the changelog entries put back under Unreleased
      after the release absorbed them.

## Surprises & Discoveries

- Observation: the media store refuses SVG on purpose, and its comment says
  why: "an SVG is a document that can carry script, and refusing one format is
  cheaper than being sure about sanitizing it"
  (`internal/api/v1api/apimedia/apimedia.go`). A BIMI logo is an SVG that has
  to be served, so this plan cannot reuse that store — and it must answer the
  question that comment declined to answer.
  Consequence: the answer is that BIMI's own profile is the sanitiser. SVG
  Tiny Portable/Secure forbids script, event handlers, external references,
  embedded raster images and animation. A file that passes the check the
  specification requires is, by construction, a file with nothing in it that
  runs. The validation is not a nicety on top of the feature; it is what makes
  hosting the file safe.

- Observation: four marks published by well-known senders all pass the
  validator unchanged, which is the evidence that these rules match what is
  actually published rather than being stricter than reality. Two of the four
  use the literal title "bimi-svg-tiny-12-ps", so the title a generator writes
  is not always meaningful — worth knowing before showing it to anybody as the
  mark's name.

- Observation: a self-hosted logo cannot be verified by fetching it. The
  fetch goes through the guard that refuses anything but a public address, and
  a great many of these servers answer to a name that resolves inside the
  network they run in — a dev box certainly does. Checking a perfectly correct
  record would then say "not a public address" and send the operator looking
  for a fault that is not there. A logo this server hosts is read out of
  storage instead, and the DNS package is given a function to do that with so
  it needs to know nothing about where files are kept.

- Observation: the record set is computed on a schedule and cached, so a logo
  uploaded now does not appear in the row until the next sweep — thirty
  minutes by default. The page has to ask for a re-check after an upload, or
  the operator uploads a file and the row still tells them to upload one.

- Observation: the logo does not have to live on the domain it belongs to. The
  `l=` tag is any HTTPS URL, so a domain with no web server at all can point
  at one on this mail host, which already has a certificate for its own name.
  That removes the hardest step for the operator this feature is for.

- Observation: the guarantee that every GraphQL resolver authorizes is a test
  that reads the package's own source, and routes outside `apigraph` are
  invisible to it. Implication: the logo upload shipped asking only that the
  caller was signed in, while removing the same logo asked for
  `domain:manage` — anybody with a mailbox here could have replaced any
  domain's published mark. The picture upload beside it had had the same hole
  for longer. Both fixed, and `apimedia` now has the equivalent test, which
  caught the second one immediately. A guarantee that lives in a test only
  covers what the test can see.

- Observation: the record was published as `https://mx1.teanode.com/...`,
  which verified, because that name really does serve the file. Implication:
  it is still wrong. `mx1` is one machine in a pair and the address goes into
  DNS, where it has to keep meaning the same thing after that machine is
  replaced. `LinkHostFor` already existed for exactly this and is documented
  as "whatever else a recipient's program later fetches". Both names are
  accepted as ours when reading a logo back, so records written before this
  keep verifying.

- Observation: the domain list counted optional records as missing, and its
  query did not ask for the `optional` field at all. Implication: the obvious
  one-line fix compiles, typechecks, and changes nothing, because
  `record.optional` is `undefined` on data that never carried it. This was
  true of AAAA records for as long as there have been optional ones; the BIMI
  row made it visible on every domain at once.

- Observation: a release on main absorbs whatever sits under `[Unreleased]`,
  and a rebase over it reports success while leaving this branch's entries
  under the released version — the second time in this branch's life.
  Implication: the `Changelog entry` check passes either way, since it
  verifies that an entry exists rather than which heading it is under. Rebuild
  the section from main's file plus this branch's own entries and diff the
  released half to prove it is untouched.

## Decision Log

- Decision: the SVG is validated against the Portable/Secure profile at upload
  and refused if it fails, rather than sanitised into shape.
  Rationale: the receiver will refuse it anyway, so accepting it would be
  storing a file that cannot work; and refusing is the same check that makes
  it safe to serve from this server's own origin. Telling somebody which
  element is wrong is more useful than silently rewriting their artwork.
  Date/Author: 2026-09-08, this plan.

- Decision: the DMARC prerequisite is stated on the row whether or not a logo
  is configured, and the row is optional so an unpublished BIMI record never
  makes a domain read as misconfigured.
  Rationale: most operators will never publish a logo, and a red mark for
  something nobody needs teaches people to ignore the colour — which is the
  reasoning already written into `Record.Optional` for AAAA records.
  Date/Author: 2026-09-08, this plan.

- Decision: say what a Verified Mark Certificate costs and who requires one,
  in the interface, before the operator starts.
  Rationale: it is the difference between "this works" and "this works at
  every receiver except the two that matter most to you". Learning it after
  buying an SVG from a designer is worse than learning it first.
  Date/Author: 2026-09-08, this plan.

## Surprises, continued

- Observation: the dashboard cannot draw its own published logo from the
  public address in development. The dev server serves the dashboard's files
  and answers `/.well-known/...` itself before the proxy sees it, whatever the
  proxy is configured with.
  Consequence: the page reads the file through the API instead, which is the
  better split anyway — the public address exists for receiving mail systems
  and the dashboard is the operator's own, behind their session. The domain
  view carries both addresses and says which is which.

## Outcomes & Retrospective

The five things offered are all there: the row that says what is stopping a
domain, hosting for the file, validation that names what is wrong with it,
verification of the whole chain rather than the record's existence, and the
sentence about the certificate.

What went well: the validator was written before anything that used it, and
checking it against four marks published by well-known senders — all four pass
unchanged — is the evidence that these rules match what is really published
rather than being an invention. The same check turned out to be what makes
hosting the file safe, which resolved a question the media store had
deliberately declined to answer.

What did not go to plan: two of the three interesting problems were only
visible once it ran. A self-hosted logo cannot be verified by fetching it,
because the fetch refuses addresses that are not public and these servers
routinely answer to names that resolve inside their own network. And the
dashboard cannot draw its own logo from the public address in development,
because the dev server answers that path itself. Both are recorded above; both
changed the design rather than being worked around.

What remains: nothing verifies the certificate a record names, and the
interface is careful never to say "verified". The `a=` tag is not offered as a
field — an operator with a certificate has to write the record by hand, which
is the right trade until somebody actually has one.

Added after the five: a mark can be withdrawn, which it could not be when the
five were done — `DeleteBimiPublication` had been written and never called, so
a logo could be replaced but never taken down except by editing the database.
Both features reach the command line as commands rather than only through
`api call`, which needed the client's first file upload.

What the later passes are worth recording for: the UX audit found a panicking
test suite, and the review found a permission hole in code written an hour
earlier. Neither was going to be found by reading the diff again — one came
from running the thing at two window widths, the other from asking what each
route checks and writing the answer down as a test. The question that started
both was somebody asking whether it had been done, not a checklist.

Proved end to end on the live server rather than argued: `teanode.com`
publishes a mark, and the chain a receiver walks — record read through public
DNS, file fetched over HTTPS, bytes validated against the profile — passes
from outside the network. No certificate, so Gmail and Yahoo will not draw it,
which is the honest limit the card states. The mark itself is the full
lockup and is a green block at 20px; that is artwork rather than code, and is
the one thing this work leaves undone.

## Context and Orientation

This assumes no prior knowledge of the repository. `AGENTS.md` at the root
describes what the program is.

**Where DNS advice comes from.** `internal/dns/record_set.go` builds, for one
domain, the list of records that should exist: MX, the A and AAAA of the mail
host, SPF, the DKIM key, and DMARC. Each is a `Record` with `Type`, `Name`,
`Expected` (what to publish), `Found` (what is published now), `Verified`, and
`Optional` — the last for records that are worth having but whose absence is
not a fault. The dashboard renders this on a domain's DNS tab, with a copy
button per record, from `web/src/pages/domainDns.tsx`. Adding advice means
adding a `Record` to that list; the page needs no change to display it.

**What already exists for BIMI.** `internal/bimi` reads what a *sending*
domain publishes: `Parse` reads a record's value, `Lookup` asks DNS for
`<selector>._bimi.<domain>` and falls back to the organizational domain,
`SelectorFrom` reads the selector a message names. The dashboard shows the
result through `web/src/components/senderLogo.tsx`, which draws the logo when
there is one and a coloured monogram otherwise. All of that is for other
people's domains. Nothing yet helps with one's own.

**Storing a file.** `internal/storage` has a `Files` interface —
`PutFile(ctx, id, content)`, `GetFile`, `DeleteFile` — used today by the media
store for pictures in templates. The media store itself
(`internal/api/v1api/apimedia/`) uploads behind a session, decides the content
type by reading the bytes, refuses anything not on an allow list of raster
formats, and serves the result at `/media/{mediaId}`, which is public because
mail programs have no session.

**Terms.**

*BIMI* — Brand Indicators for Message Identification. A TXT record at
`default._bimi.<domain>` naming a logo:

    v=BIMI1; l=https://mail.example.com/.well-known/bimi/abc.svg; a=https://example.com/vmc.pem

*Selector* — which record to read. `default` unless a message says otherwise
with a `BIMI-Selector` header. This plan publishes only `default`, because a
sender with one logo has no use for more and the extra control would be a
setting nobody touches.

*SVG Tiny Portable/Secure* — the restricted SVG a BIMI logo must be. In
practice: the root element carries `baseProfile="tiny-ps"` and `version="1.2"`,
there is a `<title>`, the viewBox is square, and the file contains no
`<script>`, no `on*` event attributes, no `<animate>` or similar, no
`<foreignObject>`, no `<image>`, no external references, and no hyperlinks.

*VMC* — Verified Mark Certificate. A certificate issued by one of a handful of
authorities after checking that the sender owns the trademark in the logo.
Gmail and Yahoo require one before they will display anything. It is renewed
annually and costs on the order of a thousand dollars. A *CMC* (Common Mark
Certificate) is a cheaper variant for marks that are not registered
trademarks, accepted by fewer receivers.

*Organizational domain* — the registrable domain above a subdomain:
`example.com` for `mail.example.com`. DMARC and BIMI are both discovered by
asking the name itself and then the organizational domain above it.

## Plan of Work

### Milestone 1 — the BIMI row, and the DMARC prerequisite said plainly

At the end of this milestone a domain's DNS tab has a BIMI row that says what
is published, what is stopping it, and what to publish. Nothing can be
uploaded yet.

In `internal/dns/record_set.go`, after the DMARC record is appended, add a
`Record` for BIMI: type TXT, name `default._bimi.<domain>.`, `Optional` true.
Resolve it with the existing `resolveTxt` and mark it verified when what is
published parses as a BIMI record with a logo — reuse `bimi.Parse` rather than
writing a second parser.

`Expected` is the record to publish, and depends on whether a logo has been
uploaded (Milestone 2). Until then it is the shape with the address left to
fill in, which is honest and still useful to copy.

The prerequisite is the point of this milestone. The DMARC record's own value
was resolved a few lines above; read its policy, and when it is `p=none` or
absent, say so in the record's `Purpose` — the field the page already renders
under the value. Something like: "shows your logo beside your mail at
receivers that support it; needs your DMARC policy at quarantine or reject
first, and it is none". Add a field to `Record` if `Purpose` cannot carry it
without reading badly; a `Blocked string` that the page renders as a notice
under the row is the better shape, and the page change is small.

Acceptance: on the dev server, a domain with `p=none` shows the BIMI row with
the prerequisite spelled out, and no red mark, because the record is optional.
Change the domain's DMARC to `p=reject` in DNS and the notice goes.

### Milestone 2 — upload a logo, validated, and host it

At the end of this milestone the operator can upload an SVG on the domain's
page, be told exactly what is wrong with it if anything is, and get back an
HTTPS address that this server serves.

Write the validator first, in a new file `internal/bimi/logo.go`, exporting
`ValidateLogo(content []byte) error` and a `Logo` describing what was found
(title, viewBox). Check, in this order, reporting the first failure with the
element that caused it: the file parses as XML; the root is `<svg>`; it
declares `baseProfile="tiny-ps"`; it has a `<title>`; the viewBox is present
and square; and it contains none of `script`, `foreignObject`, `image`,
`animate`, `animateTransform`, `animateMotion`, `set`, `a`, or any attribute
beginning `on`. Reject external references: any `href`, `xlink:href` or `url(`
pointing anywhere but inside the document. Cap the size at 32KB, which is what
the specification asks for and is generous for a mark.

Write `internal/bimi/logo_test.go` alongside: a minimal valid logo passes;
each rule has a case that fails with a message naming the problem; and a file
with a `<script>` element fails even when everything else is right.

Store it as a new table `bimi_publication` — migration `0027` — keyed by
domain, holding the media identifier, the filename, when it was uploaded, and
the title read out of the file. The bytes go in `storage.Files` under that
identifier, the way media does.

Serve it at `PathBimiLogo = "/.well-known/bimi/{id}.svg"`, public and outside
the API prefix for the same reason media files are: the fetcher is a receiving
mail system with no session. Answer with `image/svg+xml`,
`X-Content-Type-Options: nosniff`, a long `Cache-Control`, and the same
content security policy the sender-logo endpoint uses —
`default-src 'none'; style-src 'unsafe-inline'; sandbox` — because defence in
depth is cheap and the file came from a person who might have been given it by
somebody else.

The mutation is `PublishBimiLogo(domainId, filename, content)` beside the
other domain mutations, requiring `PermissionDomainManage`, and
`RemoveBimiLogo(domainId)`. Uploading goes through a POST endpoint rather than
GraphQL, the way media does, because it carries a file.

Acceptance: upload a valid SVG, fetch the address it returns, and get the file
back with the right type. Upload one with a `<script>` element and be refused
with a message naming it.

### Milestone 3 — verify it the way the other records are verified

At the end of this milestone the BIMI row is green only when the whole chain
works, not merely when a TXT record exists.

Extend the check in Milestone 1: after the record parses, fetch the logo it
names through `internal/util/safefetch` — the same guard the image proxy and
the sender-logo fetcher use — and run `ValidateLogo` over what comes back.
Report the three failures separately, because they need different actions: the
record is missing, the logo cannot be fetched, or the logo is not a valid one.
Keep the fetch off the page's own path: the record set is already computed on
a schedule and cached, and `RecordSet.CheckedAt` says when.

Acceptance: publish the record with a good logo and the row verifies. Point
`l=` at a 404 and the row says the logo could not be fetched, naming the
status. Point it at a PNG and the row says it is not an SVG.

### Milestone 4 — say what a certificate is for, and show the preview

At the end of this milestone nobody starts this without knowing what it will
cost them, and everybody can see what they are publishing.

On the domain's page, beside the uploaded logo, show it drawn the way a
receiver would draw it — reuse `SenderLogo` at the size the mailbox list uses
and at the size the message header uses, so the operator sees both.

Under it, a short paragraph in the three catalogues: a logo published without
a certificate is shown by some receivers and not by Gmail or Yahoo, which
require a Verified Mark Certificate; that a VMC is issued against a registered
trademark by a handful of authorities, renewed annually, and costs on the
order of a thousand dollars; and that the `a=` tag holds its address once
there is one. Give the record an `a=` field to fill in, empty by default.

No wizard, and no claim that following these steps produces a logo in Gmail.
The page's job is to get the record and the file right; what a receiver does
with them is the receiver's.

Acceptance: the preview shows the uploaded mark; the paragraph is in all three
catalogues and `make lint-ci` passes.

### Milestone 5 — documentation, changelog, deployment

A `CHANGELOG.md` entry under Unreleased in the house style: what an operator
can now do, and the honest limit. A paragraph in the domain documentation if
there is a natural place for it. Then `make lint-ci`, `make test`, and the
deployment to the development server.

### Milestone 6 — a logo can be withdrawn, and both features reach the CLI

Found by asking whether the command line covers the two features shipped this
week. It covers subscriptions through `teanode api call`, since those are
schema operations; it does not cover a logo at all, because uploading one is a
multipart request rather than a schema operation. Asking that question turned
up something worse than a missing command.

**A published logo cannot be withdrawn.** `DeleteBimiPublication` was written
and never called — not by the dashboard, not by the API, not by the CLI. A
mark can be replaced but never taken down, so an operator who publishes the
wrong artwork, or who stops using a domain, has no way out but an edit to the
database. The record keeps pointing at a file this server keeps serving.

Four pieces, smallest first:

1. `DeleteDomainLogo(domainId)` as a mutation. The graph already holds
   `storage`, so it can drop the row and the bytes together, in that order —
   the same order the upload uses in reverse, since a row pointing at bytes
   that are gone answers 404 for ever while bytes with no row cost only space.
   Being a schema operation, the CLI reaches it through `api call` the moment
   it exists, and a Remove button on the logo card gives the dashboard the
   same.

2. `token revoke --user`. `DeleteTokenArguments` has no `Username`, so the
   resolver passes nil to `owner`, which refuses on the console with "say whose
   this is with --user" — a flag `revoke` does not define. `create` and `list`
   both take it. The asymmetry makes revocation impossible from the server's
   own console, which is exactly where somebody who has lost their token is
   standing.

3. `teanode mailbox subscription`: list, show, unsubscribe. Wiring over the
   four operations that already exist, shaped like the `folder` and `rule`
   groups beside it.

4. `teanode domain logo`: show, publish, remove. `publish` needs the one new
   thing here — an `Upload` on the client, mirroring the `Download` that
   already fetches a stored message. Nothing in the CLI sends a file today.

Acceptance: `DeleteBimiPublication` has a caller; a logo published in the
dashboard can be removed there and from the command line; `teanode token
revoke --user ziyan <id>` works on the console; the two new command groups
appear in `--help` and are documented in `command-line.md`.

## Validation

From the repository root: `make test` for the Go tests, which needs Docker;
`make lint-ci`, which must print `0 issues.`; `make dev` for a development
server on `http://127.0.0.1:10000`.

For the DNS side, the dev domain's records can be inspected with `dig`:

    dig +short TXT default._bimi.example.com

For the layout, load the domain's DNS tab in a frame at 1400 and 390 pixels
and check the same things the dashboard is always checked for: no sideways
scroll, nothing wider than the window outside a declared scroller, no row
shorter than its content, no native `title` tooltips, no native `select`, and
nothing under 24 pixels to tap on a phone.

## Risks and what to do about them

An operator uploads a logo they do not own the rights to. Nothing here can
tell, and the certificate authorities exist precisely because somebody has to
check. The interface should not imply that publishing a mark grants any right
to it.

The validator is stricter than some receiver, and refuses a file that would
have worked. That is the safer direction — the file is being served from this
server's origin — and the message says which rule refused it, so an operator
can fix the file rather than guess.

A logo published here is fetched by receiving mail systems, which means a URL
on this server is visited by strangers. It is a static file with no identifier
in it beyond the domain's own, so there is nothing to leak; but the route must
not log more than the ordinary access log does.
