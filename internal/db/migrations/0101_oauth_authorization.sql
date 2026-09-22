-- A program can be authorized to act for somebody, instead of being handed a
-- token that somebody typed.
--
-- Three additions. A client is a program that introduced itself; it holds no
-- secret, because a program on somebody's own machine cannot keep one, and it
-- can do nothing at all until a person approves it. An authorization is the
-- few minutes between a person approving and the program collecting, and is
-- deleted the moment it is spent. The two columns on "token" say which client
-- holds a token and what it was issued for, so that one minted for the agent
-- tools endpoint cannot be replayed anywhere else.

CREATE TABLE "oauth_client" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,
    "modified_at" timestamp with time zone,

    -- What the program calls itself. Shown to the person approving it, and
    -- not to be trusted: anybody may register under any name.
    "name" character varying(256) NOT NULL DEFAULT '',

    -- Where an approval may be sent back to, one per line. Checked on
    -- registration and again at approval time; an address that is not on
    -- this list is refused rather than redirected to.
    "redirect_uris" text NOT NULL DEFAULT '',

    -- When somebody last approved this client, so a registration nobody ever
    -- used can be swept without touching one in service.
    "approved_at" timestamp with time zone,

    PRIMARY KEY ("id")
);

CREATE INDEX "idx_oauth_client_created_at" ON "oauth_client" ("created_at");
CREATE INDEX "idx_oauth_client_approved_at" ON "oauth_client" ("approved_at");

CREATE TABLE "oauth_authorization" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,

    "client_id" character varying(32) NOT NULL,
    "user_id" character varying(32) NOT NULL,

    -- Where this particular approval is to be sent, which must be one of the
    -- client's registered addresses, and must match again at collection.
    "redirect_uri" text NOT NULL,

    -- The hash the program sent when it asked. It collects by presenting the
    -- original, and only the program that asked has it, so an approval
    -- captured in transit is worth nothing.
    "code_challenge" character varying(128) NOT NULL,

    -- What the token will be good for. Carried from the request so the token
    -- minted at collection is bound to the same thing.
    "resource" text NOT NULL DEFAULT '',

    -- Only the hash, as everywhere else here: a copy of this table is not a
    -- set of usable approvals.
    "key_hash" character varying(64) NOT NULL,

    -- Minutes, not hours. An approval is collected immediately or not at all.
    "expires_at" timestamp with time zone NOT NULL,

    PRIMARY KEY ("id"),
    CONSTRAINT "fk_oauth_authorization_client" FOREIGN KEY ("client_id")
        REFERENCES "oauth_client" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_oauth_authorization_expires_at" ON "oauth_authorization" ("expires_at");
CREATE INDEX "idx_oauth_authorization_user_id" ON "oauth_authorization" ("user_id");

-- Which program holds a token, so a person reading their list sees a name
-- rather than an opaque row. Null is every token that exists today: one a
-- person minted for themselves, belonging to no program.
ALTER TABLE "token" ADD COLUMN "client_id" character varying(32);

-- What the token is good for. Empty means what it has always meant: good for
-- this server, the way a token minted by hand is.
ALTER TABLE "token" ADD COLUMN "resource" text NOT NULL DEFAULT '';

-- The other half of a refresh, hashed like every other secret here. Null on a
-- token that cannot be refreshed, which is every token minted by hand.
ALTER TABLE "token" ADD COLUMN "refresh_hash" character varying(64);

CREATE INDEX "idx_token_client_id" ON "token" ("client_id") WHERE "client_id" IS NOT NULL;
