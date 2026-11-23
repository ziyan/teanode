-- Coding sessions' questions stand alone again.
UPDATE "agent_feed_item" SET "feed_stack_key" = '', "feed_stack_title" = '' WHERE "feed_source_kind" = 'coding_session';
