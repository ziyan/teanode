# An operator can sign in as another person, and is named in everything they do

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.


## Purpose / Big Picture

A person reports that their dashboard shows something wrong: a rule that does not fire, a folder that is missing, a page that breaks. Today the operator who looks after the server can only ask for screenshots, or reset the person's password to see for themselves, which takes the account away from its owner and leaves no record of who did what. After this change an operator who may manage accounts opens the Users page, chooses "Sign in as" on that person's row, and the dashboard reloads as that person, able to do exactly what they can, with a mark on the avatar at the foot of the rail. "Return to my account" in the account menu brings the operator back to their own session. Everything they change meanwhile is written in the audit log under the person's name and the operator's. The person can see the session in their own list of sessions, named as the operator's, and can end it.

To see it working: sign in as an operator, open Access → Users, choose "Sign in as" on another account, and observe the mark on the avatar and that the inbox and the agent are that person's; change something, open the audit log, and see the row naming both; choose "Return to my account" and observe the operator's own dashboard again, without signing in.


## Progress

- [x] (2026-10-02) Milestone 1: storage. Migration 0146 adds the impersonator to sessions and to audit events; models and database methods carry them.
- [x] (2026-10-02) Milestone 2: the server. The authenticator recognizes an impersonation session; the request knows who is behind it; the audit rows name them; StartImpersonation, EndImpersonation and the session state; logging out returns the operator; what an impersonation session may not do is refused.
- [x] (2026-10-02) Milestone 3: the dashboard. "Sign in as" on the Users page with a confirmation, the strip, the session list and the audit log name the operator; en, ja and zh.
- [x] (2026-10-02) Milestone 4: the command line and the documentation. `teanode session list` and `teanode audit` show the operator; the security review describes the guarantees.
- [x] (2026-10-02) Review, pull request, and a check in a browser on a running server.


## Surprises & Discoveries

- Observation: the schema names mutations by their Go method names (`RevokeSession`), not in lower camel case, so the refused list is written in that form and compared without regard to case.
  Evidence: `internal/client/session.go` sends `mutation { RevokeSession(...) }`.

- Observation: the server keeps only a hash of a session's secret, so the operator's own cookie cannot be written again at the end; it has to be kept by the browser meanwhile.
  Evidence: `models.Session` has no key; `database_session.go` stores `key_hash`.


## Decision Log

- Decision: impersonation is gated on the existing `user:manage` permission, plus the rule `SetUserPassword` already applies: the caller must hold every permission the person holds (`EffectivePermissions.Covers`).
  Rationale: whoever may set a person's password may already become them, so a separate permission would not narrow anything; and the covers rule stops an account manager from becoming an administrator by signing in as one. Date: 2026-10-02.

- Decision: the operator's own session is kept in a second cookie while impersonating, not re-created at the end.
  Rationale: the server stores only a hash of a session's secret, so it cannot write the operator's original cookie again. The browser holds it; a second HttpOnly cookie carries it for the hour, and ending the impersonation moves it back. Date: 2026-10-02.

- Decision: an impersonation session lasts one hour, ends when the operator's own session ends or the operator's account is disabled, and cannot start another impersonation.
  Rationale: a support visit, not a second login; accountability needs a live operator behind it, and a chain of impersonations would hide who is really acting. Date: 2026-10-02.

- Decision: while impersonating, the person's credentials and their agent are off limits: no password change, no passkey registered, renamed or removed, no API token or mail app password created or removed, no stored service credential changed, no program authorized through OAuth, no session of theirs revoked, and no agent conversation started, asked or confirmed.
  Rationale: a credential minted during the hour outlives it, which would turn a one-hour visit into a lasting login nobody sees; and the agent learns from what is said to it as the person, so an operator chatting as them would put words into the person's memory. The list is checked once, before any resolver runs, by the name of the mutation, so a resolver added later cannot forget it. Date: 2026-10-02.

- Decision (replaces the one above about credentials and the agent): an impersonation is view-only. Over GraphQL it may run queries and only the `EndImpersonation` and `Logout` mutations, collected through inline and named fragments; outside GraphQL it may send only reads, and never to the agent's tab or computer sockets; the dashboard does not send a mutation at all while it lasts; and the operator's permission to manage accounts, and to hold all that the person holds, is checked again on every request.
  Rationale: a security review found the list of refused mutations bypassed by a fragment (`mutation { ... on RootMutation { CreateToken } }`), the agent's MCP endpoint taking the session cookie with every tool, and lasting changes the list never named: a mail rule forwarding to the operator, a chat linked to the person's agent, a schedule that asks the agent after the hour, words saved to its memory. A list of what to refuse keeps missing what lasts; a list of what to allow cannot. The purpose is to see what the person sees, which needs no writes. The dashboard's own writes on load (presence, read marks, ideas seen, the person's time zone and language) would otherwise be said in the person's name. Date: 2026-10-02.

- Decision (replaces the view-only one above): the operator can do exactly what the person can, no more and no less. The owner asked for it: helping somebody solve a problem means doing what they would do. Every request is the person's, with the person's permissions and none of the operator's; the bounds stay (an hour, the operator's own session alive, the operator still allowed and still holding all that the person holds, checked on every request, no impersonation from inside one, a new sign-in in the same browser ends it); and accountability replaces refusal: every audit row written by a request in it, by an agent turn asked for in it, through the MCP endpoint or an upload, names the operator beside the person. What the page says on its own about the person being present is not sent, since they are not, and their time zone and language are not taken from the operator's browser. Date: 2026-10-02.

- Decision: the impersonation is shown by a mark on the avatar at the foot of the rail, and on the menu button on a phone, with the note and "Return to my account" in the account menu, not a strip across the page. The owner asked for it. Date: 2026-10-02.

- Decision (replaces the hour above): an impersonation has no time limit of its own; it lasts as long as the operator's own session and ends with it. The owner did not want one, nor a note in the account menu: the menu holds only "Return to my account". Date: 2026-10-02.

- Decision: no command-line or agent-tool way to start an impersonation.
  Rationale: impersonation is a browser session, carried by cookies; the command line authenticates with tokens, and a token is exactly what an impersonation may not mint. Parity is kept where it applies: the command line lists sessions and audit events with the operator named, as the dashboard does. Date: 2026-10-02.


## Outcomes & Retrospective

Built; made view-only after a security review; then, at the owner's request, given exactly what the person can do, with every write audited under both names, and the indicator moved into the account menu (see the Decision Log). Checked in headless Chrome at 1400 and 390 pixels, light and dark: starting from the Users page, the mark on the avatar and the phone's menu button, the menu with the way back, the person's session list naming the operator, and returning to the operator's own session. Tests: `internal/web/impersonation_test.go` (acting as the person and returning; ending with the operator's session or account; starting only from the operator's own session; logging out returns; a new sign-in ends it; the middleware header and a forged one; reaching what the person reaches outside GraphQL), `internal/api/v1api/apigraph/impersonation_test.go` (the permission and covers gate; a change the person may make goes through and one they may not is refused; the audit row naming both; an operator who loses permissions cut off), the dashboard tests. 


## Context and Orientation

TeaNode is a mail server with a web dashboard, written in Go with a React dashboard. Its HTTP API is GraphQL, built by reflection from Go interfaces in `internal/api/v1api/apigraph/` (there are no schema files: each interface method becomes a query or mutation, and its doc comment becomes the description, generated into `internal/util/commentparse/commentparse_gen.go` by `go generate ./internal/util/commentparse`).

A browser signs in to a session, a row in the `session` table (`internal/models/session.go`, `internal/db/database_session.go`). The cookie `teanode_session` carries the row's identifier and a secret; only a SHA-256 hash of the secret is stored. `internal/web/session.go` is the authenticator: `authenticate` reads a bearer token or the cookie, resolves the session, and returns the username. `internal/web/auth_middleware.go` puts that username in an internal request header, `api.AuthenticatedUsernameHeader`, after deleting any copy the client sent. `internal/api/v1api/apigraph/graph.go` reads the header, loads the account, and builds the request context: the authenticated username, the audit principal (who audit rows will name), and for mutations the principal (the account and its effective permissions, `internal/access/principal.go`). Resolvers check permissions with helpers in `apigraph/authorize.go`.

An audit event (`internal/models/audit.go`, table `audit_event`) is written by `db.applyMutation` in the same transaction as the change, naming the actor from `db.AuditPrincipal` in the context (`internal/db/database_audit.go`).

Permissions are listed in `internal/models/permission.go`; `user:manage` is the permission to manage accounts. `EffectivePermissions.Covers(other)` says whether one set of grants includes another.

The dashboard loads the session state with `getSession()` in `web/src/api.ts` and provides it through `SessionProvider` (`web/src/app.tsx`, `web/src/session.tsx`). The Users page is `web/src/pages/access/users.tsx`; sessions are listed on `web/src/pages/settings/sessions.tsx`. The strip goes at the top of `<main className="content">` in `web/src/app.tsx`, beside `PasskeyNudge`. Message catalogues are `web/src/i18n/en.ts`, `ja.ts`, `zh.ts`; English is the source of truth.

Migrations are numbered SQL files in `internal/db/migrations/`, each with a `.reverse.sql`; the next free number is 0146.


## Plan of Work

Milestone 1 adds two nullable columns to `session`, `impersonator_user_id` and `impersonator_session_id`, and one to `audit_event`, `impersonator_user_id`, in `internal/db/migrations/0146_impersonation.sql` with its reverse. `models.Session` gains `ImpersonatorUserID`, `ImpersonatorSessionID` and a looked-up `ImpersonatorUsername`; `models.AuditEvent` gains `ImpersonatorUserID`. `CreateSession`, `GetSession`, `ListSessions` and the audit write and read carry them; `db.AuditPrincipal` gains `ImpersonatorUserID`.

Milestone 2 changes `authenticate` in `internal/web/session.go` so that a session with an impersonator is accepted only while the impersonator's account exists and is enabled and the impersonator's own session is active, and returns the impersonator's username as well. The middleware puts it in a second internal header, `api.ImpersonatorUsernameHeader`, deleted from incoming requests like the first. `graph.go` loads that account, checks again that the operator may manage accounts and holds all that the person holds, puts it on the principal (`access.Principal.Impersonator`) and in the audit principal, and refuses any mutation but `EndImpersonation` and `Logout`, with fields collected through fragments, before running anything. The middleware lets an impersonation send only reads outside GraphQL, and nothing to the agent's tab or computer sockets. `apigraph/impersonation.go` adds `StartImpersonation(userId)`, which checks the gate, refuses nested impersonation, a caller not using a browser session, the console, the caller themselves and a disabled account, creates a one-hour session for the person with the impersonator on it, moves the caller's cookie into `teanode_session_return` and sets the new one; and `EndImpersonation`, which revokes the impersonation session and moves the operator's cookie back. `GetSession` reports the impersonator and when the session ends. Logging out while impersonating does what ending does.

Milestone 3 adds "Sign in as" to each row of the Users page the operator may act on, the strip with the person's name, the operator's name, the end time and "Return to my account", and the operator's name on impersonation sessions in the session list and on audit rows.

Milestone 4 adds the operator to `teanode session list` and `teanode audit` output, and documents the feature in `docs/reference/` where sessions are described.


## Concrete Steps

From the repository root:

    go build ./... && go vet ./internal/...
    TEANODE_TEST_DATABASE_HOST=<postgres address> go test -mod=vendor ./internal/web/ ./internal/api/v1api/apigraph/ ./internal/db/ -count=1
    cd web && npx tsc --noEmit && npx vitest run src/pages/access
    make lint-ci


## Validation and Acceptance

Tests in `internal/api/v1api/apigraph/` show: an account manager starts an impersonation of a member and the next request is the member's; an account manager cannot impersonate an administrator; a member cannot impersonate anybody; an impersonation cannot start another; a mutation in the refused list fails while impersonating and works otherwise; an audit row written while impersonating names both accounts; ending restores the operator's cookie. Tests in `internal/web/` show that an impersonation session stops authenticating when the operator's own session is revoked, when the operator's account is disabled, and after its hour.


## Idempotence and Recovery

The migration only adds nullable columns; its reverse drops them. Nothing existing changes meaning: a session without an impersonator behaves exactly as before.


## Interfaces and Dependencies

In `internal/api/v1api/apigraph/impersonation.go`:

    type ImpersonationMutation interface {
        StartImpersonation(ctx context.Context, arguments StartImpersonationArguments) (*SessionState, error)
        EndImpersonation(ctx context.Context) (*SessionState, error)
    }

`SessionState` gains `ImpersonatorUsername string`.
