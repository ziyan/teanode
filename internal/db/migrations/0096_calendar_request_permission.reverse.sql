-- Drain calendar commands and discard pending retry requests before downgrade.
ALTER TABLE "calendar_request" DROP COLUMN "is_mail_send_required";
