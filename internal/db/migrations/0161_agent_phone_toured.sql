-- When the agent showed the person around a phone they had just connected:
-- what this server lets the phone do, one thing at a time. Once per phone;
-- NULL until then.
ALTER TABLE "agent_phone" ADD COLUMN "toured_at" timestamp with time zone;
