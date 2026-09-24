-- A run of the memory check: every question the person stands behind,
-- answered from memory, from the sources and from both, and graded. The
-- scores over time say whether memory is getting better.
CREATE TABLE "agent_evaluation_run" (
    "id"                  character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"            character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "started_at"          timestamp with time zone NOT NULL,
    "finished_at"         timestamp with time zone,
    "question_count"      integer                  NOT NULL DEFAULT 0,
    "cost"                double precision         NOT NULL DEFAULT 0,

    -- Per source: the score with and without the questions whose answers
    -- were filed after they were written, and the count of each verdict.
    "source_scores"       jsonb                    NOT NULL DEFAULT '[]'
);
CREATE INDEX "agent_evaluation_run_agent" ON "agent_evaluation_run" ("agent_id", "started_at");

-- One question answered from one source in a run.
CREATE TABLE "agent_evaluation_answer" (
    "id"             character varying(32)    NOT NULL PRIMARY KEY,
    "run_id"         character varying(32)    NOT NULL REFERENCES "agent_evaluation_run" ("id") ON DELETE CASCADE,
    "question_id"    character varying(32)    NOT NULL REFERENCES "agent_evaluation_question" ("id") ON DELETE CASCADE,
    "created_at"     timestamp with time zone NOT NULL,
    "answer_from"    character varying(20)    NOT NULL,
    "answer_verdict" character varying(20)    NOT NULL,
    "verdict_reason" text                     NOT NULL DEFAULT '',
    "answer_text"    text                     NOT NULL DEFAULT '',
    "cost"           double precision         NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX "agent_evaluation_answer_run" ON "agent_evaluation_answer" ("run_id", "question_id", "answer_from");
