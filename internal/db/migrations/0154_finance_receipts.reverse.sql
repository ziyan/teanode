-- Receipts, their lines and their matches go, and so does every
-- annotation and the charge an upload was for, with their checks and
-- index, newest first.
DROP INDEX IF EXISTS "agent_attachment_finance_transaction";
ALTER TABLE "agent_attachment" DROP COLUMN IF EXISTS "finance_transaction_id";
DROP TABLE IF EXISTS "agent_finance_receipt_match";
DROP INDEX IF EXISTS "agent_finance_receipt_line_spending_category";
DROP INDEX IF EXISTS "agent_finance_receipt_line_discounted";
DROP TABLE IF EXISTS "agent_finance_receipt_line";
DROP TABLE IF EXISTS "agent_finance_receipt";
ALTER TABLE "agent_finance_transaction"
    DROP CONSTRAINT IF EXISTS "agent_finance_transaction_annotation_author",
    DROP CONSTRAINT IF EXISTS "agent_finance_transaction_annotated_by",
    DROP COLUMN IF EXISTS "annotation",
    DROP COLUMN IF EXISTS "annotated_by";
