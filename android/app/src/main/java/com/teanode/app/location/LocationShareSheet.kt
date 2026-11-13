package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.ShareCircle
import com.teanode.kit.location.SharePerson
import com.teanode.kit.location.ShareTarget
import com.teanode.kit.location.shareCircles
import com.teanode.kit.location.sharePeople
import kotlinx.coroutines.launch

/** The people and circles something can be shared with, read from the server. */
class ShareCandidates(val people: List<SharePerson>, val circles: List<ShareCircle>) {
    val isEmpty: Boolean get() = people.isEmpty() && circles.isEmpty()
}

suspend fun readShareCandidates(connection: Connection): ShareCandidates {
    val people = connection.sharePeople()
    val circles = connection.shareCircles()
    return ShareCandidates(people, circles)
}

/** What a choice is called in a sentence, such as "Shared with Ben". */
fun shareTargetName(target: ShareTarget?, candidates: ShareCandidates, everyoneIn: (String) -> String): String = when (target) {
    is ShareTarget.Person -> candidates.people.firstOrNull { person -> person.userId == target.userId }?.displayName.orEmpty()
    is ShareTarget.Circle -> everyoneIn(candidates.circles.firstOrNull { circle -> circle.id == target.circleId }?.name.orEmpty())
    null -> ""
}

/** A choice of a person, or a whole circle, to share with. */
@Composable
fun ShareTargetPicker(candidates: ShareCandidates, chosen: ShareTarget?, onChoose: (ShareTarget) -> Unit, modifier: Modifier = Modifier) {
    var isOpen by remember { mutableStateOf(false) }
    val chosenText = when (chosen) {
        is ShareTarget.Person -> candidates.people.firstOrNull { person -> person.userId == chosen.userId }?.pickerLabel
        is ShareTarget.Circle -> candidates.circles.firstOrNull { circle -> circle.id == chosen.circleId }?.let { circle -> stringResource(R.string.location_everyone_in_with_count, circle.name, pluralStringResource(R.plurals.location_member_count, circle.memberCount, circle.memberCount)) }
        null -> null
    }
    Box(modifier) {
        OutlinedButton(onClick = { isOpen = true }, modifier = Modifier.fillMaxWidth()) {
            Text(chosenText ?: stringResource(R.string.location_choose_person_or_circle), modifier = Modifier.weight(1f), maxLines = 1)
            Icon(Icons.Outlined.ExpandMore, contentDescription = null)
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            if (candidates.people.isNotEmpty()) {
                Text(stringResource(R.string.location_people), Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                candidates.people.forEach { person -> DropdownMenuItem(text = { Text(person.pickerLabel) }, onClick = { isOpen = false; onChoose(ShareTarget.Person(person.userId)) }) }
            }
            if (candidates.circles.isNotEmpty()) {
                Text(stringResource(R.string.location_circles), Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                candidates.circles.forEach { circle ->
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.location_everyone_in_with_count, circle.name, pluralStringResource(R.plurals.location_member_count, circle.memberCount, circle.memberCount))) },
                        onClick = { isOpen = false; onChoose(ShareTarget.Circle(circle.id)) },
                    )
                }
            }
        }
    }
}

/** Shares where the person is with a person or a circle, at the level they choose. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LocationShareSheet(model: LocationModel, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    var candidates by remember { mutableStateOf<ShareCandidates?>(null) }
    var loadProblem by remember { mutableStateOf<String?>(null) }
    var chosen by remember { mutableStateOf<ShareTarget?>(null) }
    var level by remember { mutableStateOf(LocationShareLevel.Current) }
    var isWorking by remember { mutableStateOf(false) }
    suspend fun load() {
        loadProblem = null
        try {
            candidates = readShareCandidates(connection)
        } catch (error: Exception) {
            if (error is kotlinx.coroutines.CancellationException) throw error
            loadProblem = session.describeLoadProblem(error)
        }
    }
    LaunchedEffect(Unit) { load() }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 24.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.location_share_where_i_am), style = MaterialTheme.typography.titleLarge)
            val ready = candidates
            when {
                loadProblem != null && ready == null -> ErrorState(loadProblem.orEmpty(), onRetry = { scope.launch { load() } })
                ready == null -> Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                ready.isEmpty -> Text(stringResource(R.string.location_no_circle_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
                else -> {
                    ShareTargetPicker(ready, chosen, onChoose = { chosen = it })
                    Text(stringResource(R.string.location_choose_person_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    HorizontalDivider()
                    Text(stringResource(R.string.location_how_much), style = MaterialTheme.typography.titleSmall)
                    LocationShareLevel.grantable.forEach { each ->
                        Row(Modifier.fillMaxWidth().selectable(selected = each == level, onClick = { level = each }), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(selected = each == level, onClick = { level = each })
                            Text(text.levelTitle(each))
                        }
                    }
                    Text(text.levelExplanation(level), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        TextButton(onClick = onDismiss, enabled = !isWorking) { Text(stringResource(R.string.common_cancel)) }
                        Button(
                            enabled = chosen != null && !isWorking,
                            onClick = {
                                val target = chosen ?: return@Button
                                isWorking = true
                                scope.launch {
                                    val name = shareTargetName(target, ready, text::everyoneIn)
                                    val refusal = model.share(target, name, level)
                                    isWorking = false
                                    if (refusal == null) onDismiss() else snackbar.showProblem(refusal)
                                }
                            },
                        ) { Text(stringResource(R.string.location_share)) }
                    }
                }
            }
        }
    }
}
