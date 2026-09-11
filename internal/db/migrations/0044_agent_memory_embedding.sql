-- A memory's meaning as a vector, so what a turn is about can be found by
-- what it means rather than by the words it happens to use: "the boat" is
-- to find the memory that says "Kittiwake". Kept beside the memory rather
-- than in a table of its own — there is one vector per memory, and it
-- dies with it — as an array of reals, because the database this ships
-- with has no vector type and a person's memories are counted in hundreds.
ALTER TABLE "agent_memory" ADD COLUMN "vector" real[];
ALTER TABLE "agent_memory" ADD COLUMN "vector_model" text NOT NULL DEFAULT '';
