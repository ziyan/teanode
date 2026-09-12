-- Fails rather than silently dropping contacts when two books share a name.
ALTER TABLE "contact" DROP CONSTRAINT "contact_pkey";
ALTER TABLE "contact" ADD PRIMARY KEY ("id");
