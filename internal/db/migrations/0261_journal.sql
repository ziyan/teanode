-- The journal: one page per day of a person's life, and the weeks, months and
-- years above the days.
--
-- A journal day links to what happened that day (photos, visits, events,
-- transactions, messages) and copies none of it: those are read from their
-- own tables by the day's time range whenever the day is read. What is kept
-- here is what only the journal has: the person's own words and mood, the
-- day's weather, the narrative the agent writes in its dreams, the records the
-- agent chose as mattering (with why) or the person pinned, and the day's
-- numbers (facts), which the timeline shows without reading every source for
-- every day.
--
-- Everything here belongs to one person and goes with the account.

-- One day. day is the person's calendar date and time_zone the zone its edges
-- were cut in when the row was made, so that a later change of zone does not
-- move what was in a day.
--
-- words is what the person wrote, the original. touched_up_words is the
-- version the agent made from it when the person asked (null when touching
-- up is off for the day), touch_up_how what they asked for, and
-- touched_up_from_digest the digest of the words it was made from, so a
-- touched-up text made from older words is known to be stale.
--
-- mood_level is 1 (awful) to 5 (great), null when not chosen; mood_source
-- says whether the person chose it or it came from Apple Health's State of
-- Mind.
--
-- weather and facts are JSON objects (models.JournalWeather and
-- models.JournalFacts): numbers and ids, never content.
--
-- narrative_input_digest is a digest of what the narrative was written from,
-- so a day whose records or words changed since is written again.
-- narrative_corrections is the person's corrections, a JSON list of
-- {correctionText, createdAt}, given to every later writing as facts to keep.
-- is_narrative_requested marks a day older than the depth dreams write back
-- to whose narrative the person asked for.
CREATE TABLE "journal_day" (
    "id"                       character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"                  character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "day"                      date                     NOT NULL,
    "time_zone"                character varying(64)    NOT NULL,
    "words"                    text                     NOT NULL DEFAULT '',
    "words_modified_at"        timestamp with time zone,
    "touched_up_words"         text,
    "touch_up_how"             text                     NOT NULL DEFAULT '',
    "touched_up_from_digest"   character varying(64)    NOT NULL DEFAULT '',
    "touch_up_requested_at"    timestamp with time zone,
    "mood_level"               smallint                 CHECK ("mood_level" BETWEEN 1 AND 5),
    "mood_words"               text                     NOT NULL DEFAULT '',
    "mood_source"              character varying(16)    NOT NULL DEFAULT '' CHECK ("mood_source" IN ('', 'person', 'health')),
    "weather"                  jsonb,
    "facts"                    jsonb                    NOT NULL DEFAULT '{}',
    "facts_computed_at"        timestamp with time zone,
    "headline"                 text                     NOT NULL DEFAULT '',
    "narrative"                text                     NOT NULL DEFAULT '',
    "narrative_written_at"     timestamp with time zone,
    "narrative_input_digest"   character varying(64)    NOT NULL DEFAULT '',
    "narrative_failure"        text                     NOT NULL DEFAULT '',
    "narrative_corrections"    jsonb                    NOT NULL DEFAULT '[]',
    "narrative_requested_at"   timestamp with time zone,
    "is_narrative_requested"   boolean                  NOT NULL DEFAULT false,
    "cover_photo_id"           character varying(32),
    "created_at"               timestamp with time zone NOT NULL,
    "modified_at"              timestamp with time zone NOT NULL,
    UNIQUE ("user_id", "day")
);

-- The timeline reads a person's days newest first.
CREATE INDEX "journal_day_user_day" ON "journal_day" ("user_id", "day" DESC);

-- A week (period_start its first day), a month (the first of the month) or a
-- year (January 1). looking_back is the person's own text about it, with the
-- same touching up as a day's words. chapters is a year's stretches that
-- belong together, a JSON list of {fromDay, untilDay, title, detail}.
CREATE TABLE "journal_period" (
    "id"                       character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"                  character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "period_kind"              character varying(8)     NOT NULL CHECK ("period_kind" IN ('week', 'month', 'year')),
    "period_start"             date                     NOT NULL,
    "looking_back"             text                     NOT NULL DEFAULT '',
    "looking_back_modified_at" timestamp with time zone,
    "touched_up_looking_back"  text,
    "touch_up_how"             text                     NOT NULL DEFAULT '',
    "touched_up_from_digest"   character varying(64)    NOT NULL DEFAULT '',
    "touch_up_requested_at"    timestamp with time zone,
    "headline"                 text                     NOT NULL DEFAULT '',
    "narrative"                text                     NOT NULL DEFAULT '',
    "narrative_written_at"     timestamp with time zone,
    "narrative_input_digest"   character varying(64)    NOT NULL DEFAULT '',
    "narrative_failure"        text                     NOT NULL DEFAULT '',
    "narrative_corrections"    jsonb                    NOT NULL DEFAULT '[]',
    "narrative_requested_at"   timestamp with time zone,
    "chapters"                 jsonb                    NOT NULL DEFAULT '[]',
    "created_at"               timestamp with time zone NOT NULL,
    "modified_at"              timestamp with time zone NOT NULL,
    UNIQUE ("user_id", "period_kind", "period_start")
);

-- A record a day or a period links to by hand or by the agent's choice: a
-- kind and an id, as a memory page links to a record (agent_record_link), read
-- through the same resolver, which checks the reader may see it each time.
-- link_source is agent (a choice, with link_reason saying why it mattered) or
-- person (a pin). is_hidden is an agent choice the person took off the page,
-- kept so that the next dream does not choose it again.
CREATE TABLE "journal_link" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"            character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "journal_day_id"     character varying(32)    REFERENCES "journal_day" ("id") ON DELETE CASCADE,
    "journal_period_id"  character varying(32)    REFERENCES "journal_period" ("id") ON DELETE CASCADE,
    "record_kind"        character varying(32)    NOT NULL,
    "record_id"          character varying(160)   NOT NULL,
    "link_source"        character varying(8)     NOT NULL CHECK ("link_source" IN ('agent', 'person')),
    "link_reason"        text                     NOT NULL DEFAULT '',
    "link_rank"          integer                  NOT NULL DEFAULT 0,
    "is_hidden"          boolean                  NOT NULL DEFAULT false,
    "created_at"         timestamp with time zone NOT NULL,
    CHECK (("journal_day_id" IS NULL) <> ("journal_period_id" IS NULL))
);

CREATE UNIQUE INDEX "journal_link_day_record" ON "journal_link" ("journal_day_id", "record_kind", "record_id") WHERE "journal_day_id" IS NOT NULL;
CREATE UNIQUE INDEX "journal_link_period_record" ON "journal_link" ("journal_period_id", "record_kind", "record_id") WHERE "journal_period_id" IS NOT NULL;

-- What the person decided about their journal: whether the day's weather is
-- fetched (the coordinates of the day's main place, rounded to about 11 km,
-- go to the weather service), and whether and when TeaNode asks "How was
-- today?" in the evening, as a local time of day written HH:MM.
ALTER TABLE "user" ADD COLUMN "is_journal_weather_on" boolean NOT NULL DEFAULT true;
ALTER TABLE "user" ADD COLUMN "is_journal_evening_question_on" boolean NOT NULL DEFAULT true;
ALTER TABLE "user" ADD COLUMN "journal_evening_question_time" character varying(5) NOT NULL DEFAULT '21:00';

-- How many days and periods a dream wrote, and how many narratives it refused
-- because they named a record that was not in what the dream was given.
ALTER TABLE "agent_dream" ADD COLUMN "journal_days_written" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "journal_periods_written" integer NOT NULL DEFAULT 0;
ALTER TABLE "agent_dream" ADD COLUMN "journal_narratives_refused" integer NOT NULL DEFAULT 0;

-- Keeping a journal is something a person does with their own account, so
-- everybody who has the ordinary roles gets it. An operator can take it away
-- from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'journal:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
