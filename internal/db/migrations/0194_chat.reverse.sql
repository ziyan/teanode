-- The chats go, with their messages and reactions, and the members' chat
-- conversations with them (their foreign key cascades).
DELETE FROM "agent_conversation" WHERE "kind" = 'chat';
-- And the sources that read a chat for an agent: code from before chats has no
-- such kind of source, and would fail on each one when its time came.
DELETE FROM "agent_source" WHERE "kind" = 'chat';
DROP INDEX "agent_conversation_chat_id";
ALTER TABLE "agent_conversation" DROP CONSTRAINT "agent_conversation_chat";
ALTER TABLE "agent_conversation" DROP COLUMN "chat_id";
DROP TABLE "chat_reaction";
DROP TABLE "chat_message";
DROP TABLE "chat_member";
DROP TABLE "chat";
