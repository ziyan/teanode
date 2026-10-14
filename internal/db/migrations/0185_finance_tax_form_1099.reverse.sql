DELETE FROM "agent_finance_tax_form" WHERE "tax_form_kind" <> 'w2';
DROP INDEX "agent_finance_tax_form_attachment";
CREATE UNIQUE INDEX "agent_finance_tax_form_attachment" ON "agent_finance_tax_form" ("agent_id", "agent_attachment_id");
ALTER TABLE "agent_finance_tax_form" DROP CONSTRAINT "agent_finance_tax_form_kind";
ALTER TABLE "agent_finance_tax_form" ADD CONSTRAINT "agent_finance_tax_form_kind" CHECK ("tax_form_kind" IN ('w2'));
