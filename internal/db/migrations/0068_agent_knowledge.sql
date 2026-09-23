-- Knowledge: what the person has pointed their agent at, and what it
-- found there.
--
-- Three levels. A source is a standing grant of reach -- a directory on
-- their own machine, a chat export, a skill that pages a wiki. A document
-- is one thing from it: a file, a commit, a thread, a page. A chunk is a
-- slice of a document small enough to rank.
--
-- This is where the volume is. A checkout and a chat archive come to
-- hundreds of thousands of chunks, which is why the vectors live in a
-- table of their own with the index on it, and why a document's full text
-- lives in the object store rather than here.

CREATE TABLE "agent_source" (
    "id"            character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"      character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "created_at"    timestamp with time zone NOT NULL,
    "modified_at"   timestamp with time zone NOT NULL,

    "kind"          character varying(40)    NOT NULL,
    "name"          character varying(200)   NOT NULL DEFAULT '',

    -- What to read: the computer and the path, the skill tool and its
    -- arguments, the address to crawl. Shaped by the kind.
    "specification" jsonb                    NOT NULL DEFAULT '{}',

    -- Where in the graph what it finds is filed.
    "root_path"     character varying(500)   NOT NULL DEFAULT '',

    "enabled"       boolean                  NOT NULL DEFAULT true,

    -- How often it is read, as a cron line in the person's zone. Empty
    -- means only when they ask.
    "cron"          character varying(200)   NOT NULL DEFAULT '',

    -- Where the last pass got to: a commit per repository, a timestamp
    -- per channel, a page token. Shaped by the kind, as the specification
    -- is.
    "cursor"        jsonb                    NOT NULL DEFAULT '{}',

    -- Which instance holds the socket to the computer this source is on.
    -- An ingest job for it is claimed only there, because the device is
    -- attached to one instance and a job on another cannot reach it.
    "instance"      character varying(64)    NOT NULL DEFAULT '',

    "last_run_at"   timestamp with time zone,
    "next_run_at"   timestamp with time zone,
    "last_error"    text                     NOT NULL DEFAULT '',

    -- What the last pass did, for the page that shows it: documents,
    -- chunks, and what the secret filter refused.
    "document_count" integer                 NOT NULL DEFAULT 0,
    "chunk_count"    integer                 NOT NULL DEFAULT 0,
    "refused_count"  integer                 NOT NULL DEFAULT 0,

    -- Whether the last pass left more to do. A pass is bounded; it says
    -- here that it wants another, and the queue gives it one.
    "more"           boolean                 NOT NULL DEFAULT false,

    -- Directories the scan flagged as looking private -- evaluations,
    -- recruiting, medical, tax -- and whether the person has let them in.
    -- Nothing under a flagged name crosses the socket until they do.
    "sensitive"      jsonb                   NOT NULL DEFAULT '[]',
    "allowed"        jsonb                   NOT NULL DEFAULT '[]'
);

CREATE INDEX "agent_source_agent" ON "agent_source" ("agent_id");
CREATE INDEX "agent_source_due" ON "agent_source" ("enabled", "next_run_at") WHERE "enabled";

CREATE TABLE "agent_document" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "source_id"   character varying(32)    NOT NULL REFERENCES "agent_source" ("id") ON DELETE CASCADE,

    -- What it is called where it came from: a path, a commit hash, a post
    -- identifier. Unique per source, which is what makes a second pass an
    -- update rather than a copy.
    "external_id" character varying(500)   NOT NULL,

    "kind"        character varying(40)    NOT NULL,
    "title"       character varying(500)   NOT NULL DEFAULT '',
    "url"         text                     NOT NULL DEFAULT '',

    -- When the thing itself happened -- a commit's date, a thread's first
    -- post -- which is what puts it on a period page.
    "happened_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- What was read, so an unchanged file is not read again.
    "hash"        character varying(64)    NOT NULL DEFAULT '',
    "bytes"       bigint                   NOT NULL DEFAULT 0,

    -- Where the whole text is, when it was worth keeping whole. Empty
    -- when the chunks are all there is.
    "storage_key" character varying(500)   NOT NULL DEFAULT '',

    -- The author's address, the repository, the participants, the
    -- language. Shaped by the kind.
    "metadata"    jsonb                    NOT NULL DEFAULT '{}',

    -- A private channel or a direct message. Kept, and marked, so that a
    -- citation can say where it came from.
    "private"     boolean                  NOT NULL DEFAULT false,

    "created_at"  timestamp with time zone NOT NULL
);

CREATE UNIQUE INDEX "agent_document_external" ON "agent_document" ("source_id", "external_id");
CREATE INDEX "agent_document_agent" ON "agent_document" ("agent_id", "kind");
CREATE INDEX "agent_document_happened" ON "agent_document" ("agent_id", "happened_at" DESC NULLS LAST);

CREATE TABLE "agent_chunk" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"    character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "document_id" character varying(32)    NOT NULL REFERENCES "agent_document" ("id") ON DELETE CASCADE,
    "source_id"   character varying(32)    NOT NULL,
    "number"      integer                  NOT NULL,
    "text"        text                     NOT NULL,

    -- Whether the words in it are ones PostgreSQL can segment. It cannot
    -- segment Chinese or Japanese, so a chunk that is mostly those is
    -- found by meaning and not by words, and an answer citing it says so
    -- rather than leaving the reader to wonder why a search missed it.
    "segmented"   boolean                  NOT NULL DEFAULT true,

    "created_at"  timestamp with time zone NOT NULL,
    "search"      tsvector
);

CREATE UNIQUE INDEX "agent_chunk_number" ON "agent_chunk" ("document_id", "number");
CREATE INDEX "agent_chunk_search" ON "agent_chunk" USING gin ("search");
CREATE INDEX "agent_chunk_source" ON "agent_chunk" ("agent_id", "source_id");

CREATE TABLE "agent_chunk_vector" (
    "chunk_id"   character varying(32)    NOT NULL REFERENCES "agent_chunk" ("id") ON DELETE CASCADE,
    "agent_id"   character varying(32)    NOT NULL,
    "source_id"  character varying(32)    NOT NULL,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("chunk_id", "model")
);
ALTER TABLE "agent_chunk_vector" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
CREATE INDEX "agent_chunk_vector_scope" ON "agent_chunk_vector" ("agent_id", "model");

-- Every top-level definition a code file declares. An exact index, asked
-- before any vector: a log line pasted into a chat names a function, and
-- "which file is ComputeShippingQuote in" is a question a cosine
-- answers badly and a string match answers exactly.
CREATE TABLE "agent_symbol" (
    "agent_id"    character varying(32)  NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "document_id" character varying(32)  NOT NULL REFERENCES "agent_document" ("id") ON DELETE CASCADE,
    "symbol"      character varying(200) NOT NULL,
    "lowered"     character varying(200) NOT NULL,
    "kind"        character varying(40)  NOT NULL DEFAULT '',
    "line"        integer                NOT NULL DEFAULT 0,
    PRIMARY KEY ("document_id", "symbol", "line")
);

CREATE INDEX "agent_symbol_lookup" ON "agent_symbol" ("agent_id", "lowered");
