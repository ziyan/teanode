-- A person's own address book, and the contacts in it.
--
-- Distinct from "mailbox_contact", which is every address a mailbox has
-- written to or heard from, learned from traffic for completion and for the
-- "sender is known" rule. That is a list of people you have corresponded
-- with; this is a list of people you chose to keep. An address learned there
-- becomes a contact here only when somebody saves it.
--
-- The book belongs to the account rather than to a mailbox: a person with two
-- mailboxes has one address book, reachable through either. Signing in from a
-- phone is still per mailbox, with one of its app passwords, and the book a
-- client then sees is the one belonging to the account that owns that mailbox.
CREATE TABLE "addressbook" (
    "id"          varchar(32)  NOT NULL,
    "user_id"     varchar(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "name"        varchar(200) NOT NULL DEFAULT '',
    "description" text         NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE INDEX "addressbook_user" ON "addressbook" ("user_id");

-- The vCard text is the contact. Everything beside it is extracted when the
-- card is written, so that listing, searching and completing never have to
-- parse a vCard; when the two disagree the card is right.
--
-- "uid" is the card's own UID property, which is how a client names the same
-- person across devices. The unique index on it is what stops a contact being
-- added twice by two devices that each think they are creating it.
--
-- "etag" names a version of the card. A client sends it back in an If-Match
-- header when writing, and a write whose ETag is stale is refused, so that two
-- devices editing at once cannot silently overwrite one another.
CREATE TABLE "contact" (
    "id"             varchar(32)  NOT NULL,
    "addressbook_id" varchar(32)  NOT NULL REFERENCES "addressbook" ("id") ON DELETE CASCADE,
    "created_at"     timestamptz  NOT NULL,
    "modified_at"    timestamptz  NOT NULL,
    "uid"            varchar(255) NOT NULL,
    "etag"           varchar(64)  NOT NULL,
    "card"           text         NOT NULL,
    "name"           varchar(256) NOT NULL DEFAULT '',
    "organization"   varchar(256) NOT NULL DEFAULT '',
    "emails"         text         NOT NULL DEFAULT '',
    "phones"         text         NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "contact_uid" ON "contact" ("addressbook_id", "uid");
CREATE INDEX "contact_name" ON "contact" ("addressbook_id", "name");

-- Keeping an address book is something a member does with their own account,
-- like having a mailbox, so everybody who can read mail here gets it. An
-- operator can take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'contacts:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
