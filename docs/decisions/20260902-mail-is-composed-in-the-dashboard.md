# Mail is written and sent from the dashboard, and templates carry their translations as child rows

- Status: accepted
- Date: 2026-09-02
- Deciders: Ziyan Zhou, with Claude

## Context

Templates and layouts lived in PostgreSQL and could be rendered and sent
through `POST /api/v1/send/{domain}/{template}`, but nothing in the dashboard
listed, edited or previewed them, and there was no way to send a message from
the dashboard at all. A template was one subject and one body in whatever
language it was written; a recipient in another language got that.

Three questions had to be settled: where sending from the dashboard goes,
how a template holds more than one language, and how the dashboard edits
rich text without adding a dependency.

## Decision

A message sent from the dashboard goes through `mx` exactly as a
credential's submission does, with `Envelope.DomainID` set and no
credential: signed with the domain's key, recorded under Mail, queued for
delivery. One `mailer.Send` assembles a message from its parts — text, HTML,
attachments, in whichever combination is present — and the templated send
renders and then calls it. Rendering is a package-level function so the API
previews with the same code that sends.

A template stays one row with one name. Its content in other locales is a
row per locale in `template_translation`, and a layout's in
`layout_translation`. The row's own content is the default, with an optional
`locale` saying which language that is. Whoever sends names a locale, and
the closest translation is chosen: exact, then the bare language, then any
region of that language, then the default. The layout's translation is
chosen independently of the template's.

The dashboard's rich text editor is a `contentEditable` element driven by
the browser's own editing commands, and the plain text alternative of a
message written in it is derived in the browser before sending.

The send endpoint is let through the authentication middleware by path
prefix, because its caller is an application with an SMTP credential rather
than an operator with a session, and it checks that credential itself.

## Consequences

The template name is what a caller sends by, in every language, so adding a
translation changes nothing for callers; a caller that wants a language
passes `locale`. The cost is a second table per kind and a save that diffs
the translations against their rows. A template row per locale would have
been simpler to store and would have broken the name as an identifier.

Locale matching is deliberately ignorant of scripts and regional fallbacks:
`pt-PT` gets `pt-BR` when that is the only Portuguese. An operator who wants
otherwise writes the translation.

The variables a template reads are found by scanning its source, which is a
heuristic: pongo2 does not expose its syntax tree. A name a template reads
only through a construct the scanner does not understand is missing from the
list, and the template still renders.

`document.execCommand` is deprecated on paper and implemented everywhere in
practice. Should a browser remove it, the toolbar stops working and typing
does not; the plain text editor is always there.

Nothing about who sent a message from the dashboard is recorded on the
message itself. The `Mail` row has no place for an operator, and adding one
is a separate decision.
