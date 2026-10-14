-- The token of the person's tax document address, sealed with the
-- server's secret: their mailbox address with +taxes- and the token, where
-- the PDFs and pictures mailed in are read as paystubs and W-2s. Empty
-- until the address is first asked for.
ALTER TABLE "agent" ADD COLUMN "tax_document_token" text NOT NULL DEFAULT '';
