-- The words a second, vocabulary-aware pass of the plain transcription model
-- heard in the span of a segment. Empty means the pass did not run or failed
-- for this segment, and the speaker-separating model's words stand.
ALTER TABLE "recording_transcript_segment" ADD COLUMN "accurate_transcript_text" text NOT NULL DEFAULT '';
