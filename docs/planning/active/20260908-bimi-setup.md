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
- [ ] Milestone 3 — verify it the way the other records are verified.
- [ ] Milestone 4 — say what a certificate is for, and show the preview.
- [ ] Milestone 5 — documentation, changelog, deployment.

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

- Observation: the record set is computed on a schedule and cached, so a logo
  uploaded now does not appear in the row until the next sweep — thirty
  minutes by default. The page has to ask for a re-check after an upload, or
  the operator uploads a file and the row still tells them to upload one.

- Observation: the logo does not have to live on the domain it belongs to. The
  `l=` tag is any HTTPS URL, so a domain with no web server at all can point
  at one on this mail host, which already has a certificate for its own name.
  That removes the hardest step for the operator this feature is for.

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

## Outcomes & Retrospective

To be written at completion.

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
