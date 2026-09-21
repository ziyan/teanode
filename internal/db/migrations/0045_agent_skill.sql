-- A skill installed from a registry: the file as it was fetched, with what
-- the registry said about it. The text is kept rather than only its address
-- so that a server which can no longer reach the registry still has
-- everything it needs, and so that an operator can read exactly what is
-- installed. Installed by an operator and offered to everyone, which is why
-- there is no agent here.
CREATE TABLE "agent_skill" (
    "name"        varchar(64)  NOT NULL,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "version"     varchar(32)  NOT NULL DEFAULT '',
    "publisher"   varchar(200) NOT NULL DEFAULT '',
    "url"         text         NOT NULL DEFAULT '',
    "sha256"      varchar(64)  NOT NULL DEFAULT '',
    "description" text         NOT NULL DEFAULT '',
    "content"     text         NOT NULL,
    "enabled"     boolean      NOT NULL DEFAULT true,
    PRIMARY KEY ("name")
);
