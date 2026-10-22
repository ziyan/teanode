-- A person can stop their location being recorded for a while: for an hour,
-- until tomorrow morning, or until they resume. A pause is a row, not a flag,
-- so that the timeline can show the gap afterwards and the journal can say
-- that where the person was is not known.
--
-- ended_at is when the pause stops by itself, or null for a pause that lasts
-- until the person resumes. Resuming sets it to that moment. A pause is in
-- effect at an instant T when started_at <= T and (ended_at is null or
-- ended_at > T). There is at most one in effect at a time: pausing while
-- paused changes the end of the pause in effect, and the code that does it
-- holds the person's location lock, which no constraint here can express.
--
-- Everything is the person's own and goes when the account does.
CREATE TABLE "location_pause" (
    "id"         character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"    character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "started_at" timestamp with time zone NOT NULL,
    "ended_at"   timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    CHECK ("ended_at" IS NULL OR "ended_at" > "started_at")
);

-- "Is this person paused now" and "which pauses touch this range" both start
-- from the person and the start.
CREATE INDEX "location_pause_user_started" ON "location_pause" ("user_id", "started_at");
