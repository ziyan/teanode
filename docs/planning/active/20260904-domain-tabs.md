# One domain, one page with tabs

## Why this matters

Today, everything about a single domain is scattered across pages that are two
clicks deep and named after the shape of the page rather than the question
somebody arrived with. From the dashboard's rail you press Domains, then a
domain, and land on an overview whose bottom half is four large tiles that do
nothing but link somewhere else: Mail, Queue, Templates, Settings. Two of those
tiles leave for a filtered view of a page that already exists elsewhere in the
app; the other two lead to `/domains/<id>/settings` and
`/domains/<id>/templates`, which are the pages an operator actually came to
use.

The settings page is worse than deep. It is one 516-line scroll holding six
unrelated subjects — the DNS records to publish, the names the MX records
point at, the host that serves pictures in mail, the DKIM signing key, the
aliases that decide who receives mail, and the credentials that let a device
send it. Aliases are the single most-used thing on that page for a forwarding
mail server, and they are four screens down.

After this change, a domain is one page with a row of tabs across the top —
Overview, Settings, Aliases, Credentials, Templates — and every tab has its own URL,
so any of them can be linked to, bookmarked, reloaded, and reached with the
browser's back button. Aliases go from "two clicks and a long scroll" to one
click from the domain. The two tiles that only left the page become two
ordinary buttons beside the domain's name, because leaving is what they do.

You can see it working by starting the dev server, opening a domain, and
clicking along the tab row: the address bar changes with every tab, reloading
any of those addresses comes back to the same tab, and the browser's back
button walks back through them.

## What already exists, and where

The dashboard is a React and TypeScript app under `web/`, built with webpack.
Its source is in `web/src`. There is no test runner for it; it is checked by
building it (`cd web && npm run build`) and by looking at it in a browser.

The routes are declared in one place, `web/src/app.tsx`, in a `<Routes>` block
around line 164. The five that matter here are:

    /domains                              DomainsPage        the list
    /domains/:domainId                    DomainOverviewPage the overview
    /domains/:domainId/settings           DomainPage         the six sections
    /domains/:domainId/templates          TemplatesPage      the templates
    /domains/:domainId/templates/:templateId  TemplateEditorPage
    /domains/:domainId/layouts/:layoutId      LayoutEditorPage

The pages behind them:

- `web/src/pages/domainOverview.tsx` (195 lines). One GraphQL query named
  `OVERVIEW` fetching the domain, mail counts by status, the newest message and
  the pending deliveries. It renders two `Section`s of tiles: an "Overview"
  section of five `StatTile`s (messages, accepted, rejected, queued, DNS) and a
  "Resources" section of four `ResourceTile`s (Mail, Queue, Templates,
  Settings). `StatTile` and `ResourceTile` both live in
  `web/src/components/tiles.tsx`.

- `web/src/pages/domain.tsx` (516 lines). One component, `DomainPage`, with one
  query named `DOMAIN` that fetches everything the six sections need, and one
  `run()` helper that runs a mutation, reloads the query and puts any failure
  in a single `problem` string rendered at the top. The six sections are plain
  `<div className="card">` blocks with an `<h3>`, at these lines:

      114  DNS records          domain.dnsTitle
      172  Mail server names    domain.mailServersTitle
      222  Pictures in mail     domain.linkHostTitle
      258  Signing key          domain.keyTitle
      352  Aliases              domain.aliasesTitle
      443  Credentials          domain.credentialsTitle

  Two `ConfirmDialog`s belong to the signing key section, at lines 318 and 340.

- `web/src/pages/templates.tsx` (343 lines). Its own query for the domain, its
  templates and its layouts, and its own `problem` string. It renders
  `SettingsSection`/`SettingsRow` lists from
  `web/src/components/settingsList.tsx`.

The tab pattern to copy already exists: `web/src/pages/server.tsx` renders
`<div className="tabs">` of `<button>`s, reads the active tab from the route
with `useParams()`, and redirects an unknown or missing tab to the first one
with `<Navigate replace>`. Its route is declared twice in `app.tsx`, once as
`/server` and once as `/server/:tab`. The `.tabs` CSS is in
`web/src/style.css` at line 1442 and needs no changes.

The breadcrumb is derived from the route, not declared by pages, in
`web/src/components/breadcrumb.tsx`. `TRAILS` maps a path prefix to a trail;
`DOMAIN_PAGES` adds a crumb for `/settings` and `/templates` under a domain;
`DOMAIN_ITEM_PAGES` adds one for a template or layout under its list. A page
supplies only the name of the thing it is showing, through the
`useBreadcrumbDetail()` hook. `Breadcrumb` renders every crumb but the last,
and `PageHeading` renders the last one as the `<h1>`.

Translations are three catalogues that must stay in step:
`web/src/i18n/en.ts`, `web/src/i18n/zh.ts`, `web/src/i18n/ja.ts`. English is
the reference; `make check-catalogs` compares the other two against it and
fails on a key that is missing or extra.

## What "tab" means here

A tab is a route, not a piece of component state. Pressing one navigates; the
address bar changes; reloading that address comes back to the same tab; the
back button returns to the previous one. This is the same rule `server.tsx`
already follows, and the reason is stated in a comment there: a tab that only
exists in memory is a place you cannot send somebody.

## Milestone one: the shell and the routes

At the end of this milestone the five tabs exist, each at its own URL, and
every one of them renders what its old page rendered. Nothing has been split
up yet and nothing has been deleted; the DNS tab shows all six of the old
settings sections. This is the milestone that proves the routing, the
breadcrumb and the redirects are right before any content moves.

Create `web/src/pages/domainTabs.tsx`. It exports one component,
`DomainTabsPage`, which is the shell for every tab:

- It reads `domainId` and `tab` from `useParams()`.
- If `tab` is not one of the five known ids, it returns
  `<Navigate to={'/domains/' + domainId + '/overview'} replace />`. A path
  somebody typed is not a tab.
- It runs one query for the domain's name — the `GetDomain(domainId) { id
  domain }` fields — and calls `useBreadcrumbDetail()` with the domain name, so
  the trail reads "Domains › example.com" and the `<h1>` is the domain.
- It renders a header row holding the domain name's actions — a Mail button
  linking to `/mail?domain=<domain>` and a Queue button linking to
  `/queue?domain=<domain>` — then `<div className="tabs">` with one button per
  tab, then the active tab's component.

The tab ids, in this order, are `overview`, `settings`, `aliases`, `credentials`,
`templates`. Order is the order somebody meets them: what is happening, then
whether the domain is set up right, then who receives mail, then who may send
it, then what the messages look like.

In `web/src/app.tsx`, replace the two routes `/domains/:domainId` and
`/domains/:domainId/settings` with:

    <Route path="/domains/:domainId" element={<Navigate to="overview" replace />} />
    <Route path="/domains/:domainId/:tab" element={<DomainTabsPage />} />

and keep the two item routes as they are, declared *before* the `:tab` route so
that `templates/:templateId` and `layouts/:layoutId` still reach their editors
rather than being read as a tab:

    /domains/:domainId/templates/:templateId  TemplateEditorPage
    /domains/:domainId/layouts/:layoutId      LayoutEditorPage

Add one redirect so no old link breaks. `/domains/<id>/settings` was the DNS,
mail servers, link host, key, aliases and credentials page; it now means the
DNS tab:

    <Route path="/domains/:domainId/settings" element={<RedirectDomainSettings />} />

where `RedirectDomainSettings` reads `domainId` from `useParams()` and returns
`<Navigate to={'/domains/' + domainId + '/dns'} replace />`. `app.tsx` already
has two components of exactly this shape, `RedirectDomain` and
`RedirectIntegrations`; write this one beside them and follow their form.

In `web/src/components/breadcrumb.tsx`, delete the `DOMAIN_PAGES` list and the
code that consults it. With tabs, the tab row says which tab you are on, and a
crumb saying it as well is the same word twice — the trail should end at the
domain, whose name becomes the `<h1>`. Leave `DOMAIN_ITEM_PAGES` alone: a
template editor is still three levels down and still needs its middle crumb,
which must now point at `/domains/<id>/templates`.

Add these keys to all three catalogues (English text given; translate for `zh`
and `ja`):

    domain.tabOverview      Overview
    domain.tabDns           DNS
    domain.tabAliases       Aliases
    domain.tabCredentials   Credentials
    domain.tabTemplates     Templates
    domain.viewMail         Mail
    domain.viewQueue        Queue

Acceptance for this milestone. Run `cd web && npm run build` and see it compile
with no new warnings, then `make check-catalogs` and see it pass. Start the dev
server with `make dev` and open `http://localhost:8833/domains`. Press a
domain: the address becomes `/domains/<id>/overview`, the breadcrumb reads
"Domains" alone, the heading is the domain's name, and a row of five tabs is
under it. Press each tab in turn and watch the address bar change to
`/dns`, `/aliases`, `/credentials`, `/templates`. Reload the page on each one
and come back to the same tab. Press the browser's back button five times and
walk back through them. Open `/domains/<id>/settings` and land on `/dns`. Open
`/domains/<id>/nonsense` and land on `/overview`. Open a template from the
Templates tab and check the trail reads "Domains › example.com › Templates"
with both crumbs working.

## Milestone two: the overview loses its tiles

At the end of this milestone the overview is stats and nothing else, and the
two links that left the page are buttons in the header.

In `web/src/pages/domainOverview.tsx`, delete the entire second `<Section>` —
the one labelled `domainOverview.resources` holding the four `ResourceTile`s —
and the now-unused imports (`ResourceTile`, `GridIcon`, `QueueIcon`,
`TemplateIcon`, `DomainsIcon`). The five `StatTile`s stay exactly as they are,
including their links: the messages, accepted, rejected and queued tiles keep
linking to the filtered `/mail` and `/queue` views, and the DNS tile's link
changes from `/domains/<id>/settings` to `/domains/<id>/dns`.

If `ResourceTile` in `web/src/components/tiles.tsx` now has no callers, delete
it and its styles. Check with `grep -rn ResourceTile web/src` before deleting;
if something else uses it, leave it.

The keys `domainOverview.resources`, `domainOverview.mail`,
`domainOverview.mailDetail`, `domainOverview.queue`,
`domainOverview.queueDetail`, `domainOverview.templatesDetail`,
`domainOverview.settings` and `domainOverview.settingsDetail` may now be
unused. Check each with `grep -rn` across `web/src` before removing it from all
three catalogues — `domainOverview.settings` in particular was also used by the
breadcrumb's `DOMAIN_PAGES`, which milestone one deleted.

Acceptance. Build, then open a domain's Overview tab: five stat tiles and no
tile wall under them. The Mail and Queue buttons in the header go to
`/mail?domain=<domain>` and `/queue?domain=<domain>` and those pages come up
filtered to this domain. The DNS stat tile goes to the DNS tab.

## Milestone three: the settings page splits three ways

At the end of this milestone `web/src/pages/domain.tsx` is gone, replaced by
three files, and the DNS, Aliases and Credentials tabs each render their own.

The obstacle is that all six sections share one query, one `run()` helper and
one `problem` line. Splitting them naively would fetch the domain three times
and put three different error lines in three places. Instead, `DomainTabsPage`
owns the fetch and passes it down. Give it the full `DOMAIN` query from
`domain.tsx` — it already needs the domain's name, and one query for the whole
page is what exists today — and give it the `run()` helper and the `problem`
state too, rendering `{problem && <p className="error">{problem}</p>}` once,
above the tab content.

Define this shared shape in `web/src/pages/domainTabs.tsx` and export it:

    export type DomainTabProps = {
      domain: Domain
      run: (work: () => Promise<unknown>) => Promise<void>
    }

Then create three files, moving the sections across without rewriting them:

- `web/src/pages/domainDns.tsx` exporting `DomainDnsTab`, holding the DNS
  records card (domain.tsx:114), the mail server names card (172), the
  pictures-in-mail card (222) and the signing key card (258) with its two
  `ConfirmDialog`s (318, 340). The state those cards use — `selector`,
  `movingSelector`, `mailServers`, `linkHost`, `replacing` — moves with them.
- `web/src/pages/domainAliases.tsx` exporting `DomainAliasesTab`, holding the
  aliases card (352) and the `pattern`, `kind`, `destination` and `comment`
  state it uses, plus the `CREATE_ALIAS` and `DELETE_ALIAS` mutations.
- `web/src/pages/domainCredentials.tsx` exporting `DomainCredentialsTab`,
  holding the credentials card (443), the `created` state and the
  `CREATE_CREDENTIAL` and `DELETE_CREDENTIAL` mutations.

The `CHECK` mutation is used by both the DNS card and the mail servers card, so
it belongs in `domainDns.tsx`. Move each `const` GraphQL string to the file
that uses it; if two files need the same one, keep it in `domainTabs.tsx` and
import it.

Delete `web/src/pages/domain.tsx` and its import in `app.tsx`.

No translation keys change in this milestone: every `t('domain.…')` call moves
with the markup that made it.

Acceptance. Build. Then exercise every mutation, because this milestone moves
the code that performs them and a broken one is invisible until pressed:

- DNS tab: press "Check again" and watch the records table's state column
  update. Change the mail server names, save, and see the DNS records below
  change to match. Set a host for pictures, save, reload, and see it kept.
  Move the DKIM selector and regenerate the key, confirming each dialog.
- Aliases tab: add an alias, see it in the list, delete it, see it go.
- Credentials tab: create a credential, see the secret dialog with a password
  in it, dismiss it, then delete the credential.
- A failure still shows once, above the tabs: try saving a mail server name of
  `not a host` and see one error line, not three.

## Milestone four: templates in a tab

At the end of this milestone the Templates tab renders the templates list
inside the shell rather than as a page with its own heading.

`web/src/pages/templates.tsx` already fetches its own data and holds its own
`problem` state, and that is fine — it is a different query from the domain's
and there is no reason to fold it into the shell's. The only change it needs is
that its `useBreadcrumbDetail()` call and any page-level heading are now the
shell's job. Read the file, remove the duplication, and leave the rest.

Acceptance. Build. The Templates tab shows the same list it always did, the new
template button still creates one, and the heading above the tab row is the
domain's name rather than the word "Templates".

## Progress

- [x] Milestone one: the shell, the five routes, the redirect, the breadcrumb
      and the seven new translation keys
- [x] Milestone two: the overview's tile wall removed, `ResourceTile` deleted
      if unused, dead keys removed from all three catalogues
- [x] Milestone three: `domain.tsx` split into `domainDns.tsx`,
      `domainAliases.tsx` and `domainCredentials.tsx`, with the query, `run()`
      and the error line hoisted into the shell
- [x] Milestone four: templates rendered inside the shell without its own
      heading

All four milestones are implemented. `npm run build` and `npx tsc --noEmit`
pass, `make check-catalogs` reports 695 keys agreeing across en, zh and ja, and
the dashboard dev server on port 10000 serves a bundle containing the new tab
keys. What has not been done is looking at it: the browser extension was not
connected in the session that wrote this, so the acceptance walks described in
each milestone — clicking the tabs, watching the address bar, exercising every
mutation — are still to be performed by a human at
http://127.0.0.1:10000/domains.

## Decisions

**The settings tab is called Settings, not DNS.** It holds four subjects —
the records, the mail server names, the pictures host and the signing key —
and naming it after one of them promised less than it carries. The route is
`/domains/<id>/settings`, which is also what the old page was called, so the
redirect that pointed the old path at `/dns` is gone: the path is the tab.

**Five tabs rather than three.** The alternative was Overview, Settings and
Templates, keeping the settings page whole. It was rejected because it moves
the problem rather than solving it: aliases would still be four screens down a
page named after its own shape. Splitting along the seams already in the file
costs one milestone and is what `/server` did to the rail for the same reason.

**DNS holds four subjects, not one.** The mail server names, the pictures host
and the signing key are all "what this domain publishes and how it is
identified", and each is a card of a few fields. Four small cards under one tab
read better than four tabs with one card each.

**Mail and Queue are buttons, not tabs.** They navigate away to a different
page of the app with a filter applied, and a tab that leaves the page it is a
tab of is a lie about where you are.

**The shell owns the domain query.** Three tabs need the same domain, and
fetching it three times would show three loading states and three error lines
for one failure.

## Still open

Whether "Mail server names" and "Pictures in mail" should offer their possible
values rather than being empty text fields. Both accept any host name — the
mail servers may be outside the domain and the pictures host must be under it —
so neither is a fixed list and neither can be a plain dropdown. The shape that
fits is a text field with suggestions attached (`<input list>` and a
`<datalist>`), offering the computed default and the server-wide names while
still accepting anything typed. Not part of this plan; decide before the DNS
tab is considered finished.
