package com.teanode.app.chats

import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import com.teanode.app.R
import com.teanode.app.photos.PhotoViewer
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.kit.chats.shareAddress
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.VideoControls
import com.teanode.kit.photos.photoDetail
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/**
 * AgentPhotoViewer opens one photo the agent showed in a conversation in the app's photo viewer (ios AgentPhotoViewer). The
 * photo is not in any list the Photos screens have read, so the viewer is given just that photo.
 */
@Composable
fun AgentPhotoViewer(photoId: String, onClose: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    var photo by remember(photoId) { mutableStateOf<Photo?>(null) }
    var failure by remember(photoId) { mutableStateOf<Throwable?>(null) }
    LaunchedEffect(photoId) {
        try {
            photo = connection.photoDetail(photoId).photo
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (problem: Throwable) {
            failure = problem
        }
    }
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surface) {
            val read = photo
            val problem = failure
            when {
                read != null -> PhotoViewer(
                    photos = listOf(read), initialPhotoId = read.id, onClose = onClose, library = null,
                    onPhotoChanged = { changed -> photo = changed }, onPhotosGone = { onClose() },
                )
                problem != null -> EmptyState(
                    Icons.Outlined.ErrorOutline, stringResource(R.string.chats_cannot_show_photo), description = session.describeLoadProblem(problem),
                    actionLabel = stringResource(R.string.chats_close), onAction = onClose,
                )
                else -> LoadingState()
            }
        }
    }
}

/**
 * The player of a video or a sound the agent handed over, in its card. A player cannot send the app's sign-in, so it is
 * given an address that opens without one, for a few hours. Nothing is drawn when the server cannot give one: the card's
 * Open and Share still work.
 */
@Composable
fun AttachmentMediaPlayer(attachmentId: String, isVideo: Boolean, modifier: Modifier = Modifier) {
    val connection = LocalConnection.current
    var address by remember(attachmentId) { mutableStateOf<String?>(null) }
    var isUnavailable by remember(attachmentId) { mutableStateOf(false) }
    LaunchedEffect(attachmentId) {
        try {
            address = connection.shareAddress(attachmentId)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (problem: Throwable) {
            isUnavailable = true
        }
    }
    val readAddress = address
    when {
        isUnavailable -> Unit
        readAddress == null -> Box(modifier.fillMaxWidth().height(if (isVideo) 220.dp else 60.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp)) }
        else -> InlineMediaPlayer(readAddress, isVideo, modifier)
    }
}

/** A video (220 dp tall) or a sound (one line of controls) played in place. */
@Composable
fun InlineMediaPlayer(address: String, isVideo: Boolean, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val player = remember(address) { ExoPlayer.Builder(context).build().apply { setMediaItem(MediaItem.fromUri(address)); prepare() } }
    DisposableEffect(player) { onDispose { player.release() } }
    if (isVideo) InlineVideo(player, modifier) else InlineSound(player, modifier)
}

@OptIn(UnstableApi::class)
@Composable
private fun InlineVideo(player: ExoPlayer, modifier: Modifier) {
    AndroidView(
        factory = { viewContext ->
            PlayerView(viewContext).apply {
                this.player = player
                useController = true
                // One video, not a list: no previous or next.
                setShowNextButton(false)
                setShowPreviousButton(false)
            }
        },
        modifier = modifier.fillMaxWidth().height(220.dp).clip(RoundedCornerShape(12.dp)),
    )
}

/** A sound: play and pause, a bar to drag, and the time played of the whole. */
@Composable
private fun InlineSound(player: ExoPlayer, modifier: Modifier) {
    var isPlaying by remember(player) { mutableStateOf(false) }
    var elapsedSeconds by remember(player) { mutableFloatStateOf(0f) }
    var durationSeconds by remember(player) { mutableFloatStateOf(0f) }
    var isScrubbing by remember(player) { mutableStateOf(false) }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) { isPlaying = isPlayingNow }
            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_READY) durationSeconds = player.duration.coerceAtLeast(0L) / 1000f
                if (state == Player.STATE_ENDED) { player.pause(); player.seekTo(0) }
            }
        }
        player.addListener(listener)
        onDispose { player.removeListener(listener) }
    }
    LaunchedEffect(player, isScrubbing) {
        while (true) {
            if (!isScrubbing) elapsedSeconds = player.currentPosition / 1000f
            delay(250)
        }
    }
    Row(modifier.fillMaxWidth().height(60.dp).clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceVariant), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { if (isPlaying) player.pause() else player.play() }) {
            Icon(
                if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                contentDescription = stringResource(if (isPlaying) R.string.photos_video_pause else R.string.photos_play),
            )
        }
        Slider(
            value = VideoControls.playedFraction(elapsedSeconds.toDouble(), durationSeconds.toDouble()).toFloat(),
            onValueChange = { fraction -> isScrubbing = true; elapsedSeconds = VideoControls.seconds(fraction.toDouble(), durationSeconds.toDouble()).toFloat() },
            onValueChangeFinished = { player.seekTo((elapsedSeconds * 1000).toLong()); isScrubbing = false },
            modifier = Modifier.weight(1f),
        )
        Text(
            "${VideoControls.timeText(elapsedSeconds.toDouble())} / ${VideoControls.timeText(durationSeconds.toDouble())}",
            style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 4.dp, end = 12.dp),
        )
    }
}
