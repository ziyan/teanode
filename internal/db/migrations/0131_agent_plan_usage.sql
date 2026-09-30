-- What each provider paid for by subscription last said of its plan's
-- allowance. The provider keeps it in memory; this is the copy a restart
-- reads back, so the dashboard does not show nothing until the plan next
-- answers. One row a provider, overwritten only by a newer reading.
CREATE TABLE "agent_plan_usage" (
    "provider"    varchar(200) NOT NULL PRIMARY KEY,
    "observed_at" timestamptz  NOT NULL,
    "plan_usage"  jsonb        NOT NULL
);
