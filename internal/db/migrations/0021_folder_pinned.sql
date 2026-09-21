-- A folder the owner pinned to the top of the rail, beside the Inbox and
-- Starred, and when: the pins are shown in the order they were made. Null
-- for the rest.
ALTER TABLE "mailbox_folder" ADD COLUMN "pinned_at" timestamptz;
