-- A contact's identifier is the file name the client chose for it.
--
-- That is not a free choice this server makes: in CardDAV the client picks
-- the last segment of the URL when it creates a card, and clients disagree
-- about what it should be. iOS and macOS use the card's UID, which is a
-- 36-character UUID, so the 32 characters this column started with -- the
-- width of the identifiers this server generates for itself -- refused every
-- contact either of them ever created, with a 500 and no explanation.
ALTER TABLE "contact" ALTER COLUMN "id" TYPE varchar(255);
