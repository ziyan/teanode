-- A phone a person talks to their agent from, in the TeaNode app: one row per
-- install, tied to the OAuth client the app registered as. Its tokens are
-- replaced on every refresh, the client stays: a phone is listed and notified
-- only while the client holds a token that works, and removing the phone
-- revokes them all.
--
-- What reaches the phone goes through a push relay (internal/push): the row
-- keeps the relay's sealed registration, never the device token itself, and
-- the key the phone made for its notifications, sealed like other secrets.

CREATE TABLE "agent_phone" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    "agent_id" character varying(32) NOT NULL,

    -- The OAuth client the app registered as. A phone registering again
    -- updates its row.
    "client_id" character varying(32) NOT NULL,

    -- What the phone calls itself, and what it is ("iPhone16,2").
    "phone_name" character varying(128) NOT NULL DEFAULT '',
    "phone_model" character varying(64) NOT NULL DEFAULT '',

    -- The relay's sealed registration; empty once Apple says the phone is
    -- gone, until the app registers again.
    "push_registration" text NOT NULL DEFAULT '',

    -- The 256-bit key the phone decrypts its notifications with, sealed.
    "payload_key" text NOT NULL DEFAULT '',

    -- Which kinds of notification it wants, comma-separated; empty is all.
    "notification_kinds" text NOT NULL DEFAULT '',

    -- The agent's own turns in the main conversation are sent from here on:
    -- the newest message already sent, or looked past.
    "pushed_through" character varying(32) NOT NULL DEFAULT '',

    "last_seen_at" timestamp with time zone,
    "last_delivered_at" timestamp with time zone,
    "last_delivery_error" text NOT NULL DEFAULT '',

    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "idx_agent_phone_agent_id_client_id" ON "agent_phone" ("agent_id", "client_id");
