-- Where a piece of a live capture starts reading, when the speech at the end of
-- the piece before it ran into the cut: the first piece's audio is preceded by
-- the tail of the previous piece's, from this timeline millisecond on.
ALTER TABLE "recording_transcript_run" ADD COLUMN "handoff_start_ms" bigint CHECK ("handoff_start_ms" >= 0);
