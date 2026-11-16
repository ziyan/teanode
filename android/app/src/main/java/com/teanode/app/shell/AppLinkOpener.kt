package com.teanode.app.shell

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.finance.FinanceRoutes
import com.teanode.app.health.HealthRoutes
import com.teanode.app.journal.JournalRoutes
import com.teanode.app.knowledge.KnowledgeRoutes
import com.teanode.app.location.LocationRoutes
import com.teanode.app.mail.MailRoutes
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.AppSection
import com.teanode.kit.Connection
import com.teanode.kit.chats.AppLinkTarget
import com.teanode.kit.chats.AppLinks
import com.teanode.kit.knowledge.sharedKnowledgeRoots
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * AppLinkOpener follows a link tapped in an answer, or a path the agent opens, inside the app: a recording, a mail
 * item, a page of memory and the dashboard's pages open their own screens, a web page opens in the app's browser, and
 * the rest is the system's (ios AppModel.route and show(path:)).
 */
class AppLinkOpener(
    private val context: Context,
    private val navigator: AppNavigator,
    private val connection: Connection,
    private val snackbar: SnackbarController,
    private val scope: CoroutineScope,
) {
    /** Follows a link written in an answer. */
    fun followLink(address: String) = open(if (address.startsWith("/")) AppLinks.forPath(address, connection.account.server) else AppLinks.forLink(address, connection.account.server))

    /** Follows a path of the dashboard, as the agent's open_page names one. */
    fun followPath(path: String) = open(AppLinks.forPath(path, connection.account.server))

    private fun open(target: AppLinkTarget) {
        when (target) {
            is AppLinkTarget.WebPage -> openWebPage(context, target.address)
            is AppLinkTarget.System -> try {
                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(target.address)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            } catch (_: Exception) {
                // Nothing on the phone opens it.
            }
            is AppLinkTarget.Recording -> {
                val link = target.recordingLink
                val route = if (link.recordingTranscriptRunId != null && link.passageStartMS != null && link.passageEndMS != null) {
                    "cite/${Uri.encode(link.recordingId)}/${Uri.encode(link.recordingTranscriptRunId)}/${link.passageStartMS}/${link.passageEndMS}"
                } else {
                    "detail/${Uri.encode(link.recordingId)}"
                }
                navigator.openInSection(AppSection.Recordings, Routes.child(AppSection.Recordings, route))
            }
            is AppLinkTarget.MailItem -> navigator.openInSection(AppSection.Mail, MailRoutes.thread(target.itemId, null))
            is AppLinkTarget.Mailbox -> {
                val itemId = target.itemId
                if (itemId != null) navigator.openInSection(AppSection.Mail, MailRoutes.thread(itemId, null)) else navigator.openSection(AppSection.Mail)
            }
            is AppLinkTarget.MemoryPage -> navigator.openInSection(AppSection.Knowledge, KnowledgeRoutes.page(target.pagePath))
            is AppLinkTarget.SharedMemoryPage -> openSharedPage(target)
            is AppLinkTarget.Knowledge ->
                if (target.pagePath.isEmpty()) navigator.openSection(AppSection.Knowledge) else navigator.openInSection(AppSection.Knowledge, KnowledgeRoutes.page(target.pagePath))
            is AppLinkTarget.Location -> {
                val day = target.day
                if (day != null) navigator.openInSection(AppSection.Location, LocationRoutes.day(day)) else navigator.openSection(AppSection.Location)
            }
            is AppLinkTarget.Health -> {
                val healthType = target.healthType
                when {
                    target.isSettings -> navigator.openInSection(AppSection.Health, HealthRoutes.settings)
                    healthType != null -> navigator.openInSection(AppSection.Health, HealthRoutes.type(healthType))
                    else -> navigator.openSection(AppSection.Health)
                }
            }
            is AppLinkTarget.Finance -> financeRoute(target.partName)?.let { route -> navigator.openInSection(AppSection.Finance, route) } ?: navigator.openSection(AppSection.Finance)
            is AppLinkTarget.Journal ->
                if (target.day.isEmpty()) navigator.openSection(AppSection.Journal) else navigator.openInSection(AppSection.Journal, JournalRoutes.day(target.day))
            AppLinkTarget.Passwords -> navigator.openSection(AppSection.Passwords)
            AppLinkTarget.Shopping -> navigator.openSection(AppSection.Shopping)
            is AppLinkTarget.Settings ->
                if (target.isMailPrograms) navigator.openInSection(AppSection.Mail, MailRoutes.SETTINGS_PROGRAMS) else navigator.openSection(AppSection.Settings)
            AppLinkTarget.Connect -> snackbar.show(context.getString(R.string.shell_link_already_connected, connection.account.server.host))
            AppLinkTarget.Discarded -> Unit
        }
    }

    /** The link names the person by username; the page is asked for by the identifier they are listed under, which only the pages shared with this person can say. */
    private fun openSharedPage(target: AppLinkTarget.SharedMemoryPage) {
        scope.launch {
            val roots = try {
                connection.sharedKnowledgeRoots()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
                return@launch
            }
            // Not among those who share with this person: the owner stopped, or never did.
            val owner = roots.firstOrNull { root -> root.ownerUsername.equals(target.ownerUsername, ignoreCase = true) }
            if (owner == null) {
                snackbar.showProblem(context.getString(R.string.shell_link_no_longer_shared))
                return@launch
            }
            navigator.openInSection(AppSection.Knowledge, KnowledgeRoutes.shared(owner.ownerUserId, target.pagePath))
        }
    }

    private fun financeRoute(partName: String?): String? = when (partName) {
        "spending" -> FinanceRoutes.spending
        "transactions" -> FinanceRoutes.transactions()
        "accounts" -> FinanceRoutes.accounts
        "budgets" -> FinanceRoutes.budgets
        "networth" -> FinanceRoutes.netWorth
        "savingstargets" -> FinanceRoutes.savingsTargets
        "receipts" -> FinanceRoutes.receipts
        "taxes" -> FinanceRoutes.taxes
        "sharing" -> FinanceRoutes.sharing
        "setup" -> FinanceRoutes.setup
        else -> null
    }
}

/** The opener of the account in use, for the screens that show links. */
@Composable
fun rememberAppLinkOpener(): AppLinkOpener {
    val context = LocalContext.current
    val navigator = LocalAppNavigator.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    return remember(context, navigator, connection, snackbar, scope) { AppLinkOpener(context, navigator, connection, snackbar, scope) }
}
