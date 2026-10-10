-- A phone app can be signed in by scanning a code from somewhere the person is
-- already signed in: the dashboard's Connect page, the command line, or a card
-- their agent shows. The code is a one-time secret for that person, good for
-- ten minutes, and the app that presents it is handed a token as them.
--
-- The secret travels from the person's own signed-in screen to their own
-- phone's camera. That is the point of this direction: in the other one, a
-- device showing a code for the person to approve, anybody can show a code
-- and send the person a link to approve it, and a press of Approve signs the
-- stranger in.

CREATE TABLE "oauth_connect_code" (
    "id" character varying(32) NOT NULL,
    "created_at" timestamp with time zone,

    -- Who the code signs a phone in as.
    "user_id" character varying(32) NOT NULL,

    -- Only the hash, as everywhere else here: a copy of this table is not a
    -- set of codes anybody can sign in with.
    "code_hash" character varying(64) NOT NULL,

    "expires_at" timestamp with time zone NOT NULL,

    PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "idx_oauth_connect_code_code_hash" ON "oauth_connect_code" ("code_hash");
CREATE INDEX "idx_oauth_connect_code_expires_at" ON "oauth_connect_code" ("expires_at");
CREATE INDEX "idx_oauth_connect_code_user_id" ON "oauth_connect_code" ("user_id");
