-- 1099 forms beside the W-2: interest (1099-INT), dividends (1099-DIV)
-- and broker sales (1099-B). A brokerage sends them together in one
-- statement, so one upload holds at most one form of each kind rather
-- than one form.
ALTER TABLE "agent_finance_tax_form" DROP CONSTRAINT "agent_finance_tax_form_kind";
ALTER TABLE "agent_finance_tax_form" ADD CONSTRAINT "agent_finance_tax_form_kind"
    CHECK ("tax_form_kind" IN ('w2', '1099_int', '1099_div', '1099_b'));
DROP INDEX "agent_finance_tax_form_attachment";
CREATE UNIQUE INDEX "agent_finance_tax_form_attachment" ON "agent_finance_tax_form" ("agent_id", "agent_attachment_id", "tax_form_kind");
