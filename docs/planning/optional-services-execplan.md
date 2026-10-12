# Optional hosted services and assistant-first setup

This ExecPlan is a living document. Keep Progress, Surprises & Discoveries,
Decision Log, and Outcomes & Retrospective current during implementation.

## Purpose / Big Picture

An operator must be able to run the personal assistant without hosting email,
creating mailboxes, or keeping calendars and contacts. Chat, knowledge, goals,
scheduled assistant work, attached computers, coding-session hooks, connected
servers, files, voice and supported notification channels should work with
only the database, file storage, the HTTP API and a configured model provider.
An email domain, MX records, reverse DNS and access to SMTP ports are required
only when the operator chooses to host mail.

Hosted services are independent product choices. Forwarding mail without
mailboxes remains supported. Calendars, reminders and contacts can be used
without hosting mail. The existing mail server remains a supported preset;
upgrading an installation must preserve its current behavior and identifiers.

This document records the implementation in progress; changes remain in the
draft pull request until reviewed. It does not implement an external email reader
or sender. A connected service can supply those tools under its own permission
and confirmation policy; turning off hosted mail must not disable those tools.

## Progress

- [x] Audited configuration, bootstrap, startup and mailbox provisioning.
- [x] Audited API checks, lazy collection creation, DAV credentials and profiles.
- [x] Audited assistant tools, prompt construction, jobs and introductions.
- [x] Audited web navigation, polling, setup, native app and widget reads.
- [x] Specified service switches, upgrade behavior and disabling semantics.
- [x] Specified operator onboarding, implementation milestones and acceptance.
- [x] Implement and test the service configuration and capability contract.
- [x] Implement optional runtime construction and guarded operations.
- [x] Decouple calendar/contact sync credentials from mailboxes.
- [x] Implement web, native app, CLI and assistant capability awareness.
- [x] Implement setup choices and an assistant deployment preset; smoke-tested HTTP-only startup and account creation.
- [x] Complete source review and fix protocol, identity-provisioning, REST-route and client-mode findings.
- [ ] Complete the final macOS simulator build and screenshot checks in CI.
- [x] Commit and push the implementation on the existing draft branch; CI is checking the native app.

## Surprises & Discoveries

The following are controlling code observations. They describe the current
implementation, rather than conclusions inferred from product copy.

| Boundary | Current coupling | Required change |
| --- | --- | --- |
| Validation | `internal/config/validate.go`, `validateListen`, requires both incoming and outgoing SMTP addresses. `validateServer` describes the hostname as an SMTP identity; mail configuration is always validated. | Allow an HTTP-only server. Validate active services and preserve shared TLS, storage, session and database checks. |
| Fresh defaults | `internal/config/defaults.go` enables SMTP on ports 25 and 587. `internal/bootstrap/bootstrap.go`, `SeedConfiguration`, applies those defaults; its error says a server domain is always needed. Domain seeding itself is optional. | Separate compatible configuration defaults from the explicit choices written for a new installation. Require a hosted email domain only for hosted mail. |
| Startup | `internal/cmd/server/run.go`, `openServer`, always constructs the exchange and mail-specific integrations. `internal/mx/exchange.go`, `Open`, starts the delivery queue, retention and usage workers immediately. | Gate construction, listeners and worker start, rather than merely suppressing UI. |
| Web startup | `openWeb` always constructs the mailer, submission reconciler, mail DNS verifier, mail-list backfill and sender-logo fetcher. It also launches a notes backfill over mailbox content. | Separate core HTTP/session maintenance from optional mail maintenance. |
| Invitations | `internal/cmd/server/agent.go`, `openScheduler`, always starts invitation processing and attaches it to the exchange. | Start only when hosted mailboxes and event calendars are enabled. Keep assistant schedules and goals independent. |
| Accounts | `internal/access/access.go`, `EnsureMailboxes`, provisions everyone at startup. `internal/web/session.go`, `CreateFirstUser`, the server user command, and GraphQL `CreateUser` also call `EnsureMailbox`. | Gate every entry point. An account and its agent must not imply a mailbox. |
| Collections | GraphQL `ListCalendars` creates event and reminder collections; `ListAddressBooks` creates an address book. DAV listing creates collections too. | Check service availability before creating or reading a collection; preserve lazy creation only for enabled kinds. |
| DAV identity | `internal/dav/auth.go` authenticates a mailbox address and mailbox app password. The resulting mailbox supplies the user ID. | Add account-owned sync credentials. Calendar-only and contacts-only users must need no mailbox or mail address. |
| Device profiles | `internal/api/v1api/apigraph/mailbox_profile.go` requires a mailbox and an IMAP listener before producing a combined mail/calendar/contact profile. | Retain the existing mail operation; add an account device-profile operation that emits only the requested, available payloads. |
| API authorization | Mailbox, calendar and contact operations enforce permissions and ownership, but no deployment-wide service switch. Command layers do not yet receive this service policy. | Enforce capability, permission and ownership at each public and worker entry point. Disabling a service overrides an existing role or grant. |
| Agent construction | `openAgentWorker` unconditionally calls `self.exchange.SetAgentHook`; the worker settings already allow a nil exchange and several mail paths check it. | Guard hook attachment and make the worker's core independent of a mail exchange or mailer. |
| Prompt reads | `internal/agent/ask.go`, `sources`, always asks for mailboxes. `collections` requests calendars and contacts in one GraphQL document, invoking lazy provisioning. | Skip unavailable reads and split independent reads so one refused service cannot hide another. |
| Tools and ideas | `internal/agent/tools/tool.go`, `Catalog.Offered`, filters permissions and disabled-tool policy. The catalog contains hosted mail, calendar and contacts tools. Ideas depend on available tool names. | Add service requirements to internal tools and merged actions, and apply the same availability in discovery, invocation and idea selection. |
| Introduction | `speak_first_onboarding.go` promises mail sorting and ends by offering a mailbox. `internal/agent/tools/agentprofile` repeats the mailbox offer. The phone tour checks mailbox existence. | Describe only available and granted services, and offer computers or other sources without a mailbox prerequisite. |
| Email notices | `internal/agent/mail_notice.go` requires both a mailer and an addressed, granted mailbox to email an answer. | Hide/refuse this destination when unavailable; keep conversation, push and chat-app delivery operational. |
| Web home | `web/src/app.tsx` sends an account home to `/mailbox`. `web/src/mailboxes.tsx` polls for every session with a user ID. | Select a usable home from capabilities and permissions, and never poll a disabled service. |
| Web navigation | In `web/src/components/sidebar.tsx`, contacts, calendars, knowledge and finance are rendered inside the mailbox section. Management navigation starts with mail-related pages. | Separate personal navigation from the mailbox tree and filter management rows as well as routes. |
| Operator setup | `web/src/pages/setup.tsx` is a mail checklist, requesting domain records, public mail addresses, outgoing identity and mail-client settings. | Add a service chooser and independent setup tasks; do not run mail diagnostics for an assistant installation. |
| Native app | `ios/TeaNode/Sources/AppShell.swift` has fixed sections, constructs a mail model and loads it even when reading chat. `TeaNodeKit/Glance.swift` asks for calendars and mailboxes. | Read capabilities per account; filter sections, eager loads, periodic reads, deep links, profiles and widgets. |
| Deployment | `deploy/docker-compose.yml` uses host networking to retain SMTP client addresses and grants privileged-port binding. `config env` and the README guide operators through mail domains and DNS. | Supply a minimal assistant deployment and an explicit mail preset; preserve real client addresses in the mail deployment. |

The calendar and contacts ownership checks are already account-based in
GraphQL. Their main mailbox dependency is sync authentication, profile setup,
mail invitation delivery and client navigation. The agent is already a person
rather than a mailbox, so replacing its identity model is unnecessary.

## Decision Log

These are proposed decisions to make the implementation concrete. Record the
accepted architecture in a decision record when implementation starts.

1. Put deployment-wide choices in a new `services` configuration section.
   Reuse `agent.enabled` for the assistant; do not add a second assistant
   switch. Proposed paths are `services.mailRouting.isEnabled`,
   `services.mailboxes.isEnabled`, `services.calendar.isEnabled`,
   `services.reminders.isEnabled` and `services.contacts.isEnabled`.
   Nullable switches resolve to enabled in existing configurations. A fresh
   assistant setup writes explicit false values for hosted services. Disabled services retain their
   configured listener addresses, but neither bind nor validate them until
   enabled; re-enabling validates the retained settings before applying them.
   `internal/config/dbstore.go` must map the section in both directions.

2. Mail routing owns hosted domains, SMTP reception/submission, forwarding,
   hosted sending, DKIM/DMARC/ARC and their workers. Mailboxes require mail
   routing in the first release. Mail routing without mailboxes supports
   forwarding. Reject an incompatible configuration with an explanation;
   do not silently turn another service on. Import-only local mailboxes can
   be a later design if needed.

3. Calendar events, reminders and contacts are independent of mail routing
   and of each other. Reminders reuse calendar storage and CalDAV as they
   do today, but the calendar switch must not disable assistant schedules,
   goals or a separately enabled reminders list. Keep the existing
   `calendar:use` authorization for reminder operations initially; check
   the requested collection kind's service as well. Do not expand a role's
   access as part of this work.

4. Capabilities are distinct from permissions and agent grants. The operator
   offers a service, a role authorizes its use, and the person separately
   grants an available collection to their agent. Switching a service off
   does not rewrite roles, remove grants, or erase its collections. Facts
   already copied into the person's memory are not erased by this switch;
   forgetting or deleting those facts remains a separate explicit operation. A
   user-facing service preference can be added later; deployment switches
   and existing roles cover the initial scope.

5. Keep one stable GraphQL schema. Add an authenticated capability query
   shared by browser, native app, CLI and agent discovery. Return no model
   credentials or operator settings. Disabled operations fail with a stable
   service-unavailable code and a useful sentence, before lazy creation or
   a write. Requests cannot bypass this by using an old client, a token,
   a cached tool, or a command-layer entry point.

6. Separate configured choices from what the process is running. Use the
   existing restart reporting for construction and listener changes. While
   disabling, immediately deny new operations and new work; runtime
   capability is the intersection of configured and running services.
   Enabling becomes usable only after successful construction/restart.
   Every background entry point checks current availability. Setup shows
   pending restart and failed start clearly rather than claiming success.

7. Disabling preserves data and pauses pending service work. Do not delete
   mail, messages, collections, aliases, passwords or queued submissions.
   Do not count disabled work as a failed retry or a successful delivery.
   Re-enabling resumes from stored configuration. A separate explicit
   deletion workflow remains responsible for data removal.

8. Account-owned sync credentials are narrowly scoped to calendar,
   reminders and/or contacts, hashed, named by device and individually
   revocable. They never authorize SMTP or IMAP and never accept the
   dashboard password. Preserve existing mailbox app passwords, their
   identifiers and their currently authorized DAV access for compatibility.
   New pure-DAV credentials require no email address. They authenticate a
   normal account username; advertise complete discovery URLs explicitly.

9. Core file storage remains available. Assistant attachments, artifacts,
   avatars, receipts and source files do not depend on hosted email.
   Database schema migrations remain universal: this is runtime
   optionality, not a separate binary or selective migration system.

## Outcomes & Retrospective

The audit and implementation design are complete. No runtime change has been
made. The main work is a shared availability contract and enforcing it across
existing boundaries; changing only listener addresses would leave mail work,
provisioning, DAV identity and mail-first UI active.

## Context and Orientation

`internal/config` owns operator configuration, its defaults, validation and
persistent section mapping. `internal/bootstrap` seeds a new database from
process environment, after which the configuration store is the answer.
`internal/cmd/server/run.go` constructs and closes components and binds ports.
`internal/cmd/server/agent.go` constructs the assistant and mail invitation
worker. Preserve the ACME challenge before authentication and routing.

Accounts live independently of mailboxes. Roles and effective permissions are
in `internal/access`; mailbox creation currently accompanies account creation.
`internal/api/v1api/apigraph` exposes the stable schema, and commands such as
`internal/addressbook/commands.go` and `internal/calendar/commands` perform
ownership and transactional writes. `internal/dav` shares those accounts but
currently reaches them through mailbox credentials.

The assistant catalog, its merged actions and run interfaces are in
`internal/agent/tools`. Prompt-time collection reads and source descriptions
are in `internal/agent/ask.go`; mailbox processing jobs share a worker with
knowledge, dreams, schedules and goals. Preserve that shared worker and gate
mail job kinds separately. Existing decisions about account-owned agents,
configuration in the database, and the address book as the sole list of
people remain in force.

## Plan of Work

### Milestone 1: configuration and one capability contract

Add the service settings and resolution helpers in `internal/config`. Register
`services` in configuration section persistence, import/export, redaction and
configuration reference documentation. Existing sections with no new keys
resolve to the current enabled behavior; test both old database rows and old
configuration imports. Do not infer an upgrade's choices from empty domain
lists, mailbox counts or agent settings.

Add an authenticated `GetServiceCapabilities` operation. Its initial booleans
are `isMailRoutingEnabled`, `isMailboxEnabled`, `isCalendarEnabled`,
`isRemindersEnabled` and `isContactsEnabled`. Report assistant availability
using the existing configured/running agent checks. Include
`hasPendingServiceRestart` and a service-specific explanation when a selected
service is not running. The operator gets desired configuration and detailed
setup checks through a separate permission-protected operation. Add the
shared documents and typed responses to Go, TypeScript and TeaNodeKit.

Define a service requirement on public operations, commands, internal tools
and job kinds, using the same resolver. Authorization remains a separate
check. An administrative tool that edits server settings remains available
when mail is off, but the assistant must not be able to enable services or
provision credentials through onboarding. Service changes require the
operator's normal management permission and existing confirmation policy.

Acceptance: old configurations resolve unchanged; an all-hosted-services-off
configuration validates; invalid dependencies explain what to change;
capabilities disclose no secrets and do not create resources.

### Milestone 2: optional runtime and provisioning

Split startup into shared components and service-specific components. Always
construct the database, secret/session services, configured file storage,
HTTP API, and the assistant only when configured. Mail routing controls the
exchange, SMTP listeners, mail DNS verifier, built-in/external spam filter,
virus client, mail delivery/reconciliation and mail-only maintenance. IMAP
requires mailboxes; the existing empty listener addresses still disable an
individual listener. SMTP settings must not affect HTTP-only startup.

Construct the invitation worker only when mailboxes and event calendars are
available. Guard exchange hook attachment. Construct DAV only when an enabled
collection kind is available; within DAV, advertise and dispatch only those
kinds. Disabled DAV and well-known paths must return a protocol response,
never the dashboard's HTML catch-all. Continue to apply TLS and authentication
correctly to every enabled path.

Gate startup mailbox backfill, first-account creation, CLI-created users,
API-created users, and any account creation through other authentication
paths. An assistant-only account has no mailbox, address book or calendar
until an enabled service is intentionally used. Do not use a read for service
readiness that implicitly creates a collection.

Add availability checks to public APIs, photo/file routes, commands, profile
downloads and scheduled work. Inventory all mailbox job kinds, sent-mail
sources, incoming invitation jobs, auto-reply paths, submission reconciliation
and queued deliveries. Exclude paused kinds from claims and check immediately
before external delivery or transactional writes. A disabled service cannot
keep retrying in the background. Keep shared session, agent and attachment
retention operational; do not accidentally strand files by removing a mail
component that owned part of cleanup. Read-only operator inspection and export
of preserved service data remain possible through explicit management paths;
those paths never provision collections or resume paused work.

Acceptance: a fresh HTTP-only process starts and serves chat with SMTP ports
unavailable, no mail domain, no spam/virus dependencies and no exchange.
Tests observe no mail worker, mail diagnostics or disabled collection
provisioning. Agent files and memory still work.

### Milestone 3: calendars and contacts without a mailbox

Add account-owned sync credentials with scoped service access and an additive
migration with reverse SQL. Reuse the existing password hashing, device
labels, credential attempt accounting and revocation patterns. Keep legacy
mailbox credential authentication intact rather than moving or replacing
those records. Test same-looking account and mailbox usernames, wrong-owner
records, revoked credentials and a credential whose allowed service is off.

Separate credential management and device setup from Mail programs. Add an
account Devices/Sync surface and a profile operation that selects calendars,
reminders and contacts independently, with IMAP/SMTP payloads included only
when a usable mailbox is explicitly selected. Preserve the old profile
operation for old clients. Keep secrets sealed, short-lived, single-use and
out of model context. A password is provisioned only after the requested
payloads and accessible origin have been validated.

A calendar with no hosted mail can create/edit/delete local events without
guests. Invitations, RSVP delivery and notifications that need a sender must
be refused before changing an event or accepting a send when that path is
unavailable. Do not silently remove guests or report that invitations were
sent. Existing SMTP relay configuration is a transport for hosted sending,
not a complete independent notification-email service. External notification
email can be designed separately; conversation, push and chat-app notices
must already work without it.

Acceptance: a calendar-only account syncs through CalDAV, a contacts-only
account through CardDAV, and a reminders-only account exposes VTODO without
VEVENT. None owns a mailbox or a mail address. Pure-DAV passwords fail SMTP
and IMAP. Existing mail app passwords and profiles continue to work.

### Milestone 4: assistant and every client respect availability

Filter hosted tools before prompt construction, deferred tool discovery,
merged action schemas, direct calls, MCP exposure of internal tools, scheduled
runs and subagents. Recheck invocation so a tool cached before a service was
disabled cannot still act. Do not disable the whole account tool family: it
contains unrelated assistant and identity functions. Connected external
calendar/email tools retain their own policy and scope.

Split `AskRun.collections` into independent allowed reads; skip `sources`
when hosted mailboxes are unavailable. Apply the same checks to `ReadAgent`
source lists, source grants, mail delivery hooks, sent-mail ingestion, ideas,
mail alerts, email notice destinations, introductions and the phone tour.
Distinguish mail-derived alerts from finance and other assistant alerts.
Remove assumptions that somebody with no collection has an empty available
collection. The prompt should say that a hosted service is off, rather than
claiming that the person withheld a grant or promising to connect a mailbox.

Make a capability provider in the web shell. Personal navigation contains
Agent, Goals/Ideas, Knowledge and enabled personal services independently of
the mailbox tree. Calendar and contacts use neutral canonical routes with
compatible aliases from the old mailbox routes. Use capabilities and
permissions to select home and to filter management tabs, direct routes,
compose actions, reference links, account settings and mail-related forms.
A disabled page explains availability and offers a usable destination without
a redirect loop. Turn off the mailbox provider's polling and all related
freshness queries when mailboxes are off.

Give each native app account its own capability state. Filter the side menu,
mail model startup, foreground refresh, agent collection settings, sign-in
follow-up, device profiles, notification actions, Siri/Shortcuts, widgets and
deep-link destinations. Combined GraphQL operations must skip unavailable
fields or split their reads. Capability refresh on foreground/account switch
must remove obsolete UI and stop polling. Old native apps still receive
well-formed schema responses and explicit refusals, rather than nil-service
panics. CLI commands show a useful disabled-service error from the same API.

Acceptance: chat and knowledge remain reachable on both clients with no
mailbox. Calendar-only and contacts-only views work independently. Network
traces contain no disabled-service polling, and no introduction, idea or
profile offers a service that is off.

### Milestone 5: an operator can get to value without mail setup

Replace the mail-only setup checklist with the flow specified below. Store its
completion in operator configuration, and separate it from the agent's
personal introduction. Refreshing, changing browser or reconnecting resumes
the same selected services and unfinished tasks. No secrets are returned by
setup status. Reuse the shared forms, validation, restart reporting, toasts
and navigation conventions rather than creating a parallel settings system.

Update `config env` to offer an explicit service preset and produce
preset-specific next steps. For an empty database with no mail-specific seed
choices, bootstrap writes the minimal services-off configuration. Existing
explicit mail/domain seed choices retain a mail-compatible first start; an
explicit assistant preset conflicts with a requested hosted mail domain or
SMTP listener and reports that conflict instead of starting mail silently.
Existing databases ignore seed presets as they ignore other seed settings.
For the minimal local assistant seed, disable ACME and the HTTPS listener,
use a loopback HTTP listener on an unprivileged port, and leave SMTP/IMAP
listeners inactive. An explicit secure deployment preset supplies its
reachable hostname and TLS/proxy settings instead. The setup flow can change
those choices through the stored configuration; it must not need a hostname
merely to show the local service chooser.

Supply a documented assistant compose configuration with ordinary HTTP port
mapping, persistent storage and the database. It must not inherit host
networking, SMTP bindings or mail-specific privilege requirements. Preserve
the current mail deployment's accurate SMTP client IP handling. Support both
reverse-proxy TLS and built-in HTTPS. A loopback-only setup can start before a
public hostname is chosen; public login and phone setup still need the
existing secure-origin protections. Do not expose an unclaimed instance to
public traffic merely to make the wizard convenient.

Update the README, deployment guide and installation commands so the operator
first chooses what to run. Keep the full mail DNS and deliverability checklist
for the mail branch of the flow. Review the flow at desktop/mobile widths,
light/dark appearance, with a keyboard and a screen reader; check the native
client's equivalent capability-driven entry points too.

## Operator onboarding specification

The operator and the person may be the same account, but setup configures the
server and the assistant introduction configures the person's preferences.
Do not mix those permissions or make an assistant conversation responsible
for completing installation.

1. **Claim this server.** Create the first administrator through the existing
   protected first-account flow or provision it through the server CLI.
   Establish secure web access before requesting provider credentials. An
   external TLS proxy is a supported answer, not a failure to install ACME.

2. **What would you like to run?** Offer three starting points: Personal
   assistant, Mail server, or Choose individually. Personal assistant is the
   recommended new choice, with hosted mail, calendars, reminders and contacts
   off. Mail server selects mail routing and mailboxes; calendar, reminders
   and contacts remain explicit choices. A review list shows each service
   before applying either preset. Presets seed choices, not permanently
   coupled modes. Explain each choice in one sentence and show dependencies
   beside the choice that needs them. Nothing is enabled merely by navigating
   forward. The person can return and change any choice.

3. **Set up what you selected.** For the assistant, connect a supported model
   provider, choose a model, set spending/unattended-work limits and verify a
   small connection test. Use existing provider sign-in and secret handling.
   Let the operator defer the model connection with an honest incomplete task;
   do not force dummy credentials to boot. Save provider setup while the agent
   is disabled, then enable it only after provider/model validation succeeds.
   For mail, ask for domains, delivery
   destination and required protocol bindings, then run only relevant DNS,
   TLS and sending checks. For calendar/contacts, describe local use and offer
   optional device sync setup without an email address. No MX, SPF, DKIM,
   reverse-DNS or SMTP warning appears on an assistant-only checklist.

4. **Review and apply.** Summarize the offered services, required listener
   changes, unfinished tasks and any needed restart. On an existing server,
   also show affected devices and pending service work. State that disabling
   preserves data and pauses work. Keep the current app usable until a
   confirmed restart finishes, and show actual running availability after it.
   A cancelled edit changes nothing. A validation failure returns to the
   relevant field without discarding other choices.

5. **Try the useful thing.** In the assistant path, explicitly offer to enable
   this person's agent and open its first conversation. Retain individual
   consent even when the operator chose the assistant for the deployment.
   Offer Connect a computer, Connect a service, Add a source, or Connect a
   phone as optional next actions. An empty memory is a usable starting point.
   In the mail path, send/receive a test message and show the result. In the
   calendar/contact path, create a local item and optionally sync a device.
   Setup can finish with deferred optional tasks; it cannot mark a selected
   service ready when its model, listener or credential path is unavailable.

Later, **Server > Services** uses the same chooser and review. Enabling mail
starts its setup tasks; it does not automatically redirect every user to a
mailbox. Default home is the agent when available, otherwise an enabled
personal service, otherwise an authorized management page or an explicit
account-with-no-access view. Preserve existing users' mail home through a
compatible saved/default preference rather than changing their landing page
merely because the software upgraded.

## Concrete Steps

Work in small dependency-ordered changes: contract and compatibility first,
runtime gating second, account sync third, assistant/client integration fourth,
and setup/deployment last. Give each its own regression checks and describe
whether a restart is required. Do not ship assistant onboarding before its
mail-free backend path works.

Representative verification commands from the checkout root:

    go test -mod=vendor ./internal/config ./internal/bootstrap ./internal/access
    go test -mod=vendor ./internal/cmd/server ./internal/web ./internal/dav
    go test -mod=vendor ./internal/api/v1api/apigraph ./internal/agent/...
    go test -mod=vendor ./internal/addressbook/... ./internal/calendar/commands/...
    make format lint test build
    cd web && npm run build
    cd ios/TeaNodeKit && swift test

Database cases must run with the repository's test database configured rather
than relying on the packages' skip behavior. Native app builds and simulator
checks run on macOS through the existing iOS CI workflow. Runtime acceptance
uses a disposable database and instance, never a production mailbox. Capture
network calls, bound listeners and worker starts in addition to screenshots.

## Validation and Acceptance

| Installation | Expected result |
| --- | --- |
| Assistant only | Claim account, configure model, opt in, chat, store an attachment, search knowledge and run a goal with no mail/DAV listeners or hosted collections. |
| Forwarding only | Receive and forward authenticated mail through existing rules; no mailbox provisioning or IMAP. |
| Full existing mail installation | Upgrade preserves listeners, domains, aliases, identifiers, credentials, mailbox data, delivery queue, DNS checks and client access. |
| Assistant and calendar only | Chat and local events work; DAV uses an account credential; no mailbox/address is created. |
| Contacts only | Read/write/sync cards with no mail or event calendar; contact query does not create a calendar. |
| Reminders only | Reminder operations and VTODO sync work; no VEVENT collection is created; assistant schedules remain independently available. |
| Mailboxes with calendar off | Mail delivery works; invitations are not processed into calendars; no implicit calendar creation or RSVP success is reported. |
| Calendar on, contacts off | Calendar prompt context, UI and sync remain usable despite contacts being disabled. Test the reverse combination too. |

Test every service off both before first use and after data, grants and jobs
exist. Cover GraphQL, direct command calls, tool invocation, an already-loaded
tool, DAV methods/discovery, HTTP file routes, CLI and old native clients.
Refusals must happen before writes or external sends. Pure-DAV credentials
must never confer mail access. Same-account ownership and cross-account
refusals still apply.

Disable during an active request and with a nonempty delivery queue. New
requests are denied, queued work stays paused without retry inflation, and
already-started external work is reported truthfully; a message sent before
shutdown cannot be recalled by toggling a switch. Re-enable and verify
resumption without duplicates or changed identifiers. In a multi-instance
setup, all instances observe the new policy and only ready instances serve
the newly enabled protocol; show any instance awaiting restart.

Count database resources before and after assistant-only bootstrap, first-user
creation, a chat turn, a widget refresh and a phone tour. All hosted mailbox,
calendar and address-book counts remain zero. Check that core file cleanup,
session expiry, auth rate limits, TLS and agent budgets continue working.

Complete fresh setup, interrupt it at each step, resume, and change an existing
installation's choices. Verify disabled diagnostics make no network calls.
Check layouts in all supported languages and the native app at the largest
text size. The operator should reach their first chat without seeing a mail
DNS checklist or being asked to assign an email address to a calendar.

## Idempotence and Recovery

A failed or repeated setup request must not duplicate accounts, credentials,
collections, domains or provider connections. Save complete validated choices
through the configuration store's normal concurrency mechanism. Keep generated
secrets and all existing identifiers unchanged.

Service disable is reversible. Data deletion is never an implicit part of it.
Persist why work is paused so a process restart cannot resume a disabled send.
Normal retention may continue for an enabled service according to its existing
policy; disabling must not trigger cleanup that purges preserved service data.
Explain the storage cost of keeping disabled services' data in the management
view and leave export/deletion to explicit actions.

The additive credential migration ships reverse SQL. Reverting that migration
removes the new credential feature, never the existing mailbox credentials or
calendar/contact content. Older binaries must not be rolled back against new
settings without checking their handling: they do not know service switches
and could start mail again. Plan a supported rollback that retains the patched
runtime gates, or explicitly stop the instance before a downgrade. Do not
promise that retaining configuration alone makes an old binary safe.

## Artifacts and Notes

Use a short shared availability table as the input to both setup and discovery.
Avoid a large plugin/module framework or database splitting. Keep service
requirements close to the operations and merged tool actions that need them,
with a test that fails when a new operation omits a requirement.

Product words remain the established words: mailbox rules, agent conduct,
personal instructions and operator house instructions. A disabled service is
not a denied grant. A reminders list is not an assistant schedule or the
conversation's task list. A provider connection is not an external mailbox.

## Interfaces and Dependencies

The configuration helpers resolve nullable legacy settings once. A shared
capability resolver accepts configured choices, running components and the
caller's permissions where needed. Core construction depends on neither
`mx.Exchange` nor `mailer.Mailer`; mail-specific consumers accept an absent
service and return the normal disabled-service error rather than dereference
nil interfaces.

Command entry points and jobs need the service policy explicitly or through
their established run/context interface; do not read process environment at
point of use. The tool registry carries service requirements at action level
so merged tools remain accurate. Reads and invocation both use this policy.
The setup status operation reports selected services, readiness and pending
restart separately, and exposes credentials only through existing one-time,
user-confirmed creation paths.


## Implementation checkpoint

Implemented nullable legacy service switches, selected/running capability intersection,
runtime construction and provisioning gates, paused mailbox jobs, scoped account sync
credentials, selective profiles, client navigation/polling gates and operator setup.
A real HTTP-only process bound only its web listener; creating its first account left
all hosted collection tables empty. A fake-provider assistant turn completed without
reading or provisioning disabled collections. Database-backed DAV scope, revocation,
disable/re-enable and queue-resume checks pass. Web typecheck/lint/tests/build and
283 Swift package tests pass. The macOS app build and final review remain outstanding.
Review found missing live IMAP checks and setup edits clearing completion. Existing
mail sessions now recheck service availability before operations. Identity-provider
account creation now obeys current and running mailbox availability. Setup edits
retain completion unless explicitly changed. A SwiftUI compiler timeout is addressed
by separating the presence task and the available screen expression.
REST templated sends, message reads, remote images and template/logo uploads now
check hosted mail availability before accessing dependencies. Already published
image bytes remain available independently, so previously sent messages keep their
images. Disabled mailboxes no longer authorize ordinary mailbox downloads.
The debug transport advertises its simulated capabilities so screenshots exercise
the intended screens rather than an unavailable-service placeholder.
Merged tool discovery now filters unavailable actions individually. Account and
phone-pairing actions remain offered while mail setup is omitted. A cached mail
action rechecks availability before execution, and re-enabling restores discovery.

The security follow-up updates Go to 1.26.9 and x/net to 0.60.0, updates fast-uri,
and applies a tested bounded-nesting guard to braces until a patched upstream release
exists. The Go scan reports no reachable vulnerabilities. The npm advisory still
reports braces by version despite the local guard; it has not been dismissed.

Meeting recording is being started in a separate checkout and feature branch. It
must not absorb this service migration or its dependency updates accidentally.
