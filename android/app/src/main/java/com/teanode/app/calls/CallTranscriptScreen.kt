package com.teanode.app.calls

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.calls.ChatCallDetailView
import com.teanode.kit.calls.ChatCallTranscriptSegment
import com.teanode.kit.calls.readChatCall

/** ChatCallTranscriptScreen lists what was said on a call that was transcribed, each line under its speaker. */
@Composable
fun ChatCallTranscriptScreen(
    callId: String,
    title: String,
    onBack: () -> Unit,
    onOpenRecording: (detail: ChatCallDetailView) -> Unit,
) {
    val connection = LocalConnection.current
    val detail = rememberLoad(callId) { connection.readChatCall(callId) }
    TeaNodeScreen(title = title, onBack = onBack) { contentPadding ->
        LoadedContent(
            detail,
            modifier = Modifier.padding(contentPadding),
            isEmpty = { read -> read.transcript.isEmpty() },
            empty = {
                EmptyState(
                    icon = Icons.Outlined.ChatBubbleOutline,
                    title = stringResource(R.string.calls_transcript_none),
                    description = stringResource(R.string.calls_transcript_none_description),
                )
            },
        ) { read -> ChatCallTranscriptList(read, onOpenRecording, Modifier.padding(contentPadding)) }
    }
}

/** The lines of a transcript, and the way to open the recording the call became. */
@Composable
fun ChatCallTranscriptList(detail: ChatCallDetailView, onOpenRecording: (ChatCallDetailView) -> Unit, modifier: Modifier = Modifier) {
    LazyColumn(modifier.fillMaxSize()) {
        // The call is a recording, which plays it and keeps the corrections of its words.
        if (detail.recordingId != null) {
            item {
                Button(onClick = { onOpenRecording(detail) }, modifier = Modifier.padding(16.dp)) {
                    androidx.compose.material3.Icon(Icons.Outlined.GraphicEq, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
                    Text(stringResource(R.string.calls_open_recording))
                }
            }
        }
        items(detail.transcript, key = { segment -> segment.id }) { segment ->
            TranscriptSegmentRow(segment)
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        }
    }
}

@Composable
private fun TranscriptSegmentRow(segment: ChatCallTranscriptSegment) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(segment.speakerName, style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold))
            Text(segment.startText, style = MaterialTheme.typography.labelSmall.copy(fontFamily = FontFamily.Monospace), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Text(segment.text, style = MaterialTheme.typography.bodyLarge)
    }
}
