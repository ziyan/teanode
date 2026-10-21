-- A person's Passwords: logins, cards and notes that only their own devices
-- can read, and who they share some of them with.
--
-- The server never holds a key that opens an item. Every value in these
-- tables that is named "by" a key (account_key_by_passphrase,
-- item_key_by_recipient_key and so on) is sealed text made on a device, in
-- the JSON format internal/passwords describes, and the server stores and
-- hands it back without reading it. The one exception is item_key_by_agent_key,
-- an item key sealed for the server's own agent key, which exists only for an
-- item the person turned "Let the agent use this" on.
--
-- Everything here belongs to one person, so every table goes when the account
-- does.

-- What a person set up: the account key sealed twice (once by the key the
-- passphrase makes, once by the recovery key), proofs of those two secrets
-- (the server keeps only a hash of each proof), and the person's key pair for
-- being shared with. The salt and iteration count are what a device needs to
-- make the passphrase key again.
CREATE TABLE "password_setup" (
    "user_id"                     character varying(32)    NOT NULL PRIMARY KEY REFERENCES "user" ("id") ON DELETE CASCADE,
    "account_key_by_passphrase"   text                     NOT NULL,
    "passphrase_salt"             text                     NOT NULL,
    "passphrase_iteration_count"  integer                  NOT NULL CHECK ("passphrase_iteration_count" > 0),
    "passphrase_proof_hash"       text                     NOT NULL,
    "account_key_by_recovery_key" text                     NOT NULL,
    "recovery_key_proof_hash"     text                     NOT NULL,
    "public_key"                  text                     NOT NULL,
    "private_key_by_account_key"  text                     NOT NULL,
    "lock_after_minutes"          integer                  NOT NULL DEFAULT 15 CHECK ("lock_after_minutes" BETWEEN 1 AND 10080),
    "created_at"                  timestamp with time zone NOT NULL,
    "updated_at"                  timestamp with time zone NOT NULL
);

-- Every change a reader must see (a save, a delete, a restore, a change of who
-- it is shared with, a change of the agent's use) gives the item the next
-- number of this sequence, so that a device asks for what changed since the
-- highest number it has. The code takes an advisory lock before drawing a
-- number, so that numbers are committed in the order they are drawn and a
-- device never skips one.
CREATE SEQUENCE "password_item_revision";

-- One login, card or note. The id is made by the device, as a lower case UUID,
-- because the encryption names it (the sealed fields and the sealed keys are
-- bound to it). A deleted item stays 30 days with deleted_at set, then a sweep
-- removes it. An item the agent may use has is_usable_by_agent set and an
-- item_key_by_agent_key; a card is never usable by the agent.
CREATE TABLE "password_item" (
    "id"                       character varying(36)    NOT NULL PRIMARY KEY
        CHECK ("id" ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    "owner_user_id"            character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "item_kind"                character varying(16)    NOT NULL CHECK ("item_kind" IN ('login', 'card', 'note')),
    "encrypted_fields"         text                     NOT NULL,
    "item_key_by_account_key"  text                     NOT NULL,
    "revision"                 bigint                   NOT NULL DEFAULT nextval('password_item_revision'),
    "is_usable_by_agent"       boolean                  NOT NULL DEFAULT false,
    "item_key_by_agent_key"    text,
    "last_used_by_agent_at"    timestamp with time zone,
    "deleted_at"               timestamp with time zone,
    "created_at"               timestamp with time zone NOT NULL,
    "updated_at"               timestamp with time zone NOT NULL,
    CHECK ("item_key_by_agent_key" IS NULL OR "is_usable_by_agent"),
    CHECK ("is_usable_by_agent" = false OR "item_kind" <> 'card')
);

-- "What changed in my items since revision N" reads the owner's items in
-- revision order.
CREATE INDEX "password_item_owner_revision" ON "password_item" ("owner_user_id", "revision");

-- The sweep that removes items deleted 30 days ago reads only the deleted.
CREATE INDEX "password_item_deleted" ON "password_item" ("deleted_at") WHERE "deleted_at" IS NOT NULL;

-- The versions a save replaced, newest 20 for each item, kept 30 days (the
-- hourly sweep removes older ones). A save overwrites the sealed fields, so
-- without this a device that saved a mistake, or a bug that saved the wrong
-- thing, would have destroyed the only copy. The revision is the one the item
-- had before the save. Nothing reads these rows through the API: they are for
-- an operator to put back from the database.
CREATE TABLE "password_item_version" (
    "item_id"                  character varying(36)    NOT NULL REFERENCES "password_item" ("id") ON DELETE CASCADE,
    "revision"                 bigint                   NOT NULL,
    "encrypted_fields"         text                     NOT NULL,
    "item_key_by_account_key"  text                     NOT NULL,
    "replaced_at"              timestamp with time zone NOT NULL,
    PRIMARY KEY ("item_id", "revision")
);

-- The sweep removes the versions replaced more than 30 days ago.
CREATE INDEX "password_item_version_replaced" ON "password_item_version" ("replaced_at");

-- An item shared with one person, or with every member of one circle. It has
-- the same shape and the same reach as health_share (sharing with a person
-- needs the two to be in a circle together, sharing with a circle needs the
-- owner to be a member, and the code that removes a person from a circle keeps
-- that true afterwards). A share says who may be given the item's key; it opens
-- nothing by itself: a recipient reads the item only once a password_share_key
-- row exists for them.
CREATE TABLE "password_share" (
    "id"                character varying(32)    NOT NULL PRIMARY KEY,
    "item_id"           character varying(36)    NOT NULL REFERENCES "password_item" ("id") ON DELETE CASCADE,
    "owner_user_id"     character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_user_id"   character varying(32)    REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id" character varying(32)    REFERENCES "circle" ("id") ON DELETE CASCADE,
    "created_at"        timestamp with time zone NOT NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("item_id", "grantee_user_id"),
    UNIQUE ("item_id", "grantee_circle_id")
);

-- "What is shared with me" starts from the grantee; "what did this person
-- share" from the owner.
CREATE INDEX "password_share_grantee_user" ON "password_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "password_share_grantee_circle" ON "password_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
CREATE INDEX "password_share_owner" ON "password_share" ("owner_user_id");

-- An item's key sealed for one recipient's public key. recipient_public_key is
-- the key it was sealed for: a recipient who erased their Passwords and set up
-- again has a new key, and a row made for the old one opens nothing, so it is
-- not used and the recipient is listed as waiting until the owner's device
-- seals the key again. A circle share has a row for each member who has set
-- up Passwords; a member who has not, or who joined later, has none yet.
CREATE TABLE "password_share_key" (
    "item_id"                    character varying(36)    NOT NULL REFERENCES "password_item" ("id") ON DELETE CASCADE,
    "recipient_user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "item_key_by_recipient_key"  text                     NOT NULL,
    "recipient_public_key"       text                     NOT NULL,
    "created_at"                 timestamp with time zone NOT NULL,
    PRIMARY KEY ("item_id", "recipient_user_id")
);
CREATE INDEX "password_share_key_recipient" ON "password_share_key" ("recipient_user_id");

-- The server's own key pair, made the first time something asks for it. The
-- private scalar is sealed with the server secret (secretbox); the public key
-- is what a device seals an item key for when the person lets the agent use
-- that item. There is one row.
CREATE TABLE "password_agent_key" (
    "singleton"          boolean                  NOT NULL PRIMARY KEY DEFAULT true CHECK ("singleton"),
    "public_key"         text                     NOT NULL,
    "private_key_sealed" text                     NOT NULL,
    "created_at"         timestamp with time zone NOT NULL
);

-- A card the agent raised remembers whether the turn that raised it was one
-- the person started. The turn that carries on an answer given later counts
-- as the person's only when that turn did: otherwise a card raised by a turn
-- a chat app, a mail or a program began would hand the person's standing to
-- whatever the answer starts. A card from before this column is not the
-- person's.
ALTER TABLE "agent_interaction" ADD COLUMN "is_started_by_person" boolean NOT NULL DEFAULT false;

-- Keeping one's own passwords is something a person does with their own
-- account, so everybody who has the ordinary roles gets it. An operator can
-- take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'passwords:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
