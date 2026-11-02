# Sharing all of one's finance at once

This ExecPlan is a living document. The sections Progress, Surprises &
Discoveries, Decision Log and Outcomes & Retrospective are kept up to date as
the work goes on.

## Purpose / Big Picture

Finance is shared one account at a time (`finance_account_share`, see
docs/subsystems/finance.md, "Sharing"). A person who wants their partner to
see what the household spends has to share every account, and again for each
account they link later. After this change they share their finance as a whole
with a person or a circle, at the spending level or the transactions level,
and every account they hold now or link later is covered. The reader sees one
combined view: the owner's spending by month and category across all their
accounts, and at the transactions level the transactions too.

To see it: Mira shares all her finance with Tobias at the spending level from
the Sharing panel. Tobias opens Finance, chooses Showing Mira, and sees one
Spending page across all her accounts, with no account names or balances.

## Progress

- [x] (2026-11-01) Plan written with the person's decisions.
- [x] (2026-11-01) Migration 0268, database reads and the access question.
- [x] (2026-11-01) GraphQL, CLI, the Sharing list, the person card.
- [ ] Dashboard and iPhone.
- [x] (2026-11-01) Server and command line docs and tests.
- [ ] Review, deploy.

## Decision Log

- Decision (the person, 2026-11-01): a whole-finance share covers every
  account, now and later. Per-account shares still work beside it, and for an
  account both reach, the higher level wins.
- Decision (the person, 2026-11-01): at the spending level the reader sees
  combined money out and in by month and spending category across all
  accounts, in each currency, with the owner's category names, and no account
  names, masks or balances. Balances stay with per-account shares.
- Decision (the person, 2026-11-01): the reader sees one combined view, under
  Showing <person>, like their own Spending page.
- Decision: the levels of a whole-finance share are `spending` and
  `transactions`. At `transactions` the reader also sees each transaction's day,
  amount, currency, merchant name and spending category, across all accounts,
  and still no account name or balance. Everything the per-account levels never
  show stays hidden (receipts, notes, provider data, budgets, rules, tax
  documents, sources).
- Decision: everything else follows per-account sharing: one question answers
  who may read; a share to a person needs a shared circle and a share to a
  circle needs the owner in it; starting or widening is the person's tap;
  the agent has no part in it, reading or sharing; the spending level widens a
  range of days to whole months.

- Decision: the combined reads use the level of the shares of all finance
  alone. A per-account share at a higher level is read per account and does
  not widen the combined page; in return an account the reader reaches by its
  own share is read at the higher of the two levels. A share of all finance
  names no account, so it lists none.
- Decision: the combined page of transactions is the owner's Transactions page
  without the account, so a transfer between their accounts is listed in its
  category; the combined spending leaves transfers out as their Spending page
  does.
- Decision: a read of shared finance is of one account (`financeAccountId`) or
  of a person's whole finance (`ownerUserId`), exactly one of the two.
  `ListSharedFinance` tells the reader who shares all their finance, and at
  which level.

## Interfaces

- Table `finance_share`: one row per owner and grantee (a person or a circle),
  with `finance_share_level` (`spending`, `transactions`).
- GraphQL (in `FinanceShareQuery` / `FinanceShareMutation`, never the tool's):
  `ShareFinance(granteeUserId | granteeCircleId, financeShareLevel)`,
  `UnshareFinance(financeShareId)`, `ListFinanceShares`; for the reader,
  `ListSharedFinance(ownerUserId?)`, and `ListSharedFinanceSpending` /
  `ListSharedFinanceTransactions` with `ownerUserId` in place of
  `financeAccountId`.
- CLI: `teanode finance share all <username|circle> --level spending|transactions`,
  and `shares`, `unshare` list and end both kinds.
