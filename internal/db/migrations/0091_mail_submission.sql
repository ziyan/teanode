-- Accepted identities outlive mail retention; none of these references cascade.
CREATE TABLE "mail_submission" (
    "owner_id" character varying(32) NOT NULL,
    "submission_id" character varying(128) NOT NULL,
    "mailbox_id" character varying(32) NOT NULL,
    "request_digest" character varying(64) NOT NULL,
    "mail_id" character varying(32) NOT NULL,
    "sent_item_id" character varying(32) NOT NULL DEFAULT '',
    "draft_item_id" character varying(32) NOT NULL DEFAULT '',
    "reply_item_id" character varying(32) NOT NULL DEFAULT '',
    "forward_item_id" character varying(32) NOT NULL DEFAULT '',
    "accepted_at" timestamp with time zone NOT NULL,
    "reconciled_at" timestamp with time zone,
    PRIMARY KEY ("owner_id", "submission_id"),
    CHECK ("owner_id" <> '' AND "submission_id" <> '' AND "mailbox_id" <> '' AND "mail_id" <> ''),
    CHECK ("request_digest" ~ '^[0-9a-f]{64}$')
);
CREATE INDEX "mail_submission_pending" ON "mail_submission" ("accepted_at", "owner_id", "submission_id") WHERE "reconciled_at" IS NULL;
