-- The pictures and videos of a card: the person's own photos and videos by
-- their photo id, and the pictures of web pages, which the server fetches,
-- scales down and keeps in the file store under image_file_id. An entry with
-- a list_row_index is the thumbnail of that row of a list card.
ALTER TABLE "agent_feed_item" ADD COLUMN "feed_media" jsonb NOT NULL DEFAULT '[]';
