-- Receipts predating API integration contain no accepted notification sends.
ALTER TABLE "calendar_request" ADD COLUMN "is_mail_send_required" boolean NOT NULL DEFAULT false;
