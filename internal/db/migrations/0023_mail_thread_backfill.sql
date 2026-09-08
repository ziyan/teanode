-- Only mail that arrived from outside was given a conversation. A message
-- that was sent, a draft, a bounce, an auto-reply, and a message a mail
-- program appended over IMAP were all stored with none.
--
-- An empty thread id is not "no conversation", it is a conversation whose id
-- is the empty string — so every message that had none grouped together, and
-- a folder would show one row standing for hundreds of unrelated messages.
-- Each of them begins its own conversation, which is what a message that
-- answers nothing is.
UPDATE "mail" SET "thread_id" = "id" WHERE "thread_id" = '';
