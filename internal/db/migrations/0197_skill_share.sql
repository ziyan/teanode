-- A person shares named tools of an installed skill with one person in a
-- circle they share, so that the other person's agent can use them, run on
-- the owner's computer as the owner. For the payment skill that is how a
-- spouse buys groceries with the household's account without asking each
-- time, and how a child asks every time.
-- docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md says why
-- it is shared call by call.
--
-- Sharing needs the two people to be in a circle together. That is checked
-- when the share is made and again at every call. What ends the share when
-- they stop sharing one is not this table: the code that takes a person out of
-- a circle deletes the skill shares between people who no longer share any.
--
-- The limits are what the server checks before anything runs. A payment above
-- payment_limit_minor, or that takes the month past monthly_limit_minor, or
-- that has no amount the server can read, or any call at all when
-- is_approval_required is set, waits for the owner. Both limits are in the
-- minor unit of currency_code (cents), so a limit cannot be set without the
-- currency it is in.
CREATE TABLE "skill_share" (
    "id"                  character varying(32)  NOT NULL PRIMARY KEY,
    "created_at"          timestamp with time zone NOT NULL,
    "modified_at"         timestamp with time zone NOT NULL,
    "owner_agent_id"      character varying(32)  NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "skill_name"          character varying(100) NOT NULL CHECK ("skill_name" <> ''),
    "tool_names"          text[]                 NOT NULL CHECK (cardinality("tool_names") > 0),
    "grantee_user_id"     character varying(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "payment_limit_minor" bigint CHECK ("payment_limit_minor" > 0),
    "monthly_limit_minor" bigint CHECK ("monthly_limit_minor" > 0),
    "currency_code"       character varying(3) CHECK ("currency_code" ~ '^[a-z]{3}$'),
    "is_approval_required" boolean NOT NULL DEFAULT false,
    CHECK (("payment_limit_minor" IS NULL AND "monthly_limit_minor" IS NULL) OR "currency_code" IS NOT NULL),
    UNIQUE ("owner_agent_id", "skill_name", "grantee_user_id")
);

-- "What is shared with me" starts from the grantee.
CREATE INDEX "skill_share_grantee" ON "skill_share" ("grantee_user_id");

-- One row is one call of a shared tool, whatever came of it.
--
-- call_state says how it stands: ran (it was carried out on the owner's
-- computer), failed (it was tried and did not complete, or could not be tried
-- because the computer was not connected), refused (the server turned it back
-- before it ran), waiting_for_approval, declined (the owner said no) and
-- expired (the owner did not answer within a day).
--
-- A call is written as ran before it is carried out, and changed to failed if
-- it then errors, so that the month's total counts a call still in flight and
-- two at once cannot both pass the limit. The total is of ran calls only.
--
-- tool_arguments are the arguments the owner is shown and that approving runs,
-- unchanged. request_reason is the other person's agent's own words for why,
-- shown to the owner as the other person's words and never as an instruction.
-- tool_output is what the call answered, which is also how a later call of a
-- tool that names an earlier request (a spend request's identifier) is
-- allowed: only an identifier that appeared in an earlier output of the same
-- share is.
--
-- wake_claimed_at and woken_at are how the other person's conversation is
-- woken once with the outcome of a call that waited: whoever claims the row
-- wakes, and a claim older than the server's bound is taken again.
CREATE TABLE "skill_share_call" (
    "id"                      character varying(32)  NOT NULL PRIMARY KEY,
    "created_at"              timestamp with time zone NOT NULL,
    "modified_at"             timestamp with time zone NOT NULL,
    "skill_share_id"          character varying(32)  NOT NULL REFERENCES "skill_share" ("id") ON DELETE CASCADE,
    "grantee_run_id"          character varying(32),
    "grantee_conversation_id" character varying(32) REFERENCES "agent_conversation" ("id") ON DELETE SET NULL,
    "tool_name"               character varying(100) NOT NULL,
    "tool_arguments"          jsonb                  NOT NULL DEFAULT '{}',
    "request_reason"          text                   NOT NULL DEFAULT '',
    "amount_minor"            bigint,
    "currency_code"           character varying(3),
    "call_state"              character varying(24)  NOT NULL
        CHECK ("call_state" IN ('ran', 'failed', 'refused', 'waiting_for_approval', 'declined', 'expired')),
    "tool_output"             jsonb,
    "decided_at"              timestamp with time zone,
    "decision_note"           text                   NOT NULL DEFAULT '',
    "expires_at"              timestamp with time zone,
    "wake_claimed_at"         timestamp with time zone,
    "woken_at"                timestamp with time zone,
    CHECK ("call_state" <> 'waiting_for_approval' OR "expires_at" IS NOT NULL)
);

CREATE INDEX "skill_share_call_share" ON "skill_share_call" ("skill_share_id", "created_at" DESC);
CREATE INDEX "skill_share_call_waiting" ON "skill_share_call" ("expires_at") WHERE "call_state" = 'waiting_for_approval';
CREATE INDEX "skill_share_call_unwoken" ON "skill_share_call" ("decided_at") WHERE "decided_at" IS NOT NULL AND "woken_at" IS NULL;
