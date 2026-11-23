-- The trip that last bought an item, and when it was bought before that, so a
-- trip finished by mistake can be resumed (its items go back in the cart with
-- the purchase before it as their last), and an item checked off on the list
-- and unchecked again keeps the purchase it had.
ALTER TABLE "shopping_item"
    ADD COLUMN "bought_shopping_trip_id" character varying(32) REFERENCES "shopping_trip" ("id") ON DELETE SET NULL,
    ADD COLUMN "previous_last_bought_at" timestamp with time zone;
CREATE INDEX "shopping_item_bought_shopping_trip" ON "shopping_item" ("bought_shopping_trip_id")
    WHERE "bought_shopping_trip_id" IS NOT NULL;
