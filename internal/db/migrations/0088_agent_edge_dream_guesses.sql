-- Mark the links an earlier night guessed as guesses.
--
-- 0085 gave the new column a default of 'stated', so every link that
-- already existed came out of it stated. That was right for every writer
-- of the time but one, and the one is the reason the column exists: the
-- generative half of the night walks the graph, finds two pages a few
-- steps apart, asks a model whether they are related, and writes a link.
-- On a graph that had been dreaming for months those rows are exactly
-- what the distinction was added to hedge, and 0085 left them asserted as
-- firmly as the person's own. Nothing promotes or demotes a link on its
-- own, so without this they would have stayed that way for good.
--
-- No model is asked anything here. What a walk left behind is already
-- stored, in three places, and any one of them is enough:
--
--   * the link's own evidence, under the kind 'dream', which nothing but
--     the walk writes;
--   * before that kind existed the walk filed the same thing as a
--     'document' with no identifier -- a citation of a document that does
--     not exist, which nothing else on a link produces. Together with a
--     'linked' revision in the page's history whose actor is 'dream',
--     that is the older walk;
--   * and the night that made the link wrote the two paths down as a
--     'linked' proposal on its own agent_dream row.
--
-- What is deliberately not enough:
--
--   * A 'linked' revision by the dream on its own. The night states links
--     as well as guessing them -- a month's page links what it was about
--     by counting facts, the reading files the links it read out of a
--     document, and a merge rewrites somebody else's link under whoever
--     merged. Those are derived or inherited, not guessed.
--   * Silence. A link carrying none of the three signals is left alone.
--     Provenance is sometimes genuinely gone: a page's history goes with
--     the page. A guess left stated reads tomorrow as it read yesterday,
--     while a stated link called a guess drops out of the index the
--     prompt carries and starts reading as "perhaps" -- so where it is
--     not clear, nothing changes.
--
-- And a link somebody said is kept stated whoever else said it: a
-- 'linked' revision by any actor other than the dream -- the person in
-- the Link dialog, the memory tool, an ingest, or an actor too old to
-- have been recorded -- means the link was stated too. That is the rule
-- confirming a guess already follows.
--
-- A revision and a proposal name the other end by path, which is what
-- they keep, so a page renamed since the night that linked it is not
-- recognized. Those stay stated, which is the direction chosen above.
--
-- Every row this changes is marked in its evidence, so the reverse puts
-- back exactly these rows and not the ones a later night proposed.
UPDATE "agent_edge" AS edge
SET "status" = 'proposed',
    "evidence" = edge."evidence" || '[{"kind": "dream", "quote": "marked by migration 0088 as a link the night guessed"}]'::jsonb
FROM "agent_node" AS source, "agent_node" AS target
WHERE source."id" = edge."from_id"
  AND target."id" = edge."to_id"
  AND edge."status" = 'stated'
  AND (
    edge."evidence" @> '[{"kind": "dream"}]'
    OR (
      EXISTS (
        SELECT 1 FROM jsonb_array_elements(edge."evidence") AS entry
        WHERE entry->>'kind' = 'document' AND COALESCE(entry->>'id', '') = '')
      AND EXISTS (
        SELECT 1 FROM "agent_revision" AS revision
        WHERE revision."node_id" = edge."from_id"
          AND revision."kind" = 'linked'
          AND revision."actor" = 'dream'
          AND revision."after"->>'relation' = edge."relation"
          AND revision."after"->>'path' = target."path")
    )
    OR EXISTS (
      SELECT 1 FROM "agent_dream" AS dream, jsonb_array_elements(dream."proposals") AS proposal
      WHERE dream."agent_id" = edge."agent_id"
        AND proposal->>'kind' = 'linked'
        AND proposal->>'path' = source."path"
        AND proposal->>'to' = target."path")
  )
  AND NOT EXISTS (
    SELECT 1 FROM "agent_revision" AS revision
    WHERE revision."node_id" = edge."from_id"
      AND revision."kind" = 'linked'
      AND revision."actor" <> 'dream'
      AND revision."after"->>'relation' = edge."relation"
      AND revision."after"->>'path' = target."path");
