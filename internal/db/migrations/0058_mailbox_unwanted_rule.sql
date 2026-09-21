-- The rule every mailbox now starts with, given to the ones that already
-- exist.
--
-- It files what an agent sorts as phishing or junk into the Junk folder and
-- marks it read. Switched on, and safe to switch on for somebody who never
-- asked for it, because the category it matches is only ever set by an agent
-- sorting that mailbox's mail: a mailbox with no agent behaves exactly as it
-- did. It is in the rules list like any other rule, so it can be changed or
-- switched off by whoever owns the mailbox.
--
-- Only where there is nothing like it already: a mailbox whose owner has
-- written their own rule about these categories keeps theirs, and a mailbox
-- that somehow has no Junk folder is left alone rather than given a rule that
-- files into nowhere.
UPDATE "mailbox" AS m
SET "rules" = COALESCE(m."rules", '[]'::jsonb) || jsonb_build_array(jsonb_build_object(
        'name', 'Phishing and junk',
        'enabled', true,
        'stop', true,
        'conditions', jsonb_build_array(jsonb_build_object(
            'field', 'category', 'operator', 'matches', 'value', '^(phishing|junk)$')),
        'actions', jsonb_build_array(
            jsonb_build_object('kind', 'move', 'folderId', f."id"),
            jsonb_build_object('kind', 'markRead'))))
FROM "mailbox_folder" AS f
WHERE f."mailbox_id" = m."id"
  AND f."kind" = 'junk'
  AND NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(m."rules", '[]'::jsonb)) AS rule,
           jsonb_array_elements(COALESCE(rule->'conditions', '[]'::jsonb)) AS condition
      WHERE condition->>'field' = 'category'
        AND COALESCE(condition->>'value', '') LIKE '%phishing%');
