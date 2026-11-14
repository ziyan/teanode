package com.teanode.app.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material.icons.outlined.Business
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Receipt
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.goals.ChatHandoff
import com.teanode.app.shell.AppNavigator
import com.teanode.kit.AppSection
import com.teanode.kit.knowledge.RecordTarget

/** The symbol that marks a kind of page (ios KnowledgeSymbols). */
object KnowledgeSymbols {
    fun icon(kind: String): ImageVector = when (kind) {
        "folder" -> Icons.Outlined.Folder
        "self", "person" -> Icons.Outlined.Person
        "organization" -> Icons.Outlined.Business
        "project" -> Icons.Outlined.Build
        "place" -> Icons.Outlined.Place
        "period" -> Icons.Outlined.CalendarMonth
        "thing" -> Icons.Outlined.Category
        else -> Icons.Outlined.Description
    }

    /** The symbol that marks a kind of record. */
    fun recordIcon(recordKind: String): ImageVector = when (recordKind) {
        "contact" -> Icons.Outlined.AccountCircle
        "mail" -> Icons.Outlined.Email
        "calendar_event" -> Icons.Outlined.CalendarMonth
        "reminder" -> Icons.Outlined.Checklist
        "conversation_message" -> Icons.Outlined.ChatBubbleOutline
        "finance_transaction" -> Icons.Outlined.CreditCard
        "finance_receipt" -> Icons.Outlined.Receipt
        "recording_passage" -> Icons.Outlined.GraphicEq
        else -> Icons.Outlined.Description
    }
}

/** A small rounded tag: a kind, a mark such as "A guess", or a warning when [isAlert]. */
@Composable
fun Chip(text: String, isAlert: Boolean = false, modifier: Modifier = Modifier) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = if (isAlert) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.onSecondaryContainer,
        modifier = modifier
            .background(if (isAlert) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.secondaryContainer, RoundedCornerShape(50))
            .padding(horizontal = 8.dp, vertical = 2.dp),
    )
}

/** Who made a change and where a fact came from, in words (ios KnowledgeWordsShown). */
object KnowledgeWordsShown {
    @Composable
    fun actor(actor: String): String = when (actor) {
        "person" -> stringResource(R.string.knowledge_actor_you)
        "agent" -> stringResource(R.string.knowledge_actor_agent)
        "remember" -> stringResource(R.string.knowledge_actor_remember)
        "dream" -> stringResource(R.string.knowledge_actor_dream)
        "ingest" -> stringResource(R.string.knowledge_actor_ingest)
        else -> actor
    }

    @Composable
    fun source(kind: String): String = when (kind) {
        "conversation", "conversation_message" -> stringResource(R.string.knowledge_source_conversation)
        "mail" -> stringResource(R.string.knowledge_source_mail)
        "document" -> stringResource(R.string.knowledge_source_document)
        "commit" -> stringResource(R.string.knowledge_source_commit)
        "chat" -> stringResource(R.string.knowledge_source_chat)
        "contact" -> stringResource(R.string.knowledge_source_contact)
        "repository" -> stringResource(R.string.knowledge_source_repository)
        "calendar_event" -> stringResource(R.string.knowledge_source_calendar_event)
        "reminder" -> stringResource(R.string.knowledge_source_reminder)
        "finance_transaction" -> stringResource(R.string.knowledge_source_transaction)
        "finance_receipt" -> stringResource(R.string.knowledge_source_receipt)
        "recording_passage" -> stringResource(R.string.knowledge_source_recording)
        "memory" -> stringResource(R.string.knowledge_source_memory)
        "person" -> stringResource(R.string.knowledge_source_person)
        "dream" -> stringResource(R.string.knowledge_source_dream)
        else -> kind
    }
}

/**
 * Opens a record of the person's that a page of memory points to. The kind decides the part of the app: conversations
 * open in Chats, and every other kind opens the part that holds it (Mail, Contacts, Calendar, Finance, Recordings),
 * since each part registers its own places. A path the app has no screen for does nothing.
 */
fun openRecord(navigator: AppNavigator, dashboardPath: String) {
    when (val target = RecordTarget.fromDashboardPath(dashboardPath)) {
        is RecordTarget.Mail -> navigator.openSection(AppSection.Mail)
        is RecordTarget.Contact -> navigator.openSection(AppSection.Contacts)
        is RecordTarget.CalendarEvent, is RecordTarget.Reminder -> navigator.openSection(AppSection.Calendar)
        is RecordTarget.Conversation -> ChatHandoff.open(navigator, target.id)
        is RecordTarget.FinanceTransaction, is RecordTarget.FinanceReceipt -> navigator.openSection(AppSection.Finance)
        is RecordTarget.Recording -> navigator.openSection(AppSection.Recordings)
        null -> Unit
    }
}

/**
 * The few things of markdown a summary or an overview uses, drawn as text: paragraphs, **bold**, *italic* and `code`,
 * and "- " lists with a bullet. The agent's other markup is left as it was written.
 */
fun markdownText(text: String, codeColor: androidx.compose.ui.graphics.Color): AnnotatedString = buildAnnotatedString {
    val lines = text.trim().lines()
    lines.forEachIndexed { index, line ->
        val bullet = line.trimStart().let { trimmed -> if (trimmed.startsWith("- ") || trimmed.startsWith("* ")) trimmed.drop(2) else null }
        append(if (bullet != null) "• " else "")
        appendInline(bullet ?: line, codeColor)
        if (index < lines.size - 1) append("\n")
    }
}

private fun AnnotatedString.Builder.appendInline(text: String, codeColor: androidx.compose.ui.graphics.Color) {
    var position = 0
    val pattern = Regex("\\*\\*(.+?)\\*\\*|\\*(.+?)\\*|_(.+?)_|`(.+?)`")
    for (match in pattern.findAll(text)) {
        append(text.substring(position, match.range.first))
        val bold = match.groups[1]?.value
        val italic = match.groups[2]?.value ?: match.groups[3]?.value
        val code = match.groups[4]?.value
        when {
            bold != null -> withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(bold) }
            italic != null -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(italic) }
            code != null -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace, color = codeColor)) { append(code) }
        }
        position = match.range.last + 1
    }
    append(text.substring(position))
}

@Composable
fun MarkdownText(text: String, modifier: Modifier = Modifier) {
    Text(markdownText(text, MaterialTheme.colorScheme.onSurface), style = MaterialTheme.typography.bodyMedium, modifier = modifier)
}
