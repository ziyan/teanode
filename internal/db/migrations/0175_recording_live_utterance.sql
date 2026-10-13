-- Words heard while a recording is still being captured, kept only until the
-- saved audio has been transcribed. Derived data: nothing refers to it.
CREATE TABLE "recording_live_utterance" (
    "id" varchar(32) PRIMARY KEY,
    "recording_id" varchar(32) NOT NULL REFERENCES "recording"("id") ON DELETE CASCADE,
    "created_at" timestamptz NOT NULL,
    "utterance_id" varchar(128) NOT NULL,
    "utterance_start_ms" bigint NOT NULL CHECK ("utterance_start_ms" >= 0),
    "utterance_end_ms" bigint NOT NULL CHECK ("utterance_end_ms" >= "utterance_start_ms"),
    "transcript_text" text NOT NULL,
    UNIQUE ("recording_id", "utterance_id")
);
CREATE INDEX "recording_live_utterance_timeline" ON "recording_live_utterance" ("recording_id", "utterance_start_ms", "id");
