-- The kinds of action that would need the person's word which they let the
-- agent take when they are not there to give it: outward, destructive,
-- granting, listed. Empty, the default, allows none of them.
ALTER TABLE "agent" ADD COLUMN "unattended_allowed_risks" jsonb NOT NULL DEFAULT '[]'::jsonb;
