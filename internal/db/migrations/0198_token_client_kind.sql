-- Which of a person's tokens are the phone app's own sign-in.
--
-- Approving a call that spends the owner's money, and lending that money, are
-- done where a person taps, and the server has to tell the tap from a program
-- that holds a command line token. The dashboard's cookie session is one kind
-- of tap; this column marks the other, the phone app. It is 'app' on a token
-- made by the app's sign-in (a connect code, or the app's own consent) and
-- NULL on every other token, which is every token that existed before.
--
-- The two rows a sign-in passes through carry the same word, so that it is
-- decided where the sign-in was started: a connect code or an approval made
-- with a command line token could have been made by a program running on the
-- person's computer, and it yields a token with no marker.
ALTER TABLE "token" ADD COLUMN "client_kind" character varying(16)
    CHECK ("client_kind" IN ('app'));
ALTER TABLE "oauth_authorization" ADD COLUMN "client_kind" character varying(16)
    CHECK ("client_kind" IN ('app'));
ALTER TABLE "oauth_connect_code" ADD COLUMN "client_kind" character varying(16)
    CHECK ("client_kind" IN ('app'));

-- Tokens the app already holds were issued before there was anything to
-- check, and a token with a client and no resource can only have come from the
-- app's two sign-ins (every other program is given the agent tools endpoint as
-- its resource). They are marked, so the people who signed in their phone
-- before this do not have to do it again to approve a call.
UPDATE "token" SET "client_kind" = 'app'
WHERE "client_id" IS NOT NULL AND COALESCE("resource", '') = '';
