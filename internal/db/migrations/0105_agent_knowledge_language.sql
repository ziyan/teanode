-- What the agent's notes are written in: its pages, their openings and
-- their facts. Kept apart from the language it writes to the person in,
-- because that one follows the dashboard and the mail being answered,
-- and notes that change language with it end up in two languages, where
-- the same fact said twice no longer reads as the same. Empty follows the
-- person's language.
ALTER TABLE "agent" ADD COLUMN "knowledge_language" varchar(35) NOT NULL DEFAULT '';
