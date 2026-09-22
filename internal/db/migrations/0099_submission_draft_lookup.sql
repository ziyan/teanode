CREATE INDEX "mail_submission_draft" ON "mail_submission" ("owner_id", "mailbox_id", "draft_item_id", "accepted_at", "submission_id") WHERE "draft_item_id" <> '';
