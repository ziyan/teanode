-- Put back exactly the links the forward migration marked, and take the
-- marker off. A link a later night proposed by itself carries no marker
-- and keeps its status; a marked one the person has since stated has had
-- its evidence written over by that statement, marker and all, so it is
-- not touched either.
UPDATE "agent_edge"
SET "status" = 'stated',
    "evidence" = COALESCE((
      SELECT jsonb_agg(entry) FROM jsonb_array_elements("evidence") AS entry
      WHERE entry->>'quote' IS DISTINCT FROM 'marked by migration 0088 as a link the night guessed'
    ), '[]'::jsonb)
WHERE "evidence" @> '[{"kind": "dream", "quote": "marked by migration 0088 as a link the night guessed"}]';
