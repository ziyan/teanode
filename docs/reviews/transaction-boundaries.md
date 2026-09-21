# Transaction boundaries during the refactoring

This inventory supports Milestones 2, 4 and 5 of
[the ExecPlan](../planning/main-refactoring-execplan.md). It describes the current
boundaries, not a claim that command atomicity is complete.

`db.Transaction.Commit()` commits the current SQL transaction and immediately
opens another using the same context. A later callback error can roll back only
the reopened transaction. Cancellation can therefore make reopening fail after
the preceding writes are durable. Callers must distinguish acceptance from work
that happens after acceptance.

## Manual commits

There are nine application calls to this method. Atomic file commits, version
metadata and the database implementation's own SQL commit are separate concerns.

| Location | Durable boundary | Work after the commit |
| --- | --- | --- |
| `apigraph.CreateDomain` in `internal/api/v1api/apigraph/domain.go` | New domain and generated signing key | DNS verification reads through another transaction; response assembly reads the saved domain. |
| `apigraph.RegenerateDomainKey` in the same file | Replacement signing key | DNS verification and response assembly. |
| `apigraph.ConnectAgentServer` in `internal/api/v1api/apigraph/agent_connection.go` | Connection and sealed credential | Probe the connected server, then record its status in another transaction. |
| `mx.handleIncoming`, authentication refusal, in `internal/mx/exchange_incoming.go` | Rejected mail record | Store message bytes, count refusal usage, return the authentication error. |
| `mx.handleIncoming`, acceptance, in the same file | Incoming mail record | Match aliases, create deliveries and file mailbox items. |
| `mx.handleOutgoing`, authentication refusal, in `internal/mx/exchange_outgoing.go` | Rejected outgoing mail record | Store message bytes, count refusal usage, return the authentication error. |
| `mx.handleOutgoing`, acceptance, in the same file | Outgoing mail and its Sent item when a mailbox is supplied | Create recipient deliveries and continue exchange acceptance. |
| `mx.handleDsn`, authentication refusal, in `internal/mx/exchange_bounce.go` | Rejected delivery-status mail record | Count refusal usage and return the authentication error. |
| `mx.handleDsn`, accepted report, in the same file | Delivery-status changes and the report's mail record | Count bounce usage and continue processing. |

The refusal commits intentionally preserve evidence while returning an error.
They must not become ordinary rollback-on-error commands. The accepted-mail
commits need a different treatment: submission identity and all local acceptance
records must share a defined durable boundary before safe retries can be offered.
Preserving the old sequence under a renamed method would not satisfy that goal.

## Nested work and connection pools

HTTP GraphQL currently commits the surrounding transaction even when a resolver
returns a GraphQL error; the agent adapter rolls back its surrounding transaction.
Neither behavior reverses an explicit commit or a separate inner transaction.
Keep existing multi-field partial-result behavior while moving each application
command's writes into its own tested boundary. Do not claim document-wide
atomicity.

`SendMailboxMessage` now uses the submission coordinator on the API transaction.
Composition, required byte persistence, mail, deliveries, Sent item and retry
identity share its command scope. Draft/flag reconciliation has a separate
savepoint and durable pending state, so its failure does not lose acceptance.
Dispatch wakes only after the enclosing transaction commits. Domain API sends
use the same transaction-bound composition and acceptance with a separate
principal-scoped identity; their requests have no mailbox reconciliation.

`SaveContact` and `DeleteContact` now use shared address-book commands on the
caller's transaction. Form merging locks and rereads the existing contact so
omitted fields reflect the latest committed version. `SaveAddressBook` now uses
the same command boundary, locks metadata and preserves the agent grant. The
agent grant adapter takes that same row lock before changing the setting, so
neither operation can overwrite the other's freshly committed fields.
Domain verification and connected-server probes perform further database reads
while the legacy `Commit()` has already reopened an outer transaction.

Websocket principal resolution now commits before subscription handlers begin.
The stream context carries the resolved principal and no completed transaction;
the handlers use their own short lookup transactions.

Do not introduce a small global connection-pool limit until the remaining nested
transaction calls and persistent notification listeners have been inventoried.
A caller holding one connection while waiting for another can otherwise stall a
bounded pool. This document records known cases; the complete lifecycle and
notification-listener audit remains part of the plan.

## Command scopes introduced during extraction

`internal/mailbox` folder commands accept either a database or an existing
transaction as their transaction scope. The latter uses SQL savepoints, including
release on success or after rollback. This preserves one connection and its locks
while allowing a failing command to undo all of its own writes. Nested commands
cannot commit the parent, and a failed cleanup prevents a later parent commit.
Cleanup has its own bounded context so cancellation of a shorter command does
not prevent rolling back its writes.

GraphQL folder adapters use this scope on their existing transaction. HTTP still
returns partial results and commits successful sibling fields; the agent adapter
still returns its document error to the outer transaction. A shared command's
failure is rolled back in both paths, but the adapters' treatment of successful
siblings is not yet unified. That remaining compatibility decision must be tested
before changing the agent adapter's document-wide behavior.

Submission cancellation uses its own coordinator command scope. It obtains the
same owner/identifier lock as acceptance, returns an already accepted submission
unchanged, or writes a durable cancellation. The send coordinator checks that
record before preparation. A rolled-back cancellation cannot block a later send;
a committed cancellation blocks even an original request that arrives afterward.
The API returns that result only after the request transaction commits.


Draft saving now delegates to `internal/mailbox.Commands.SaveDraft`, including
multipart uploads. Ownership and mail-write permission precede preparation.
Composition uses the caller transaction for media links; mail, Drafts item,
search indexing and replacement cleanup share the savepoint. Failed mandatory
storage or late cleanup restores the previous item and rolls back new metadata,
even if the caller handles the error and commits unrelated work. Stored bytes
are not deleted during rollback and follow normal retention. MIME-specific
preparation remains in the API adapter pending further command extraction.
