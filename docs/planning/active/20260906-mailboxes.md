# Mailboxes: users, folders, IMAP, and the rest of a mail system

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. It is maintained in accordance with `~/.claude/PLAN.md`.

This is a programme rather than a feature: it is too large to implement from
one document. It fixes the design — above all the data model — and divides the
work into milestones that are each independently shippable. Each milestone
becomes its own ExecPlan when it starts, incorporating this one by reference.
Nothing here is implemented yet.

## Purpose / Big Picture

Today this server receives mail and *hands it on*: every message is forwarded
to an address, relayed to another server, or posted to a webhook, and the
dashboard is an operator's window onto what passed through. Nobody *reads
their mail here*. There is one kind of account — an operator who can do
everything — and the mail a message became is a delivery record, not a thing
in anyone's possession.

After this programme, this server is somewhere mail *lives*. A person signs in
— with a password, a passkey, or their organisation's identity provider — and
has an inbox. Messages for their addresses land in it, without being copied.
They read, reply, reply to all, forward, file into folders they made, search,
archive, delete, and set rules so the inbox sorts itself. They read the same
mailbox from a phone over IMAP. And an administrator decides who may do what:
which people are in which groups, which groups hold which roles, which roles
carry which permissions — over the whole server, or over one domain.

You can see it working like this: create a domain, give a user an address on
it, send that address a message from outside. It appears in their inbox in the
dashboard and in their mail app over IMAP, unread, in a thread with anything
it replied to. They reply from the dashboard; the reply is in their Sent
folder, in the same thread, and the recipient receives it DKIM-signed. A rule
they wrote files the next newsletter into a folder before they see it.

## What is already here, and what this builds on

Read these before designing anything further; the plan relies on them.

**Mail is stored once.** `internal/models/mail.go` is a received or sent
message — envelope, headers, authentication results, spam score — as a row in
the `mail` table, with the raw message in the spool (`internal/storage`, a
directory with an optional S3 mirror). Everything in this programme
*references* that row and that stored message. Nothing copies them. The one
place that copies today — a submission to a local address in
`internal/mx/exchange_outgoing.go` creates a second `mail` row per recipient
domain — is replaced by a reference in milestone two.

**Delivery is a separate record.** `internal/models/delivery.go` is one
attempt to hand a message on: to whom, how, with what result. An alias
(`config.Alias`, kind `email`, `mailServer` or `webhook`) decides what happens
to a message. Milestone two adds a fourth kind, `mailbox`, whose delivery is a
row in a folder.

**Users exist, roles do not.** The `user` table (migration `0004_user.sql`)
holds username, name, password hash and a notification email. Every user is an
operator: `requireOperator` in `internal/api/v1api/apigraph/authorize.go` asks
only whether somebody is signed in. Sessions, API tokens and passkeys already
work and are untouched by this plan; the authorisation layer above them is
what changes.

**Retention prunes by age.** `internal/storage/filesystem.go` sweeps messages
older than `storage.spoolRetention` out of the spool, and the `mail` row
follows. A mailbox cannot live on a spool that forgets things after thirty
days. Retention changes meaning in milestone two: a message is kept as long as
any folder holds it, and for the retention period after the last folder lets
go.

**The dashboard can already compose and send.** `web/src/pages/compose.tsx`
and `SendMail` in `internal/api/v1api/apigraph/send.go` write and send a
message with attachments, and the result is a `mail` row of kind outgoing.
Reply, reply to all and forward are that page with the recipients, subject,
quoted body and threading headers filled in.

**Everything is shared through PostgreSQL.** This server runs as several
instances against one database, and the object store is optional. Every table
this plan adds is shared state; nothing lives on one instance's disk, and
anything one instance must tell another — a new message for a mailbox somebody
is watching over IMAP — goes through the database.

## Several instances, one mailbox

This server runs as several instances against one PostgreSQL, and this
programme is judged against that on every milestone. A person's mailbox is
one thing however many instances serve it; a message arriving at instance A
is readable over IMAP from instance B a moment later; and no instance holds
anything in memory that another one would need. Concretely:

- **Every table above is shared state.** Nothing lives in a directory on one
  instance — no rule files, no index, no session state for IMAP. The
  precedent is the spam filter's rule sets, which went into the database for
  exactly this reason.
- **The bytes of a message live where it arrived.** The spool is a directory
  on the instance that received the message, mirrored to the object store
  when one is configured, and `storage.Get` already falls back to the mirror
  when the local spool has no copy. A mailbox read from any instance
  therefore **requires the object store in a cluster** — as reading a message
  in the dashboard from any instance already does. A single instance needs
  nothing beyond its spool. The deployment guide says so in milestone two.
- **UIDs and `modseq` are allocated in the database, in the transaction that
  needs them:** `UPDATE folder SET uid_next = uid_next + 1 … RETURNING`, never
  a counter in memory. Two instances adding to one folder at once get two
  different UIDs, and IMAP's promise that they only ever grow holds.
- **The retention sweep is one instance's job at a time.** It takes a
  PostgreSQL advisory lock before deciding what is unreferenced; the others
  skip the tick. Without that, two sweeps racing a new item could each
  decide the message is unreferenced.
- **IDLE crosses instances through the database.** The instance that changes
  a folder issues `NOTIFY` in the same transaction; every instance `LISTEN`s.
  A client idling on instance B is woken by a message instance A stored.
- **Effective permissions are computed per request from the database**,
  never cached across requests on an instance, so a role change made through
  instance A applies to instance B's next request. If that ever costs too
  much, the cache is keyed on a version the way the configuration's is — not
  on time.
- **The OIDC callback may land on a different instance than the one that
  started the sign-in.** Everything the callback needs — state, nonce, PKCE
  verifier, where to return to — travels in a signed, expiring blob, not in
  an instance's memory.
- **The rate limiter is per instance**, as it is for SMTP today: an attacker
  reaching every instance gets that many budgets. Acceptable, and stated,
  rather than solved with shared counters that would slow every sign-in.

## Terms

A **mailbox** is a container of folders that belongs to one user. A person
gets one when their account is made and may have more. An **address** is
what delivers into a mailbox: `support@example.com` delivers into whichever
mailbox claims it. A shared inbox — several people reading one mailbox — is
not in this plan (see the open decisions). A **folder** is a named place inside
a mailbox, nested as deep as its owner likes; Inbox, Sent, Drafts, Archive,
Junk and Trash are folders with a fixed kind. A **mailbox item** is one
message in one folder, with its flags: read or unread, flagged, answered. The
same message can be an item in several mailboxes — a message to two people
here is one `mail` row and two items — and moving it is changing which folder
the item is in.

A **permission** is a named thing somebody may do, written `resource:verb`. A
**role** is a named set of permissions. A **group** is a named set of users,
and it is the only thing a role or a domain is attached to: a user in a group
holds the group's roles over the group's domains. Nothing is attached to a
user directly. A user's **effective permissions** are the union over every
group they are in — additive, never subtractive — resolved once per request.
Every permission has a **kind**, declared with it: **server** permissions
(users, groups, roles, the queue, the server itself) apply whatever domains
a group has; **domain** permissions apply only over the group's domains; and
**all-domains** permissions are the same verb over every domain, present and
future — `domain:manage-all`, `mail:audit-all` — which is how the
administrators reach a domain created tomorrow without anyone listing it.

**IMAP** is the protocol mail programs read a mailbox with; each folder is an
IMAP mailbox, each item a message with a **UID** that never changes while it
stays in that folder. **SSO** here means signing in through an organisation's
identity provider using **OIDC**; the provider says who the person is and
which of its groups they are in, and this server maps those onto its own.

## Data model

Two layers, as everywhere in this repository: `internal/models/` holds the
domain type with `json` tags; `internal/db/database_*.go` holds a matching
GORM model with `gorm` tags and the conversion, following
`internal/db/database_mail.go`. Every table below arrives by a migration in
`internal/db/migrations/` with its `.reverse.sql`, per
`docs/coding/database-migrations.md`. Identifiers are 32-character ULIDs like
every existing table.

### Access control

Permissions are a vocabulary in Go, not a table, so that a permission the code
does not know cannot be granted and a row naming one the code has forgotten is
ignored rather than fatal:

    package models

    type Permission string

    const (
        PermissionMailRead        Permission = "mail:read"         // read one's own mailboxes
        PermissionMailWrite       Permission = "mail:write"        // flag, move, delete in them
        PermissionMailSend        Permission = "mail:send"         // send as an address of them
        PermissionMailboxManage   Permission = "mailbox:manage"    // folders, rules, addresses of them
        PermissionMailAudit       Permission = "mail:audit"        // every message of the group's domains, as the operator sees it today
        PermissionMailAuditAll    Permission = "mail:audit-all"    // the same, over every domain
        PermissionDomainManage    Permission = "domain:manage"     // the group's domains: aliases, credentials, templates
        PermissionDomainManageAll Permission = "domain:manage-all" // every domain, present and future, and creating new ones
        PermissionReportRead      Permission = "report:read"
        PermissionUserManage      Permission = "user:manage"
        PermissionGroupManage     Permission = "group:manage"
        PermissionRoleManage      Permission = "role:manage"
        PermissionServerManage    Permission = "server:manage"     // settings, upgrades, certificates
        PermissionAuditRead       Permission = "audit:read"        // the audit log
    )

    // PermissionKind is where a permission applies, declared with the
    // vocabulary: there is no flag on a group, the permission itself says how
    // far it reaches.
    type PermissionKind string

    const (
        PermissionKindServer     PermissionKind = "server"      // everywhere, whatever domains the group has
        PermissionKindDomain     PermissionKind = "domain"      // over the group's domains only
        PermissionKindAllDomains PermissionKind = "all-domains" // over every domain, present and future; group_domain is not consulted
    )

    // Kind is the permission's reach. A key the code does not know has no kind
    // and grants nothing.
    func (self Permission) Kind() PermissionKind

    // Widens is, for an all-domains permission, the domain permission it stands
    // in for on every domain: "domain:manage-all" widens "domain:manage".
    func (self Permission) Widens() Permission

Tables:

    CREATE TABLE "role" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "name"        varchar(128) NOT NULL,
        "description" text         NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "role_name" ON "role" (lower("name"));

    CREATE TABLE "role_permission" (
        "role_id"        varchar(32) NOT NULL REFERENCES "role"("id") ON DELETE CASCADE,
        "permission_key" varchar(64) NOT NULL,
        PRIMARY KEY ("role_id", "permission_key")
    );

    CREATE TABLE "group" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "name"        varchar(128) NOT NULL,
        "description" text         NOT NULL DEFAULT '',
        -- The group's name at the identity provider, when SSO fills it.
        "idp_group"   varchar(256) NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "group_name" ON "group" (lower("name"));

    -- Membership: the many-to-many between users and groups, nothing more.
    CREATE TABLE "user_group" (
        "user_id"  varchar(32) NOT NULL REFERENCES "user"("id")  ON DELETE CASCADE,
        "group_id" varchar(32) NOT NULL REFERENCES "group"("id") ON DELETE CASCADE,
        PRIMARY KEY ("user_id", "group_id")
    );
    CREATE INDEX "user_group_group" ON "user_group" ("group_id");

    -- A group's roles: every user in the group holds them.
    CREATE TABLE "group_role" (
        "group_id" varchar(32) NOT NULL REFERENCES "group"("id") ON DELETE CASCADE,
        "role_id"  varchar(32) NOT NULL REFERENCES "role"("id")  ON DELETE CASCADE,
        PRIMARY KEY ("group_id", "role_id")
    );

    -- A group's domains: the ones its roles' domain-kind permissions apply
    -- to. All-domains permissions do not look here.
    CREATE TABLE "group_domain" (
        "group_id"  varchar(32) NOT NULL REFERENCES "group"("id")  ON DELETE CASCADE,
        "domain_id" varchar(32) NOT NULL REFERENCES "domain"("id") ON DELETE CASCADE,
        PRIMARY KEY ("group_id", "domain_id")
    );
    CREATE INDEX "group_domain_domain" ON "group_domain" ("domain_id");

Three roles are seeded by the migration and are ordinary rows from then on:
renamed, edited, deleted, or left alone like any role an administrator makes.
There is no built-in flag and nothing a seeded role may not do.
**Administrator** holds every permission; **Operator** holds everything except
`user:manage`, `group:manage` and `role:manage`, which is what every user is
today; **Member** holds `mail:read`, `mail:write`, `mail:send` and
`mailbox:manage`, which is what a person with an inbox and nothing else
needs. The migration creates a group **Administrators**, puts every existing
user in it and binds Administrator to it — so the day this lands, nobody can
do less than they could the day before. There is no guard against an
administrator editing themselves out: that is an accepted risk. The way back
is `teanode-server rescue <username>`, run on the host against the database
with no permission check — whoever holds the database holds everything
anyway — which puts the user into a group holding every permission,
recreating Administrators and Administrator if they were deleted.

Effective permissions are computed once per request in the authentication
layer and carried on the context, as
`api.ContextEffectivePermissions(ctx)`: the user's groups, each group's roles
and each group's domains, crossed and unioned by kind: server and
all-domains permissions land in `Everywhere`, domain permissions in
`ByDomain` under each of the group's domains. So `user:manage` in a group
with one domain still manages every user, `mail:audit` in that group reads
only that domain's mail, and `mail:audit-all` reads all of it.
`requireDomainPermission(ctx, "domain:manage", id)` passes on either
`ByDomain[id]["domain:manage"]` or `Everywhere["domain:manage-all"]`. `requireOperator` is replaced by
`requirePermission(ctx, permission)` and `requireDomainPermission(ctx,
permission, domainId)`. A resolver that finds the caller lacks permission over
a row answers **not found**, never "forbidden": "you may not touch this"
confirms the row exists, which is itself a leak. The dashboard is told the
caller's effective permissions in `GetSession` and hides what they cannot do;
that is a courtesy, and every mutation is checked again on the server.

The `user` table gains:

    ALTER TABLE "user"
        ADD COLUMN "disabled_at"    timestamptz,                 -- a disabled user cannot sign in, keeps their mail
        ADD COLUMN "locale"         varchar(16)  NOT NULL DEFAULT '';
    ALTER TABLE "user" ALTER COLUMN "password_hash" DROP NOT NULL;

A user with no password hash signs in only with a passkey or through SSO.
`Email` on the user stays what it is — where the server notifies them — and
is not their mailbox address; a mailbox may have several.

### Mailboxes, folders, items

    CREATE TABLE "mailbox" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "user_id"     varchar(32)  NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
        "name"        varchar(128) NOT NULL,   -- "Personal"; a user may have several
        "signature_html" text      NOT NULL DEFAULT '',
        "signature_text" text      NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE INDEX "mailbox_user" ON "mailbox" ("user_id");

    -- Which addresses deliver into a mailbox. A domain's alias of kind
    -- "mailbox" points here; this table is what the alias resolves to and
    -- what "send as" is checked against.
    CREATE TABLE "mailbox_address" (
        "mailbox_id" varchar(32)  NOT NULL REFERENCES "mailbox"("id") ON DELETE CASCADE,
        "domain_id"  varchar(32)  NOT NULL,
        "local_part" varchar(64)  NOT NULL,
        "primary"    boolean      NOT NULL DEFAULT false,
        PRIMARY KEY ("domain_id", "local_part")
    );

    CREATE TABLE "mailbox_folder" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "mailbox_id"  varchar(32)  NOT NULL REFERENCES "mailbox"("id") ON DELETE CASCADE,
        "parent_id"   varchar(32),                      -- NULL at the top
        "name"        varchar(128) NOT NULL,
        "kind"        varchar(16)  NOT NULL DEFAULT '', -- inbox, sent, drafts, archive, junk, trash, or '' for one the owner made
        -- IMAP's contract: UIDs in a folder only ever grow, and a folder
        -- that is recreated announces itself with a new validity.
        "uid_validity" bigint      NOT NULL,
        "uid_next"     bigint      NOT NULL DEFAULT 1,
        -- Grows on every change to the folder's items, for clients that
        -- ask "what changed since". Also what IMAP IDLE watches.
        "modseq"       bigint      NOT NULL DEFAULT 1,
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "mailbox_folder_name" ON "mailbox_folder" ("mailbox_id", COALESCE("parent_id", ''), lower("name"));
    CREATE UNIQUE INDEX "mailbox_folder_kind" ON "mailbox_folder" ("mailbox_id", "kind") WHERE "kind" <> '';

    -- One message in one folder. The message is the existing mail row; this
    -- is the possession of it, with its flags. The same mail can be an item
    -- in many folders of many mailboxes.
    CREATE TABLE "mailbox_item" (
        "id"          varchar(32) NOT NULL,
        "folder_id"   varchar(32) NOT NULL REFERENCES "mailbox_folder"("id") ON DELETE CASCADE,
        "mail_id"     varchar(32) NOT NULL REFERENCES "mail"("id")   ON DELETE CASCADE,
        "uid"         bigint      NOT NULL,
        "modseq"      bigint      NOT NULL,
        "seen"        boolean     NOT NULL DEFAULT false,
        "flagged"     boolean     NOT NULL DEFAULT false,
        "answered"    boolean     NOT NULL DEFAULT false,
        "forwarded"   boolean     NOT NULL DEFAULT false,
        "draft"       boolean     NOT NULL DEFAULT false,
        "added_at"    timestamptz NOT NULL,
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "mailbox_item_uid"  ON "mailbox_item" ("folder_id", "uid");
    CREATE INDEX        "mailbox_item_mail" ON "mailbox_item" ("mail_id");
    CREATE INDEX        "mailbox_item_list" ON "mailbox_item" ("folder_id", "added_at" DESC);

Moving a message between folders is: insert an item in the new folder with
that folder's next UID — taken with `UPDATE … SET uid_next = uid_next + 1
RETURNING`, so two instances cannot hand out the same one — delete the old
item, bump both folders' `modseq`, in one transaction. That is what IMAP `MOVE` means and it keeps the promise that
a UID never changes while the message stays put. Deleting is moving to
Trash; emptying Trash deletes items. The `mail` row and the stored message
are touched by neither — retention takes them once nothing holds them.

The `mail` table gains two columns, filled at receipt:

    ALTER TABLE "mail"
        -- The conversation this message is part of, derived from
        -- In-Reply-To and References; the root message's own id when it
        -- starts one. What "show me the thread" reads.
        ADD COLUMN "thread_id" varchar(32) NOT NULL DEFAULT '',
        -- Subject, sender, recipients and the message's text, as a search
        -- document. The body itself stays in the spool; this is what full
        -- text search runs over, bounded to the first 64 KB of text.
        ADD COLUMN "search"    tsvector;
    CREATE INDEX "mail_thread" ON "mail" ("thread_id");
    CREATE INDEX "mail_search" ON "mail" USING gin ("search");

### Rules

    -- Rules are few, per-mailbox, and edited as a whole, so they are a column
    -- on the mailbox rather than a table of their own.
    ALTER TABLE "mailbox" ADD COLUMN "rules" jsonb NOT NULL DEFAULT '[]';  -- [MailboxRule], run in array order

A rule runs when a message is added to a mailbox's Inbox, in the order of
the array, and again by hand on a folder when the owner asks. Conditions
match on from, to, subject, a header, the spam score, whether the sender is
in the owner's contacts, or "everything". Actions move to a folder, mark
read, flag, forward to an address (a delivery of kind `forward`, so it is
signed and recorded like any other), or delete. A rule that forwards is the
one that needs `mail:send` on the mailbox, and is checked for it when saved
and when run. Saving rules replaces the array: one row write, no positions
to renumber.

### Signing in from a mail program, and from an identity provider

    -- A password a mail program uses; one per device, revocable one at a
    -- time. Passkeys cannot speak IMAP, and the account password should
    -- not sit in a phone's keychain.
    CREATE TABLE "mailbox_app_password" (
        "id"            varchar(32)  NOT NULL,
        "created_at"    timestamptz  NOT NULL,
        "mailbox_id"    varchar(32)  NOT NULL REFERENCES "mailbox"("id") ON DELETE CASCADE,
        "name"          varchar(128) NOT NULL,   -- "phone", "laptop"
        "password_hash" varchar(128) NOT NULL,
        "last_used_at"  timestamptz,
        PRIMARY KEY ("id")
    );

    -- Who a user is at an identity provider. A row per provider, so a
    -- person can arrive through more than one and so a provider can be
    -- changed without inventing a new user. Written by the SSO milestone,
    -- created now so that milestone changes no existing table.
    CREATE TABLE "identity" (
        "id"            varchar(32)  NOT NULL,
        "created_at"    timestamptz  NOT NULL,
        "user_id"       varchar(32)  NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
        "provider"      varchar(64)  NOT NULL,   -- the provider's configured name
        "subject"       varchar(256) NOT NULL,   -- the provider's stable id for them
        "email"         varchar(320) NOT NULL DEFAULT '',
        "last_login_at" timestamptz,
        PRIMARY KEY ("id")
    );
    CREATE UNIQUE INDEX "identity_subject" ON "identity" ("provider", "subject");

### Audit

    -- One row per administrative mutation, written in the same transaction as
    -- the mutation itself. Per-user private things — folders, items, contacts,
    -- flags — are not audited; what they hold is the user's own.
    CREATE TABLE "audit_event" (
        "id"            varchar(32)  NOT NULL,
        "created_at"    timestamptz  NOT NULL,
        "actor_kind"    varchar(8)   NOT NULL,   -- "user", "system" (a sweep, SSO reconciling a group), "rescue" (teanode-server on the host)
        "actor_user_id" varchar(32),             -- the signed-in user, when actor_kind is "user"; kept after the user is deleted
        "token_id"      varchar(32),             -- the session or API token that authorised the request; never its secret
        "source_ip"     varchar(45)  NOT NULL DEFAULT '',
        "instance"      varchar(64)  NOT NULL DEFAULT '',  -- which instance served the request
        "resource_type" varchar(32)  NOT NULL,   -- user, group, role, domain, mailbox, mailbox_address, mailbox_app_password, token, passkey, setting
        "resource_id"   varchar(64)  NOT NULL,
        "action"        varchar(8)   NOT NULL,   -- "create", "update", "delete"
        "before"        jsonb,                   -- the row before, redacted; null on create
        "after"         jsonb,                   -- the row after, redacted; null on delete
        PRIMARY KEY ("id")
    );
    CREATE INDEX "audit_event_resource" ON "audit_event" ("resource_type", "resource_id", "created_at" DESC);
    CREATE INDEX "audit_event_actor"    ON "audit_event" ("actor_user_id", "created_at" DESC) WHERE "actor_user_id" IS NOT NULL;
    CREATE INDEX "audit_event_time"     ON "audit_event" ("created_at" DESC);

Every change to something administrative — a user, a group and its users,
roles and domains, a role and its permissions, a domain, a mailbox and its
addresses, rules and signature, an app password, a token, a passkey, a
server setting — writes one `audit_event` row in the transaction that
makes the change, holding who did it, from where, and the row before and
after with secrets removed (a password hash, a token's key, an app
password's hash are never written; models that carry one implement
`RedactForAudit`). Folders, items, contacts and flags are not audited: they
are the user's own, and a log of every message read would be surveillance,
not accountability.

The seam is in `internal/db`: one function every audited write goes
through — `recordMutation(transaction, resourceType, id, before, after)` —
so a mutation cannot forget, and a test asserts that each audited model's
db writer calls it. The actor comes from the request context the
authentication layer already fills; a sweep or SSO reconciling a group's
users is `system`; `teanode-server rescue` is `rescue`, so the one write
that bypasses permissions is the one most visibly recorded. Rows are kept
for `audit.retention`, a year by default, and swept on one instance at a
time under the same advisory lock as the other sweeps.

Reading it is `audit:read`, a server-kind permission that Administrator
holds: a page under Server, filterable by resource, actor and time, showing
each event as a before-and-after diff, and a "history" tab on every user,
group, role, domain and mailbox page listing that row's events.

### The Go types

Every table above has a domain type in `internal/models`, and four existing
types change. Written out so that "what is added" has one answer:

    // New types in internal/models. Every one is the domain shape with json tags;
    // the GORM model beside it in internal/db is the persistence shape.
    
    type Permission string
    
    const (
        PermissionMailRead      Permission = "mail:read"
        PermissionMailWrite     Permission = "mail:write"
        PermissionMailSend      Permission = "mail:send"
        PermissionMailboxManage Permission = "mailbox:manage"
        PermissionMailAudit     Permission = "mail:audit"
        PermissionDomainManage  Permission = "domain:manage"
        PermissionReportRead    Permission = "report:read"
        PermissionUserManage    Permission = "user:manage"
        PermissionGroupManage   Permission = "group:manage"
        PermissionRoleManage    Permission = "role:manage"
        PermissionServerManage  Permission = "server:manage"
    )
    
    // Role is a named set of permissions.
    type Role struct {
        ID          string       `json:"id"`
        CreatedAt   time.Time    `json:"createdAt"`
        ModifiedAt  time.Time    `json:"modifiedAt"`
        Name        string       `json:"name"`
        Description string       `json:"description,omitempty"`
        Permissions []Permission `json:"permissions"`
    }
    
    // Group is a named set of users.
    type Group struct {
        ID          string    `json:"id"`
        CreatedAt   time.Time `json:"createdAt"`
        ModifiedAt  time.Time `json:"modifiedAt"`
        Name        string    `json:"name"`
        Description string    `json:"description,omitempty"`
        // IDPGroup is the group's name at the identity provider; SSO adds and
        // removes members of a group that has one and never touches one that
        // does not.
        IDPGroup    string    `json:"idpGroup,omitempty"`
        // UserIDs is the user_group table, read with the group.
        UserIDs     []string  `json:"userIds,omitempty"`
        // RoleIDs is the group_role table: what every user in the group may do.
        RoleIDs     []string  `json:"roleIds,omitempty"`
        // DomainIDs is the group_domain table: where the domain-kind
        // permissions of those roles apply.
        DomainIDs   []string  `json:"domainIds,omitempty"`
    }
    
    // EffectivePermissions is what one request may do, resolved once from the
    // caller's groups: user_group × group_role × role_permission, scoped by
    // group_domain. Carried on the context; never cached across requests on an
    // instance.
    type EffectivePermissions struct {
        // Everywhere holds the server and all-domains permissions the
        // caller's groups carry.
        Everywhere map[Permission]bool            `json:"everywhere"`
        // ByDomain holds the domain-kind permissions by domain id, from each
        // group's roles crossed with that group's domains. Additive: two groups
        // that each reach a domain contribute both their roles.
        ByDomain   map[string]map[Permission]bool `json:"byDomain"`
    }

    // AuditEvent is one administrative change: who, from where, what row, and
    // the row before and after with secrets removed. Written in the same
    // transaction as the change. Not for per-user private things.
    type AuditEvent struct {
        ID           string            `json:"id"`
        CreatedAt    time.Time         `json:"createdAt"`
        ActorKind    AuditActorKind    `json:"actorKind"`
        ActorUserID  string            `json:"actorUserId,omitempty"`
        TokenID      string            `json:"tokenId,omitempty"`
        SourceIP     string            `json:"sourceIp,omitempty"`
        Instance     string            `json:"instance,omitempty"`
        ResourceType AuditResourceType `json:"resourceType"`
        ResourceID   string            `json:"resourceId"`
        Action       AuditAction       `json:"action"`
        Before       json.RawMessage   `json:"before,omitempty"` // nil on create
        After        json.RawMessage   `json:"after,omitempty"`  // nil on delete
        // ActorLabel is the actor's username or "system" or "rescue", resolved
        // when read, never stored.
        ActorLabel   string            `json:"actorLabel,omitempty"`
    }

    type AuditActorKind string

    const (
        AuditActorUser   AuditActorKind = "user"
        AuditActorSystem AuditActorKind = "system"
        AuditActorRescue AuditActorKind = "rescue"
    )

    type AuditAction string

    const (
        AuditActionCreate AuditAction = "create"
        AuditActionUpdate AuditAction = "update"
        AuditActionDelete AuditAction = "delete"
    )

    // AuditResourceType names what was changed. Adding one here is what makes
    // a table audited; the test that every writer records its mutation keys
    // off this list.
    type AuditResourceType string

    const (
        AuditResourceUser               AuditResourceType = "user"
        AuditResourceGroup              AuditResourceType = "group"      // and its users, roles, domains
        AuditResourceRole               AuditResourceType = "role"       // and its permissions
        AuditResourceDomain             AuditResourceType = "domain"
        AuditResourceMailbox            AuditResourceType = "mailbox"    // and its rules, signature
        AuditResourceMailboxAddress     AuditResourceType = "mailbox_address"
        AuditResourceMailboxAppPassword AuditResourceType = "mailbox_app_password"
        AuditResourceToken              AuditResourceType = "token"
        AuditResourcePasskey            AuditResourceType = "passkey"
        AuditResourceSetting            AuditResourceType = "setting"
    )

    // Redactor is implemented by models that carry a secret, so the secret
    // never reaches the audit row.
    type Redactor interface {
        RedactForAudit() any
    }
    
    // Mailbox is a container of folders belonging to one user. Its small
    // per-mailbox settings — rules, signature — are columns on it.
    type Mailbox struct {
        ID            string            `json:"id"`
        CreatedAt     time.Time         `json:"createdAt"`
        ModifiedAt    time.Time         `json:"modifiedAt"`
        UserID        string            `json:"userId"`
        Name          string            `json:"name"`
        // The signature the compose page appends when sending from this mailbox;
        // a user with two mailboxes signs differently from each.
        SignatureHTML string            `json:"signatureHtml,omitempty"`
        SignatureText string            `json:"signatureText,omitempty"`
        Rules         []MailboxRule     `json:"rules"` // jsonb column, run in order
        Addresses     []*MailboxAddress `json:"addresses,omitempty"`
    }
    
    // MailboxAddress is an address that delivers into a mailbox, and that the
    // mailbox's members may send as.
    type MailboxAddress struct {
        MailboxID string `json:"mailboxId"`
        DomainID  string `json:"domainId"`
        LocalPart string `json:"localPart"`
        Primary   bool   `json:"primary"`
    }
    
    type MailboxFolderKind string
    
    const (
        MailboxFolderKindCustom  MailboxFolderKind = ""
        MailboxFolderKindInbox   MailboxFolderKind = "inbox"
        MailboxFolderKindSent    MailboxFolderKind = "sent"
        MailboxFolderKindDrafts  MailboxFolderKind = "drafts"
        MailboxFolderKindArchive MailboxFolderKind = "archive"
        MailboxFolderKindJunk    MailboxFolderKind = "junk"
        MailboxFolderKindTrash   MailboxFolderKind = "trash"
    )
    
    // MailboxFolder is a named place in a mailbox, nested as deep as its owner likes.
    type MailboxFolder struct {
        ID          string     `json:"id"`
        CreatedAt   time.Time  `json:"createdAt"`
        ModifiedAt  time.Time  `json:"modifiedAt"`
        MailboxID   string     `json:"mailboxId"`
        ParentID    string     `json:"parentId,omitempty"`
        Name        string     `json:"name"`
        Kind        MailboxFolderKind `json:"kind,omitempty"`
        // IMAP's contract: UIDs in a folder only grow, and a folder that is
        // recreated announces itself with a new validity.
        UIDValidity uint64     `json:"uidValidity"`
        UIDNext     uint64     `json:"uidNext"`
        // Grows on every change to the folder's items; what IDLE watches and
        // what CONDSTORE compares.
        ModSeq      uint64     `json:"modseq"`
        // Counted when listed, not stored.
        Unread      int64      `json:"unread"`
        Total       int64      `json:"total"`
    }
    
    // MailboxItem is one message in one folder: the possession of it, with its
    // flags. The message is the existing Mail; this only refers to it.
    type MailboxItem struct {
        ID        string    `json:"id"`
        FolderID  string    `json:"folderId"`
        MailID    string    `json:"mailId"`
        Mail      *Mail     `json:"mail,omitempty"` // resolved when listed
        UID       uint64    `json:"uid"`
        ModSeq    uint64    `json:"modseq"`
        Seen      bool      `json:"seen"`
        Flagged   bool      `json:"flagged"`
        Answered  bool      `json:"answered"`
        Forwarded bool      `json:"forwarded"`
        Draft     bool      `json:"draft"`
        AddedAt   time.Time `json:"addedAt"`
    }
    
    // MailboxRule is one entry of Mailbox.Rules, run in array order when a
    // message reaches the Inbox. No id and no row of its own: rules are saved
    // as a whole.
    type MailboxRule struct {
        Name       string                 `json:"name"`
        Enabled    bool                   `json:"enabled"`
        Conditions []MailboxRuleCondition `json:"conditions"` // all must match
        Actions    []MailboxRuleAction    `json:"actions"`    // in order
        Stop       bool                   `json:"stop"`       // no later rule runs after this one matches
    }
    
    // MailboxRuleCondition is one test: a field, how to compare, and against what.
    type MailboxRuleCondition struct {
        Field    string `json:"field"`    // from, to, subject, header, score, sender-known, any
        Header   string `json:"header,omitempty"`
        Operator string `json:"operator"` // contains, equals, matches, above, below
        Value    string `json:"value,omitempty"`
    }
    
    // MailboxRuleAction is one thing to do: move somewhere, mark, forward, delete.
    type MailboxRuleAction struct {
        Kind     string `json:"kind"` // move, markRead, flag, forward, delete
        FolderID string `json:"folderId,omitempty"`
        Address  string `json:"address,omitempty"`
    }
    
    // MailboxAppPassword is what a mail program signs in with. It belongs to a
    // mailbox, not a user: a program's "account" is one mailbox, so the login
    // name is one of the mailbox's addresses and the app password is what says
    // which mailbox that is. One per device, revocable alone; the hash never
    // leaves the server.
    type MailboxAppPassword struct {
        ID           string     `json:"id"`
        CreatedAt    time.Time  `json:"createdAt"`
        MailboxID    string     `json:"mailboxId"`
        Name         string     `json:"name"`
        PasswordHash string     `json:"-"`
        LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
    }
    
    // Identity is who a user is at an identity provider.
    type Identity struct {
        ID          string     `json:"id"`
        CreatedAt   time.Time  `json:"createdAt"`
        UserID      string     `json:"userId"`
        Provider    string     `json:"provider"`
        Subject     string     `json:"subject"`
        Email       string     `json:"email,omitempty"`
        LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
    }
    
    // MailboxContact is an address learned from traffic, for completion and for the
    // "sender is known" rule condition.
    type MailboxContact struct {
        MailboxID  string    `json:"mailboxId"`
        Address    string    `json:"address"`
        Name       string    `json:"name,omitempty"`
        LastSeenAt time.Time `json:"lastSeenAt"`
        Count      int       `json:"count"`
    }

    // Existing types that change. Additions only; nothing is renamed or removed.
    
    type User struct {
        // … existing fields …
        // PasswordHash becomes optional: a user with none signs in only with a
        // passkey or through an identity provider.
        PasswordHash  string     `json:"-"`
        DisabledAt    *time.Time `json:"disabledAt,omitempty"` // cannot sign in; keeps their mail
        Locale        string     `json:"locale,omitempty"`
        // Resolved for GetCurrentUser and GetSession, not stored.
        Permissions   *EffectivePermissions `json:"permissions,omitempty"`
        GroupIDs      []string              `json:"groupIds,omitempty"`
    }
    
    type Mail struct {
        // … existing fields …
        // ThreadID is the conversation this message is part of, derived from
        // In-Reply-To and References at receipt; the root message's own id when
        // it starts one. The search document is a database column only.
        ThreadID string `json:"threadId,omitempty"`
        // UnreferencedAt is when the last mailbox item holding this message went
        // away — or its arrival, for a message no mailbox took. Nil while any
        // item references it. Retention prunes what has been unreferenced for
        // longer than the retention period, nothing else.
        UnreferencedAt *time.Time `json:"unreferencedAt,omitempty"`
        // Kind gains MailKindDraft for a message being written.
    }
    
    const MailKindDraft MailKind = "draft"
    
    type Delivery struct {
        // … existing fields …
        // Kind gains DeliveryKindMailbox: the message placed in a folder of a
        // mailbox. One such delivery per mailbox the message reached — three
        // addresses in three mailboxes is three deliveries, three items, one
        // Mail. Created in the receipt transaction, already delivered, like the
        // internal kind today; nothing to queue.
        MailboxID     string `json:"mailboxId,omitempty"`
        MailboxItemID string `json:"mailboxItemId,omitempty"`
        // Method gains "mailbox"; Destination is the mailbox's name, so the
        // message page reads "delivered into the support mailbox".
    }

    const DeliveryKindMailbox DeliveryKind = "mailbox"

    // In the database:
    //   ALTER TABLE "delivery"
    //       ADD COLUMN "mailbox_id"      varchar(32),
    //       ADD COLUMN "mailbox_item_id" varchar(32);
    //   CREATE INDEX "delivery_mailbox" ON "delivery" ("mailbox_id") WHERE "mailbox_id" IS NOT NULL;
    
    // In internal/config, an alias gains a kind and a target:
    type Alias struct {
        // … existing fields …
        // Kind gains AliasKindMailbox.
        MailboxID string `yaml:"mailboxId,omitempty"`
    }
    
    const AliasKindMailbox AliasKind = "mailbox"
    
    // And a section for identity providers:
    type SSO struct {
        Providers []SSOProvider `yaml:"providers"`
    }
    
    type SSOProvider struct {
        Name         string   `yaml:"name"`
        DiscoveryURL string   `yaml:"discoveryUrl"`
        ClientID     string   `yaml:"clientId"`
        ClientSecret string   `yaml:"clientSecret" secret:"true"`
        GroupsClaim  string   `yaml:"groupsClaim,omitempty"` // e.g. "groups"
        CreateUsers  bool     `yaml:"createUsers"`
        IssuerHosts  []string `yaml:"issuerHosts,omitempty"`
    }

### Contacts

    -- Learned from mail sent and received, for address completion when
    -- composing and for the "sender is known" rule condition. Not an address
    -- book anybody edits; that would be a separate thing.
    CREATE TABLE "mailbox_contact" (
        "mailbox_id"   varchar(32)  NOT NULL REFERENCES "mailbox"("id") ON DELETE CASCADE,
        "address"      varchar(320) NOT NULL,
        "name"         varchar(256) NOT NULL DEFAULT '',
        "last_seen_at" timestamptz  NOT NULL,
        "count"        integer      NOT NULL DEFAULT 1,
        PRIMARY KEY ("mailbox_id", "address")
    );

## How mail reaches a mailbox

An alias gains a fourth kind, `mailbox`, with a `mailboxId`. When a message
matches it, the delivery is a row: a `mailbox_item` in the mailbox's Inbox,
referencing the `mail` row that already exists, with the folder's next UID.
A delivery record is created too — kind `mailbox`, status delivered,
naming the mailbox and the item it produced — so the message page shows
"delivered into the support mailbox" beside its forwards, and a rule that
moved the item can be traced from it. One delivery per mailbox reached. No bytes are copied and no second `mail` row is written. A message
to three addresses in three mailboxes is one row and three items.

Then the mailbox's rules run, in order, against the new item. A rule that
moves it moves the item; one that forwards creates a `forward` delivery like
any alias would.

A submission from a user of this server — from the dashboard, or over SMTP
with an app password — is a `mail` row of kind outgoing, as today, plus an
item in the sender's Sent folder referencing it. A recipient at a local
address gets an item in their Inbox referencing the *same* row: the copy that
`exchange_outgoing.go` makes today for a local recipient is replaced by that
reference.

Retention changes to match. The sweep in `internal/storage/filesystem.go`
prunes by file age today, and cannot go on doing that once a folder can hold
a message for years. It becomes: prune a `mail` row and its stored message
once it has been **unreferenced for longer than `storage.spoolRetention`**.
The clock is `mail.unreferenced_at`: null while any `mailbox_item` holds the
message; set, in the same transaction, when the last item is deleted; set on
arrival for a message no mailbox took, which is every message today, so
today's behaviour is the degenerate case. Creating an item clears it again.
A message that sat in a folder for two years and was then deleted is kept
for a further thirty days, not zero — long enough to notice.

    ALTER TABLE "mail" ADD COLUMN "unreferenced_at" timestamptz;
    CREATE INDEX "mail_unreferenced" ON "mail" ("unreferenced_at") WHERE "unreferenced_at" IS NOT NULL;

Trash is the exception: an item in Trash older than `mailbox.trashRetention`
(thirty days by default) is deleted, and then the message is unreferenced
and goes with everything else. The sweep therefore asks the database which
messages to drop rather than reading the directory listing, which is a
change to a component that today knows nothing about the database — and it
runs on one instance at a time, under an advisory lock, since each instance
can only sweep the spool it holds while the decision belongs to all of them.

## Drafts

A draft is not a separate kind of thing. It is a message, held the way every
message is held, in three places by reference: the bytes in storage — the
spool and the mirror — as a complete RFC 5322 message, exactly like an
outgoing message before it is sent; a `mail` row of kind `draft`; and a
`mailbox_item` in the mailbox's Drafts folder with the `draft` flag set,
which is what IMAP calls `\Draft`. There is no structured draft — no JSON
of to, subject and body. The compose page reopens a draft by parsing the
stored message with the parser that renders any message. One
representation, one parser.

**Saving again is a new message, not an edit in place.** Stored messages
are immutable and stay so. Each save writes new bytes, a new `mail` row and
a new item, and deletes the previous draft's item and row in the same
transaction — a superseded draft is the one message nobody wants back, so
it does not get the retention grace; its bytes go with the next sweep on
the instance holding them. This is exactly what a mail program does over
IMAP when it saves a draft: `APPEND` the new one to Drafts, delete the old
one. So the dashboard and Thunderbird do the same thing to the same folder
and neither can confuse the other. Sending removes the draft's item and row
and creates an ordinary outgoing message with an item in Sent.

**Attachments are the thing to watch.** A complete message per save is fine
for text and wrong for a draft carrying a 20 MB file: forty saves over an
afternoon must not be 800 MB written and 800 MB sent up from the browser.
Three rules keep it honest:

- *The browser uploads a file once.* Attachments are added to a draft
  through their own upload path — a `PUT` of the bytes to
  `/api/mail/{draftId}/attachment`, the sibling of the download path that
  exists today — not as base64 inside a GraphQL mutation, which is how the
  compose page sends them now and which turns 20 MB into a 27 MB JSON
  string. A save sends the text fields and the list of parts to keep, by
  index; the server assembles the new message from the new text and the
  previous draft's parts. The file crosses the wire once, when it is chosen.
- *A save without a change in attachments copies parts, not files.* The
  server builds the new message by streaming the kept parts out of the
  previous stored message into the new one; nothing is decoded and
  re-encoded, and the browser is not involved.
- *Save on intent, not on keystroke.* The page saves on an explicit save,
  when the tab loses focus, and on a timer of half a minute. A draft
  written over an afternoon is a few dozen rows, each superseded and gone.

The storage cost that remains — one complete copy of the attachments per
save, briefly — is accepted; the alternative, storing attachments as
separate objects and assembling the message at send time, would make a
draft the one kind of message with a second representation, and every
reader of it, IMAP included, would have to know. A draft is subject to the
same size limit as a message being sent, checked at save, so a draft that
cannot be sent cannot be saved either.

**Nothing is shared.** A mailbox has one owner and a draft is referenced by
one item in one Drafts folder, so a draft is the one kind of message that
is never a shared row.

## IMAP

`internal/imap` serves IMAP4rev1 with the extensions a modern client expects
— `IDLE`, `MOVE`, `UIDPLUS`, `LITERAL+`, `NAMESPACE`, `SPECIAL-USE` (so a
client knows which folder is Sent and which is Trash), `CONDSTORE` when
`modseq` is ready — on port 993 over TLS, and on 143 with `STARTTLS`
required. It uses the same certificate the HTTPS and SMTP listeners do.

Authentication is one of the mailbox's addresses as the login name plus an
app password of that mailbox — and nothing else: not the account password,
not a passkey. The app password is what picks the mailbox, so a user with
two mailboxes sets up two accounts in their mail program, which is what the
program expects anyway. SSO users get app passwords like everyone else. Every
sign-in goes through the per-address rate limiter the SMTP listener already
has; there is no lockout, because a lockout is a way for an attacker to shut
someone out of their mail by guessing wrong on purpose.

The mapping is direct. A mailbox's folders are the IMAP mailbox tree, with
`/` as the separator and nested folders as nested names. A folder's
`uid_validity` and `uid_next` are its own. `FETCH` reads flags from the item
and the message from `storage.Get`, parsing on demand; `STORE` writes flags
and bumps `modseq`; `COPY` and `MOVE` create items; `EXPUNGE` deletes items
flagged deleted; `APPEND` — a client saving its own sent message — stores a
`mail` row of kind outgoing and an item, so a message sent from a phone
appears in the dashboard's Sent folder too. `SEARCH` on headers and flags
runs as SQL; body search runs over the `search` column.

`IDLE` is the one part that is not a query. A client holding a folder open
must be told when something arrives, and on a server of several instances the
arrival may have been handled by another one. The instance that changes a
folder issues `NOTIFY folder_changed, '<folder id>'` in the same transaction;
every instance `LISTEN`s and wakes the sessions idling on that folder. Nothing
is held in memory that another instance would need.

The library is `github.com/emersion/go-imap/v2`, the standard Go IMAP
implementation, vendored like every dependency here, at `v2.0.0-beta.8`. It
is tried first. If its beta API churns, or its backend interfaces fight the
database-backed model — UIDs allocated in a transaction, `IDLE` woken by
another instance — then `internal/imap` is written from RFC 9051 and the
extension RFCs directly, for the subset above and no more. That is a bounded
job: the server side of IMAP is a well-specified state machine over exactly
the operations this plan lists, and this server already has its own SMTP
server and client for the same reason. The owner has said so.

## Signing in through an identity provider

OIDC only. The identity providers that matter — Entra ID, Okta, Google,
Keycloak, Authentik — all speak it, and SAML would be a second protocol for
the same job. Provisioning (creating and, above all, *disabling* accounts
when the directory does) is a different problem from signing in; the login
path can only reconcile a user who turns up. That is a later milestone, SCIM,
and the `identity` table and `disabled_at` column are shaped for it now.

Configuration, in the database like everything else, under `sso.providers`:
a name, the discovery URL, client id and secret, the claim that carries group
names, and whether a user who arrives with no account is created. Sign-in is
the authorisation-code flow with PKCE, a signed and expiring `state`, an
allowlist of issuer hosts, and a guard against redirecting or fetching to a
private address. The dashboard's sign-in page shows a button per provider
beside the password and passkey forms.

On return, the identity is looked up by `(provider, subject)`. Found: that
user signs in. Not found and the provider allows creation: a user is created
with no password, an identity, and a Personal mailbox. Either way, the groups
claim is reconciled against `group.idp_group`: the user is added to every
group naming one of the claimed groups and removed from every group naming
one that is no longer claimed. Groups without an `idp_group` are never
touched by SSO. Roles therefore follow the directory without anybody
administering them here, which is the point.

## The dashboard

A new top-level place in the rail, **Mail**: the mailboxes the user can
read, and nothing else. Today's pages — every message on a domain, the
queue, the domains, the server settings — each stay behind the permission
they name (`mail:audit`, `queue:manage`, `domain:manage`, and so on): the
rail shows them only to a user who holds it, and the API answers not found
to one who does not. A normal user signs in to their own mailboxes and sees
no sign that the rest exists. The Mail page is a folder tree on the left —
every mailbox the user owns — a message list in the middle, and the message on the right, which is the
existing message page with its authentication panel, spam breakdown and
rendered frame. The list is the existing `DataTable`, remembering its place
as it now does, with unread rows bold, a flag column, and a search box that
runs the full text search.

Selecting rows offers mark read or unread, flag, archive, move to a folder,
delete; the same on one message. Reply, reply to all and forward open the
compose page with recipients, subject, quoted body and threading headers
filled in and, for forward, the attachments carried over; sending adds the
reply to Sent and marks the original answered. Drafts save to the Drafts
folder as a `mail` row of kind draft and reopen from there — see Drafts
above for what a save is, and how attachments cross the wire once. A message opened
is marked read after a moment, not on arrival.

Mailbox settings: folders, rules with a live "which of the last hundred
messages would this match", addresses, signature, app passwords. Under
Server, three pages for whoever holds the permissions: Users, Groups, Roles.
A group's page is the one that matters: its users, its roles, its domains,
so "why can this person do this" is answered by the groups they are in, on
one screen.

## Milestones

Each is independently shippable and leaves the server working for everyone
who used it the day before. The order is the order of dependency.

**One — who may do what.** The access-control tables and the three seeded
roles; every existing user into Administrators. `requirePermission` in place
of `requireOperator` across every resolver, with the not-found rule.
`teanode-server rescue`. The `audit_event` table and the recording seam,
so the first role edit is the first row. Effective
permissions on the session and in `GetSession`. The Users, Groups and Roles
pages. The command line's `user` commands grow `group` and `role` siblings.
Acceptance: editing a role writes an audit row naming the editor; a user whose only group carries Member can sign in and sees nothing but an
empty Mail page; an Operator sees today's dashboard; an Administrator sees
everything; a Member asking the API for a domain gets not found.

**Two — mail that lives here.** Mailboxes, folders, items, threads; the
`mailbox` alias kind and delivery by reference; the retention change; the
Mail page with folder tree, list, message, flags, move, archive, trash, empty
trash. Acceptance: a message to a mailbox address appears in the inbox, one
`mail` row however many mailboxes it reached, survives the spool retention
while it sits there, and is gone from the spool a day after the last folder
let go of it.

**Three — writing back.** Reply, reply to all, forward, drafts, Sent by
reference, the answered flag, threading headers, and the local-recipient copy
replaced by a reference. Acceptance: a reply from the dashboard arrives at an
external recipient DKIM-signed with `In-Reply-To` and `References` set, sits
in Sent, and the original shows answered.

**Four — finding and sorting.** The search column and full text search;
list filtering by unread, flagged, sender, date; rules with the dry run;
contacts learned from traffic and address completion in compose. Acceptance:
a rule filing a newsletter into a folder runs on the next arrival; searching a
word in a body finds the message.

**Five — reading from anywhere.** App passwords; the IMAP server with `IDLE`
over `LISTEN`/`NOTIFY`; submission over port 587 with an app password sending
as an address of that mailbox; the compose file and the docs. This is
the milestone that decides on the beta dependency. Acceptance: a phone's mail
app reads and flags the inbox, is woken when a message arrives at another
instance, and a message sent from it appears in the dashboard's Sent folder.

**Six — the organisation's identity.** OIDC sign-in with the flow above,
identity rows, just-in-time users, group reconciliation. Acceptance: a user
who exists only in the provider signs in for the first time, has a mailbox,
and holds the roles their provider groups map to; removing them from a
provider group and signing in again removes the role.

**Seven — the rest of a mailbox.** Out-of-office replies, with the loop and
list protections that need; per-mailbox quotas; unread counts in the rail;
notifications; SCIM provisioning; JMAP if a client ever asks for it. Each is
its own small plan when it is wanted.

## Decision Log

Decisions are the repository owner's.

- Decision: nothing is copied. A mailbox holds references to the one `mail`
  row and the one stored message; the same message in three mailboxes is one
  row and three items.
  Rationale: this was the brief, and it is also right — a copy per mailbox
  would multiply storage by the number of readers and make "the message" a
  question with several answers.
  Date/Author: 2026-09-06, Ziyan

- Decision: the access model is groups, roles, permissions and users. Roles
  and domains attach only to groups (`group_role`, `group_domain`); a user in
  a group holds the group's roles over the group's domains, additively across
  groups. Nothing attaches to a user directly.
  Rationale: one shape for every grant. "Some users manage other users" is a
  group with a role that carries `user:manage`; "this team runs this domain"
  is a group with that domain and an Operator role. No bindings, no subject
  kinds, no per-user exceptions to audit.
  Date/Author: 2026-09-06, Ziyan

- Decision: every milestone is judged against several instances sharing one
  database, and a cluster serving mailboxes requires the object store.
  Rationale: the server already runs that way, and a mailbox that differed
  between instances — or a message readable only where it arrived — would be
  the kind of fault nothing shows. The spool is per instance; the mirror is
  what makes the bytes reachable from any of them.
  Date/Author: 2026-09-06, Ziyan

- Decision: IMAP may be implemented from scratch if using a library proves
  more trouble than it saves.
  Rationale: the server side is a bounded, well-specified state machine over
  the operations this plan lists, and this server already carries its own
  SMTP implementation for the same reason.
  Date/Author: 2026-09-06, Ziyan

- Decision: SSO is planned for from the start.
  Rationale: the identity table, the nullable password, the `idp_group`
  column and `disabled_at` cost nothing now and would each be a migration
  across live data later.
  Date/Author: 2026-09-06, Ziyan

- Decision: OIDC only; no SAML. SCIM for provisioning is a later milestone.
  Rationale: the providers that matter all federate over OIDC, and SAML would
  be a second protocol for one job. The gap that actually bites is disabling
  an account when the directory does, and that is SCIM, not SAML.
  Date/Author: 2026-09-06, Ziyan

- Decision: retention becomes "unreferenced for longer than the retention
  period". A message is scavenged only once nothing has held it for that
  long; `mail.unreferenced_at` is the clock.
  Rationale: a mailbox cannot live on a spool that forgets things after
  thirty days, and a message deleted from a folder deserves the same grace
  a message that never reached one gets today.
  Date/Author: 2026-09-06, Ziyan

- Decision: groups and roles are seeded, and all of them are editable; there
  are no built-in restrictions.
  Rationale: a role nobody may edit is a rule the administrator cannot see
  the reason for.
  Date/Author: 2026-09-06, Ziyan

- Decision: today's views of all mail, the queue and the domains are
  restricted to roles holding those permissions. A normal user sees only the
  mailboxes they have access to.
  Rationale: right for one operator, wrong the moment a second person has an
  inbox.
  Date/Author: 2026-09-06, Ziyan

- Decision: mail programs use per-device app passwords, and an app password
  belongs to a mailbox, not a user. Nothing else signs in to IMAP or
  submission: not the account password, not a passkey.
  Rationale: IMAP clients store what they are given in a keychain on the
  device, and a per-device secret is the one that can be revoked alone.
  Date/Author: 2026-09-06, Ziyan

- Decision: membership is the `user_group` table, a plain many-to-many.
  Rationale: that is all it is; a name like "member" suggests a row with a
  life of its own.
  Date/Author: 2026-09-06, Ziyan

- Decision: a mailbox belongs to one user — `mailbox.user_id`, nothing else.
  No group ownership, no owner kind.
  Rationale: one owner is what every IMAP client, every rule and every "who
  read this" question assumes; a second kind of owner doubled every check
  for a case that can be added later as sharing.
  Date/Author: 2026-09-06, Ziyan

- Decision: the signature belongs to the mailbox, not the user.
  Rationale: a signature goes with what one sends as; a user with two
  mailboxes signs differently from each.
  Date/Author: 2026-09-06, Ziyan

- Decision: everything that belongs to a mailbox is named for it. Tables
  `mailbox_folder`, `mailbox_item`, `mailbox_address`, `mailbox_contact`,
  `mailbox_app_password`; types `MailboxFolder`, `MailboxRule`,
  `MailboxRuleCondition`, `MailboxRuleAction`, `MailboxContact`,
  `MailboxAppPassword`.
  Rationale: named for what they belong to, like `MailboxAddress`.
  Date/Author: 2026-09-06, Ziyan

- Decision: small per-mailbox settings — rules, the signature — are columns
  on `mailbox`, jsonb where they are lists. There is no `mailbox_rule`
  table.
  Rationale: a mailbox has a handful of rules, edited as a whole; a table
  with ids and positions is machinery for a list that fits in one row.
  Date/Author: 2026-09-06, Ziyan

- Decision: no labels on items. Folders and the flag are enough.
  Rationale: a second way to sort mail is a second thing to explain, and
  IMAP clients disagree about how to show keywords.
  Date/Author: 2026-09-06, Ziyan

- Decision: a draft is a complete stored message — bytes, a `mail` row of
  kind draft, an item in Drafts — and every save is a new message that
  supersedes and deletes the previous one, with no retention grace. No
  structured draft. Attachments are uploaded once through their own path
  and copied part-to-part between saves, never re-sent from the browser.
  Rationale: one representation and one parser, and the same behaviour a
  mail program has over IMAP, so the two never disagree about a draft.
  Date/Author: 2026-09-06, Ziyan

- Decision: administrative changes are audited — users, groups, roles,
  domains, mailboxes and their addresses, app passwords, tokens, passkeys,
  settings — with before and after, in the same transaction. Per-user
  private things — folders, items, contacts, flags — are not.
  Rationale: "who gave this group that role" must be answerable a year
  later; "who read this message" must not be recorded.
  Date/Author: 2026-09-06, Ziyan

- Decision: try `github.com/emersion/go-imap/v2` at its beta first; if it
  does not work out, write IMAP from the RFCs.
  Rationale: it is the standard Go implementation; the risk is API churn,
  confined to `internal/imap`, and the fallback was already accepted.
  Date/Author: 2026-09-06, Ziyan

- Decision: no last-administrator guard. Locking oneself out is an accepted
  risk; `teanode-server rescue` on the host is the way back.
  Rationale: a guard is one more rule to reason about on every mutation for
  a state that the host operator can repair in one command.
  Date/Author: 2026-09-06, Ziyan

- Decision: shared inboxes are out of scope. This programme is per-user
  mailboxes only.
  Rationale: focus. Nothing here forecloses sharing later.
  Date/Author: 2026-09-06, Ziyan

- Decision: no `all_domains` flag on a group. Reach is a property of the
  permission — its kind — and the administrators' role holds the
  all-domains permissions `domain:manage-all` and `mail:audit-all`.
  Rationale: a flag on the group and a kind on the permission say the same
  thing, and the permission is the one that is already declared in Go and
  already checked on every request.
  Date/Author: 2026-09-06, Ziyan

### Recommendations awaiting a decision

None. Everything proposed so far has been decided; the log above is the
record.

## Progress

- [x] (2026-09-06) Design and data model; the reference model's shape studied
      and adapted; milestones ordered by dependency.
- [ ] Milestone one: access control.
- [ ] Milestone two: mailboxes and delivery by reference.
- [ ] Milestone three: reply, forward, drafts.
- [ ] Milestone four: search, filters, rules, contacts.
- [ ] Milestone five: IMAP and submission.
- [ ] Milestone six: SSO.
- [ ] Milestone seven: the rest.

## Surprises & Discoveries

- Observation: the server already copies a message for a local recipient of
  a submission, which is the one place the "no copies" rule is broken today.
  Evidence: `internal/mx/exchange_outgoing.go` creates a `mail` row per
  recipient domain with `DeliveryKindInternal`. Milestone three replaces it.

- Observation: retention is a property of the spool directory, not the
  database, and the two have to change places.
  Evidence: `internal/storage/filesystem.go` sweeps files older than
  `Retention` and knows nothing about who holds a message.

## Outcomes & Retrospective

Not started.
