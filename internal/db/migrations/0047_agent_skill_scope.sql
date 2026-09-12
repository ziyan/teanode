-- Who fills in a skill's secrets. A skill's author says, per secret, whether
-- a value is the deployment's or each person's own, which is right most of
-- the time: a licence the organisation bought is the operator's, an account
-- with a service is the person's. But the same skill can be either, depending
-- on the deployment -- one household with one camera system, or twenty people
-- each with their own -- and only the operator knows which this is. Empty
-- means the skill's own declaration stands.
ALTER TABLE "agent_skill" ADD COLUMN "scope" varchar(16) NOT NULL DEFAULT '';
