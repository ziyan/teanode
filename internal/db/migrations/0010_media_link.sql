-- One address per picture per message.
--
-- A template names a picture once; every message sent from it gets its own
-- address for that picture, and this table is what an address resolves to. It
-- is what makes an open detectable: a fetch of one of these can only have come
-- from the one message it was put in.
--
-- The token is not an identifier like the others here. Every other key in this
-- schema is a ULID, which carries a timestamp and sorts, so one can be guessed
-- from another; these are reachable by anybody on the internet with no session,
-- and guessing one would let a stranger fetch a picture meant for somebody
-- else's message and, worse, mark it opened. Sixteen random bytes cannot be
-- walked.
CREATE TABLE "media_link" (
    "token" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,
    "media_id" character varying(32) NOT NULL DEFAULT '',

    -- The message, by the identifier it is given as it is composed, which is
    -- the same one the stored mail records as its envelope. One address per
    -- message rather than per recipient: the rewrite happens once, while the
    -- body is being built, and the copies handed to each recipient are the
    -- same bytes. So a message sent to three people that comes back opened
    -- says the message was opened, not which of them opened it.
    "envelope_id" character varying(32) NOT NULL DEFAULT '',

    -- Filled in when somebody fetches it. First and last are both kept: the
    -- first is when the message was opened, and the last is how long it went
    -- on being looked at.
    "opened_at" timestamp with time zone,
    "last_opened_at" timestamp with time zone,
    "open_count" bigint NOT NULL DEFAULT 0,
    "ip" character varying(64) NOT NULL DEFAULT '',
    "user_agent" character varying(512) NOT NULL DEFAULT '',

    PRIMARY KEY ("token")
);

-- Answering "was this message opened" without reading the table.
CREATE INDEX "media_link_envelope_id" ON "media_link" ("envelope_id");
