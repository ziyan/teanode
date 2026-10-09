-- A coding session finds its checkout's page from the lines a checkout's
-- profile writes ("The checkout is at ...", "Lives at ..."), looked up by
-- how they start, at every session start and every prompt. Without an
-- index the lookup read every live fact the agent has: 157 ms on two
-- hundred thousand facts, up to three times a prompt. text_pattern_ops,
-- because a prefix LIKE cannot use an index in the database's collation.
CREATE INDEX "agent_fact_line_start" ON "agent_fact"
    ("agent_id", left("text", 24) text_pattern_ops)
    WHERE NOT "dormant" AND "superseded_by" IS NULL;
