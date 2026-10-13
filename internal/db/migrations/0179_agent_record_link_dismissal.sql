-- What the person took off a page by hand: a link the automatic passes would
-- otherwise make again on the next dream.
--
-- The passes are idempotent by the unique key of a link, so a link the person
-- removed is a link the next pass finds missing and writes back. This is the
-- record of the removal, with the same key as the link, and the passes skip
-- what it names. Making the link by hand again clears it.
CREATE TABLE "agent_record_link_dismissal" (
    "agent_id"         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "node_id"          character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,
    "record_kind"      character varying(40)    NOT NULL,
    "record_id"        character varying(128)   NOT NULL,
    "record_relation"  character varying(40)    NOT NULL,
    "passage_start_ms" bigint                   NOT NULL DEFAULT 0,
    "passage_end_ms"   bigint                   NOT NULL DEFAULT 0,
    "created_at"       timestamp with time zone NOT NULL,
    PRIMARY KEY ("agent_id", "node_id", "record_kind", "record_id", "record_relation", "passage_start_ms", "passage_end_ms")
);
