-- An idea is an offer of work the agent can do, kept as a row the person can
-- browse, start, finish or dismiss. It replaces the tips: each tip given
-- becomes an idea already shown, the switch for tips becomes the switch for
-- ideas, and the agent's jobs that spoke first for a tip now speak first for
-- an idea, so the once-a-day rule carries across.
CREATE TABLE "agent_idea" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"                character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    -- The catalog's key for a catalog idea, a generated one for a personal
    -- idea; unique per agent, so a catalog idea is one row however often
    -- the catalog is read.
    "idea_key"                character varying(64)    NOT NULL,
    -- catalog or personal.
    "idea_kind"               character varying(20)    NOT NULL,
    "idea_category"           character varying(20)    NOT NULL,
    "emoji"                   character varying(16)    NOT NULL DEFAULT '',
    "headline"                text                     NOT NULL,
    "body"                    text                     NOT NULL DEFAULT '',
    "opening_request"         text                     NOT NULL DEFAULT '',
    "needed_tool_names"       text[]                   NOT NULL DEFAULT '{}',
    -- What prompted a personal idea: a list of {evidenceKind, evidenceId,
    -- evidenceSummary}.
    "evidence"                jsonb                    NOT NULL DEFAULT '[]',
    "suggestion_reason"       text                     NOT NULL DEFAULT '',
    -- open, started, done, dismissed or expired.
    "idea_status"             character varying(20)    NOT NULL DEFAULT 'open',
    "rank_score"              double precision         NOT NULL DEFAULT 0,
    "started_conversation_id" character varying(32)    NOT NULL DEFAULT '',
    "created_at"              timestamp with time zone NOT NULL,
    "modified_at"             timestamp with time zone NOT NULL,
    "shown_at"                timestamp with time zone,
    "started_at"              timestamp with time zone,
    "closed_at"               timestamp with time zone,
    "expires_at"              timestamp with time zone
);
CREATE UNIQUE INDEX "agent_idea_key" ON "agent_idea" ("agent_id", "idea_key");
CREATE INDEX "agent_idea_status" ON "agent_idea" ("agent_id", "idea_status");

-- Each tip given is its catalog idea, already shown. The text is filled in
-- from the catalog the next time the ideas are refreshed.
INSERT INTO "agent_idea" ("id", "agent_id", "idea_key", "idea_kind", "idea_category", "headline", "started_conversation_id", "created_at", "modified_at", "shown_at")
SELECT "id", "agent_id", "tip_key", 'catalog', 'assistant', "tip_key", '', "given_at", "given_at", "given_at" FROM "agent_tip";

DROP TABLE "agent_tip";

ALTER TABLE "agent" RENAME COLUMN "is_tips_enabled" TO "is_ideas_enabled";

UPDATE "agent_job" SET "subject_id" = 'idea' WHERE "kind" = 'speak_first' AND "subject_id" = 'tip';
UPDATE "agent_job" SET "subject_id" = 'idea:asked' WHERE "kind" = 'speak_first' AND "subject_id" = 'tip:asked';
