-- The Administrator role gains agent:act: every person's agent
-- conversations and runs, and a word with their agent as them. Content,
-- so the Operator role does not get it. Once, here, so an operator who
-- later takes it away is not overruled at every start.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'agent:act' FROM "role" WHERE lower("name") = 'administrator'
ON CONFLICT DO NOTHING;
