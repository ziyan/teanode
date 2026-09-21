-- Only the row the forward migration inserted. An operator may have given
-- agent:act to a role of their own, or to Operator, since; a blanket
-- delete on the permission key would take those away too, and a downgrade
-- is not a decision about who may read other people's conversations.
DELETE FROM "role_permission"
WHERE "permission_key" = 'agent:act'
  AND "role_id" IN (SELECT "id" FROM "role" WHERE lower("name") = 'administrator');
