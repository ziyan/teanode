-- Cancellations must survive retries and message retention, just as acceptance does.
CREATE TABLE "mail_submission_cancellation" (
    "owner_id" character varying(32) NOT NULL,
    "submission_id" character varying(128) NOT NULL,
    PRIMARY KEY ("owner_id", "submission_id"),
    CHECK ("owner_id" <> '' AND "submission_id" <> '')
);
