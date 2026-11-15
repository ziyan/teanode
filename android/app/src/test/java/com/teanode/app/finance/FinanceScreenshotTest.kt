package com.teanode.app.finance

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performTextReplacement
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.shopping.LocalShoppingSheetsInline
import com.teanode.kit.finance.FinanceTransactionFilter
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.unit.dp
import com.teanode.kit.finance.TaxDocumentUpload
import com.teanode.kit.finance.TaxDocumentUploadPhase
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every Finance screen, light and dark, and the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class FinanceScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun shoot(name: String, look: Look = Look.Light, fontScale: Float? = null, isSheet: Boolean = false, scrollTo: String? = null, afterShown: () -> Unit = {}, content: @Composable () -> Unit) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                CompositionLocalProvider(LocalShoppingSheetsInline provides isSheet) {
                    if (fontScale != null) WithFontScale(fontScale) { content() } else content()
                }
            }
        }
        composeRule.waitForIdle()
        if (scrollTo != null) {
            composeRule.onNode(hasScrollAction()).performScrollToNode(hasText(scrollTo))
            composeRule.waitForIdle()
        }
        afterShown()
        composeRule.waitForIdle()
        composeRule.photograph("finance-$name-${look.fileName}")
    }

    @Test fun `receipt in memory`() = shoot("receipt-in-memory", scrollTo = "In memory") { FinanceReceiptDetailScreen(FinanceDemoData.RECEIPT_ID) }
    @Test fun `transaction in memory dark`() = shoot("transaction-in-memory", Look.Dark, scrollTo = "In memory") { FinanceTransactionDetailScreen(FinanceDemoData.TRANSACTION_ID) }
    @Test fun `taxes investment preview`() = shoot("taxes-investment-preview", scrollTo = "Interest and dividends from your accounts") { FinanceTaxesScreen() }
    @Test fun `taxes investment`() = shoot("taxes-investment", scrollTo = "What the year's 1099s report: interest, dividends and the gains or losses on sales.") { FinanceTaxesScreen() }
    @Test fun `taxes investment dark`() = shoot("taxes-investment", Look.Dark, scrollTo = "What the year's 1099s report: interest, dividends and the gains or losses on sales.") { FinanceTaxesScreen() }
    @Test fun `paystub original`() = shoot("paystub-original", scrollTo = "Open the original") { FinancePaystubDetailScreen("demo-paystub-1") }
    @Test fun `tax form original dark`() = shoot("tax-form-original", Look.Dark, scrollTo = "Open the original") { FinanceTaxFormDetailScreen("demo-form-1") }
    @Test fun `exchange rate note`() = shoot("exchange-rate-note") {
        androidx.compose.foundation.layout.Column(androidx.compose.ui.Modifier.padding(16.dp)) { ExchangeRateNote("EUR") }
    }
    @Test fun `tax document uploads`() = shoot("tax-uploads") {
        androidx.compose.foundation.layout.Column {
            TaxDocumentUploadRow(TaxDocumentUpload(1, "paystub-march.pdf"), {})
            TaxDocumentUploadRow(TaxDocumentUpload(2, "w2.jpg", "a", TaxDocumentUploadPhase.Reading), {})
            TaxDocumentUploadRow(TaxDocumentUpload(3, "statement.pdf", "b", TaxDocumentUploadPhase.StillReading), {})
            TaxDocumentUploadRow(TaxDocumentUpload(4, "scan.pdf", null, TaxDocumentUploadPhase.Failed, "The file is too large."), {})
        }
    }

    @Test fun `home`() = shoot("home") { FinanceSection() }
    @Test fun `home dark`() = shoot("home", Look.Dark) { FinanceSection() }
    @Test fun `home largest text`() = shoot("home-largest-text", fontScale = 2f) { FinanceSection() }

    @Test fun `spending`() = shoot("spending") { FinanceSpendingScreen() }
    @Test fun `spending dark`() = shoot("spending", Look.Dark) { FinanceSpendingScreen() }
    @Test fun `spending largest text`() = shoot("spending-largest-text", fontScale = 2f) { FinanceSpendingScreen() }

    @Test fun `transactions`() = shoot("transactions") { FinanceTransactionsScreen(FinanceTransactionFilter()) }
    @Test fun `transactions dark`() = shoot("transactions", Look.Dark) { FinanceTransactionsScreen(FinanceTransactionFilter()) }
    @Test fun `transactions largest text`() = shoot("transactions-largest-text", fontScale = 2f) { FinanceTransactionsScreen(FinanceTransactionFilter()) }
    @Test fun `transactions narrowed`() = shoot("transactions-narrowed") { FinanceTransactionsScreen(FinanceTransactionFilter(isUncategorized = true)) }
    @Test fun `filter sheet`() = shoot("filter", isSheet = true) { TransactionFilterSheet(null, FinanceTransactionFilter(from = "2030-11-01"), {}, {}) }
    @Test fun `transaction detail`() = shoot("transaction") { FinanceTransactionDetailScreen(FinanceDemoData.TRANSACTION_ID) }
    @Test fun `transaction detail dark`() = shoot("transaction", Look.Dark) { FinanceTransactionDetailScreen(FinanceDemoData.TRANSACTION_ID) }

    @Test fun `accounts`() = shoot("accounts") { FinanceAccountsScreen() }
    @Test fun `accounts dark`() = shoot("accounts", Look.Dark) { FinanceAccountsScreen() }
    @Test fun `accounts largest text`() = shoot("accounts-largest-text", fontScale = 2f) { FinanceAccountsScreen() }
    @Test fun `trades`() = shoot("trades") { FinanceTradesScreen("demo-account-brokerage") }

    @Test fun `budgets`() = shoot("budgets") { FinanceBudgetsScreen() }
    @Test fun `budgets dark`() = shoot("budgets", Look.Dark) { FinanceBudgetsScreen() }
    @Test fun `budget editor with a month already past`() = shoot("budget-editor-past-month", afterShown = {
        composeRule.onNodeWithText("Groceries").performClick()
        composeRule.waitForIdle()
        composeRule.onNode(androidx.compose.ui.test.hasText("From month", substring = true) and androidx.compose.ui.test.hasSetTextAction()).performTextReplacement("2020-01")
    }) { FinanceBudgetsScreen() }

    @Test fun `budget editor dark`() = shoot("budget-editor", Look.Dark, afterShown = { composeRule.onNodeWithText("Groceries").performClick() }) { FinanceBudgetsScreen() }

    @Test fun `rule editor with an account`() = shoot("rule-editor", scrollTo = "New rule", afterShown = { composeRule.onNodeWithText("New rule").performClick() }) { FinanceBudgetsScreen() }

    @Test fun `asset editor with an estimate`() = shoot("asset-editor-estimate", afterShown = {
        composeRule.onNodeWithContentDescription("Add").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Valued by", substring = true).performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("The agent's estimate").performClick()
    }) { FinanceNetWorthScreen() }

    @Test fun `asset editor dark`() = shoot("asset-editor", Look.Dark, afterShown = { composeRule.onNodeWithContentDescription("Add").performClick() }) { FinanceNetWorthScreen() }

    @Test fun `net worth`() = shoot("net-worth") { FinanceNetWorthScreen() }
    @Test fun `net worth dark`() = shoot("net-worth", Look.Dark) { FinanceNetWorthScreen() }
    @Test fun `asset`() = shoot("asset") { FinanceAssetDetailScreen("asset-3") }
    @Test fun `savings targets`() = shoot("savings-targets") { FinanceSavingsTargetsScreen() }
    @Test fun `savings targets dark`() = shoot("savings-targets", Look.Dark) { FinanceSavingsTargetsScreen() }
    @Test fun `receipts`() = shoot("receipts") { FinanceReceiptsScreen() }
    @Test fun `receipt`() = shoot("receipt") { FinanceReceiptDetailScreen(FinanceDemoData.RECEIPT_ID) }
    @Test fun `receipt dark`() = shoot("receipt", Look.Dark) { FinanceReceiptDetailScreen(FinanceDemoData.RECEIPT_ID) }

    @Test fun `taxes`() = shoot("taxes") { FinanceTaxesScreen() }
    @Test fun `taxes dark`() = shoot("taxes", Look.Dark) { FinanceTaxesScreen() }
    @Test fun `paystub`() = shoot("paystub") { FinancePaystubDetailScreen("demo-paystub-1") }
    @Test fun `tax form`() = shoot("tax-form") { FinanceTaxFormDetailScreen("demo-form-1") }

    @Test fun `sharing`() = shoot("sharing") { FinanceSharingScreen() }
    @Test fun `sharing dark`() = shoot("sharing", Look.Dark) { FinanceSharingScreen() }
    @Test fun `shared accounts`() = shoot("shared-accounts") { FinanceSharedAccountsScreen(FinanceDemoData.SHARER_ID) }
    @Test fun `shared account`() = shoot("shared-account") { FinanceSharedAccountScreen("shared-account-1") }
    @Test fun `shared all`() = shoot("shared-all") { FinanceSharedAllScreen(FinanceDemoData.WHOLE_SHARER_ID) }
    @Test fun `setup`() = shoot("setup") { FinanceSetupScreen() }
    @Test fun `setup dark`() = shoot("setup", Look.Dark) { FinanceSetupScreen() }

    @Test fun `receipt entry`() = shoot("receipt-entry") { FinanceReceiptEntryScreen() }
    @Test fun `receipt entry dark`() = shoot("receipt-entry", Look.Dark) { FinanceReceiptEntryScreen() }
    @Test fun `paystub entry`() = shoot("paystub-entry") { FinancePaystubEntryScreen() }
    @Test fun `tax form entry`() = shoot("tax-form-entry") { FinanceTaxFormEntryScreen() }
    @Test fun `tax form entry largest text`() = shoot("tax-form-entry-largest-text", fontScale = 2f) { FinanceTaxFormEntryScreen() }
    @Test fun `import credential`() = shoot("import-credential") { FinanceImportCredentialScreen() }
    @Test fun `import credential dark`() = shoot("import-credential", Look.Dark) { FinanceImportCredentialScreen() }
    @Test fun `import rows`() = shoot("import-rows") { FinanceImportRowsScreen() }
    @Test fun `suggest rules`() = shoot("suggest-rules", isSheet = true) {
        val connection = com.teanode.app.session.LocalConnection.current
        val context = androidx.compose.ui.platform.LocalContext.current
        var base by androidx.compose.runtime.remember { androidx.compose.runtime.mutableStateOf<FinanceBase?>(null) }
        androidx.compose.runtime.LaunchedEffect(Unit) { base = connection.readFinanceBase(context) }
        base?.let { loaded -> SuggestRulesSheet(loaded, {}, {}) }
    }
    @Test fun `shared all by year`() {
        composeRule.setContent { DemoHost { FinanceSharedAllScreen(FinanceDemoData.WHOLE_SHARER_ID) } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Year").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("finance-shared-all-year-light")
    }
    @Test fun `spending with a chosen month`() {
        composeRule.setContent { DemoHost { FinanceSpendingScreen() } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Cash flow").assertExists()
        composeRule.photograph("finance-spending-callouts-light")
    }
}
