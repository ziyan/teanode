-- An account's identity at an identity provider: which provider, and the
-- subject the provider calls them, which is the one thing about a person a
-- provider promises never to change. One person may arrive through several
-- providers; one (provider, subject) is one person.
CREATE TABLE "user_identity" (
    "id"          varchar(26)  PRIMARY KEY,
    "created_at"  timestamptz  NOT NULL DEFAULT now(),
    "user_id"     varchar(26)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "provider"    varchar(64)  NOT NULL,
    "subject"     varchar(255) NOT NULL,
    -- What the provider said last time, for the audit trail and the Users page.
    "email"       varchar(255) NOT NULL DEFAULT '',
    "last_seen_at" timestamptz NOT NULL DEFAULT now(),
    UNIQUE ("provider", "subject")
);
CREATE INDEX "user_identity_user" ON "user_identity" ("user_id");
