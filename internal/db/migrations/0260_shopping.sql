-- Shopping lists: a person's named lists of things to buy, shared with a
-- person or a circle, checked off at the store, by a receipt, and remembered
-- with their prices. docs/subsystems/shopping.md says how they are used.

-- A list belongs to one person, who alone shares it or deletes it.
CREATE TABLE "shopping_list" (
    "id"         character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"    character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "list_name"  character varying(200)   NOT NULL CHECK ("list_name" <> ''),
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);
CREATE INDEX "shopping_list_user" ON "shopping_list" ("user_id", "created_at");

-- One row gives one person, or every member of one circle, the list: to add,
-- change and check off its items. There are no levels.
--
-- Sharing with a person needs the owner and that person to be in a circle
-- together, and sharing with a circle needs the owner to be a member of it.
-- That is checked when the share is made. What keeps it true afterwards is the
-- join on circle_member in the query that says who reaches a list, and the
-- code that takes a person out of a circle, which deletes the direct shares
-- between people who no longer share one.
CREATE TABLE "shopping_list_share" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "shopping_list_id"   character varying(32)    NOT NULL REFERENCES "shopping_list" ("id") ON DELETE CASCADE,
    "grantee_user_id"    character varying(32) REFERENCES "user" ("id") ON DELETE CASCADE,
    "grantee_circle_id"  character varying(32) REFERENCES "circle" ("id") ON DELETE CASCADE,
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "created_at"         timestamp with time zone NOT NULL,
    CHECK (num_nonnulls("grantee_user_id", "grantee_circle_id") = 1),
    UNIQUE ("shopping_list_id", "grantee_user_id"),
    UNIQUE ("shopping_list_id", "grantee_circle_id")
);
CREATE INDEX "shopping_list_share_grantee_user" ON "shopping_list_share" ("grantee_user_id") WHERE "grantee_user_id" IS NOT NULL;
CREATE INDEX "shopping_list_share_grantee_circle" ON "shopping_list_share" ("grantee_circle_id") WHERE "grantee_circle_id" IS NOT NULL;

-- A person who does not want to be reminded of a list when they arrive at a
-- store. Everybody the list reaches is reminded until they say so here.
CREATE TABLE "shopping_list_reminder_mute" (
    "shopping_list_id" character varying(32)    NOT NULL REFERENCES "shopping_list" ("id") ON DELETE CASCADE,
    "user_id"          character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "created_at"       timestamp with time zone NOT NULL,
    PRIMARY KEY ("shopping_list_id", "user_id")
);

-- A store a person asked never to be reminded at. store_key is the store's
-- name in lower case with only its letters and digits, the form every store
-- is compared in, and store_name is how it was written.
CREATE TABLE "shopping_store_mute" (
    "user_id"    character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "store_key"  character varying(200)   NOT NULL CHECK ("store_key" <> ''),
    "store_name" character varying(200)   NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    PRIMARY KEY ("user_id", "store_key")
);

-- One thing on one list.
--
-- store_name is the store to get it from, empty for any store, and store_key
-- its compared form. is_store_brand says only that store sells it. aisle_name
-- is where it is in the store, and aisle_rank the order the aisles were given
-- in, so a list imported in the store's order is shown in it; item_rank orders
-- the items inside an aisle.
--
-- needed_quantity is how many to buy next time, with quantity_unit (each, lb,
-- pack, or empty). shopping_item_state is needed (to buy), in_cart (checked
-- off at the store, until the trip ends or a receipt matches it) or
-- not_needed (bought, or not wanted now). An item stays on its list when it is
-- bought, so the list is reused.
--
-- unit_price_amount is the latest price seen, the one shopping_item_price
-- keeps the history of. out_of_stock_at is set while a store had none, and
-- cleared when the item is checked off or needed again.
--
-- The picture's bytes are in the file store under picture_file_id, scaled
-- down; picture_source is web (found by the agent on picture_page_url) or
-- upload. picture_searched_at is when a search was last made, found or not.
CREATE TABLE "shopping_item" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "shopping_list_id"        character varying(32)    NOT NULL REFERENCES "shopping_list" ("id") ON DELETE CASCADE,
    "item_description"        character varying(500)   NOT NULL CHECK ("item_description" <> ''),
    "item_details"            text                     NOT NULL DEFAULT '',
    "store_name"              character varying(200)   NOT NULL DEFAULT '',
    "store_key"               character varying(200)   NOT NULL DEFAULT '',
    "is_store_brand"          boolean                  NOT NULL DEFAULT false,
    "aisle_name"              character varying(200)   NOT NULL DEFAULT '',
    "aisle_rank"              integer                  NOT NULL DEFAULT 0,
    "item_rank"               integer                  NOT NULL DEFAULT 0,
    "needed_quantity"         numeric(12, 3)           NOT NULL DEFAULT 1 CHECK ("needed_quantity" > 0),
    "quantity_unit"           character varying(32)    NOT NULL DEFAULT '',
    "unit_price_amount"       numeric(24, 8),
    "currency_code"           character varying(3)     NOT NULL DEFAULT '',
    "shopping_item_state"     character varying(16)    NOT NULL DEFAULT 'needed'
        CHECK ("shopping_item_state" IN ('needed', 'in_cart', 'not_needed')),
    "carted_by_user_id"       character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "carted_at"               timestamp with time zone,
    "out_of_stock_at"         timestamp with time zone,
    "out_of_stock_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "out_of_stock_store_name" character varying(200)   NOT NULL DEFAULT '',
    "last_bought_at"          timestamp with time zone,
    "picture_file_id"         character varying(128)   NOT NULL DEFAULT '',
    "picture_page_url"        text                     NOT NULL DEFAULT '',
    "picture_source"          character varying(16)    NOT NULL DEFAULT ''
        CHECK ("picture_source" IN ('', 'web', 'upload')),
    "picture_searched_at"     timestamp with time zone,
    "created_by_user_id"      character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "created_at"              timestamp with time zone NOT NULL,
    "updated_at"              timestamp with time zone NOT NULL,
    CHECK (("store_name" = '') = ("store_key" = '')),
    CHECK (("shopping_item_state" = 'in_cart') = ("carted_at" IS NOT NULL)),
    CHECK (("picture_file_id" = '') = ("picture_source" = ''))
);
CREATE INDEX "shopping_item_list" ON "shopping_item" ("shopping_list_id", "shopping_item_state", "aisle_rank", "item_rank");
-- The store screen and the arrival reminder start from the store.
CREATE INDEX "shopping_item_store" ON "shopping_item" ("store_key", "shopping_item_state");

-- One price seen for an item at a store: from a receipt (finance_receipt_id,
-- which is cleared if the receipt is deleted) or typed by a person.
CREATE TABLE "shopping_item_price" (
    "id"                 character varying(32)    NOT NULL PRIMARY KEY,
    "shopping_item_id"   character varying(32)    NOT NULL REFERENCES "shopping_item" ("id") ON DELETE CASCADE,
    "store_name"         character varying(200)   NOT NULL DEFAULT '',
    "store_key"          character varying(200)   NOT NULL DEFAULT '',
    "unit_price_amount"  numeric(24, 8)           NOT NULL CHECK ("unit_price_amount" >= 0),
    "currency_code"      character varying(3)     NOT NULL DEFAULT '',
    "quantity"           numeric(12, 3),
    "quantity_unit"      character varying(32)    NOT NULL DEFAULT '',
    "observed_on"        date                     NOT NULL,
    "price_source"       character varying(16)    NOT NULL CHECK ("price_source" IN ('receipt', 'person')),
    "finance_receipt_id" character varying(32) REFERENCES "agent_finance_receipt" ("id") ON DELETE SET NULL,
    "created_by_user_id" character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "created_at"         timestamp with time zone NOT NULL,
    CHECK (("store_name" = '') = ("store_key" = ''))
);
CREATE INDEX "shopping_item_price_item" ON "shopping_item_price" ("shopping_item_id", "observed_on" DESC, "created_at" DESC);

-- A line of a receipt tied to an item, by the line's number on the receipt and
-- its words (receipt_line_description): reading a receipt again replaces its
-- lines (new rows, the same numbers), and what was decided about them stays,
-- while a reading that numbered the lines differently matches nothing. applied: the item was checked off and its
-- price written (shopping_item_price_id); what it was before is kept, so the
-- receipt can be undone. proposed: the matcher was unsure and left it to the
-- person. dismissed: undone or refused, and never proposed again.
CREATE TABLE "shopping_receipt_match" (
    "id"                                   character varying(32)    NOT NULL PRIMARY KEY,
    "finance_receipt_id"                   character varying(32)    NOT NULL REFERENCES "agent_finance_receipt" ("id") ON DELETE CASCADE,
    "receipt_line_number"                  integer                  NOT NULL CHECK ("receipt_line_number" > 0),
    "receipt_line_description"             text                     NOT NULL DEFAULT '',
    "shopping_item_id"                     character varying(32)    NOT NULL REFERENCES "shopping_item" ("id") ON DELETE CASCADE,
    "receipt_match_state"                  character varying(16)    NOT NULL
        CHECK ("receipt_match_state" IN ('applied', 'proposed', 'dismissed')),
    "matched_quantity"                     numeric(12, 3),
    "previous_shopping_item_state"         character varying(16)    NOT NULL DEFAULT ''
        CHECK ("previous_shopping_item_state" IN ('', 'needed', 'in_cart', 'not_needed')),
    "previous_needed_quantity"             numeric(12, 3),
    "previous_last_bought_at"              timestamp with time zone,
    "previous_out_of_stock_at"             timestamp with time zone,
    "previous_out_of_stock_by_user_id"     character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "previous_out_of_stock_store_name"     character varying(200)   NOT NULL DEFAULT '',
    "shopping_item_price_id"               character varying(32) REFERENCES "shopping_item_price" ("id") ON DELETE SET NULL,
    "decided_by_user_id"                   character varying(32) REFERENCES "user" ("id") ON DELETE SET NULL,
    "created_at"                           timestamp with time zone NOT NULL,
    "decided_at"                           timestamp with time zone,
    UNIQUE ("finance_receipt_id", "receipt_line_number", "shopping_item_id")
);
CREATE INDEX "shopping_receipt_match_item" ON "shopping_receipt_match" ("shopping_item_id");

-- Somebody shopping a store now, for some lists. finished_at is set by Done,
-- or when a new trip on one of its lists starts long after it.
CREATE TABLE "shopping_trip" (
    "id"          character varying(32)    NOT NULL PRIMARY KEY,
    "user_id"     character varying(32)    NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
    "store_name"  character varying(200)   NOT NULL DEFAULT '',
    "store_key"   character varying(200)   NOT NULL DEFAULT '',
    "started_at"  timestamp with time zone NOT NULL,
    "finished_at" timestamp with time zone,
    CHECK (("store_name" = '') = ("store_key" = ''))
);
CREATE INDEX "shopping_trip_open" ON "shopping_trip" ("user_id", "started_at") WHERE "finished_at" IS NULL;

CREATE TABLE "shopping_trip_list" (
    "shopping_trip_id" character varying(32) NOT NULL REFERENCES "shopping_trip" ("id") ON DELETE CASCADE,
    "shopping_list_id" character varying(32) NOT NULL REFERENCES "shopping_list" ("id") ON DELETE CASCADE,
    PRIMARY KEY ("shopping_trip_id", "shopping_list_id")
);
CREATE INDEX "shopping_trip_list_list" ON "shopping_trip_list" ("shopping_list_id");

-- Shopping lists are on for everybody who has the ordinary roles; an operator
-- can take shopping:use away from a role in the usual place.
INSERT INTO "role_permission" ("role_id", "permission_key")
SELECT "id", 'shopping:use' FROM "role" WHERE lower("name") IN ('administrator', 'operator', 'member')
ON CONFLICT DO NOTHING;
