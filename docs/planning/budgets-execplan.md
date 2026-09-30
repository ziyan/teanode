# A person sets budgets and savings targets, and the agent tells them when they are off track

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It builds on
`docs/planning/finance-accounts-execplan.md` (finance transactions, exchange
rates, the categorize model setting, the `finance` tool, the `teanode
finance` command group) and, for savings targets that follow what accounts
hold, on `docs/planning/net-worth-execplan.md` (assets and valuations). What
it needs from each is repeated under Context and Orientation.

## Purpose / Big Picture

Finance transactions alone answer "what did I spend". A person trying to
save wants a monthly budget per spending category, a way to see while the
month is still happening whether it is on track, a word from the agent when
it is not, and help deciding what the budgets and savings targets should
be.

After this change:

- Every finance transaction has a **spending category** from the person's
  own list, assigned by the person, then by their spending rules, then by
  the provider category mapping, then by the categorize model, which may be
  a decision model such as Jev or a cheap chat model, as the operator
  chooses. The person can always correct it. Transfers between the
  person's own accounts, and card payments, are recognized and left out of
  spending.
- The person sets a monthly **budget** per spending category, by hand or by
  accepting what the agent proposes from the last three months.
- The person sets **savings targets**: an amount by a date, measured by
  money not spent (income minus spending) or by what chosen assets are
  worth.
- The **Finance** tab shows this month's spending day by day against last
  month's, each spending category against its budget, twelve months of
  spending and income, and each savings target's progress, all in the
  reporting currency.
- The agent **alerts** the person when a spending category crosses most of
  its budget, when the month's pace means it will overshoot, and when a
  savings target falls behind, through the alert path the agent already
  has, with its daily limit, quiet night hours and mutes.
- Everything is in the dashboard, the `teanode finance` command group and
  the `finance` tool alike.

How to see it working: with Plaid's sandbox institution linked, ask the
agent "suggest a monthly budget from my last three months"; accept it; open
the Finance tab and see spending categories against budgets and this month
against last; create a sandbox transaction (Plaid's
`/sandbox/transactions/create`) large enough to push one past its budget;
after the next sync an alert arrives in the agent's conversation, and
`teanode finance budget-status` shows the same numbers.

## Names used in this plan

The finance accounts plan's names list applies. Added here:

- **spending category**: a label in the person's own list. Never called
  just "category" in code or API names, so it cannot be confused with the
  provider category.
- **spending rule**: assigns a spending category (or marks a transfer) to
  finance transactions that match it.
- **provider category mapping**: the fixed table from provider categories
  (Plaid's categories and SimpleFIN's merchant category codes) to the
  default spending categories.
- **categorization**: assigning a spending category to a finance
  transaction. Who did it is `categorized_by`: `person`, `spending_rule`,
  `provider_category_mapping` or `categorize_model`.
- **transfer**: money moving between the person's own finance accounts,
  including a card payment. Neither spending nor income.
- **spending**: money out that is not a transfer; a refund in the same
  spending category reduces it. **Income**: money in that is not a
  transfer.
- **budget**: an amount for one spending category for each month from a
  given month on.
- **budget pace**: `under`, `on_track`, `at_risk` or `over`.
- **savings target**: an amount to save by a date. (TeaNode's agent already
  has "goals", which are something else; see the Decision Log.)

## Progress

- [x] (2026-09-29) Wrote this plan and its decision record.
- [x] (2026-09-29) Revised: savings goals renamed savings targets; operations
  on the shared `finance` tool; charts reuse the dashboard's components.
- [x] (2026-09-29) Revised: "bank" renamed "finance"; one name per thing;
  categorization runs on the categorize model, which may be Jev or a chat
  model; budgets and totals across currencies; parity across dashboard,
  command line and tool.
- [x] (2026-09-30) Milestone 1: spending categories, the columns on finance transactions,
  transfer recognition.
- [x] (2026-09-30) Milestone 2: spending rules and the provider category mapping.
- [x] (2026-09-30) Milestone 3: categorization by the categorize model.
- [x] (2026-09-30) Milestone 4: budgets and the spending queries.
- [x] (2026-09-30) Milestone 5: savings targets.
- [x] (2026-09-30) Milestone 6: the dashboard, command line and tool operations, and the
  monthly review.
- [x] (2026-09-30) Milestone 7: alerts through the existing alert path.
- [x] (2026-09-30) Milestone 8: documentation.

## Surprises & Discoveries

- Observation: to measure in Milestones 2 and 3: the share of a month's
  finance transactions the provider category mapping gets right before any
  spending rule exists; how many reach the categorize model; and, with Jev,
  what probability floor gives few wrong answers without leaving too many
  uncategorized.
  Evidence: to be filled.

## Decision Log

- Decision: only a card payment and a move between the person's own
  accounts count as transfers by provider category. A mortgage payment is
  housing, other loan payments are other, and a cash withdrawal is spending
  in other; money in from a loan stays a transfer, since it is not income.
  Rationale: the reason for leaving transfers out ("the purchases on the
  card already were") holds for a card payment and for money moving between
  one's own accounts, not for a mortgage, which is the largest housing cost
  most people have. The first version treated every loan payment as a
  transfer, and a mortgage never counted toward housing. Found in review,
  2026-09-30.
  Date/Author: 2026-09-30.

- Decision: a transaction the categorize model considered and could not
  place is marked as attempted and not asked about again until the
  provider changes its merchant, description or provider category. Pairing
  transfers is one-to-one and never touches a row the person decided about
  or one already marked. A person's choices on a pending transaction carry
  over to the posted one that replaces it.
  Rationale: all three were found in review. Without the mark the job asked
  the same newest transactions every sync and never reached older history;
  without one-to-one pairing a rent check could pair with an earlier
  transfer of the same amount and disappear from the budget; without the
  carry-over a correction was lost when the charge posted.
  Date/Author: 2026-09-30.

- Decision: the spending category that counts is the person's, from their
  own list; the provider category is kept as a hint. Order: the person,
  then a spending rule, then the provider category mapping, then the
  categorize model. Recorded as
  `docs/decisions/20260929-spending-categories-are-the-persons.md`.
  Rationale: providers disagree with each other and with the person, and
  SimpleFIN assigns none. Budgets mean nothing if the spending categories
  under them shift.
  Date/Author: 2026-09-29, the maintainer and the coding agent.

- Decision: the categorize model is the finance accounts plan's
  `agent.models.categorize` setting. If it names a decision model (Jev),
  each finance transaction is one decision whose answers are the person's
  spending categories, accepted when the answer's confidence is at
  least a floor, and the unsure rest go to a chat model; if it names a
  chat model, finance transactions go in batches with a structured answer.
  Rationale: the maintainer plans to categorize with Jev, and other
  operators will use a cheap chat model. A decision model scores every
  spending category at once and cannot answer off the list; a chat model
  can do the same job at a cost. TeaNode already has both shapes of call
  (`deciderFor` and structured answers), so neither is new code in kind.
  Date/Author: 2026-09-29.

- Decision: a person's own choice is never overwritten by a spending rule,
  a sync or the categorize model. Correcting a finance transaction offers
  to make a spending rule for that merchant, and a new spending rule
  applies to the past except where the person chose.
  Rationale: a correction that comes back after the next sync destroys
  trust in the budget.
  Date/Author: 2026-09-29.

- Decision: transfers are marked (`is_transfer`) and left out of spending
  and income. They are recognized by the provider's transfer and loan
  payment categories, and by pairing an amount out of one of the person's
  finance accounts with the same amount into another within three days. The
  person can mark or unmark one.
  Rationale: paying a card from checking is not spending; the purchases on
  the card already were.
  Date/Author: 2026-09-29.

- Decision: a budget has one currency. Spending in another currency is
  converted into it at the exchange rate of the day the finance transaction
  posted, and shown with its original amount. Spending in a currency with
  no exchange rate is not counted against the budget and is listed as such.
  Rationale: the maintainer asked for multiple currencies with conversion;
  a trip abroad should count against the dining budget at home.
  Date/Author: 2026-09-29.

- Decision: whether a month is on track, and when to alert, is computed in
  code. The model only words the alert and decides, with the person's
  memory in hand, whether it is worth saying now.
  Rationale: as for mail alerts
  (`docs/decisions/20260929-the-agent-tells-the-person-what-their-mail-says.md`),
  a threshold cannot be argued with, and the numbers must be right.
  Date/Author: 2026-09-29.

- Decision: budget and savings target alerts enter the existing alert path
  as a new kind of alert candidate.
  Rationale: the daily limit, quiet night hours, "nothing twice in a week",
  mutes and delivery to the linked chat app already exist.
  Date/Author: 2026-09-29.

- Decision: a budget changes by adding a row effective from a month, never
  by editing the old amount.
  Rationale: last March is compared with last March's budget.
  Date/Author: 2026-09-29.

- Decision: the pace test knows about fixed monthly charges. A merchant
  that charged a spending category in each of the last three months is
  expected again, and the projection counts it before it lands.
  Rationale: rent on the first makes a straight line say "over budget" on
  day one of every month, and an alert that is always wrong gets muted.
  Date/Author: 2026-09-29.

- Decision: what a person saves toward is a savings target, a table of its
  own, not an agent goal.
  Rationale: an agent goal is a sentence on a conversation the agent works
  toward turn by turn until it says the goal is met
  (`docs/subsystems/jobs-and-schedules.md`). A savings target is a number
  and a date lasting months, measured by queries. Reusing the word would
  make "clear my goal" ambiguous; reusing the mechanism would spend model
  turns on what a query knows.
  Date/Author: 2026-09-29.

- Decision: spending rules are a table of their own.
  Rationale: the only rules TeaNode has are mailbox rules
  (`models.MailboxRule`), which match mail headers and move messages.
  Everything else reuses what exists: schedules, the alert path and its
  mutes, the job queue, the configured models, and the dashboard's chart
  components.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Built on 2026-09-30 with the finance accounts plan. Spending categories,
spending rules, the provider category mapping (Plaid categories and
merchant category codes), the categorize model (Jev by decision with a
chat model for the unsure, or a chat model in batches), transfer
detection, budgets with pace and projection, savings targets, budget
alerts through the existing alert path, and the Spending and Budgets
sections. Review changed the transfer rules (loan payments are spending,
pairing is one-to-one), carried a person's choices from a pending
transaction to the posted one, and stopped the categorize job asking
about what it could not place.

## Context and Orientation

The server is Go and the dashboard is React in `web/`. Paths are from the
repository root.

**From the finance accounts plan.** `agent_finance_transaction` has, per
finance transaction: `agent_id`, `finance_account_id`,
`provider_transaction_id`, `posted_on`, `amount` (`numeric(19,4)`, negative
is money out), `currency_code`, `description`, `merchant_name`,
`provider_category_primary` and `provider_category_detailed` (Plaid's
categories; for SimpleFIN, `mcc:<code>` or empty), `is_pending`, and
`provider_metadata`. `ApplyFinanceSync` in `internal/db/database_finance.go`
writes a sync's changes in one database transaction, called by
`readFinanceSource` in `internal/agent/ingest_finance.go` on the finance
source's own schedule. `ExchangeRate(from, to, on)` in
`internal/db/database_exchange_rate.go` gives a day's rate, and the agent
row has `reporting_currency_code`. The settings have
`agent.models.categorize`. The `finance` tool, the `teanode finance`
command group, the Finance tab and the parity tests exist.
`FinanceSpendingSummary` groups by provider category.

**From the net worth plan.** `agent_asset` and `agent_asset_valuation`, and
`NetWorthSeries`. Savings targets measured by assets read these.

**Models.** `internal/agent/dream_decide.go` shows the decision model path:
`deciderFor()` returns the `llm.Decider` for a configured decision model,
and `Decide(ctx, state, questions)` scores the choices of each question
(`decide.Question` has `Instructions` and `Choices`, a map from answer name
to its description) and returns, per question, `Probabilities` by answer
name and a `Confidence`. Calls run a few at a time under a gate
(`decidersAtOnce`). The chat model path and structured answers are in
`docs/subsystems/providers-and-models.md`. The finance accounts plan says
how `agent.models.categorize` resolves to a model (itself, else `decide`,
else `scan`, else `fast`).

**Alerts.** `internal/agent/alert.go` runs the alert job. Alert candidates
are rows (`models.AgentAlertCandidate`, `internal/models/alert.go`) with a
kind (`message` or `burst` today), a signal (`soon` or `now`) and a reason.
The job gathers alert candidates for two minutes, asks the model once, with
the person's memory, which to tell and how, then `planAlerts` applies the
bounds in code (a daily limit, quiet night hours, no repeat of a subject
within a week, mutes) and delivers each alert in the agent's main
conversation, which the drawer shows and the linked chat app receives.
Sent alerts are `models.AgentAlert` rows whose `SubjectKey` prevents
repeats. Read
`docs/decisions/20260929-the-agent-tells-the-person-what-their-mail-says.md`
and `docs/planning/mail-alerts-execplan.md` before Milestone 7.

**Schedules, tools, API, command line, catalogs.** As in the finance
accounts plan. Charts: `web/src/components/usageChart.tsx` (a column per
day, ranked bars) and `web/src/components/budgetBar.tsx` (an amount against
a limit, colored by `budgetNearness` from `web/src/components/common`).

## Parity

    what                           GraphQL                   dashboard (Finance tab)   subcommand / tool operation
    spending categories            SpendingCategories        Spending categories       spending-categories / spending_categories
    add one                        CreateSpendingCategory    Spending categories       create-spending-category / create_spending_category
    change one                     UpdateSpendingCategory    Spending categories       update-spending-category / update_spending_category
    delete one                     DeleteSpendingCategory    Spending categories       delete-spending-category / delete_spending_category
    spending rules                 SpendingRules             Spending rules            spending-rules / spending_rules
    add one                        CreateSpendingRule        Spending rules            create-spending-rule / create_spending_rule
    change one                     UpdateSpendingRule        Spending rules            update-spending-rule / update_spending_rule
    delete one                     DeleteSpendingRule        Spending rules            delete-spending-rule / delete_spending_rule
    categorize a transaction       CategorizeTransaction     Transactions, inline      categorize-transaction / categorize_transaction
    mark or unmark a transfer      MarkTransfer              Transactions, inline      mark-transfer / mark_transfer
    uncategorized transactions     FinanceTransactions       Transactions, filter      transactions --is-uncategorized
    budgets                        Budgets                   Budgets                   budgets
    set a budget                   SetBudget                 Budgets                   set-budget / set_budget
    budget status                  BudgetStatus              Budgets, bars             budget-status / budget_status
    this month against last        SpendingByDay             Spending chart            spending-by-day / spending_by_day
    income and spending by month   CashFlow                  Cash flow chart           cash-flow / cash_flow
    savings targets                SavingsTargets            Savings targets           savings-targets / savings_targets
    add one                        CreateSavingsTarget       Savings targets           create-savings-target / create_savings_target
    change one                     UpdateSavingsTarget       Savings targets           update-savings-target / update_savings_target
    close one                      CloseSavingsTarget        Savings targets           close-savings-target / close_savings_target
    mute budget alerts             the existing mute operation, from the dashboard, the command line and the conversation

Names follow the finance accounts plan's rule (the GraphQL name in
snake_case for the tool, kebab-case for the subcommand).

Reads are risk `read`; deletes `destructive`; the rest `write`. No new
exceptions to the parity tests.

## Plan of Work

### Milestone 1: spending categories and transfers

Migration `0135_agent_budget.sql` (after the net worth plan's `0134`; take
the next free number when the work starts) creates:

    agent_spending_category
      id                          text primary key
      agent_id                    text not null, references agent, on delete cascade
      spending_category_name      text not null
      parent_spending_category_id text, references agent_spending_category   one level
      is_income                   boolean not null default false
      is_hidden                   boolean not null default false
      created_at, updated_at timestamptz
      unique (agent_id, spending_category_name)

and adds to `agent_finance_transaction`:

    spending_category_id        text, references agent_spending_category, on delete set null
    categorized_by              text      person, spending_rule, provider_category_mapping, categorize_model
    categorization_confidence   numeric(5,4)   the decision model's confidence; empty otherwise
    is_transfer                 boolean not null default false
    transfer_marked_by          text      '', person, spending_rule, provider_category_mapping, detection:
                                          what marked it, so each clears only its own marks
    categorize_attempted_at     timestamptz   the categorize model was asked and could not place it

When a person links their first finance source, create the default
spending categories: income, housing, utilities, groceries, dining,
transport, travel, shopping, health, entertainment, subscriptions, fees,
gifts and donations, other.

`ApplyFinanceSync` gains a categorization step for each added or modified
finance transaction whose `categorized_by` is not `person`: spending rules
(Milestone 2), then the provider category mapping (Milestone 2), else left
for the categorize model (Milestone 3). It also runs transfer recognition
over the finance source's last seven days: a provider transfer or loan
payment category, or an amount out of one of the agent's finance accounts
matched by the same amount into another within three days, marks both
sides unless the person set them.

Acceptance: a database test with a card payment from checking and the
matching credit on the card marks both as transfers; a finance transaction
the person moved to dining stays in dining after a sync that modifies it.

### Milestone 2: spending rules and the provider category mapping

    agent_spending_rule
      id                   text primary key
      agent_id             text not null, references agent, on delete cascade
      match_text           text not null   matched case-insensitively against merchant_name,
                                            or description when there is no merchant
      finance_account_id   text, references agent_finance_account, on delete cascade
      minimum_amount       numeric(19,4)
      maximum_amount       numeric(19,4)
      spending_category_id text, references agent_spending_category, on delete cascade
      is_transfer          boolean not null default false   marks matches as transfers
      rule_priority        integer not null
      created_at, updated_at timestamptz

The first matching spending rule by priority wins. Creating or changing one
reapplies it, in one statement, to the agent's finance transactions whose
`categorized_by` is not `person`.

The provider category mapping is `internal/finance/spending_categories.go`:
Plaid's `personal_finance_category` values (for example
`FOOD_AND_DRINK_GROCERIES` to groceries, the rest of `FOOD_AND_DRINK` to
dining; `TRANSFER_IN`, `TRANSFER_OUT` and `LOAN_PAYMENTS` to transfers) and
merchant category code ranges for SimpleFIN's `mcc:<code>` (grocery stores
to groceries, restaurants to dining, airlines and lodging to travel), to the
default spending category names. A person who renamed or deleted a default
spending category simply gets no mapping for it.

Acceptance: tests for rule priority, retroactive application that skips
the person's choices, and the mapping for a Plaid and an `mcc` example.

### Milestone 3: categorization by the categorize model

A job kind `AgentJobCategorize` (in `internal/models/agent.go`, with a
timeout in `internal/agent/job_policy.go`), queued at the end of a sync
that left finance transactions uncategorized, in
`internal/agent/categorize.go`. It resolves `agent.models.categorize` as the
finance accounts plan describes.

With a decision model: for each finance transaction, one `Decide` call,
run a few at a time under the same kind of gate `dream_decide.go` uses, with
the state

    merchant: Corner Grocer; description: CORNER GROCER 0412;
    amount: -42.17 USD; account kind: depository; provider category: mcc:5411

and one question, `spending_category`, whose choices are the person's
spending categories by id with each one's name (and parent) as the
description. The answer's `Choice` is written with `categorized_by`
`categorize_model` and its `Confidence` as `categorization_confidence` when
the confidence is at least `categorizeFloor` (start at 0.6, like
`worthOpeningFloor`, and tune it from Surprises & Discoveries). Below the
floor, the finance transaction goes to the chat model path with the model
`agent.models.scan` resolves to (else `fast`), which is what the decision
client's own documentation describes: take the answer where it is sure and
ask a language model where it is not. If no chat model is configured it
stays uncategorized.

With a chat model: batches of fifty, the finance transactions as fenced
untrusted data with the same fields, the spending categories by id, and a
structured answer of transaction id and spending category id pairs. The
code drops any pair whose ids it did not send and writes the rest with
`categorized_by` `categorize_model`.

Either way, what is sent is merchant, description, amount, currency,
account kind and provider category, never account numbers or provider
metadata. Usage is recorded against the person like every other model
call.

Acceptance: with a fake decider, answers above the floor are written with
their confidence and answers below go to the chat model path; with a fake chat model, a batch
answer including an invented id and an id from another agent writes only
the valid pairs; with `categorize` set to a `typesafe` model the decider
path runs, and with it set to a chat model the batch path runs.

### Milestone 4: budgets and spending queries

    agent_budget
      id                   text primary key
      agent_id             text not null, references agent, on delete cascade
      spending_category_id text not null, references agent_spending_category, on delete cascade
      monthly_amount       numeric(19,4) not null   zero ends the budget
      currency_code        text not null
      effective_from       date not null   the first of a month
      created_at, updated_at timestamptz
      unique (spending_category_id, effective_from)

The budget for a month is the row with the latest `effective_from` on or
before it.

GraphQL, added to `FinanceQuery` and `FinanceMutation`, each resolver
starting with `requireAgentPerson`: the spending category, spending rule,
categorize transaction, mark transfer, budget operations in the Parity
table, and:

- `BudgetStatus(month)`: per spending category with a budget, the budget
  and its currency, spending so far converted into it at each posted day's
  rate, spending by the same day last month, fixed charges still expected,
  the projected month end, `budgetPace`, and any spending left out for want
  of a rate.
- `SpendingByDay(month, compareMonth)`: cumulative spending per day for both
  months in the reporting currency.
- `CashFlow(from, to)`: income, spending and their difference per month in
  the reporting currency, and per currency.

The projection is a pure function, `ProjectSpendingCategoryMonth`, in
`internal/agent/budget_pace.go` with table tests: spending so far, plus the
fixed charges expected but not yet seen this month (merchants that charged
this spending category in each of the last three months, at their median
amount), plus the rest of this month's spending divided by the days
elapsed, times the days left. `at_risk` when the projection exceeds the
budget by more than ten percent after the seventh day; `over` when spending
so far exceeds it.

`FinanceSpendingSummary` switches its grouping from provider category to
spending category and leaves out transfers.

Acceptance: table tests for the projection, including rent on the first
(not `at_risk` on day two) and a month running hot on dining (`at_risk` by
day twelve); a budget in USD with a EUR finance transaction counts the
converted amount; API tests that a second person can read or change none
of it.

### Milestone 5: savings targets

    agent_savings_target
      id                  text primary key
      agent_id            text not null, references agent, on delete cascade
      savings_target_name text not null
      target_amount       numeric(19,4) not null
      currency_code       text not null
      target_on           date not null
      target_measure      text not null   cash_flow, asset_value
      starting_amount     numeric(19,4)
      started_on          date not null
      closed_on           date
      created_at, updated_at timestamptz

    agent_savings_target_asset
      savings_target_id text, references agent_savings_target, on delete cascade
      asset_id          text, references agent_asset, on delete cascade
      primary key (savings_target_id, asset_id)

Progress, in the savings target's currency: for `cash_flow`, income minus
spending since `started_on`; for `asset_value`, the linked assets' latest
valuations (converted) minus `starting_amount`. The required pace is what
remains divided by the months left; a savings target is behind when the
last two full months both fell short of it. If the net worth plan has not
landed, build `cash_flow` only and record that here.

Acceptance: four months of fixture income and spending give the expected
progress and flag a savings target behind after two short months.

### Milestone 6: the three ways in, and the monthly review

The command line (`internal/cmd/finance.go`) and the tool
(`internal/agent/tools/finance/finance.go`) gain every operation in the
Parity table. The tool's description carries recipes the agent follows the
same way every time: proposing budgets (the median of each spending
category over the last three full months, rounded, shown to the person, set
only as accepted); a savings plan (what they save a month now, what a
savings target needs, which spending categories could close the gap, with
numbers); after a correction, offering a spending rule for that merchant.
Results with merchant names or descriptions are untrusted.

On the Finance tab: a chart of cumulative spending by day, this month
against last, built on `usageChart.tsx`; a bar per spending category
against its budget built on `budgetBar.tsx`, colored by `budgetPace`, with
the projection marked; twelve months of income and spending; savings
targets with progress and the pace needed; the transactions table with the
spending category editable in place and the offer to make a spending rule,
filterable to uncategorized; the spending category, spending rule and
budget lists. Strings in all three catalogs.

When the person sets their first budget, the agent offers a schedule for a
monthly review on the first of the month: last month against its budgets,
the largest changes from the month before, savings targets, and anything
uncategorized. It is an ordinary schedule.

Acceptance: "suggest a budget" in a conversation proposes and sets nothing
until accepted; `teanode finance budgets` then shows the rows, and the
dashboard the same; the parity tests pass.

### Milestone 7: alerts

Add an alert candidate kind `budget` to `models.AgentAlertCandidate`, with a
`budget_key` column (for example
`spending-category:<id>:2026-09:at_risk` or
`savings-target:<id>:2026-09:behind`) that becomes the alert's subject key,
so the same crossing is never told twice. Alert candidates are written in
code: after every sync, for each spending category that newly crossed 80
percent of its budget, newly became `at_risk`, or newly went `over`, with a
reason giving the numbers; on the first sync of each day, for each savings
target newly behind. The signal is `soon`, never `now`. The alert job words
them with the person's memory in hand and `planAlerts` applies the same
bounds and mutes as for mail. Add a mute for one spending category and one
for all budget alerts, as rows beside the existing mutes, reachable
wherever the existing mutes are.

Acceptance: a sync that pushes groceries from 70 to 85 percent writes one
alert candidate; a second sync the same day writes none; a muted spending
category writes none; `planAlerts` keeps the daily limit.

### Milestone 8: documentation

Add spending categories, spending rules, categorization and the categorize
model, budgets, savings targets and budget alerts to
`docs/subsystems/finance.md`; the subcommands to
`docs/reference/command-line.md`; and to the security review, what
categorization sends to the categorize model.

## Concrete Steps

From the repository root:

    go test ./internal/agent/ -run 'BudgetPace|Categorize'
    go test ./internal/agent/tools/finance/ ./internal/cmd/ ./internal/finance/
    make test
    git add <new files by name>
    make lint-ci
    cd web && npx tsc --noEmit && node scripts/check-catalogs.mjs

## Validation and Acceptance

Accepted when, on a development server with Plaid's sandbox institution
linked:

1. Finance transactions arrive categorized by the provider category
   mapping, card payments are transfers and left out of spending, and the
   rest are categorized by the categorize model within minutes of the sync,
   with Jev configured and, separately, with a chat model configured.
2. Correcting a finance transaction and accepting the spending rule
   recategorizes the merchant's past finance transactions, and a later sync
   does not undo it.
3. The agent proposes budgets from three months and sets only what is
   accepted.
4. The Finance tab, the command line and the agent show the same budget
   status, spending by day, cash flow and savings targets, converted where
   currencies differ.
5. Pushing a spending category past 80 percent produces one alert within
   the alert bounds; a muted spending category produces none.
6. A second person can read or change none of it, and the parity tests
   pass.

## Idempotence and Recovery

The migration is additive; its reverse drops the new tables and the added
columns. Categorization is repeatable: spending rules and the provider
category mapping give the same answer each time, the categorize model only
touches uncategorized finance transactions, and nothing overwrites the
person. Alert candidates are keyed so a repeated sync writes no second one.

## Artifacts and Notes

`BudgetStatus` for one spending category, with invented values, on the 18th
of a 30-day month:

    spending category   dining
    budget              400.00 USD
    spending so far     310.40 USD   (including 38.00 EUR at 1.1172)
    same day last month 240.10 USD
    fixed charges due     0.00 USD
    projected           517.30 USD
    budgetPace          at_risk

## Interfaces and Dependencies

No new Go modules and no npm packages. By the end of Milestone 7:
`db.BudgetOperation`, the budget operations in `FinanceQuery` and
`FinanceMutation` with their subcommands and tool operations,
`AgentJobCategorize`, the pure function `ProjectSpendingCategoryMonth`, and
the `budget` alert candidate kind.

Revision notes, 2026-09-29: savings goals became savings targets so they
are not confused with the agent's goals; the separate `budget` tool became
operations on the shared `finance` tool; charts reuse the dashboard's
components. Then, at the maintainer's request: "bank" became "finance";
every thing got one name (spending category is never shortened to
category, `categorized_by` values name the thing that categorized);
categorization runs on `agent.models.categorize`, by decision with Jev or by
batch with a chat model; budgets convert other currencies at the posted
day's rate; and a parity table covers the dashboard, command line and tool.
