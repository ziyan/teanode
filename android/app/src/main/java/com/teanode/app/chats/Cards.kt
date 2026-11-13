package com.teanode.app.chats

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckBox
import androidx.compose.material.icons.outlined.CheckBoxOutlineBlank
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.HelpOutline
import androidx.compose.material.icons.outlined.PanTool
import androidx.compose.material.icons.outlined.RadioButtonChecked
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.teanode.app.R
import com.teanode.kit.chats.TranscriptLine

/** What a card is, small and quiet above what it says. */
@Composable
fun CardHeader(icon: ImageVector, title: String, modifier: Modifier = Modifier, maxLines: Int = 1) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(title, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = maxLines, overflow = TextOverflow.Ellipsis)
    }
}

/** One answer a card offers: a row with a radio or a check, as a list row is, rather than a button of its own. */
@Composable
fun ChoiceRow(title: String, detail: String, isChosen: Boolean, isMultiple: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable(onClick = onClick).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            when {
                isMultiple && isChosen -> Icons.Outlined.CheckBox
                isMultiple -> Icons.Outlined.CheckBoxOutlineBlank
                isChosen -> Icons.Outlined.RadioButtonChecked
                else -> Icons.Outlined.RadioButtonUnchecked
            },
            contentDescription = null, tint = if (isChosen) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline,
        )
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** CardView is something waiting for the person: a tool call to approve, or the agent's question. */
@Composable
fun CardView(line: TranscriptLine, model: ConversationModel, agentTitle: String) {
    // The answer picked, sent only with Send, as the dashboard's card asks: one stray tap sends nothing.
    var chosen by rememberSaveable(line.id) { mutableStateOf<String?>(null) }
    val isDestructive = line.risk == "destructive"
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (line.kind == TranscriptLine.Kind.Approval) {
            CardHeader(if (isDestructive) Icons.Outlined.Warning else Icons.Outlined.PanTool, if (line.detail.isEmpty()) stringResource(R.string.chats_approve) else toolWords(line.detail))
        } else {
            CardHeader(Icons.Outlined.HelpOutline, stringResource(R.string.chats_agent_asks, agentTitle))
        }
        MarkdownView(line.text, conversationId = model.conversationId)
        if (line.isDone) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.CheckCircle, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(
                    when (line.resolution) {
                        "approved" -> stringResource(R.string.chats_approved)
                        "declined" -> stringResource(R.string.chats_declined)
                        else -> line.resolution
                    },
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else if (line.kind == TranscriptLine.Kind.Approval) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Button(
                    onClick = { model.approve(line, true) }, modifier = Modifier.weight(1f).heightIn(min = 48.dp),
                    colors = if (isDestructive) ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError) else ButtonDefaults.buttonColors(),
                ) { Text(stringResource(R.string.chats_do_it), maxLines = 2) }
                OutlinedButton(onClick = { model.approve(line, false) }, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) { Text(stringResource(R.string.chats_dont), maxLines = 2) }
            }
        } else {
            Column {
                for ((index, choice) in line.choices.withIndex()) {
                    if (index > 0) HorizontalDivider()
                    ChoiceRow(choice, "", chosen == choice, false) { chosen = if (chosen == choice) null else choice }
                }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                if (line.choices.isNotEmpty()) {
                    Button(onClick = { chosen?.let { picked -> model.answerCard(line, picked) } }, enabled = chosen != null, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) {
                        Text(stringResource(R.string.chats_send))
                    }
                }
                OutlinedButton(
                    onClick = { model.answerCard(line, ConversationModel.CHAT_ABOUT_IT) },
                    modifier = (if (line.choices.isEmpty()) Modifier else Modifier.weight(1f)).heightIn(min = 48.dp),
                ) { Text(stringResource(R.string.chats_chat_about_it), maxLines = 2) }
            }
            Text(stringResource(R.string.chats_or_type_answer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline)
        }
    }
}

/** The whole of a coding session's question as the session wrote it: a command, a script, a diff, in a fixed-width face, in a box that scrolls. */
@Composable
fun CodingQuestionDetails(text: String) {
    SelectionContainer {
        Column(
            Modifier.fillMaxWidth().heightIn(max = 280.dp).clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.onSurface.copy(alpha = 0.07f))
                .verticalScroll(rememberScrollState()).padding(10.dp),
        ) { Text(text, fontFamily = FontFamily.Monospace, fontSize = 12.sp, lineHeight = 16.sp) }
    }
}

/**
 * CodingQuestionCard is a question one of the person's coding sessions asked, with its options as buttons: one press
 * answers, or with several allowed, the chosen ones are sent together.
 */
@Composable
fun CodingQuestionCard(line: TranscriptLine, model: ConversationModel) {
    val question = line.codingQuestion ?: return
    var isShowingDetails by rememberSaveable(line.id) { mutableStateOf(false) }
    val chosen = remember(line.id) { mutableStateListOf<Int>() }
    val sessionTitle = listOf(question.codingAgentName, question.sessionName, question.computer).filter { part -> part.isNotEmpty() }.joinToString(" · ")
    if (line.isDone) {
        // An answered question is a record, so it takes two lines: the session, then what it asked with what was answered. A tap opens the rest.
        Column(Modifier.clickable { isShowingDetails = !isShowingDetails }, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.Terminal, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(sessionTitle, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Icon(if (isShowingDetails) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = stringResource(if (isShowingDetails) R.string.chats_hide_details else R.string.chats_show_details), modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.CheckCircle, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(question.headline, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Text(line.resolution.ifEmpty { stringResource(R.string.chats_answered) }, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (isShowingDetails) CodingQuestionDetails(question.shownText)
        }
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        // The session, with the details beside it rather than on a row of their own.
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            CardHeader(Icons.Outlined.Terminal, sessionTitle, Modifier.weight(1f))
            OutlinedButton(onClick = { isShowingDetails = !isShowingDetails }, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp)) {
                Text(stringResource(if (isShowingDetails) R.string.chats_hide_details else R.string.chats_details))
            }
        }
        Text(question.headline, maxLines = 3, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyLarge)
        if (isShowingDetails) CodingQuestionDetails(question.shownText)
        Column {
            for ((index, option) in question.choices.withIndex()) {
                if (index > 0) HorizontalDivider()
                ChoiceRow(option.optionLabel, option.optionDescription, option.optionNumber in chosen, question.isMultipleChoice) {
                    if (question.isMultipleChoice) {
                        if (option.optionNumber in chosen) chosen.remove(option.optionNumber) else chosen.add(option.optionNumber)
                    } else {
                        model.answerCoding(line, listOf(option), null)
                    }
                }
            }
        }
        if (question.isMultipleChoice) {
            Button(
                onClick = { model.answerCoding(line, question.choices.filter { option -> option.optionNumber in chosen }, null) },
                enabled = chosen.isNotEmpty(), modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp),
            ) { Text(stringResource(R.string.chats_send)) }
        }
        if (question.options.any { option -> option.herdrOptionKind != "choice" }) {
            Text(stringResource(R.string.chats_or_type_answer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline)
        }
    }
}
