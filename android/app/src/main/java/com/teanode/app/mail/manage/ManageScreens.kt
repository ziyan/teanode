package com.teanode.app.mail.manage

import android.content.Context
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material.icons.outlined.Dangerous
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.MoveToInbox
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.mail.MailTimes
import com.teanode.app.mail.fileSize
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.Connection
import com.teanode.kit.mail.AuditedMail
import com.teanode.kit.mail.AuditedMailDetail
import com.teanode.kit.mail.DMARCReport
import com.teanode.kit.mail.DeliveryQueueFilter
import com.teanode.kit.mail.DomainDNSSummary
import com.teanode.kit.mail.MailAuthenticationResults
import com.teanode.kit.mail.MailDelivery
import com.teanode.kit.mail.MailListFilter
import com.teanode.kit.mail.MailOutcome
import com.teanode.kit.mail.ManagedDomain
import com.teanode.kit.mail.ManagedTone
import com.teanode.kit.mail.ManagedWords
import com.teanode.kit.mail.OperatorAccess
import com.teanode.kit.mail.ReportAlignment
import com.teanode.kit.mail.ReportListFilter
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.allMail
import com.teanode.kit.mail.auditedMail
import com.teanode.kit.mail.dmarcReport
import com.teanode.kit.mail.dmarcReports
import com.teanode.kit.mail.mailOpens
import com.teanode.kit.mail.managedDomainNames
import com.teanode.kit.mail.managedDomains
import com.teanode.kit.mail.operatorAccess
import com.teanode.kit.mail.pendingDeliveries
import com.teanode.kit.mail.retryDelivery
import com.teanode.kit.mail.teachSpamFilter
import kotlinx.coroutines.launch

/** The words and colors the management pages share. */
object ManageLabels {
    fun mailStatus(context: Context, statusText: String?): String = when (statusText ?: "") {
        "" -> context.getString(R.string.mail_manage_unknown)
        "received" -> context.getString(R.string.mail_manage_received)
        "accepted" -> context.getString(R.string.mail_manage_accepted)
        "rejected" -> context.getString(R.string.mail_manage_rejected)
        "queued" -> context.getString(R.string.mail_manage_queued)
        "dropped" -> context.getString(R.string.mail_manage_dropped)
        "delivered" -> context.getString(R.string.mail_manage_delivered)
        "attempted" -> context.getString(R.string.mail_manage_attempted)
        "failed" -> context.getString(R.string.mail_manage_failed)
        "delayed" -> context.getString(R.string.mail_manage_delayed)
        "relayed" -> context.getString(R.string.mail_manage_relayed)
        "expanded" -> context.getString(R.string.mail_manage_expanded)
        else -> statusText ?: ""
    }

    fun mailKind(context: Context, kindText: String?): String = when (kindText ?: "") {
        "incoming" -> context.getString(R.string.mail_manage_incoming)
        "outgoing" -> context.getString(R.string.mail_manage_outgoing)
        "exchange" -> context.getString(R.string.mail_manage_exchange)
        "dsn" -> context.getString(R.string.mail_manage_bounce)
        "rua" -> context.getString(R.string.mail_manage_dmarc_report)
        "ruf" -> context.getString(R.string.mail_manage_dmarc_failure)
        "forward" -> context.getString(R.string.mail_manage_forward)
        "internal" -> context.getString(R.string.mail_manage_internal)
        "external" -> context.getString(R.string.mail_manage_external)
        else -> kindText ?: ""
    }

    fun alignment(context: Context, alignment: ReportAlignment): String = context.getString(
        when (alignment) {
            ReportAlignment.Both -> R.string.mail_manage_dkim_and_spf
            ReportAlignment.One -> R.string.mail_manage_one_aligned
            ReportAlignment.Neither -> R.string.mail_manage_neither_aligned
        },
    )

    fun alignmentMode(context: Context, mode: String?): String = context.getString(if (mode == "s") R.string.mail_manage_strict else R.string.mail_manage_relaxed)

    fun relativeTime(timeText: String?): String? = ServerTime.instant(timeText)?.let { moment ->
        android.text.format.DateUtils.getRelativeTimeSpanString(moment.toEpochMilli(), System.currentTimeMillis(), android.text.format.DateUtils.MINUTE_IN_MILLIS).toString()
    }

    fun fullTime(timeText: String?): String? = ServerTime.instant(timeText)?.let { moment -> MailTimes.dateAndTime(moment) }

    fun byteText(byteCount: Int?): String = if (byteCount == null || byteCount <= 0) "" else fileSize(byteCount.toLong())

    fun domainName(context: Context, domainId: String?, domains: List<ManagedDomain>): String =
        domains.firstOrNull { domain -> domain.id == domainId }?.domainName ?: context.getString(R.string.mail_manage_deleted)
}

/** A status or a verdict as a small tag: a symbol and a word, red when bad and orange as a warning. */
@Composable
fun ManageTag(text: String, tone: ManagedTone) {
    val color = when (tone) {
        ManagedTone.Good, ManagedTone.Plain -> MaterialTheme.colorScheme.onSurfaceVariant
        ManagedTone.Bad -> MaterialTheme.colorScheme.error
        ManagedTone.Warn -> Color(0xFFE08A00)
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(
            when (tone) {
                ManagedTone.Good -> Icons.Outlined.CheckCircle
                ManagedTone.Bad -> Icons.Outlined.Dangerous
                ManagedTone.Warn -> Icons.Outlined.WarningAmber
                ManagedTone.Plain -> Icons.Outlined.Circle
            },
            null, Modifier.size(14.dp), tint = color,
        )
        Spacer(Modifier.width(4.dp))
        Text(text, style = MaterialTheme.typography.labelMedium, color = color)
    }
}

/** A labeled fact in a list of facts; nothing when there is nothing to say. */
@Composable
fun DetailRow(label: String, fieldText: String?, isMonospaced: Boolean = false) {
    if (fieldText.isNullOrEmpty()) return
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        SelectionContainer { Text(fieldText, style = MaterialTheme.typography.bodyMedium, fontFamily = if (isMonospaced) FontFamily.Monospace else null) }
    }
}

/** One filter of a list: its name, the choice that means everything, and the other choices. */
class FilterPicker(val title: String, val allTitle: String, val choices: List<Pair<String, String>>, val selected: String?, val onChoose: (String?) -> Unit)

@Composable
fun FilterMenu(isFiltering: Boolean, pickers: List<FilterPicker>) {
    var isOpen by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { isOpen = true }) {
            Icon(Icons.Outlined.FilterList, stringResource(R.string.mail_filter), tint = if (isFiltering) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            for (picker in pickers) {
                Text(picker.title, Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                DropdownMenuItem(text = { Text(picker.allTitle, fontFamily = null, color = if (picker.selected == null) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) }, onClick = { picker.onChoose(null); isOpen = false })
                for ((value, title) in picker.choices) {
                    DropdownMenuItem(text = { Text(title, color = if (picker.selected == value) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) }, onClick = { picker.onChoose(value); isOpen = false })
                }
                HorizontalDivider()
            }
        }
    }
}

/** What the signed-in person may open on the management side of mail: all mail, the queue, the reports and the domains. */
@Composable
fun ManageOverviewScreen(onBack: () -> Unit, onOpenAllMail: () -> Unit, onOpenQueue: () -> Unit, onOpenReports: () -> Unit, onOpenDomains: () -> Unit) {
    val connection = LocalConnection.current
    val access = rememberLoad(connection) { connection.operatorAccess() }
    TeaNodeScreen(title = stringResource(R.string.mail_manage_title), onBack = onBack) { contentPadding ->
        LoadedContent(
            access, modifier = Modifier.padding(contentPadding),
            isEmpty = { granted -> !granted.isOperator },
            empty = { EmptyState(Icons.Outlined.Lock, stringResource(R.string.mail_manage_nothing_to_manage), description = stringResource(R.string.mail_manage_nothing_to_manage_description)) },
        ) { granted -> ManageOverviewRows(granted, onOpenAllMail, onOpenQueue, onOpenReports, onOpenDomains) }
    }
}

@Composable
fun ManageOverviewRows(access: OperatorAccess, onOpenAllMail: () -> Unit, onOpenQueue: () -> Unit, onOpenReports: () -> Unit, onOpenDomains: () -> Unit) {
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        if (access.canOpenMailDelivery) {
            if (access.canAuditMail) TeaNodeNavigationRow(stringResource(R.string.mail_title), onClick = onOpenAllMail, leadingIcon = Icons.Outlined.MoveToInbox)
            if (access.canManageQueue) TeaNodeNavigationRow(stringResource(R.string.mail_manage_queue), onClick = onOpenQueue, leadingIcon = Icons.Outlined.Schedule)
            if (access.canReadReports) TeaNodeNavigationRow(stringResource(R.string.mail_manage_reports), onClick = onOpenReports, leadingIcon = Icons.Outlined.Shield)
            Text(stringResource(R.string.mail_manage_arriving_and_stuck), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (access.canManageDomains) {
                TeaNodeNavigationRow(stringResource(R.string.mail_manage_domains), onClick = onOpenDomains, leadingIcon = Icons.Outlined.Language)
                Text(stringResource(R.string.mail_manage_domains_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** A list searched by words and narrowed by filters, shown a page at a time as the dashboard's tables are. */
@Composable
private fun <Row> ManagedList(
    title: String,
    onBack: () -> Unit,
    rows: List<Row>?,
    failure: Throwable?,
    onRetry: () -> Unit,
    searchText: String,
    onSearch: (String) -> Unit,
    searchPrompt: String,
    filterMenu: @Composable () -> Unit,
    emptyTitle: String,
    footer: @Composable (shown: Int) -> String,
    key: (Row) -> String,
    row: @Composable (Row) -> Unit,
) {
    var shownCount by remember(searchText) { mutableIntStateOf(ManagedWords.PAGE_SIZE) }
    TeaNodeScreen(title = title, onBack = onBack, actions = { filterMenu() }) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            OutlinedTextField(
                searchText, onSearch, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true, leadingIcon = { Icon(Icons.Outlined.Search, null) },
                placeholder = { Text(searchPrompt) }, shape = RoundedCornerShape(28.dp),
            )
            when {
                rows == null && failure != null -> ErrorState(failure.message.orEmpty(), onRetry = onRetry)
                rows == null -> LoadingState()
                rows.isEmpty() -> EmptyState(Icons.Outlined.Search, emptyTitle)
                else -> LazyColumn(Modifier.fillMaxSize()) {
                    items(rows.take(shownCount), key = { item -> key(item) }) { item -> row(item) }
                    if (rows.size > shownCount) item { LaunchedEffect(shownCount) { shownCount += ManagedWords.PAGE_SIZE }; Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { androidx.compose.material3.CircularProgressIndicator(Modifier.size(24.dp)) } }
                    item { Text(footer(rows.size), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
        }
    }
}

/** All the mail the server has handled, newest first: who, what, what became of it. */
@Composable
fun AllMailScreen(onBack: () -> Unit, onOpenMail: (String) -> Unit, startingFilter: MailListFilter = MailListFilter()) {
    val connection = LocalConnection.current
    val context = LocalContext.current
    var filter by remember { mutableStateOf(startingFilter) }
    var domains by remember { mutableStateOf(emptyList<ManagedDomain>()) }
    var mails by remember { mutableStateOf<List<AuditedMail>?>(null) }
    var opens by remember { mutableStateOf(emptyMap<String, com.teanode.kit.mail.AuditedMailOpens>()) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    LaunchedEffect(attempt) {
        try {
            domains = connection.managedDomainNames()
            val loaded = connection.allMail()
            mails = loaded
            opens = runCatching { connection.mailOpens(loaded.map { mail -> mail.id }) }.getOrDefault(emptyMap())
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }
    val all = mails
    val shown = all?.let { loaded -> filter.applied(loaded) }
    ManagedList(
        title = stringResource(R.string.mail_title), onBack = onBack, rows = shown, failure = failure, onRetry = { failure = null; attempt += 1 },
        searchText = filter.searchText, onSearch = { typed -> filter = filter.copy(searchText = typed) }, searchPrompt = stringResource(R.string.mail_manage_sender_or_subject),
        filterMenu = {
            FilterMenu(
                filter.domainId != null || filter.mailStatus != null || filter.mailKind != null,
                listOf(
                    FilterPicker(stringResource(R.string.mail_manage_domain), stringResource(R.string.mail_manage_all_domains), domains.map { domain -> domain.id to domain.domainName }, filter.domainId) { chosen -> filter = filter.copy(domainId = chosen) },
                    FilterPicker(stringResource(R.string.mail_manage_status), stringResource(R.string.mail_manage_any_status), (all ?: emptyList()).mapNotNull { mail -> mail.mailStatus }.toSortedSet().map { status -> status to ManageLabels.mailStatus(context, status) }, filter.mailStatus) { chosen -> filter = filter.copy(mailStatus = chosen) },
                    FilterPicker(stringResource(R.string.mail_manage_kind), stringResource(R.string.mail_manage_any_kind), (all ?: emptyList()).mapNotNull { mail -> mail.mailKind }.toSortedSet().map { kind -> kind to ManageLabels.mailKind(context, kind) }, filter.mailKind) { chosen -> filter = filter.copy(mailKind = chosen) },
                ),
            )
        },
        emptyTitle = stringResource(if (all?.isEmpty() == true) R.string.mail_manage_no_mail_yet else R.string.mail_nothing_matches),
        footer = { count ->
            val total = all?.size ?: 0
            when {
                filter.isFiltering -> stringResource(R.string.mail_manage_n_of_total_messages, count, total)
                total >= ManagedWords.LIST_LIMIT -> stringResource(R.string.mail_manage_most_recent_messages, total)
                else -> pluralStringResource(R.plurals.mail_messages_count, total, total)
            }
        },
        key = { mail -> mail.id },
    ) { mail ->
        Column(Modifier.fillMaxWidth().clickable { onOpenMail(mail.id) }.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(mail.shownSender, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                ManageLabels.relativeTime(mail.receivedAt)?.let { received -> Text(received, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            Text(mail.subject?.ifEmpty { null } ?: stringResource(R.string.mail_no_subject), style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                ManageTag(ManageLabels.mailStatus(context, mail.mailStatus), ManagedTone.of(mail.mailStatus))
                Text(
                    listOf(ManageLabels.domainName(context, mail.domainId, domains), ManageLabels.mailKind(context, mail.mailKind), ManageLabels.byteText(mail.byteCount)).filter { part -> part.isNotEmpty() }.joinToString(" · "),
                    style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
                )
                val open = opens[mail.id]
                if (open != null && open.isTrackable) {
                    if (open.isOpened) ManageTag(if ((open.openCount ?: 0) > 1) stringResource(R.string.mail_manage_opened_times, open.openCount ?: 0) else stringResource(R.string.mail_manage_opened), ManagedTone.Good)
                    else Text(stringResource(R.string.mail_manage_not_fetched), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        HorizontalDivider()
    }
}

/** What happened to a message, in one line, as the top of the dashboard's message page says it. */
@Composable
fun outcomeText(outcome: MailOutcome): String = when (outcome) {
    is MailOutcome.RejectedForViruses -> stringResource(R.string.mail_manage_outcome_viruses, outcome.viruses.joinToString(", "))
    is MailOutcome.RejectedAsSpam -> stringResource(R.string.mail_manage_outcome_spam, ManagedWords.score(outcome.score))
    is MailOutcome.RejectedByDMARC -> stringResource(R.string.mail_manage_outcome_dmarc, outcome.policy)
    is MailOutcome.RejectedWithErrors -> stringResource(R.string.mail_manage_outcome_errors, outcome.errorMessages.joinToString(", "))
    MailOutcome.Rejected -> stringResource(R.string.mail_manage_outcome_rejected)
    MailOutcome.NoDeliveries -> stringResource(R.string.mail_manage_outcome_no_deliveries)
    is MailOutcome.PartlyDelivered -> stringResource(R.string.mail_manage_outcome_partly, outcome.deliveredCount, outcome.totalCount, outcome.failedCount)
    is MailOutcome.Delivered -> pluralStringResource(R.plurals.mail_manage_outcome_delivered, outcome.deliveredCount, outcome.deliveredCount)
    is MailOutcome.Pending -> pluralStringResource(R.plurals.mail_manage_outcome_pending, outcome.pendingCount, outcome.pendingCount)
}

/** One message as the operator sees it: its status and what became of it, who sent it, the checks it went through, its deliveries and what the spam filter was taught. */
@Composable
fun AuditedMailScreen(mailId: String, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val detail = rememberLoad(mailId) { connection.auditedMail(mailId) }
    var trainingLabel by remember(mailId) { mutableStateOf<String?>(null) }
    var hasTrainingChanged by remember(mailId) { mutableStateOf(false) }
    var isTeaching by remember { mutableStateOf(false) }
    var subPage by remember { mutableStateOf<AuditSubPage?>(null) }
    subPage?.let { opened ->
        AuditSubPageScreen(opened, onBack = { subPage = null })
        return
    }
    TeaNodeScreen(title = stringResource(R.string.mail_manage_message), onBack = onBack) { contentPadding ->
        LoadedContent(detail, modifier = Modifier.padding(contentPadding)) { shown: AuditedMailDetail ->
            val mail = shown.mail
            val label = if (hasTrainingChanged) trainingLabel else shown.trainingLabel
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    ManageTag(ManageLabels.mailStatus(context, mail.mailStatus), ManagedTone.of(mail.mailStatus))
                    if (!mail.mailKind.isNullOrEmpty()) ManageTag(ManageLabels.mailKind(context, mail.mailKind), ManagedTone.Plain)
                }
                Text(outcomeText(MailOutcome.of(mail, shown.deliveries)), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyLarge)
                val classifier = shown.classifier
                if (classifier != null && classifier.canBeTaught) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_spam_filter))
                    Text(
                        when (label) {
                            "spam" -> stringResource(R.string.mail_manage_taught_spam)
                            "ham" -> stringResource(R.string.mail_manage_taught_ham)
                            else -> stringResource(R.string.mail_manage_not_taught)
                        },
                        Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium,
                    )
                    Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        for ((choice, title) in listOf("spam" to R.string.mail_manage_teach_spam, "ham" to R.string.mail_manage_teach_ham)) {
                            OutlinedButton(enabled = !isTeaching && label != choice, onClick = {
                                isTeaching = true
                                scope.launch {
                                    try {
                                        val taught = connection.teachSpamFilter(mailId, choice)
                                        trainingLabel = taught.trainingLabel.ifEmpty { null }
                                        hasTrainingChanged = true
                                        snackbar.show(context.getString(R.string.mail_manage_taught_done))
                                    } catch (failed: kotlinx.coroutines.CancellationException) {
                                        throw failed
                                    } catch (failed: Throwable) {
                                        snackbar.showError(failed)
                                    } finally {
                                        isTeaching = false
                                    }
                                }
                            }) { Text(stringResource(title)) }
                        }
                        if (label != null) TextButton(enabled = !isTeaching, onClick = {
                            isTeaching = true
                            scope.launch {
                                try {
                                    connection.teachSpamFilter(mailId, null)
                                    trainingLabel = null
                                    hasTrainingChanged = true
                                } catch (failed: kotlinx.coroutines.CancellationException) {
                                    throw failed
                                } catch (failed: Throwable) {
                                    snackbar.showError(failed)
                                } finally {
                                    isTeaching = false
                                }
                            }
                        }) { Text(stringResource(R.string.mail_manage_forget)) }
                    }
                }
                HorizontalDivider(Modifier.padding(vertical = 8.dp))
                DetailRow(stringResource(R.string.mail_from), mail.shownSender)
                DetailRow(stringResource(R.string.mail_manage_envelope_sender), mail.sender, isMonospaced = true)
                DetailRow(stringResource(R.string.mail_to), (mail.recipients ?: emptyList()).joinToString(", "))
                DetailRow(stringResource(R.string.mail_received), ManageLabels.fullTime(mail.receivedAt))
                DetailRow(stringResource(R.string.mail_manage_connection), connectionText(mail))
                DetailRow(stringResource(R.string.mail_manage_message_id), mail.messageId, isMonospaced = true)
                DetailRow(stringResource(R.string.mail_manage_size), ManageLabels.byteText(mail.byteCount))
                mail.authenticationResults?.let { results -> AuthenticationRows(results, onOpenSpamRules = { rules -> subPage = AuditSubPage.SpamRules(rules) }) }
                val opens = shown.opens
                if (opens != null && opens.isTrackable) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_pictures_fetched))
                    Text(
                        if (opens.isOpened) stringResource(R.string.mail_manage_opens_found, ManageLabels.fullTime(opens.openedAt).orEmpty()) else stringResource(R.string.mail_manage_opens_none),
                        Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium,
                    )
                    Text(stringResource(R.string.mail_manage_opens_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_deliveries))
                if (shown.deliveries.isEmpty()) Text(stringResource(R.string.mail_manage_no_deliveries), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (delivery in shown.deliveries) DeliveryRow(delivery, Modifier.clickable { subPage = AuditSubPage.DeliveryDetail(delivery) })
                val content = shown.content
                if (content != null) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_message))
                    if (!content.available) {
                        Text(stringResource(R.string.mail_no_longer_stored), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    } else {
                        if (content.html.isNotEmpty()) TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_rendered), onClick = { subPage = AuditSubPage.Rendered(content) })
                        if (content.text.isNotEmpty()) TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_text), onClick = { subPage = AuditSubPage.Source(context.getString(R.string.mail_manage_text), content.text, false) })
                        if (content.html.isNotEmpty()) TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_html), onClick = { subPage = AuditSubPage.Source(context.getString(R.string.mail_manage_html), content.html, true) })
                        TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_headers), onClick = { subPage = AuditSubPage.Headers((content.headers ?: emptyList()).filterNotNull()) })
                        val raw = content.rawHeaders
                        if (!raw.isNullOrEmpty()) TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_raw_headers), onClick = { subPage = AuditSubPage.Source(context.getString(R.string.mail_manage_raw_headers), raw, true) })
                        TeaNodeRow(headline = stringResource(R.string.mail_manage_download_eml), onClick = {
                            scope.launch { openFile(context, connection, snackbar, "/api/v1/mail/${mail.id}/raw", mail.id + ".eml") }
                        })
                        for (file in content.presentAttachments) {
                            TeaNodeRow(
                                headline = file.filename.ifEmpty { stringResource(R.string.mail_attachment) },
                                supporting = listOf(file.contentType, ManageLabels.byteText(file.size)).filter { part -> part.isNotEmpty() }.joinToString(" · "),
                                onClick = { scope.launch { openFile(context, connection, snackbar, "/api/v1/mail/${mail.id}/attachment/${file.index}", file.filename) } },
                            )
                        }
                    }
                }
            }
        }
    }
}

/** Opens a file of a message the operator is looking at, saying so when it cannot be fetched. */
private suspend fun openFile(context: android.content.Context, connection: Connection, snackbar: com.teanode.app.ui.components.SnackbarController, path: String, name: String) {
    try {
        com.teanode.app.mail.openServerFile(context, connection, path, name)
    } catch (failed: kotlinx.coroutines.CancellationException) {
        throw failed
    } catch (failed: Throwable) {
        snackbar.showError(failed)
    }
}

private fun connectionText(mail: AuditedMail): String {
    val parts = mutableListOf<String>()
    mail.ip?.takeIf { ip -> ip.isNotEmpty() }?.let { ip -> parts.add(ip + (mail.reverseDnsName?.takeIf { name -> name.isNotEmpty() }?.let { name -> " (${ManagedWords.hostName(name)})" } ?: "")) }
    mail.location?.placeText?.takeIf { place -> place.isNotEmpty() }?.let { place -> parts.add(place) }
    mail.hello?.takeIf { hello -> hello.isNotEmpty() }?.let { hello -> parts.add("HELO $hello") }
    listOfNotNull(mail.tlsVersion, mail.tlsCipherSuite).filter { text -> text.isNotEmpty() }.takeIf { tls -> tls.isNotEmpty() }?.let { tls -> parts.add(tls.joinToString(" ")) }
    return parts.joinToString("\n")
}

/** The checks a message went through on arrival, each with what it looked at to decide, and where the domains it claims receive mail. */
@Composable
fun AuthenticationRows(results: MailAuthenticationResults, onOpenSpamRules: (List<MailAuthenticationResults.SpamRule>) -> Unit = {}) {
    val context = LocalContext.current
    TeaNodeSectionHeader(stringResource(R.string.mail_manage_authentication))
    // Once the sender's DMARC policy is satisfied, an SPF or signature failure is what forwarding does to a message: the word stays, the red goes.
    val isDmarcPassed = results.dmarc?.isPassed ?: false
    fun toneUnderDmarc(result: String?): ManagedTone = ManagedTone.of(result).let { tone -> if (isDmarcPassed && tone == ManagedTone.Bad) ManagedTone.Plain else tone }
    val none = stringResource(R.string.mail_manage_none)
    var checkCount = 0
    results.spf?.let { spf ->
        checkCount += 1
        val domain = spf.domain.orEmpty()
        val address = spf.ip.orEmpty()
        val detail = when ((spf.spfResult ?: "").lowercase()) {
            "pass" -> context.getString(R.string.mail_manage_spf_pass, domain, address)
            "fail" -> context.getString(R.string.mail_manage_spf_fail, domain, address)
            "softfail" -> context.getString(R.string.mail_manage_spf_softfail, domain, address)
            "neutral" -> context.getString(R.string.mail_manage_spf_neutral, domain, address)
            "none" -> context.getString(R.string.mail_manage_spf_none, domain, address)
            "temperror", "permerror" -> context.getString(R.string.mail_manage_spf_error, domain, address)
            else -> context.getString(R.string.mail_manage_spf_other, domain, address)
        }
        CheckRow("SPF", spf.spfResult?.ifEmpty { null } ?: none, toneUnderDmarc(spf.spfResult), detail)
    }
    for (dkim in results.dkims ?: emptyList()) {
        checkCount += 1
        var detail = ManagedWords.dkimRecordName(dkim.selector, dkim.domain)
        dkim.identifier?.takeIf { identifier -> identifier.isNotEmpty() }?.let { identifier -> detail += " · " + context.getString(R.string.mail_manage_signed_for, identifier) }
        CheckRow("DKIM", dkim.dkimResult?.ifEmpty { null } ?: none, toneUnderDmarc(dkim.dkimResult), detail)
    }
    results.dmarc?.let { dmarc ->
        checkCount += 1
        val forwarderDomain = dmarc.passedAtForwarderDomain.orEmpty()
        val parts = mutableListOf(dmarc.domain.orEmpty())
        if (forwarderDomain.isNotEmpty()) parts.add(0, context.getString(R.string.mail_manage_failed_after_forwarding, forwarderDomain))
        dmarc.policy?.takeIf { policy -> policy.isNotEmpty() }?.let { policy -> parts.add("p=$policy") }
        dmarc.subdomainPolicy?.takeIf { policy -> policy.isNotEmpty() }?.let { policy -> parts.add("sp=$policy") }
        val alignments = listOfNotNull(
            dmarc.dkimAlignment?.takeIf { mode -> mode.isNotEmpty() }?.let { mode -> context.getString(R.string.mail_manage_dkim_alignment, ManageLabels.alignmentMode(context, mode)) },
            dmarc.spfAlignment?.takeIf { mode -> mode.isNotEmpty() }?.let { mode -> context.getString(R.string.mail_manage_spf_alignment, ManageLabels.alignmentMode(context, mode)) },
        )
        parts.add(alignments.joinToString(", "))
        CheckRow(
            "DMARC", if (forwarderDomain.isEmpty()) dmarc.dmarcResult?.ifEmpty { null } ?: none else context.getString(R.string.mail_manage_pass_at, forwarderDomain),
            if (forwarderDomain.isEmpty()) ManagedTone.of(dmarc.dmarcResult) else ManagedTone.Good, parts.filter { part -> part.isNotEmpty() }.joinToString(" · "),
        )
    }
    results.arc?.let { arc ->
        checkCount += 1
        val instances = arc.instanceCount ?: 0
        CheckRow("ARC", arc.arcResult?.ifEmpty { null } ?: none, ManagedTone.of(arc.arcResult), if (instances > 0) context.getString(R.string.mail_manage_sealed_times, instances) else context.getString(R.string.mail_manage_no_sealed_chain))
    }
    results.spamFilter?.let { spam ->
        checkCount += 1
        val symbols = spam.symbols ?: emptyList()
        val detail = if ((spam.checks ?: emptyList()).isEmpty()) (if (symbols.isEmpty()) context.getString(R.string.mail_manage_no_rules_matched) else symbols.joinToString(" ")) else ""
        CheckRow(context.getString(R.string.mail_manage_spam), ManagedWords.score(spam.score), if (spam.spamResult == "fail") ManagedTone.Bad else ManagedTone.Good, detail)
    }
    results.antivirus?.let { antivirus ->
        checkCount += 1
        val viruses = antivirus.viruses ?: emptyList()
        CheckRow(context.getString(R.string.mail_manage_virus), context.getString(if (viruses.isEmpty()) R.string.mail_manage_clean else R.string.mail_manage_infected), if (viruses.isEmpty()) ManagedTone.Good else ManagedTone.Bad, viruses.joinToString(", "))
    }
    results.contentFilter?.unsafeExtensions?.takeIf { extensions -> extensions.isNotEmpty() }?.let { extensions ->
        checkCount += 1
        CheckRow(context.getString(R.string.mail_manage_attachments), context.getString(R.string.mail_manage_unsafe), ManagedTone.Bad, extensions.joinToString(", "))
    }
    if (checkCount == 0) Text(stringResource(R.string.mail_manage_nothing_checked), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
    val spamRules = results.spamFilter?.checks
    if (!spamRules.isNullOrEmpty()) TeaNodeNavigationRow(headline = stringResource(R.string.mail_manage_spam_filter_rules), onClick = { onOpenSpamRules(spamRules) })
    val senderServers = results.senderMx?.mailServers
    if (results.senderMx != null && !senderServers.isNullOrEmpty()) MxRow(results.senderMx?.domain.orEmpty(), senderServers)
    val fromServers = results.fromMx?.mailServers
    if (results.fromMx != null && !fromServers.isNullOrEmpty() && (results.fromMx?.domain != results.senderMx?.domain || (results.senderMx?.mailServers ?: emptyList()).isEmpty())) MxRow(results.fromMx?.domain.orEmpty(), fromServers)
    for (message in results.errorMessages ?: emptyList()) Text(message, Modifier.padding(horizontal = 16.dp, vertical = 2.dp), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
}

/** Where a domain the message claims receives its mail. */
@Composable
private fun MxRow(domainName: String, mailServers: List<String>) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Text(stringResource(R.string.mail_manage_mail_servers_for, domainName), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        SelectionContainer { Text(mailServers.joinToString(", "), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace) }
    }
}

@Composable
private fun CheckRow(name: String, result: String, tone: ManagedTone, detail: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.Top) {
        Column(Modifier.weight(1f)) {
            Text(name, style = MaterialTheme.typography.bodyMedium)
            if (detail.isNotEmpty()) SelectionContainer { Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        ManageTag(result, tone)
    }
}

/** One recipient of a message and what happened about it. */
@Composable
fun DeliveryRow(delivery: MailDelivery, modifier: Modifier = Modifier, onRetry: (() -> Unit)? = null) {
    val context = LocalContext.current
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(delivery.recipient.orEmpty(), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            ManageTag(ManageLabels.mailStatus(context, delivery.deliveryStatus), ManagedTone.of(delivery.deliveryStatus))
        }
        val facts = listOfNotNull(
            delivery.deliveryKind?.takeIf { kind -> kind.isNotEmpty() }?.let { kind -> ManageLabels.mailKind(context, kind) },
            delivery.deliveryMethod?.takeIf { method -> method.isNotEmpty() }, delivery.attemptCount?.takeIf { count -> count > 0 }?.let { count -> context.getString(R.string.mail_manage_attempts, count) },
            ManageLabels.relativeTime(delivery.attemptedAt),
        )
        if (facts.isNotEmpty()) Text(facts.joinToString(" · "), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        delivery.errorMessage?.takeIf { error -> error.isNotEmpty() }?.let { error -> Text(error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, maxLines = 3, overflow = TextOverflow.Ellipsis) }
        delivery.retryAt?.takeIf { retry -> retry.isNotEmpty() }?.let { retry -> Text(stringResource(R.string.mail_manage_next_attempt, ManageLabels.fullTime(retry).orEmpty()), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        for ((reportingMta, report) in delivery.recipientReports) {
            Text(
                listOfNotNull(reportingMta, report.reportStatus, report.diagnosticCode).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 3, overflow = TextOverflow.Ellipsis,
            )
        }
        if (onRetry != null) OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.mail_manage_retry_now)) }
    }
    HorizontalDivider()
}

/** The messages waiting to be delivered, those that failed first, with a way to try one again now. */
@Composable
fun DeliveryQueueScreen(onBack: () -> Unit, onOpenMail: (String) -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var filter by remember { mutableStateOf(DeliveryQueueFilter()) }
    var domains by remember { mutableStateOf(emptyList<ManagedDomain>()) }
    var deliveries by remember { mutableStateOf<List<MailDelivery>?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    LaunchedEffect(attempt) {
        try {
            domains = connection.managedDomainNames()
            deliveries = connection.pendingDeliveries()
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }
    val all = deliveries
    val shown = all?.let { loaded -> filter.applied(loaded).sortedWith(compareByDescending<MailDelivery> { delivery -> delivery.isFailing }.thenBy { delivery -> delivery.retryAt ?: "" }) }
    ManagedList(
        title = stringResource(R.string.mail_manage_queue), onBack = onBack, rows = shown, failure = failure, onRetry = { failure = null; attempt += 1 },
        searchText = filter.searchText, onSearch = { typed -> filter = filter.copy(searchText = typed) }, searchPrompt = stringResource(R.string.mail_manage_recipient_or_error),
        filterMenu = {
            FilterMenu(
                filter.isFiltering,
                listOf(
                    FilterPicker(stringResource(R.string.mail_manage_domain), stringResource(R.string.mail_manage_all_domains), domains.map { domain -> domain.id to domain.domainName }, filter.domainId) { chosen -> filter = filter.copy(domainId = chosen) },
                    FilterPicker(stringResource(R.string.mail_manage_status), stringResource(R.string.mail_manage_any_status), (all ?: emptyList()).mapNotNull { delivery -> delivery.deliveryStatus }.toSortedSet().map { status -> status to ManageLabels.mailStatus(context, status) }, filter.deliveryStatus) { chosen -> filter = filter.copy(deliveryStatus = chosen) },
                    FilterPicker(stringResource(R.string.mail_manage_kind), stringResource(R.string.mail_manage_any_kind), (all ?: emptyList()).mapNotNull { delivery -> delivery.deliveryKind }.toSortedSet().map { kind -> kind to ManageLabels.mailKind(context, kind) }, filter.deliveryKind) { chosen -> filter = filter.copy(deliveryKind = chosen) },
                ),
            )
        },
        emptyTitle = stringResource(if (all?.isEmpty() == true) R.string.mail_manage_queue_empty else R.string.mail_nothing_matches),
        footer = { count -> pluralStringResource(R.plurals.mail_manage_deliveries_count, count, count) },
        key = { delivery -> delivery.id },
    ) { delivery ->
        DeliveryRow(delivery, Modifier.clickable { delivery.mailId?.let { mailId -> onOpenMail(mailId) } }, onRetry = {
            scope.launch {
                try {
                    connection.retryDelivery(delivery.id)
                    snackbar.show(context.getString(R.string.mail_manage_retry_asked))
                    attempt += 1
                } catch (failed: kotlinx.coroutines.CancellationException) {
                    throw failed
                } catch (failed: Throwable) {
                    snackbar.showError(failed)
                }
            }
        })
    }
}

/** The DMARC reports other servers sent about mail that claimed to come from the person's domains. */
@Composable
fun ReportsScreen(onBack: () -> Unit, onOpenReport: (String) -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    var filter by remember { mutableStateOf(ReportListFilter()) }
    var domains by remember { mutableStateOf(emptyList<ManagedDomain>()) }
    var reports by remember { mutableStateOf<List<DMARCReport>?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    LaunchedEffect(attempt) {
        try {
            domains = connection.managedDomainNames()
            reports = connection.dmarcReports()
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            failure = failed
        }
    }
    val all = reports
    val shown = all?.let { loaded -> filter.applied(loaded) }
    ManagedList(
        title = stringResource(R.string.mail_manage_reports), onBack = onBack, rows = shown, failure = failure, onRetry = { failure = null; attempt += 1 },
        searchText = filter.searchText, onSearch = { typed -> filter = filter.copy(searchText = typed) }, searchPrompt = stringResource(R.string.mail_manage_domain_or_sender),
        filterMenu = {
            FilterMenu(
                filter.isFiltering,
                listOf(
                    FilterPicker(stringResource(R.string.mail_manage_domain), stringResource(R.string.mail_manage_all_domains), domains.map { domain -> domain.id to domain.domainName }, filter.domainId) { chosen -> filter = filter.copy(domainId = chosen) },
                    FilterPicker(stringResource(R.string.mail_manage_alignment), stringResource(R.string.mail_manage_any_alignment), ReportAlignment.entries.map { alignment -> alignment.name to ManageLabels.alignment(context, alignment) }, filter.alignment?.name) { chosen -> filter = filter.copy(alignment = ReportAlignment.entries.firstOrNull { alignment -> alignment.name == chosen }) },
                    FilterPicker(stringResource(R.string.mail_manage_disposition), stringResource(R.string.mail_manage_any_disposition), (all ?: emptyList()).mapNotNull { report -> report.disposition }.filter { text -> text.isNotEmpty() }.toSortedSet().map { disposition -> disposition to disposition }, filter.disposition) { chosen -> filter = filter.copy(disposition = chosen) },
                ),
            )
        },
        emptyTitle = stringResource(if (all?.isEmpty() == true) R.string.mail_manage_no_reports_yet else R.string.mail_nothing_matches),
        footer = { count -> pluralStringResource(R.plurals.mail_manage_reports_count, count, count) },
        key = { report -> report.id },
    ) { report ->
        Column(Modifier.fillMaxWidth().clickable { onOpenReport(report.id) }.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(report.fromDomain.orEmpty(), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                ManageLabels.relativeTime(report.endAt ?: report.beginAt)?.let { whenText -> Text(whenText, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                ManageTag(ManageLabels.alignment(context, report.alignment), if (report.alignment.isPassed) ManagedTone.Good else ManagedTone.Bad)
                Text(
                    listOfNotNull(report.ip, report.messageCount?.let { count -> context.resources.getQuantityString(R.plurals.mail_messages_count, count, count) }, report.disposition?.takeIf { text -> text.isNotEmpty() }).joinToString(" · "),
                    style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
        }
        HorizontalDivider()
    }
}

/** One DMARC report as it arrived: who sent it, the policy they saw, and what they did with each batch of mail. */
@Composable
fun ReportDetailScreen(reportId: String, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    val report = rememberLoad(reportId) { connection.dmarcReport(reportId) }
    var openedRecord by remember { mutableStateOf<DMARCReport.FeedbackRecord?>(null) }
    openedRecord?.let { record ->
        FeedbackRecordPage(record, onBack = { openedRecord = null })
        return
    }
    TeaNodeScreen(title = stringResource(R.string.mail_manage_report), onBack = onBack) { contentPadding ->
        LoadedContent(report, modifier = Modifier.padding(contentPadding), isEmpty = { found -> found == null }, empty = { EmptyState(Icons.Outlined.Search, stringResource(R.string.mail_manage_not_found)) }) { found: DMARCReport? ->
            val shown = found ?: return@LoadedContent
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    ManageTag(ManageLabels.alignment(context, shown.alignment), if (shown.alignment.isPassed) ManagedTone.Good else ManagedTone.Bad)
                    shown.disposition?.takeIf { text -> text.isNotEmpty() }?.let { disposition -> ManageTag(disposition, ManagedTone.Plain) }
                }
                DetailRow(stringResource(R.string.mail_manage_claimed_domain), shown.fromDomain)
                DetailRow(stringResource(R.string.mail_manage_sending_address), listOfNotNull(shown.ip, shown.reverseDnsName?.let { name -> "(${ManagedWords.hostName(name)})" }).joinToString(" "))
                DetailRow(stringResource(R.string.mail_manage_place), shown.location?.placeText)
                DetailRow(stringResource(R.string.mail_manage_period), listOfNotNull(ManageLabels.fullTime(shown.beginAt), ManageLabels.fullTime(shown.endAt)).joinToString(" - "))
                val feedback = shown.feedback
                if (feedback != null) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_report_from))
                    DetailRow(stringResource(R.string.mail_manage_organization), feedback.organizationName)
                    DetailRow(stringResource(R.string.mail_manage_contact), feedback.email)
                    DetailRow(stringResource(R.string.mail_manage_report_id), feedback.reportId, isMonospaced = true)
                    DetailRow(stringResource(R.string.mail_manage_policy_seen), feedback.policyText)
                    DetailRow(stringResource(R.string.mail_manage_alignment), listOfNotNull(feedback.dkimAlignment?.let { mode -> "DKIM " + ManageLabels.alignmentMode(context, mode) }, feedback.spfAlignment?.let { mode -> "SPF " + ManageLabels.alignmentMode(context, mode) }).joinToString(" · "))
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_what_they_did))
                    for (record in feedback.records ?: emptyList()) {
                        Column(Modifier.clickable { openedRecord = record }) { FeedbackRecordSummary(record) }
                        HorizontalDivider()
                    }
                }
            }
        }
    }
}

/** The domains the server handles mail for, each with a line saying whether its DNS records are published. */
@Composable
fun DomainsScreen(onBack: () -> Unit, onOpenDomain: (String) -> Unit, connection: Connection = LocalConnection.current) {
    val domains = rememberLoad(connection) { connection.managedDomains() }
    var isAdding by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.mail_manage_domains), onBack = onBack,
        actions = { IconButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.mail_manage_new_domain)) } },
    ) { contentPadding ->
        LoadedContent(domains, modifier = Modifier.padding(contentPadding), isEmpty = { list -> list.isEmpty() }, empty = { EmptyState(Icons.Outlined.Language, stringResource(R.string.mail_manage_no_domains), description = stringResource(R.string.mail_manage_no_domains_description)) }) { list ->
            LazyColumn(Modifier.fillMaxSize()) {
                items(list, key = { domain -> domain.id }) { domain ->
                    TeaNodeRow(
                        headline = domain.domainName, leadingIcon = Icons.Outlined.Language, onClick = { onOpenDomain(domain.id) },
                        supporting = domain.comment?.takeIf { comment -> comment.isNotEmpty() },
                        trailing = { DnsSummaryTag(domain.dnsSummary) },
                    )
                }
            }
        }
    }
    if (isAdding) NewDomainDialog(onDismiss = { isAdding = false }, onCreated = { domainId -> isAdding = false; onOpenDomain(domainId) })
}

/** What a domain's DNS check says in one tag. */
@Composable
fun DnsSummaryTag(summary: DomainDNSSummary) {
    when (summary) {
        DomainDNSSummary.NotChecked -> ManageTag(stringResource(R.string.mail_manage_dns_not_checked), ManagedTone.Plain)
        is DomainDNSSummary.AllPublished -> ManageTag(stringResource(R.string.mail_manage_dns_all_published), ManagedTone.Good)
        is DomainDNSSummary.Missing -> ManageTag(pluralStringResource(R.plurals.mail_manage_dns_missing, summary.missingCount, summary.missingCount), ManagedTone.Warn)
    }
}
