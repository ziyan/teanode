-- Take back exactly the rule the forward file added, and nothing a person has
-- written themselves: the name and the condition both have to match.
UPDATE "mailbox" AS m
SET "rules" = COALESCE((
        SELECT jsonb_agg(rule)
        FROM jsonb_array_elements(m."rules") AS rule
        WHERE NOT (
            rule->>'name' = 'Phishing and junk'
            AND rule->'conditions' @> '[{"field": "category", "value": "^(phishing|junk)$"}]'::jsonb)
    ), '[]'::jsonb)
WHERE m."rules" @> '[{"name": "Phishing and junk"}]'::jsonb;
