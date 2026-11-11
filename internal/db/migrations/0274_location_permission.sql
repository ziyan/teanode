-- What a person's phone lets TeaNode record of where they are, as the app
-- last said: "always" records visits in the background, "while_using" only
-- where they are while the app is open, "off" nothing (Location is off for
-- the app, or they have not answered the question yet). is_recording_on is
-- the app's own switch for recording.
--
-- It decides how much of other people's location the person sees: nobody
-- sees more of another's location than they share with them, and a phone
-- that cannot record visits shares no visits. A person whose app never
-- reported has no row and is judged by their shares alone.
--
-- One row a person: the newest report, from whichever phone sent it.
CREATE TABLE "location_permission" (
    "user_id"          character varying(32)    NOT NULL PRIMARY KEY REFERENCES "user" ("id") ON DELETE CASCADE,
    "phone_permission" character varying(32)    NOT NULL CHECK ("phone_permission" IN ('always', 'while_using', 'off')),
    "is_recording_on"  boolean                  NOT NULL,
    "reported_at"      timestamp with time zone NOT NULL
);
