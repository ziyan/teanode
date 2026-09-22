-- Drain calendar commands and discard pending retry requests before downgrade.
-- Event and mail rows remain; completed request identities are lost.
DROP TABLE "calendar_request";
