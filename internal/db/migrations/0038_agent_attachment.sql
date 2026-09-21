-- Files a person hands their agent in a conversation. The bytes live in
-- the spool's media directory under the row's id; the row says what the
-- file is, which conversation it belongs to, and the text pulled out of
-- it when there was any, so a model reads a text file without the bytes
-- being read twice.
CREATE TABLE "agent_attachment" (
    "id"              character varying(32)    NOT NULL,
    "created_at"      timestamp with time zone NOT NULL,
    "agent_id"        character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "conversation_id" character varying(32)    NOT NULL DEFAULT '',
    "message_id"      character varying(32)    NOT NULL DEFAULT '',
    "name"            character varying(255)   NOT NULL,
    "content_type"    character varying(128)   NOT NULL DEFAULT '',
    "size"            bigint                   NOT NULL DEFAULT 0,
    "text"            text                     NOT NULL DEFAULT '',
    PRIMARY KEY ("id")
);
CREATE INDEX "agent_attachment_conversation" ON "agent_attachment" ("conversation_id");
CREATE INDEX "agent_attachment_agent" ON "agent_attachment" ("agent_id", "created_at");

-- A message may carry the files that came with it and the threads the
-- person pointed at when they wrote it.
ALTER TABLE "agent_message" ADD COLUMN "attachments" jsonb;
ALTER TABLE "agent_message" ADD COLUMN "references" jsonb;
