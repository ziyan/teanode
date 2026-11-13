package com.teanode.app.finance

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text

// What demo mode answers for Finance: a checking account, a card and a brokerage account (invented), a month of
// transactions in a few categories, budgets, net worth, savings targets, a receipt, a paystub and a W-2, and what two
// invented people share. Every name, number and address is made up.

internal object FinanceDemoData {
    const val CHECKING_ID = "demo-account-checking"
    const val CARD_ID = "demo-account-card"
    const val TRANSACTION_ID = "demo-transaction-1"
    const val RECEIPT_ID = "demo-finance-receipt-1"
    const val SHARER_ID = "demo-user-2"
    const val WHOLE_SHARER_ID = "demo-user-3"

    private fun account(id: String, name: String, mask: String, kind: String, balance: String, source: String = "demo-source-1", provider: String = "plaid") =
        """{"id":"$id","sourceId":"$source","providerKind":"$provider","institutionName":"Example Bank","accountName":"$name","accountMask":"$mask","accountKind":"$kind","currencyCode":"USD","currentBalance":"$balance","availableBalance":"$balance","balanceAt":"2030-11-01T09:00:00Z","isSignInRequired":false,"reportingCurrencyCode":"USD","convertedCurrentBalance":"$balance"}"""

    val accounts = """
        [${account(CHECKING_ID, "Checking", "0001", "checking", "2450.37")},
         ${account(CARD_ID, "Rewards card", "4321", "credit", "-612.80")},
         ${account("demo-account-brokerage", "Index fund", "7788", "investment", "18250.00", provider = "simplefin")}]
    """.trimIndent()

    val categories = """
        [{"id":"c-income","spendingCategoryName":"income","parentSpendingCategoryId":"","isIncome":true,"isHidden":false,"isTransfer":false,"isOther":false},
         {"id":"c-groceries","spendingCategoryName":"groceries","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
         {"id":"c-dining","spendingCategoryName":"dining","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
         {"id":"c-transport","spendingCategoryName":"transport","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
         {"id":"c-housing","spendingCategoryName":"housing","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
         {"id":"c-other","spendingCategoryName":"other","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":false,"isOther":true},
         {"id":"c-transfer","spendingCategoryName":"transfer","parentSpendingCategoryId":"","isIncome":false,"isHidden":false,"isTransfer":true,"isOther":false}]
    """.trimIndent()

    private fun transaction(id: String, day: String, amount: String, merchant: String, category: String, account: String = CHECKING_ID, extra: String = "") =
        """{"id":"$id","financeAccountId":"$account","postedOn":"$day","transactedAt":"${day}T12:00:00Z","amount":"$amount","currencyCode":"USD","description":"${merchant.uppercase()} #204","merchantName":"$merchant","isPending":false,"spendingCategoryId":"$category","categorizedBy":"spending_rule","categorizationConfidence":"high","annotation":"","receiptCount":0,"createdAt":"${day}T12:00:00Z","modifiedAt":"${day}T12:00:00Z"$extra}"""

    val transactions = listOf(
        transaction(TRANSACTION_ID, "2030-11-02", "-54.20", "Corner Market", "c-groceries", extra = ""","receiptCount":1"""),
        transaction("demo-transaction-2", "2030-11-02", "-18.50", "Sample Cafe", "c-dining", CARD_ID),
        transaction("demo-transaction-3", "2030-11-01", "-1200.00", "Example Property", "c-housing"),
        transaction("demo-transaction-4", "2030-11-01", "3200.00", "Example Payroll", "c-income"),
        transaction("demo-transaction-5", "2030-10-31", "-42.00", "City Transit", "c-transport", CARD_ID, extra = ""","isPending":true"""),
        transaction("demo-transaction-6", "2030-10-30", "-9.99", "Streaming Example", "", CARD_ID),
    )

    val creditUsage = """{"reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"totalOwedAmount":"612.80","totalCreditLimitAmount":"5000.00","usageShare":0.12,"leftOutCardCount":1,"leftOutOwedAmount":"40.00","creditCards":[{"financeAccountId":"$CARD_ID","accountName":"Rewards card","accountMask":"4321","institutionName":"Example Bank","currencyCode":"USD","owedAmount":"612.80","creditLimitAmount":"5000.00","creditLimitSource":"provider","usageShare":0.12}]}"""

    val spendingSummary = """{"groupBy":"spendingCategory","reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"convertedMoneyOut":"1900.00","convertedMoneyIn":"3200.00",
        "spendingSummaryRows":[{"groupKey":"c-housing","groupLabel":"housing","currencyCode":"USD","moneyOut":"1200.00","moneyIn":"0","financeTransactionCount":1},{"groupKey":"c-groceries","groupLabel":"groceries","currencyCode":"USD","moneyOut":"380.40","moneyIn":"12.00","financeTransactionCount":9},
          {"groupKey":"c-dining","groupLabel":"dining","currencyCode":"USD","moneyOut":"140.50","moneyIn":"0","financeTransactionCount":6},{"groupKey":"c-transport","groupLabel":"transport","currencyCode":"USD","moneyOut":"96.00","moneyIn":"0","financeTransactionCount":4},
          {"groupKey":"c-income","groupLabel":"income","currencyCode":"USD","moneyOut":"0","moneyIn":"3200.00","financeTransactionCount":1},{"groupKey":"","groupLabel":"","currencyCode":"USD","moneyOut":"23.10","moneyIn":"0","financeTransactionCount":2}],
        "convertedSpendingSummaryRows":[{"groupKey":"c-housing","groupLabel":"housing","moneyOut":"1200.00","moneyIn":"0"},{"groupKey":"c-groceries","groupLabel":"groceries","moneyOut":"380.40","moneyIn":"12.00"},{"groupKey":"c-dining","groupLabel":"dining","moneyOut":"140.50","moneyIn":"0"},
          {"groupKey":"c-transport","groupLabel":"transport","moneyOut":"96.00","moneyIn":"0"},{"groupKey":"c-income","groupLabel":"income","moneyOut":"0","moneyIn":"3200.00"},{"groupKey":"","groupLabel":"","moneyOut":"23.10","moneyIn":"0"}]}"""

    val budgetStatus = """{"month":"2030-11","year":"","asOf":"2030-11-13","spendingCategories":[
        {"spendingCategoryId":"c-groceries","spendingCategoryName":"groceries","budgetAmount":"500.00","currencyCode":"USD","budgetToDateAmount":"200.00","spendingAmount":"368.40","spendingBySameDayLastMonthAmount":"250.00","fixedChargesDueAmount":"0","projectedAmount":"520.00","budgetPace":"at_risk"},
        {"spendingCategoryId":"c-dining","spendingCategoryName":"dining","budgetAmount":"200.00","currencyCode":"USD","budgetToDateAmount":"80.00","spendingAmount":"60.50","spendingBySameDayLastMonthAmount":"70.00","fixedChargesDueAmount":"0","projectedAmount":"140.00","budgetPace":"on_track"},
        {"spendingCategoryId":"c-transport","spendingCategoryName":"transport","budgetAmount":"80.00","currencyCode":"USD","budgetToDateAmount":"30.00","spendingAmount":"96.00","spendingBySameDayLastMonthAmount":"50.00","fixedChargesDueAmount":"0","projectedAmount":"140.00","budgetPace":"over"}],"incomeCategories":[]}"""

    val savingSummary = """{"reportingCurrencyCode":"USD","incomeBudgetCount":1,"spendingBudgetCount":3,"expectedIncomeAmount":"3200.00","expectedSpendingAmount":"2000.00","expectedSavingAmount":"1200.00","incomeAmount":"3200.00","spendingAmount":"1900.00","savingAmount":"1300.00","projectedIncomeAmount":"3200.00","projectedSpendingAmount":"2100.00","projectedSavingAmount":"1100.00","savingPace":"on_track","unconvertedCurrencyCodes":[]}"""

    private fun months(): String {
        val values = listOf("2029-12" to ("3100" to "2300"), "2030-01" to ("3100" to "2100"), "2030-02" to ("3100" to "2500"), "2030-03" to ("3300" to "2200"), "2030-04" to ("3200" to "1900"), "2030-05" to ("3200" to "2600"),
            "2030-06" to ("3200" to "2000"), "2030-07" to ("3200" to "2400"), "2030-08" to ("3400" to "2100"), "2030-09" to ("3200" to "2300"), "2030-10" to ("3200" to "2000"), "2030-11" to ("3200" to "1900"))
        return values.joinToString(",") { (month, pair) -> """{"cashFlowMonth":"$month","incomeAmount":"${pair.first}.00","spendingAmount":"${pair.second}.00","netAmount":"${pair.first.toInt() - pair.second.toInt()}.00"}""" }
    }

    val cashFlow = """{"reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"cashFlowMonths":[${months()}]}"""

    private fun days(base: Int, scale: Double, count: Int): String {
        var total = 0.0
        return (1..count).joinToString(",") { day ->
            val spent = (base + (day * 7) % 23) * scale
            total += spent
            """{"spentOn":"2030-11-${"%02d".format(day)}","spendingAmount":"${"%.2f".format(spent)}","cumulativeSpendingAmount":"${"%.2f".format(total)}"}"""
        }
    }

    val spendingByDay = """{"month":"2030-11","compareMonth":"2030-10","reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"monthDays":[${days(30, 1.0, 13)}],"compareMonthDays":[${days(28, 1.1, 30)}]}"""

    val rules = """[{"id":"rule-1","matchText":"Corner Market","financeAccountId":"","minimumAmount":"","maximumAmount":"","spendingCategoryId":"c-groceries","rulePriority":10},{"id":"rule-2","matchText":"City Transit","financeAccountId":"","minimumAmount":"","maximumAmount":"","spendingCategoryId":"c-transport","rulePriority":10}]"""

    val budgets = """[{"id":"b1","spendingCategoryId":"c-groceries","monthlyAmount":"500.00","currencyCode":"USD","effectiveFrom":"2030-01-01"},{"id":"b2","spendingCategoryId":"c-dining","monthlyAmount":"200.00","currencyCode":"USD","effectiveFrom":"2030-01-01"},{"id":"b3","spendingCategoryId":"c-transport","monthlyAmount":"80.00","currencyCode":"USD","effectiveFrom":"2030-01-01"}]"""

    val netWorth = """{"reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"convertedNetWorthPoints":[
        {"netWorthOn":"2030-08-15","netWorthAmount":"18200.00"},{"netWorthOn":"2030-09-01","netWorthAmount":"19050.00"},{"netWorthOn":"2030-09-15","netWorthAmount":"19800.00"},{"netWorthOn":"2030-10-01","netWorthAmount":"19600.00"},
        {"netWorthOn":"2030-10-15","netWorthAmount":"20900.00"},{"netWorthOn":"2030-11-01","netWorthAmount":"21650.00"},{"netWorthOn":"2030-11-13","netWorthAmount":"22088.00"}]}"""

    private fun asset(id: String, name: String, kind: String, value: String, liability: Boolean = false, closed: Boolean = false) =
        """{"id":"$id","assetName":"$name","assetKind":"$kind","isLiability":$liability,"currencyCode":"USD","financeAccountId":"","financeSecurity":null,"valuationSource":"manual","estimateDescription":"","isEstimateAllowed":false,"closedOn":${if (closed) "\"2030-05-01\"" else "null"},
            "latestValuation":{"id":"$id-v1","valuedOn":"2030-11-01","value":"$value","currencyCode":"USD","valuationSource":"manual","estimateLow":"","estimateHigh":"","valuationNote":"","heldQuantity":"","unitPrice":"","costBasis":""}}"""

    val assets = """
        [${asset("asset-1", "Checking", "cash", "2450.37")}, ${asset("asset-2", "Index fund", "investment", "18250.00")}, ${asset("asset-3", "Family car", "vehicle", "9800.00")},
         ${asset("asset-4", "Rewards card", "credit_card", "612.80", liability = true)}, ${asset("asset-5", "Old savings", "cash", "0.00", closed = true)}]
    """.trimIndent()

    val assetHistory = """{"assetValuations":[{"id":"v3","valuedOn":"2030-11-01","value":"9800.00","currencyCode":"USD","valuationSource":"manual","estimateLow":"","estimateHigh":"","valuationNote":"Checked a listing","heldQuantity":"","unitPrice":"","costBasis":""},{"id":"v2","valuedOn":"2030-08-01","value":"10200.00","currencyCode":"USD","valuationSource":"manual","estimateLow":"","estimateHigh":"","valuationNote":"","heldQuantity":"","unitPrice":"","costBasis":""},{"id":"v1","valuedOn":"2030-02-01","value":"11000.00","currencyCode":"USD","valuationSource":"manual","estimateLow":"","estimateHigh":"","valuationNote":"Bought","heldQuantity":"","unitPrice":"","costBasis":""}]}"""

    val savingsTargets = """
        [{"savingsTarget":{"id":"t1","savingsTargetName":"Emergency fund","targetAmount":"10000.00","currencyCode":"USD","targetOn":"2031-06-01","targetMeasure":"net_worth","startingAmount":"2000.00","startedOn":"2030-01-01","closedOn":null,"assetIds":[],"financeAccountIds":[]},
          "savingsTargetProgress":{"savedAmount":"6400.00","remainingAmount":"3600.00","monthsLeftCount":7,"requiredMonthlyAmount":"514.29","isBehind":true,"unconvertedCurrencyCodes":[]}},
         {"savingsTarget":{"id":"t2","savingsTargetName":"Trip","targetAmount":"3000.00","currencyCode":"USD","targetOn":"2031-03-01","targetMeasure":"cash_flow","startingAmount":"0","startedOn":"2030-06-01","closedOn":null,"assetIds":[],"financeAccountIds":[]},
          "savingsTargetProgress":{"savedAmount":"2200.00","remainingAmount":"800.00","monthsLeftCount":4,"requiredMonthlyAmount":"200.00","isBehind":false,"unconvertedCurrencyCodes":[]}}]
    """.trimIndent()

    val receipt = """{"id":"$RECEIPT_ID","receiptSourceKind":"photo","agentAttachmentId":"att-1","mailboxItemId":"","merchantName":"Corner Market","merchantReceiptNumber":"20418","purchasedOn":"2030-11-02","currencyCode":"USD","subtotalAmount":"50.20","totalAmount":"54.20","receiptCheckState":"unbalanced","checkDifferenceAmount":"-4.00",
        "receiptLines":[{"id":"rl1","lineNumber":1,"receiptLineKind":"item","description":"Apples","quantity":"6","quantityUnit":"","unitPriceAmount":"0.89","lineAmount":"5.34"},{"id":"rl2","lineNumber":2,"receiptLineKind":"item","description":"Tomato soup","quantity":"2","quantityUnit":"","unitPriceAmount":"1.99","lineAmount":"3.98"},{"id":"rl3","lineNumber":3,"receiptLineKind":"discount","description":"Member discount","quantity":"","quantityUnit":"","unitPriceAmount":"","lineAmount":"-1.00"}],
        "receiptMatches":[{"receiptId":"$RECEIPT_ID","financeTransactionId":"$TRANSACTION_ID","matchedAmount":"54.20","receiptMatchSource":"receipt_matcher"}]}"""

    private fun person(id: String, name: String) = """{"userId":"$id","username":"${name.lowercase().substringBefore(' ')}","name":"$name","avatarUrl":null,"avatarColorIndex":3}"""

    val sharedAccounts = """
        [{"financeAccountId":"shared-account-1","ownerUserId":"$SHARER_ID","ownerUsername":"sam","ownerName":"Sam Lee","financeShareLevel":"transactions","accountName":"Joint checking","accountMask":"9911","accountKind":"checking","currencyCode":"USD","currentBalance":"3120.50","balanceAt":"2030-11-01T09:00:00Z","creditUsage":null},
         {"financeAccountId":"shared-account-2","ownerUserId":"$SHARER_ID","ownerUsername":"sam","ownerName":"Sam Lee","financeShareLevel":"balance","accountName":"Sam's card","accountMask":"5544","accountKind":"credit","currencyCode":"USD","currentBalance":"-250.00","balanceAt":"2030-11-01T09:00:00Z","creditUsage":{"owedAmount":"250.00","creditLimitAmount":"1000.00","creditLimitSource":"provider","usageShare":0.25}}]
    """.trimIndent()

    val sharedAll = """[{"ownerUserId":"$WHOLE_SHARER_ID","ownerUsername":"alex","ownerName":"Alex Kim","financeShareLevel":"transactions"}]"""

    val sharedSpending = """
        [{"spendingMonth":"2030-11","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"310.40","moneyIn":"0.00","financeTransactionCount":8},{"spendingMonth":"2030-11","spendingCategoryName":"Dining","currencyCode":"USD","moneyOut":"92.00","moneyIn":"0.00","financeTransactionCount":5},
         {"spendingMonth":"2030-11","spendingCategoryName":"Pay","currencyCode":"USD","moneyOut":"0.00","moneyIn":"2800.00","financeTransactionCount":1},{"spendingMonth":"2030-10","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"402.15","moneyIn":"0.00","financeTransactionCount":11}]
    """.trimIndent()

    val sharedTransactions = """{"nextCursor":"","totalCount":2,"financeTransactions":[{"id":"st1","postedOn":"2030-11-02","amount":"-42.17","currencyCode":"USD","merchantName":"Example Grocer","spendingCategoryName":"Groceries"},{"id":"st2","postedOn":"2030-11-01","amount":"2800.00","currencyCode":"USD","merchantName":null,"spendingCategoryName":null}]}"""

    val financeShares = """[{"id":"fs1","financeAccountId":"$CHECKING_ID","accountName":"Checking","accountMask":"0001","granteeUserId":"$SHARER_ID","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending"}]"""

    val allShares = """[{"id":"fa1","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"demo-circle-1","granteeCircleName":"Home","financeShareLevel":"spending","createdAt":"2030-10-30T09:00:00Z"}]"""

    val sources = """[{"id":"demo-source-1","name":"Example Bank","providerKind":"plaid","institutionName":"Example Bank","isEnabled":true,"lastRunAt":"2030-11-13T06:00:00Z","nextRunAt":"2030-11-14T06:00:00Z","lastError":"","isSignInRequired":false,"financeAccounts":[]},
        {"id":"demo-source-2","name":"Index broker","providerKind":"simplefin","institutionName":"Index Broker","isEnabled":true,"lastRunAt":"2030-11-12T06:00:00Z","nextRunAt":"2030-11-14T06:00:00Z","lastError":"The bank asked for a new sign-in.","isSignInRequired":true,"financeAccounts":[]}]"""

    private val figures = """{"wagesAmount":"96000.00","federalIncomeTaxAmount":"11200.00","socialSecurityWagesAmount":"96000.00","socialSecurityTaxAmount":"5952.00","medicareWagesAmount":"96000.00","medicareTaxAmount":"1392.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"4800.00","retirementRothAmount":"0.00","hsaAmount":"1200.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"96000.00","stateIncomeTaxAmount":"3100.00"}]}"""

    val taxYear = """{"taxYear":2030,"socialSecurityWageBaseAmount":"170000.00","retirementDeferralLimitAmount":"24500.00","isYearOver":false,"paystubCount":22,"taxFormCount":1,"figures":$figures,"projectedFigures":$figures,
        "taxYearEmployments":[{"employeeName":"Robin Example","employerName":"Example Payroll","employerTaxId":null,"taxYearFigureSource":"paystubs","taxFormId":null,"latestPayDate":"2030-11-10","paystubCount":22,"payCadenceDayCount":14,"isPaystubYearComplete":false,"remainingPayCount":2,"figures":$figures,"paystubFigures":$figures,"projectedFigures":$figures,
          "taxYearComparisons":[{"boxCode":"12","boxQualifier":"D","boxLabel":"Retirement","w2Amount":"4800.00","paystubAmount":"4600.00","differenceAmount":"200.00","isMatching":false}]}],
        "taxYearPeople":[],"taxYearFindings":[{"taxYearFindingCode":"pay_date_gap","employeeName":"Robin Example","employerName":"Example Payroll","findingText":"A paystub between two pay dates is missing.","paystubIds":["demo-paystub-1"]}],
        "taxYearEstimate":{"taxFilingStatus":"single","isTaxFilingStatusAssumed":true,"isProjected":true,"payAmount":"96000.00","incomeAmount":"96000.00","totalTaxAmount":"21900.00","paidTaxAmount":"18900.00","upcomingWithheldTaxAmount":"2400.00","owingTaxAmount":"600.00","refundAmount":"0.00","remainingIncomeAmount":"74100.00"}}"""

    val paystub = """{"id":"demo-paystub-1","agentAttachmentId":"att-2","employerName":"Example Payroll","employerTaxId":"","employeeName":"Robin Example","employeeTaxIdMask":"1234","payDate":"2030-11-10","periodStartOn":"2030-10-27","periodEndOn":"2030-11-09","currencyCode":"USD","grossAmount":"4000.00","yearToDateGrossAmount":"88000.00","totalTaxAmount":"900.00","totalDeductionAmount":"500.00","netAmount":"2600.00","paystubCheckState":"balanced","checkDifferenceAmount":"0.00",
        "paystubLines":[{"id":"pl1","lineNumber":1,"paystubLineKind":"earning","description":"Regular pay","hours":"80","rateAmount":"50.00","currentAmount":"4000.00","yearToDateAmount":"88000.00"},{"id":"pl2","lineNumber":2,"paystubLineKind":"tax","description":"Federal income tax","currentAmount":"600.00","yearToDateAmount":"13200.00","paystubTaxKind":"federal_income"},{"id":"pl3","lineNumber":3,"paystubLineKind":"deduction","description":"Retirement","currentAmount":"500.00","yearToDateAmount":"11000.00","paystubDeductionKind":"retirement_traditional"}]}"""

    val taxForm = """{"financeTaxForm":{"id":"demo-form-1","agentAttachmentId":"att-3","taxFormKind":"1099_int","taxYear":2030,"isCorrected":false,"payerName":"Sample Bank","payerTaxId":"","recipientName":"Robin Example","recipientTaxIdMask":"4321","taxFormEntries":[{"id":"e1","entryNumber":1,"boxCode":"1","boxQualifier":"","amount":"120.50","textValue":"","isChecked":false}]},"taxFormFindings":[{"taxFormFindingCode":"interest_total","isAgreeing":true,"expectedAmount":"120.50","printedAmount":"120.50","findingText":"The interest adds up."}]}"""
}

/** What demo mode answers for Finance. The most specific document names come first. */
val financeDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("FinanceProviders", """[{"providerKind":"plaid","isBrowserRequired":true},{"providerKind":"simplefin","isBrowserRequired":false}]"""),
    graphQLField("FinanceSources") { _ -> FinanceDemoData.sources },
    graphQLField("FinanceAccounts", FinanceDemoData.accounts),
    graphQLField("CreditUsage", FinanceDemoData.creditUsage),
    graphQLField("SpendingCategories", FinanceDemoData.categories),
    graphQLField("ReadAgent", """{"timezone":"America/Los_Angeles"}"""),
    graphQLField("ReportingCurrency", """{"reportingCurrencyCode":"USD","isChosen":false}"""),
    graphQLField("FinanceTransactions") { variables ->
        val ids = (variables["financeTransactionIds"] as? kotlinx.serialization.json.JsonArray)?.map { element -> (element as kotlinx.serialization.json.JsonPrimitive).content }
        val rows = when {
            ids != null -> FinanceDemoData.transactions.filter { row -> ids.any { id -> row.contains("\"id\":\"$id\"") } }
            variables.text("duplicateOfTransactionId") != null -> emptyList()
            else -> FinanceDemoData.transactions
        }
        """{"financeTransactions":[${rows.joinToString(",")}],"totalCount":${rows.size},"leftOutDuplicateCount":1}"""
    },
    graphQLField("FinanceReceipts") { variables ->
        if (variables.text("financeTransactionId") == FinanceDemoData.TRANSACTION_ID || variables.text("financeTransactionId") == null) """{"financeReceipts":[${FinanceDemoData.receipt}],"totalCount":1}""" else """{"financeReceipts":[],"totalCount":0}"""
    },
    graphQLField("ProposeReceiptMatches", """[{"financeTransactionId":"${FinanceDemoData.TRANSACTION_ID}","matchedAmount":"54.20","isExactAmount":true,"isSameAccount":true,"isMerchantNameShared":true,"dayDistanceCount":0,"financeTransaction":${FinanceDemoData.transactions.first()}}]"""),
    graphQLField("FinanceSpendingSummary", FinanceDemoData.spendingSummary),
    graphQLField("BudgetStatus", FinanceDemoData.budgetStatus),
    graphQLField("SavingSummary", FinanceDemoData.savingSummary),
    graphQLField("CashFlow", FinanceDemoData.cashFlow),
    graphQLField("SpendingByDay", FinanceDemoData.spendingByDay),
    graphQLField("SpendingRules", FinanceDemoData.rules),
    graphQLField("Budgets", FinanceDemoData.budgets),
    graphQLField("NetWorth", FinanceDemoData.netWorth),
    graphQLField("AssetHistory", FinanceDemoData.assetHistory),
    graphQLField("Assets", FinanceDemoData.assets),
    graphQLField("SavingsTargets", FinanceDemoData.savingsTargets),
    graphQLField("TaxYear", FinanceDemoData.taxYear),
    graphQLField("FinancePaystubs", """{"financePaystubs":[${FinanceDemoData.paystub}],"hasMore":false,"totalCount":1}"""),
    graphQLField("FinancePaystub", FinanceDemoData.paystub),
    graphQLField("FinanceTaxForms", """{"taxForms":[${FinanceDemoData.taxForm}],"hasMore":false,"totalCount":1}"""),
    graphQLField("FinanceTaxForm", FinanceDemoData.taxForm),
    graphQLField("TaxDocumentAddress", """{"taxDocumentAddress":"robin+taxes-invented0token01@example.com","canReadPdfs":true}"""),
    graphQLField("StatementImport", """{"sourceId":"source-1","importAddress":"robin+statements-invented0token01@example.com","isEnabled":true,"lastStatementImport":{"importedAt":"2030-11-01T12:00:00Z","statementImportOrigin":"mail","addedTransactionCount":3,"updatedTransactionCount":0,"unchangedTransactionCount":1,"financeAccountNames":["Rewards card"],"importErrorMessage":""}}"""),
    graphQLField("FinanceTrades", """{"financeTrades":[{"id":"trade-1","financeAccountId":"demo-account-brokerage","financeSecurityId":"s1","financeSecurity":{"tickerSymbol":"INVX","securityName":"Invented Index Fund"},"providerTradeId":"p1","tradedOn":"2030-10-02","tradeKind":"buy","tradedQuantity":"2","unitPrice":"100","tradeAmount":"-200","feeAmount":"0","currencyCode":"USD","description":"Bought 2"}],"totalCount":1}"""),
    graphQLField("ListFinanceAccountShares", FinanceDemoData.financeShares),
    graphQLField("ListFinanceShares", FinanceDemoData.allShares),
    graphQLField("ListSharedFinanceAccounts", FinanceDemoData.sharedAccounts),
    graphQLField("ListSharedFinanceSpending", FinanceDemoData.sharedSpending),
    graphQLField("ListSharedFinanceTransactions", FinanceDemoData.sharedTransactions),
    graphQLField("ListSharedFinance", FinanceDemoData.sharedAll),
    graphQLField("ProposeSpendingRules", """{"spendingRuleProposals":[],"tooGenericMatchTextCount":0,"changingNumberMatchTextCount":0,"overLimitMatchTextCount":0}"""),
)
