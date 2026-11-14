-- The phone's operating system, as the app said when it registered. It says
-- which of the phone's words are true: an iPhone is rung through PushKit and
-- an Android phone through a high priority message of its own, and the
-- dashboard, the command line and the agent tell the person which phone is
-- which. Every phone that exists is an iPhone, the only app there was.
ALTER TABLE "agent_phone"
    ADD COLUMN "phone_platform" character varying(16) NOT NULL DEFAULT 'ios'
    CHECK ("phone_platform" IN ('ios', 'android'));
