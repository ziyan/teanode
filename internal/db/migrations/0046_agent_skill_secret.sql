-- A person's own value for a secret an installed skill declared as theirs
-- to fill in. An operator's values live in the configuration, one per
-- server; these are per person, because a credential that is somebody's
-- own account should not be shared with everybody who has an agent here.
-- The value is sealed with the server secret before it is written, as the
-- connected servers' credentials are.
CREATE TABLE "agent_skill_secret" (
    "agent_id"    varchar(32) NOT NULL REFERENCES "agent"("id") ON DELETE CASCADE,
    "skill"       varchar(64) NOT NULL,
    "key"         varchar(200) NOT NULL,
    "created_at"  timestamptz NOT NULL,
    "modified_at" timestamptz NOT NULL,
    "value"       text        NOT NULL,
    PRIMARY KEY ("agent_id", "skill", "key")
);
