-- What a tax year's estimate needs that no paystub or form says: children
-- for the child tax credit, the dependent care paid for, other household
-- income (a spouse's pay that no W-2 here shows), charitable gifts and
-- property tax for itemizing, and the gains realized so far in a year
-- whose 1099-Bs have not come, which the agent reads from a brokerage.
CREATE TABLE "agent_tax_year_setting" (
    "agent_id"                         character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "tax_year"                         integer                  NOT NULL,
    "child_count"                      integer                  NOT NULL DEFAULT 0,
    "other_dependent_count"            integer                  NOT NULL DEFAULT 0,
    "dependent_care_expense_amount"    numeric(14,2)            NOT NULL DEFAULT 0,
    "other_income_amount"              numeric(14,2)            NOT NULL DEFAULT 0,
    "charitable_contribution_amount"   numeric(14,2)            NOT NULL DEFAULT 0,
    "property_tax_amount"              numeric(14,2)            NOT NULL DEFAULT 0,
    "estimated_short_term_gain_amount" numeric(14,2),
    "estimated_long_term_gain_amount"  numeric(14,2),
    "estimated_gain_note"              text                     NOT NULL DEFAULT '',
    "modified_at"                      timestamp with time zone NOT NULL,
    PRIMARY KEY ("agent_id", "tax_year"),
    CONSTRAINT "agent_tax_year_setting_year" CHECK ("tax_year" BETWEEN 1900 AND 2999),
    CONSTRAINT "agent_tax_year_setting_counts" CHECK ("child_count" BETWEEN 0 AND 20 AND "other_dependent_count" BETWEEN 0 AND 20),
    CONSTRAINT "agent_tax_year_setting_amounts" CHECK ("dependent_care_expense_amount" >= 0 AND "charitable_contribution_amount" >= 0 AND "property_tax_amount" >= 0)
);

-- Form 1098, mortgage interest, which itemizing counts.
ALTER TABLE "agent_finance_tax_form" DROP CONSTRAINT "agent_finance_tax_form_kind";
ALTER TABLE "agent_finance_tax_form" ADD CONSTRAINT "agent_finance_tax_form_kind"
    CHECK ("tax_form_kind" IN ('w2', '1099_int', '1099_div', '1099_b', '1098'));
