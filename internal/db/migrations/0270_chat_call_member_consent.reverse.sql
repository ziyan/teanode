ALTER TABLE "chat_call_member" ADD COLUMN "transcription_consent" character varying(8) NOT NULL DEFAULT ''
    CHECK ("transcription_consent" IN ('', 'yes', 'no'));
