ALTER TABLE "agent_dream" DROP COLUMN "revised";
DROP INDEX "agent_fact_version";
ALTER TABLE "agent_revision" DROP COLUMN "version";
ALTER TABLE "agent_fact" DROP COLUMN "version";
ALTER TABLE "agent_node" DROP COLUMN "version";
