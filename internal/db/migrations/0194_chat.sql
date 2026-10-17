-- A chat is a conversation with people in it: direct between two people, or a
-- group. A circle has one circle chat whose members follow the circle's. What
-- the people and their agents say is stored once, in chat_message; each member
-- has their own agent_conversation of the kind 'chat' that points at the chat
-- (chat_id below) and later holds the rows their own agent wrote there, which
-- only they read.
--
-- docs/decisions/20261016-a-chat-is-a-conversation-with-people-in-it.md says
-- why, and docs/subsystems/conversations.md how it is read.
CREATE TABLE "chat" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "created_at"         timestamp with time zone NOT NULL,
    "modified_at"        timestamp with time zone NOT NULL,
    "chat_kind"          character varying(16)  NOT NULL
        CHECK ("chat_kind" IN ('direct', 'group')),

    -- What a group is called; NULL for a direct chat, and for a group nobody
    -- named, which the screens call by its members.
    "title"              character varying(100),

    -- The circle this is the circle chat of. Deleting the circle deletes it.
    "circle_id"          character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "last_message_at"    timestamp with time zone,

    -- The two user ids of a direct chat, sorted and joined by ':', so that
    -- opening the chat with somebody twice finds the same chat.
    "direct_key"         character varying(65) UNIQUE,
    CHECK (("chat_kind" = 'direct') = ("direct_key" IS NOT NULL))
);

-- A circle has one circle chat.
CREATE UNIQUE INDEX "chat_circle" ON "chat" ("circle_id") WHERE "circle_id" IS NOT NULL;

-- A person in a chat. A member who left or was taken out keeps their row with
-- removed_at set, and read the chat up to then. When the account is deleted
-- user_id becomes NULL and member_name keeps what the person was called.
CREATE TABLE "chat_member" (
    "chat_id"                    character varying(32)  NOT NULL REFERENCES "chat" ("id") ON DELETE CASCADE,
    "user_id"                    character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "member_name"                character varying(100) NOT NULL,
    "member_role"                character varying(16)  NOT NULL
        CHECK ("member_role" IN ('admin', 'member')),
    "joined_at"                  timestamp with time zone NOT NULL,
    "removed_at"                 timestamp with time zone,
    "read_through_message_id"    character varying(32),
    "muted_until"                timestamp with time zone,

    -- Whether the member's agent remembers this chat (reads it as a source of
    -- what it knows), and whether the other members may ask that agent in the
    -- chat. Both are the member's own to switch, and both start off.
    "is_agent_reading"           boolean NOT NULL DEFAULT false,
    "is_agent_askable_by_others" boolean NOT NULL DEFAULT false,
    UNIQUE ("chat_id", "user_id")
);

CREATE INDEX "chat_member_user" ON "chat_member" ("user_id") WHERE "user_id" IS NOT NULL;

-- What was said. The id is a ULID, so ids sort by time. The author's name is
-- kept so that it still reads after the account is gone, and an agent's
-- message carries the agent as well as its owner.
CREATE TABLE "chat_message" (
    "id"                 character varying(32)  NOT NULL PRIMARY KEY,
    "chat_id"            character varying(32)  NOT NULL REFERENCES "chat" ("id") ON DELETE CASCADE,
    "created_at"         timestamp with time zone NOT NULL,
    "author_user_id"     character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "author_member_name" character varying(100) NOT NULL,
    -- Whose words they are, kept on its own: author_agent_id is cleared when
    -- the agent is deleted, and an agent's message must not then read as its
    -- owner's.
    "author_kind"        character varying(16)  NOT NULL DEFAULT 'person'
        CHECK ("author_kind" IN ('person', 'agent')),
    "author_agent_id"    character varying(32) REFERENCES "agent" ("id") ON DELETE SET NULL,
    -- Whose agent spoke: most agents are still called "Agent", so the name
    -- alone does not say whose it was. The owner's name as the chat knew
    -- them, kept as author_member_name is, so it still reads after the
    -- owner's account is gone. Empty on a person's message.
    "author_owner_name"  character varying(100) NOT NULL DEFAULT '',
    "agent_run_id"       character varying(32),
    "body"               text NOT NULL,
    "attachments"        jsonb NOT NULL DEFAULT '[]',
    "reply_to_message_id" character varying(32),
    "edited_at"          timestamp with time zone,
    "deleted_at"         timestamp with time zone
);

CREATE INDEX "chat_message_chat" ON "chat_message" ("chat_id", "id");

-- One reaction per reactor per message. reactor_key is 'user:<id>' or
-- 'agent:<id>', and reactor_name the name they had, which outlives the
-- account.
CREATE TABLE "chat_reaction" (
    "message_id"   character varying(32) NOT NULL REFERENCES "chat_message" ("id") ON DELETE CASCADE,
    "reactor_key"  character varying(40) NOT NULL,
    "reactor_name" character varying(100) NOT NULL DEFAULT '',
    "emoji"        character varying(32) NOT NULL CHECK (octet_length("emoji") BETWEEN 1 AND 32),
    "created_at"   timestamp with time zone NOT NULL,
    PRIMARY KEY ("message_id", "reactor_key")
);

-- A member's own conversation for the chat. The unique key makes one per
-- agent per chat; it does not bind the many rows that are not chats, whose
-- chat_id is NULL.
ALTER TABLE "agent_conversation" ADD COLUMN "chat_id" character varying(32) REFERENCES "chat" ("id") ON DELETE CASCADE;
ALTER TABLE "agent_conversation" ADD CONSTRAINT "agent_conversation_chat" UNIQUE ("agent_id", "chat_id");
CREATE INDEX "agent_conversation_chat_id" ON "agent_conversation" ("chat_id") WHERE "chat_id" IS NOT NULL;
