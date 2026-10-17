-- A circle is a named set of accounts on one server that know each other: a
-- family, some friends, colleagues. Later features share things with a circle
-- or with a person in one; this is only the list of who belongs together.
--
-- circle_kind is how the screens describe it ("your family"), and decides
-- nothing else. member_role is admin or member: an admin manages who is in the
-- circle, a member takes part, and neither role ever decides what somebody may
-- see of anybody else's, which is always the owner's choice.
--
-- A circle always keeps at least one admin and is deleted when its last member
-- leaves. Neither rule is a constraint here, because both depend on the other
-- rows; they are kept by the code that changes membership, in one transaction
-- holding the circle's row.
CREATE TABLE "circle" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "created_at"         timestamp with time zone NOT NULL,
    "modified_at"        timestamp with time zone NOT NULL,
    "name"               character varying(100) NOT NULL,
    "circle_kind"        character varying(16)  NOT NULL
        CHECK ("circle_kind" IN ('family', 'friends', 'colleagues')),
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL
);

CREATE TABLE "circle_member" (
    "circle_id"          character varying(32)  NOT NULL REFERENCES "circle" ("id") ON DELETE CASCADE,
    "user_id"            character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "member_role"        character varying(16)  NOT NULL
        CHECK ("member_role" IN ('admin', 'member')),
    "joined_at"          timestamp with time zone NOT NULL,
    "invited_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    PRIMARY KEY ("circle_id", "user_id")
);

-- "Which circles am I in" and "who shares a circle with me" both start from a
-- person, and the primary key starts from the circle.
CREATE INDEX "circle_member_user" ON "circle_member" ("user_id");

-- Joining is by invitation, naming an exact username. A row stays after it is
-- answered, with the answer, so a circle can say who declined; answered_at and
-- invitation_answer are both NULL while it waits. It expires thirty days after
-- it was made, which is checked when it is read rather than by a sweep.
CREATE TABLE "circle_invitation" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "circle_id"          character varying(32)  NOT NULL REFERENCES "circle" ("id") ON DELETE CASCADE,
    "invited_user_id"    character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "invited_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "member_role"        character varying(16)  NOT NULL
        CHECK ("member_role" IN ('admin', 'member')),
    "created_at"         timestamp with time zone NOT NULL,
    "expires_at"         timestamp with time zone NOT NULL,
    "answered_at"        timestamp with time zone,
    "invitation_answer"  character varying(16)
        CHECK ("invitation_answer" IN ('accepted', 'declined', 'withdrawn')),
    CHECK (("answered_at" IS NULL) = ("invitation_answer" IS NULL))
);

-- One waiting invitation per person per circle: inviting again renews it.
CREATE UNIQUE INDEX "circle_invitation_waiting" ON "circle_invitation" ("circle_id", "invited_user_id")
    WHERE "answered_at" IS NULL;
CREATE INDEX "circle_invitation_invited_user" ON "circle_invitation" ("invited_user_id")
    WHERE "answered_at" IS NULL;

-- Making a circle is something a person does with their own account, like
-- keeping a calendar, so everybody who has the ordinary roles gets it. An
-- operator can take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'circle:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
