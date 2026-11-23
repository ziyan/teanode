package com.teanode.app.finance

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.kit.Connection
import com.teanode.kit.finance.FinanceAccount
import com.teanode.kit.finance.FinanceCategories
import com.teanode.kit.finance.SpendingCategory
import com.teanode.kit.finance.financeAccounts
import com.teanode.kit.finance.financeZone
import com.teanode.kit.finance.reportingCurrency
import com.teanode.kit.finance.spendingCategories
import kotlinx.coroutines.CancellationException
import java.time.LocalDate
import java.time.YearMonth
import java.time.ZoneId

/** What every Finance screen needs to say what a charge is: the categories, the accounts, the zone days are counted in and the currency totals are in. */
internal class FinanceBase(
    val categories: List<SpendingCategory>,
    val accounts: List<FinanceAccount>,
    val reportingCurrency: String,
    val zone: ZoneId,
    private val context: Context,
) {
    fun category(id: String?): SpendingCategory? = if (id.isNullOrEmpty()) null else categories.firstOrNull { category -> category.id == id }

    fun categoryName(id: String?): String = category(id)?.let(::label) ?: context.getString(R.string.finance_uncategorized)

    /** A category as shown: in the reader's words when it is one of the built-in ones, under its parent. */
    fun label(category: SpendingCategory): String = FinanceCategories.label(category, categories) { each -> FinanceCategoryNames.shown(context, each) }

    /** The categories to choose from, in the order they are shown. */
    fun pickable(keeping: String?): List<SpendingCategory> = FinanceCategories.pickable(categories, keeping) { each -> FinanceCategoryNames.shown(context, each) }

    fun account(id: String?): FinanceAccount? = accounts.firstOrNull { account -> account.id == id }

    /** Today as the person's calendar has it: "2026-10-10". */
    val today: String get() = LocalDate.now(zone).toString()

    val thisMonth: YearMonth get() = YearMonth.now(zone)
}

internal suspend fun Connection.readFinanceBase(context: Context): FinanceBase {
    val zone = financeZone()
    val categories = spendingCategories()
    val accounts = financeAccounts()
    val currency = try {
        reportingCurrency().currencyCode
    } catch (failure: CancellationException) {
        throw failure
    } catch (_: Throwable) {
        ""
    }
    return FinanceBase(categories, accounts, currency, zone, context)
}

@Composable
internal fun financeContext(): Context = LocalContext.current

/** How a spending category's stored name is shown, as the dashboard shows it: the categories everybody starts with are stored under the server's names, and a person reads them in their own language. */
internal object FinanceCategoryNames {
    fun shown(context: Context, category: SpendingCategory): String = shown(context, category.spendingCategoryName, category.isTransfer == true, category.isOther == true)

    fun shown(context: Context, name: String, isTransfer: Boolean = false, isOther: Boolean = false): String {
        val resource = when (name) {
            "transfer" -> if (isTransfer) R.string.finance_category_transfer else null
            "other" -> if (isOther) R.string.finance_category_other else null
            "income" -> R.string.finance_category_income
            "housing" -> R.string.finance_category_housing
            "utilities" -> R.string.finance_category_utilities
            "groceries" -> R.string.finance_category_groceries
            "dining" -> R.string.finance_category_dining
            "transport" -> R.string.finance_category_transport
            "travel" -> R.string.finance_category_travel
            "shopping" -> R.string.finance_category_shopping
            "health" -> R.string.finance_category_health
            "entertainment" -> R.string.finance_category_entertainment
            "subscriptions" -> R.string.finance_category_subscriptions
            "fees" -> R.string.finance_category_fees
            "gifts and donations" -> R.string.finance_category_gifts
            "taxes" -> R.string.finance_category_taxes
            "loans" -> R.string.finance_category_loans
            "education" -> R.string.finance_category_education
            "children" -> R.string.finance_category_children
            "business services" -> R.string.finance_category_business
            else -> null
        }
        return if (resource == null) name else context.getString(resource)
    }

    /** Who filed a transaction under its category, as a line under it. */
    fun filedBy(context: Context, source: String): String = context.getString(
        when (source) {
            "spending_rule" -> R.string.finance_filed_by_rule
            "provider_category_mapping" -> R.string.finance_filed_by_provider
            "categorize_model" -> R.string.finance_filed_by_model
            "transfer_detection" -> R.string.finance_filed_as_transfer
            else -> R.string.finance_filed_automatically
        },
    )
}
