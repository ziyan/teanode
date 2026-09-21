-- Narrowing again would refuse the rows that are the reason this widened,
-- so anything longer is cleared rather than the migration failing.
UPDATE "agent_node" SET "contact_id" = NULL WHERE length("contact_id") > 32;
UPDATE "user" SET "contact_id" = NULL WHERE length("contact_id") > 32;
ALTER TABLE "agent_node" ALTER COLUMN "contact_id" TYPE character varying(32);
ALTER TABLE "user" ALTER COLUMN "contact_id" TYPE character varying(32);
