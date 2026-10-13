-- Intentionally a no-op. The lengths written by the forward migration are the
-- true lengths of the originals and cannot be told apart from lengths a
-- capture reported, and the summaries queued again have already been
-- regenerated. Leaving the data is harmless to the previous schema.
SELECT 1;
