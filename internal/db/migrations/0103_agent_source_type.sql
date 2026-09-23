-- A source type: the file that says how to read one kind of knowledge
-- source, installed from the source types registry or added by an operator
-- from a file of their own ("local", unsigned). Kept whole, as a skill is,
-- so a server that can no longer reach the registry still has it, and
-- offered to everyone, which is why there is no agent here. A source names
-- its type in its specification.
CREATE TABLE "agent_source_type" (
    "name"        varchar(64)  NOT NULL,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "version"     varchar(32)  NOT NULL DEFAULT '',
    "publisher"   varchar(200) NOT NULL DEFAULT '',
    "url"         text         NOT NULL DEFAULT '',
    "sha256"      varchar(64)  NOT NULL DEFAULT '',
    "description" text         NOT NULL DEFAULT '',
    "content"     text         NOT NULL,
    "is_local"    boolean      NOT NULL DEFAULT false,
    PRIMARY KEY ("name")
);
