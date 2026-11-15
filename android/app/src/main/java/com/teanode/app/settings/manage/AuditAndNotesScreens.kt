package com.teanode.app.settings.manage

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.shortDateTime
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.access.AuditAction
import com.teanode.kit.access.AuditEvent
import com.teanode.kit.manage.serverUpgrade
import com.teanode.kit.photos.ServerTime

@Composable
fun auditEventSummary(event: AuditEvent): String {
    val name = event.resourceLabel?.ifEmpty { null } ?: event.resourceId
    return when (event.auditAction) {
        AuditAction.Create -> stringResource(R.string.settings_audit_created, event.resourceType, name)
        AuditAction.Update -> stringResource(R.string.settings_audit_changed, event.resourceType, name)
        AuditAction.Delete -> stringResource(R.string.settings_audit_removed, event.resourceType, name)
        null -> "${event.action} ${event.resourceType} $name"
    }
}

/**
 * One change in full: who made it and from where, the thing it was made to, and the fields that differ as a table of the field, before and
 * after. The table scrolls sideways on a phone rather than folding each row into lines.
 */
@Composable
fun AuditEventDetailScreen(event: AuditEvent, onBack: () -> Unit) {
    val unset = stringResource(R.string.settings_audit_unset)
    TeaNodeScreen(title = auditEventSummary(event), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).verticalScroll(rememberScrollState())) {
            TeaNodeRow(headline = stringResource(R.string.manage_audit_who), supporting = event.actorName)
            if (event.isByAgent) FooterText(stringResource(R.string.settings_audit_agent_made))
            event.impersonatorLabel?.ifEmpty { null }?.let { label -> TeaNodeRow(headline = stringResource(R.string.manage_audit_signed_in_by), supporting = label) }
            ServerTime.instant(event.createdAt)?.let { instant -> TeaNodeRow(headline = stringResource(R.string.manage_audit_when), supporting = shortDateTime(instant)) }
            event.sourceIp?.ifEmpty { null }?.let { address -> TeaNodeRow(headline = stringResource(R.string.manage_audit_from), supporting = address) }
            event.instance?.ifEmpty { null }?.let { instance -> TeaNodeRow(headline = stringResource(R.string.manage_audit_instance), supporting = instance) }
            TeaNodeSectionHeader(auditEventSummary(event))
            TeaNodeRow(headline = stringResource(R.string.manage_audit_kind), supporting = event.resourceType)
            event.resourceLabel?.ifEmpty { null }?.let { label -> TeaNodeRow(headline = stringResource(R.string.manage_audit_name), supporting = label) }
            SelectionContainer { TeaNodeRow(headline = stringResource(R.string.manage_audit_id), supporting = event.resourceId) }
            TeaNodeSectionHeader(stringResource(R.string.manage_audit_what_changed))
            val fields = event.changedFields
            if (fields.isEmpty()) {
                FooterText(stringResource(R.string.settings_audit_nothing_differs))
            } else {
                Column(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 4.dp)) {
                    Row {
                        listOf(R.string.manage_audit_field, R.string.manage_audit_before, R.string.manage_audit_after).forEach { header ->
                            Text(stringResource(header), Modifier.width(if (header == R.string.manage_audit_field) 140.dp else 220.dp).padding(end = 12.dp), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    HorizontalDivider(Modifier.padding(vertical = 4.dp))
                    fields.forEach { field ->
                        Row(Modifier.padding(vertical = 4.dp)) {
                            Text(field, Modifier.width(140.dp).padding(end = 12.dp), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.Medium)
                            AuditValue(AuditEvent.describe(event.beforeValue(field)), unset)
                            AuditValue(AuditEvent.describe(event.afterValue(field)), unset)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun AuditValue(text: String?, unset: String) {
    SelectionContainer {
        Text(
            text ?: unset, Modifier.width(220.dp).padding(end = 12.dp), style = MaterialTheme.typography.bodyMedium,
            color = if (text == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** What changed in a release, as the release says it. */
@Composable
fun ReleaseNotesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val upgrade = rememberLoad { connection.serverUpgrade() }
    val version = upgrade.value?.latest.orEmpty()
    TeaNodeScreen(title = if (version.isEmpty()) stringResource(R.string.settings_server_upgrade) else stringResource(R.string.manage_release_notes_title, version), onBack = onBack) { contentPadding ->
        LoadedContent(upgrade, Modifier.padding(contentPadding)) { found ->
            Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(16.dp)) { com.teanode.app.knowledge.MarkdownText(found.notes.orEmpty()) }
        }
    }
}
