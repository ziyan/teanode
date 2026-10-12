# Hosted services are independent of the personal assistant

- Status: accepted
- Date: 2026-10-12
- Deciders: project maintainers

## Context

Startup, provisioning, navigation and device sync assumed hosted mail even
though the assistant already belongs to an account. Operators need chat and
attached computers without an email domain or SMTP ports.

## Decision

Keep routing, mailboxes, event calendars, reminders and contacts in the
configuration store's services section. Omitted switches preserve existing
installations; new assistant seeds explicitly disable hosted services.
Mailboxes require routing. Collections and the assistant remain independent.
Expose a stable schema and authenticated capabilities. Until restart,
availability is the intersection of selected and constructed services.

Check availability before provisioning, operations and background work.
Retain data and grants on disabling. Keep mailbox jobs queued and defer
claimed work without counting failures. Account-owned sync credentials grant
only selected collections and cannot authenticate mail or the dashboard.
Existing mailbox passwords retain their behavior for enabled collections.
Profiles contain selected payloads and remain sealed until one download.

## Consequences

Enabling requires a restart. Operators retain the settings needed to re-enable.
Local events need no mail; invitations are refused before a write when no
hosted sender can send them. Memory already copied from disabled collections
is retained and can be explicitly forgotten. Connected tools remain independent.
Account preferences for hiding enabled services remain future work.

Implementation and acceptance: [ExecPlan](../planning/optional-services-execplan.md).
