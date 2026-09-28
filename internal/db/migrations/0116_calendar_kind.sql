-- A calendar holds events or reminders. Every calendar so far holds events;
-- a person's reminders list is a calendar of the other kind, one each, made
-- the first time anything asks for it, which is what a phone's Reminders
-- app needs to find somewhere to put a reminder.
ALTER TABLE "calendar" ADD COLUMN "calendar_kind" character varying(20) NOT NULL DEFAULT 'events';
CREATE UNIQUE INDEX "calendar_one_reminders_list" ON "calendar" ("user_id") WHERE "calendar_kind" = 'reminders';
