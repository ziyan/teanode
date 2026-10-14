-- A paystub line may be a reimbursement: money paid back to the employee
-- (expenses, a phone stipend paid as a refund) that is not pay, so it is
-- outside gross pay and its taxes and reaches take-home pay on its own.
ALTER TABLE "agent_finance_paystub_line" DROP CONSTRAINT "agent_finance_paystub_line_kind";
ALTER TABLE "agent_finance_paystub_line" ADD CONSTRAINT "agent_finance_paystub_line_kind"
    CHECK ("paystub_line_kind" IN ('earning', 'tax', 'deduction', 'reimbursement'));
