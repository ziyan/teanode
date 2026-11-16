package com.teanode.app.chats

import androidx.compose.animation.core.LinearOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.chats.EarlierPull
import com.teanode.kit.chats.EarlierRowState

/**
 * EarlierPullTracker follows a finger pulling a conversation down past its oldest message. It loads only when the
 * finger lets go after the pull armed; reaching the top by scrolling loads nothing.
 */
class EarlierPullTracker(private val density: Density, private val onArmed: () -> Unit, private val onLetGoArmed: () -> Unit) {
    private val pull = EarlierPull()

    /** Whether a finger is on the list, which the pull and the following of new messages both go by. */
    var isFingerDown: Boolean by mutableStateOf(false)
        private set
    var pullDp: Float by mutableFloatStateOf(0f)
        private set
    var isArmed: Boolean by mutableStateOf(false)
        private set

    val nestedScrollConnection = object : NestedScrollConnection {
        // Moving the finger back up first takes back what was pulled.
        override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
            if (source != NestedScrollSource.UserInput || pullDp <= 0f || available.y >= 0f) return Offset.Zero
            val before = pullDp
            pull.pulled(available.y / density.density, isFingerDown)
            sync()
            return Offset(0f, -(before - pullDp) * density.density)
        }

        // What the list could not scroll any further down is the pull.
        override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset {
            if (source != NestedScrollSource.UserInput || available.y <= 0f || !isFingerDown) return Offset.Zero
            if (pull.pulled(available.y / density.density, isFingerDown)) onArmed()
            sync()
            return Offset(0f, available.y)
        }
    }

    private fun sync() {
        pullDp = pull.pullDp
        isArmed = pull.isArmed
    }

    /** Watches the fingers on the list without taking any of their movement. */
    val fingerWatcher: Modifier = Modifier.pointerInput(this) {
        awaitEachGesture {
            awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
            isFingerDown = true
            do {
                val event = awaitPointerEvent(PointerEventPass.Initial)
            } while (event.changes.any { change -> change.pressed })
            isFingerDown = false
            val shouldLoad = pull.released()
            sync()
            if (shouldLoad) onLetGoArmed()
        }
    }
}

/**
 * The row at the very top of a conversation, which always says what is above: a hint to pull, that letting go will
 * load, that it is loading, that it failed (with Try again), or that this is the start. For a screen reader the row is
 * a button with an action to load the earlier messages.
 */
@Composable
fun EarlierMessagesRow(state: EarlierRowState, pullDp: Float, failureText: String?, onLoad: () -> Unit, modifier: Modifier = Modifier) {
    val loadLabel = stringResource(R.string.chats_load_earlier_action)
    val arrowTurn by animateFloatAsState(if (state == EarlierRowState.Armed) 180f else 0f, tween(150, easing = LinearOutSlowInEasing), label = "arrow turn")
    val canLoad = state == EarlierRowState.PullHint || state == EarlierRowState.Armed
    val rowModifier = modifier.fillMaxWidth().heightIn(min = 48.dp).let { base ->
        if (canLoad) base.semantics { customActions = listOf(CustomAccessibilityAction(loadLabel) { onLoad(); true }) }.clickable(role = Role.Button, onClickLabel = loadLabel, onClick = onLoad) else base
    }
    Row(rowModifier.padding(vertical = 8.dp), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
        when (state) {
            EarlierRowState.PullHint, EarlierRowState.Armed -> {
                Icon(
                    Icons.Outlined.ArrowDownward, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(18.dp).alpha(EarlierPull.arrowOpacity(if (state == EarlierRowState.Armed) EarlierPull.ARM_DP else pullDp)).rotate(arrowTurn),
                )
                Text(
                    stringResource(if (state == EarlierRowState.Armed) R.string.chats_release_for_earlier else R.string.chats_pull_for_earlier),
                    Modifier.padding(start = 8.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center,
                )
            }
            EarlierRowState.Loading -> {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.chats_loading_earlier), Modifier.padding(start = 8.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            EarlierRowState.Failed -> {
                Text(failureText ?: stringResource(R.string.chats_could_not_load_earlier), Modifier.weight(1f, fill = false), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.error)
                OutlinedButton(onClick = onLoad, Modifier.padding(start = 8.dp)) { Text(stringResource(R.string.chats_try_again)) }
            }
            EarlierRowState.StartOfConversation -> {
                Text(stringResource(R.string.chats_start_of_conversation), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.outline)
            }
        }
    }
}

/** The button that waits under the messages being read while new ones arrive; it scrolls to the end. */
@Composable
fun NewMessagesButton(onClick: () -> Unit) {
    val label = stringResource(R.string.chats_new_messages)
    androidx.compose.material3.Surface(
        shape = androidx.compose.foundation.shape.RoundedCornerShape(50), color = MaterialTheme.colorScheme.primary, shadowElevation = 4.dp,
        modifier = Modifier.clickable(role = Role.Button, onClickLabel = label, onClick = onClick),
    ) {
        Row(Modifier.heightIn(min = 44.dp).padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Icon(Icons.Outlined.ArrowDownward, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(18.dp))
            Text(label, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimary)
        }
    }
}

/** The round button that jumps to the end of a conversation when nothing new has arrived. */
@Composable
fun JumpToEndButton(onClick: () -> Unit) {
    androidx.compose.foundation.layout.Box(
        Modifier.size(44.dp).clip(androidx.compose.foundation.shape.CircleShape).background(MaterialTheme.colorScheme.surfaceContainerHighest).clickable(role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Icon(Icons.Outlined.ArrowDownward, contentDescription = stringResource(R.string.chats_jump_to_end)) }
}
