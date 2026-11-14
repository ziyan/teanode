package com.teanode.app.knowledge

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.clickable
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.kit.knowledge.KnowledgeRecordKinds
import com.teanode.kit.knowledge.KnowledgeRecordLink
import com.teanode.kit.knowledge.recordMemoryLinks

/**
 * InMemoryCard is the pages of the agent's memory that link to one record, read when the record's screen opens (ios
 * InMemoryViews.swift). A screen that has one says so in a heading and the pages under it; one whose record no page is
 * linked to says nothing, and a read that fails leaves the screen as it was. Any part may draw it for its record:
 * `InMemoryCard("calendar_event", eventId)`.
 */
@Composable
fun InMemoryCard(recordKind: String, recordId: String, modifier: Modifier = Modifier, passageStartMS: Long? = null, passageEndMS: Long? = null) {
    val connection = LocalConnection.current
    var links by remember(recordKind, recordId) { mutableStateOf<List<KnowledgeRecordLink>>(emptyList()) }
    LaunchedEffect(recordKind, recordId, passageStartMS, passageEndMS) {
        if (recordId.isEmpty()) return@LaunchedEffect
        links = try {
            connection.recordMemoryLinks(recordKind, recordId, passageStartMS, passageEndMS)
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (_: Exception) {
            emptyList()
        }
    }
    val pages = KnowledgeRecordKinds.pages(links)
    if (pages.isEmpty()) return
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.knowledge_in_memory), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
        for (link in pages) InMemoryRow(link)
    }
}

/** One page of memory a record is linked to: its name, what it is to the record, and a guess marked as one. A tap opens the page. */
@Composable
fun InMemoryRow(link: KnowledgeRecordLink) {
    val navigator = LocalAppNavigator.current
    Row(
        Modifier.fillMaxWidth().clickable { navigator.navigate(KnowledgeRoutes.page(link.nodePath)) }.padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(link.nodeName.ifEmpty { link.nodePath }, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
        if (link.isProposed) {
            Text(stringResource(R.string.knowledge_a_guess), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Text(KnowledgeRecordWords.relation(link.recordRelation), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** KnowledgeRecordWords says in a word or two what a page is to a record and what a kind of record is called. */
object KnowledgeRecordWords {
    @Composable
    fun kind(recordKind: String): String = when (recordKind) {
        "contact" -> stringResource(R.string.knowledge_kind_contact)
        "mail" -> stringResource(R.string.knowledge_kind_mail)
        "calendar_event" -> stringResource(R.string.knowledge_kind_calendar_event)
        "reminder" -> stringResource(R.string.knowledge_kind_reminder)
        "conversation_message" -> stringResource(R.string.knowledge_kind_conversation)
        "finance_transaction" -> stringResource(R.string.knowledge_kind_transaction)
        "finance_receipt" -> stringResource(R.string.knowledge_kind_receipt)
        "recording_passage" -> stringResource(R.string.knowledge_kind_recording)
        else -> recordKind
    }

    @Composable
    fun relation(recordRelation: String): String = when (recordRelation) {
        "is_contact" -> stringResource(R.string.knowledge_kind_contact)
        "wrote" -> stringResource(R.string.knowledge_relation_wrote)
        "received" -> stringResource(R.string.knowledge_relation_received)
        "attended" -> stringResource(R.string.knowledge_relation_attended)
        "organized" -> stringResource(R.string.knowledge_relation_organized)
        "spoke_in" -> stringResource(R.string.knowledge_relation_spoke_in)
        "mentioned_in" -> stringResource(R.string.knowledge_relation_mentioned_in)
        "paid" -> stringResource(R.string.knowledge_relation_paid)
        "paid_by" -> stringResource(R.string.knowledge_relation_paid_by)
        "receipt_for" -> stringResource(R.string.knowledge_relation_receipt_for)
        "about" -> stringResource(R.string.knowledge_relation_about)
        else -> recordRelation.replace('_', ' ')
    }
}
