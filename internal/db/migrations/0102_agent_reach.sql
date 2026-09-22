-- A reach: the computer a person's skill or connected server makes its
-- requests through. Through this server is no row; a row names one of the
-- person's attached computers by the name it attaches under. A service that
-- answers only inside one network -- a code host behind a company VPN, a box
-- on the home network -- goes through the computer that is on that network.
--
-- By name rather than by a row of the computer: an attached computer is a
-- connection that comes and goes, not something stored, and the name is what
-- the person gives it with `teanode computer start --name`.
CREATE TABLE "agent_reach" (
    "agent_id"      character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "kind"          character varying(16)    NOT NULL,
    "name"          character varying(128)   NOT NULL,
    "computer_name" character varying(128)   NOT NULL,
    "modified_at"   timestamp with time zone NOT NULL,
    PRIMARY KEY ("agent_id", "kind", "name"),
    CHECK ("kind" IN ('skill', 'server')),
    CHECK ("name" <> '' AND "computer_name" <> '')
);
