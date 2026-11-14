package com.teanode.app.calls

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.kit.calls.ChatCallGrid
import com.teanode.kit.calls.ChatCallMemberView
import com.teanode.kit.calls.ChatCallStage

/** The people on the call and those still being rung, the person first. */
fun tileMembersOf(state: CallSessionState): List<ChatCallMemberView> {
    val call = state.call ?: return emptyList()
    val shown = call.members.filter { member -> member.isOn || member.ringState == "ringing" }
    return shown.filter { member -> member.userId == state.ownUserId } + shown.filter { member -> member.userId != state.ownUserId }
}

/**
 * CallTile is one person on the call: their picture when their camera is on, otherwise their avatar, with their name
 * over it.
 */
@Composable
fun CallTile(
    member: ChatCallMemberView,
    isOwn: Boolean,
    isVideoOn: Boolean,
    isMuted: Boolean,
    track: CallVideoTrack?,
    modifier: Modifier = Modifier,
    /** Whether the person chose them to feature, marked with a pin. */
    isChosen: Boolean = false,
    /** How big the picture of a person with their camera off is: smaller in the row under the one featured. */
    avatarSize: Dp = 96.dp,
) {
    val isSmall = avatarSize < 96.dp
    val isRinging = !member.isOn && member.ringState == "ringing"
    val name = if (isOwn) stringResource(R.string.calls_you) else member.name
    Box(
        modifier
            .clip(RoundedCornerShape(18.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh),
    ) {
        when {
            isVideoOn && track != null -> CallVideoView(track, Modifier.fillMaxSize(), isMirrored = isOwn)
            // The picture is on its way.
            // In a small tile it sits at the top, clear of the name under it.
            isVideoOn -> Icon(
                Icons.Filled.Videocam, contentDescription = null,
                modifier = if (isSmall) Modifier.align(Alignment.TopCenter).padding(top = 8.dp).size(28.dp) else Modifier.align(Alignment.Center).size(40.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            // In a small tile the picture sits at the top, clear of the name under it.
            else -> Box(
                Modifier.fillMaxSize().padding(top = if (isSmall) 8.dp else 0.dp).alpha(if (isRinging) 0.5f else 1f),
                contentAlignment = if (isSmall) Alignment.TopCenter else Alignment.Center,
            ) {
                AvatarView(name = member.name, avatarUrl = member.avatarUrl, colorIndex = member.avatarColorIndex, size = avatarSize)
            }
        }
        Surface(
            modifier = Modifier.align(Alignment.BottomStart).padding(8.dp),
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surface.copy(alpha = 0.88f),
            border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant),
        ) {
            Column(Modifier.padding(horizontal = 10.dp, vertical = 5.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    if (isChosen) Icon(Icons.Filled.PushPin, contentDescription = null, modifier = Modifier.size(14.dp))
                    if (isMuted) {
                        Icon(Icons.Filled.MicOff, contentDescription = stringResource(R.string.calls_muted_mark), modifier = Modifier.size(14.dp))
                    }
                    Text(name, style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold), maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                if (isRinging) {
                    Text(stringResource(R.string.calls_ringing), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

/** A person's tile on the call screen: tapping it (not the person's own) features them, or goes back to following whoever speaks when they are the one chosen. */
@Composable
private fun MemberTile(state: CallSessionState, member: ChatCallMemberView, onFeature: (userId: String?) -> Unit, modifier: Modifier, avatarSize: Dp = 96.dp) {
    val isOwn = member.userId == state.ownUserId
    val isChosen = member.userId == state.featuredUserId
    CallTile(
        member = member, isOwn = isOwn, isVideoOn = if (isOwn) state.isCameraOn else member.isVideoOn,
        isMuted = if (isOwn) state.isMuted else member.isMuted,
        track = if (isOwn) state.localVideoTrack else state.remoteVideoTracks[member.userId],
        modifier = if (isOwn) modifier else modifier.semantics { selected = isChosen }.clickable { onFeature(if (isChosen) null else member.userId) },
        isChosen = isChosen, avatarSize = avatarSize,
    )
}

/**
 * The people of the call: one person whole above a row of the rest once the call is big enough for the grid's
 * pictures to be small or the person chose somebody, otherwise a grid. Tapping somebody features them; tapping the one
 * chosen goes back to following whoever speaks.
 */
@Composable
fun CallTiles(state: CallSessionState, onFeature: (userId: String?) -> Unit, modifier: Modifier = Modifier) {
    val members = tileMembersOf(state)
    if (members.isEmpty()) {
        Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        return
    }
    val featured = if (ChatCallStage.isFeatured(members.size, state.featuredUserId)) {
        ChatCallStage.featured(members, state.ownUserId, state.featuredUserId, state.speakingUserId, state.lastOtherSpeakerId)
    } else {
        null
    }
    if (featured != null) {
        Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            MemberTile(state, featured, onFeature, Modifier.fillMaxWidth().weight(1f))
            // The row scrolls sideways when it is longer than the screen.
            Row(Modifier.fillMaxWidth().height(ROW_TILE_HEIGHT).horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                members.filter { member -> member.userId != featured.userId }.forEach { member ->
                    MemberTile(state, member, onFeature, Modifier.width(ROW_TILE_WIDTH).height(ROW_TILE_HEIGHT), avatarSize = 36.dp)
                }
            }
        }
    } else {
        val columns = ChatCallGrid.columnCount(members.size)
        Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            members.chunked(columns).forEach { rowMembers ->
                Row(Modifier.fillMaxWidth().weight(1f), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    rowMembers.forEach { member -> MemberTile(state, member, onFeature, Modifier.weight(1f).fillMaxSize()) }
                    // A last row with fewer people keeps their size.
                    repeat(columns - rowMembers.size) { Box(Modifier.weight(1f)) }
                }
            }
        }
    }
}

private val ROW_TILE_WIDTH = 120.dp
private val ROW_TILE_HEIGHT = 90.dp
