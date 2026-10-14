DELETE FROM "agent_finance_paystub_line" WHERE "paystub_line_kind" = 'reimbursement';
ALTER TABLE "agent_finance_paystub_line" DROP CONSTRAINT "agent_finance_paystub_line_kind";
ALTER TABLE "agent_finance_paystub_line" ADD CONSTRAINT "agent_finance_paystub_line_kind"
    CHECK ("paystub_line_kind" IN ('earning', 'tax', 'deduction'));
