# Paystubs and W-2s, read and checked, for the tax year

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the ExecPlan rules the author keeps in their own notes: self-contained, prose first, every milestone verifiable on its own. It builds on docs/planning/finance-receipts-execplan.md (checked in), whose shape it copies: a document is uploaded, a background job has a model read it into rows, the server checks the rows add up, and the dashboard, the command line and the agent's finance tool all reach it through the same GraphQL operations.


## Purpose / Big Picture

Filing a tax return starts with a pile of paper: a paystub every two weeks and a W-2 from every employer in January. Today TeaNode knows the money that reached the bank but nothing of what was earned before it got there: gross pay, the taxes withheld, the 401(k), the HSA. After this change a person can, on the dashboard, from the command line, or by asking their agent:

1. Upload a paystub or a W-2, as a PDF or a photo, or read one out of an email in their TeaNode mailbox. A background job reads it into rows: a paystub's earnings, taxes (with the wages each tax was figured on) and deductions, this period and year to date; a W-2's boxes. The server checks that a paystub adds up (gross less taxes less deductions is take-home pay) and that a W-2 agrees with itself (Social Security tax is 6.2% of Social Security wages, Medicare tax is 1.45% of Medicare wages plus 0.9% above 200,000).
2. See a tax year on a Taxes tab under Finance: per person and employer, wages, federal and state income tax withheld, Social Security and Medicare, 401(k), HSA and dependent care, taken from the W-2 when there is one and from the latest paystub's year-to-date figures when there is not, with a projection to December 31 from the pay cadence. When both exist, each W-2 box is set beside the paystubs' figure for it and any difference is shown. Paystubs that do not chain (the previous paystub's year to date plus this period should be this paystub's year to date) and gaps in the pay dates are listed, so a missing paystub is noticed in October, not April. Social Security withheld beyond the year's maximum across several employers, which comes back as a credit on the return, is pointed out.

To see it working: upload two consecutive paystubs and the year's W-2 on the Taxes tab, wait for the reading job, and see both paystubs "Adds up", the chain unbroken, the W-2 "Agrees with itself", and the year view show the W-2's figures with every box matching the last paystub.

Estimating the tax owed or the refund, and 1099 forms (interest, dividends, brokerage), are not part of this plan. The tax form table is general (a form kind and its boxes) so 1099s need no migration later.


## Progress

- [x] Milestone 1 (2026-10-13): the server reads PDF text (poppler in the image, text at upload, scanned PDFs as pictures). `ReadAttachmentText(ctx, storage, attachment)` reads a stored attachment's PDF text on demand; the receipt job reads a scanned PDF as up to three pictures.
- [x] Milestone 2 (2026-10-13): migration 0181 (four tables and `agent_attachment.is_tax_document`), models in internal/models/finance_tax.go, checks and the year summary in internal/finance (paystub.go, tax_form.go, tax_year.go, taxpayer_number.go, tax_test.go), database layer internal/db/database_finance_tax.go with tests, the sweep and conversation deletion keep tax uploads. Note: the year summary landed here rather than in Milestone 4, since it is pure; Milestone 4 only exposes it.
- [ ] Milestone 3: reading job `read_tax_document`, agent operations, GraphQL, client, CLI and finance tool, with tests.
- [ ] Milestone 4: the tax year (summary, reconciliation, chain and gaps, projection, excess Social Security) on every surface, with tests.
- [ ] Milestone 5: dashboard Taxes tab, upload, document dialog, mailbox "Read as paystub or W-2", with web tests.
- [ ] Milestone 6: docs, visual audit, review, pull request, deploy, and the owner's documents imported on the server and checked.


## Surprises & Discoveries

- Paystubs from at least one payroll service print, for every tax, the wages it was figured on, this period and year to date. Those are exactly the W-2's wage boxes: federal wages are box 1, Social Security wages box 3, Medicare wages box 5, state wages box 16. Federal wages there are gross pay less the 401(k), HSA and dependent care deductions; Social Security and Medicare wages are gross less only the HSA and dependent care (a traditional 401(k) is still taxed for Social Security and Medicare). So the year's last paystub reconciles to the W-2 box by box with no tax rules on our side.
- Consecutive paystubs chain to the cent: the earlier one's year-to-date tax plus the later one's tax this period is the later one's year-to-date tax.
- Social Security stops at the year's wage base and the paystub shows it: a year-to-date Social Security wage of exactly the base and a tax this period of zero. The 2026 base is 184,500; the 2025 base is 176,100.
- Additional Medicare tax (0.9% above 200,000 of Medicare wages in the year, withheld by each employer regardless of filing status) may be printed as its own tax line ("Medicare Additional") whose wages are only the part above 200,000; on the W-2 it is inside box 6.
- A mail client sent each paystub as `application/octet-stream` named just ".pdf". A PDF is recognized by its first bytes, `%PDF-`.
- A W-2 PDF holds several copies of the form (B, C, 2, sometimes 2 again), and one payroll service's text layer prints the employee's Social Security number as nine digits with no dashes. The text a model is shown has every such number masked to its last four digits first, and the text is not stored for tax documents.
- The server could not read PDF text before Milestone 1 (receipts asked for a photo of a PDF instead).


## Decision Log

- Decision: an upload handed in as a tax document is marked (`agent_attachment.is_tax_document`): created with the mark, its text is never stored; marked later (a file sent in a conversation, then read as a paystub), its stored text is cleared. The sweep of unclaimed uploads skips marked ones, and deleting a conversation detaches them like receipt files. General uploads still keep a PDF's text unmasked, since a nine-digit order number on a receipt would be lost to masking. Date: 2026-10-13.
- Decision: every tax document is an agent attachment (an upload). A message in the person's mailbox is read by copying its PDF or picture attachments into agent attachments first, each then read on its own. Rationale: one source kind, one unique key, the original file is always there to open beside the rows, and the reading job has one path. Receipts needed three source kinds because a receipt is often the message's own text; a paystub or W-2 is a file. Date: 2026-10-13.
- Decision: one job and one operation read either kind, `read_tax_document` / `ReadTaxDocument`; the model says which kind it is (`paystub`, `w2`, or `none` with why). Rationale: the person uploads "a tax document" and should not have to say which; the job records into the paystub or the tax form tables accordingly. Date: 2026-10-13.
- Decision: a paystub is stored line by line exactly as printed, with a line kind (`earning`, `tax`, `deduction`), the amounts this period and year to date, for a tax the wages it was figured on (this period and year to date) and its kind (`federal_income`, `social_security`, `medicare`, `medicare_additional`, `state_income`, `local_income`, `state_disability`, `other`) and jurisdiction (a state code or locality name), and for a deduction its kind (`retirement_traditional`, `retirement_roth`, `hsa`, `health_premium`, `fsa_health`, `fsa_dependent_care`, `commuter`, `insurance`, `other`) and the employer's part beside the employee's. Lines with no amounts (hours only, such as "Worked Hours 72.00") are not stored. Rationale: the kinds are what the W-2 reconciliation and the year totals need; storing as printed keeps the add-up check exact. Date: 2026-10-13.
- Decision: a tax form is a kind (`w2` for now), a tax year, payer and recipient, and its boxes as rows (`box_code`, `box_qualifier`, `amount`, `text_value`, `is_checked`). Box 12's letter, box 14's label, the state code of boxes 15 to 17 and the locality of boxes 18 to 20 are the qualifier; box 13's three checkboxes are box 13 rows with qualifiers `statutory_employee`, `retirement_plan`, `third_party_sick_pay`. Rationale: 1099s later are other kinds with other boxes, no migration. The valid boxes of each kind are a catalog in internal/finance/tax_form.go. Date: 2026-10-13.
- Decision: only the last four digits of a taxpayer number (Social Security number) are ever stored: `employee_tax_id_mask` on a paystub and `recipient_tax_id_mask` on a tax form, refused unless exactly four digits. Every text field written is refused when it holds a nine-digit run or a number shaped like a Social Security number. The employer's identification number (EIN) is a business's and is kept as printed. Rationale: the person's number is the one thing in these documents that does harm when it leaks, and nothing TeaNode does needs more than its last four. Date: 2026-10-13.
- Decision: a paystub's check is stored (`paystub_check_state` `balanced`/`unbalanced`, `check_difference_amount`, like receipts, so lists can filter); a W-2's checks and the year's reconciliation are worked out each time they are read, from rules in internal/finance (so a corrected rule applies to forms already stored). Date: 2026-10-13.
- Decision: rounding tolerance for a W-2's own checks is one dollar: payroll rounds each paycheck's tax to the cent, so 26 paychecks can drift a few cents from the rate applied to the year's total. A W-2 box against the paystubs' figure has no tolerance: the W-2 is the sum of what the paystubs withheld. Date: 2026-10-13.
- Decision: the Social Security wage base by year is a table in internal/finance (2023 160,200; 2024 168,600; 2025 176,100; 2026 184,500). A year not in it skips the checks that need it and says so, rather than guessing. Date: 2026-10-13.
- Decision: a person and an employer are told apart by name, NFKC, upper case, punctuation and "INC", "CORP", "LLC", "CO", "LTD", "CORPORATION", "COMPANY" dropped, and by the last four digits of the EIN when both documents print one. A household's two earners are two people in the year view; the totals are the household's. Date: 2026-10-13.
- Decision: matching a paystub's take-home pay to the bank deposit is left for later. The year view's gaps in pay dates already show a missing paystub. Date: 2026-10-13.
- Decision: no preview operation for recording (receipts have `PreviewReceipt` behind `record-receipt --dry-run`); a paystub or W-2 recorded by mistake is deleted. If the parity tests require a preview pairing, follow what they require and record it here. Date: 2026-10-13.


## Outcomes & Retrospective

(To be written at each milestone.)


## Context and Orientation

TeaNode is one Go server (cmd/teanode-server) and one command line (cmd/teanode) with a React dashboard (web/). A person has an agent (models.Agent); everything financial is the agent's and every table carries `agent_id`. Read AGENTS.md and CONTRIBUTING.md first; naming rules are strict (no abbreviations, `self` receivers, `ID`/`Id` casing, booleans start with is/has/does/can/should, enum-valued names carry the enum's name).

The finance feature lives in these places, and receipts (the closest relative) are the model to copy:

- internal/db/migrations/: SQL files applied in name order by internal/db/database_migrate.go, each with a `.reverse.sql`. Receipts are 0154 and 0155. This plan adds 0181 (the next free number on main when written; renumber if main moves). docs/coding/database-migrations.md says how to write one.
- internal/models/finance.go: the structs (`FinanceReceipt` and its lines). Amounts are decimal strings. The `graphapi:"nullable"` tag marks a nullable GraphQL field. Enums are string types with `IsValid()`.
- internal/db/database_finance_receipt.go: the `FinanceReceiptOperation` interface (embedded in the big interface in internal/db/db.go), GORM row structs with `toModel()`, writes through `self.applyMutation(models.AuditResource..., ...)` (the resource names are in internal/models/audit.go). Ids come from `newID()`.
- internal/finance/receipt.go and decimal.go: pure checks; `parseDecimal` into `big.Rat`, `FormatAmount` writes four places for storage, `CurrencyMinorUnits`.
- internal/agent/receipt_job.go: the background job `read_receipt`. `QueueReceiptReading` enqueues `models.AgentJobReadReceipt` with the source id as the job subject (one id, varchar(32)); the handler is registered in internal/agent/agent.go; it renders a prompt from internal/agent/prompts/read_receipt.txt, fences untrusted text, calls `self.thinkAbout(...)` with `config.AgentWorkScan`, parses JSON with `readModelAnswer`, records, and retitles the run. A job kind needs a label `agent.runKinds.<kind>` in web/src/i18n/en.ts, ja.ts and zh.ts (a test enforces it) and an entry in `RUN_KINDS` in web/src/agentRuns.ts.
- internal/agent/finance_receipt.go: the agent's operations (`RecordReceipt` and friends), which the GraphQL layer calls.
- internal/api/v1api/apigraph/agent_finance.go: the `FinanceQuery` and `FinanceMutation` interfaces; a method added there becomes a GraphQL field (the schema is built by reflection; comments become field docs through internal/util/commentparse, regenerated with `go generate ./internal/util/commentparse`). agent_finance_receipt.go holds the receipt handlers: `requireAgentPerson`, `requireFinanceOffered`, `agentWorker()`, ownership checks, then the worker call.
- internal/client/finance.go: GraphQL documents (`Document...` constants in the `FinanceDocuments` map) used by the CLI and the tool through `RunFinance`.
- internal/cmd/finance_receipt.go: `teanode finance` subcommands; each maps to an operation in `financeSubcommandOperations` in internal/cmd/finance.go. Parity tests require the CLI name to be the operation's name in kebab case and the tool's in snake case, a leading `Finance` dropped.
- internal/agent/tools/finance/finance.go: the agent's finance tool, an `operations` map of name to GraphQL operation, risk, arguments and preview, plus the long description.
- Uploads: `POST /api/v1/agent/attachments` (internal/api/v1api/apigraph/agent_attachment.go) stores an `agent_attachment` row and the bytes in storage under the attachment id. The orphan sweeper (`ListOrphanAgentAttachments` in internal/db/database_attachment.go) deletes attachments nothing points at; it must skip ones a paystub or tax form points at, and so must `DetachReceiptAttachments`' equivalent when a conversation is deleted.
- web/src/pages/financePage.tsx: the Finance tabs (`FINANCE_SECTIONS`); web/src/pages/finance/financeReceiptList.tsx and financeReceipts.tsx are the receipt pages; financeApi.ts the documents; web/src/pages/mailboxReadReceipt.tsx the mailbox menu item.
- docs/subsystems/finance.md: the finance subsystem's documentation.

Terms. A paystub (pay statement) is the employer's record of one paycheck: gross pay (earnings), taxes withheld, deductions, and take-home (net) pay, each this period and year to date (YTD). A W-2 is the employer's annual statement of wages and taxes; its numbered boxes are: 1 wages for federal income tax, 2 federal income tax withheld, 3 Social Security wages, 4 Social Security tax withheld, 5 Medicare wages, 6 Medicare tax withheld (including additional Medicare), 7 Social Security tips, 8 allocated tips, 10 dependent care benefits, 11 nonqualified plans, 12 coded amounts (D a traditional 401(k), AA a Roth 401(k), W HSA contributions by employer and employee, DD the cost of employer health coverage, C taxable group-term life insurance, and others), 13 checkboxes, 14 other, 15 state and the employer's state id, 16 state wages, 17 state income tax, 18 local wages, 19 local income tax, 20 locality. The Social Security wage base is the most wages Social Security tax applies to in a year.


## Plan of Work

Milestone 1 (helper session): poppler's `pdftotext` and `pdftoppm` are copied into the production image by the `media-readers` stage of deploy/Dockerfile, as ffmpeg is. internal/agent gains a PDF reader (`readPDFText`, `renderPDFPages`, `isPDFContent`) and an on-demand reader for a stored attachment's text. The upload endpoint reads a PDF's text into `agent_attachment.text` and corrects a generic content type to application/pdf. The receipt job reads a text PDF as text and a scanned PDF as pictures. Acceptance: a PDF receipt uploaded to a charge is read.

Milestone 2: migration 0181_finance_tax_documents creates `agent_finance_paystub`, `agent_finance_paystub_line`, `agent_finance_tax_form`, `agent_finance_tax_form_entry`, each with `agent_id`, and a source `agent_attachment_id` (unique per agent, ON DELETE CASCADE) plus an optional `mail_id` naming the message it was copied from. Models `FinancePaystub`, `FinancePaystubLine`, `FinanceTaxForm`, `FinanceTaxFormEntry` with enums `PaystubLineKind`, `PaystubTaxKind`, `PaystubDeductionKind`, `PaystubCheckState`, `TaxFormKind`. internal/finance/paystub.go: `CheckPaystub` (normalize and refuse impossible input, then balanced when earnings add to gross, taxes and deductions to their printed totals when printed, and gross less taxes less employee deductions is net). internal/finance/tax_form.go: the box catalog, `CheckTaxForm` (refuses unknown boxes and malformed qualifiers) and `TaxFormFindings` (the self-checks of a W-2). internal/finance/taxpayer_number.go: `MaskTaxpayerNumbers(text)` and `HasTaxpayerNumber(text)`. internal/db/database_finance_tax.go: `FinanceTaxOperation` with put (replace by attachment), get, list (by year and by pay date range), delete, for both kinds, audited. The attachment sweeper and the conversation deletion keep their files.

Milestone 3: job kind `read_tax_document` (internal/agent/tax_document_job.go, prompt internal/agent/prompts/read_tax_document.txt). The text is read on demand from storage and masked; pictures for photos and scanned PDFs. Agent operations in internal/agent/finance_tax.go: `RecordPaystub`, `RecordTaxForm`, `DeletePaystub`, `DeleteTaxForm`, `QueueTaxDocumentReading`, and for a mailbox message, copying its PDF and picture attachments into agent attachments and queueing each. GraphQL: queries `Paystubs`, `Paystub`, `TaxForms`, `TaxForm`; mutations `RecordPaystub`, `RecordTaxForm`, `DeletePaystub`, `DeleteTaxForm`, `ReadTaxDocument` (`agentAttachmentId` or `mailboxItemId`). Client documents, CLI subcommands (`paystubs`, `paystub`, `record-paystub`, `delete-paystub`, `tax-forms`, `tax-form`, `record-tax-form`, `delete-tax-form`, `read-tax-document <file>|--mailbox-item`), finance tool operations in snake case.

Milestone 4: internal/finance/tax_year.go `SummarizeTaxYear(year, paystubs, taxForms, today)` returns per person and employer the figures, their source (W-2 or paystubs through a day), the projection to December 31 when there is no W-2 and the year is not over, the W-2 against paystubs comparison when both exist and the paystubs reach the year's last pay date, broken chains and gaps, and household totals with excess Social Security. GraphQL query `TaxYear(taxYear)`, CLI `tax-year`, tool `tax_year`.

Milestone 5: a Taxes tab (`/finance/taxes?year=2026`): the year's summary, the comparison, the findings, the W-2s and the paystubs with their checks, an "Add paystubs or W-2s" upload taking several files, a dialog per document showing its rows and the file, delete. Mailbox message menu: "Read as paystub or W-2". Labels in en, ja, zh.

Milestone 6: docs/subsystems/finance.md section, docs/reference/command-line.md, a visual audit (desktop and phone, light and dark), review, a draft pull request, deploy, then the owner's paystubs and W-2s read on the server and the year view checked against the numbers worked out by hand.


## Concrete Steps

From the repository root:

    go build ./... && go vet ./internal/finance/ ./internal/agent/... ./internal/db/
    go test ./internal/finance/
    # database tests need PostgreSQL with pgvector (docs/reference/local-development.md)
    go test ./internal/db/ -run 'Tax|Paystub'
    go test ./internal/agent/ -run 'Tax|Paystub'
    go test ./internal/api/v1api/apigraph/ -run 'Tax|Paystub'
    go test ./internal/cmd/ ./internal/agent/tools/finance/
    cd web && npx vitest run src/pages/finance && npx tsc --noEmit

Expected after Milestone 3, against a development server with the local profile:

    $ TEANODE_PROFILE=local teanode finance read-tax-document stub.pdf
    queued reading stub.pdf
    $ TEANODE_PROFILE=local teanode finance paystubs
    PAY DATE    EMPLOYER          GROSS      TAXES     DEDUCTIONS  NET       CHECK
    2026-04-03  Example Payroll   4,000.00   880.00    400.00      2,720.00  adds up


## Validation and Acceptance

Unit tests in internal/finance cover: a paystub that adds up and one that misses by a cent (state and difference); refusals (negative gross, unknown kinds, a nine-digit number in a description, a last-four that is not four digits); a W-2 whose Social Security and Medicare agree and one whose Medicare does not; the wage base check; the year summary with two chained paystubs and a W-2 that matches, a broken chain, a gap, a projection, and two employers whose Social Security together exceeds the year's maximum. All examples are invented (an invented employer, invented amounts that add up).

Database tests: put replaces by attachment; list by year; delete; the migration forward, reverse and forward again; the sweeper keeps a paystub's attachment.

Agent tests: the job records a paystub and a W-2 from a canned model answer, masks a nine-digit run in the text it sends, refuses an answer carrying a full taxpayer number, and answers `none` without recording.

End to end on the server after deploy: the owner's three paystubs and two W-2s are read; each paystub adds up; the two later paystubs chain; the W-2s agree with themselves; the 2026 year view projects to December 31.


## Idempotence and Recovery

Reading the same attachment again replaces its paystub or tax form (unique per agent and attachment), so a job retried or a document read twice leaves one record. The migration only creates tables; its reverse drops them. Deleting a paystub or tax form deletes its uploaded file as receipts do.


## Artifacts and Notes

The arithmetic of an invented paystub used by the tests:

    gross 4,000.00 = Regular 4,000.00
    taxes 880.00 = Federal 500.00 + Social Security 229.40 + Medicare 53.65 + State 96.95
    deductions 400.00 = 401(k) 300.00 + HSA 100.00
    net 2,720.00 = 4,000.00 - 880.00 - 400.00
    federal wages 3,600.00 = 4,000.00 - 300.00 - 100.00
    Social Security and Medicare wages 3,700.00 = 4,000.00 - 100.00
    229.40 = 6.2% of 3,700.00; 53.65 = 1.45% of 3,700.00


## Interfaces and Dependencies

No new Go modules. poppler-utils (Debian package) in the image for Milestone 1. In internal/finance:

    func CheckPaystub(paystub *models.FinancePaystub) (models.PaystubCheckState, string, error)
    func CheckTaxForm(taxForm *models.FinanceTaxForm) error
    func TaxFormFindings(taxForm *models.FinanceTaxForm) []TaxFormFinding
    func SummarizeTaxYear(taxYear int, paystubs []*models.FinancePaystub, taxForms []*models.FinanceTaxForm, today time.Time) *TaxYearSummary
    func MaskTaxpayerNumbers(text string) string
    func HasTaxpayerNumber(text string) bool
    func SocialSecurityWageBase(taxYear int) (string, bool)
