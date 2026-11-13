package com.teanode.app.calendar

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.CalendarShare
import com.teanode.kit.calendar.CalendarSharePerson
import com.teanode.kit.calendar.CalendarShareCircle
import com.teanode.kit.calendar.CalendarShareLevel
import com.teanode.kit.calendar.ShareTarget
import com.teanode.kit.calendar.calendarSharePeople
import com.teanode.kit.calendar.calendarShareCircles
import com.teanode.kit.calendar.calendarShares
import com.teanode.kit.calendar.shareCalendar
import com.teanode.kit.calendar.unshareCalendar
import kotlinx.coroutines.launch

/** Who the person's calendar, or a page, is shared with, read once: the shares so far, and the people and circles it could be shared with. */
private data class ShareSheet(val shares: List<CalendarShare>, val people: List<CalendarSharePerson>, val circles: List<CalendarShareCircle>)

/** What the choice is called in a sentence, such as "Shared with Ben". */
@Composable
fun shareTargetName(target: ShareTarget?, people: List<CalendarSharePerson>, circles: List<CalendarShareCircle>): String = when (target) {
    is ShareTarget.Person -> people.firstOrNull { person -> person.userId == target.userId }?.displayName.orEmpty()
    is ShareTarget.Circle -> stringResource(R.string.calendar_everyone_in, circles.firstOrNull { circle -> circle.id == target.circleId }?.name.orEmpty())
    null -> ""
}

/**
 * ShareTargetPicker is the one picker for who a calendar or a page is shared with: the people in one section and the
 * circles in another, so that a person and a circle are never both chosen (ios CalendarSharingViews.swift).
 */
@Composable
fun ShareTargetPicker(people: List<CalendarSharePerson>, circles: List<CalendarShareCircle>, chosen: ShareTarget?, onChoose: (ShareTarget?) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    val chosenName = shareTargetName(chosen, people, circles).ifEmpty { stringResource(R.string.calendar_choose_person_or_circle) }
    androidx.compose.foundation.layout.Box {
        Row(Modifier.fillMaxWidth().clickable { isOpen = true }.padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.calendar_share_with), modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
            Text(chosenName, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodyLarge)
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            DropdownMenuItem(text = { Text(stringResource(R.string.calendar_choose_person_or_circle)) }, onClick = { isOpen = false; onChoose(null) })
            if (people.isNotEmpty()) {
                Text(stringResource(R.string.calendar_people), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(horizontal = 12.dp, vertical = 4.dp))
                for (person in people) DropdownMenuItem(text = { Text(person.pickerLabel) }, onClick = { isOpen = false; onChoose(ShareTarget.Person(person.userId)) })
            }
            if (circles.isNotEmpty()) {
                Text(stringResource(R.string.calendar_circles), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(horizontal = 12.dp, vertical = 4.dp))
                for (circle in circles) {
                    // The size too, so two circles with one name can be told apart.
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.calendar_everyone_in_size, circle.name, circleMemberCountText(circle.memberCount))) },
                        onClick = { isOpen = false; onChoose(ShareTarget.Circle(circle.id)) },
                    )
                }
            }
        }
    }
}

/** How many people a circle is: "4 people". */
@Composable
fun circleMemberCountText(memberCount: Int): String =
    if (memberCount == 1) stringResource(R.string.calendar_one_person) else stringResource(R.string.calendar_people_count, memberCount)

/**
 * CalendarShareScreen is who the person's calendar is shared with, at what level, with a way to stop, and a form to
 * share it with someone else: one person in a circle they share, or a whole circle (ios CalendarSharingViews.swift,
 * CalendarShareScreen).
 */
@Composable
fun CalendarShareScreen(model: CalendarModel, calendar: AgentCalendar) {
    val navigator = LocalAppNavigator.current
    val connection = model.connection
    val scope = rememberCoroutineScope()
    val sheet = rememberLoad(calendar.id) {
        ShareSheet(connection.calendarShares(calendar.id), connection.calendarSharePeople(), connection.calendarShareCircles())
    }
    var chosen by remember { mutableStateOf<ShareTarget?>(null) }
    var level by remember { mutableStateOf(CalendarShareLevel.Titles) }
    var stopping by remember { mutableStateOf<CalendarShare?>(null) }
    var isWorking by remember { mutableStateOf(false) }
    val failedText = stringResource(R.string.calendar_did_not_work)

    fun nameOf(share: CalendarShare, everyone: String): String = if (share.isWithCircle) everyone else share.granteeDisplayName

    TeaNodeScreen(title = stringResource(R.string.calendar_share_my_calendar), onBack = { navigator.back() }) { contentPadding ->
        LoadedContent(sheet, modifier = Modifier.fillMaxSize().padding(contentPadding)) { loaded ->
            val shares = loaded.shares
            LazyColumn(Modifier.fillMaxSize()) {
                item { TeaNodeSectionHeader(stringResource(R.string.calendar_shared_with)) }
                if (shares.isEmpty()) {
                    item { Text(stringResource(R.string.calendar_not_shared_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                items(shares, key = { share -> share.id }) { share ->
                    val name = nameOf(share, stringResource(R.string.calendar_everyone_in, share.granteeDisplayName))
                    val changedWords = stringResource(R.string.calendar_changed_what_sees, name)
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(name, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        var isMenuOpen by remember { mutableStateOf(false) }
                        androidx.compose.foundation.layout.Box {
                            TextButton(onClick = { isMenuOpen = true }, enabled = !isWorking) { Text((share.level ?: CalendarShareLevel.Busy).title()) }
                            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                                for (each in CalendarShareLevel.grantable) {
                                    DropdownMenuItem(text = { Text(each.title()) }, onClick = {
                                        isMenuOpen = false
                                        if (each != share.level) {
                                            isWorking = true
                                            scope.launch {
                                                try {
                                                    connection.shareCalendar(calendar.id, share.granteeUserId, share.granteeCircleId, each)
                                                    model.snackbar?.show(changedWords)
                                                } catch (failure: kotlinx.coroutines.CancellationException) {
                                                    throw failure
                                                } catch (failure: Throwable) {
                                                    model.snackbar?.showProblem(model.describeError(failure).ifEmpty { failedText })
                                                }
                                                // Read again either way, so that the menu shows what the server holds.
                                                sheet.refresh()
                                                isWorking = false
                                            }
                                        }
                                    })
                                }
                            }
                        }
                        TextButton(onClick = { stopping = share }, enabled = !isWorking) {
                            androidx.compose.material3.Icon(Icons.Outlined.PersonRemove, contentDescription = stringResource(R.string.calendar_stop_sharing))
                        }
                    }
                }
                if (loaded.people.isEmpty() && loaded.circles.isEmpty()) {
                    item {
                        Text(stringResource(R.string.calendar_no_circle_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                } else {
                    item { ShareTargetPicker(loaded.people, loaded.circles, chosen) { target -> chosen = target } }
                    item { Text(stringResource(R.string.calendar_choose_person_or_whole_circle), modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    item { TeaNodeSectionHeader(stringResource(R.string.calendar_how_much)) }
                    items(CalendarShareLevel.grantable, key = { each -> "level-${each.wireName}" }) { each ->
                        Row(Modifier.fillMaxWidth().clickable { level = each }.padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(selected = level == each, onClick = { level = each })
                            Text(each.title(), style = MaterialTheme.typography.bodyLarge)
                        }
                    }
                    item { Text(level.explanation(), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    item {
                        val sharedWords = stringResource(R.string.calendar_shared_with_name, shareTargetName(chosen, loaded.people, loaded.circles))
                        Button(
                            enabled = !isWorking && chosen != null,
                            modifier = Modifier.padding(16.dp),
                            onClick = {
                                val target = chosen ?: return@Button
                                isWorking = true
                                scope.launch {
                                    try {
                                        connection.shareCalendar(calendar.id, target.granteeUserId, target.granteeCircleId, level)
                                        model.snackbar?.show(sharedWords)
                                        chosen = null
                                        level = CalendarShareLevel.Titles
                                        sheet.refresh()
                                    } catch (failure: kotlinx.coroutines.CancellationException) {
                                        throw failure
                                    } catch (failure: Throwable) {
                                        model.snackbar?.showProblem(model.describeError(failure).ifEmpty { failedText })
                                    }
                                    isWorking = false
                                }
                            },
                        ) { Text(stringResource(R.string.calendar_share)) }
                    }
                }
            }
        }
    }
    stopping?.let { share ->
        val name = nameOf(share, stringResource(R.string.calendar_everyone_in, share.granteeDisplayName))
        val stoppedWords = stringResource(R.string.calendar_stopped_sharing_with, name)
        ConfirmationDialog(
            title = stringResource(R.string.calendar_stop_sharing),
            text = stringResource(R.string.calendar_will_no_longer_see, name),
            confirmLabel = stringResource(R.string.calendar_stop_sharing),
            isDestructive = true,
            onConfirm = {
                stopping = null
                isWorking = true
                scope.launch {
                    try {
                        connection.unshareCalendar(share.id)
                        model.snackbar?.show(stoppedWords)
                        sheet.refresh()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        model.snackbar?.showProblem(model.describeError(failure).ifEmpty { failedText })
                    }
                    isWorking = false
                }
            },
            onDismiss = { stopping = null },
        )
    }
}

/**
 * CircleCalendarOfferDialog is the question asked once, as a person joins a family or a group of colleagues: share their
 * calendar with everyone in it, at a level they choose, or not now (ios CalendarSharingViews.swift,
 * CircleCalendarOfferSheet). The circles' screens show it with the circle's name and its calendar.
 */
@Composable
fun CircleCalendarOfferDialog(circleName: String, onShare: (CalendarShareLevel) -> Unit, onNotNow: () -> Unit) {
    var level by remember { mutableStateOf(CalendarShareLevel.Busy) }
    AlertDialog(
        onDismissRequest = onNotNow,
        title = { Text(stringResource(R.string.calendar_share_your_calendar_with, circleName)) },
        text = {
            Column {
                Text(stringResource(R.string.calendar_offer_message, circleName))
                for (each in CalendarShareLevel.grantable.filter { each -> each != CalendarShareLevel.Edit }) {
                    Row(Modifier.fillMaxWidth().clickable { level = each }, verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(selected = level == each, onClick = { level = each })
                        Text(each.title())
                    }
                }
                Text(level.explanation(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = { TextButton(onClick = { onShare(level) }) { Text(stringResource(R.string.calendar_share)) } },
        dismissButton = { TextButton(onClick = onNotNow) { Text(stringResource(R.string.calendar_not_now)) } },
    )
}
