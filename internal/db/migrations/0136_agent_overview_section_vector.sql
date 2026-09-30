-- One vector for each section of a page's overview, so that a question
-- about how a thing relates to others, or what has been happening to it,
-- can find the page by the section that says so, and recall can carry that
-- section rather than the first.
--
-- section_id is the page, the section's place in the overview and a hash
-- of the section's words, so a section the night rewrote is a new row and
-- a vector of the old words can never be taken for the new ones; the
-- embedding pass removes rows whose section is gone.
CREATE TABLE "agent_overview_section_vector" (
    "section_id" character varying(120)   NOT NULL,
    "node_id"    character varying(32)    NOT NULL REFERENCES "agent_node" ("id") ON DELETE CASCADE,
    "agent_id"   character varying(32)    NOT NULL,
    "model"      character varying(200)   NOT NULL,
    "vector"     real[]                   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("section_id", "model")
);
ALTER TABLE "agent_overview_section_vector" ALTER COLUMN "vector" SET STORAGE EXTERNAL;
CREATE INDEX "agent_overview_section_vector_scope" ON "agent_overview_section_vector" ("agent_id", "model");
CREATE INDEX "agent_overview_section_vector_node" ON "agent_overview_section_vector" ("node_id");
