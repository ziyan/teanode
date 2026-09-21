-- The history of a page: what changed, what it said before, and who did
-- it.
--
-- A page is rewritten by the nightly run, corrected by the person, and
-- added to by the run that files what a conversation taught. Without a
-- record of that, a page that says something surprising is a dead end:
-- there is no way to ask when it started saying it, or whether the person
-- wrote it or the agent worked it out. With one, every sentence on a page
-- can be traced back to the change that put it there.
--
-- One table for the page and its facts and its links together, because
-- they are read together: "what happened to this page" is one question,
-- and three tables would be three answers to join.
CREATE TABLE "agent_revision" (
    "id"         character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"   character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    -- The page this is about. A fact's and a link's changes are filed
    -- under the page they are on, which is where somebody looks for them.
    "node_id"    character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,

    -- Numbered per page, so "people/alice-chen r7" names one change.
    "revision"   integer                  NOT NULL,

    -- What sort of change: the page's own words, or one of its facts, or
    -- one of its links.
    "kind"       character varying(40)    NOT NULL,

    -- Who: the person, the run that files conversations, the nightly run,
    -- the agent's own tool, or a source being read.
    "actor"      character varying(40)    NOT NULL DEFAULT '',

    -- What it was and what it became. Both are kept: a change that only
    -- says the new value cannot be undone or explained.
    "before"     jsonb                    NOT NULL DEFAULT '{}',
    "after"      jsonb                    NOT NULL DEFAULT '{}',

    -- Why, where the writer had a reason worth keeping.
    "reason"     text                     NOT NULL DEFAULT '',

    "created_at" timestamp with time zone NOT NULL
);

CREATE UNIQUE INDEX "agent_revision_number" ON "agent_revision" ("node_id", "revision");
CREATE INDEX "agent_revision_page" ON "agent_revision" ("node_id", "created_at" DESC);
CREATE INDEX "agent_revision_agent" ON "agent_revision" ("agent_id", "created_at" DESC);

-- The next revision number a page will use, for the same reason a fact
-- has one: a revision is cited, so a number must never name two changes.
ALTER TABLE "agent_node" ADD COLUMN "next_revision" integer NOT NULL DEFAULT 1;
