-- Documents by what they say, for finding the other copies of something
-- the night has read. The same page copied from a template, the same file
-- in two checkouts, and one item read again under a new name after its
-- source changed type all carry the same hash, and each was read and
-- filed once more under its own name.
CREATE INDEX "agent_document_content" ON "agent_document" ("agent_id", "hash") WHERE "hash" <> '';
