-- The agent's feed: cards the agent posts without being asked, newest first,
-- in place of turns in the person's main conversation. A card is a headline,
-- a few lines, sometimes a short table of facts, and sometimes choices the
-- person answers with a tap.
--
-- feed_stack_key names the stack a card belongs to, empty for a card that
-- stands alone; stack_position orders the cards of a stack that has an order
-- (a brief's summary first), and feed_stack_title names the stack.
-- feed_card_layout is how the card is drawn, and layout_content the
-- structured content of a layout other than text. coalesce_key lets a later post about the same thing replace the card
-- instead of piling up. interaction_id is the durable question behind a
-- decision, so that a late answer is recorded once. chosen_conversation_id
-- is the main conversation a chosen action carried on in, and
-- is_outcome_pending says the turn it started has not ended yet.
CREATE TABLE "agent_feed_item" (
    "id" character varying(32) NOT NULL PRIMARY KEY,
    "agent_id" character varying(32) NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "user_id" character varying(32) NOT NULL,
    "feed_item_kind" text NOT NULL,
    "feed_source_kind" text NOT NULL,
    "feed_source_id" text NOT NULL DEFAULT '',
    "run_conversation_id" text NOT NULL DEFAULT '',
    "title" text NOT NULL DEFAULT '',
    "body" text NOT NULL DEFAULT '',
    "picture_symbol" text NOT NULL DEFAULT '',
    "fact_rows" jsonb NOT NULL DEFAULT '[]',
    "source_links" jsonb NOT NULL DEFAULT '[]',
    "record_references" jsonb NOT NULL DEFAULT '[]',
    "coalesce_key" text,
    "choices" jsonb NOT NULL DEFAULT '[]',
    "interaction_id" text,
    "chosen_choice_key" text,
    "chosen_at" timestamp with time zone,
    "chosen_conversation_id" text NOT NULL DEFAULT '',
    "outcome_text" text NOT NULL DEFAULT '',
    "is_outcome_pending" boolean NOT NULL DEFAULT false,
    "feed_urgency" text NOT NULL DEFAULT 'today',
    "feed_stack_key" text NOT NULL DEFAULT '',
    "stack_position" integer,
    "feed_stack_title" text NOT NULL DEFAULT '',
    "feed_card_layout" text NOT NULL DEFAULT 'text',
    "layout_content" jsonb,
    "is_liked" boolean NOT NULL DEFAULT false,
    "read_at" timestamp with time zone,
    "dismissed_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    "expires_at" timestamp with time zone
);

-- The list: a person's cards that are not dismissed, newest first.
CREATE INDEX idx_agent_feed_item_user_list ON "agent_feed_item" USING btree (user_id, dismissed_at, created_at DESC);

-- Cards of one source pile into a stack, shown as one entry: the cards of a
-- stack are found by its key, and a card that stands alone has none.
CREATE INDEX idx_agent_feed_item_stack ON "agent_feed_item" USING btree (user_id, feed_stack_key, created_at DESC) WHERE feed_stack_key <> '';

-- A post with the key of an open card replaces it, so the key is unique among
-- an agent's cards that are not dismissed.
CREATE UNIQUE INDEX idx_agent_feed_item_coalesce ON "agent_feed_item" USING btree (agent_id, coalesce_key)
    WHERE coalesce_key IS NOT NULL AND dismissed_at IS NULL;

-- The cards a run posted, for telling whether a scheduled run posted any.
CREATE INDEX idx_agent_feed_item_run ON "agent_feed_item" USING btree (run_conversation_id) WHERE run_conversation_id <> '';

-- Where an agent's unasked words go: its feed, or the main conversation as
-- they did before the feed.
ALTER TABLE "agent" ADD COLUMN "feed_destination" text NOT NULL DEFAULT 'feed';

-- A schedule may tell the person once per run, quietly, that its cards are
-- ready, though the cards themselves are not urgent.
ALTER TABLE "agent_schedule" ADD COLUMN "is_notified_when_posted" boolean NOT NULL DEFAULT false;
