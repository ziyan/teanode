-- A person's own way into a connected server the operator declared: their
-- credential, or the tokens an authorization gave, sealed with the server
-- secret. One row per person per server. The server itself is in the
-- configuration; this is only the person's part.
CREATE TABLE "agent_mcp_connection" (
    "id"                character varying(32)    NOT NULL PRIMARY KEY,
    "created_at"        timestamp with time zone NOT NULL,
    "modified_at"       timestamp with time zone NOT NULL,
    "agent_id"          character varying(32)    NOT NULL,
    "server_name"       character varying(200)   NOT NULL,
    "status"            character varying(16)    NOT NULL,
    "credential"        text                     NOT NULL DEFAULT '',
    "tokens"            text                     NOT NULL DEFAULT '',
    "pending"           text                     NOT NULL DEFAULT '',
    "last_error"        text                     NOT NULL DEFAULT '',
    "last_connected_at" timestamp with time zone,
    UNIQUE ("agent_id", "server_name")
);
