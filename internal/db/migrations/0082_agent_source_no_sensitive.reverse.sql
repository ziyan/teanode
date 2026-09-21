ALTER TABLE "agent_source" ADD COLUMN "sensitive" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "agent_source" ADD COLUMN "allowed" jsonb NOT NULL DEFAULT '[]';
