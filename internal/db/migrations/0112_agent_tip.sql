-- The tips the agent has given, one row each, so that none is given twice.
CREATE TABLE "agent_tip" (
    "id"              character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"        character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "tip_key"         character varying(64)    NOT NULL,
    "given_at"        timestamp with time zone NOT NULL,
    "conversation_id" character varying(32)    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX "agent_tip_key" ON "agent_tip" ("agent_id", "tip_key");
