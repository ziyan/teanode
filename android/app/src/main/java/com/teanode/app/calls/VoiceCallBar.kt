package com.teanode.app.calls

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CallEnd
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.calls.VoiceCallState

/**
 * VoiceCallBar is the composer while a call with the agent is on, as the iPhone app's `CallBar`: how loud the person is
 * (a tap on the meter cuts an answer short), what is heard or said, mute and hang up. It takes plain values so that it is
 * photographed in every state; [AgentVoiceCallBar] feeds it from the call.
 */
@Composable
fun VoiceCallBar(
    callState: VoiceCallState,
    captionText: String,
    problemText: String?,
    level: Float,
    isMuted: Boolean,
    onCutAnswer: () -> Unit,
    onMuteChange: (Boolean) -> Unit,
    onHangUp: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val isSpeaking = callState == VoiceCallState.Speaking
    val caption = problemText ?: captionText.ifEmpty { stateCaption(callState, isMuted) }
    Surface(
        modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp),
        shape = RoundedCornerShape(30.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
        tonalElevation = 2.dp,
    ) {
        Row(
            Modifier.padding(start = 16.dp, end = 8.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            val meterDescription = stringResource(R.string.calls_voice_stop_answer)
            Box(
                Modifier.heightIn(min = 48.dp).width(48.dp).clip(CircleShape)
                    .clickable(enabled = isSpeaking, role = Role.Button, onClickLabel = meterDescription, onClick = onCutAnswer),
                contentAlignment = Alignment.Center,
            ) { VoiceCallMeter(level, isSpeaking) }
            Text(
                caption,
                Modifier.weight(1f),
                style = MaterialTheme.typography.bodyMedium,
                color = if (problemText == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                maxLines = if (problemText == null) 3 else 6,
                overflow = TextOverflow.Ellipsis,
            )
            Box(
                Modifier.size(48.dp).clip(CircleShape)
                    .background(if (isMuted) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceContainerHighest)
                    .clickable(role = Role.Button) { onMuteChange(!isMuted) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (isMuted) Icons.Filled.MicOff else Icons.Filled.Mic,
                    contentDescription = stringResource(if (isMuted) R.string.calls_unmute else R.string.calls_mute),
                    tint = if (isMuted) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.primary,
                )
            }
            Box(
                Modifier.size(48.dp).clip(CircleShape).background(MaterialTheme.colorScheme.error).clickable(role = Role.Button, onClick = onHangUp),
                contentAlignment = Alignment.Center,
            ) { Icon(Icons.Filled.CallEnd, contentDescription = stringResource(R.string.calls_hang_up), tint = MaterialTheme.colorScheme.onError) }
        }
    }
}

@Composable
private fun stateCaption(callState: VoiceCallState, isMuted: Boolean): String = when (callState) {
    VoiceCallState.Starting -> stringResource(R.string.calls_voice_starting_microphone)
    VoiceCallState.Listening -> stringResource(if (isMuted) R.string.calls_muted else R.string.calls_voice_listening)
    VoiceCallState.Hearing -> stringResource(R.string.calls_voice_hearing_you)
    VoiceCallState.Working -> stringResource(R.string.calls_voice_working_on_it)
    VoiceCallState.Speaking -> stringResource(R.string.calls_voice_speaking_cut_in)
    VoiceCallState.Ended -> ""
}

/** Five bars moving with the microphone, or standing at a fixed height in the accent while an answer is spoken. */
@Composable
private fun VoiceCallMeter(level: Float, isSpeaking: Boolean) {
    val shownLevel by animateFloatAsState(if (isSpeaking) 0.6f else level.coerceIn(0f, 1f), label = "voiceCallMeter")
    Row(Modifier.height(30.dp), horizontalArrangement = Arrangement.spacedBy(3.dp), verticalAlignment = Alignment.CenterVertically) {
        for (scale in BAR_SCALES) {
            Box(
                Modifier.width(4.dp).height((6f + 22f * shownLevel * scale).dp).clip(CircleShape)
                    .background(if (isSpeaking) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f)),
            )
        }
    }
}

private val BAR_SCALES = listOf(0.5f, 0.8f, 1f, 0.8f, 0.5f)

/** The bar fed by a call with the agent. */
@Composable
fun AgentVoiceCallBar(call: AgentVoiceCall, modifier: Modifier = Modifier) {
    val callState by call.callState.collectAsState()
    val captionText by call.captionText.collectAsState()
    val problemText by call.problemText.collectAsState()
    val level by call.level.collectAsState()
    val isMuted by call.isMuted.collectAsState()
    VoiceCallBar(callState, captionText, problemText, level, isMuted, call::cutAnswer, call::setMuted, call::hangUp, modifier)
}
