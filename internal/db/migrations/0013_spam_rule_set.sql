-- The public pattern rules the built-in spam filter evaluates.
--
-- In the database rather than in a directory under the data directory,
-- because a server can run as several instances against one database. Three
-- instances each downloading their own copy would drift: the same message
-- would score differently depending on which one received it, and nothing
-- would show that had happened. The object store is not an alternative
-- either, since it is optional and exists only for a cluster.
--
-- "content" is the rule text as it was verified and stored, so that a later
-- version of the parser can re-read it without fetching again, and so that
-- what was checked is what is kept. "rules_loaded" and "rules_skipped" are
-- what the dashboard reports: a published set contains rules this server
-- cannot run, and an operator should be able to see how many.
CREATE TABLE "spam_rule_set" (
    "channel"       varchar(256) NOT NULL,
    "version"       varchar(64)  NOT NULL,
    "content"       bytea        NOT NULL,
    "rules_loaded"  integer      NOT NULL DEFAULT 0,
    "rules_skipped" integer      NOT NULL DEFAULT 0,
    "updated_at"    timestamptz  NOT NULL,
    "error"         text         NOT NULL DEFAULT '',
    PRIMARY KEY ("channel")
);
