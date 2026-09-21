-- What a message carries that belongs somewhere else.
--
-- An invitation that arrives as a calendar part is read by the scheduling
-- path and becomes an event. Most appointments do not arrive that way: they
-- arrive as "shall we say Thursday at four", and most new telephone numbers
-- arrive in a signature. Nothing read those.
--
-- A proposal is what the agent found in the words: an event or a person, with
-- the line it came from, offered above the message in the reader. It is never
-- written anywhere on its own -- putting an appointment in somebody's diary
-- because a stranger's message mentioned a day is how a calendar stops being
-- trusted -- so the row holds what was found and what the person did about
-- it, and nothing else happens until they press something.
ALTER TABLE "mail_insight" ADD COLUMN "proposals" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "mail_insight" ADD COLUMN "extract_asked" boolean NOT NULL DEFAULT false;
