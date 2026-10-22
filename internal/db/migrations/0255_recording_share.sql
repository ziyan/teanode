-- A recording can be shared with a person or with a circle at a level.
-- recording_share_level is how much they read: summary (title, date, length,
-- the generated summary and the speaker names the owner confirmed) or
-- transcript (the summary, and the transcript with the owner's corrections).
-- The audio, the voice samples and the contacts the owner tied speakers to
-- are never shared at any level.
--
-- Sharing with a person needs the sharer and that person to be in a circle
-- together, and sharing with a circle needs the sharer to be a member of it.
-- That is checked when the share is made. What keeps it true afterwards is not
-- this table: a circle's share reaches only members whose circle the
-- recording's owner is still in, by the join on circle_member, and the code
-- that takes a person out of a circle deletes the direct shares between people
-- who no longer share one.
--
-- recording.user_id is nullable (the recording stays while its owner's account
-- is deleted, until its media is cleaned up): a recording with no owner is
-- shared with nobody, because no query below finds an owner to join.
CREATE TABLE "recording_share" (
    "id"                    character varying(32)  NOT NULL PRIMARY KEY,
    "recording_id"          character varying(32)  NOT NULL REFERENCES "recording" ("id") ON DELETE CASCADE,
    "grantee_user_id"       character varying(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"     character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "recording_share_level" character varying(16)  NOT NULL
        CHECK ("recording_share_level" IN ('summary', 'transcript')),
    "created_at"            timestamp with time zone NOT NULL,
    "created_by_user_id"    character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("recording_id", "grantee_user_id"),
    UNIQUE ("recording_id", "grantee_circle_id")
);

-- "Which recordings are shared with me" starts from the grantee.
CREATE INDEX "recording_share_grantee_user" ON "recording_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "recording_share_grantee_circle" ON "recording_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;
