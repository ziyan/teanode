-- Passkeys: the credentials authenticators hold on an operator's behalf.
--
-- The private half never leaves the authenticator, so this table holds nothing
-- that would let anybody sign in — only the public half and enough to
-- recognise the credential when it answers a challenge. A copy of this table
-- is not a set of working credentials, which is the whole point of WebAuthn
-- over a password.
CREATE TABLE "passkey" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- Removing an account takes its passkeys with it, the way it takes its
    -- sessions and tokens.
    "user_id" character varying(32) NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,

    -- What the person calls this authenticator.
    "name" character varying(128) NOT NULL DEFAULT '',

    -- What the authenticator returns to identify itself. Unique across the
    -- server: sign-in looks a credential up by this and nothing else, because
    -- a discoverable credential means the server is never told who is signing
    -- in before the assertion arrives.
    "credential_id" bytea NOT NULL,

    -- The public half, and how the authenticator vouched for itself.
    "public_key" bytea NOT NULL,
    "attestation_type" character varying(64) NOT NULL DEFAULT '',

    -- usb, nfc, ble, internal, hybrid. Passed back at sign-in so the browser
    -- can offer the right thing rather than every thing.
    "transports" text NOT NULL DEFAULT '',

    -- The make and model of the authenticator, when it says. Most passkeys
    -- deliberately do not.
    "aaguid" bytea,

    -- The authenticator's own counter. A count that does not advance means
    -- two authenticators are answering for one credential.
    "sign_count" bigint NOT NULL DEFAULT 0,

    "backup_eligible" boolean NOT NULL DEFAULT false,
    "backup_state" boolean NOT NULL DEFAULT false,

    "used_at" timestamp with time zone,
    "ip" character varying(64) NOT NULL DEFAULT '',
    "user_agent" text NOT NULL DEFAULT '',

    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "idx_passkey_credential_id" ON "passkey" ("credential_id");
CREATE INDEX "idx_passkey_user_id" ON "passkey" ("user_id");
