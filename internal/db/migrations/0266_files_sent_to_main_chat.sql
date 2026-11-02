-- A file can be sent to the person's main chat: by a program over MCP, by
-- the agent from another conversation, or by a run nobody is present for.
--
-- "caption" is the line sent with it and "sent_by" who sent it, both shown
-- in the chat. "main_chat_queued_at" is when it was sent while it waits to
-- be posted: a turn running in the main chat holds it until that turn ends,
-- and it is cleared when the file is posted.
ALTER TABLE "agent_attachment"
    ADD COLUMN "caption" text NOT NULL DEFAULT '',
    ADD COLUMN "sent_by" varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN "main_chat_queued_at" timestamptz;

-- The files waiting to be posted, oldest first.
CREATE INDEX "agent_attachment_main_chat_waiting" ON "agent_attachment" ("main_chat_queued_at")
    WHERE "main_chat_queued_at" IS NOT NULL;
