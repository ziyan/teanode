-- A device can be signed in by approving a short code somewhere else
-- (RFC 8628): a phone app shows "KQ7M-4TXD", and the person approves it on the
-- dashboard they are already signed in to, or tells their agent the code. The
-- phone has been asking all along and collects its token once it is approved.
--
-- A row lives ten minutes. It is approved or refused by a person, and deleted
-- the moment its token is collected, so a code issues at most one token.

CREATE TABLE "oauth_device_authorization" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,

    "client_id" character varying(32) NOT NULL,

    -- The secret the device polls with, hashed like every other secret here:
    -- a copy of this table is not a set of codes anybody can collect with.
    "device_code_hash" character varying(64) NOT NULL,

    -- What the person reads off the device and types, without its dash.
    -- Short on purpose, so it is guarded by expiry, by being worth nothing
    -- until a signed-in person approves it, and by the login limiter
    -- counting wrong codes.
    "user_code" character varying(16) NOT NULL,

    -- What the device calls itself ("Ana's iPhone"), shown beside the
    -- program's name when the person approves. Claimed, not verified.
    "device_name" character varying(128) NOT NULL DEFAULT '',

    -- What the token will be good for. Empty is the whole API, as for a
    -- token minted by hand; the agent tools endpoint is the narrower one.
    "resource" text NOT NULL DEFAULT '',

    -- Who approved it, once somebody has; whether it was refused instead.
    "user_id" character varying(32),
    "is_denied" boolean NOT NULL DEFAULT false,
    "decided_at" timestamp with time zone,

    -- When the device last asked, so one asking faster than it was told to
    -- is told to slow down.
    "polled_at" timestamp with time zone,

    "expires_at" timestamp with time zone NOT NULL,

    PRIMARY KEY ("id"),
    CONSTRAINT "fk_oauth_device_authorization_client" FOREIGN KEY ("client_id")
        REFERENCES "oauth_client" ("id") ON DELETE CASCADE
);

CREATE UNIQUE INDEX "idx_oauth_device_authorization_device_code_hash"
    ON "oauth_device_authorization" ("device_code_hash");
CREATE UNIQUE INDEX "idx_oauth_device_authorization_user_code"
    ON "oauth_device_authorization" ("user_code");
CREATE INDEX "idx_oauth_device_authorization_expires_at"
    ON "oauth_device_authorization" ("expires_at");
