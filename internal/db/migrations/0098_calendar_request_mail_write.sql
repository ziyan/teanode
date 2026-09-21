-- Proposal acceptance records whether it required mail-write permission.
ALTER TABLE "calendar_request" ADD COLUMN "is_mail_write_required" boolean NOT NULL DEFAULT false;
