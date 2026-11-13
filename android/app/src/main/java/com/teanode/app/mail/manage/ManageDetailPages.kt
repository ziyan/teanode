package com.teanode.app.mail.manage

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.mail.ChipRow
import com.teanode.app.mail.MailBody
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.Connection
import com.teanode.kit.mail.DMARCReport
import com.teanode.kit.mail.DNSRecord
import com.teanode.kit.mail.DomainCredential
import com.teanode.kit.mail.DomainLayout
import com.teanode.kit.mail.DomainTemplate
import com.teanode.kit.mail.MailAuthenticationResults
import com.teanode.kit.mail.MailContent
import com.teanode.kit.mail.MailDelivery
import com.teanode.kit.mail.ManagedTone
import com.teanode.kit.mail.ManagedWords
import com.teanode.kit.mail.deleteDomainCredential
import com.teanode.kit.mail.domainLayout
import com.teanode.kit.mail.domainTemplate
import com.teanode.kit.mail.updateDomainCredential
import kotlinx.coroutines.launch

// The pages the operator opens from the pages of a message, a report and a domain: each is a full screen that replaces
// the page it was opened from, and back returns to it.

/** A page of one message the operator can open: its checks, one delivery, or the message as it came. */
sealed class AuditSubPage {
    data class SpamRules(val rules: List<MailAuthenticationResults.SpamRule>) : AuditSubPage()
    data class DeliveryDetail(val delivery: MailDelivery) : AuditSubPage()
    data class Rendered(val content: MailContent) : AuditSubPage()
    data class Source(val title: String, val sourceText: String, val isMonospaced: Boolean) : AuditSubPage()
    data class Headers(val headers: List<MailContent.Header>) : AuditSubPage()
}

/** The page shown over the message for [page]. */
@Composable
fun AuditSubPageScreen(page: AuditSubPage, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    when (page) {
        is AuditSubPage.SpamRules -> SpamRulesPage(page.rules, onBack)
        is AuditSubPage.DeliveryDetail -> DeliveryDetailPage(page.delivery, onBack)
        is AuditSubPage.Rendered -> RenderedMailPage(page.content, onBack)
        is AuditSubPage.Source -> MailSourcePage(page.title, page.sourceText, page.isMonospaced, onBack)
        is AuditSubPage.Headers -> MailHeadersPage(page.headers, onBack)
    }
}

internal fun copyToClipboard(context: Context, copiedText: String) {
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("", copiedText))
}

/** The built-in filter's rules that matched a message, and the points each added. */
@Composable
fun SpamRulesPage(rules: List<MailAuthenticationResults.SpamRule>, onBack: () -> Unit) {
    TeaNodeScreen(title = stringResource(R.string.mail_manage_spam_filter_rules), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            for (rule in rules) {
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(rule.symbol, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                        Text(
                            (if (rule.score > 0) "+" else "") + ManagedWords.score(rule.score), style = MaterialTheme.typography.bodyMedium,
                            color = if (rule.score > 0) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    rule.ruleDescription?.takeIf { description -> description.isNotEmpty() }?.let { description ->
                        Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                HorizontalDivider()
            }
        }
    }
}

/** How a delivery is handed on, and where: the thing to check when one is stuck. */
@Composable
private fun deliveryMethodText(delivery: MailDelivery): String? {
    val destination = delivery.destination?.takeIf { text -> text.isNotEmpty() } ?: return null
    val method = delivery.deliveryMethod?.takeIf { text -> text.isNotEmpty() } ?: return null
    return when (method) {
        "email" -> stringResource(R.string.mail_manage_method_email, destination)
        "mailServer" -> stringResource(R.string.mail_manage_method_mail_server, destination)
        "webhook" -> stringResource(R.string.mail_manage_method_webhook, destination)
        else -> stringResource(R.string.mail_manage_method_other, destination)
    }
}

/** One delivery of a message: how it is handed on and where, its attempts, and what the far end said. */
@Composable
fun DeliveryDetailPage(delivery: MailDelivery, onBack: () -> Unit) {
    val attempts = delivery.attemptCount
    TeaNodeScreen(title = delivery.recipient?.ifEmpty { null } ?: stringResource(R.string.mail_manage_delivery), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            DeliveryRow(delivery)
            deliveryMethodText(delivery)?.let { text -> Text(text, Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            DetailRow(
                stringResource(R.string.mail_manage_attempts_label),
                if (attempts == null || attempts <= 0) stringResource(R.string.mail_manage_not_attempted)
                else stringResource(R.string.mail_manage_attempt_count_last, attempts, ManageLabels.fullTime(delivery.attemptedAt).orEmpty()),
            )
            DetailRow(stringResource(R.string.mail_manage_delivered_label), ManageLabels.fullTime(delivery.deliveredAt))
            DetailRow(stringResource(R.string.mail_manage_dropped_label), ManageLabels.fullTime(delivery.droppedAt))
            DetailRow(stringResource(R.string.mail_manage_next_attempt_label), ManageLabels.fullTime(delivery.retryAt))
            DetailRow(stringResource(R.string.mail_manage_sender_notified), ManageLabels.fullTime(delivery.notifiedAt))
            DetailRow(stringResource(R.string.mail_manage_size_label), ManageLabels.byteText(delivery.byteCount))
            DetailRow(stringResource(R.string.mail_manage_last_error), delivery.errorMessage)
            val reports = delivery.recipientReports
            if (reports.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_far_end_said))
                for ((_, report) in reports) {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                        report.reportStatus?.takeIf { status -> status.isNotEmpty() }?.let { status ->
                            Text(stringResource(R.string.mail_manage_far_end_said_status, status), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(listOfNotNull(report.remoteMta, report.reportAction).filter { text -> text.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                        report.diagnosticCode?.takeIf { code -> code.isNotEmpty() }?.let { code ->
                            SelectionContainer { Text(code, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                    }
                }
            }
        }
    }
}

/** A message as sent, in a web view with scripts off and its pictures held back until asked for. Nothing is recorded: looking at somebody's mail is not choosing for them. */
@Composable
fun RenderedMailPage(content: MailContent, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    var isShowingPictures by remember { mutableStateOf(false) }
    var isOriginalLook by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.mail_manage_rendered), onBack = onBack,
        actions = {
            if (isSystemInDarkTheme()) {
                TextButton(onClick = { isOriginalLook = !isOriginalLook }) {
                    Text(stringResource(if (isOriginalLook) R.string.mail_manage_show_dark_theme else R.string.mail_manage_show_original_theme))
                }
            }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (content.hasRemoteContent == true && content.imagesAllowed != true && !isShowingPictures) {
                Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(stringResource(R.string.mail_manage_remote_images_blocked), style = MaterialTheme.typography.bodySmall)
                    TextButton(onClick = { isShowingPictures = true }) { Text(stringResource(R.string.mail_manage_load_anyway)) }
                }
            }
            MailBody(content.html, content.mailId, connection, isShowingPictures = isShowingPictures || content.imagesAllowed == true, isOriginalLook = isOriginalLook)
        }
    }
}

/** A part of a message as text: its plain text, its cleaned HTML or its headers as they arrived. */
@Composable
fun MailSourcePage(title: String, sourceText: String, isMonospaced: Boolean, onBack: () -> Unit) {
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    TeaNodeScreen(
        title = title, onBack = onBack,
        actions = {
            IconButton(onClick = { copyToClipboard(context, sourceText); snackbar.show(context.getString(R.string.mail_manage_copied)) }) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.mail_manage_copy)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            SelectionContainer {
                Text(sourceText, Modifier.fillMaxWidth().padding(16.dp), style = if (isMonospaced) MaterialTheme.typography.bodySmall else MaterialTheme.typography.bodyMedium, fontFamily = if (isMonospaced) FontFamily.Monospace else null)
            }
        }
    }
}

/** A message's headers, a name and its value to a row. */
@Composable
fun MailHeadersPage(headers: List<MailContent.Header>, onBack: () -> Unit) {
    TeaNodeScreen(title = stringResource(R.string.mail_manage_headers), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            for (header in headers) {
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Text(header.key, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    SelectionContainer { Text(header.value, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
                }
                HorizontalDivider()
            }
        }
    }
}

/** One source of a report: how the receiver evaluated it, and the signatures and SPF checks behind that. */
@Composable
fun FeedbackRecordPage(record: DMARCReport.FeedbackRecord, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    TeaNodeScreen(title = record.sourceIp.orEmpty(), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            FeedbackRecordSummary(record)
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            DetailRow(
                stringResource(R.string.mail_manage_evaluated_as),
                listOf(
                    record.dkimEvaluation?.takeIf { text -> text.isNotEmpty() }?.let { text -> "DKIM $text" }, record.spfEvaluation?.takeIf { text -> text.isNotEmpty() }?.let { text -> "SPF $text" },
                ).filterNotNull().joinToString(" · "),
            )
            // A forwarder and a mailing list both break alignment legitimately, and this is the only place that is said.
            val reasonType = record.reasonType?.takeIf { text -> text.isNotEmpty() }
            DetailRow(stringResource(R.string.mail_manage_overridden_because), if (reasonType == null) null else record.reasonComment?.takeIf { text -> text.isNotEmpty() }?.let { comment -> "$reasonType: $comment" } ?: reasonType)
            DetailRow(stringResource(R.string.mail_manage_from_header), record.headerFrom, isMonospaced = true)
            DetailRow(stringResource(R.string.mail_manage_envelope_sender), record.envelopeFrom, isMonospaced = true)
            DetailRow(stringResource(R.string.mail_manage_envelope_recipient), record.envelopeTo, isMonospaced = true)
            val dkimResults = record.dkimResults ?: emptyList()
            val spfResults = record.spfResults ?: emptyList()
            if (dkimResults.isNotEmpty() || spfResults.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_dkim_and_spf))
                for (dkim in dkimResults) {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                        Text("DKIM ${dkim.dkimResult.orEmpty()}", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(ManagedWords.dkimRecordName(dkim.selector, dkim.domain), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                        dkim.humanResult?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                }
                for (spf in spfResults) {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                        Text("SPF ${spf.spfResult.orEmpty()}", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(listOfNotNull(spf.domain, spf.scope?.takeIf { text -> text.isNotEmpty() }?.let { text -> "($text)" }).filter { text -> text.isNotEmpty() }.joinToString(" "), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                    }
                }
            }
        }
    }
}

/** The tags of one source of a report: how many messages, and what the receiver did with them. */
@Composable
fun FeedbackRecordSummary(record: DMARCReport.FeedbackRecord) {
    val context = LocalContext.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(record.sourceIp.orEmpty(), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val count = record.messageCount ?: 0
            ManageTag(context.resources.getQuantityString(R.plurals.mail_manage_messages_count, count, count), if (record.isPassed) ManagedTone.Good else ManagedTone.Bad)
            ManageTag(record.disposition?.ifEmpty { null } ?: stringResource(R.string.mail_manage_none), ManagedTone.of(record.disposition))
        }
    }
}

// MARK: Domain pages

/** A page of one domain the operator can open: a DNS record, a credential to change, a template or a layout. */
sealed class DomainSubPage {
    data class Record(val record: DNSRecord) : DomainSubPage()
    data class Credential(val credential: DomainCredential) : DomainSubPage()
    data class Template(val templateId: String, val layoutName: String?) : DomainSubPage()
    data class Layout(val layoutId: String) : DomainSubPage()
}

/** One record to publish: its name and the value to put there, each a tap from the clipboard, what is published now, and what is stopping it when something is. */
@Composable
fun DnsRecordPage(record: DNSRecord, onBack: () -> Unit) {
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    BackHandler(onBack = onBack)
    @Composable
    fun CopyRow(copiedText: String) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalAlignment = Alignment.Top) {
            SelectionContainer { Text(copiedText, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace) }
            IconButton(onClick = { copyToClipboard(context, copiedText); snackbar.show(context.getString(R.string.mail_manage_copied)) }) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.mail_manage_copy)) }
        }
    }
    TeaNodeScreen(title = record.recordType.orEmpty(), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(record.recordType.orEmpty(), Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                DnsRecordTag(record)
            }
            record.purpose?.takeIf { text -> text.isNotEmpty() && record.isVerified != true }?.let { text -> Text(text, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            record.blockedReason?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.tertiary) }
            TeaNodeSectionHeader(stringResource(R.string.mail_manage_name))
            CopyRow(record.name.orEmpty())
            TeaNodeSectionHeader(stringResource(R.string.mail_manage_required_value))
            if (record.publishedValue.isEmpty()) Text(stringResource(R.string.mail_manage_nothing_to_publish), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) else CopyRow(record.publishedValue)
            TeaNodeSectionHeader(stringResource(R.string.mail_manage_published_now))
            val found = record.foundValues ?: emptyList()
            when {
                record.isVerified == true -> Text(stringResource(R.string.mail_manage_matches), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                found.isNotEmpty() -> for (value in found) SelectionContainer { Text(value, Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace) }
                else -> Text(stringResource(R.string.mail_manage_nothing_published), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** Whether a record is published as asked, optional, or has to change. */
@Composable
fun DnsRecordTag(record: DNSRecord) {
    when {
        record.isVerified == true -> ManageTag(stringResource(R.string.mail_manage_record_ok), ManagedTone.Good)
        record.isOptional == true -> ManageTag(stringResource(R.string.mail_manage_record_optional), ManagedTone.Plain)
        else -> ManageTag(stringResource(R.string.mail_manage_record_change), ManagedTone.Warn)
    }
}

/** A credential changed: what it is for, the one address it may send as, whether it is refused; or removed. */
@Composable
fun EditCredentialPage(domainName: String, credential: DomainCredential, onBack: () -> Unit, onChanged: (String) -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    BackHandler(onBack = onBack)
    var comment by remember(credential.id) { mutableStateOf(credential.comment.orEmpty()) }
    var restrictedLocalPart by remember(credential.id) { mutableStateOf(credential.alias.orEmpty()) }
    var isDisabled by remember(credential.id) { mutableStateOf(credential.isDisabled ?: false) }
    var isBusy by remember { mutableStateOf(false) }
    var isRemoving by remember { mutableStateOf(false) }

    fun work(said: String, action: suspend () -> Unit) {
        isBusy = true
        scope.launch {
            try {
                action()
                onChanged(said)
            } catch (failed: kotlinx.coroutines.CancellationException) {
                throw failed
            } catch (failed: Throwable) {
                snackbar.showError(failed)
            } finally {
                isBusy = false
            }
        }
    }

    TeaNodeScreen(
        title = stringResource(R.string.mail_manage_edit_credential), onBack = onBack,
        actions = {
            TextButton(enabled = !isBusy, onClick = {
                work(context.getString(R.string.mail_manage_saved)) { connection.updateDomainCredential(credential.id, comment.trim(), restrictedLocalPart.trim(), isDisabled) }
            }) { Text(stringResource(R.string.mail_manage_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(comment, { typed -> comment = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_manage_what_will_hold_it)) }, singleLine = true)
            OutlinedTextField(
                restrictedLocalPart, { typed -> restrictedLocalPart = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_manage_send_only_as)) },
                placeholder = { Text(stringResource(R.string.mail_manage_any_address)) }, singleLine = true,
            )
            Text(stringResource(R.string.mail_manage_send_only_as_footer, domainName), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.mail_manage_disabled), Modifier.weight(1f))
                Switch(checked = isDisabled, onCheckedChange = { checked -> isDisabled = checked })
            }
            Text(stringResource(R.string.mail_manage_credential_disabled_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Column {
                Text(stringResource(R.string.mail_manage_username), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                SelectionContainer { Text(credential.id, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace) }
            }
            Text(stringResource(R.string.mail_manage_password_shown_once), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(enabled = !isBusy, onClick = { isRemoving = true }) { Text(stringResource(R.string.mail_manage_remove_credential), color = MaterialTheme.colorScheme.error) }
        }
    }
    if (isRemoving) {
        val credentialName = comment.trim().ifEmpty { credential.id }
        ConfirmationDialog(
            title = stringResource(R.string.mail_manage_remove_credential), text = stringResource(R.string.mail_manage_remove_named_credential, credentialName), confirmLabel = stringResource(R.string.mail_remove), isDestructive = true,
            onConfirm = { isRemoving = false; work(context.getString(R.string.mail_manage_removed)) { connection.deleteDomainCredential(credential.id) } }, onDismiss = { isRemoving = false },
        )
    }
}

/** One language of a template or a layout. */
class TemplateLanguage(val locale: String, val subject: String?, val htmlContent: String?, val textContent: String?)

/** The languages of a template, its own first. */
fun DomainTemplate.languages(): List<TemplateLanguage> =
    listOf(TemplateLanguage(locale.orEmpty(), subject.orEmpty(), htmlContent, textContent)) +
        (translations ?: emptyList()).map { translation -> TemplateLanguage(translation.locale, translation.subject.orEmpty(), translation.htmlContent, translation.textContent) }

/** The languages of a layout, its own first. */
fun DomainLayout.languages(): List<TemplateLanguage> =
    listOf(TemplateLanguage(locale.orEmpty(), null, htmlContent, textContent)) +
        (translations ?: emptyList()).map { translation -> TemplateLanguage(translation.locale, null, translation.htmlContent, translation.textContent) }

/** The subject, HTML and text of a template or a layout, in the language chosen at the top when there is more than one. */
@Composable
fun TemplateContentSections(languages: List<TemplateLanguage>, chosenLocale: String, onChoose: (String) -> Unit) {
    val chosen = languages.firstOrNull { language -> language.locale == chosenLocale } ?: languages.firstOrNull() ?: return
    if (languages.size > 1) {
        ChipRow {
            for (language in languages) {
                FilterChip(selected = language.locale == chosen.locale, onClick = { onChoose(language.locale) }, label = { Text(language.locale.ifEmpty { stringResource(R.string.mail_manage_default) }) })
            }
        }
    }
    chosen.subject?.let { subject ->
        TeaNodeSectionHeader(stringResource(R.string.mail_manage_subject))
        SelectionContainer { Text(subject.ifEmpty { " " }, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium) }
    }
    TeaNodeSectionHeader(stringResource(R.string.mail_manage_html))
    SelectionContainer { Text(chosen.htmlContent.orEmpty(), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
    TeaNodeSectionHeader(stringResource(R.string.mail_manage_text))
    SelectionContainer { Text(chosen.textContent.orEmpty(), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
    Text(stringResource(R.string.mail_manage_written_on_dashboard), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** One template to read: what it is, the layout it sits in, what it reads, and its words in each language. Templates are written on the dashboard. */
@Composable
fun TemplatePage(templateId: String, layoutName: String?, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    BackHandler(onBack = onBack)
    val loaded = rememberLoad(templateId) { connection.domainTemplate(templateId) ?: throw com.teanode.kit.TeaNodeError.Refused("api: not found") }
    var chosenLocale by remember(templateId) { mutableStateOf("") }
    TeaNodeScreen(title = loaded.value?.name.orEmpty(), onBack = onBack) { contentPadding ->
        LoadedContent(loaded, modifier = Modifier.padding(contentPadding)) { template: DomainTemplate ->
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                DetailRow(stringResource(R.string.mail_manage_name), template.name, isMonospaced = true)
                DetailRow(stringResource(R.string.mail_manage_layout), layoutName ?: stringResource(R.string.mail_manage_no_layout))
                DetailRow(stringResource(R.string.mail_manage_comment), template.comment)
                DetailRow(stringResource(R.string.mail_manage_variables), (template.variables ?: emptyList()).joinToString(", "), isMonospaced = true)
                DetailRow(stringResource(R.string.mail_manage_changed), ManageLabels.fullTime(template.modifiedAt))
                TemplateContentSections(template.languages(), chosenLocale.ifEmpty { template.locale.orEmpty() }, onChoose = { locale -> chosenLocale = locale })
            }
        }
    }
}

/** One layout to read: what it is for, and its words in each language. */
@Composable
fun LayoutPage(layoutId: String, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    BackHandler(onBack = onBack)
    val loaded = rememberLoad(layoutId) { connection.domainLayout(layoutId) ?: throw com.teanode.kit.TeaNodeError.Refused("api: not found") }
    var chosenLocale by remember(layoutId) { mutableStateOf("") }
    TeaNodeScreen(title = loaded.value?.comment?.ifEmpty { null } ?: stringResource(R.string.mail_manage_untitled_layout), onBack = onBack) { contentPadding ->
        LoadedContent(loaded, modifier = Modifier.padding(contentPadding)) { layout: DomainLayout ->
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                DetailRow(stringResource(R.string.mail_manage_what_it_is_for), layout.comment)
                DetailRow(stringResource(R.string.mail_manage_changed), ManageLabels.fullTime(layout.modifiedAt))
                TemplateContentSections(layout.languages(), chosenLocale.ifEmpty { layout.locale.orEmpty() }, onChoose = { locale -> chosenLocale = locale })
            }
        }
    }
}

/** The page shown over a domain for [page]. */
@Composable
fun DomainSubPageScreen(page: DomainSubPage, domainName: String, onBack: () -> Unit, onChanged: (String) -> Unit) {
    when (page) {
        is DomainSubPage.Record -> DnsRecordPage(page.record, onBack)
        is DomainSubPage.Credential -> EditCredentialPage(domainName, page.credential, onBack, onChanged)
        is DomainSubPage.Template -> TemplatePage(page.templateId, page.layoutName, onBack)
        is DomainSubPage.Layout -> LayoutPage(page.layoutId, onBack)
    }
}
