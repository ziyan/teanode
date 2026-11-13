package com.teanode.app.finance

import android.net.Uri
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.finance.FinanceTransactionFilter

private fun NavBackStackEntry.text(name: String): String = Uri.decode(arguments?.getString(name).orEmpty())

private fun NavGraphBuilder.screen(route: String, arguments: List<String> = emptyList(), content: @androidx.compose.runtime.Composable (NavBackStackEntry) -> Unit) {
    composable(
        route, arguments = arguments.map { name -> navArgument(name) { type = NavType.StringType; defaultValue = "" } },
        deepLinks = Routes.deepLinks(route),
    ) { entry -> content(entry) }
}

/** The routes of Finance: its first screen, which lists the parts, and every place inside it. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.financeRoutes(navigator: AppNavigator) {
    screen(Routes.home(AppSection.Finance)) { FinanceSection() }
    screen(FinanceRoutes.spending) { FinanceSpendingScreen() }
    screen(FinanceRoutes.transactionsPattern, listOf("from", "to", "account", "category", "text", "isUncategorized")) { entry ->
        FinanceTransactionsScreen(
            FinanceTransactionFilter(
                from = entry.text("from"), to = entry.text("to"), financeAccountId = entry.text("account"), spendingCategoryId = entry.text("category"),
                text = entry.text("text"), isUncategorized = entry.text("isUncategorized") == "true",
            ),
        )
    }
    screen(FinanceRoutes.transactionPattern, listOf("id")) { entry -> FinanceTransactionDetailScreen(entry.text("id")) }
    screen(FinanceRoutes.accounts) { FinanceAccountsScreen() }
    screen(FinanceRoutes.tradesPattern, listOf("id")) { entry -> FinanceTradesScreen(entry.text("id")) }
    screen(FinanceRoutes.budgets) { FinanceBudgetsScreen() }
    screen(FinanceRoutes.netWorth) { FinanceNetWorthScreen() }
    screen(FinanceRoutes.assetPattern, listOf("id")) { entry -> FinanceAssetDetailScreen(entry.text("id")) }
    screen(FinanceRoutes.savingsTargets) { FinanceSavingsTargetsScreen() }
    screen(FinanceRoutes.receipts) { FinanceReceiptsScreen() }
    screen(FinanceRoutes.receiptPattern, listOf("id")) { entry -> FinanceReceiptDetailScreen(entry.text("id")) }
    screen(FinanceRoutes.taxes) { FinanceTaxesScreen() }
    screen(FinanceRoutes.paystubPattern, listOf("id")) { entry -> FinancePaystubDetailScreen(entry.text("id")) }
    screen(FinanceRoutes.taxFormPattern, listOf("id")) { entry -> FinanceTaxFormDetailScreen(entry.text("id")) }
    screen(FinanceRoutes.sharing) { FinanceSharingScreen() }
    screen(FinanceRoutes.sharedPattern, listOf("ownerUserId")) { entry -> FinanceSharedAccountsScreen(entry.text("ownerUserId")) }
    screen(FinanceRoutes.sharedAccountPattern, listOf("id")) { entry -> FinanceSharedAccountScreen(entry.text("id")) }
    screen(FinanceRoutes.sharedAllPattern, listOf("ownerUserId")) { entry -> FinanceSharedAllScreen(entry.text("ownerUserId")) }
    screen(FinanceRoutes.setup) { FinanceSetupScreen() }
    screen(FinanceRoutes.receiptEntry) { FinanceReceiptEntryScreen() }
    screen(FinanceRoutes.paystubEntry) { FinancePaystubEntryScreen() }
    screen(FinanceRoutes.taxFormEntry) { FinanceTaxFormEntryScreen() }
    screen(FinanceRoutes.importCredential) { FinanceImportCredentialScreen() }
    screen(FinanceRoutes.importRows) { FinanceImportRowsScreen() }
}

/** What Finance plugs into the shell; listed in `Areas`. */
val financeArea = Area(
    section = AppSection.Finance,
    routes = NavGraphBuilder::financeRoutes,
    demoAnswerers = financeDemoAnswerers,
)
