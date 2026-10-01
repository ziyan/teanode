-- One finance source per agent holds the accounts of imported statements
-- (OFX files mailed in or uploaded). It is made the first time somebody
-- asks for their statement import address, and two requests at once would
-- otherwise make two, each with its own address and its own copy of every
-- account a statement names.
CREATE UNIQUE INDEX "agent_source_one_statement_source" ON "agent_source" ("agent_id")
    WHERE "kind" = 'finance' AND "specification"->>'type' = 'statement';
