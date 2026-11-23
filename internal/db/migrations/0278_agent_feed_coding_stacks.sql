-- A coding session's questions now stack, one stack for each day of the
-- person's: the cards posted before that go in the stack of the day they were
-- posted, with the stack's title in the person's language. A zone the
-- database does not know counts as UTC.
WITH "coding_card" AS (
    SELECT "item"."id",
        ("item"."created_at" AT TIME ZONE (CASE WHEN "zone"."name" IS NULL THEN 'UTC' ELSE "person"."timezone" END))::date AS "posted_on",
        lower(split_part(split_part(COALESCE(NULLIF("agent"."language", ''), NULLIF("person"."locale", ''), "person"."locale_seen"), '-', 1), '_', 1)) AS "language"
    FROM "agent_feed_item" AS "item"
    JOIN "agent" ON "agent"."id" = "item"."agent_id"
    JOIN "user" AS "person" ON "person"."id" = "item"."user_id"
    LEFT JOIN pg_timezone_names AS "zone" ON "zone"."name" = "person"."timezone"
    WHERE "item"."feed_source_kind" = 'coding_session' AND "item"."feed_stack_key" = ''
)
UPDATE "agent_feed_item" AS "item"
SET "feed_stack_key" = 'coding:' || to_char("coding_card"."posted_on", 'YYYY-MM-DD'),
    "feed_stack_title" = CASE "coding_card"."language"
        WHEN 'zh' THEN '编程会话，' || to_char("coding_card"."posted_on", 'FMMM') || '月' || to_char("coding_card"."posted_on", 'FMDD') || '日 周'
            || (ARRAY['日', '一', '二', '三', '四', '五', '六'])[extract(dow FROM "coding_card"."posted_on")::int + 1]
        WHEN 'ja' THEN 'コーディングセッション、' || to_char("coding_card"."posted_on", 'FMMM') || '月' || to_char("coding_card"."posted_on", 'FMDD') || '日('
            || (ARRAY['日', '月', '火', '水', '木', '金', '土'])[extract(dow FROM "coding_card"."posted_on")::int + 1] || ')'
        ELSE 'Coding sessions, ' || to_char("coding_card"."posted_on", 'Dy Mon FMDD')
    END
FROM "coding_card"
WHERE "item"."id" = "coding_card"."id";
