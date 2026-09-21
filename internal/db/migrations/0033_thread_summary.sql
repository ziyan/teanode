-- A conversation's summary: one per conversation per mailbox, written by the
-- agent and rewritten as the conversation grows. through_mail_id is the
-- newest message it covers, which is how a stale summary is told from a
-- fresh one without reading either.
CREATE TABLE "thread_summary" (
    "thread_id"       character varying(32)    NOT NULL,
    "mailbox_id"      character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "agent_id"        character varying(32)    NOT NULL,
    "summary"         text                     NOT NULL DEFAULT '',
    "through_mail_id" character varying(32)    NOT NULL DEFAULT '',
    "message_count"   integer                  NOT NULL DEFAULT 0,
    "model"           character varying(200)   NOT NULL DEFAULT '',
    "run_id"          character varying(32)    NOT NULL DEFAULT '',
    "created_at"      timestamp with time zone NOT NULL,
    PRIMARY KEY ("thread_id", "mailbox_id")
);
