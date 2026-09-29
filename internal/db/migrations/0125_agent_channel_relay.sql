-- How far into the main conversation a chat app's bot has looked for the
-- agent's own turns to send on: alerts, speaking first, schedules, goal
-- check-ins, background wakes, which no chat message asked for. The last
-- message looked at; moved forward before anything is sent, by the instance
-- holding the bot, so that each is sent once.
ALTER TABLE "agent_channel" ADD COLUMN "relayed_through" character varying(32) NOT NULL DEFAULT '';
