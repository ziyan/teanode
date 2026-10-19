-- A person's Health data from their iPhone: the readings (samples), each
-- day's summary of a kind of reading, a workout's route, the facts that do not
-- change much (date of birth, blood type), and who the person shares some of
-- it with.
--
-- Everything here belongs to one person and is kept until that person erases
-- it, so every table goes when the account does.

-- One reading. health_sample_id is HealthKit's own identifier for it, which is
-- what the phone sends again until the server has answered and what a
-- deletion on the phone names, so the unique constraint is what makes a batch
-- sent twice add nothing the second time.
--
-- A reading is one of three shapes and only the columns of its own shape are
-- filled: a quantity (quantity_value in quantity_unit), a category
-- (category_value, HealthKit's raw number, such as a sleep stage), or one of
-- the larger kinds (a workout, an electrocardiogram, an audiogram, a State of
-- Mind entry), whose details are in sample_metadata.
CREATE TABLE "health_sample" (
    "id"                    character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"               character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_sample_id"      character varying(64)    NOT NULL,
    "health_type"           character varying(96)    NOT NULL,
    "started_at"            timestamp with time zone NOT NULL,
    "ended_at"              timestamp with time zone NOT NULL,
    "quantity_value"        double precision,
    "quantity_unit"         character varying(32)    NOT NULL DEFAULT '',
    "category_value"        integer,
    "workout_activity_type" character varying(64)    NOT NULL DEFAULT '',
    "source_name"           character varying(256)   NOT NULL DEFAULT '',
    "source_bundle_id"      character varying(256)   NOT NULL DEFAULT '',
    "device_name"           character varying(256)   NOT NULL DEFAULT '',
    "sample_metadata"       jsonb                    NOT NULL DEFAULT '{}',
    -- Which install reported it. The reading stays when the phone is removed.
    "agent_phone_id"        character varying(32)    REFERENCES "agent_phone" ("id") ON DELETE SET NULL,
    "received_at"           timestamp with time zone NOT NULL,
    CHECK ("ended_at" >= "started_at"),
    UNIQUE ("user_id", "health_sample_id")
);

-- "The latest readings of this kind" and "the readings of this week" both read
-- newest first.
CREATE INDEX "health_sample_user_type_started" ON "health_sample" ("user_id", "health_type", "started_at" DESC);

-- Removing a phone sets agent_phone_id to null on every reading it sent, and
-- without this index PostgreSQL would read the whole table, every person's,
-- to find them.
CREATE INDEX "health_sample_agent_phone" ON "health_sample" ("agent_phone_id") WHERE "agent_phone_id" IS NOT NULL;

-- How many readings of each kind a person has, and when the latest arrived.
-- A person has millions of readings, so counting them each time a page, a
-- report or the agent asks would read all of them; these rows are kept by the
-- two triggers below, which run once for each insert or delete statement and
-- not once for each row, so they are right however the readings come and go
-- (a report, a deletion, an erase). last_received_at is when the latest
-- reading of the kind arrived and is not lowered when that reading is deleted.
CREATE TABLE "health_type_total" (
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_type"      character varying(96)    NOT NULL,
    "sample_count"     bigint                   NOT NULL CHECK ("sample_count" >= 0),
    "last_received_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "health_type")
);

CREATE FUNCTION "health_sample_count_inserted"() RETURNS trigger AS $$
BEGIN
    INSERT INTO "health_type_total" ("user_id", "health_type", "sample_count", "last_received_at")
    SELECT "user_id", "health_type", count(*), max("received_at") FROM inserted_rows GROUP BY "user_id", "health_type"
    ON CONFLICT ("user_id", "health_type") DO UPDATE SET
        "sample_count" = "health_type_total"."sample_count" + EXCLUDED."sample_count",
        "last_received_at" = greatest("health_type_total"."last_received_at", EXCLUDED."last_received_at");
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER "health_sample_inserted" AFTER INSERT ON "health_sample"
    REFERENCING NEW TABLE AS inserted_rows FOR EACH STATEMENT EXECUTE FUNCTION "health_sample_count_inserted"();

CREATE FUNCTION "health_sample_count_deleted"() RETURNS trigger AS $$
BEGIN
    UPDATE "health_type_total" SET "sample_count" = "health_type_total"."sample_count" - deleted_counts."deleted_count"
    FROM (SELECT "user_id", "health_type", count(*) AS "deleted_count" FROM deleted_rows GROUP BY "user_id", "health_type") deleted_counts
    WHERE "health_type_total"."user_id" = deleted_counts."user_id" AND "health_type_total"."health_type" = deleted_counts."health_type";
    DELETE FROM "health_type_total" USING (SELECT DISTINCT "user_id", "health_type" FROM deleted_rows) deleted_types
    WHERE "health_type_total"."user_id" = deleted_types."user_id" AND "health_type_total"."health_type" = deleted_types."health_type"
        AND "health_type_total"."sample_count" <= 0;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER "health_sample_deleted" AFTER DELETE ON "health_sample"
    REFERENCING OLD TABLE AS deleted_rows FOR EACH STATEMENT EXECUTE FUNCTION "health_sample_count_deleted"();

-- A reading that was deleted, and what it was, for 30 days. A phone that
-- deleted a reading sends its identifier and learns from the answer which
-- days to work out again; if the answer is lost, the phone sends the
-- identifier again and must get the same answer, although the reading is
-- already gone. The code that deletes readings writes this, answers from it,
-- and forgets what is older than 30 days.
CREATE TABLE "health_deleted_sample" (
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_sample_id" character varying(64)    NOT NULL,
    "health_type"      character varying(96)    NOT NULL,
    "started_at"       timestamp with time zone NOT NULL,
    "ended_at"         timestamp with time zone NOT NULL,
    "deleted_at"       timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "health_sample_id")
);
CREATE INDEX "health_deleted_sample_user_deleted" ON "health_deleted_sample" ("user_id", "deleted_at");

-- One kind of reading on one day. The phone works these out because only it
-- can merge an iPhone and a Watch counting the same walk the way the Health
-- app does, so the server keeps what the phone sent and never adds samples up
-- itself. The phone sends a day again when it changes, which replaces the row.
--
-- sum_value suits what adds up (steps, energy), average_value with the minimum
-- and maximum what does not (heart rate, weight), duration_seconds what is
-- measured in time covered (sleep, mindful minutes). category_seconds splits a
-- duration by the category's own words, for example the stages of a night's
-- sleep. A column that does not mean anything for the kind is null.
CREATE TABLE "health_day_summary" (
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_type"      character varying(96)    NOT NULL,
    "summary_day"      date                     NOT NULL,
    "quantity_unit"    character varying(32)    NOT NULL DEFAULT '',
    "sum_value"        double precision,
    "average_value"    double precision,
    "minimum_value"    double precision,
    "maximum_value"    double precision,
    "sample_count"     integer                  NOT NULL DEFAULT 0,
    "duration_seconds" double precision,
    "category_seconds" jsonb                    NOT NULL DEFAULT '{}',
    "updated_at"       timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "health_type", "summary_day")
);

-- The path of a workout, as [secondsFromStart, latitudeDegrees,
-- longitudeDegrees, altitudeMeters] rows. It is keyed by the workout's
-- HealthKit identifier rather than by a row of health_sample so that it can
-- arrive in a later batch than the workout, and it is deleted with the
-- workout by the code that deletes samples.
CREATE TABLE "health_workout_route" (
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_sample_id" character varying(64)    NOT NULL,
    "route_points"     jsonb                    NOT NULL,
    "received_at"      timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "health_sample_id")
);

-- What HealthKit calls characteristics: facts about the person rather than
-- readings. There is one value of each kind.
CREATE TABLE "health_characteristic" (
    "user_id"              character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "health_type"          character varying(96)    NOT NULL,
    "characteristic_value" character varying(64)    NOT NULL,
    "updated_at"           timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "health_type")
);

-- A person's Health shared with one person, or with every member of one
-- circle, by Health category: the people it reaches read the day summaries of
-- the kinds in those categories and nothing else. It has the same shape and
-- the same rules as location_share (sharing with a person needs the two to be
-- in a circle together, sharing with a circle needs the sharer to be a member,
-- and the code that removes a person from a circle keeps that true
-- afterwards). A share with no categories is no share, so none is stored.
CREATE TABLE "health_share" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"            character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_user_id"    character varying(32)    REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"  character varying(32)    REFERENCES "circle" ("id") ON DELETE CASCADE,
    "health_categories"  text[]                   NOT NULL,
    "created_at"         timestamp with time zone NOT NULL,
    "created_by_user_id" character varying(32)    REFERENCES "user" ("id") ON DELETE SET NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    CHECK (cardinality("health_categories") > 0),
    UNIQUE ("user_id", "grantee_user_id"),
    UNIQUE ("user_id", "grantee_circle_id")
);

-- "Whose Health is shared with me" starts from the grantee.
CREATE INDEX "health_share_grantee_user" ON "health_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "health_share_grantee_circle" ON "health_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;

-- can_agent_read_health is the person's switch for the agent's health tool. It
-- is off until they turn it on, because what the agent reads goes to the
-- model's provider. health_erased_at is when the person last erased all of
-- their Health data: the phone stops sending and forgets where it was when it
-- sees a time newer than the one it turned Health on at.
ALTER TABLE "user" ADD COLUMN "can_agent_read_health" boolean NOT NULL DEFAULT false;
ALTER TABLE "user" ADD COLUMN "health_erased_at" timestamp with time zone;

-- Keeping one's own Health data is something a person does with their own
-- account, like recording where they go, so everybody who has the ordinary
-- roles gets it. An operator can take it away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'health:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
