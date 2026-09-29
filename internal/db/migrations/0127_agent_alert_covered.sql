-- What an alert was about, in terms that do not depend on how the model
-- worded it: the bursts it covered (a sender's address and a subject with
-- the digits out), the senders' addresses and domains, and what the
-- sorting called the messages. A mute taken from an alert names these,
-- so it holds for the next message from the same sender or of the same
-- burst whatever subject key the model gives that one. Empty for alerts
-- made before, whose terms are read from their candidates instead.
ALTER TABLE "agent_alert" ADD COLUMN "covered_burst_keys" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "agent_alert" ADD COLUMN "covered_sender_addresses" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "agent_alert" ADD COLUMN "covered_sender_domains" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "agent_alert" ADD COLUMN "covered_mail_categories" jsonb NOT NULL DEFAULT '[]';
