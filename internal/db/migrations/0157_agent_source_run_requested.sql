-- When a source was last asked to be read now (a coding session that just
-- answered), so that a request made while a pass is under way is honored
-- when that pass ends, rather than lost when the pass writes its next
-- scheduled time over next_run_at. A pass that starts after the request
-- clears it: it reads what was asked for.
ALTER TABLE "agent_source" ADD COLUMN IF NOT EXISTS "run_requested_at" timestamp with time zone;
