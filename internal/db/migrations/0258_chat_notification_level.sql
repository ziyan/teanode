-- What a member of a chat is told of on their phone, in place of a mute
-- until a time: every message ('all'), only one that mentions them by name or
-- mentions everybody ('mentions'), or nothing ('off'). It stays until they
-- change it.
ALTER TABLE "chat_member" ADD COLUMN "notification_level" character varying(16) NOT NULL DEFAULT 'all'
    CHECK ("notification_level" IN ('all', 'mentions', 'off'));

-- A mute still running silenced every message, so it carries over as 'off';
-- one that has ended is 'all', as it already was.
UPDATE "chat_member" SET "notification_level" = 'off' WHERE "muted_until" > now();

ALTER TABLE "chat_member" DROP COLUMN "muted_until";

-- Who a message mentions: the members by their account, and whether it
-- mentions everybody in the chat. Stored when the message is said, so what
-- it mentions does not depend on the words or the language they are in.
ALTER TABLE "chat_message" ADD COLUMN "mentioned_user_ids" jsonb NOT NULL DEFAULT '[]';
ALTER TABLE "chat_message" ADD COLUMN "is_all_mentioned" boolean NOT NULL DEFAULT false;
