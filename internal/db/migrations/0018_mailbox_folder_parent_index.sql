-- Deleting a folder walks its children by parent; without an index of its
-- own that walk is a scan of every folder on the server.
CREATE INDEX "mailbox_folder_parent" ON "mailbox_folder" ("parent_id") WHERE "parent_id" IS NOT NULL;
