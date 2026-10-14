DELETE FROM "agent_finance_tax_form" WHERE "tax_form_kind" = '1098';
ALTER TABLE "agent_finance_tax_form" DROP CONSTRAINT "agent_finance_tax_form_kind";
ALTER TABLE "agent_finance_tax_form" ADD CONSTRAINT "agent_finance_tax_form_kind"
    CHECK ("tax_form_kind" IN ('w2', '1099_int', '1099_div', '1099_b'));
DROP TABLE IF EXISTS "agent_tax_year_setting";
