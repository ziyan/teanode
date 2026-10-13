DROP INDEX "recording_speaker_identity_account_owner";
ALTER TABLE "recording_speaker_identity" DROP COLUMN "is_account_owner";
ALTER TABLE "recording" DROP COLUMN "recording_summary_queued_at";
ALTER TABLE "recording" DROP COLUMN "recording_title_source";
ALTER TABLE "recording" DROP COLUMN "recording_summary";
