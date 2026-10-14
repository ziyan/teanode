-- State and local income tax paid besides what paychecks withheld (an
-- estimated payment, last year's balance due), which itemizing counts with
-- the withholding up to the cap on state and local taxes.
ALTER TABLE "agent_tax_year_setting" ADD COLUMN "state_income_tax_paid_amount" numeric(14,2) NOT NULL DEFAULT 0
    CHECK ("state_income_tax_paid_amount" >= 0);
