-- An operator may sign in as another person. The session that
-- does it belongs to the person and names the operator, and the operator's
-- own session, which ending it must still be alive for; and every audit
-- event written meanwhile names the operator beside the person.
ALTER TABLE "session" ADD COLUMN "impersonator_user_id" character varying(32);
ALTER TABLE "session" ADD COLUMN "impersonator_session_id" character varying(32);
ALTER TABLE "audit_event" ADD COLUMN "impersonator_user_id" character varying(32);
