package com.teanode.app.chats

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.HelpOutline
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import com.teanode.app.settings.SkillShareCallCard
import com.teanode.kit.chats.SkillShareNote
import com.teanode.kit.skillsharing.SkillShareCallReading
import com.teanode.kit.skillsharing.readSkillShareCall
import kotlinx.coroutines.delay

/** How often a call that waits, or could not be read, is read again. */
private const val SKILL_SHARE_CALL_POLL_MS = 10_000L

/**
 * SkillShareCallWaitingCard is the skill_share_call_waiting note of the main conversation as a card (ios
 * SkillShareCallWaitingCard): the note names the call, the call is read for how it stands now (it may have been answered
 * on the dashboard since), and the card is drawn from that, with approve and decline in place. It is read again every few
 * seconds while the call waits, so an answer given elsewhere shows here. Only the server answering that the call is not
 * there says it is gone; a read that did not get through says so, offers to try again, and keeps trying.
 */
@Composable
fun SkillShareCallWaitingCard(noteDetail: String, initialReading: SkillShareCallReading? = null) {
    val connection = LocalConnection.current
    var reading by remember(noteDetail) { mutableStateOf(initialReading) }
    var retryCount by remember(noteDetail) { mutableStateOf(0) }
    LaunchedEffect(noteDetail, retryCount) {
        val note = SkillShareNote.parsing(noteDetail)
        if (note == null) {
            reading = SkillShareCallReading.Gone
            return@LaunchedEffect
        }
        while (true) {
            val latest = connection.readSkillShareCall(note.skillShareId, note.callId)
            // A read that fails after the card was drawn leaves the card as it was.
            if (!(latest is SkillShareCallReading.Failed && reading is SkillShareCallReading.Found)) reading = latest
            val shown = reading
            val isStillWorthReading = (shown is SkillShareCallReading.Found && shown.call.isWaiting) || shown is SkillShareCallReading.Failed
            if (!isStillWorthReading) return@LaunchedEffect
            delay(SKILL_SHARE_CALL_POLL_MS)
        }
    }
    CardSurface(paddingDp = 0) {
        when (val shown = reading) {
            is SkillShareCallReading.Found -> SkillShareCallCard(shown.call, isOwner = true, onAnswered = { answered -> if (answered != null) reading = SkillShareCallReading.Found(answered) else retryCount += 1 }, hasDivider = false)
            SkillShareCallReading.Gone -> Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.HelpOutline, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(stringResource(R.string.chats_skill_share_call_gone), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            SkillShareCallReading.Failed -> Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Icon(Icons.Outlined.WifiOff, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(stringResource(R.string.chats_skill_share_call_failed), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                OutlinedButton(onClick = { retryCount += 1 }) { Text(stringResource(R.string.chats_try_again)) }
            }
            null -> Row(Modifier.padding(16.dp)) { CircularProgressIndicator(Modifier.padding(4.dp)) }
        }
    }
}
