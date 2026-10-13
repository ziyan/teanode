DELETE FROM "recording_transcript_text_correction" WHERE "text_correction_origin" = 'glossary';
ALTER TABLE "recording_transcript_text_correction" DROP COLUMN "corrected_phrase";
ALTER TABLE "recording_transcript_text_correction" DROP COLUMN "original_phrase";
ALTER TABLE "recording_transcript_text_correction" DROP COLUMN "glossary_term";
ALTER TABLE "recording_transcript_text_correction" DROP COLUMN "text_correction_origin";
