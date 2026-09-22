-- Keep request identity after event deletion without retaining calendar content.
CREATE TABLE "calendar_request" (
    "user_id" character varying(32) NOT NULL,
    "request_id" character varying(128) NOT NULL,
    "operation" character varying(16) NOT NULL,
    "calendar_id" character varying(32) NOT NULL,
    "object_id" character varying(255) NOT NULL,
    "request_digest" character varying(64) NOT NULL,
    "completed_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "request_id"),
    CHECK ("user_id" <> '' AND "request_id" <> '' AND "calendar_id" <> '' AND "object_id" <> ''),
    CHECK ("operation" IN ('save', 'delete', 'answer')),
    CHECK ("request_digest" ~ '^[0-9a-f]{64}$')
);
