-- One measurement of the server's storage on one day (UTC), taken by the
-- daily storage job, so the operator's page can draw a trend and fit a line
-- through it. measure_kind says what measure_key names: "database" (one row,
-- key "total"), "databaseArea" (the area of a table, see storageusage), "fileKind"
-- (photo originals, mail messages and so on) or "disk" (a measured disk, whose
-- total and free bytes are in disk_total_bytes and disk_free_bytes). measured_bytes
-- is the size for the first three and the bytes in use for a disk. Nothing
-- references a user: people are measured live, never sampled, so a deleted
-- account leaves nothing behind here. Rows older than 400 days are deleted by
-- the job.
CREATE TABLE "storage_usage_sample" (
    "sampled_on" date NOT NULL,
    "measure_kind" varchar(32) NOT NULL CHECK ("measure_kind" IN ('database', 'databaseArea', 'fileKind', 'disk')),
    "measure_key" varchar(64) NOT NULL,
    "measured_bytes" bigint NOT NULL CHECK ("measured_bytes" >= 0),
    "disk_total_bytes" bigint CHECK ("disk_total_bytes" >= 0),
    "disk_free_bytes" bigint CHECK ("disk_free_bytes" >= 0),
    "sampled_at" timestamptz NOT NULL,
    PRIMARY KEY ("sampled_on", "measure_kind", "measure_key"),
    CHECK (("measure_kind" = 'disk') = ("disk_total_bytes" IS NOT NULL AND "disk_free_bytes" IS NOT NULL))
);

-- The warning most recently sent about a disk: how many days of room were
-- left when it was sent (30 or 7). The job sends a warning only when the
-- disk is closer to full than the one before, and clears the row when the
-- disk is no longer projected to fill within 30 days, so a warning is not
-- repeated every day and is sent again if the disk comes back to it.
CREATE TABLE "storage_disk_warning" (
    "disk_label" varchar(64) PRIMARY KEY,
    "warning_days" integer NOT NULL CHECK ("warning_days" IN (7, 30)),
    "projected_full_on" date NOT NULL,
    "sent_at" timestamptz NOT NULL
);
