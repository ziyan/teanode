-- The person's memory check: questions the agent asked them about what it
-- remembers, or that they supplied, with the answers they gave. Private to
-- the person, like the graph it tests, and gone with the agent.
CREATE TABLE "agent_evaluation_question" (
    "id"                    character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"              character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "created_at"            timestamp with time zone NOT NULL,
    "modified_at"           timestamp with time zone NOT NULL,

    -- direct, paraphrase, changed, multihop or abstain: the kinds a
    -- question file already uses.
    "question_kind"         character varying(20)    NOT NULL DEFAULT 'direct',
    "question_text"         text                     NOT NULL,
    "expected_answer"       text                     NOT NULL DEFAULT '',
    -- What was true once, for a question about something that changed.
    "outdated_answer"       text                     NOT NULL DEFAULT '',

    -- asked, confirmed, corrected, dropped or unsure.
    "question_state"        character varying(20)    NOT NULL DEFAULT 'asked',

    -- The facts a drafted question came from; empty for one the person
    -- supplied.
    "source_fact_ids"       character varying(32)[]  NOT NULL DEFAULT '{}',

    -- Whether the answer was filed into memory after the question was
    -- written, which makes the question easy and is reported apart.
    "is_answer_filed_after" boolean                  NOT NULL DEFAULT false,

    "conversation_id"       character varying(32)    NOT NULL DEFAULT '',
    "answered_at"           timestamp with time zone
);
CREATE INDEX "agent_evaluation_question_agent" ON "agent_evaluation_question" ("agent_id", "created_at");
