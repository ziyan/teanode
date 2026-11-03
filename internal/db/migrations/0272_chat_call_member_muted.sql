-- Whether each person on a call has their microphone off, so everybody else
-- sees it beside their name.
ALTER TABLE "chat_call_member" ADD COLUMN "is_muted" boolean NOT NULL DEFAULT false;
