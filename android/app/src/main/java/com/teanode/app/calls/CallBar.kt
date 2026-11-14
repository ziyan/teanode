package com.teanode.app.calls

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CallEnd
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material.icons.filled.Phone
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R

/**
 * CallBar is a call put away while the phone is on it: a bar at the top of the app, over whatever the person went to
 * (a mail, a photo, the chat), with mute and hang up. Tapping it brings the call screen back.
 */
@Composable
fun CallBar(state: CallSessionState, actions: CallActions, modifier: Modifier = Modifier) {
    val returnDescription = stringResource(R.string.calls_return_to_call)
    Surface(
        modifier.fillMaxWidth().statusBarsPadding(),
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
        tonalElevation = 2.dp,
    ) {
        Row(Modifier.padding(start = 16.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(
                Modifier.weight(1f).heightIn(min = 56.dp).clickable { actions.onMinimize() }.semantics(mergeDescendants = true) { contentDescription = returnDescription + ": " + state.title },
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Icon(
                    if (state.isCameraOn) Icons.Filled.Videocam else Icons.Filled.Phone, contentDescription = null,
                    modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.primary,
                )
                Column {
                    Text(state.title, style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    CallStatusLine(state, MaterialTheme.typography.bodySmall)
                }
            }
            IconButton(onClick = { actions.onMuteChange(!state.isMuted) }) {
                Icon(
                    if (state.isMuted) Icons.Filled.MicOff else Icons.Filled.Mic,
                    contentDescription = stringResource(if (state.isMuted) R.string.calls_unmute else R.string.calls_mute),
                )
            }
            IconButton(onClick = actions.onHangUp) {
                Icon(Icons.Filled.CallEnd, contentDescription = stringResource(R.string.calls_hang_up), tint = MaterialTheme.colorScheme.error)
            }
        }
    }
}
