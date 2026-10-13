-- A voice with no clean passage to cut a sample from still gets a temporary
-- speaker label, with no sample and no passage bounds.
ALTER TABLE "recording_temporary_speaker" DROP CONSTRAINT "recording_temporary_speaker_check";
ALTER TABLE "recording_temporary_speaker" ADD CONSTRAINT "recording_temporary_speaker_sample_passage" CHECK (
 ("recording_blob_id" IS NULL AND "passage_start_ms" = 0 AND "passage_end_ms" = 0)
 OR ("passage_end_ms" - "passage_start_ms" BETWEEN 2000 AND 10000)
);
