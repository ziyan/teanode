ALTER TABLE "recording_transcript_text_correction" ADD COLUMN "text_correction_origin" varchar(16) NOT NULL DEFAULT 'person' CHECK ("text_correction_origin" IN ('person', 'glossary'));
ALTER TABLE "recording_transcript_text_correction" ADD COLUMN "glossary_term" text NOT NULL DEFAULT '';
ALTER TABLE "recording_transcript_text_correction" ADD COLUMN "original_phrase" text NOT NULL DEFAULT '';
ALTER TABLE "recording_transcript_text_correction" ADD COLUMN "corrected_phrase" text NOT NULL DEFAULT '';
