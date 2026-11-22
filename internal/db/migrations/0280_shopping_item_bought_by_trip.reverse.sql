DROP INDEX IF EXISTS "shopping_item_bought_shopping_trip";
ALTER TABLE "shopping_item" DROP COLUMN "bought_shopping_trip_id", DROP COLUMN "previous_last_bought_at";
