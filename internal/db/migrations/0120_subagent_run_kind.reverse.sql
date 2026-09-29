UPDATE "agent_conversation" SET "job_kind" = ''
WHERE "kind" = 'run' AND "surface" = 'subagent' AND "job_kind" = 'subagent';
