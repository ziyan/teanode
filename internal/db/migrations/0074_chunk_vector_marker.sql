-- Which model's vector a passage already has, on the passage itself.
--
-- Finding "the passages with no vector yet" was a LEFT JOIN against the
-- vector table with an IS NULL test. PostgreSQL answers that with a hash
-- anti-join over both tables in full: on a real corpus -- 474,000
-- passages, 422,000 of them already done -- it touched ninety thousand
-- buffers and took a quarter of a second to return a hundred rows, and
-- it ran once per batch of a hundred. The first ingest of a person's
-- machines slowed to a crawl exactly as it got large, which is the
-- moment it must not.
--
-- With the model written on the passage, the same question is an index
-- lookup that stops after a hundred rows, whatever the size of what is
-- already done. Empty means no vector; a change of embedding model makes
-- every row differ from the one configured, which is how a re-embedding
-- catches up by itself.
ALTER TABLE "agent_chunk" ADD COLUMN "vector_model" character varying(200) NOT NULL DEFAULT '';

UPDATE "agent_chunk" c SET "vector_model" = v."model"
FROM "agent_chunk_vector" v WHERE v."chunk_id" = c."id";

CREATE INDEX "agent_chunk_vector_model" ON "agent_chunk" ("agent_id", "vector_model");
