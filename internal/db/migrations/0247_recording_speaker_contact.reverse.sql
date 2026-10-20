DROP INDEX "recording_speaker_identity_contact";
ALTER TABLE "recording_speaker_identity" DROP CONSTRAINT "recording_speaker_identity_contact_fkey";
ALTER TABLE "recording_speaker_identity" DROP CONSTRAINT "recording_speaker_identity_contact_both_or_neither";
ALTER TABLE "recording_speaker_identity" DROP COLUMN "contact_id";
ALTER TABLE "recording_speaker_identity" DROP COLUMN "contact_addressbook_id";
