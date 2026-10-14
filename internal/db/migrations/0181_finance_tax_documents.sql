-- One paycheck's pay statement, read out of an uploaded PDF or photo: what
-- was earned, the taxes withheld and the deductions, this period and year
-- to date, stored line by line as printed. A message's paystub is copied
-- into an upload before it is read, so every one has its file; mail_id
-- names the message it was copied from, with no foreign key, so the
-- paystub outlives the message. Reading the same upload again replaces it.
CREATE TABLE "agent_finance_paystub" (
    "id"                      character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"                character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "agent_attachment_id"     character varying(32)    NOT NULL REFERENCES "agent_attachment" ("id") ON DELETE CASCADE,
    "mail_id"                 character varying(32),

    "employer_name"           text                     NOT NULL DEFAULT '',
    -- The employer's identification number as printed: a business's, not
    -- a person's.
    "employer_tax_id"         text                     NOT NULL DEFAULT '',
    "employee_name"           text                     NOT NULL DEFAULT '',
    -- Never more than the last four digits of the person's number.
    "employee_tax_id_mask"   character varying(4)     NOT NULL DEFAULT '',

    "pay_date"                date                     NOT NULL,
    "period_start_on"         date,
    "period_end_on"           date,
    "currency_code"           text                     NOT NULL,

    -- As printed. A total the paystub does not print is null.
    "gross_amount"            numeric(19,4)            NOT NULL,
    "year_to_date_gross_amount" numeric(19,4),
    "total_tax_amount"        numeric(19,4),
    "total_deduction_amount"  numeric(19,4),
    "net_amount"              numeric(19,4)            NOT NULL,

    -- Whether the lines add up to the printed totals, and by how much they
    -- miss when they do not.
    "paystub_check_state"     character varying(20)    NOT NULL,
    "check_difference_amount" numeric(19,4)            NOT NULL DEFAULT 0,

    "created_at"              timestamp with time zone NOT NULL,
    "modified_at"             timestamp with time zone NOT NULL,

    CONSTRAINT "agent_finance_paystub_check_state"
        CHECK ("paystub_check_state" IN ('balanced', 'unbalanced')),
    CONSTRAINT "agent_finance_paystub_tax_id_mask"
        CHECK ("employee_tax_id_mask" ~ '^([0-9]{4})?$')
);
CREATE UNIQUE INDEX "agent_finance_paystub_attachment" ON "agent_finance_paystub" ("agent_id", "agent_attachment_id");
CREATE INDEX "agent_finance_paystub_pay_date" ON "agent_finance_paystub" ("agent_id", "pay_date" DESC, "id" DESC);

-- One printed line of a paystub with an amount: an earning, a tax (with
-- the wages it was figured on) or a deduction (with the employer's part
-- beside the employee's). Lines of hours only are not kept.
CREATE TABLE "agent_finance_paystub_line" (
    "id"                              character varying(32) NOT NULL PRIMARY KEY,
    "agent_id"                        character varying(32) NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "paystub_id"                      character varying(32) NOT NULL REFERENCES "agent_finance_paystub" ("id") ON DELETE CASCADE,
    "line_number"                     integer               NOT NULL,
    "paystub_line_kind"               character varying(20) NOT NULL,
    "description"                     text                  NOT NULL DEFAULT '',

    "hours"                           numeric(24,8),
    "rate_amount"                     numeric(19,4),
    -- An earning's or a tax's amount, a deduction's employee part.
    "current_amount"                  numeric(19,4),
    "year_to_date_amount"             numeric(19,4),

    "paystub_tax_kind"                character varying(30) NOT NULL DEFAULT '',
    -- A state code ("CA") or a locality's name, for a state or local tax.
    "tax_jurisdiction_code"           text                  NOT NULL DEFAULT '',
    "current_wages_amount"            numeric(19,4),
    "year_to_date_wages_amount"       numeric(19,4),

    "paystub_deduction_kind"          character varying(30) NOT NULL DEFAULT '',
    "employer_current_amount"         numeric(19,4),
    "employer_year_to_date_amount"    numeric(19,4),

    CONSTRAINT "agent_finance_paystub_line_kind"
        CHECK ("paystub_line_kind" IN ('earning', 'tax', 'deduction')),
    CONSTRAINT "agent_finance_paystub_line_tax_kind"
        CHECK (("paystub_line_kind" = 'tax') = ("paystub_tax_kind" <> '')),
    CONSTRAINT "agent_finance_paystub_line_deduction_kind"
        CHECK (("paystub_line_kind" = 'deduction') = ("paystub_deduction_kind" <> ''))
);
CREATE UNIQUE INDEX "agent_finance_paystub_line_number" ON "agent_finance_paystub_line" ("paystub_id", "line_number");

-- One tax form for one tax year (a W-2 for now; 1099s are other kinds
-- with other boxes), read out of an uploaded PDF or photo. Its boxes are
-- rows of agent_finance_tax_form_entry. Reading the same upload again
-- replaces it.
CREATE TABLE "agent_finance_tax_form" (
    "id"                     character varying(32)    NOT NULL PRIMARY KEY,
    "agent_id"               character varying(32)    NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "agent_attachment_id"    character varying(32)    NOT NULL REFERENCES "agent_attachment" ("id") ON DELETE CASCADE,
    "mail_id"                character varying(32),

    "tax_form_kind"          character varying(20)    NOT NULL,
    "tax_year"               integer                  NOT NULL,
    -- A corrected form (a W-2c) replaces the figures of the one it
    -- corrects.
    "is_corrected"           boolean                  NOT NULL DEFAULT false,

    "payer_name"             text                     NOT NULL DEFAULT '',
    "payer_tax_id"           text                     NOT NULL DEFAULT '',
    "recipient_name"         text                     NOT NULL DEFAULT '',
    "recipient_tax_id_mask" character varying(4)     NOT NULL DEFAULT '',

    "created_at"             timestamp with time zone NOT NULL,
    "modified_at"            timestamp with time zone NOT NULL,

    CONSTRAINT "agent_finance_tax_form_kind" CHECK ("tax_form_kind" IN ('w2')),
    CONSTRAINT "agent_finance_tax_form_year" CHECK ("tax_year" BETWEEN 1900 AND 2999),
    CONSTRAINT "agent_finance_tax_form_tax_id_mask"
        CHECK ("recipient_tax_id_mask" ~ '^([0-9]{4})?$')
);
CREATE UNIQUE INDEX "agent_finance_tax_form_attachment" ON "agent_finance_tax_form" ("agent_id", "agent_attachment_id");
CREATE INDEX "agent_finance_tax_form_year" ON "agent_finance_tax_form" ("agent_id", "tax_year" DESC, "id" DESC);

-- One box of a tax form: its number ("1", "12"), what tells boxes of one
-- number apart (box 12's letter, a state's code, a locality, box 14's
-- label, box 13's checkbox), and its amount, text or mark.
CREATE TABLE "agent_finance_tax_form_entry" (
    "id"            character varying(32) NOT NULL PRIMARY KEY,
    "agent_id"      character varying(32) NOT NULL REFERENCES "agent" ("id") ON DELETE CASCADE,
    "tax_form_id"   character varying(32) NOT NULL REFERENCES "agent_finance_tax_form" ("id") ON DELETE CASCADE,
    "entry_number"  integer               NOT NULL,
    "box_code"      character varying(8)  NOT NULL,
    "box_qualifier" text                  NOT NULL DEFAULT '',
    "amount"        numeric(19,4),
    "text_value"    text                  NOT NULL DEFAULT '',
    "is_checked"    boolean               NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX "agent_finance_tax_form_entry_number" ON "agent_finance_tax_form_entry" ("tax_form_id", "entry_number");
CREATE UNIQUE INDEX "agent_finance_tax_form_entry_box" ON "agent_finance_tax_form_entry" ("tax_form_id", "box_code", "box_qualifier");

-- An upload handed in as a paystub or a tax form: kept from the sweep of
-- unclaimed uploads while it waits to be read, and its text, which holds
-- the person's taxpayer number, is not kept on the row.
ALTER TABLE "agent_attachment" ADD COLUMN "is_tax_document" boolean NOT NULL DEFAULT false;
