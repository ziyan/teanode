-- A reply the agent wrote on the person's behalf, and what became of it.
--
-- One row per answered message: held while it waits in Drafts for the hold
-- to pass, then sent, or cancelled by the person, or refused by the ladder
-- with the reason recorded. The text is kept here as written, so the send
-- can tell whether the draft was changed by hand — an edit makes the reply
-- the person's, and it is no longer sent for them.
CREATE TABLE "agent_reply" (
    "id"            character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"    timestamp with time zone NOT NULL,
    "modified_at"   timestamp with time zone NOT NULL,
    "agent_id"      character varying(32)    NOT NULL,
    "mailbox_id"    character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "mail_id"       character varying(32)    NOT NULL,
    "thread_id"     character varying(32)    NOT NULL DEFAULT '',
    "draft_item_id" character varying(32)    NOT NULL DEFAULT '',
    "run_id"        character varying(32)    NOT NULL DEFAULT '',
    "status"        character varying(16)    NOT NULL,
    "reason"        text                     NOT NULL DEFAULT '',
    "subject"       text                     NOT NULL DEFAULT '',
    "from_address"  character varying(320)   NOT NULL DEFAULT '',
    "to_address"    character varying(320)   NOT NULL DEFAULT '',
    "text"          text                     NOT NULL DEFAULT '',
    "send_after"    timestamp with time zone,
    "sent_mail_id"  character varying(32)    NOT NULL DEFAULT '',
    "sent_at"       timestamp with time zone
);
CREATE INDEX "agent_reply_mailbox_status" ON "agent_reply" ("mailbox_id", "status", "created_at");
CREATE INDEX "agent_reply_agent_created" ON "agent_reply" ("agent_id", "created_at");
CREATE INDEX "agent_reply_mail" ON "agent_reply" ("mail_id");
