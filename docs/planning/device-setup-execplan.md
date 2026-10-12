# One device setup flow

This ExecPlan is a living document. Keep progress, decisions, discoveries and
validation current. This is independent of passive meeting recordings.

## Purpose

A person chooses which available Mail, Calendar, Reminders and Contacts services
to add to a device, then opens one short-lived setup profile. The existing profile
shortcut is the primary action. An account without a mailbox has the same setup
path for collections. Manual credentials remain available as secondary controls.

## Progress

- [x] Inventory the existing web, native, CLI and tool entry points.
- [x] Identify existing authorization, service availability and credential boundaries.
- [ ] Agree one additive shared profile contract and selectable service capabilities.
- [ ] Implement profile composition, preserving existing credential/revocation handling.
- [ ] Implement one coherent web and native entry point and available-service selector.
- [ ] Add applicable CLI/tool parity and migrate confusing shortcut copy/navigation.
- [ ] Review and fix ownership, disabled-service, partial-failure and credential findings.
- [ ] Inspect actual Chrome desktop/mobile and native light/dark/largest-text layouts.
- [ ] Run meaningful tests and CI, merge reviewed increments and deploy merged main.

## Discoveries and decisions

Existing mailbox profiles create a mailbox app password and automatically include
available DAV payloads. Existing account sync profiles select Calendar, Reminders
and Contacts, but use a separate, less prominent setup entry point. The tool's
profile guidance currently points only to mailbox settings.

Reuse the existing sealed, one-download profile delivery and existing Apple plist
writer. Mail keeps mailbox app-password authorization and revocation. Collection
sync keeps account-owned scope credentials and cannot authenticate mail or the
web dashboard. A combined profile must carry each credential in its own payload;
a shared UI does not widen either credential's authority. Calendar and Reminders
share the CalDAV transport while the server credential scopes remain distinct.

Keep existing APIs compatible. Use an additive unified setup operation if needed;
compose only selected, available payloads. Resolve every selection and access check
before creating credentials, and retain existing transactional rollback behavior.
Do not show a Mail selection when no usable mailbox exists. Do not require a
mailbox to configure collections. Do not create a second component/style system.

## Acceptance and validation

Test mail-only, collection-only and mixed profiles with exact payloads and separate
credentials. Refuse unavailable selections, foreign account access, impersonation
where existing sync rules forbid it, insecure downloads and empty selections.
Verify one-time download/expiry, rollback when profile generation fails and that
revoking a sync credential cannot revoke or authenticate mailbox access. Preserve
old mailbox/sync profile contracts and existing clients.

Use the repository's navigation, forms, buttons and icon conventions. Inspect both
Chrome viewport layouts with actual interactions and native simulator captures;
report physical-device installation checks separately. Merge only reviewed, green
increments and verify deployed commit, health and authenticated setup semantics.
