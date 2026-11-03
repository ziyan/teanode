-- A schedule can run when mail arrives instead of at set times. mail_conditions
-- are the tests an arriving message must pass, the same ones a mailbox rule
-- makes; a schedule with them has no cron line. mail_arrivals are the
-- messages that passed since the schedule last ran, which its next run is
-- told about and then forgets.
ALTER TABLE "agent_schedule" ADD COLUMN "mail_conditions" jsonb;
ALTER TABLE "agent_schedule" ADD COLUMN "mail_arrivals" jsonb;
