-- Who linked the bot, as well as where.
--
-- A linked chat was the whole of the check: anything arriving in that chat was
-- acted on as the person who owns the agent. In a group that is every member,
-- and everybody they invite -- each of them able to read the owner's mail,
-- write, schedule and mint credentials through the bot, and to answer the
-- confirmation cards the agent puts up, because the pending question belonged
-- to the chat rather than to a person.
--
-- Both bots already knew who spoke; nothing read it. This is where it is kept.
--
-- Empty for a bot linked before this column existed. Those are refused rather
-- than trusted, and told to link again, because a check that turns itself off
-- for the rows that predate it is not a check.
ALTER TABLE "agent_channel" ADD COLUMN "linked_sender_id" character varying(200) NOT NULL DEFAULT '';
ALTER TABLE "agent_channel" ADD COLUMN "linked_sender_name" character varying(200) NOT NULL DEFAULT '';
