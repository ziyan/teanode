-- Both casts can fail, on exactly the content the forward migration exists to
-- allow. Reversing is only safe on a deployment that never stored a NUL.
ALTER TABLE "alias" ALTER COLUMN "mail_server" TYPE jsonb USING "mail_server"::jsonb;
ALTER TABLE "setting" ALTER COLUMN "value" TYPE jsonb USING "value"::jsonb;
