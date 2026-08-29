-- Sessions and API tokens, as rows rather than as signed strings and
-- configuration entries.
--
-- A session used to be a cookie carrying a username, an expiry and an HMAC
-- over both, and the server kept nothing. That is what let it survive a
-- restart without a table, and it is also why the only way to end one was to
-- rotate the signing key, which ended every session on the server. A row can
-- be revoked on its own, can say where it was last used from, and disappears
-- when the account does.
--
-- A token used to live inside the account in the configuration. That made
-- every use of it a candidate to rewrite the whole configuration — recording
-- when a token was last used would have bumped the configuration version and
-- had every instance reload. Tokens are data about who has been here, not
-- settings, and they belong with the mail.
CREATE TABLE "session" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- The operator this session belongs to. By name rather than by
    -- identifier, because that is what an account is keyed by in the
    -- configuration; renaming an account is not something the dashboard
    -- offers, and a rename would end that person's sessions, which is
    -- the safe direction.
    "username" character varying(256) NOT NULL,

    -- SHA-256 of the secret half, hex encoded. The secret itself is in the
    -- cookie and nowhere else, so a copy of this table is not a set of
    -- working sessions.
    "key_hash" character varying(64) NOT NULL,

    "expires_at" timestamp with time zone,
    "used_at" timestamp with time zone,

    -- Set rather than deleted, so the list can say "revoked an hour ago"
    -- instead of the row silently vanishing. The sweep removes it later.
    "revoked_at" timestamp with time zone,

    -- Where it was last used from, for the person deciding whether a session
    -- in their list is theirs.
    "ip" character varying(64),
    "user_agent" text,

    PRIMARY KEY ("id")
);

-- The list a person sees is their own sessions, and the sweep looks for
-- expired and long-revoked ones. Both would otherwise scan the table.
CREATE INDEX "idx_session_username" ON "session" ("username");
CREATE INDEX "idx_session_expires_at" ON "session" ("expires_at");
CREATE INDEX "idx_session_revoked_at" ON "session" ("revoked_at") WHERE "revoked_at" IS NOT NULL;

CREATE TABLE "token" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    "username" character varying(256) NOT NULL,

    -- What holds it, for example "laptop". Chosen by the operator.
    "name" character varying(256) NOT NULL DEFAULT '',

    "key_hash" character varying(64) NOT NULL,

    "expires_at" timestamp with time zone,
    "used_at" timestamp with time zone,
    "revoked_at" timestamp with time zone,

    "ip" character varying(64),
    "user_agent" text,

    PRIMARY KEY ("id")
);

CREATE INDEX "idx_token_username" ON "token" ("username");
CREATE INDEX "idx_token_expires_at" ON "token" ("expires_at");
CREATE INDEX "idx_token_revoked_at" ON "token" ("revoked_at") WHERE "revoked_at" IS NOT NULL;

-- The configuration no longer carries tokens. Any that were stored there are
-- gone: their secrets were never recoverable, the format has changed, and
-- there is no way to write a row that would accept one. Issue new ones.
DROP TABLE IF EXISTS "operator_token";
