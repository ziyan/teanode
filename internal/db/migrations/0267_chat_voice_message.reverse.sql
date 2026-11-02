DROP INDEX IF EXISTS "chat_message_transcript_waiting";
ALTER TABLE "chat_message"
    DROP COLUMN IF EXISTS "transcript_tried_at",
    DROP COLUMN IF EXISTS "transcript_state",
    DROP COLUMN IF EXISTS "transcript_text",
    DROP COLUMN IF EXISTS "is_voice_message";
ALTER TABLE "agent_attachment"
    DROP COLUMN IF EXISTS "audio_waveform",
    DROP COLUMN IF EXISTS "audio_duration_ms";
