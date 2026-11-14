package com.teanode.kit.finance

import kotlinx.serialization.Serializable

/**
 * FinanceShareLevel is how much of a finance account somebody sees. Each
 * level shows what the one before it shows.
 */
enum class FinanceShareLevel(val rawValue: String) {
    Balance("balance"),
    Spending("spending"),
    Transactions("transactions");
}

/**
 * FinanceShareView is one share of one of the caller's finance accounts:
 * with a person or with a circle, never both.
 */
@Serializable
data class FinanceShareView(
    val id: String,
    val financeAccountId: String,
    val accountName: String,
    val accountMask: String? = null,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    val financeShareLevel: String,
)

/**
 * AllFinanceShareView is one share of all of the caller's finance: with a
 * person or with a circle, never both. It covers every account the caller has
 * now and links later, so it names none. Its levels are spending and
 * transactions; there is no balance level, since a balance belongs to one
 * account.
 */
@Serializable
data class AllFinanceShareView(
    val id: String,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    val financeShareLevel: String,
    val createdAt: String? = null,
)

/**
 * SharedAllFinance is a person who shares all of their finance with the
 * caller, and at which level. It names no account.
 */
@Serializable
data class SharedAllFinance(
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String? = null,
    val financeShareLevel: String,
)

/**
 * SharedCreditUsage is what a shared credit card owes against its limit, in
 * the card's own currency.
 */
@Serializable
data class SharedCreditUsage(
    val owedAmount: String? = null,
    val creditLimitAmount: String? = null,
    val creditLimitSource: String? = null,
    /** 0.25 is a quarter of the limit used. */
    val usageShare: Double? = null,
)

/**
 * SharedFinanceAccount is somebody else's finance account as the level it is
 * shared at shows it.
 */
@Serializable
data class SharedFinanceAccount(
    val financeAccountId: String,
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String? = null,
    val financeShareLevel: String,
    val accountName: String,
    val accountMask: String? = null,
    val accountKind: String? = null,
    val currencyCode: String,
    val currentBalance: String? = null,
    val balanceAt: String? = null,
    val creditUsage: SharedCreditUsage? = null,
)

/**
 * SharedFinanceSpendingRow is one month of one spending category of a shared
 * account, in one currency. The category is empty for transactions with none.
 */
@Serializable
data class SharedFinanceSpendingRow(
    /** "2026-09". */
    val spendingMonth: String,
    val spendingCategoryName: String,
    val currencyCode: String,
    val moneyOut: String,
    val moneyIn: String,
    val financeTransactionCount: Int,
)

/**
 * SharedFinanceTransaction is a transaction of a shared account as a reader
 * at the transactions level sees it: no description, no notes, no receipts.
 */
@Serializable
data class SharedFinanceTransaction(
    val id: String,
    val postedOn: String,
    val amount: String,
    val currencyCode: String,
    val merchantName: String? = null,
    val spendingCategoryName: String? = null,
)

@Serializable
data class SharedFinanceTransactionPage(
    val financeTransactions: List<SharedFinanceTransaction>,
    val nextCursor: String? = null,
    val totalCount: Int,
)
