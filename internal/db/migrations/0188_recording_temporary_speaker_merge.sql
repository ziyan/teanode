-- A new speaker label of a live capture can be one more label for a voice the
-- recording already has: the provider is sent only four samples with a piece,
-- so a fifth voice comes back under a fresh letter and gets a label of its
-- own. When the provider says the new label's voice is an earlier label's, the
-- new one points at it here and reads as it. Separating clears the pointer.
-- merge_checked_at is when the provider was asked, so each label is asked once.
ALTER TABLE "recording_temporary_speaker" ADD COLUMN "merged_into_temporary_speaker_id" varchar(32)
    REFERENCES "recording_temporary_speaker"("id") ON DELETE SET NULL;
ALTER TABLE "recording_temporary_speaker" ADD COLUMN "merge_checked_at" timestamptz;
