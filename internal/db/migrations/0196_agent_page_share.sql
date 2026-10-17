-- A person shares a page, and everything under it, with one other person or
-- with a circle. The reader reads the owner's rows as they are now, through
-- the memory tool and the Knowledge page, and nothing is copied into the
-- reader's graph. docs/decisions/20261016-shared-knowledge-is-read-where-it-lives.md
-- says why.
--
-- path is the root of the shared subtree: a share of "people/a" reaches the
-- page at people/a and every page under people/a/, and never people/ab. The
-- boundary is a slash, which the code that reads a share checks and this table
-- cannot, so a share never stores a trailing slash.
--
-- Sharing with a person needs the owner and that person to be in a circle
-- together, and sharing with a circle needs the owner to be in it. That is
-- checked when the share is made. What keeps it true afterwards is not this
-- table: a circle's share reaches only members while the owner is still in
-- that circle, by the join on circle_member, and the code that takes a person
-- out of a circle deletes the direct shares between people who no longer share
-- one.
CREATE TABLE "agent_page_share" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "agent_id"           character varying(32)  NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "path"               character varying(500) NOT NULL CHECK ("path" <> '' AND "path" NOT LIKE '%/'),
    "grantee_user_id"    character varying(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"  character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,

    -- Whether the words each fact was read in are shown to the reader. Off
    -- unless the owner chose it: a fact about an appointment can be shared
    -- without the mail it was read in.
    "is_quote_shared"    boolean NOT NULL DEFAULT false,

    "created_at"         timestamp with time zone NOT NULL,
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("agent_id", "path", "grantee_user_id"),
    UNIQUE ("agent_id", "path", "grantee_circle_id")
);

-- "What is shared with me" starts from the grantee.
CREATE INDEX "agent_page_share_grantee_user" ON "agent_page_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "agent_page_share_grantee_circle" ON "agent_page_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
