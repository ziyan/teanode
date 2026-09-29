-- A subagent's run was kept without a kind, so the runs list showed an
-- empty tag beside it and a filter by kind could not find it. New runs
-- carry the kind subagent; the ones kept before are given it here, found
-- by the surface every subagent run is made with.
UPDATE "agent_conversation" SET "job_kind" = 'subagent'
WHERE "kind" = 'run' AND "surface" = 'subagent' AND "job_kind" = '';
