-- The chats go, with their messages and reactions, and the members' chat
-- conversations with them (their foreign key cascades).
DELETE FROM "agent_conversation" WHERE "kind" = 'chat';
DROP INDEX "agent_conversation_chat_id";
ALTER TABLE "agent_conversation" DROP CONSTRAINT "agent_conversation_chat";
ALTER TABLE "agent_conversation" DROP COLUMN "chat_id";
DROP TABLE "chat_reaction";
DROP TABLE "chat_message";
DROP TABLE "chat_member";
DROP TABLE "chat";
