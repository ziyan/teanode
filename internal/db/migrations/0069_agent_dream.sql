-- What the nightly run did, one row a night.
--
-- Written so the person can read it over coffee, and so the run itself
-- can see how far behind it is: a backlog is reported, never silently
-- skipped. A cap here is pacing, not truncation.
CREATE TABLE "agent_dream" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "started_at"  timestamp with time zone NOT NULL,
    "finished_at" timestamp with time zone,

    -- What each phase did.
    "digested"     integer NOT NULL DEFAULT 0,
    "filed"        integer NOT NULL DEFAULT 0,
    "merged"       integer NOT NULL DEFAULT 0,
    "rewritten"    integer NOT NULL DEFAULT 0,
    "moved"        integer NOT NULL DEFAULT 0,
    "dormant"      integer NOT NULL DEFAULT 0,
    "embedded"     integer NOT NULL DEFAULT 0,

    -- How much is still waiting, and at this pace how many nights that
    -- is. The number a person needs to decide whether to raise the share
    -- or let it coarsen.
    "backlog"      integer NOT NULL DEFAULT 0,

    -- Whether this night worked at full resolution or coarsened because
    -- the backlog was larger than anybody would wait for. A month done
    -- coarse is marked so a later night, or the person, can redo it fine.
    "coarse"       boolean NOT NULL DEFAULT false,

    -- What it wants the person to decide: a page to move, a split, an
    -- alias. Applied by a press, never by the run.
    "proposals"    jsonb   NOT NULL DEFAULT '[]',

    "tokens"       bigint  NOT NULL DEFAULT 0,
    "notes"        text    NOT NULL DEFAULT '',
    "last_error"   text    NOT NULL DEFAULT ''
);

CREATE INDEX "agent_dream_agent" ON "agent_dream" ("agent_id", "started_at" DESC);

-- When the person's night is, so a dream runs while they are asleep and
-- not while they are working. Empty means the default window.
ALTER TABLE "agent" ADD COLUMN "dream_from" character varying(5) NOT NULL DEFAULT '';
ALTER TABLE "agent" ADD COLUMN "dream_until" character varying(5) NOT NULL DEFAULT '';
ALTER TABLE "agent" ADD COLUMN "dreamed_at" timestamp with time zone;

-- When a page's summary was last written from its facts, so the nightly
-- run can tell a page that has moved on from one that has not.
ALTER TABLE "agent_node" ADD COLUMN "consolidated_at" timestamp with time zone;
