-- What one person is to another who shares a circle with them: "Sam is my
-- father". It is one fact between the two, so there is one row for the pair,
-- written from the side of whoever said it last: user_id said that
-- related_user_id is their relationship_kind. The other side is the opposite
-- kind (a parent's child), and is not stored.
--
-- related_user_term_gender is the gender of the word user_id used ("wife" is
-- feminine, "spouse" neutral). It is a word's, not a person's: how a person is
-- described is theirs to say, in user.pronoun, and when they have said it that
-- is what their words follow.
--
-- Only the two of them see it, and only while they share a circle: it is
-- deleted in the transaction in which they stop sharing any, as a share
-- between them is, and checked when it is read and when it is said.
CREATE TABLE "person_relationship" (
    "id"                       character varying(32) NOT NULL PRIMARY KEY,
    "created_at"               timestamp with time zone NOT NULL,
    "modified_at"              timestamp with time zone NOT NULL,
    "user_id"                  character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "related_user_id"          character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "relationship_kind"        character varying(32) NOT NULL
        CHECK ("relationship_kind" IN ('spouse', 'partner', 'parent', 'child', 'sibling', 'grandparent', 'grandchild',
            'aunt_or_uncle', 'niece_or_nephew', 'cousin', 'parent_in_law', 'child_in_law', 'sibling_in_law',
            'step_parent', 'step_child', 'friend', 'colleague')),
    "related_user_term_gender" character varying(16) NOT NULL
        CHECK ("related_user_term_gender" IN ('masculine', 'feminine', 'neutral')),
    CHECK ("user_id" <> "related_user_id")
);

-- One fact per pair, whichever of the two said it.
CREATE UNIQUE INDEX "person_relationship_pair" ON "person_relationship"
    (LEAST("user_id", "related_user_id"), GREATEST("user_id", "related_user_id"));
-- A person's relationships are read from either side, on every turn of their
-- agent; the pair's index above is on expressions and serves neither.
CREATE INDEX "person_relationship_user" ON "person_relationship" ("user_id");
CREATE INDEX "person_relationship_related_user" ON "person_relationship" ("related_user_id");

-- When one person last told another about a relationship between them, so
-- that saying it, taking it back and saying it again tells the other at most
-- once a day. It is kept apart from the relationship, which taking back
-- deletes.
CREATE TABLE "person_relationship_notice" (
    "user_id"      character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "told_user_id" character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "told_at"      timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "told_user_id")
);
CREATE INDEX "person_relationship_notice_told_user" ON "person_relationship_notice" ("told_user_id");

-- How a person is described: he, she or they. NULL until they say. It is
-- shown to the people who share a circle with them, as their name is.
ALTER TABLE "user" ADD COLUMN "pronoun" character varying(8)
    CHECK ("pronoun" IN ('he', 'she', 'they'));
