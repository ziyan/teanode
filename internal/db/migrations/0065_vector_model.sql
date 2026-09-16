-- What width each embedding model writes, so the server can build one
-- vector index per model without guessing the dimension. Written the first
-- time a vector of a model is stored; read at start to build the indexes.
--
-- A model's name here carries its width when the width was asked for,
-- "openai:text-embedding-3-small@512", because two widths of one model are
-- two vector spaces and must never be ranked against each other.
CREATE TABLE "vector_model" (
    "model"      character varying(200)   NOT NULL PRIMARY KEY,
    "dimension"  integer                  NOT NULL,
    "created_at" timestamp with time zone NOT NULL
);

-- A vector is an array of floats that does not compress. Left to itself
-- PostgreSQL would try, fail, and store it out of line anyway, having spent
-- the CPU; EXTERNAL says store it out of line and do not try.
ALTER TABLE "mail_embedding" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
