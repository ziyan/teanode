-- What several instances of the server sharing one database have to agree on,
-- which each used to keep in its own memory. Nine tables.
--
-- throttle is something that happens at most once in a while, whichever
-- instance asks: a notification not told again for ten minutes, a sweep that
-- runs once an hour. Taking a turn is one statement that only succeeds when
-- the last one is old enough.
--
-- rate_limit_bucket is a token bucket counted by every instance together, for
-- the limits that guard a password: signing in, a mail program's password,
-- unlocking the password vault. Each instance counting on its own gave a
-- guesser as many tries as there are instances.
--
-- agent_turn is a turn running somewhere: which conversation, on which
-- instance, and when that instance last said it was still running it. Every
-- instance can then tell whether a conversation has a turn in flight, stop it,
-- send it an answer, and carry on a turn whose instance went away. Its
-- conversation is not a foreign key on purpose: a turn is recorded as it
-- starts, often inside a request that holds its conversation's row, and the
-- check would wait on that request while the request waits on the turn.
--
-- acme_challenge is a challenge a certificate authority is about to check,
-- answered by whichever instance its request reaches.
--
-- mcp_held_result is the rest of a long tool result read with result_more,
-- which the next request may ask any instance for.
--
-- agent_provider_sign_in is a model provider's sign-in waiting for its code,
-- which the dashboard asks after on whichever instance its request reaches.
--
-- agent_provider_access_token is the access token a signed-in provider was
-- last given, and the refresh token it was given for, sealed together: an
-- instance whose own has run out takes this one rather than spending the
-- refresh token again, which the provider would refuse.
--
-- location_visit_notice is an arrival or departure that waits before it is
-- told, so that a restart does not lose it.
--
-- agent_computer_attachment is which instance holds the websocket of a
-- person's computer, and when it last said it still did. Only that instance
-- can read the computer, so a job for it claimed by another instance waits for
-- the right one. An instance that stopped without saying so is known by its
-- heartbeat going stale. It has no foreign keys on purpose: a computer attaches
-- outside any transaction, and a key check could wait on a row another request
-- holds.
CREATE TABLE "throttle" (
    "throttle_key" character varying(512)   NOT NULL PRIMARY KEY,
    "taken_at"     timestamp with time zone NOT NULL
);

CREATE INDEX "throttle_taken" ON "throttle" ("taken_at");

CREATE TABLE "rate_limit_bucket" (
    "limiter_key" character varying(512)   NOT NULL PRIMARY KEY,
    "token_count" double precision         NOT NULL,
    "refilled_at" timestamp with time zone NOT NULL
);

CREATE INDEX "rate_limit_bucket_refilled" ON "rate_limit_bucket" ("refilled_at");

CREATE TABLE "agent_turn" (
    "run_id"          character varying(32)    NOT NULL PRIMARY KEY,
    "conversation_id" character varying(32)    NOT NULL,
    "instance"        character varying(64)    NOT NULL,
    "started_at"      timestamp with time zone NOT NULL,
    "heartbeat_at"    timestamp with time zone NOT NULL,
    "cut_at"          timestamp with time zone
);

CREATE INDEX "agent_turn_conversation" ON "agent_turn" ("conversation_id");
CREATE INDEX "agent_turn_instance" ON "agent_turn" ("instance");

CREATE TABLE "acme_challenge" (
    "challenge_key"     character varying(512)   NOT NULL PRIMARY KEY,
    "challenge_kind"    character varying(16)    NOT NULL,
    "key_authorization" text                     NOT NULL,
    "expires_at"        timestamp with time zone NOT NULL,
    CHECK ("challenge_kind" IN ('http-01', 'tls-alpn-01'))
);

CREATE INDEX "acme_challenge_expires" ON "acme_challenge" ("expires_at");

CREATE TABLE "mcp_held_result" (
    "id"           character varying(64)    NOT NULL PRIMARY KEY,
    "holder"       character varying(256)   NOT NULL,
    "result_text"  text                     NOT NULL,
    "is_untrusted" boolean                  NOT NULL DEFAULT false,
    "is_error"     boolean                  NOT NULL DEFAULT false,
    "stored_at"    timestamp with time zone NOT NULL
);

CREATE INDEX "mcp_held_result_holder" ON "mcp_held_result" ("holder", "stored_at");
CREATE INDEX "mcp_held_result_stored" ON "mcp_held_result" ("stored_at");

CREATE TABLE "agent_provider_sign_in" (
    "id"                    character varying(64)    NOT NULL PRIMARY KEY,
    "provider_name"         character varying(128)   NOT NULL,
    "user_code"             character varying(64)    NOT NULL,
    "verification_address"  text                     NOT NULL,
    "sealed_device_auth_id" text                     NOT NULL,
    "poll_interval_ms"      bigint                   NOT NULL,
    "expires_at"            timestamp with time zone NOT NULL
);

CREATE INDEX "agent_provider_sign_in_expires" ON "agent_provider_sign_in" ("expires_at");

CREATE TABLE "agent_provider_access_token" (
    "provider_name"       character varying(128)   NOT NULL PRIMARY KEY,
    "sealed_access_token" text                     NOT NULL,
    "expires_at"          timestamp with time zone NOT NULL,
    "updated_at"          timestamp with time zone NOT NULL
);

CREATE TABLE "location_visit_notice" (
    "id"               character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "client_visit_key" character varying(128)   NOT NULL,
    "is_arrival"       boolean                  NOT NULL,
    "happened_at"      timestamp with time zone NOT NULL,
    "due_at"           timestamp with time zone NOT NULL,
    "claimed_at"       timestamp with time zone,
    "created_at"       timestamp with time zone NOT NULL
);

CREATE INDEX "location_visit_notice_due" ON "location_visit_notice" ("due_at");

CREATE TABLE "agent_computer_attachment" (
    "agent_id"      character varying(32)    NOT NULL,
    "computer_name" character varying(200)   NOT NULL,
    "instance"      character varying(64)    NOT NULL,
    "heartbeat_at"  timestamp with time zone NOT NULL,
    PRIMARY KEY ("agent_id", "computer_name")
);
