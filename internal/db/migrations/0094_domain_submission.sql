-- Domain operators and the console need no mailbox. Identities survive retention.
CREATE TABLE "domain_submission" (
    "principal_id" character varying(64) NOT NULL,
    "submission_id" character varying(128) NOT NULL,
    "domain_id" character varying(32) NOT NULL,
    "request_digest" character varying(64) NOT NULL,
    "mail_id" character varying(32) NOT NULL,
    "accepted_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("principal_id", "submission_id"),
    CHECK ("principal_id" <> '' AND "submission_id" <> '' AND "domain_id" <> '' AND "mail_id" <> ''),
    CHECK ("request_digest" ~ '^[0-9a-f]{64}$')
);
