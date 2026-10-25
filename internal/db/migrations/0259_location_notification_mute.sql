-- A person who is told on their phone when somebody sharing their location
-- with them arrives at or leaves a place can mute those notices from that one
-- person: for an hour, until tomorrow morning, or until they unmute.
--
-- user_id is the person who muted, muted_user_id the person whose arrivals
-- and departures are no longer told to them. muted_until is when the mute
-- ends by itself, or null for one that lasts until the person unmutes, which
-- deletes the row. A mute is in effect at an instant T when muted_until is
-- null or after T; a row that has ended stays until the next mute or unmute
-- of the same person replaces or deletes it, and is read as no mute.
--
-- It goes when either account does.
CREATE TABLE "location_notification_mute" (
    "user_id"       character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "muted_user_id" character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "muted_until"   timestamp with time zone,
    "created_at"    timestamp with time zone NOT NULL,
    "updated_at"    timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "muted_user_id"),
    CHECK ("user_id" <> "muted_user_id")
);
