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

A **mailbox** is a container of folders that belongs to a user or to a group.
A person has one by default; a group can have one, which is how a shared inbox
works. An **address** is what delivers into a mailbox: `support@example.com`
delivers into the support group's mailbox. A **folder** is a named place inside
a mailbox, nested as deep as its owner likes; Inbox, Sent, Drafts, Archive,
Junk and Trash are folders with a fixed kind. A **mailbox item** is one
message in one folder, with its flags: read or unread, flagged, answered. The
same message can be an item in several mailboxes — a message to two people
here is one `mail` row and two items — and moving it is changing which folder
the item is in.

A **permission** is a named thing somebody may do, written `resource:verb`. A
**role** is a named set of permissions. A **group** is a named set of users. A
**binding** grants a role to a user or a group, over the whole server or over
one domain. A user's **effective permissions** are the union of every binding
that reaches them, directly or through a group, resolved once per request.

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
        PermissionMailRead      Permission = "mail:read"      // read the mailboxes one is a member of
        PermissionMailWrite     Permission = "mail:write"     // flag, move, delete in them
        PermissionMailSend      Permission = "mail:send"      // send as an address of them
        PermissionMailboxManage Permission = "mailbox:manage" // folders, rules, addresses of them
        PermissionMailAudit     Permission = "mail:audit"     // every message of a domain, as the operator sees it today
        PermissionDomainManage  Permission = "domain:manage"  // domains, aliases, credentials, templates
        PermissionReportRead    Permission = "report:read"
        PermissionUserManage    Permission = "user:manage"
        PermissionGroupManage   Permission = "group:manage"
        PermissionRoleManage    Permission = "role:manage"
        PermissionServerManage  Permission = "server:manage"  // settings, upgrades, certificates
    )

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

    -- Grants a role to a user or a group, over everything or over one domain.
    CREATE TABLE "role_binding" (
        "id"           varchar(32) NOT NULL,
        "created_at"   timestamptz NOT NULL,
        "role_id"      varchar(32) NOT NULL REFERENCES "role"("id") ON DELETE CASCADE,
        "subject_kind" varchar(8)  NOT NULL,          -- "user" or "group"
        "subject_id"   varchar(32) NOT NULL,
        "domain_id"    varchar(32) NOT NULL DEFAULT '', -- '' means the whole server
        PRIMARY KEY ("id")
    );
    CREATE INDEX "role_binding_subject" ON "role_binding" ("subject_kind", "subject_id");

Three roles are seeded by the migration and are ordinary rows from then on:
renamed, edited, deleted, or left alone like any role an administrator makes.
There is no built-in flag and nothing a seeded role may not do.
**Administrator** holds every permission; **Operator** holds everything except
`user:manage`, `group:manage` and `role:manage`, which is what every user is
today; **Member** holds `mail:read`, `mail:write`, `mail:send` and
`mailbox:manage`, which is what a person with an inbox and nothing else
needs. The migration creates a group **Administrators**, puts every existing
user in it, and binds it to Administrator — so the day this lands, nobody can
do less than they could the day before. The one guard that remains is not
about seeded roles at all: a mutation that would leave no enabled user
holding `role:manage` over the whole server is refused, whichever role or
binding it touches, because the only way back from that state is SQL.

Effective permissions are computed once per request in the authentication
layer and carried on the context, as
`api.ContextEffectivePermissions(ctx)`: a set keyed by permission, each with
the domains it holds for (or "all"). `requireOperator` is replaced by
`requirePermission(ctx, permission)` and `requireDomainPermission(ctx,
permission, domainId)`. A resolver that finds the caller lacks permission over
a row answers **not found**, never "forbidden": "you may not touch this"
confirms the row exists, which is itself a leak. The dashboard is told the
caller's effective permissions in `GetSession` and hides what they cannot do;
that is a courtesy, and every mutation is checked again on the server.

The `user` table gains:

    ALTER TABLE "user"
        ADD COLUMN "disabled_at"    timestamptz,                 -- a disabled user cannot sign in, keeps their mail
        ADD COLUMN "locale"         varchar(16)  NOT NULL DEFAULT '',
        ADD COLUMN "signature_html" text         NOT NULL DEFAULT '',
        ADD COLUMN "signature_text" text         NOT NULL DEFAULT '';
    ALTER TABLE "user" ALTER COLUMN "password_hash" DROP NOT NULL;

A user with no password hash signs in only with a passkey or through SSO.
`Email` on the user stays what it is — where the server notifies them — and
is not their mailbox address; a mailbox may have several.

### Mailboxes, folders, items

    CREATE TABLE "mailbox" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "owner_kind"  varchar(8)   NOT NULL,   -- "user" or "group"
        "owner_id"    varchar(32)  NOT NULL,
        "name"        varchar(128) NOT NULL,   -- "Personal", or the group's name
        PRIMARY KEY ("id")
    );
    CREATE INDEX "mailbox_owner" ON "mailbox" ("owner_kind", "owner_id");

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

    CREATE TABLE "folder" (
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
    CREATE UNIQUE INDEX "folder_name" ON "folder" ("mailbox_id", COALESCE("parent_id", ''), lower("name"));
    CREATE UNIQUE INDEX "folder_kind" ON "folder" ("mailbox_id", "kind") WHERE "kind" <> '';

    -- One message in one folder. The message is the existing mail row; this
    -- is the possession of it, with its flags. The same mail can be an item
    -- in many folders of many mailboxes.
    CREATE TABLE "mailbox_item" (
        "id"          varchar(32) NOT NULL,
        "folder_id"   varchar(32) NOT NULL REFERENCES "folder"("id") ON DELETE CASCADE,
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

    -- Runs when a message is added to a mailbox's Inbox, in position order,
    -- and again by hand on a folder when the owner asks.
    CREATE TABLE "mail_rule" (
        "id"          varchar(32)  NOT NULL,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "mailbox_id"  varchar(32)  NOT NULL REFERENCES "mailbox"("id") ON DELETE CASCADE,
        "position"    integer      NOT NULL,
        "name"        varchar(128) NOT NULL,
        "enabled"     boolean      NOT NULL DEFAULT true,
        "conditions"  jsonb        NOT NULL,   -- all of: {field, operator, value}
        "actions"     jsonb        NOT NULL,   -- in order: move, mark read, flag, forward, delete
        "stop"        boolean      NOT NULL DEFAULT true,
        PRIMARY KEY ("id")
    );

Conditions match on from, to, subject, a header, the spam score, whether the
sender is in the owner's contacts, or "everything". Actions move to a folder,
mark read, flag, forward to an address (a delivery of kind `forward`, so it
is signed and recorded like any other), or delete. A rule that forwards is
the one that needs `mail:send` on the mailbox, and is checked for it when
saved and when run.

### Signing in from a mail program, and from an identity provider

    -- A password a mail program uses; one per device, revocable one at a
    -- time. Passkeys cannot speak IMAP, and the account password should
    -- not sit in a phone's keychain.
    CREATE TABLE "app_password" (
        "id"            varchar(32)  NOT NULL,
        "created_at"    timestamptz  NOT NULL,
        "user_id"       varchar(32)  NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
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
    }
    
    // RoleBinding grants a role to a user or a group, over the whole server or
    // over one domain.
    type RoleBinding struct {
        ID          string      `json:"id"`
        CreatedAt   time.Time   `json:"createdAt"`
        RoleID      string      `json:"roleId"`
        SubjectKind SubjectKind `json:"subjectKind"` // "user" or "group"
        SubjectID   string      `json:"subjectId"`
        DomainID    string      `json:"domainId,omitempty"` // "" is the whole server
    }
    
    // EffectivePermissions is what one request may do, resolved once from
    // every binding that reaches the caller, directly or through a group.
    // Carried on the context; never cached across requests on an instance.
    type EffectivePermissions struct {
        // Everywhere holds the permissions granted with no domain.
        Everywhere map[Permission]bool            `json:"everywhere"`
        // ByDomain holds the ones granted over one domain, by domain id.
        ByDomain   map[string]map[Permission]bool `json:"byDomain"`
    }
    
    // Mailbox is a container of folders belonging to a user or a group.
    type Mailbox struct {
        ID         string    `json:"id"`
        CreatedAt  time.Time `json:"createdAt"`
        ModifiedAt time.Time `json:"modifiedAt"`
        OwnerKind  SubjectKind `json:"ownerKind"` // "user" or "group"
        OwnerID    string    `json:"ownerId"`
        Name       string    `json:"name"`
        Addresses  []*MailboxAddress `json:"addresses,omitempty"`
    }
    
    // MailboxAddress is an address that delivers into a mailbox, and that the
    // mailbox's members may send as.
    type MailboxAddress struct {
        MailboxID string `json:"mailboxId"`
        DomainID  string `json:"domainId"`
        LocalPart string `json:"localPart"`
        Primary   bool   `json:"primary"`
    }
    
    type FolderKind string
    
    const (
        FolderKindCustom  FolderKind = ""
        FolderKindInbox   FolderKind = "inbox"
        FolderKindSent    FolderKind = "sent"
        FolderKindDrafts  FolderKind = "drafts"
        FolderKindArchive FolderKind = "archive"
        FolderKindJunk    FolderKind = "junk"
        FolderKindTrash   FolderKind = "trash"
    )
    
    // Folder is a named place in a mailbox, nested as deep as its owner likes.
    type Folder struct {
        ID          string     `json:"id"`
        CreatedAt   time.Time  `json:"createdAt"`
        ModifiedAt  time.Time  `json:"modifiedAt"`
        MailboxID   string     `json:"mailboxId"`
        ParentID    string     `json:"parentId,omitempty"`
        Name        string     `json:"name"`
        Kind        FolderKind `json:"kind,omitempty"`
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
    
    // MailRule runs when a message reaches a mailbox's Inbox.
    type MailRule struct {
        ID         string          `json:"id"`
        CreatedAt  time.Time       `json:"createdAt"`
        ModifiedAt time.Time       `json:"modifiedAt"`
        MailboxID  string          `json:"mailboxId"`
        Position   int             `json:"position"`
        Name       string          `json:"name"`
        Enabled    bool            `json:"enabled"`
        Conditions []RuleCondition `json:"conditions"` // all must match
        Actions    []RuleAction    `json:"actions"`    // in order
        Stop       bool            `json:"stop"`       // no later rule runs after this one matches
    }
    
    // RuleCondition is one test: a field, how to compare, and against what.
    type RuleCondition struct {
        Field    string `json:"field"`    // from, to, subject, header, score, sender-known, any
        Header   string `json:"header,omitempty"`
        Operator string `json:"operator"` // contains, equals, matches, above, below
        Value    string `json:"value,omitempty"`
    }
    
    // RuleAction is one thing to do: move somewhere, mark, forward, delete.
    type RuleAction struct {
        Kind     string `json:"kind"` // move, markRead, flag, forward, delete
        FolderID string `json:"folderId,omitempty"`
        Address  string `json:"address,omitempty"`
    }
    
    // AppPassword is what a mail program signs in with: one per device,
    // revocable alone. The hash never leaves the server.
    type AppPassword struct {
        ID           string     `json:"id"`
        CreatedAt    time.Time  `json:"createdAt"`
        UserID       string     `json:"userId"`
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
    
    // Contact is an address learned from traffic, for completion and for the
    // "sender is known" rule condition.
    type Contact struct {
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
        SignatureHTML string     `json:"signatureHtml,omitempty"`
        SignatureText string     `json:"signatureText,omitempty"`
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
    CREATE TABLE "contact" (
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

## IMAP

`internal/imap` serves IMAP4rev1 with the extensions a modern client expects
— `IDLE`, `MOVE`, `UIDPLUS`, `LITERAL+`, `NAMESPACE`, `SPECIAL-USE` (so a
client knows which folder is Sent and which is Trash), `CONDSTORE` when
`modseq` is ready — on port 993 over TLS, and on 143 with `STARTTLS`
required. It uses the same certificate the HTTPS and SMTP listeners do.

Authentication is username plus an app password, or the account password for
a user who has one. Passkeys do not apply; SSO users get app passwords. Every
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
to one who does not. A normal user signs in to their own mailboxes and those
of the groups they are in, and sees no sign that the rest exists. The Mail
page is a folder tree on the left — every
mailbox the user can read, personal first, then the groups they are in — a
message list in the middle, and the message on the right, which is the
existing message page with its authentication panel, spam breakdown and
rendered frame. The list is the existing `DataTable`, remembering its place
as it now does, with unread rows bold, a flag column, and a search box that
runs the full text search.

Selecting rows offers mark read or unread, flag, archive, move to a folder,
delete; the same on one message. Reply, reply to all and forward open the
compose page with recipients, subject, quoted body and threading headers
filled in and, for forward, the attachments carried over; sending adds the
reply to Sent and marks the original answered. Drafts save to the Drafts
folder as a `mail` row of kind draft and reopen from there. A message opened
is marked read after a moment, not on arrival.

Mailbox settings: folders, rules with a live "which of the last hundred
messages would this match", addresses, signature, app passwords. Under
Server, three pages for whoever holds the permissions: Users, Groups, Roles —
each a list with the bindings shown inline, so "why can this person do this"
is answered on one screen.

## Milestones

Each is independently shippable and leaves the server working for everyone
who used it the day before. The order is the order of dependency.

**One — who may do what.** The access-control tables and the three seeded
roles; every existing user into Administrators. `requirePermission` in place
of `requireOperator` across every resolver, with the not-found rule. Effective
permissions on the session and in `GetSession`. The Users, Groups and Roles
pages. The command line's `user` commands grow `group` and `role` siblings.
Acceptance: a user bound only to Member can sign in and sees nothing but an
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
as any address of the user's mailboxes; the compose file and the docs. This is
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

- Decision: the access model is groups, roles, permissions and users, with
  roles bound to users or groups over the whole server or one domain.
  Rationale: it is the model the owner asked for, and the smallest one in
  which "some users manage other users" and "this group runs this domain" are
  both expressible without special cases.
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
  the reason for. The lock-out worry is handled by the last-administrator
  guard, which applies to every role equally.
  Date/Author: 2026-09-06, Ziyan

- Decision: today's views of all mail, the queue and the domains are
  restricted to roles holding those permissions. A normal user sees only the
  mailboxes they have access to.
  Rationale: right for one operator, wrong the moment a second person has an
  inbox.
  Date/Author: 2026-09-06, Ziyan

- Decision: mail programs use per-device app passwords, never the account
  password when the account has a passkey.
  Rationale: IMAP clients store what they are given in a keychain on the
  device, and a per-device secret is the one that can be revoked alone.
  Date/Author: 2026-09-06, Ziyan

- Decision: membership is the `user_group` table, a plain many-to-many.
  Rationale: that is all it is; a name like "member" suggests a row with a
  life of its own.
  Date/Author: 2026-09-06, Ziyan

### Recommendations awaiting a decision

- Proposed: try `github.com/emersion/go-imap/v2` at its beta first.
  Rationale: it is the standard Go implementation. The risk is API churn
  before a stable release, confined to `internal/imap`. The owner has
  decided that writing IMAP from the RFCs is acceptable if the library is
  trouble, so this is a choice of order, not of whether.

- Proposed: the last-administrator guard. Any change to a role, a binding, a
  group's membership or a user's enabled state that would leave no enabled
  user holding `role:manage` over the whole server is refused.
  Rationale: it is not a restriction on seeded roles — it applies to every
  role and binding alike — and without it the only recovery is SQL.

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
