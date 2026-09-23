-- A value a source's type declares as a secret -- a token for a service's
-- web API, say -- filled in by the person whose source it is. Kept for each
-- source rather than for each type, because two sources of one type are
-- usually two accounts. Sealed with the server secret before it is written,
-- as a skill's secrets are, and gone with its source.
CREATE TABLE "agent_source_secret" (
    "source_id"   varchar(32)  NOT NULL REFERENCES "agent_source"("id") ON DELETE CASCADE,
    "key"         varchar(200) NOT NULL,
    "created_at"  timestamptz  NOT NULL,
    "modified_at" timestamptz  NOT NULL,
    "value"       text         NOT NULL,
    PRIMARY KEY ("source_id", "key")
);
