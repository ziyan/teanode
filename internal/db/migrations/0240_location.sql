-- Where a person goes: the positions their phone or browser reported, the
-- places those positions are at, the stays at those places, and who the
-- person shares it with.
--
-- Everything here belongs to one person and is kept until that person deletes
-- it, so every table goes when the account does.

-- One position a phone or browser reported. The phone sends a point only when
-- it has moved, so a day is a few hundred rows at most. The unique index is
-- what makes a batch sent twice (the phone sends again until the server has
-- answered) add nothing the second time.
CREATE TABLE "location_point" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"                 character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "latitude_degrees"        double precision         NOT NULL,
    "longitude_degrees"       double precision         NOT NULL,
    "accuracy_meters"         double precision         NOT NULL,
    "altitude_meters"         double precision,
    "speed_meters_per_second" double precision,
    "course_degrees"          double precision,
    "measured_at"             timestamp with time zone NOT NULL,
    "received_at"             timestamp with time zone NOT NULL,
    "location_source"         character varying(16)    NOT NULL
        CHECK ("location_source" IN ('phone', 'browser')),
    -- Which install reported it. The point stays when the phone is removed.
    "agent_phone_id"          character varying(32)    REFERENCES "agent_phone" ("id") ON DELETE SET NULL,
    UNIQUE ("user_id", "measured_at", "location_source")
);

-- "Where was I last" and "the path of a day" both read newest first.
CREATE INDEX "location_point_user_measured" ON "location_point" ("user_id", "measured_at" DESC);

-- A spot with a name and a radius, belonging to one person. Two people who
-- visit the same park each have their own place for it, because what a person
-- calls a place is theirs.
--
-- place_name is what the person sees: the name they gave it, otherwise the
-- OpenStreetMap name, otherwise the street address, otherwise the city.
-- is_named_by_person says whether the first of those is the case, so that a
-- later answer from OpenStreetMap never replaces a name somebody chose.
--
-- place_label is a fixed vocabulary the program can act on (an icon, "left
-- work", the town for the weekly look around) where a name cannot be. More
-- than one place may carry the same label.
--
-- A place is named after its city when it is first made and by OpenStreetMap
-- later: named_at is set when the provider answered (whatever it answered),
-- naming_claimed_at is set by the instance that took the place to name, so two
-- instances do not ask for the same one, and naming_error_message says why the
-- last try failed.
CREATE TABLE "place" (
    "id"                    character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"               character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "place_name"            character varying(256)   NOT NULL DEFAULT '',
    "is_named_by_person"    boolean                  NOT NULL DEFAULT false,
    "place_label"           character varying(16)    NOT NULL DEFAULT ''
        CHECK ("place_label" IN ('', 'home', 'work', 'school', 'family', 'gym')),
    "latitude_degrees"      double precision         NOT NULL,
    "longitude_degrees"     double precision         NOT NULL,
    "radius_meters"         double precision         NOT NULL DEFAULT 75
        CHECK ("radius_meters" >= 25 AND "radius_meters" <= 2000),
    "osm_type"              character varying(16)    NOT NULL DEFAULT ''
        CHECK ("osm_type" IN ('', 'node', 'way', 'relation')),
    "osm_id"                bigint,
    "osm_name"              character varying(256)   NOT NULL DEFAULT '',
    "osm_category"          character varying(128)   NOT NULL DEFAULT '',
    "address_text"          character varying(512)   NOT NULL DEFAULT '',
    "named_at"              timestamp with time zone,
    "naming_claimed_at"     timestamp with time zone,
    "naming_error_message"  character varying(512)   NOT NULL DEFAULT '',
    "created_at"            timestamp with time zone NOT NULL,
    "updated_at"            timestamp with time zone NOT NULL
);

CREATE INDEX "place_user" ON "place" ("user_id");
-- "Where is home" and "how long was I at work" start from the label.
CREATE INDEX "place_user_label" ON "place" ("user_id", "place_label") WHERE "place_label" <> '';
-- The naming job looks for places nobody has named yet, across all people.
CREATE INDEX "place_unnamed" ON "place" ("naming_claimed_at") WHERE "named_at" IS NULL;

-- One stay at a place. arrived_at is null when iOS reported a departure whose
-- arrival it did not see, and departed_at is null while the person is still
-- there. client_visit_key is what the phone calls the stay, so that a report
-- sent twice, or an arrival followed later by its departure, is one row. A
-- stay recorded by hand has no key from a phone and is given one.
CREATE TABLE "place_visit" (
    "id"                character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"           character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "place_id"          character varying(32)    NOT NULL REFERENCES "place" ("id") ON DELETE CASCADE,
    "arrived_at"        timestamp with time zone,
    "departed_at"       timestamp with time zone,
    "latitude_degrees"  double precision         NOT NULL,
    "longitude_degrees" double precision         NOT NULL,
    "accuracy_meters"   double precision         NOT NULL DEFAULT 0,
    -- visit: iOS visit monitoring. region: the phone entered or left a place
    -- the person named. check_in: recorded by hand. browser: the dashboard.
    "visit_source"      character varying(16)    NOT NULL
        CHECK ("visit_source" IN ('visit', 'region', 'check_in', 'browser')),
    "client_visit_key"  character varying(128)   NOT NULL,
    "created_at"        timestamp with time zone NOT NULL,
    "updated_at"        timestamp with time zone NOT NULL,
    UNIQUE ("user_id", "client_visit_key")
);

CREATE INDEX "place_visit_user_arrived" ON "place_visit" ("user_id", "arrived_at" DESC);
CREATE INDEX "place_visit_place" ON "place_visit" ("place_id");

-- A person's location shared with one person, or with every member of one
-- circle, at a level: current (where they are now, and the place) or history
-- (that, and their visits). It has the same shape as calendar_share and the
-- same rules: sharing with a person needs the two to be in a circle together,
-- sharing with a circle needs the sharer to be a member, and what keeps that
-- true afterwards is the code that removes a person from a circle. Nothing
-- reads this table until sharing is built.
CREATE TABLE "location_share" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "user_id"            character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_user_id"    character varying(32)  REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"  character varying(32)  REFERENCES "circle" ("id") ON DELETE CASCADE,
    "share_level"        character varying(16)  NOT NULL
        CHECK ("share_level" IN ('current', 'history')),
    "created_at"         timestamp with time zone NOT NULL,
    "created_by_user_id" character varying(32)  REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("user_id", "grantee_user_id"),
    UNIQUE ("user_id", "grantee_circle_id")
);

-- "Whose location is shared with me" starts from the grantee.
CREATE INDEX "location_share_grantee_user" ON "location_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "location_share_grantee_circle" ON "location_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;

-- Recording where you go is something a person does with their own account,
-- like keeping a calendar, so everybody who has the ordinary roles gets it. An
-- operator can take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'location:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
