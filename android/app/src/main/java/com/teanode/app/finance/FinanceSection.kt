package com.teanode.app.finance

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalance
import androidx.compose.material.icons.outlined.DonutLarge
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.ReceiptLong
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.TrendingUp
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.SharedAllFinance
import com.teanode.kit.finance.SharedFinanceAccount
import com.teanode.kit.finance.isFinanceShown
import com.teanode.kit.finance.ownerShownName
import com.teanode.kit.finance.sharedAllFinance
import com.teanode.kit.finance.sharedFinanceAccounts
import kotlinx.coroutines.CancellationException

/** The parts of Finance, one section each of the dashboard's Finance page. */
internal enum class FinancePart(val icon: ImageVector) {
    Spending(Icons.Outlined.DonutLarge),
    Transactions(Icons.Outlined.ViewList),
    Accounts(Icons.Outlined.AccountBalance),
    Budgets(Icons.Outlined.Speed),
    NetWorth(Icons.Outlined.TrendingUp),
    SavingsTargets(Icons.Outlined.Flag),
    Receipts(Icons.Outlined.ReceiptLong),
    Taxes(Icons.Outlined.Description),
    Sharing(Icons.Outlined.People),
    Setup(Icons.Outlined.Link),
    ;

    val titleResource: Int
        get() = when (this) {
            Spending -> R.string.finance_part_spending
            Transactions -> R.string.finance_part_transactions
            Accounts -> R.string.finance_part_accounts
            Budgets -> R.string.finance_part_budgets
            NetWorth -> R.string.finance_part_net_worth
            SavingsTargets -> R.string.finance_part_savings_targets
            Receipts -> R.string.finance_part_receipts
            Taxes -> R.string.finance_part_taxes
            Sharing -> R.string.finance_part_sharing
            Setup -> R.string.finance_part_setup
        }

    val route: String
        get() = when (this) {
            Spending -> FinanceRoutes.spending
            Transactions -> FinanceRoutes.transactions()
            Accounts -> FinanceRoutes.accounts
            Budgets -> FinanceRoutes.budgets
            NetWorth -> FinanceRoutes.netWorth
            SavingsTargets -> FinanceRoutes.savingsTargets
            Receipts -> FinanceRoutes.receipts
            Taxes -> FinanceRoutes.taxes
            Sharing -> FinanceRoutes.sharing
            Setup -> FinanceRoutes.setup
        }
}

private class FinanceHome(val isShown: Boolean, val sharedAccounts: List<SharedFinanceAccount>, val sharedAll: List<SharedAllFinance>)

/** The first screen of Finance: its parts, and what other people share with the person. */
@Composable
fun FinanceSection() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val home = rememberLoad(connection) {
        val isShown = connection.isFinanceShown()
        // A failure leaves what was shown: the person's own finance does not wait for what others share.
        val accounts = try { connection.sharedFinanceAccounts() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { emptyList() }
        val all = try { connection.sharedAllFinance() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { emptyList() }
        FinanceHome(isShown, accounts, all)
    }
    TeaNodeScreen(title = stringResource(R.string.finance_title)) { padding ->
        LoadedContent(home, modifier = Modifier.padding(padding)) { read ->
            LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
                if (!read.isShown && read.sharedAccounts.isEmpty() && read.sharedAll.isEmpty()) {
                    item("not-set-up") {
                        EmptyState(
                            Icons.Outlined.AccountBalance, stringResource(R.string.finance_not_set_up), description = stringResource(R.string.finance_not_set_up_description),
                            modifier = Modifier.padding(vertical = 24.dp),
                        )
                    }
                    item("setup") { TeaNodeNavigationRow(stringResource(FinancePart.Setup.titleResource), { navigator.navigate(FinancePart.Setup.route) }, leadingIcon = FinancePart.Setup.icon) }
                } else {
                    if (read.isShown) {
                        for (part in FinancePart.entries) {
                            item(part.name) { TeaNodeNavigationRow(stringResource(part.titleResource), { navigator.navigate(part.route) }, leadingIcon = part.icon) }
                        }
                    } else {
                        item("setup") { TeaNodeNavigationRow(stringResource(FinancePart.Setup.titleResource), { navigator.navigate(FinancePart.Setup.route) }, leadingIcon = FinancePart.Setup.icon) }
                    }
                    val sharers = sharersOf(read.sharedAccounts, read.sharedAll)
                    if (sharers.isNotEmpty()) {
                        item("shared-header") { TeaNodeSectionHeader(stringResource(R.string.finance_shared_with_you)) }
                        for (sharer in sharers) {
                            item("sharer-" + sharer.userId) {
                                TeaNodeNavigationRow(
                                    sharer.displayName, { navigator.navigate(if (sharer.hasAccounts) FinanceRoutes.shared(sharer.userId) else FinanceRoutes.sharedAllOf(sharer.userId)) },
                                    supporting = if (sharer.isWhole) stringResource(R.string.finance_shares_all) else stringResource(R.string.finance_shares_accounts),
                                    leadingIcon = Icons.Outlined.People,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** A person who shares finance with the person, whole or an account at a time. */
internal class FinanceSharer(val userId: String, val displayName: String, val isWhole: Boolean, val hasAccounts: Boolean = false)

internal fun sharersOf(accounts: List<SharedFinanceAccount>, all: List<SharedAllFinance>): List<FinanceSharer> {
    val sharers = linkedMapOf<String, FinanceSharer>()
    for (each in all) sharers[each.ownerUserId] = FinanceSharer(each.ownerUserId, each.ownerShownName, true)
    for (each in accounts) {
        val known = sharers[each.ownerUserId]
        sharers[each.ownerUserId] = FinanceSharer(each.ownerUserId, each.ownerShownName, known?.isWhole == true, true)
    }
    return sharers.values.sortedBy { sharer -> sharer.displayName.lowercase() }
}
