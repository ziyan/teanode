-- Links from a memory page to one of the person's own records: a contact
-- card, a mail message, a calendar event or reminder, an agent
-- conversation message, a finance transaction or receipt, a passage of a
-- recording.
--
-- Why a table of links and not a page per record: a page is the agent's
-- own writing, and a record is the person's data, owned and permissioned
-- elsewhere. A link names the record by kind and identifier, so there is
-- one copy of it and every read can check the grant at the moment of
-- reading. There is deliberately no foreign key to the record: the records
-- live in a dozen tables, and one that is deleted leaves a link that reads
-- as unavailable until somebody removes it.
CREATE TABLE "agent_record_link" (
    "id"               character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "node_id"          character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,

    "record_kind"      character varying(40)    NOT NULL,
    "record_id"        character varying(128)   NOT NULL,

    -- Where inside the record, for the kinds that need it: the transcript
    -- run of a recording passage, the mailbox of a mail message.
    "record_detail"    jsonb                    NOT NULL DEFAULT '{}',

    -- A passage of a recording is a range in milliseconds. In columns
    -- rather than in the detail because a recording has many passages, so
    -- the range is part of what makes a link unique, and because asking
    -- which pages link to a moment of a recording is a range overlap.
    -- Zero for every other kind.
    "passage_start_ms" bigint                   NOT NULL DEFAULT 0,
    "passage_end_ms"   bigint                   NOT NULL DEFAULT 0,

    "record_relation"  character varying(40)    NOT NULL,

    -- Stated, or proposed when the agent only guessed, as for a link
    -- between two pages.
    "link_status"      character varying(20)    NOT NULL DEFAULT 'stated',

    "evidence"         jsonb                    NOT NULL DEFAULT '[]',
    "note"             text                     NOT NULL DEFAULT '',
    "happened_at"      timestamp with time zone,
    "created_at"       timestamp with time zone NOT NULL,
    "modified_at"      timestamp with time zone NOT NULL
);

CREATE UNIQUE INDEX "agent_record_link_unique" ON "agent_record_link"
    ("agent_id", "node_id", "record_kind", "record_id", "record_relation", "passage_start_ms", "passage_end_ms");
CREATE INDEX "agent_record_link_node" ON "agent_record_link" ("node_id");
CREATE INDEX "agent_record_link_record" ON "agent_record_link" ("agent_id", "record_kind", "record_id");
