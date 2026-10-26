-- How a person got from one place to the next. Four tables.
--
-- motion_activity is what the phone's motion sensor said the person was doing
-- from a moment on: it lasts until the next row starts. A report sent twice
-- adds nothing, because a person has one activity starting at a given instant.
--
-- trip is the travel between the departure of one visit and the arrival of
-- the next, and trip_leg is a part of a trip made one way. Both are derived
-- from visits, positions and motion activity and are deleted and built again
-- whenever those change, so nothing the person said lives in them. The visits
-- a trip joins are kept only as a reference: when one goes, the trip stays.
--
-- travel_mode_correction is the person saying that a span of time was
-- traveled some way. It is kept apart from the legs so that it survives a
-- rebuild: a leg's way is the correction covering at least half of its time,
-- if any, otherwise detected_travel_mode.
--
-- The ways are walk, run, bike, car, bus, train, boat and plane. Only walk,
-- run, bike, car and plane are ever detected; bus, train and boat come from
-- a correction. detection_source says whether a leg's way came from the
-- phone's motion activity or was guessed from speed.
--
-- Everything is the person's own and goes when the account does.
CREATE TABLE "motion_activity" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"             character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "started_at"          timestamp with time zone NOT NULL,
    "activity_kind"       character varying(16)    NOT NULL,
    "activity_confidence" character varying(16)    NOT NULL,
    "created_at"          timestamp with time zone NOT NULL,
    UNIQUE ("user_id", "started_at"),
    CHECK ("activity_kind" IN ('stationary', 'walking', 'running', 'cycling', 'automotive', 'unknown')),
    CHECK ("activity_confidence" IN ('low', 'medium', 'high'))
);

CREATE INDEX "motion_activity_user_started" ON "motion_activity" ("user_id", "started_at");

CREATE TABLE "trip" (
    "id"              character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"         character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "started_at"      timestamp with time zone NOT NULL,
    "ended_at"        timestamp with time zone NOT NULL,
    "from_visit_id"   character varying(32)    REFERENCES "place_visit" ("id") ON DELETE SET NULL,
    "to_visit_id"     character varying(32)    REFERENCES "place_visit" ("id") ON DELETE SET NULL,
    "distance_meters" double precision         NOT NULL,
    "created_at"      timestamp with time zone NOT NULL,
    CHECK ("ended_at" > "started_at")
);

CREATE INDEX "trip_user_started" ON "trip" ("user_id", "started_at");
CREATE INDEX "trip_from_visit" ON "trip" ("from_visit_id");
CREATE INDEX "trip_to_visit" ON "trip" ("to_visit_id");

CREATE TABLE "trip_leg" (
    "id"                   character varying(32)    NOT NULL PRIMARY KEY,
    "trip_id"              character varying(32)    NOT NULL REFERENCES "trip" ("id") ON DELETE CASCADE,
    "user_id"              character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "started_at"           timestamp with time zone NOT NULL,
    "ended_at"             timestamp with time zone NOT NULL,
    "detected_travel_mode" character varying(16)    NOT NULL,
    "detection_source"     character varying(16)    NOT NULL,
    "distance_meters"      double precision         NOT NULL,
    CHECK ("ended_at" > "started_at"),
    CHECK ("detected_travel_mode" IN ('walk', 'run', 'bike', 'car', 'bus', 'train', 'boat', 'plane')),
    CHECK ("detection_source" IN ('motion', 'speed'))
);

CREATE INDEX "trip_leg_user_started" ON "trip_leg" ("user_id", "started_at");
CREATE INDEX "trip_leg_trip" ON "trip_leg" ("trip_id");

CREATE TABLE "travel_mode_correction" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"     character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "started_at"  timestamp with time zone NOT NULL,
    "ended_at"    timestamp with time zone NOT NULL,
    "travel_mode" character varying(16)    NOT NULL,
    "created_at"  timestamp with time zone NOT NULL,
    "updated_at"  timestamp with time zone NOT NULL,
    CHECK ("ended_at" > "started_at"),
    CHECK ("travel_mode" IN ('walk', 'run', 'bike', 'car', 'bus', 'train', 'boat', 'plane'))
);

CREATE INDEX "travel_mode_correction_user_started" ON "travel_mode_correction" ("user_id", "started_at");

-- The people whose days from before trips were built have had their trips
-- built: the server does it once, at start, for each person not in here.
CREATE TABLE "trip_backfill" (
    "user_id"       character varying(32)    NOT NULL PRIMARY KEY REFERENCES "user" ("id") ON DELETE CASCADE,
    "backfilled_at" timestamp with time zone NOT NULL
);
