-- When a piece of a capture was stitched to the piece before it, so the piece
-- after it can read where its last passage ended. A nullable column with no
-- default is a catalog change only: it does not rewrite the table or scan it.
ALTER TABLE "recording_transcript_run" ADD COLUMN "published_at" timestamptz;
