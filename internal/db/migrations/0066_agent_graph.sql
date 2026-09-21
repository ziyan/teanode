-- The graph: what the agent knows about the person, as pages with facts on
-- them rather than one flat list of memories.
--
-- Three tables. A node is a page about one thing, addressed by a path --
-- "people/alice-chen" -- which is also the hierarchy: a path is the chain
-- of parents, and the parent link is kept beside it so a query can walk
-- what a string can only spell. A fact is one sentence on a node, with the
-- evidence it came from and when it was true, which is a different
-- question from when the agent learned it. An edge is what a directory
-- cannot hold: this person works on that project.
--
-- Why a fact is a row and not a field on the node: it carries its own
-- evidence, its own dates, its own vector and its own confidence, and it
-- can be superseded or go dormant on its own. The node's summary is
-- written *from* its facts, so the facts are the record and the summary is
-- the digest.

CREATE TABLE "agent_node" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    -- The address the model uses. Lowercase slugs joined by "/", at most
    -- eight segments. Unique per agent, which is what makes a path an
    -- address rather than a description.
    "path"        character varying(500)   NOT NULL,
    "parent_id"   character varying(32)    REFERENCES "agent_node" ("id") ON DELETE SET NULL,

    "kind"        character varying(40)    NOT NULL,
    "name"        character varying(200)   NOT NULL DEFAULT '',
    "aliases"     jsonb                    NOT NULL DEFAULT '[]',
    "summary"     text                     NOT NULL DEFAULT '',

    -- A person node names the contact that is them. The address book is
    -- the only list of people this server keeps; this page is the agent's
    -- knowledge about one of them.
    "contact_id"  character varying(32),

    "pinned"      boolean                  NOT NULL DEFAULT false,

    -- What orders the index the prompt carries. Recomputed nightly and by
    -- nothing else, so the cacheable part of a prompt does not change from
    -- turn to turn.
    "importance"  real                     NOT NULL DEFAULT 0,

    -- Out of the index, still searchable. Nothing here is ever deleted by
    -- the agent itself.
    "dormant"     boolean                  NOT NULL DEFAULT false,

    "used_at"     timestamp with time zone,
    "created_at"  timestamp with time zone NOT NULL,
    "modified_at" timestamp with time zone NOT NULL,

    -- The next number a fact on this page will take. A high-water mark
    -- rather than a count, because a number is cited -- "people/alice#2"
    -- travels into a conversation and into another fact's evidence -- and
    -- handing a forgotten number to a new sentence would silently change
    -- what an old citation points at.
    "next_fact_number" integer              NOT NULL DEFAULT 1,

    "search"      tsvector
);

CREATE UNIQUE INDEX "agent_node_path" ON "agent_node" ("agent_id", "path");
CREATE INDEX "agent_node_parent" ON "agent_node" ("parent_id");
CREATE INDEX "agent_node_index" ON "agent_node" ("agent_id", "dormant", "pinned" DESC, "importance" DESC);
CREATE INDEX "agent_node_search" ON "agent_node" USING gin ("search");
CREATE INDEX "agent_node_contact" ON "agent_node" ("contact_id") WHERE "contact_id" IS NOT NULL;

CREATE TABLE "agent_fact" (
    "id"            character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"      character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "node_id"       character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,

    -- Stable within the node, so a person and a model can both say
    -- "people/alice-chen#3" and mean the same sentence.
    "number"        integer                  NOT NULL,

    "kind"          character varying(40)    NOT NULL,
    "text"          text                     NOT NULL,

    -- When it was true, which is not when it was learned. This is the time
    -- axis: a fact about June 2023 written today sorts under June 2023.
    "happened_at"   timestamp with time zone,

    -- Below one for anything assembled rather than said. An answer put
    -- together from a chat thread and a commit is an inference, and an
    -- inference filed as a fact would make a guess permanent.
    "confidence"    real                     NOT NULL DEFAULT 1,
    "inferred"      boolean                  NOT NULL DEFAULT false,

    -- Where it came from: a message, a mail item, a chunk, a commit, the
    -- person's own hand. Each entry keeps the line it was read in.
    "evidence"      jsonb                    NOT NULL DEFAULT '[]',

    -- Which unattended runs read it, as memories were addressed before.
    "audiences"     jsonb                    NOT NULL DEFAULT '[]',

    "superseded_by" character varying(32),
    "dormant"       boolean                  NOT NULL DEFAULT false,

    "used_at"       timestamp with time zone,
    "created_at"    timestamp with time zone NOT NULL,
    "modified_at"   timestamp with time zone NOT NULL,

    "search"        tsvector
);

CREATE UNIQUE INDEX "agent_fact_number" ON "agent_fact" ("node_id", "number");
CREATE INDEX "agent_fact_node" ON "agent_fact" ("node_id", "dormant", "used_at" DESC NULLS LAST);
CREATE INDEX "agent_fact_agent" ON "agent_fact" ("agent_id", "dormant");
CREATE INDEX "agent_fact_happened" ON "agent_fact" ("agent_id", "happened_at" DESC NULLS LAST);
CREATE INDEX "agent_fact_search" ON "agent_fact" USING gin ("search");

CREATE TABLE "agent_edge" (
    "agent_id"   character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "from_id"    character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,
    "to_id"      character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,
    "relation"   character varying(40)    NOT NULL,
    "weight"     real                     NOT NULL DEFAULT 1,
    "evidence"   jsonb                    NOT NULL DEFAULT '[]',
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("from_id", "to_id", "relation")
);

CREATE INDEX "agent_edge_to" ON "agent_edge" ("to_id");
CREATE INDEX "agent_edge_agent" ON "agent_edge" ("agent_id");

-- Vectors live apart from the rows they describe. An array of floats does
-- not compress and is larger than the text beside it, so keeping it in the
-- same row would push every read through the out-of-line store. Apart, the
-- page stays small and hot and the index has a table of its own.
--
-- The model column carries the width where one was asked for
-- ("openai:text-embedding-3-small@512"), because two widths of one model
-- are two spaces and must never be compared.
CREATE TABLE "agent_node_vector" (
    "node_id"    character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,
    "agent_id"   character varying(32)    NOT NULL,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("node_id", "model")
);
ALTER TABLE "agent_node_vector" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
CREATE INDEX "agent_node_vector_scope" ON "agent_node_vector" ("agent_id", "model");

CREATE TABLE "agent_fact_vector" (
    "fact_id"    character varying(32)    NOT NULL REFERENCES "agent_fact" ("id") ON DELETE CASCADE,
    "agent_id"   character varying(32)    NOT NULL,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("fact_id", "model")
);
ALTER TABLE "agent_fact_vector" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
CREATE INDEX "agent_fact_vector_scope" ON "agent_fact_vector" ("agent_id", "model");

-- The contact that is the person themselves. Kept on the account rather
-- than on the agent because it is true of them whether or not they have an
-- agent: the composer and a CardDAV client can read it too. It carries
-- their own addresses, which is what tells a commit or a sent message from
-- somebody else's.
ALTER TABLE "user" ADD COLUMN "contact_id" character varying(32);
