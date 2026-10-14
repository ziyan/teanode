-- How the person files their tax return, which the tax year's estimate
-- works the federal tax out for. Empty means they have not said: married
-- filing jointly when their documents name two people, single otherwise.
ALTER TABLE "agent" ADD COLUMN "tax_filing_status" text NOT NULL DEFAULT ''
    CHECK ("tax_filing_status" IN ('', 'single', 'married_filing_jointly', 'married_filing_separately', 'head_of_household'));
