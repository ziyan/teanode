-- Drain calendar commands and discard pending proposal requests before downgrade.
ALTER TABLE "calendar_request" DROP COLUMN "is_mail_write_required";
