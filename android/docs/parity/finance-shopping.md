# Parity: finance and shopping

What the iPhone app does in Finance and Shopping, and where the Android app stands. Status is done, partly or not ported.
Logic is in `android/kit` (`com.teanode.kit.finance`, `com.teanode.kit.shopping`), screens in `android/app`
(`com.teanode.app.finance`, `com.teanode.app.shopping`), demo data in each part's `*Demo.kt`, pictures from
`FinanceScreenshotTest` and `ShoppingScreenshotTest`.

## The kit (ios/TeaNodeKit/Sources/TeaNodeKit)

| iOS file | Android | Status |
| --- | --- | --- |
| Finance.swift | FinanceDocuments, FinanceModels, FinanceExtras, FinanceMoney, FinanceCalls | done (money formatting uses `java.text`) |
| FinanceEntry.swift | FinanceEntry, FinanceEntryDocuments, FinanceEntryModels | done (drafts, checks, arguments, rates, calls) |
| FinanceManage.swift | FinanceManageModels, TransactionRowsFile, FinanceCalls | done |
| FinanceSharing.swift | FinanceSharingModels, FinanceExtras (ShareTarget), SharePeople | done (ShareTarget and the people and circles lists are here, since the calendar's are in another part) |
| FinanceStatement.swift | FinanceStatementDocuments, FinanceModels | done |
| FinanceTax.swift | FinanceTaxModels, FinanceExtras | done |
| SharedFinanceSpendingSummary.swift | SharedSpendingSummary | done |
| Shopping.swift | ShoppingModels, ShoppingLogic, ShoppingCalls, ShoppingDocuments | done (lenient decoding through `ShoppingJson`) |
| ShoppingFeed.swift | ShoppingFeed (a Flow over `Connection.subscribe`) | done |
| Tests | FinanceEntryTest, FinanceKitTest (spending, statement, tax, manage, money), FinanceSharingTest, SharedSpendingSummaryTest, SpendingLinesTest, ShoppingTest | done (the PersonCard test belongs to Contacts) |

## Shopping screens (ios/TeaNode/Sources)

| iOS file | Android | Status |
| --- | --- | --- |
| ShoppingViews.swift: lists, shared lists, stores, new list | ShoppingSection | done |
| ShoppingViews.swift: one list (group by store or aisle, rows or pictures, not needed, who is shopping, totals, Done, shop at a store, import, receipt, share, reminder, rename, delete) | ShoppingListScreen | done |
| ShoppingViews.swift: add item sheet (similar items, store suggestions, "is it one of these"), need again | ShoppingSheets | done |
| ShoppingStoreViews.swift: store screen, lists switches, cart progress, out of stock, reminder, receipt scan, Done | ShoppingStoreScreen | done (long press on a row opens the out of stock menu) |
| ShoppingItemViews.swift: rows, tiles, picture, item sheet, price chart, prices | ShoppingComponents, ShoppingItemSheet | done (tap a picture) |
| ShoppingReceiptViews.swift | ShoppingReceiptSheet | done (swipes became buttons) |
| ShoppingImportViews.swift | ShoppingImportSheet | done |
| ShoppingShareViews.swift | ShoppingShareSheet | done (no link to the circles screen, which is Settings') |
| ShoppingModel.swift, ShoppingWords.swift | ShoppingState, ShoppingWords, ShoppingFiles | done |
| ShoppingDemo.swift | ShoppingDemo | partly: answers are fixed, a check or an add does not change what is read next |

## Finance screens

| iOS file | Android | Status |
| --- | --- | --- |
| FinanceViews.swift: part menu | FinanceSection (a list of the parts, and who shares with the person) | done |
| FinanceViews.swift: Spending (month and year, cash flow, day by day, saving, budgets, where it went donut) | FinanceSpending | done (tap a bar or a day for its figures) |
| FinanceViews.swift: Transactions, filter, detail (category, rule, note, duplicates, receipts) | FinanceTransactions | partly: no memory section; "ask the agent" opens the main chat with the charge copied (the chat takes no reference from another screen) |
| FinanceViews.swift: Accounts, credit used, statement import address | FinanceAccounts | done |
| FinanceMoreViews.swift: Budgets, categories, rules | FinanceBudgets | done |
| FinanceMoreViews.swift: Net worth, assets | FinanceNetWorth | done (no memory section) |
| FinanceMoreViews.swift: Savings targets | FinanceNetWorth | done |
| FinanceMoreViews.swift: Receipts, receipt detail | FinanceReceipts | partly: no picture; typing in a receipt is FinanceEntryScreens |
| FinanceMoreViews.swift: Banks and currency | FinanceSetup | done |
| FinanceEntryViews.swift (type in a receipt, paystub, tax form) | kit only | done (FinanceEntryScreens) |
| FinanceManageViews.swift: trades | FinanceTrades | done |
| FinanceManageViews.swift: rename or delete a statement account, suggest rules, import credential, import rows | kit only | done (FinanceManageScreens, rename and delete on the accounts screen) |
| FinanceSharingViews.swift, FinanceShareAllViews.swift | FinanceSharing | done (the "Showing" menu is the list on the first screen) |
| SharedAllFinanceViews.swift | FinanceSharing (FinanceSharedAllScreen) | done |
| FinanceTaxViews.swift | FinanceTaxes | done (ring, upload, every setting) |
| FinanceCategoryNames.swift, StatementImportWords.swift | FinanceBase, FinanceAccounts | done |
| FinanceManageDemo.swift, FinanceSharingDemo.swift | FinanceDemo | done |

## Notes

- `GraphQLField.kt` in the kit's root is a small addition: one call that reads a field of an answer (the iPhone app
  writes an Answer struct for each call).
- `com.teanode.app.charts` was not on the branch yet, so the finance charts (donut, bars, line) are in
  `com.teanode.app.finance.FinanceCharts`. The location and health part can move them.
- `ShoppingSheet` (a bottom sheet with a title and two buttons) is used by Finance too.
