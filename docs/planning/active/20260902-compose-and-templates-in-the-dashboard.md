# Write and send mail from the dashboard, and edit its templates and layouts there

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. It is maintained in accordance with the ExecPlan
requirements in `~/.claude/PLAN.md`, which is not checked into this repository;
everything needed to follow this plan is contained in this file.

## Purpose / Big Picture

TeaNode already stores mail templates and layouts in PostgreSQL, renders them
with the pongo2 template language, and sends the result when an application
calls `POST /api/v1/send/{domain}/{template}` with an SMTP credential. None of
that is reachable from the dashboard: there is no page that lists a domain's
templates, no way to write one without the command line client, and no way for
the person running the server to send a message at all without setting up a
mail client.

After this change an operator signed in to the dashboard can:

- open a domain, go to its **Templates** page, and see every template and
  layout the domain has, create new ones, edit their subject, HTML and text,
  choose which layout a template sits in, and see a live preview rendered by
  the server with sample values for the variables the template uses;
- open **Compose** from the rail, pick one of their domains and an address at
  it to send as, and either choose a template and fill in its variables, or
  write a message from scratch in a rich text editor (bold, italics, lists,
  links, headings) or as plain text, attach files, and send it. The message
  is signed with the domain's DKIM key, recorded under Mail like any other
  outgoing message, and the page links to it once it has gone.

To see it working: run a development server (`make dev`), sign in, add a
domain, open `/compose`, write a message to an address at `example.net`, and
send it. The message appears under Mail as an outgoing message with an
external delivery. With `TEANODE_SMTP_DISABLE_SEND=true`, which the
development environment sets, the delivery is marked delivered without leaving
the machine.

Templates and layouts also become multilingual. A template keeps one name and
one place in the domain, and carries its subject and content in as many
locales as the operator writes: a default, and a translation per further
locale. Whoever sends — the dashboard, an application calling the send
endpoint, or the command line client through the API — names the locale the
recipient should read, and the server picks the closest translation the
template has, falling back to the default. Nothing changes for a template
with no translations.

## Progress

- [x] (2026-09-03 00:30Z) Read the code paths involved: `internal/mailer`,
      `internal/mx/exchange_outgoing.go`, `internal/util/mailparse`,
      `internal/util/templating`, `internal/api/v1api/apigraph`, and the
      dashboard under `web/src`. Wrote this plan.
- [x] (2026-09-03 00:45Z) Milestone 1 — message assembly and variable
      discovery on the server: `mailparse.Compose`, `templating.Variables`,
      `mailer.Render` and `mailer.Send`, with tests.
- [x] (2026-09-03 00:45Z) Milestone 2 — translations on the server: migration
      `0006_translation`, the models, storage with a round-trip test against
      PostgreSQL, `templating.MatchLocale`, and the mailer choosing a
      translation. `apisend` reads `locale`.
- [x] (2026-09-03 00:55Z) Milestone 2b — the API: `Template.variables`,
      translations on the template and layout types and parameters, a
      `locale` argument on `RenderTemplate`, `RenderLayout`, `SendMail` and
      the send endpoint. A test builds the schema by reflection.
- [x] (2026-09-03 01:20Z) Milestone 3 — the dashboard: a domain's templates
      page, the template and layout editors with locale tabs and a
      server-rendered preview.
- [x] (2026-09-03 01:20Z) Milestone 4 — the dashboard: the Compose page,
      rich text editor with a derived text alternative, attachments.
- [x] (2026-09-03 01:40Z) Milestone 5 — translations into zh and ja, changelog,
      decision record, reference docs; every dashboard query validated
      against a running server; the API exercised end to end (below).
      Found and fixed on the way: the send endpoint was unreachable behind
      the authentication middleware.
- [x] (2026-09-03 01:55Z) Walked through every page in a browser signed in
      as a throwaway account on the worktree's own server, in both themes.
      Cleaned up what that showed: Compose moved under Mail as
      `/mail/compose` with a New message action on the Mail page and no rail
      entry (the owner's call); the editors and the compose form sit in
      cards like every other form; the toolbar uses drawn icons rather than
      glyphs; the domain is chosen as the half of the address after the @;
      long template lines wrap; the page scrolls to the notice after
      sending. Sent a message written by hand and one from a template from
      the page, and opened both under Mail.
- [x] (2026-09-03 01:55Z) Tested with a real template: the cue sign-in mail
      (a table-based shell with a preheader, a code box and a button, with
      English, Chinese and Japanese wording) ported to a layout with three
      blocks and a template with two translations. Variables `code`,
      `link`, `logoUrl` and `productName` were detected; the preview and the
      stored message render it as the original does, in each language.

## Surprises & Discoveries

- Observation: the mailer always produced a `multipart/alternative` with both a
  text and an HTML part, even when the template's HTML was empty, and never
  set `MIME-Version`.
  Evidence: `mailparse.CombineParts` in `internal/util/mailparse/multipart.go`
  writes both parts unconditionally; the header list in `mailer.SendMail` has
  no `MIME-Version`.

- Observation: `POST /api/v1/send/{domain}/{template}` answered 401 "not
  logged in" on any server with an account. The authentication middleware
  refused every `/api/` path without a session except the GraphQL endpoint,
  and the send endpoint's credential check lives inside its handler, which
  was never reached. The test asserting that path was refused was a leftover
  from the login refactor, mislabelled "the login endpoint is reachable".
  Evidence: `curl -u <credential> .../api/v1/send/example.com/welcome` gave
  `{"error":"not logged in"}` with a valid credential; after letting the path
  through by prefix, 200 and the stored message.

- Observation: `web/.prettierrc.json` says `jsxSingleQuote: true` but the
  source uses double quotes in JSX throughout, so running prettier as
  configured rewrites every file it touches. The new files were formatted
  with `--jsx-single-quote=false` to match the code rather than the config.
  `npm run lint` fails before reading any file because `eslint-config-prettier`
  is not installed; the rules were run with a copy of the config without that
  line, and the only complaint is a pre-existing one in `useQuery.ts`.

- Observation: `make dev` reuses the main checkout's development database
  and its data directory, and a server from that checkout was already
  running on the ports, so the worktree's server was run against a database
  of its own (`teanode_compose` in the same PostgreSQL) on ports 20025,
  20587 and 20081. Migration `0006_translation` was applied to the shared
  development database on the first attempt; the older binary reverts it on
  its next start, which is what the migration runner is for.

## Decision Log

- Decision: sending from the dashboard goes through the same `mx` outgoing
  path a credential's submission uses, with `Envelope.DomainID` set and no
  credential.
  Rationale: `exchange.HandleEnvelope` already treats an envelope with a
  domain and no credential as outgoing mail from that domain, signs it,
  records it and queues deliveries. The `mailer` already uses this for
  templated sends. Building a second path would duplicate the signing and
  recording and drift from it.
  Date/Author: 2026-09-03, Claude.

- Decision: one `mailer.Send` that takes a `Message` (addresses, subject,
  text, HTML, attachments); the templated `SendMail` renders and then calls
  it. Rendering is a package-level `mailer.Render` so the API can preview
  without sending.
  Rationale: the compose page and the send endpoint must produce identical
  messages for identical input. One assembly path is how that stays true.
  Date/Author: 2026-09-03, Claude.

- Decision: the variables a template uses are discovered on the server by
  scanning the template source (`templating.Variables`) and returned on the
  `Template` type as `variables`, including those of the template's layout.
  Rationale: the dashboard needs the list to draw one input per variable, and
  so does anyone calling the send endpoint from a script. Doing it in the
  server means one implementation, and one that the command line client
  benefits from too. It is a heuristic over the source, not the parser's own
  answer, because pongo2 does not expose its syntax tree; the plan says
  exactly what the heuristic does so the limits are known.
  Date/Author: 2026-09-03, Claude.

- Decision: a template is one row with one name, and its translations are
  rows in a `template_translation` table keyed by locale; likewise
  `layout_translation`. The template's own subject and content are the
  default, with an optional `locale` column saying which language that is.
  Rationale: the alternative — one template row per locale, with the name
  shared — makes the name no longer identify a template, breaks the unique
  index on (domain, name), and leaves a template's layout reference pointing
  at a layout in one language. Translations as children keep the identity a
  caller sends by, keep the layout reference meaning "this layout, in
  whatever language the message is in", and let a layout be translated
  independently of the templates in it. The request was for the sender to
  choose the locale, and this is the shape in which the choice is one
  argument rather than a different template name per language.
  Date/Author: 2026-09-03, Claude, after the owner asked for locale support.

- Decision: locale matching is exact first, then by primary language, then
  the default. `zh-CN` finds a `zh-CN` translation, else a `zh` one, else
  any `zh-*`, else the default. Case and the `_` separator are ignored.
  Rationale: this is the ordinary shape of language negotiation and needs
  no table of languages. Anything cleverer — script inference, region
  fallbacks between `pt-BR` and `pt-PT` — is a decision about the operator's
  content that the operator should make by writing the translation.
  Date/Author: 2026-09-03, Claude.

- Decision: the rich text editor is a `contentEditable` element with a small
  toolbar driving `document.execCommand`, and the plain text alternative is
  derived from the HTML in the browser.
  Rationale: the dashboard deliberately ships no dependency it can avoid (see
  the comment at the top of `web/src/api.ts`). `execCommand` is deprecated on
  paper and supported by every browser in practice; the formatting needed for
  a message — bold, italic, lists, links, headings — is exactly what it does.
  A text alternative is derived in the browser so the reader can see it on the
  Text tab before sending.
  Date/Author: 2026-09-03, Claude.

- Decision: attachments travel to the server as base64 inside the GraphQL
  request, using the existing `Data` scalar, and their total is capped at
  the configured `smtp.maxMessageSize`.
  Rationale: the dashboard already speaks only GraphQL, and a separate upload
  endpoint would be a second protocol for one field. The cap is the same
  limit the SMTP listener applies, so what can be sent from the dashboard is
  what could be sent from a mail client.
  Date/Author: 2026-09-03, Claude.

- Decision: Compose is a page under Mail, at `/mail/compose`, reached from
  a New message action on the Mail page, with no entry of its own on the
  rail. The domain is chosen as the part of the sender's address after the
  @ rather than by a separate control.
  Rationale: the owner asked for it under Mail: writing a message is part of
  the Mail activity, and a rail entry for something done occasionally is a
  row read on every visit. A template's page links to it with the domain
  and template already chosen. (Superseded an earlier decision to put it on
  the rail with a domain picker.)
  Date/Author: 2026-09-03, Claude, at the owner's request.

## Outcomes & Retrospective

Against the purpose: an operator can list, create, edit, translate and
preview templates and layouts per domain, and can send a message from the
dashboard as any address at a domain, from a template in a chosen locale or
written by hand in rich or plain text, with attachments. The server side is
covered by unit tests and by the end-to-end run in Artifacts and Notes; the
dashboard is type-checked, linted, built, and its queries validated against
the server, but has not been driven in a browser.

What it cost: a migration, two child tables, and a mailer that is now two
functions instead of one. What was found on the way was worth the trip: the
send endpoint had been unreachable since logging in moved into GraphQL, and
the messages the server composed were missing `MIME-Version` and writing
base64 on one line.

Deferred, deliberately: recording which operator sent a message from the
dashboard (the `Mail` row has no field for it); inline images pasted into
the rich text editor (they go out as `data:` URLs, which some clients do not
show); the send endpoint accepting a hand-written body rather than only a
template. Each is a small, separate change.

## Context and Orientation

TeaNode is a Go mail server with a React dashboard compiled into the binary.
The pieces this plan touches:

- `internal/util/mailparse` — splitting and building messages. `CombineParts`
  writes a `multipart/alternative` body from a text and an HTML reader;
  `TraverseParts` and `PartAt` walk a stored message's MIME tree.
- `internal/util/templating` — `Render(writer, variables, templates...)`
  renders pongo2 templates. When more than one template is given, each
  extends the one before it: the first is the layout, the last the content.
  A layout declares `{% block name %}…{% endblock %}` and a template fills
  those blocks. Variables are written `{{ name }}`.
- `internal/mailer` — `Mailer.SendMail(ctx, envelope, templateName,
  variables)` looks up the template and layout for the sender's domain,
  renders them, inlines the CSS, builds the message and hands it to
  `mx.Exchange.HandleEnvelope`.
- `internal/mx/exchange_outgoing.go` — the outgoing path. An envelope with a
  `CredentialID` or a `DomainID` is outgoing: the domain is looked up, the
  message is DKIM-signed with its key, recorded as a `Mail` of kind
  `outgoing`, checked for unsafe attachments, and a delivery is created per
  recipient.
- `internal/models/template.go`, `internal/models/layout.go` — the structs
  the API returns. `Template` has `LayoutID`, `Name`, `Comment`, `Subject`,
  `HTMLContent`, `TextContent`; `Layout` has `Comment`, `HTMLContent`,
  `TextContent`. Neither knows anything about language: a template is one
  subject and one body, in whatever language it was written.
- `internal/db/database_template.go`, `database_layout.go` — storage. The
  tables are created by `internal/db/migrations/0000_initial.sql`;
  `template.layout_id` is a foreign key to `layout` with `ON DELETE SET
  NULL`. A migration is a pair of files, forward and reverse; see
  `docs/coding/database-migrations.md`.
- `internal/api/v1api/apigraph` — the GraphQL API. Resolvers are exported
  methods on `*graph`; `internal/util/graphapi` builds the schema by
  reflection over the `Query` and `Mutation` interfaces in `schema.go`.
  `template.go` and `layout.go` already have list, get, create, modify and
  delete. Every resolver must call `requireOperator` or `requireDomain`;
  `authorize_test.go` reads the source and fails otherwise. Input structs
  are converted to GraphQL input objects; a `[]byte` field becomes the
  `Data` scalar (base64 in JSON), a `map[string]interface{}` becomes `Any`.
  A field tagged `graphapi:"nullable"` is optional.
- `web/src` — the dashboard. `api.ts` has one `graphql()` fetch wrapper and
  the TypeScript shapes. Pages are under `pages/`, routed in `app.tsx`.
  Strings come from `i18n/en.ts`, `zh.ts` and `ja.ts`; a key added to
  English must be added to both others or the build fails, and
  `web/scripts/check-catalogs.mjs` refuses a translation identical to the
  English. `web/scripts/check-queries.mjs` validates every GraphQL string in
  the source against a running server. `components/messageFrame.tsx` renders
  HTML in a sandboxed iframe with no scripts; `components/dialog.tsx` has
  `FormDialog` and `ConfirmDialog`; `components/settingsList.tsx` has the
  section and row layout used by the settings pages.

Terms used below: a *layout* is the outer template a message's content is
placed into (header, footer, styling); a *template* is one kind of message
(subject and content) that optionally sits in a layout; a *variable* is a
named value substituted into either when rendering; a *locale* is a language
tag as written in `Accept-Language` or `Content-Language`, such as `en`,
`zh-CN` or `pt-BR`, and a *translation* is a template's or layout's content
in one locale.

## Plan of Work

### Milestone 1 — message assembly and variable discovery

In `internal/util/mailparse/multipart.go`, replace `CombineParts` with:

    type Attachment struct {
        Filename    string
        ContentType string
        Content     []byte
    }

    // Compose writes a message body from its parts and returns the headers
    // that describe it.
    func Compose(writer io.Writer, text, html []byte, attachments []*Attachment) ([]string, error)

The body is: the text part alone when there is no HTML; the HTML part alone
when there is no text; `multipart/alternative` of both when both are present;
and when there are attachments, `multipart/mixed` whose first part is the
above and whose remaining parts are the attachments, base64 encoded, with
`Content-Disposition: attachment; filename="…"`. The returned headers are
`MIME-Version: 1.0` and the top-level `Content-Type`. Returns an error when
text, HTML and attachments are all empty. Test by composing and then walking
the result with `TraverseParts` and `PartAt`.

In `internal/util/templating/templating.go`, add:

    // Variables reports the names a set of templates reads from their
    // context, sorted, without duplicates.
    func Variables(templates ...string) []string

Implementation: find every `{{ … }}` and `{% … %}` block; strip string
literals; split into identifier tokens; keep the first segment of a dotted
path (`user.name` reads `user`); skip the token after a `|` (a filter name)
and everything after a `:` inside a filter; skip pongo2 keywords (`if`,
`elif`, `else`, `endif`, `for`, `in`, `endfor`, `block`, `endblock`,
`extends`, `include`, `set`, `with`, `endwith`, `macro`, `endmacro`, `import`,
`from`, `as`, `not`, `and`, `or`, `is`, `true`, `false`, `none`, in any case,
and the block tag names `comment`, `raw`, `autoescape`, `filter`, `spaceless`,
`firstof`, `cycle`, `ifchanged`, `lorem`, `now`, `templatetag`, `widthratio`,
`ssi`, `empty` with their `end…` forms); skip `forloop`; and treat as defined
rather than read: the loop variables between `for` and `in`, the name after
`block`, the name after `set` or `macro`, and the names assigned in `with`.

In `internal/mailer/mailer.go`:

    type Message struct {
        From        string   // the address to send as, at a configured domain
        FromName    string   // display name, optional
        To, Cc, Bcc []string
        Subject     string
        Text, HTML  string
        Attachments []*mailparse.Attachment
    }

    type Rendered struct{ Subject, HTML, Text string }

    func Render(template *models.Template, layout *models.Layout, variables map[string]interface{}) (*Rendered, error)

    type Mailer interface {
        Close() error
        Send(ctx context.Context, envelope *mailparse.Envelope, message *Message) error
        SendMail(ctx context.Context, envelope *mailparse.Envelope, templateName string, variables map[string]interface{}) error
    }

`Render` renders subject, HTML and text with `templating.Render`, then inlines
CSS in the HTML as `SendMail` does today. `Send` validates the sender is at a
configured domain, sets `envelope.DomainID`, `envelope.Sender` and
`envelope.Recipients` (to, cc and bcc together), builds `From`, `To`, `Cc`,
`Subject`, `Message-ID` and `Date` headers, calls `mailparse.Compose`, and
hands the envelope to the exchange. `SendMail` becomes: look up the template
and layout, `Render`, then `Send`.

### Milestone 2 — translations on the server

A migration `internal/db/migrations/0006_translation.sql` with its reverse:

    ALTER TABLE "template" ADD COLUMN "locale" character varying(32) NOT NULL DEFAULT '';
    ALTER TABLE "layout" ADD COLUMN "locale" character varying(32) NOT NULL DEFAULT '';
    CREATE TABLE "template_translation" (
        "id" character varying(32) NOT NULL,
        "created_at" timestamp with time zone,
        "modified_at" timestamp with time zone,
        "template_id" character varying(32) NOT NULL REFERENCES "template" ("id") ON DELETE CASCADE,
        "locale" character varying(32) NOT NULL,
        "subject" character varying(256),
        "html_content" text,
        "text_content" text,
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX idx_template_translation_template_id_locale ON "template_translation" (template_id, locale);
    CREATE TABLE "layout_translation" ( … the same, with layout_id and no subject … );
    CREATE UNIQUE INDEX idx_layout_translation_layout_id_locale ON "layout_translation" (layout_id, locale);

The reverse drops the two tables and the two columns.

In `internal/models`, `Template` gains `Locale string` (the language of its
default content, optional) and `Translations []*TemplateTranslation`, where

    type TemplateTranslation struct {
        Locale      string
        Subject     string
        HTMLContent string
        TextContent string
    }

and `Layout` gains `Locale` and `Translations []*LayoutTranslation` without
the subject. In `internal/db`, translations are loaded with their parent in
one query per list, and saved by comparing the parent's list against the
rows: a locale present in both is updated, one only in the list is inserted,
one only in the rows is deleted. Deleting a template or layout cascades.

In `internal/util/templating/locale.go`:

    // MatchLocale picks, from the locales available, the one a request for
    // a locale should be answered with. Exact, then the same primary
    // language with no region, then any with the same primary language.
    // Reports false when nothing is close.
    func MatchLocale(requested string, available []string) (string, bool)

    // ValidLocale reports whether a string is shaped like a language tag:
    // a two to eight letter language, then dash separated subtags of one to
    // eight letters or digits.
    func ValidLocale(locale string) bool

In `internal/mailer`, `Render` takes a locale and returns which one it used:

    type Rendered struct{ Subject, HTML, Text, Locale string }
    func Render(template *models.Template, layout *models.Layout, locale string, variables map[string]interface{}) (*Rendered, error)

It chooses the template's translation with `MatchLocale` over the
translations' locales, falling back to the default content; then the
layout's, independently, so a template translated into `ja` inside a layout
that is not still renders in Japanese inside the default layout. The
`Rendered.Locale` is the template's choice, or the template's own `Locale`
when the default was used, or empty. `Message` gains `Language string`,
written as a `Content-Language` header when set. `SendMail` gains a `locale`
argument, and `apisend` reads `"locale"` from its JSON body.

### Milestone 2b — the API

In `internal/models/template.go` add `Variables []string
\`json:"variables,omitempty"\`` with a comment saying it is derived when the
template is read and not stored. In `apigraph/template.go`, a helper
`describeTemplate` fills it from `templating.Variables` over the default
content and every translation of both the template and its layout; `ListTemplates`, `GetTemplate`, `CreateTemplate` and
`ModifyTemplate` return described templates.

`TemplateParameters` gains `Locale` and `Translations
[]*TemplateTranslationParameters` (locale, subject, htmlContent,
textContent), both optional; `LayoutParameters` gains `Locale` and
`Translations []*LayoutTranslationParameters`. Create and modify validate
every locale with `ValidLocale`, refuse a locale listed twice or equal to
the default's, and store the list as given.

Add to `TemplateQuery`:

    RenderTemplate(ctx, RenderTemplateArguments) (*mailer.Rendered, error)

with arguments `DomainID`, optional `TemplateID`, optional
`TemplateParameters` (to preview unsaved content; its `LayoutID` names the
layout), optional `Locale`, and optional `Variables`. Exactly one of `TemplateID` and
`TemplateParameters` must be given. Add to `LayoutQuery`:

    RenderLayout(ctx, RenderLayoutArguments) (*mailer.Rendered, error)

with `DomainID`, `LayoutParameters`, optional `Locale` and optional
`Variables`; renders the layout on its own, so its blocks show their default
content.

Add a new file `apigraph/send.go` with `SendMutation` (added to `Mutation` in
`schema.go`):

    SendMail(ctx, SendMailArguments) (*SendMailReturnValue, error)

    type MessageParameters struct {
        From        string
        FromName    string                  nullable
        To          []string
        Cc, Bcc     []string                nullable
        Subject     string                  nullable
        TemplateID  string                  nullable
        Locale      string                  nullable
        Variables   map[string]interface{}  nullable
        HTMLContent string                  nullable
        TextContent string                  nullable
        Attachments []*AttachmentParameters nullable
    }
    type AttachmentParameters struct{ Filename, ContentType string; Content []byte }
    type SendMailReturnValue struct{ Mail *models.Mail }

The resolver requires the domain, checks the sender is at it, parses every
address with `net/mail`, refuses when there is no recipient, when neither a
template nor any content is given, or when the attachments together exceed
`smtp.maxMessageSize`. With a template it loads it and its layout, renders,
and uses the rendered subject unless `Subject` is set. It calls
`mailer.Send`, then finds the stored mail by envelope identifier through
`ListMails` with a match on `envelopeId` (added to `mailColumns` along with
`messageId`) and returns it, so the dashboard can link to it.

### Milestone 3 — templates and layouts in the dashboard

New pages, routed in `app.tsx`:

- `/domains/:domainId/templates` — `pages/templates.tsx`. Two sections in
  the `SettingsSection` style: templates (name, comment, layout, modified)
  and layouts (comment, modified), each with a "New" action that creates a
  blank one and navigates to its editor, and a remove action with
  `ConfirmDialog`. A layout in use by a template is still deletable by the
  server today; the page says which templates use it so the reader knows.
- `/domains/:domainId/templates/:templateId` — `pages/templateEditor.tsx`.
  Fields: name, comment, layout (select), the default content's locale, and
  then a row of tabs — the default, one per translation, and "Add a
  language" which asks for a locale tag — each tab holding subject, HTML
  (monospace textarea) and text (textarea) for that locale, with a remove
  action on a translation's tab. A preview panel, rendered for the locale
  whose tab is open: the variables detected on
  the server, one input each with a sample value, and Rendered / Text tabs
  showing `RenderTemplate` output for the unsaved content, refreshed on a
  short debounce. Save, Delete, and a link to Compose with this template.
- `/domains/:domainId/layouts/:layoutId` — `pages/layoutEditor.tsx`. Comment,
  the default locale, the same tabs per locale over HTML and text, the same
  preview through `RenderLayout`.

The breadcrumb's `DOMAIN_PAGES` in `components/breadcrumb.tsx` gets entries
for these so the trail reads Domains / example.com / Templates. The domain
overview gets a Templates resource tile.

### Milestone 4 — Compose

`/compose` — `pages/compose.tsx`, linked from the rail's Activity group with a
new pen icon, and reachable as `/compose?domain=<id>&template=<id>`.

Layout, top to bottom: domain select; From as a local-part input beside a
fixed `@domain` and an optional display name; To, with Cc and Bcc revealed by
a link; a mode switch between "From a template" and "Write it here".

Template mode: a template select; a language select listing the template's
default and each of its translations, preselected from the browser's
language when the template has it; one input per variable; the rendered
subject shown; and Rendered / Text tabs of the `RenderTemplate` output. The
locale chosen is sent with the message.

Write mode: subject; a Rich text / Plain text switch; the rich editor
(`components/richText.tsx`: a toolbar of bold, italic, underline, heading,
bulleted and numbered list, link, clear formatting, over a `contentEditable`
div); the plain editor is a textarea. Switching from rich to plain keeps the
derived text; switching back wraps each paragraph.

Attachments: a file input, a list of chosen files with sizes and a remove
button, and the total against the server's limit. Files are read with
`FileReader` into base64.

Send: calls `SendMail`, then shows a notice linking to the stored message and
clears the form. Errors from the server are shown in place.

### Milestone 5 — translations, changelog, documentation, verification

Every new string in `en.ts` gets a `zh.ts` and `ja.ts` translation. A
changelog entry under Unreleased / Added. `docs/reference/command-line.md`
is checked for anything that lists operations. A decision record
`docs/decisions/20260902-mail-is-composed-in-the-dashboard.md` records the
choices above. Then the end-to-end check in Validation and Acceptance.

## Concrete Steps

All commands run from the repository root.

    make build                                   # Go compiles
    go test -mod=vendor ./internal/util/mailparse ./internal/util/templating ./internal/mailer ./internal/api/...
    make test                                    # everything, needs Docker
    make lint-ci
    cd web && npm ci && npm run typecheck && npm run lint && node scripts/check-catalogs.mjs && npm run build

For the end-to-end check:

    make dev                                     # postgres, minio, the server on 127.0.0.1:10081
    cd web && TEANODE_URL=http://127.0.0.1:10081 node scripts/check-queries.mjs

Then in a browser at http://127.0.0.1:10081: create the first account, add a
domain, open its Templates page, create a template, preview it, open Compose,
send with the template and from scratch with an attachment, and open the
resulting messages under Mail.

## Validation and Acceptance

- `go test ./internal/util/mailparse` includes a test that composes a message
  with text, HTML and one attachment and walks it back: three parts in
  order, the attachment's filename and bytes intact.
- `go test ./internal/util/templating` includes cases for `Variables`: a
  plain `{{ name }}`, a dotted path, a filter, a `for` loop whose loop
  variable is excluded and whose iterable is included, and a layout with a
  block.
- `go test ./internal/util/templating` includes cases for `MatchLocale`:
  exact, primary language, `zh_CN` written with an underscore, and no match.
- `go test ./internal/db` (needs the PostgreSQL container `make test`
  starts) round-trips a template with two translations, changes one,
  removes one, and reads it back.
- `go test ./internal/mailer` renders a template with a `zh` translation
  under locale `zh-CN` and gets the Chinese subject, and under `fr` gets the
  default.
- `go test ./internal/api/...` still passes `TestEveryOperationAuthorises`,
  which now counts the new resolvers.
- `node scripts/check-queries.mjs` against the development server reports
  every operation checks out.
- In the dashboard: the Templates page lists what was created; the editor's
  preview changes as the HTML is typed; Compose in template mode shows one
  input per variable and the rendered subject; a message sent from scratch
  with an attachment appears under Mail as outgoing with the attachment
  listed on its detail page and its DKIM signature in the headers.
- A template with an English default and a `zh` translation, sent with
  locale `zh-CN`, arrives with the Chinese subject and body and a
  `Content-Language: zh` header; sent with locale `fr`, it arrives in
  English. The same through `POST /api/v1/send/{domain}/{template}` with
  `"locale": "zh-CN"` in the body.

## Idempotence and Recovery

Every step is additive and can be re-run. The one migration adds two
columns with defaults and two empty tables; its reverse removes them, and the
runner applies the reverse automatically if an older binary starts against
the database. Existing templates keep working with no translations and an
empty default locale. If the dev database is in a bad state,
`make dev-clean` removes it. The worktree is `worktree-webui-compose`; commits
are made per milestone so a partial run can be resumed from the last one.

## Artifacts and Notes

Milestones 1 and 2, from the repository root:

    $ go test -mod=vendor ./internal/util/mailparse ./internal/util/templating ./internal/mailer
    ok      github.com/ziyan/teanode/internal/util/mailparse
    ok      github.com/ziyan/teanode/internal/util/templating
    ok      github.com/ziyan/teanode/internal/mailer

    $ TEANODE_TEST_DATABASE_HOST=… go test -mod=vendor -v -run TestTemplateTranslationsRoundTrip ./internal/db
    migrating database: 0006_translation
    --- PASS: TestTemplateTranslationsRoundTrip (0.13s)

Milestone 5, against the worktree's own server on 127.0.0.1:20081 with an
API token, abbreviated. A layout with an English default and a `zh`
translation, a template in it with a `zh-CN` translation:

    $ cd web && TEANODE_URL=http://127.0.0.1:20081 node scripts/check-queries.mjs
    web/src/pages/domain.tsx: … Expected "String!", found null.   (pre-existing)
    1 problem(s) across 63 operations.

    --- render in locale 'zh-TW':
    {"locale":"zh-CN","subject":"欢迎，Ada","textContent":"你好 Ada\n—— Acme","variables":["company","name"]}
    --- render in locale 'fr':
    {"locale":"en","subject":"Welcome, Ada","textContent":"Hello Ada\n-- Acme","variables":["company","name"]}
    --- send with the template in zh-CN:
    {"mail":{"id":"01m1jcqwjppa9jpx29fy80f43c","kind":"outgoing","status":"accepted","subject":"欢迎，Ada",
             "recipients":["ada@example.net","audit@example.org"]}}
    --- stored mail 01m1jcqwjppa9jpx29fy80f43c:
    headers: From: "Acme Team" <hello@example.com> / To: ada@example.net / Subject: 欢迎，Ada /
             DKIM-Signature: a=rsa-sha256; … / Content-Language: zh-CN / MIME-Version: 1.0 /
             Content-Type: multipart/alternative; boundary=…
    deliveries: audit@example.org external attempted, ada@example.net external attempted
    --- send written by hand, with an attachment:
    attachments: [{"filename":"notes — 2026.txt","contentType":"text/plain","size":1700,"index":2}]
    headers: … Cc: bob@example.net … Content-Type: multipart/mixed; boundary=…
    --- refused:
    "invalid arguments: the sender has to be an address at example.com"
    "invalid arguments: a message needs a recipient"
    "invalid arguments: a message needs a body or an attachment"
    --- the send endpoint with a credential and "locale": "zh":
    status 200, stored subject 欢迎，Ada

The Bcc recipient is on the envelope and in the deliveries and not in the
headers, which is what blind means.

## Interfaces and Dependencies

No new Go or npm dependencies. pongo2 (`github.com/flosch/pongo2/v4`) and
douceur's inliner are already vendored and used by the mailer. The signatures
that must exist at the end are those listed under Plan of Work.

---

Revision note (2026-09-03): locale support was added to the plan after the
owner asked for it mid-way, as Milestone 2 and the `locale` arguments in 2b;
the browser walkthrough was left to the owner because the dashboard sits
behind creating the first account, which this run did not do.
