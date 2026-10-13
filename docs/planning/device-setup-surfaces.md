# Device setup surfaces execution plan

This plan supplements `device-setup-execplan.md`. This independent stream owns
web setup, CLI entry points and the existing phone profile handoff tool. Backend
profile composition and shared client contracts remain in the backend stream;
iOS implements the same account-wide selection through the native stream.

## Purpose and agreed behavior

One available-service selector chooses Mail, Calendar, Reminders and Contacts
before creating a short-lived profile. The account's Device setup page has a
prominent existing iPhone/Mac shortcut. Mail programs opens that same selector
with its owned mailbox preselected. Collection-only setup requires no mailbox.

Available Calendar, Reminders and Contacts are selected when the explicit setup
dialog opens. Generic account setup leaves Mail off until an owned mailbox is
chosen; a mailbox shortcut selects only its named Mail mailbox. Every selection
remains visible and editable before profile creation. Unavailable services are
omitted. This is coordinated with the native entry point.

Mail keeps its mailbox app password. Collections keep their separate scoped
account sync credential. Revocation and manual setup remain existing secondary
paths. The LLM tool hands off to the setup page without receiving credentials
or profile links. Preserve its published action alias and add general device
wording where applicable. No new catalog noun or permission policy is needed.

## Progress

- [x] Read repository conventions, existing setup paths and additive API contracts.
- [x] Create an isolated branch from the reviewed backend checkpoint.
- [x] Coordinate selection semantics and entry meaning with the native stream.
- [x] Add one standard web dialog and profile receipt, with available-service scopes.
- [x] Route mailbox shortcuts into the account-wide selector and retain manual setup.
- [x] Extend existing CLI sync commands and tool handoff guidance.
- [x] Review ownership, unavailable selections, refresh/account isolation and errors.
- [ ] Run meaningful tests, full naming lint and CI on the exact branch head.
- [x] Inspect actual Chrome desktop and phone interactions, including mailbox-free setup.
- [ ] Open a separate draft PR with diagrams and current validation evidence.

## Validation and acceptance

Use existing SettingsSection, FormDialog, SecretDialog/profile receipt, button,
Tooltip and icon patterns. Do not introduce a style system. Test mailbox-free
scopes, owned mailbox selection, capability removal after refresh, account
changes, empty/invalid selections and explicit CLI booleans. Verify profile
metadata never exposes either credential secret, and the tool only hands off.

The backend already tests exact selected payloads, one-use/expiry delivery,
transaction rollback, separate credential authentication/revocation and owner
refusals. This stream will use those contracts without duplicating policy.
Actual Chrome uses desktop and 390-pixel phone layouts. Profile installation
on a physical Apple device and native validation are reported separately.

## Boundaries and next steps

Recording work remains on its existing PR and is not included here. Keep shared
backend and native changes in their own reviewable increments. Do not claim
deployment or installation from compilation. The root stream reviews, merges
and deploys verified merged main after all required checks pass.

## Review and validation evidence

The unified web flow, explicit CLI service flags and device tool handoff are implemented. Existing phone-mail action naming remains supported. Root-owned API permissions and account/mailbox scope boundaries remain unchanged. The native stream implements the same selector in a separate increment.

Chrome exposed a receipt-loss bug: an explicit credential reload replaced the page with Loading and unmounted the one-time profile dialog. The page now retains loaded data during refresh, and a parent-page regression holds the request pending while asserting the receipt remains visible. A second review added an explanation inside the selector when every service becomes unavailable.

An isolated collection-only account showed Calendar, Reminders and Contacts selected without a Mail picker. A later owned-mailbox fixture exercised generic Mail off, explicit Mail selection with a required owned mailbox, and the mailbox shortcut opening the same selector with that mailbox preselected. Chrome inspected the layouts at measured CSS widths of 1440 and 390, including empty selection, receipt copy and dismissal. The browser already used 150 percent zoom on that local origin; viewport overrides accounted for it and DOM widths were checked rather than assuming screenshot dimensions.

Profile receipt and error interactions used a clearly synthetic loopback fixture that refuses real credential creation and returns a nonfunctional receipt. These checks establish rendering and selection behavior, not actual credential issuance or physical installation. A direct authenticated request against the real isolated backend refused collection profile creation over plain HTTP and left the credential list unchanged. Backend HTTPS, selected-payload, one-use, ownership and rollback evidence belongs to the separately reviewed backend tests.

Final full naming lint, zero-warning dashboard lint, typecheck, all 450 dashboard tests plus 2 dependency tests, 6 focused selector/receipt regressions, production/extension builds and focused Go race/catalog tests passed. A transient shared temporary-filesystem exhaustion interrupted one build; a successful rerun completed after capacity returned without deleting shared artifacts. Exact-head CI remains pending; no merged/deployed or physical-device claim is made.
