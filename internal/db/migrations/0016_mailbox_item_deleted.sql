-- IMAP's \Deleted: a message marked for removal that EXPUNGE takes. The web
-- UI moves to Trash instead and never sets this, but a mail program that
-- deletes the classic way — flag, then expunge — must find the flag here.
ALTER TABLE "mailbox_item" ADD COLUMN "deleted" boolean NOT NULL DEFAULT false;
