# A person sets budgets and savings targets, and the agent tells them when they are off track

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It builds on `docs/planning/bank-accounts-execplan.md`
(transactions, synced into `agent_bank_transaction`) and, for savings targets
measured by what accounts hold, on `docs/planning/net-worth-execplan.md`
(assets and their valuations over time). What it needs from each is
repeated under Context and Orientation.

## Purpose / Big Picture

Transactions alone answer "what did I spend". A person trying to save wants
more: a monthly budget per category, a way to see whether this month is on
track while it is still happening, a word from the agent when it is not,
and help deciding what the budgets and the savings target should be.

After this change:

- Every transaction has a **category** from the person's own list
  (groceries, dining, rent, transport, and whatever they add), assigned by
  the person's rules first, then by the provider's category, then by the
  agent, and always correctable by the person. Transfers between the
  person's own accounts, and card payments, are recognized and left out of
  spending.
- The person sets a **monthly budget** per category, by hand or by asking
  the agent to propose one from the last three months.
- The person sets **savings targets**: an amount by a date, measured either
  by money not spent (income minus spending each month) or by what chosen
  accounts hold (from the net worth plan).
- A **Spending** tab on the agent page shows this month's spending day by
  day against last month's, each category against its budget, twelve
  months of spending and income, and each target's progress.
- The agent **alerts** the person when a category crosses most of its
  budget, when the month's pace means it will overshoot, and when a target
  falls behind. The alerts use the alert path the agent already has for
  mail, with its daily limit, quiet night hours and mutes.
- The agent's `finance` tool gains operations to read status, set budgets
  and targets,
  categorize transactions and write rules, and it offers a short review on
  the first of each month.

How to see it working: with the sandbox bank linked (bank accounts plan),
ask the agent "suggest a monthly budget from my last three months"; accept
it; open the Spending tab and see the categories against their budgets and
this month against last month; add a transaction in the sandbox (Plaid's
`/sandbox/transactions/create`) large enough to push a category past its
budget; after the next sync, an alert arrives in the agent's conversation.

Terms. A **category** is a label in the person's list. A **rule** assigns
a category to transactions that match it. A **budget** is an amount for one
category for each month from a given month on. **Spending** is money out
that is not a transfer; a refund in the same category reduces it.
**Income** is money in that is not a transfer.

## Progress

- [x] (2026-09-29) Wrote this plan and its decision record.
- [ ] Milestone 1: categories, the category on each transaction, and
  transfer recognition.
- [ ] Milestone 2: rules, and the agent's categorization job.
- [ ] Milestone 3: budgets and the spending queries.
- [ ] Milestone 4: savings targets.
- [ ] Milestone 5: budget operations on the `finance` tool, and the monthly
  review.
- [ ] Milestone 6: alerts through the existing alert path.
- [ ] Milestone 7: the Spending tab and documentation.

## Surprises & Discoveries

- Observation: none yet. Two to measure in Milestone 2: what share of a
  typical month's transactions the default mapping from Plaid categories
  gets right before any rule exists, and how many transactions a month
  reach the agent's categorization job (which costs a model call per
  batch).
  Evidence: to be filled.

## Decision Log

- Decision: what a person saves toward is a **savings target**, not a
  goal, and it is a table of its own rather than an agent goal.
  Rationale: TeaNode already has goals: a sentence on a conversation
  that the agent works toward, turn by turn, until it says the goal is
  met (`docs/subsystems/jobs-and-schedules.md`). A savings target is a
  number and a date that lasts months, measured by queries, not by agent
  turns. Reusing the word would make "clear my goal" ambiguous; reusing
  the mechanism would spend a model turn to learn what a query knows.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: category rules are a table of their own.
  Rationale: the only rules TeaNode has are mailbox rules
  (`models.MailboxRule`), whose conditions are mail headers and whose
  actions move and flag messages. Nothing in them fits a merchant name or
  an amount. Everything else here reuses what exists: schedules for the
  monthly review, the alert path and its mutes for alerts, the job queue
  for categorization, the model purposes already configured, and the
  dashboard's chart components.
  Date/Author: 2026-09-29.

- Decision: the category that counts is the person's, from their own list.
  The provider's category is kept as a hint and never shown as the answer
  when the person's disagrees. Assignment order: the person's own choice,
  then a rule, then the provider's category through a mapping, then the
  agent.
  Rationale: providers disagree with each other and with the person (a
  warehouse store is groceries to one household and home goods to another),
  and SimpleFIN gives no category at all. Budgets mean nothing if the
  categories under them shift when the provider changes its mind. Recorded
  as `docs/decisions/20260929-spending-categories-are-the-persons.md`.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: a correction by the person is never overwritten, by a rule, the
  provider or the agent. Correcting a transaction offers to make a rule for
  that merchant, and a new rule is applied to past transactions except ones
  the person set by hand.
  Rationale: the fastest way to lose trust in a budget is to fix a category
  and see it come back wrong after the next sync.
  Date/Author: 2026-09-29.

- Decision: transfers are marked on the transaction (`is_transfer`) and
  left out of spending and income. They are recognized by the provider's
  transfer and loan payment categories, and by pairing an amount out of one
  of the person's accounts with the same amount into another within three
  days. The person can mark or unmark one by hand.
  Rationale: paying a credit card from checking is not spending; the
  purchases on the card already were. Counting both doubles the month.
  Date/Author: 2026-09-29.

- Decision: whether a month is on track, and when to alert, is computed in
  code from the numbers. The model only words the alert and decides, with
  the person's memory in hand, whether it is worth saying now.
  Rationale: the same reason mail alerts keep their bounds in code
  (`docs/decisions/20260929-the-agent-tells-the-person-what-their-mail-says.md`):
  a threshold cannot be argued with, and the numbers must be right. The
  model is good at "you have a trip booked next week, so dining will run
  over; that is expected" and bad at adding.
  Date/Author: 2026-09-29.

- Decision: budget and target alerts enter the existing alert path as a new
  kind of candidate, rather than a second alert system.
  Rationale: the daily limit, the quiet night hours, "nothing twice in a
  week", the mutes, and delivery to the linked chat app already exist and
  are what a person expects of anything the agent says unasked. A second
  path would get them subtly different.
  Date/Author: 2026-09-29.

- Decision: a budget changes by adding a row that takes effect from a
  month, never by editing the old amount.
  Rationale: last March's report must compare March with March's budget.
  Date/Author: 2026-09-29.

- Decision: the pace test ignores the day of the month on which fixed bills
  land. A category counts as recurring when the same merchant charged it
  in each of the last three months, and its budget is judged against those
  expected charges, not against a straight line through the month.
  Rationale: rent on the first makes a straight-line pace say "over budget"
  every month on day one, and an alert that is always wrong is muted by
  day three.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

The server is Go and the dashboard is React in `web/`. Paths are from the
repository root.

**From the bank accounts plan.** Table `agent_bank_transaction` has, per
transaction: `agent_id`, `account_id`, `provider_transaction_id`,
`posted_on` (date), `amount` (`numeric(19,4)`, negative is money out),
`currency_code`, `description`, `merchant_name`, `category_primary` and
`category_detailed` (the provider's, empty for SimpleFIN), `is_pending`.
`ApplyBankSync` in `internal/db/database_bank.go` writes a sync's changes
in one database transaction. A bank connection is an agent source of kind
`bank`, read by `readBankSource` in `internal/agent/ingest_bank.go` on the
source's own schedule. The `finance` tool is in
`internal/agent/tools/finance/`, with `BankSpendingSummary` behind its
spending operation.

**From the net worth plan.** Table `agent_asset` (one row per thing owned
or owed) and `agent_asset_valuation` (its value per day), and the query
`NetWorthSeries`. Savings targets that follow accounts read these.

**Alerts.** `internal/agent/alert.go` runs the alert job. Candidates are
rows (`models.AgentAlertCandidate` in `internal/models/alert.go`) of a kind
(`message` or `burst` today) with a signal (`soon` or `now`) and a reason.
The job gathers candidates for two minutes, asks the model once, with the
person's memory, which to tell and in what words, then applies bounds in
code (`planAlerts`: a daily limit, quiet night hours, no repeat of a
subject within a week, mutes) and delivers each alert as a message in the
agent's main conversation, which the drawer shows and the linked chat app
receives. Sent alerts are `models.AgentAlert` rows with a `SubjectKey`
used to avoid repeats. Read
`docs/decisions/20260929-the-agent-tells-the-person-what-their-mail-says.md`
and `docs/planning/mail-alerts-execplan.md` before Milestone 6.

**Schedules, tools, API, catalogs.** As in the other two plans: schedules
are a cron line and a prompt (`docs/subsystems/jobs-and-schedules.md`);
tools are packages under `internal/agent/tools/` with a risk class; person
resolvers start with `requireAgentPerson`; every dashboard string is in
`web/src/i18n/en.ts`, `ja.ts` and `zh.ts`.

## Plan of Work

### Milestone 1: categories and transfers

Migration `0132_agent_budget.sql` (after the net worth plan's `0131`; renumber
if the plans land in another order) creates `agent_spending_category`:
`id`, `agent_id` (cascade), `category_name`, `parent_category_id`
(nullable, one level only: "Food" over "Groceries" and "Dining"),
`is_income` (boolean: salary, interest), `is_hidden`, timestamps; unique on
`(agent_id, category_name)`. It alters `agent_bank_transaction` to add
`spending_category_id` (references the category, on delete set null),
`categorized_by` (text: `person`, `rule`, `provider`, `agent`, empty),
`is_transfer` (boolean, default false), and `is_transfer_set_by_person`
(boolean).

When a person links their first bank, create a default category list:
income, housing, utilities, groceries, dining, transport, travel, shopping,
health, entertainment, subscriptions, fees, gifts and donations, other. In
`internal/banking/categories.go`, a table maps each Plaid
`personal_finance_category.primary` value to one of these names (for
example `FOOD_AND_DRINK` with detailed `FOOD_AND_DRINK_GROCERIES` to
groceries and the rest of `FOOD_AND_DRINK` to dining; `TRANSFER_IN`,
`TRANSFER_OUT` and `LOAN_PAYMENTS` to transfers).

`ApplyBankSync` gains a categorization step for each added or modified
transaction that the person has not set: rules (Milestone 2, a no-op until
then), then the provider mapping, then leave it empty for the agent. It
also runs transfer recognition over the connection's last seven days: a
provider transfer category, or an amount out of one of the agent's accounts
matched by the same amount into another of them within three days, marks
both sides, unless the person set them.

Acceptance: a database test with a fixture of a card payment from checking
and the matching credit on the card marks both as transfers; a Plaid
groceries transaction lands in groceries; a transaction the person moved to
dining stays in dining after a sync that modifies it.

### Milestone 2: rules and agent categorization

Table `agent_spending_rule`: `id`, `agent_id` (cascade), `match_text`
(matched case-insensitively against the merchant name, or the description
when there is no merchant), `account_id` (nullable), `minimum_amount` and
`maximum_amount` (nullable), `spending_category_id`, `marks_transfer`
(boolean), `rule_priority`, timestamps. The first matching rule by priority
wins.

Creating or changing a rule reapplies it to the agent's transactions whose
`categorized_by` is not `person`, in one statement.

A job kind `AgentJobCategorize`, queued at the end of a sync that left
uncategorized transactions, sends them in batches of fifty to the model on
the cheapest configured purpose (whichever the tree uses for sorting mail;
read `docs/subsystems/providers-and-models.md`), with no tools. The prompt
lists the person's categories by id and name and the transactions as
fenced untrusted data (merchant, description, amount, account kind). The
answer is a list of transaction id and category id pairs; the code drops
any pair whose ids it did not send, and writes the rest with
`categorized_by` `agent`. A transaction the model could not place stays
empty and shows as uncategorized.

Acceptance: a test with a fake model answers for a batch including an
invented id and an id from another agent; only the valid pairs are written.

### Milestone 3: budgets and spending queries

Table `agent_budget`: `id`, `agent_id` (cascade), `spending_category_id`
(cascade), `monthly_amount` (`numeric(19,4)`), `currency_code`,
`effective_from` (date, the first of a month), timestamps; unique on
`(spending_category_id, effective_from, currency_code)`. The budget for a
month is the row with the latest `effective_from` on or before it. An
amount of zero ends a budget.

GraphQL in `internal/api/v1api/apigraph/agent_budget.go`, with
`BudgetQuery` and `BudgetMutation` added to `schema.go`, each resolver
starting with `requireAgentPerson`:

- `SpendingCategories`, `CreateSpendingCategory`, `UpdateSpendingCategory`,
  `DeleteSpendingCategory` (its transactions become uncategorized).
- `SpendingRules`, `CreateSpendingRule`, `UpdateSpendingRule`,
  `DeleteSpendingRule`.
- `CategorizeTransaction(transactionId, categoryId, makeRule)` and
  `MarkTransfer(transactionId, isTransfer)`, both setting the person flags.
- `Budgets(month)`, `SetBudget(categoryId, monthlyAmount, currencyCode,
  effectiveFrom)`.
- `BudgetStatus(month)`: per category, the budget, spent so far, what was
  spent by the same day last month, the expected recurring charges still
  to come, the projected month end, and a `budgetPace` value of `under`,
  `on_track`, `at_risk` or `over`.
- `SpendingByDay(month, compareMonth)`: cumulative spending per day for
  both months, per currency, for the chart.
- `CashFlowByMonth(from, to)`: income, spending and their difference per
  month.

The projection, in `internal/agent/budget_pace.go` as a pure function with
table tests: projected month end equals spent so far, plus the recurring
charges expected but not yet seen this month (merchants that charged this
category in each of the last three months, at their median amount), plus
the rest of the category's spending so far divided by the days elapsed,
times the days left. `at_risk` when the projection exceeds the budget by
more than ten percent after the seventh day of the month; `over` when spent
so far exceeds it.

`BankSpendingSummary` from the bank accounts plan switches its grouping by
category to the person's categories, and leaves out transfers.

Acceptance: table tests for the projection, including a month with rent on
the first (not `at_risk` on day two) and a month running hot on dining
(`at_risk` by day twelve); API tests that a second person cannot read the
first person's budgets or categorize their transactions.

### Milestone 4: savings targets

Table `agent_savings_target`: `id`, `agent_id` (cascade), `target_name`,
`target_amount`, `currency_code`, `target_on` (date), `target_measure` (text:
`cash_flow` or `asset_balance`), `starting_amount` and `started_on` (the
baseline when the target was set), `closed_on`, timestamps; and
`agent_savings_target_asset` (`target_id`, `asset_id`, both cascade) for targets
that follow accounts.

Progress: for `cash_flow`, the sum of monthly income minus spending since
`started_on`; for `asset_balance`, the sum of the linked assets' latest
valuations minus `starting_amount`. The required pace is what remains
divided by the months left; a target is behind when the last two full months
both fell short of that pace. Queries `SavingsTargets` (with progress, pace
and whether it is behind), `CreateSavingsTarget`, `UpdateSavingsTarget`,
`CloseSavingsTarget`.

If the net worth plan has not landed, build `cash_flow` only and record
that here.

Acceptance: a fixture of four months of income and spending gives the
expected progress and flags a target behind after two short months.

### Milestone 5: the tool and the monthly review

Add these operations to the `finance` tool
(`internal/agent/tools/finance/finance.go`):

- read: `status` (this month's `BudgetStatus` and targets), `compare` (a
  month against another by category), `cash_flow`, `uncategorized` (the
  transactions left for the person to decide).
- write: `set_budget`, `categorize`, `create_rule`, `mark_transfer`,
  `set_target`, `create_category`.

Its description carries the recipes the agent should follow the same way
every time:

- proposing budgets: take the median of each category over the last three
  full months, round, show the person, and set only what they accept;
- a savings plan: from `cash_flow`, say what they save a month now, what a
  target needs, and which categories could close the gap, with the numbers;
- after a correction: offer a rule for that merchant.

Results that include merchant names or descriptions are marked untrusted.

When the person sets their first budget, the agent offers a schedule for a
monthly review on the first of the month: last month against its budgets,
the largest changes from the month before, targets, and anything left
uncategorized. It is an ordinary schedule the person can change or delete.

Acceptance: in a conversation with the sandbox bank linked, "suggest a
budget" produces a proposal and sets nothing until accepted; after
acceptance `teanode api call Budgets` shows the rows.

### Milestone 6: alerts

Add a candidate kind `budget` to `models.AgentAlertCandidate`, with a
`BudgetKey` field (for example `category:<id>:2026-09:at_risk` or
`target:<id>:2026-09:behind`) that becomes the alert's subject key, so the
same crossing is never told twice. Candidates are written in code:

- at the end of every sync, compute `BudgetStatus` for the current month
  and write a candidate for each category that newly crossed 80 percent of
  its budget, newly became `at_risk`, or newly went `over`, with a reason
  giving the numbers (spent, budget, projection, days left);
- on the first sync of each day, write one for each target newly behind.

The signal is `soon`, never `now`: nothing about a budget is worth waking
someone. The alert job words them with the person's memory in hand (it may
decide a known trip explains the dining and say nothing), and `planAlerts`
applies the same daily limit, night hours and mutes as for mail. Add a mute
kind for a category and one for all budget alerts, as rows beside the
existing mutes, reachable from the dashboard, the command line and the
conversation. Budget alerts are on once the person has a budget, and can be
switched off on their own.

Acceptance: in a test, a sync that pushes groceries from 70 to 85 percent
writes one candidate; a second sync the same day writes none; a muted
category writes none; the plan in `planAlerts` keeps the daily limit.

### Milestone 7: the Spending tab and documentation

On `web/src/pages/agent.tsx` add a **Spending** tab, using the components
in `docs/coding/frontend-design.md`:

- a line chart of cumulative spending by day, this month against last month
  (`SpendingByDay`), with today marked;
- a bar per category showing spent against budget, colored by
  `budgetPace`, with the projection as a marker;
- twelve months of income and spending as paired bars with the difference
  (`CashFlowByMonth`);
- targets with a progress bar and the pace needed;
- a table of this month's transactions with the category editable in place
  (and the offer to make a rule), filterable to uncategorized; tables stay
  tables on a phone and scroll sideways;
- the category and rule lists, and budgets per category.

The dashboard already draws charts without a charting package:
`web/src/components/usageChart.tsx` draws a column per day across a range
and ranked bars, and `web/src/components/budgetBar.tsx` draws an amount
against a limit in the color of how near the limit it is, using
`budgetNearness` from `web/src/components/common`. Build on those (pull
the shared drawing into a component both use, if that is what it takes)
rather than adding a charting package. A category against its budget is exactly what
`budgetBar.tsx` already draws for the agent's daily allowance. Strings in
all three catalogs.

Documentation: add budgets, categories, rules, targets and budget alerts to
`docs/subsystems/banking.md`, and the categorization job's use of the
model (what it sends: merchant, description, amount; never account numbers)
to the security review.

## Concrete Steps

From the repository root:

    go test ./internal/agent/ -run 'BudgetPace'
    go test ./internal/agent/tools/finance/
    make test
    make lint-ci
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

## Validation and Acceptance

Accepted when, on a development server with the sandbox bank linked:

1. Transactions arrive categorized from the provider mapping, card payments
   are marked as transfers and left out of spending, and the rest are
   categorized by the agent within a few minutes of the sync.
2. Correcting a transaction and accepting the rule recategorizes the
   merchant's past transactions, and a later sync does not undo it.
3. The agent proposes budgets from three months of history and sets only
   what is accepted.
4. The Spending tab shows this month against last month, categories against
   budgets, a year of cash flow, and targets.
5. Pushing a category past 80 percent produces one alert, within the alert
   bounds, and a muted category produces none.
6. A second person can read or change none of it.

## Idempotence and Recovery

The migration is additive; its reverse drops the new tables and the added
columns. Categorization is repeatable: rules and the provider mapping give
the same answer each time, the agent's job only touches uncategorized rows,
and nothing overwrites a person's choice. Alert candidates are keyed so a
repeated sync writes no second one.

## Artifacts and Notes

`BudgetStatus` for one category, with invented values, on the 18th of a
30-day month:

    category          dining
    budget            400.00
    spent so far      310.40
    same day last     240.10
    recurring due       0.00
    projected         517.30
    budgetPace        at_risk

## Interfaces and Dependencies

No new Go modules and no new npm packages unless the Decision Log records
one. `db.BudgetOperation`, `BudgetQuery`, `BudgetMutation`, the pure
function `ProjectCategoryMonth` in `internal/agent/budget_pace.go`, the
`finance` tool's budget operations, and the `budget` alert candidate kind
must exist by the end
of Milestone 6.

Revision note, 2026-09-29: after the maintainer asked that the plans
reuse existing concepts, savings goals were renamed savings targets so
they are not confused with the agent's goals, the separate `budget` tool
became operations on the shared `finance` tool, bank transactions are
described as coming from the `bank` source reader, and the charts reuse
the dashboard's own components.
