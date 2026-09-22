-- Stop senders and discard pending client requests before downgrade: cancellation
-- identities are lost, so an older server could accept a delayed request again.
DROP TABLE "mail_submission_cancellation";
