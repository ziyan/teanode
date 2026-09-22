ALTER TABLE "mail_submission" ADD COLUMN "reconcile_after" timestamp with time zone;
CREATE INDEX "mail_submission_reconcile_due" ON "mail_submission" (COALESCE("reconcile_after", "accepted_at"), "owner_id", "submission_id") WHERE "reconciled_at" IS NULL;
DROP INDEX "mail_submission_pending";
