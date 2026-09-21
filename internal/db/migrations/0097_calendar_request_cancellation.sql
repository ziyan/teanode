-- The same account/request lock serializes these rows with completed receipts.
CREATE TABLE "calendar_request_cancellation" (
    "user_id" character varying(32) NOT NULL,
    "request_id" character varying(128) NOT NULL,
    "cancelled_at" timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY ("user_id", "request_id"),
    CHECK ("user_id" <> '' AND "request_id" <> '')
);
