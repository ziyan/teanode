-- A contact's identifier is as wide as the client that made it.
--
-- These two columns were sized at 32 to match the identifiers this
-- program generates. A contact's own identifier is 255, because a card
-- synchronized from a phone arrives with the identifier the phone chose
-- -- a UUID of 36 characters, or whatever else a CardDAV client uses.
--
-- So marking your own card as "you" failed for anybody whose address
-- book came from their phone, which is nearly everybody: the write was
-- refused at the column, the refusal poisoned the transaction, and what
-- the dashboard showed was an HTTP 500. The same held for binding a
-- person's page to their card.
ALTER TABLE "user" ALTER COLUMN "contact_id" TYPE character varying(255);
ALTER TABLE "agent_node" ALTER COLUMN "contact_id" TYPE character varying(255);
