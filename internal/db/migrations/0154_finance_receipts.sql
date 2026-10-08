-- What a person or their agent writes on a finance transaction ("a
-- birthday present"), and who wrote it. Never written by a sync. Empty
-- annotation, empty author: the two are set and cleared together.
ALTER TABLE "agent_finance_transaction"
    ADD COLUMN "annotation" text NOT NULL DEFAULT '',
    ADD COLUMN "annotated_by" character varying(20) NOT NULL DEFAULT '';
-- The checks are added unchecked and then validated, each its own
-- statement (docs/coding/database-migrations.md): adding a check outright
-- scans every row inside the ALTER TABLE, while VALIDATE CONSTRAINT scans
-- under a lock that lets readers and writers through. This file is one
-- transaction, so the ALTER's own lock is held to its end either way; the
-- columns are new, so every row holds the defaults and passes.
ALTER TABLE "agent_finance_transaction"
    ADD CONSTRAINT "agent_finance_transaction_annotated_by"
        CHECK ("annotated_by" IN ('', 'person', 'agent')) NOT VALID,
    ADD CONSTRAINT "agent_finance_transaction_annotation_author"
        CHECK (("annotation" = '') = ("annotated_by" = '')) NOT VALID;
ALTER TABLE "agent_finance_transaction" VALIDATE CONSTRAINT "agent_finance_transaction_annotated_by";
ALTER TABLE "agent_finance_transaction" VALIDATE CONSTRAINT "agent_finance_transaction_annotation_author";

-- One merchant's record of one purchase or order, read out of where it
-- came from: a stored message (the mail row, which outlives the mailbox
-- items that move it from folder to folder), a Gmail message the agent read
-- through the Gmail skill, or a photo or PDF uploaded as an agent
-- attachment. Exactly the column its kind names is set. A message's
-- receipt has no foreign key to the message: the receipt and what was read
-- out of it are kept when the message is deleted.
CREATE TABLE "agent_finance_receipt" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"                character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,

    "receipt_source_kind"     character varying(20)    NOT NULL,
    "mail_id"                 character varying(32),
    "gmail_message_id"        text,
    "agent_attachment_id"     character varying(32)    REFERENCES "agent_attachment" ("id") ON DELETE CASCADE,

    "merchant_name"           text                     NOT NULL DEFAULT '',
    "merchant_receipt_number" text                     NOT NULL DEFAULT '',
    "purchased_on"            date,
    "purchased_at"            timestamp with time zone,
    "currency_code"           text                     NOT NULL,

    -- As printed. A receipt that prints no subtotal has none.
    "subtotal_amount"         numeric(19,4),
    "total_amount"            numeric(19,4)            NOT NULL,

    -- The last digits of the card or account the receipt says paid.
    "payment_account_mask"    text                     NOT NULL DEFAULT '',

    -- Whether the lines add up to the printed totals, and by how much they
    -- miss when they do not.
    "receipt_check_state"     character varying(20)    NOT NULL,
    "check_difference_amount" numeric(19,4)            NOT NULL DEFAULT 0,

    "created_at"              timestamp with time zone NOT NULL,
    "modified_at"             timestamp with time zone NOT NULL,

    CONSTRAINT "agent_finance_receipt_source_kind"
        CHECK ("receipt_source_kind" IN ('mail', 'gmail_message', 'attachment')),
    CONSTRAINT "agent_finance_receipt_source"
        CHECK ((("receipt_source_kind" = 'mail') = ("mail_id" IS NOT NULL))
           AND (("receipt_source_kind" = 'gmail_message') = ("gmail_message_id" IS NOT NULL))
           AND (("receipt_source_kind" = 'attachment') = ("agent_attachment_id" IS NOT NULL))),
    CONSTRAINT "agent_finance_receipt_check_state"
        CHECK ("receipt_check_state" IN ('balanced', 'unbalanced'))
);
-- Reading the same source again replaces its receipt.
CREATE UNIQUE INDEX "agent_finance_receipt_mail" ON "agent_finance_receipt" ("agent_id", "mail_id")
    WHERE "mail_id" IS NOT NULL;
CREATE UNIQUE INDEX "agent_finance_receipt_gmail_message" ON "agent_finance_receipt" ("agent_id", "gmail_message_id")
    WHERE "gmail_message_id" IS NOT NULL;
CREATE UNIQUE INDEX "agent_finance_receipt_attachment" ON "agent_finance_receipt" ("agent_id", "agent_attachment_id")
    WHERE "agent_attachment_id" IS NOT NULL;
-- In the order the receipt list reads them, the undated after every dated
-- one: a plain DESC would put the nulls first and not serve that order.
CREATE INDEX "agent_finance_receipt_purchased" ON "agent_finance_receipt" ("agent_id", "purchased_on" DESC NULLS LAST, "id" DESC);

-- One printed line of a receipt, as printed: an item, a discount (negative),
-- a tax, a fee or a tip. Tax is never spread into the items here.
CREATE TABLE "agent_finance_receipt_line" (
    "id"                   character varying(32) NOT NULL PRIMARY KEY,
    "agent_id"             character varying(32) NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "receipt_id"           character varying(32) NOT NULL REFERENCES "agent_finance_receipt" ("id") ON DELETE CASCADE,
    "line_number"          integer               NOT NULL,
    "receipt_line_kind"    character varying(20) NOT NULL,
    "description"          text                  NOT NULL DEFAULT '',
    "quantity"             numeric(24,8),
    "quantity_unit"        text                  NOT NULL DEFAULT '',
    "unit_price_amount"    numeric(19,4),
    "line_amount"          numeric(19,4)         NOT NULL,

    -- The receipt's own mark: on an item or a discount the tax it falls
    -- under, on a tax line the mark it covers. Empty when it says none.
    "tax_class_code"       text                  NOT NULL DEFAULT '',

    -- A discount's item.
    "discounted_line_id"   character varying(32) REFERENCES "agent_finance_receipt_line" ("id") ON DELETE SET NULL,

    -- Reserved for splitting a charge across spending categories by its
    -- receipt; nothing writes it yet.
    "spending_category_id" character varying(32) REFERENCES "agent_spending_category" ("id") ON DELETE SET NULL,

    CONSTRAINT "agent_finance_receipt_line_kind"
        CHECK ("receipt_line_kind" IN ('item', 'discount', 'tax', 'fee', 'tip'))
);
CREATE UNIQUE INDEX "agent_finance_receipt_line_number" ON "agent_finance_receipt_line" ("receipt_id", "line_number");
-- Deleting a line, or a spending category, looks up the lines that name
-- it to set them null. Partial: few lines are discounts, and none has a
-- spending category yet.
CREATE INDEX "agent_finance_receipt_line_discounted" ON "agent_finance_receipt_line" ("discounted_line_id")
    WHERE "discounted_line_id" IS NOT NULL;
CREATE INDEX "agent_finance_receipt_line_spending_category" ON "agent_finance_receipt_line" ("spending_category_id")
    WHERE "spending_category_id" IS NOT NULL;

-- Which charges a receipt explains, and how much of each: one order can
-- become several charges (split shipments), and one charge can cover
-- several orders. A match the person made is never replaced by the
-- matcher.
CREATE TABLE "agent_finance_receipt_match" (
    "receipt_id"             character varying(32)    NOT NULL REFERENCES "agent_finance_receipt" ("id") ON DELETE CASCADE,
    "finance_transaction_id" character varying(32)    NOT NULL REFERENCES "agent_finance_transaction" ("id") ON DELETE CASCADE,
    "agent_id"               character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "matched_amount"         numeric(19,4)            NOT NULL,
    "receipt_match_source"   character varying(20)    NOT NULL,
    "match_confidence"       numeric(5,4),
    "created_at"             timestamp with time zone NOT NULL,
    PRIMARY KEY ("receipt_id", "finance_transaction_id"),
    CONSTRAINT "agent_finance_receipt_match_amount" CHECK ("matched_amount" > 0),
    CONSTRAINT "agent_finance_receipt_match_source"
        CHECK ("receipt_match_source" IN ('receipt_matcher', 'person')),
    CONSTRAINT "agent_finance_receipt_match_confidence"
        CHECK ("match_confidence" IS NULL OR ("match_confidence" >= 0 AND "match_confidence" <= 1))
);
CREATE INDEX "agent_finance_receipt_match_transaction" ON "agent_finance_receipt_match" ("finance_transaction_id");

-- The finance transaction a photo or a text file was uploaded to as its
-- receipt, for the receipt job to match what it reads to that charge.
ALTER TABLE "agent_attachment"
    ADD COLUMN "finance_transaction_id" character varying(32)
        REFERENCES "agent_finance_transaction" ("id") ON DELETE SET NULL;
-- Partial: almost no attachment is a receipt upload, and deleting a
-- finance transaction (a sync replacing a pending charge) looks its
-- uploads up by this column to set it null.
CREATE INDEX "agent_attachment_finance_transaction" ON "agent_attachment" ("finance_transaction_id")
    WHERE "finance_transaction_id" IS NOT NULL;
