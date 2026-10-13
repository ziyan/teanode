-- Imports never reported a length, so give each recording without one the end
-- of its last verified original on the timeline.
UPDATE "recording" SET "timeline_duration_ms" = "ends"."end_ms"
FROM (
    SELECT "recording_id", MAX("timeline_start_ms" + "media_duration_ms") AS "end_ms"
    FROM "recording_media"
    WHERE "verified_at" IS NOT NULL
    GROUP BY "recording_id"
) AS "ends"
WHERE "recording"."id" = "ends"."recording_id" AND "recording"."timeline_duration_ms" = 0 AND "ends"."end_ms" > 0;
-- Recordings generated before the summaries named speakers by bare letter are
-- offered to the one-time summary sweep again.
UPDATE "recording" SET "recording_summary_queued_at" = NULL WHERE "recording_title_source" = 'generated';
