package com.teanode.app.mail

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.mail.MailFilter
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

/** NarrowSheet is the dashboard's Narrow panel: search the whole mailbox, by who, by subject, between two days, with or without attachments. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NarrowSheet(filter: MailFilter, onDismiss: () -> Unit, onApply: (MailFilter) -> Unit) {
    var draft by remember { mutableStateOf(filter) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.mail_narrow_title), style = MaterialTheme.typography.titleLarge)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.mail_search_whole_mailbox), Modifier.weight(1f))
                Switch(checked = draft.isEverywhere, onCheckedChange = { checked -> draft = draft.copy(isEverywhere = checked) })
            }
            OutlinedTextField(draft.from, { typed -> draft = draft.copy(from = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_from)) }, singleLine = true, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Email))
            OutlinedTextField(draft.to, { typed -> draft = draft.copy(to = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_to)) }, singleLine = true, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Email))
            OutlinedTextField(draft.subject, { typed -> draft = draft.copy(subject = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_subject)) }, singleLine = true)
            OptionalDayField(stringResource(R.string.mail_since), draft.since) { chosen -> draft = draft.copy(since = chosen) }
            OptionalDayField(stringResource(R.string.mail_before), draft.before) { chosen -> draft = draft.copy(before = chosen) }
            Text(stringResource(R.string.mail_attachments), style = MaterialTheme.typography.titleSmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(draft.hasAttachment == null, { draft = draft.copy(hasAttachment = null) }, { Text(stringResource(R.string.mail_any)) })
                FilterChip(draft.hasAttachment == true, { draft = draft.copy(hasAttachment = true) }, { Text(stringResource(R.string.mail_with_attachments)) })
                FilterChip(draft.hasAttachment == false, { draft = draft.copy(hasAttachment = false) }, { Text(stringResource(R.string.mail_without_attachments)) })
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                TextButton(onClick = { draft = MailFilter(searchText = filter.searchText) }) { Text(stringResource(R.string.mail_clear), color = MaterialTheme.colorScheme.error) }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                Button(onClick = { onApply(draft) }) { Text(stringResource(R.string.mail_apply)) }
            }
            Spacer(Modifier.height(16.dp))
        }
    }
}

/** A day that may be left unset: a switch, and the day when it is set. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OptionalDayField(title: String, day: LocalDate?, onChange: (LocalDate?) -> Unit) {
    var isPicking by remember { mutableStateOf(false) }
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Text(title, Modifier.weight(1f))
        if (day != null) {
            OutlinedButton(onClick = { isPicking = true }) { Text(MailTimes.wholeDate(day.atStartOfDay().toInstant(ZoneOffset.UTC), ZoneOffset.UTC)) }
        }
        Switch(checked = day != null, onCheckedChange = { checked -> onChange(if (checked) (day ?: LocalDate.now()) else null) }, modifier = Modifier.padding(start = 8.dp))
    }
    if (isPicking && day != null) {
        val pickerState = rememberDatePickerState(initialSelectedDateMillis = day.atStartOfDay().toInstant(ZoneOffset.UTC).toEpochMilli())
        DatePickerDialog(
            onDismissRequest = { isPicking = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis -> onChange(Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate()) }
                    isPicking = false
                }) { Text(stringResource(R.string.common_done)) }
            },
            dismissButton = { TextButton(onClick = { isPicking = false }) { Text(stringResource(R.string.common_cancel)) } },
        ) { DatePicker(pickerState) }
    }
}

/** SortSheet tells the agent how to sort the chosen messages: category, priority and whether they need a reply, each left unchanged by default. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SortSheet(mail: MailModel, count: Int, onDismiss: () -> Unit, onSort: (category: String?, priority: String?, needsReply: Boolean?) -> Unit) {
    var category by remember { mutableStateOf<String?>(null) }
    var priority by remember { mutableStateOf<String?>(null) }
    var needsReply by remember { mutableStateOf<Boolean?>(null) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(pluralStringResource(R.plurals.mail_sort_messages, count, count), style = MaterialTheme.typography.titleLarge)
            Text(stringResource(R.string.mail_category), style = MaterialTheme.typography.titleSmall)
            ChipRow {
                FilterChip(category == null, { category = null }, { Text(stringResource(R.string.mail_unchanged)) })
                for (name in mail.sortCategories) FilterChip(category == name, { category = name }, { Text(name) })
            }
            Text(stringResource(R.string.mail_priority_label), style = MaterialTheme.typography.titleSmall)
            ChipRow {
                FilterChip(priority == null, { priority = null }, { Text(stringResource(R.string.mail_unchanged)) })
                FilterChip(priority == "high", { priority = "high" }, { Text(stringResource(R.string.mail_priority_high)) })
                FilterChip(priority == "normal", { priority = "normal" }, { Text(stringResource(R.string.mail_priority_normal)) })
                FilterChip(priority == "low", { priority = "low" }, { Text(stringResource(R.string.mail_priority_low)) })
            }
            Text(stringResource(R.string.mail_needs_reply), style = MaterialTheme.typography.titleSmall)
            ChipRow {
                FilterChip(needsReply == null, { needsReply = null }, { Text(stringResource(R.string.mail_unchanged)) })
                FilterChip(needsReply == true, { needsReply = true }, { Text(stringResource(R.string.mail_yes)) })
                FilterChip(needsReply == false, { needsReply = false }, { Text(stringResource(R.string.mail_no)) })
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                Button(onClick = { onSort(category, priority, needsReply) }, enabled = category != null || priority != null || needsReply != null) { Text(stringResource(R.string.mail_sort)) }
            }
            Spacer(Modifier.height(16.dp))
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun ChipRow(content: @Composable () -> Unit) {
    androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) { content() }
}
