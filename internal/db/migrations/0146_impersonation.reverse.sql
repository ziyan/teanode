-- Who was behind an impersonation is forgotten, and a session that was one
-- becomes an ordinary session of the person's until its hour runs out.
ALTER TABLE "audit_event" DROP COLUMN "impersonator_user_id";
ALTER TABLE "session" DROP COLUMN "impersonator_session_id";
ALTER TABLE "session" DROP COLUMN "impersonator_user_id";
