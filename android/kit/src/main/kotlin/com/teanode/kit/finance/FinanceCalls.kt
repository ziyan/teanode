package com.teanode.kit.finance

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.graphQLCall
import com.teanode.kit.graphQLField
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.time.ZoneId

// What the app asks of the server about finance: the same calls as Finance.swift and its siblings.

private fun text(value: String): JsonElement = JsonPrimitive(value)

private fun number(value: Int): JsonElement = JsonPrimitive(value)

private fun flag(value: Boolean): JsonElement = JsonPrimitive(value)

private fun strings(values: List<String>): JsonElement = JsonArray(values.map { value -> JsonPrimitive(value) })

private fun orNull(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

/** Whether finance is offered: a provider to link, or a source linked. */
suspend fun Connection.isFinanceShown(): Boolean {
    val data: JsonObject = execute(FinanceDocuments.FINANCE_PRESENCE)
    val providers = data["FinanceProviders"] as? JsonArray
    val sources = data["FinanceSources"] as? JsonArray
    return providers.orEmpty().any { element -> element !is JsonNull } || sources.orEmpty().any { element -> element !is JsonNull }
}

/** The time zone the person's days are counted in. */
suspend fun Connection.financeZone(): ZoneId {
    val data: JsonObject = execute(FinanceDocuments.FINANCE_ZONE)
    val name = (data["ReadAgent"] as? JsonObject)?.get("timezone")?.jsonPrimitive?.contentOrNull ?: ""
    return try {
        ZoneId.of(name)
    } catch (_: Exception) {
        ZoneId.systemDefault()
    }
}

suspend fun Connection.financeSources(): List<FinanceSource> =
    graphQLField<List<FinanceSource?>>(FinanceDocuments.FINANCE_SOURCES, "FinanceSources").orEmpty().filterNotNull()

suspend fun Connection.financeAccounts(): List<FinanceAccount> =
    graphQLField<List<FinanceAccount?>>(FinanceDocuments.FINANCE_ACCOUNTS, "FinanceAccounts").orEmpty().filterNotNull()

suspend fun Connection.creditUsage(): CreditUsage? = graphQLField(FinanceDocuments.CREDIT_USAGE, "CreditUsage")

suspend fun Connection.financeTransactions(
    filter: FinanceTransactionFilter,
    ids: List<String>? = null,
    duplicatesOf: String? = null,
    limit: Int = 50,
    offset: Int = 0,
): FinanceTransactionPage {
    val variables = filter.variables.toMutableMap()
    variables["limit"] = number(limit)
    variables["offset"] = number(offset)
    if (ids != null) variables["financeTransactionIds"] = strings(ids)
    if (duplicatesOf != null) variables["duplicateOfTransactionId"] = text(duplicatesOf)
    return graphQLField<FinanceTransactionPage>(FinanceDocuments.FINANCE_TRANSACTIONS, "FinanceTransactions", variables)
        ?: FinanceTransactionPage(emptyList())
}

/**
 * Files a transaction under a category; with a rule, every later one from the merchant too. Returns the rule's words
 * when one was made.
 */
suspend fun Connection.categorize(transactionId: String, categoryId: String, isMakingRule: Boolean): String? {
    val variables = mutableMapOf("financeTransactionId" to text(transactionId), "spendingCategoryId" to text(categoryId))
    if (isMakingRule) variables["shouldCreateSpendingRule"] = flag(true)
    val categorized = graphQLField<JsonObject>(FinanceDocuments.CATEGORIZE_TRANSACTION, "CategorizeTransaction", variables)
    return (categorized?.get("spendingRule") as? JsonObject)?.get("matchText")?.jsonPrimitive?.contentOrNull
}

/** Files several transactions under one category, 500 at a time. */
suspend fun Connection.categorize(transactionIds: List<String>, categoryId: String) {
    for (chunk in transactionIds.chunked(500)) {
        graphQLCall(
            FinanceDocuments.CATEGORIZE_TRANSACTIONS,
            mapOf("financeTransactionIds" to strings(chunk), "spendingCategoryId" to text(categoryId)),
        )
    }
}

/** Counts a transaction marked a duplicate, or lets detection decide again. */
suspend fun Connection.count(transactionId: String, isUndone: Boolean) {
    graphQLCall(
        if (isUndone) FinanceDocuments.UNDO_COUNT_TRANSACTION else FinanceDocuments.COUNT_TRANSACTION,
        mapOf("financeTransactionId" to text(transactionId)),
    )
}

suspend fun Connection.annotate(transactionId: String, annotation: String) {
    graphQLCall(
        FinanceDocuments.ANNOTATE_TRANSACTION,
        mapOf("financeTransactionId" to text(transactionId), "annotation" to text(annotation)),
    )
}

suspend fun Connection.financeReceipts(
    transactionId: String? = null,
    from: String = "",
    to: String = "",
    isUnmatched: Boolean = false,
    isUndated: Boolean = false,
    searchText: String = "",
    limit: Int = 50,
    offset: Int = 0,
): FinanceReceiptPage {
    val variables = mutableMapOf("limit" to number(limit), "offset" to number(offset))
    if (transactionId != null) variables["financeTransactionId"] = text(transactionId)
    if (from.isNotEmpty()) variables["from"] = text(from)
    if (to.isNotEmpty()) variables["to"] = text(to)
    if (isUnmatched) variables["isUnmatched"] = flag(true)
    if (isUndated) variables["isUndated"] = flag(true)
    if (searchText.isNotEmpty()) variables["text"] = text(searchText)
    return graphQLField<FinanceReceiptPage>(FinanceDocuments.FINANCE_RECEIPTS, "FinanceReceipts", variables) ?: FinanceReceiptPage(emptyList())
}

suspend fun Connection.proposeReceiptMatches(receiptId: String): List<ReceiptMatchProposal> =
    graphQLField<List<ReceiptMatchProposal?>>(FinanceDocuments.PROPOSE_RECEIPT_MATCHES, "ProposeReceiptMatches", mapOf("receiptId" to text(receiptId)))
        .orEmpty().filterNotNull()

suspend fun Connection.matchReceipt(receiptId: String, transactionId: String, amount: String?, isUndone: Boolean = false) {
    val variables = mutableMapOf("receiptId" to text(receiptId), "financeTransactionId" to text(transactionId))
    if (!isUndone && amount != null) variables["matchedAmount"] = text(amount)
    graphQLCall(if (isUndone) FinanceDocuments.UNMATCH_RECEIPT else FinanceDocuments.MATCH_RECEIPT, variables)
}

suspend fun Connection.deleteReceipt(receiptId: String) {
    graphQLCall(FinanceDocuments.DELETE_RECEIPT, mapOf("receiptId" to text(receiptId)))
}

/** Has the agent read a receipt: an uploaded picture, or a mail; for a charge, it is matched to it. */
suspend fun Connection.readReceipt(attachmentId: String? = null, mailboxItemId: String? = null, transactionId: String? = null) {
    val variables = mutableMapOf<String, JsonElement>()
    if (attachmentId != null) variables["agentAttachmentId"] = text(attachmentId)
    if (mailboxItemId != null) variables["mailboxItemId"] = text(mailboxItemId)
    if (transactionId != null) variables["financeTransactionId"] = text(transactionId)
    graphQLCall(FinanceDocuments.READ_RECEIPT, variables)
}

suspend fun Connection.spendingSummary(from: String, to: String, groupBy: String): SpendingSummary? =
    graphQLField(FinanceDocuments.SPENDING_SUMMARY, "FinanceSpendingSummary", mapOf("from" to text(from), "to" to text(to), "groupBy" to text(groupBy)))

suspend fun Connection.budgetStatus(month: String): BudgetStatus? =
    graphQLField(FinanceDocuments.BUDGET_STATUS, "BudgetStatus", mapOf("month" to text(month)))

/** A year's budgets, each adding up the months that had it as it was then, over the year so far or the whole of a year that is over. */
suspend fun Connection.budgetStatusOfYear(year: String): BudgetStatus? =
    graphQLField(FinanceDocuments.BUDGET_STATUS, "BudgetStatus", mapOf("year" to text(year)))

suspend fun Connection.savingSummary(month: String): SavingSummary? =
    graphQLField(FinanceDocuments.SAVING_SUMMARY, "SavingSummary", mapOf("month" to text(month)))

/** A year's saving, its months added up as each was budgeted. */
suspend fun Connection.savingSummaryOfYear(year: String): SavingSummary? =
    graphQLField(FinanceDocuments.SAVING_SUMMARY, "SavingSummary", mapOf("year" to text(year)))

/** A month's spending day by day, beside the month it is compared with. */
suspend fun Connection.spendingByDay(month: String, compareMonth: String): SpendingByDay? =
    graphQLField(FinanceDocuments.SPENDING_BY_DAY, "SpendingByDay", mapOf("month" to text(month), "compareMonth" to text(compareMonth)))

suspend fun Connection.cashFlow(fromMonth: String, toMonth: String): CashFlow? =
    graphQLField(FinanceDocuments.CASH_FLOW, "CashFlow", mapOf("fromMonth" to text(fromMonth), "toMonth" to text(toMonth)))

suspend fun Connection.spendingCategories(): List<SpendingCategory> =
    graphQLField<List<SpendingCategory?>>(FinanceDocuments.SPENDING_CATEGORIES, "SpendingCategories").orEmpty().filterNotNull()

/** Adds a category, or changes one by its id. */
suspend fun Connection.saveSpendingCategory(categoryId: String?, name: String, parentId: String, isIncome: Boolean, isHidden: Boolean) {
    val variables = mutableMapOf(
        "spendingCategoryName" to text(name), "parentSpendingCategoryId" to text(parentId),
        "isIncome" to flag(isIncome), "isHidden" to flag(isHidden),
    )
    if (categoryId != null) variables["spendingCategoryId"] = text(categoryId)
    graphQLCall(if (categoryId == null) FinanceDocuments.CREATE_SPENDING_CATEGORY else FinanceDocuments.UPDATE_SPENDING_CATEGORY, variables)
}

suspend fun Connection.deleteSpendingCategory(categoryId: String) {
    graphQLCall(FinanceDocuments.DELETE_SPENDING_CATEGORY, mapOf("spendingCategoryId" to text(categoryId)))
}

suspend fun Connection.spendingRules(): List<SpendingRule> =
    graphQLField<List<SpendingRule?>>(FinanceDocuments.SPENDING_RULES, "SpendingRules").orEmpty().filterNotNull()

/** Adds a rule, or changes one by its id; blank fields are sent empty. */
suspend fun Connection.saveSpendingRule(
    ruleId: String?,
    matchText: String,
    categoryId: String,
    accountId: String,
    minimum: String,
    maximum: String,
    priority: Int?,
) {
    val variables = mutableMapOf(
        "matchText" to text(matchText), "spendingCategoryId" to text(categoryId), "financeAccountId" to text(accountId),
        "minimumAmount" to text(minimum), "maximumAmount" to text(maximum),
    )
    if (priority != null) variables["rulePriority"] = number(priority)
    if (ruleId != null) variables["spendingRuleId"] = text(ruleId)
    graphQLCall(if (ruleId == null) FinanceDocuments.CREATE_SPENDING_RULE else FinanceDocuments.UPDATE_SPENDING_RULE, variables)
}

suspend fun Connection.deleteSpendingRule(ruleId: String) {
    graphQLCall(FinanceDocuments.DELETE_SPENDING_RULE, mapOf("spendingRuleId" to text(ruleId)))
}

suspend fun Connection.budgets(): List<Budget> =
    graphQLField<List<Budget?>>(FinanceDocuments.BUDGETS, "Budgets").orEmpty().filterNotNull()

/** Sets a category's monthly budget from a month on; zero ends it. */
suspend fun Connection.setBudget(categoryId: String, monthlyAmount: String, currencyCode: String?, effectiveFrom: String) {
    val variables = mutableMapOf(
        "spendingCategoryId" to text(categoryId), "monthlyAmount" to text(monthlyAmount), "effectiveFrom" to text(effectiveFrom),
    )
    if (!currencyCode.isNullOrEmpty()) variables["currencyCode"] = text(currencyCode)
    graphQLCall(FinanceDocuments.SET_BUDGET, variables)
}

suspend fun Connection.netWorth(from: String, to: String): NetWorth? =
    graphQLField(FinanceDocuments.NET_WORTH, "NetWorth", mapOf("from" to text(from), "to" to text(to)))

suspend fun Connection.assets(): List<FinanceAsset> =
    graphQLField<List<FinanceAsset?>>(FinanceDocuments.ASSETS, "Assets").orEmpty().filterNotNull()

suspend fun Connection.assetHistory(assetId: String): List<FinanceAsset.Valuation> {
    val history = graphQLField<JsonObject>(FinanceDocuments.ASSET_HISTORY, "AssetHistory", mapOf("assetId" to text(assetId))) ?: return emptyList()
    val valuations = history["assetValuations"] as? JsonArray ?: return emptyList()
    return valuations.filter { element -> element !is JsonNull }
        .map { element -> TeaNodeJson.decodeFromJsonElement(FinanceAsset.Valuation.serializer(), element) }
}

/** Adds an asset with its first value, or changes one by its id. */
suspend fun Connection.saveAsset(assetId: String?, fields: Map<String, JsonElement>) {
    val variables = fields.toMutableMap()
    if (assetId != null) variables["assetId"] = text(assetId)
    graphQLCall(if (assetId == null) FinanceDocuments.CREATE_ASSET else FinanceDocuments.UPDATE_ASSET, variables)
}

suspend fun Connection.closeAsset(assetId: String, closedOn: String?, isReopened: Boolean) {
    val variables = mutableMapOf("assetId" to text(assetId))
    if (isReopened) variables["shouldReopen"] = flag(true) else if (closedOn != null) variables["closedOn"] = text(closedOn)
    graphQLCall(FinanceDocuments.CLOSE_ASSET, variables)
}

suspend fun Connection.deleteAsset(assetId: String) {
    graphQLCall(FinanceDocuments.DELETE_ASSET, mapOf("assetId" to text(assetId)))
}

suspend fun Connection.recordValuation(assetId: String, value: String, valuedOn: String, note: String) {
    graphQLCall(
        FinanceDocuments.RECORD_VALUATION,
        mapOf("assetId" to text(assetId), "value" to text(value), "valuedOn" to text(valuedOn), "valuationNote" to text(note)),
    )
}

suspend fun Connection.deleteValuation(valuationId: String) {
    graphQLCall(FinanceDocuments.DELETE_VALUATION, mapOf("valuationId" to text(valuationId)))
}

suspend fun Connection.savingsTargets(): List<SavingsTarget> =
    graphQLField<List<SavingsTarget?>>(FinanceDocuments.SAVINGS_TARGETS, "SavingsTargets").orEmpty().filterNotNull()

/** Adds a savings target, or changes one by its id. */
suspend fun Connection.saveSavingsTarget(targetId: String?, fields: Map<String, JsonElement>) {
    val variables = fields.toMutableMap()
    if (targetId != null) variables["savingsTargetId"] = text(targetId)
    graphQLCall(if (targetId == null) FinanceDocuments.CREATE_SAVINGS_TARGET else FinanceDocuments.UPDATE_SAVINGS_TARGET, variables)
}

suspend fun Connection.closeSavingsTarget(targetId: String, isReopened: Boolean) {
    val variables = mutableMapOf("savingsTargetId" to text(targetId))
    if (isReopened) variables["shouldReopen"] = flag(true)
    graphQLCall(FinanceDocuments.CLOSE_SAVINGS_TARGET, variables)
}

/** The currency totals are converted to, and whether the person chose it. */
data class ReportingCurrency(val currencyCode: String, val isChosen: Boolean)

suspend fun Connection.reportingCurrency(): ReportingCurrency {
    val currency = graphQLField<JsonObject>(FinanceDocuments.REPORTING_CURRENCY, "ReportingCurrency")
    return ReportingCurrency(
        currency?.get("reportingCurrencyCode")?.jsonPrimitive?.contentOrNull ?: "",
        currency?.get("isChosen")?.jsonPrimitive?.contentOrNull == "true",
    )
}

/** Chooses the reporting currency; empty clears the choice. */
suspend fun Connection.setReportingCurrency(currencyCode: String) {
    graphQLCall(FinanceDocuments.SET_REPORTING_CURRENCY, mapOf("currencyCode" to text(currencyCode)))
}

suspend fun Connection.convert(amount: String, from: String, to: String, on: String? = null): FinanceConversion? {
    val variables = mutableMapOf("amount" to text(amount), "fromCurrencyCode" to text(from), "toCurrencyCode" to text(to))
    if (on != null) variables["rateOn"] = text(on)
    return graphQLField(FinanceDocuments.CONVERT_CURRENCY, "ConvertCurrency", variables)
}

suspend fun Connection.syncFinanceSource(sourceId: String) {
    graphQLCall(FinanceDocuments.SYNC_FINANCE_SOURCE, mapOf("sourceId" to text(sourceId)))
}

suspend fun Connection.deleteFinanceSource(sourceId: String) {
    graphQLCall(FinanceDocuments.DELETE_FINANCE_SOURCE, mapOf("sourceId" to text(sourceId)))
}

suspend fun Connection.switchFinanceSource(sourceId: String, isEnabled: Boolean) {
    graphQLCall(FinanceDocuments.SWITCH_FINANCE_SOURCE, mapOf("sourceId" to text(sourceId), "enabled" to flag(isEnabled)))
}

/** Links a bank through SimpleFIN. The setup token works once, so this is never retried on its own. */
suspend fun Connection.linkSimpleFIN(setupToken: String) {
    graphQLCall(FinanceDocuments.LINK_SIMPLE_FIN, mapOf("setupToken" to text(setupToken)))
}

// MARK: Typed-in receipts, paystubs and tax forms

/**
 * What recording a typed-in receipt would do, writing nothing: whether its lines add up, the charges it could
 * explain, and whether it replaces one read from the same file.
 */
suspend fun Connection.previewRecordReceipt(draft: ReceiptDraft, attachmentId: String): ReceiptEntryPreview? =
    graphQLField(FinanceEntryDocuments.PREVIEW_RECORD_RECEIPT, "PreviewRecordReceipt", draft.variables(attachmentId, isRecording = false))

/** Records a typed-in receipt and matches it to the charge the matcher is sure of. */
suspend fun Connection.recordReceipt(draft: ReceiptDraft, attachmentId: String): RecordedReceipt? =
    graphQLField(FinanceEntryDocuments.RECORD_RECEIPT, "RecordReceipt", draft.variables(attachmentId, isRecording = true))

suspend fun Connection.recordPaystub(draft: PaystubDraft, attachmentId: String): FinancePaystub? =
    graphQLField(FinanceEntryDocuments.RECORD_PAYSTUB, "RecordPaystub", draft.variables(attachmentId))

suspend fun Connection.recordTaxForm(draft: TaxFormDraft, attachmentId: String): FinanceTaxFormView? =
    graphQLField(FinanceEntryDocuments.RECORD_TAX_FORM, "RecordTaxForm", draft.variables(attachmentId))

/** Today's rate between two currencies. */
suspend fun Connection.exchangeRate(fromCurrencyCode: String, toCurrencyCode: String): CurrencyRate? =
    graphQLField(
        FinanceEntryDocuments.EXCHANGE_RATE, "ExchangeRate",
        mapOf("fromCurrencyCode" to text(fromCurrencyCode.uppercase()), "toCurrencyCode" to text(toCurrencyCode.uppercase())),
    )

// MARK: Tax

/** A tax year, its tax estimated as filed with [filingStatus], or as the server guesses from the number of people when it is null. */
suspend fun Connection.taxYear(year: Int, filingStatus: TaxFilingStatus? = null): TaxYearSummary? {
    val variables = mutableMapOf("taxYear" to number(year))
    if (filingStatus != null) variables["taxFilingStatus"] = text(filingStatus.rawValue)
    return graphQLField(FinanceTaxDocuments.TAX_YEAR, "TaxYear", variables)
}

/** Changes a year's settings; what the change leaves null stays. */
suspend fun Connection.setTaxYearSetting(taxYear: Int, change: TaxYearSettingChange): TaxYearSetting? {
    val variables = change.variables.toMutableMap()
    variables["taxYear"] = number(taxYear)
    return graphQLField(FinanceTaxDocuments.SET_TAX_YEAR_SETTING, "SetTaxYearSetting", variables)
}

/** The address to mail paystubs and W-2s to, made the first time. */
suspend fun Connection.taxDocumentAddress(): TaxDocumentAddress? = graphQLField(FinanceTaxDocuments.TAX_DOCUMENT_ADDRESS, "TaxDocumentAddress")

/** A new tax document address; the old one stops taking mail at once. */
suspend fun Connection.regenerateTaxDocumentAddress(): TaxDocumentAddress? =
    graphQLField(FinanceTaxDocuments.REGENERATE_TAX_DOCUMENT_ADDRESS, "RegenerateTaxDocumentAddress")

/** Keeps how the person files, which every tax year's estimate is worked out for; null goes back to assuming one. */
suspend fun Connection.setTaxFilingStatus(filingStatus: TaxFilingStatus?) {
    graphQLCall(FinanceTaxDocuments.SET_TAX_FILING_STATUS, mapOf("taxFilingStatus" to text(filingStatus?.rawValue ?: "")))
}

suspend fun Connection.financePaystubs(taxYear: Int, limit: Int = 200, offset: Int = 0): FinancePaystubPage =
    graphQLField<FinancePaystubPage>(
        FinanceTaxDocuments.FINANCE_PAYSTUBS, "FinancePaystubs",
        mapOf("taxYear" to number(taxYear), "limit" to number(limit), "offset" to number(offset)),
    ) ?: FinancePaystubPage(emptyList())

suspend fun Connection.financePaystub(paystubId: String): FinancePaystub? =
    graphQLField(FinanceTaxDocuments.FINANCE_PAYSTUB, "FinancePaystub", mapOf("paystubId" to text(paystubId)))

suspend fun Connection.financeTaxForms(taxYear: Int, limit: Int = 200, offset: Int = 0): FinanceTaxFormPage =
    graphQLField<FinanceTaxFormPage>(
        FinanceTaxDocuments.FINANCE_TAX_FORMS, "FinanceTaxForms",
        mapOf("taxYear" to number(taxYear), "limit" to number(limit), "offset" to number(offset)),
    ) ?: FinanceTaxFormPage(emptyList())

suspend fun Connection.financeTaxForm(taxFormId: String): FinanceTaxFormView? =
    graphQLField(FinanceTaxDocuments.FINANCE_TAX_FORM, "FinanceTaxForm", mapOf("taxFormId" to text(taxFormId)))

/** Has the agent read an upload as a paystub or a tax form. The upload must have been handed in as a tax document. */
suspend fun Connection.readTaxDocumentOfAttachment(attachmentId: String): TaxDocumentReading? =
    graphQLField(FinanceTaxDocuments.READ_TAX_DOCUMENT, "ReadTaxDocument", mapOf("agentAttachmentId" to text(attachmentId)))

/** Has the agent read each PDF and picture a message carries as a paystub or a tax form. */
suspend fun Connection.readTaxDocumentOfMail(mailboxItemId: String): TaxDocumentReading? =
    graphQLField(FinanceTaxDocuments.READ_TAX_DOCUMENT, "ReadTaxDocument", mapOf("mailboxItemId" to text(mailboxItemId)))

suspend fun Connection.deletePaystub(paystubId: String) {
    graphQLCall(FinanceTaxDocuments.DELETE_PAYSTUB, mapOf("paystubId" to text(paystubId)))
}

suspend fun Connection.deleteTaxForm(taxFormId: String) {
    graphQLCall(FinanceTaxDocuments.DELETE_TAX_FORM, mapOf("taxFormId" to text(taxFormId)))
}

// MARK: Sharing

/** The caller's own shares of their finance accounts. */
suspend fun Connection.financeShares(): List<FinanceShareView> =
    graphQLField<List<FinanceShareView>>(FinanceSharingDocuments.FINANCE_SHARES, "ListFinanceAccountShares").orEmpty()

/** Shares an account with a person or a circle (one of the two) at a level, or changes the level of a share already made. */
suspend fun Connection.shareFinanceAccount(financeAccountId: String, target: ShareTarget, level: FinanceShareLevel): FinanceShareView? =
    graphQLField(
        FinanceSharingDocuments.SHARE_FINANCE_ACCOUNT, "ShareFinanceAccount",
        mapOf(
            "financeAccountId" to text(financeAccountId), "financeShareLevel" to text(level.rawValue),
            "granteeUserId" to orNull(target.granteeUserId), "granteeCircleId" to orNull(target.granteeCircleId),
        ),
    )

/** Takes a share back. */
suspend fun Connection.unshareFinanceAccount(shareId: String) {
    graphQLCall(FinanceSharingDocuments.UNSHARE_FINANCE_ACCOUNT, mapOf("shareId" to text(shareId)))
}

/** The finance accounts other people share with the caller, of one person when their identifier is given. */
suspend fun Connection.sharedFinanceAccounts(ownerUserId: String? = null): List<SharedFinanceAccount> =
    graphQLField<List<SharedFinanceAccount>>(
        FinanceSharingDocuments.SHARED_FINANCE_ACCOUNTS, "ListSharedFinanceAccounts", mapOf("ownerUserId" to orNull(ownerUserId)),
    ).orEmpty()

/** A shared account's money out and in by month and spending category. */
suspend fun Connection.sharedFinanceSpending(financeAccountId: String): List<SharedFinanceSpendingRow> =
    graphQLField<List<SharedFinanceSpendingRow>>(
        FinanceSharingDocuments.SHARED_FINANCE_SPENDING, "ListSharedFinanceSpending", mapOf("financeAccountId" to text(financeAccountId)),
    ).orEmpty()

/** A page of a shared account's transactions, newest first. */
suspend fun Connection.sharedFinanceTransactions(financeAccountId: String, limit: Int = 50, offset: Int = 0): SharedFinanceTransactionPage =
    graphQLField<SharedFinanceTransactionPage>(
        FinanceSharingDocuments.SHARED_FINANCE_TRANSACTIONS, "ListSharedFinanceTransactions",
        mapOf("financeAccountId" to text(financeAccountId), "limit" to number(limit), "offset" to number(offset)),
    ) ?: SharedFinanceTransactionPage(emptyList(), null, 0)

/** The caller's own shares of all their finance. */
suspend fun Connection.allFinanceShares(): List<AllFinanceShareView> =
    graphQLField<List<AllFinanceShareView>>(FinanceSharingDocuments.ALL_FINANCE_SHARES, "ListFinanceShares").orEmpty()

/** Shares all of the caller's finance with a person or a circle (one of the two) at the spending or transactions level, or changes the level of a share already made. */
suspend fun Connection.shareAllFinance(target: ShareTarget, level: FinanceShareLevel): AllFinanceShareView? =
    graphQLField(
        FinanceSharingDocuments.SHARE_ALL_FINANCE, "ShareFinance",
        mapOf(
            "financeShareLevel" to text(level.rawValue),
            "granteeUserId" to orNull(target.granteeUserId), "granteeCircleId" to orNull(target.granteeCircleId),
        ),
    )

/** Takes a share of all finance back. */
suspend fun Connection.unshareAllFinance(financeShareId: String) {
    graphQLCall(FinanceSharingDocuments.UNSHARE_ALL_FINANCE, mapOf("financeShareId" to text(financeShareId)))
}

/** The people who share all of their finance with the caller, or the one whose identifier is given. */
suspend fun Connection.sharedAllFinance(ownerUserId: String? = null): List<SharedAllFinance> =
    graphQLField<List<SharedAllFinance>>(
        FinanceSharingDocuments.SHARED_ALL_FINANCE, "ListSharedFinance", mapOf("ownerUserId" to orNull(ownerUserId)),
    ).orEmpty()

/** The money out and in by month and spending category across all of a person's finance together, in each currency. */
suspend fun Connection.sharedAllFinanceSpending(ownerUserId: String): List<SharedFinanceSpendingRow> =
    graphQLField<List<SharedFinanceSpendingRow>>(
        FinanceSharingDocuments.SHARED_ALL_FINANCE_SPENDING, "ListSharedFinanceSpending", mapOf("ownerUserId" to text(ownerUserId)),
    ).orEmpty()

/** A page of the transactions across all of a person's finance together, newest first. */
suspend fun Connection.sharedAllFinanceTransactions(ownerUserId: String, limit: Int = 50, offset: Int = 0): SharedFinanceTransactionPage =
    graphQLField<SharedFinanceTransactionPage>(
        FinanceSharingDocuments.SHARED_ALL_FINANCE_TRANSACTIONS, "ListSharedFinanceTransactions",
        mapOf("ownerUserId" to text(ownerUserId), "limit" to number(limit), "offset" to number(offset)),
    ) ?: SharedFinanceTransactionPage(emptyList(), null, 0)

// MARK: Managing

/** An account's trades, newest first, a page at a time; narrowed to one security when it is given. */
suspend fun Connection.financeTrades(financeAccountId: String, financeSecurityId: String? = null, limit: Int = 50, offset: Int = 0): FinanceTradePage {
    val variables = mutableMapOf("financeAccountId" to text(financeAccountId), "limit" to number(limit), "offset" to number(offset))
    if (financeSecurityId != null) variables["financeSecurityId"] = text(financeSecurityId)
    return graphQLField<FinanceTradePage>(FinanceManageDocuments.FINANCE_TRADES, "FinanceTrades", variables) ?: FinanceTradePage(emptyList())
}

/**
 * Gives an account of imported statements the person's own name and, when given, last digits; later imports keep
 * them. A null is left as it is, an empty mask takes the statements' own number back.
 */
suspend fun Connection.renameStatementAccount(accountId: String, accountName: String?, accountMask: String?): FinanceAccount? {
    val variables = mutableMapOf("financeAccountId" to text(accountId))
    if (accountName != null) variables["accountName"] = text(accountName)
    if (accountMask != null) variables["accountMask"] = text(accountMask)
    return graphQLField(FinanceManageDocuments.RENAME_STATEMENT_ACCOUNT, "RenameStatementAccount", variables)
}

/** Deletes an account of imported statements with its transactions and its net worth history. */
suspend fun Connection.deleteStatementAccount(accountId: String): StatementAccountDeleted? =
    graphQLField(FinanceManageDocuments.DELETE_STATEMENT_ACCOUNT, "DeleteStatementAccount", mapOf("financeAccountId" to text(accountId)))

/** The spending rules to offer for transactions filed under a category. Saves nothing. */
suspend fun Connection.proposeSpendingRules(transactionIds: List<String>, categoryId: String): SpendingRuleProposals? =
    graphQLField(
        FinanceManageDocuments.PROPOSE_SPENDING_RULES, "ProposeSpendingRules",
        mapOf("financeTransactionIds" to strings(transactionIds), "spendingCategoryId" to text(categoryId)),
    )

/** The kinds of provider the server offers: "plaid", "simplefin". */
suspend fun Connection.financeProviderKinds(): List<String> =
    graphQLField<List<FinanceProvider?>>(FinanceManageDocuments.FINANCE_PROVIDER_KINDS, "FinanceProviders").orEmpty().mapNotNull { provider -> provider?.providerKind }

/** Brings in a provider connection made elsewhere: a Plaid access token or a SimpleFIN access URL already claimed. Never retried on its own. */
suspend fun Connection.importFinanceCredential(providerKind: String, credential: String, institutionName: String?) {
    val variables = mutableMapOf("providerKind" to text(providerKind), "credential" to text(credential))
    if (institutionName != null) variables["institutionName"] = text(institutionName)
    graphQLCall(FinanceManageDocuments.IMPORT_FINANCE_CREDENTIAL, variables)
}

/** What importing these rows would do. Nothing is written. */
suspend fun Connection.previewImportTransactions(variables: Map<String, JsonElement>): TransactionRowsPreview? =
    graphQLField(FinanceManageDocuments.PREVIEW_IMPORT_TRANSACTIONS, "PreviewImportTransactions", variables)

/** Imports the rows, and answers what the import did. */
suspend fun Connection.importTransactions(variables: Map<String, JsonElement>): StatementImport.LastImport? =
    graphQLField(FinanceManageDocuments.IMPORT_TRANSACTIONS, "ImportTransactions", variables)

// MARK: Statements

/** Imports an OFX statement (.ofx, .qfx or .qbo) already uploaded to the agent's attachments, and answers what the import did. */
suspend fun Connection.importStatement(attachmentId: String): StatementImport.LastImport? =
    graphQLField(FinanceStatementDocuments.IMPORT_STATEMENT, "ImportStatement", mapOf("agentAttachmentId" to text(attachmentId)))

/** The statement import, its address made the first time it is asked. */
suspend fun Connection.statementImport(): StatementImport? = graphQLField(FinanceStatementDocuments.STATEMENT_IMPORT, "StatementImport")

/** A new statement import address; mail to the old one is refused from now on. */
suspend fun Connection.regenerateStatementImportAddress(): StatementImport? =
    graphQLField(FinanceStatementDocuments.REGENERATE_STATEMENT_IMPORT_ADDRESS, "RegenerateStatementImportAddress")
