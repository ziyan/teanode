-- Downgrade discards retry identities and pending mailbox reconciliation.
-- Drain submission reconciliation and stop sending clients before reverting.
DROP TABLE "mail_submission";
