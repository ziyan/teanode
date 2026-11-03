-- A call being transcribed transcribes everybody on it; nobody answers for
-- their own voice, so the answer is no longer kept.
ALTER TABLE "chat_call_member" DROP COLUMN "transcription_consent";
