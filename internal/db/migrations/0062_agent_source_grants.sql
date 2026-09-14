-- The calendar and the address book become sources the agent is granted.
--
-- The rule this program was built on is that nothing from a source the person
-- has not granted is ever sent to a model. A mailbox has had that switch since
-- the agent did (mailbox.agent -> granted); the calendar and the address book
-- never did, and were reachable on the person's own permission alone. So an
-- agent granted one mailbox could read every appointment in the diary, which
-- is not what granting one mailbox means.
--
-- One switch per collection rather than one for "calendars": a person may keep
-- a work calendar and a family one and mean different things by them.
--
-- False by default, including for the calendars and address books that already
-- exist. An agent that could read them yesterday cannot today until the person
-- says so, which is the safe direction for a default to be wrong in, and the
-- agent's page puts the switches beside the mailboxes.
ALTER TABLE "calendar" ADD COLUMN "agent_granted" boolean NOT NULL DEFAULT false;
ALTER TABLE "addressbook" ADD COLUMN "agent_granted" boolean NOT NULL DEFAULT false;
