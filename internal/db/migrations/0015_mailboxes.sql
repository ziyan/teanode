-- Mail that lives here.
--
-- A mailbox is a container of folders belonging to one user; an item is one
-- message in one folder, referencing the mail row that already exists. The
-- same message in three mailboxes is one row and three items. Nothing is
-- copied.

CREATE TABLE "mailbox" (
    "id"             character varying(32)  NOT NULL,
    "created_at"     timestamp with time zone NOT NULL,
    "modified_at"    timestamp with time zone NOT NULL,
    "user_id"        character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "name"           character varying(128) NOT NULL,   -- "Personal"; a user may have several
    "signature_html" text                   NOT NULL DEFAULT '',
    "signature_text" text                   NOT NULL DEFAULT '',
    -- Rules and the out-of-office setting are few, per-mailbox, and edited
    -- as a whole, so they are columns rather than tables of their own.
    "rules"          jsonb                  NOT NULL DEFAULT '[]',
    "autoreply"      jsonb,
    PRIMARY KEY ("id")
);
CREATE INDEX "mailbox_user" ON "mailbox" ("user_id");

CREATE TABLE "mailbox_folder" (
    "id"           character varying(32)  NOT NULL,
    "created_at"   timestamp with time zone NOT NULL,
    "modified_at"  timestamp with time zone NOT NULL,
    "mailbox_id"   character varying(32)  NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "parent_id"    character varying(32),                       -- NULL at the top
    "name"         character varying(128) NOT NULL,
    "kind"         character varying(16)  NOT NULL DEFAULT '',  -- inbox, sent, drafts, archive, junk, trash, or '' for one the owner made
    -- IMAP's contract: UIDs in a folder only ever grow, and a folder that is
    -- recreated announces itself with a new validity.
    "uid_validity" bigint                 NOT NULL,
    "uid_next"     bigint                 NOT NULL DEFAULT 1,
    -- Grows on every change to the folder's items, for clients that ask
    -- "what changed since". Also what IMAP IDLE watches.
    "modseq"       bigint                 NOT NULL DEFAULT 1,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "mailbox_folder_name" ON "mailbox_folder" ("mailbox_id", COALESCE("parent_id", ''), lower("name"));
CREATE UNIQUE INDEX "mailbox_folder_kind" ON "mailbox_folder" ("mailbox_id", "kind") WHERE "kind" <> '';

-- One message in one folder. The message is the existing mail row; this is
-- the possession of it, with its flags.
CREATE TABLE "mailbox_item" (
    "id"        character varying(32) NOT NULL,
    "folder_id" character varying(32) NOT NULL REFERENCES "mailbox_folder" ("id") ON DELETE CASCADE,
    "mail_id"   character varying(32) NOT NULL REFERENCES "mail" ("id") ON DELETE CASCADE,
    "uid"       bigint                NOT NULL,
    "modseq"    bigint                NOT NULL,
    "seen"      boolean               NOT NULL DEFAULT false,
    "flagged"   boolean               NOT NULL DEFAULT false,
    "answered"  boolean               NOT NULL DEFAULT false,
    "forwarded" boolean               NOT NULL DEFAULT false,
    "draft"     boolean               NOT NULL DEFAULT false,
    "added_at"  timestamp with time zone NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "mailbox_item_uid"    ON "mailbox_item" ("folder_id", "uid");
CREATE INDEX        "mailbox_item_mail"   ON "mailbox_item" ("mail_id");
CREATE INDEX        "mailbox_item_list"   ON "mailbox_item" ("folder_id", "added_at" DESC);
CREATE INDEX        "mailbox_item_unseen" ON "mailbox_item" ("folder_id") WHERE NOT "seen";

-- The expunge log: which UIDs left a folder, and at what modseq, so that a
-- client that last synced at modseq N is told what vanished since N.
CREATE TABLE "mailbox_folder_expunge" (
    "folder_id"   character varying(32) NOT NULL REFERENCES "mailbox_folder" ("id") ON DELETE CASCADE,
    "uid"         bigint                NOT NULL,
    "modseq"      bigint                NOT NULL,
    "expunged_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("folder_id", "uid")
);
CREATE INDEX "mailbox_folder_expunge_modseq" ON "mailbox_folder_expunge" ("folder_id", "modseq");

CREATE TABLE "mailbox_contact" (
    "mailbox_id"      character varying(32)  NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "address"         character varying(320) NOT NULL,
    "name"            character varying(256) NOT NULL DEFAULT '',
    "last_seen_at"    timestamp with time zone NOT NULL,
    "count"           integer                NOT NULL DEFAULT 1,
    "auto_replied_at" timestamp with time zone,
    PRIMARY KEY ("mailbox_id", "address")
);

-- A password a mail program uses; one per device, revocable one at a time.
CREATE TABLE "mailbox_app_password" (
    "id"            character varying(32)  NOT NULL,
    "created_at"    timestamp with time zone NOT NULL,
    "mailbox_id"    character varying(32)  NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "name"          character varying(128) NOT NULL,
    "password_hash" character varying(128) NOT NULL,
    "last_used_at"  timestamp with time zone,
    PRIMARY KEY ("id")
);
CREATE INDEX "mailbox_app_password_mailbox" ON "mailbox_app_password" ("mailbox_id");

-- The conversation a message is part of, the search document, and the clock
-- retention runs on: null while any item holds the message.
ALTER TABLE "mail"
    ADD COLUMN "thread_id"       character varying(32) NOT NULL DEFAULT '',
    ADD COLUMN "search"          tsvector,
    ADD COLUMN "unreferenced_at" timestamp with time zone;
-- Everything already here belongs to no mailbox; its clock started when it
-- arrived, which is what the old age-based sweep counted from too.
UPDATE "mail" SET "unreferenced_at" = "received_at", "thread_id" = "id";
CREATE INDEX "mail_thread"       ON "mail" ("thread_id");
CREATE INDEX "mail_message_id"   ON "mail" ("message_id") WHERE "message_id" <> '';
CREATE INDEX "mail_search"       ON "mail" USING gin ("search");
CREATE INDEX "mail_unreferenced" ON "mail" ("unreferenced_at") WHERE "unreferenced_at" IS NOT NULL;

-- A delivery into a mailbox names the mailbox and the item it produced.
ALTER TABLE "delivery"
    ADD COLUMN "mailbox_id"      character varying(32) NOT NULL DEFAULT '',
    ADD COLUMN "mailbox_item_id" character varying(32) NOT NULL DEFAULT '';
CREATE INDEX "delivery_mailbox" ON "delivery" ("mailbox_id") WHERE "mailbox_id" <> '';
