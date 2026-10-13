-- Labels without a sample cannot satisfy the older rule; they are dropped.
DELETE FROM "recording_temporary_speaker" WHERE "passage_end_ms" - "passage_start_ms" NOT BETWEEN 2000 AND 10000;
ALTER TABLE "recording_temporary_speaker" DROP CONSTRAINT "recording_temporary_speaker_sample_passage";
ALTER TABLE "recording_temporary_speaker" ADD CONSTRAINT "recording_temporary_speaker_check" CHECK ("passage_end_ms" - "passage_start_ms" BETWEEN 2000 AND 10000);
