-- A person's own way to talk to their agent from a chat app: the bot they
-- made in Telegram or Discord, its token sealed with the server secret,
-- and the one chat they linked to it with a code. One row per agent per
-- app. The bot is theirs; the operator only has a switch.
CREATE TABLE "agent_channel" (
    "id"           character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"   timestamp with time zone NOT NULL,
    "modified_at"  timestamp with time zone NOT NULL,
    "agent_id"     character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "kind"         character varying(16)    NOT NULL,
    "token"        text                     NOT NULL DEFAULT '',
    "bot_name"     character varying(200)   NOT NULL DEFAULT '',
    "linked_id"    character varying(200)   NOT NULL DEFAULT '',
    "linked_name"  character varying(200)   NOT NULL DEFAULT '',
    "link_code"    character varying(16)    NOT NULL DEFAULT '',
    "enabled"      boolean                  NOT NULL DEFAULT TRUE,
    "last_error"   text                     NOT NULL DEFAULT '',
    "last_seen_at" timestamp with time zone,
    -- The one instance running this bot, and until when its claim holds:
    -- a chat app takes one connection per bot, so of several instances
    -- sharing this database exactly one may run it, and another takes
    -- over only once the claim has lapsed.
    "claimed_by"    character varying(200)   NOT NULL DEFAULT '',
    "claimed_until" timestamp with time zone,
    UNIQUE ("agent_id", "kind")
);
