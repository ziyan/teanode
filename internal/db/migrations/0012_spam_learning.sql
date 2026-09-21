-- What the built-in spam filter has learned, and from which messages.
--
-- Both tables are here rather than on an instance's disk because a server can
-- run as several instances against one database. A classifier that differed
-- between them would score the same message differently depending on which
-- instance happened to receive it, and nothing would show that had happened.
--
-- spam_token holds the counts. Tokens are stored as text rather than hashed:
-- a hashed table is impossible to inspect when the classifier misbehaves, and
-- hashing protects little when this same database already holds every
-- message's subject, sender and recipients in the clear.
CREATE TABLE "spam_token" (
    "token"       varchar(64) NOT NULL,
    "spam_count"  bigint      NOT NULL DEFAULT 0,
    "ham_count"   bigint      NOT NULL DEFAULT 0,
    "modified_at" timestamptz NOT NULL,
    PRIMARY KEY ("token")
);

-- spam_training records which messages taught it, which solves three problems
-- at once: marking the same message twice must not count it twice, changing a
-- label must be able to subtract exactly what was added, and the corpus
-- totals the classifier waits for are then a row count rather than a number
-- that can drift away from reality.
CREATE TABLE "spam_training" (
    "mail_id"     varchar(32) NOT NULL,
    "label"       varchar(16) NOT NULL,
    "created_at"  timestamptz NOT NULL,
    "modified_at" timestamptz NOT NULL,
    PRIMARY KEY ("mail_id")
);

CREATE INDEX "spam_training_label" ON "spam_training" ("label");
