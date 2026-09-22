-- Drain calendar commands and discard pending requests before downgrade.
-- Completed receipts, events and accepted mail are preserved.
DROP TABLE "calendar_request_cancellation";
