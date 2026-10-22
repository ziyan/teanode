-- The Operator role loses agent:act: every person's agent conversations and
-- runs, and a word with their agent as them. It is content, which 0080 kept
-- for the Administrator role, but a server seeded after 0080 gave it to
-- Operator too, because Operator was seeded as everything except managing
-- who may do what. A server upgraded through 0080 never had it there.
DELETE FROM "role_permission"
WHERE "permission_key" = 'agent:act'
  AND "role_id" IN (SELECT "id" FROM "role" WHERE lower("name") = 'operator');
