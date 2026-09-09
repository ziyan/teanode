-- Remembering that a reader chose to load a message's pictures.
--
-- Remote pictures are not loaded by default because loading one tells the
-- sender that the message was opened, and from roughly where. That is a
-- decision worth asking about once — but only once: a reader who said yes has
-- already told the sender, and asking again every time they reopen the
-- message protects nobody and reads as the program not paying attention.
--
-- On the item rather than the message, because the item is what one mailbox
-- holds: two people who received the same message decide separately.
ALTER TABLE "mailbox_item" ADD COLUMN IF NOT EXISTS "images_at" timestamp with time zone;

-- And the same answer given once for a whole list. A newsletter is pictures
-- with a few words around them; a reader who trusts one sender enough to load
-- them every time should be able to say so in one place rather than on every
-- issue.
ALTER TABLE "mailbox_subscription" ADD COLUMN IF NOT EXISTS "images_at" timestamp with time zone;
