-- A message's meaning as a vector, for search by meaning. One row per
-- message per mailbox per model: a person who changes the embedding model
-- gets new rows, and the old ones stay where they are, matching nothing and
-- swept by nothing. Stored as an array of reals rather than a vector type,
-- because the database this ships with is stock PostgreSQL; ranking is done
-- in the server over a mailbox's
-- candidates, which is fine at the size of a personal mailbox.
CREATE TABLE "mail_embedding" (
    "mail_id"    character varying(32)    NOT NULL REFERENCES "mail" ("id") ON DELETE CASCADE,
    "mailbox_id" character varying(32)    NOT NULL REFERENCES "mailbox" ("id") ON DELETE CASCADE,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("mail_id", "mailbox_id", "model")
);
CREATE INDEX "mail_embedding_mailbox_created" ON "mail_embedding" ("mailbox_id", "model", "created_at" DESC);
