-- Reactions on the messages of a conversation with the agent: an emoji put
-- on a message, as in a chat app. One row per reactor per message, so the
-- person has one reaction on a message and the agent one; reacting again
-- replaces the row, and taking the reaction away deletes it.
--
-- The message and its conversation are foreign keys that cascade, so a
-- reaction goes with the message it is on, whichever way that goes: a
-- conversation deleted, a run transcript swept, an agent forgotten.

CREATE TABLE "agent_message_reaction" (
    "message_id" character varying(32) NOT NULL REFERENCES "agent_message" ("id") ON DELETE CASCADE,
    "conversation_id" character varying(32) NOT NULL REFERENCES "agent_conversation" ("id") ON DELETE CASCADE,

    -- Whose conversation it is, so that a reaction is never looked up
    -- without the agent it belongs to.
    "agent_id" character varying(32) NOT NULL,

    -- 'person' or 'agent'.
    "reactor_kind" character varying(16) NOT NULL CHECK ("reactor_kind" IN ('person', 'agent')),

    -- One or two emoji, at most 32 bytes; the server checks what they are.
    "emoji" character varying(32) NOT NULL CHECK (octet_length("emoji") BETWEEN 1 AND 32),

    "reacted_at" timestamp with time zone NOT NULL,

    PRIMARY KEY ("message_id", "reactor_kind")
);

-- A page of a conversation reads its reactions by the conversation.
CREATE INDEX "agent_message_reaction_conversation" ON "agent_message_reaction" ("conversation_id");
