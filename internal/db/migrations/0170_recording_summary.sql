ALTER TABLE "recording" ADD COLUMN "recording_summary" text NOT NULL DEFAULT '';
ALTER TABLE "recording" ADD COLUMN "recording_title_source" varchar(16) NOT NULL DEFAULT 'none';
ALTER TABLE "recording" ADD COLUMN "recording_summary_queued_at" timestamptz;
UPDATE "recording" SET "recording_title_source" = 'user' WHERE "recording_title" <> '';
ALTER TABLE "recording_speaker_identity" ADD COLUMN "is_account_owner" boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX "recording_speaker_identity_account_owner" ON "recording_speaker_identity" ("user_id") WHERE "is_account_owner";
