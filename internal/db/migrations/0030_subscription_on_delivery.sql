-- A subscription exists as soon as a list writes, rather than when something
-- is done about it.
--
-- Until now the row recorded only what a reader had asked of a list — muted,
-- pictures allowed, unsubscribe requested — and the list itself was derived
-- from the mail, grouped by "list_key". That works, but it leaves a list with
-- no identity of its own until somebody acts on it: no id to put in a URL, and
-- nowhere to hang anything per-list in future.
--
-- Delivery creates the row now. This gives the lists that arrived before it
-- did the same, so every list a mailbox holds has a row, and adds the
-- subscription to the item, so "what did this list send me" is an index lookup
-- rather than a join through the message to group by a text key.
ALTER TABLE "mailbox_item" ADD COLUMN IF NOT EXISTS "subscription_id" character varying(32);

-- A row for every list this mailbox already holds mail from.
--
-- The id is built the way 0004 builds one, and for the same reason: a
-- migration is SQL and there is nowhere else to make a ULID. Ten characters of
-- millisecond timestamp, most significant first, over Crockford's base32
-- alphabet, then sixteen from an md5 of the row this id belongs to — per row,
-- because an uncorrelated random() is an InitPlan that PostgreSQL evaluates
-- once and reuses, which collides the primary key.
INSERT INTO "mailbox_subscription" ("id", "created_at", "modified_at", "mailbox_id", "list_key")
SELECT
    (
        SELECT string_agg(
            substr(
                '0123456789abcdefghjkmnpqrstvwxyz',
                (((floor(extract(epoch from now()) * 1000)::bigint >> (place * 5)) & 31)::int) + 1,
                1
            ),
            '' ORDER BY place DESC
        )
        FROM generate_series(0, 9) AS place
    ) || substr(md5("held"."mailbox_id" || "held"."list_key" || clock_timestamp()::text), 1, 16),
    now(),
    now(),
    "held"."mailbox_id",
    "held"."list_key"
FROM (
    SELECT DISTINCT "mailbox_folder"."mailbox_id" AS "mailbox_id", "mail"."list_key" AS "list_key"
    FROM "mailbox_item"
    JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
    JOIN "mail" ON "mail"."id" = "mailbox_item"."mail_id"
    WHERE "mail"."list_key" <> ''
) AS "held"
WHERE NOT EXISTS (
    SELECT 1 FROM "mailbox_subscription"
    WHERE "mailbox_subscription"."mailbox_id" = "held"."mailbox_id"
      AND "mailbox_subscription"."list_key" = "held"."list_key"
);

-- And every item that came from one of those lists now names it.
UPDATE "mailbox_item"
SET "subscription_id" = "mailbox_subscription"."id"
FROM "mailbox_folder", "mail", "mailbox_subscription"
WHERE "mailbox_folder"."id" = "mailbox_item"."folder_id"
  AND "mail"."id" = "mailbox_item"."mail_id"
  AND "mail"."list_key" <> ''
  AND "mailbox_subscription"."mailbox_id" = "mailbox_folder"."mailbox_id"
  AND "mailbox_subscription"."list_key" = "mail"."list_key";

CREATE INDEX IF NOT EXISTS "mailbox_item_subscription" ON "mailbox_item" ("subscription_id");
