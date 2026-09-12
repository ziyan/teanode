-- A contact's identifier is the file name its client chose, so it is only
-- unique inside one address book.
--
-- It was the whole table's primary key, which made it global: one person's
-- client naming a card "contact1" stopped every other person's client from
-- ever using that name, with a refusal that also said whether the name was
-- taken elsewhere. Several CardDAV clients number their files exactly like
-- that, so this was not a remote possibility.
ALTER TABLE "contact" DROP CONSTRAINT "contact_pkey";
ALTER TABLE "contact" ADD PRIMARY KEY ("addressbook_id", "id");
