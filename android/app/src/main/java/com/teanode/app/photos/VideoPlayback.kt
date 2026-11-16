package com.teanode.app.photos

import android.view.ViewGroup
import androidx.annotation.OptIn
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Forward5
import androidx.compose.material.icons.outlined.Replay5
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoPlaybackSource
import com.teanode.kit.photos.VideoControls
import com.teanode.kit.photos.photoPlaybackAddress
import kotlinx.coroutines.delay

/**
 * Plays a photo's video with ExoPlayer from the signed address the server gives (ios/TeaNode/Sources/VideoPlayback.swift):
 * play and pause, five seconds back and forward, a bar to drag, and controls that fade after three seconds of playing
 * ([VideoControls] has the rules, which are the iPhone app's).
 */
@OptIn(UnstableApi::class)
@Composable
fun VideoPlayback(photo: Photo, onEnded: () -> Unit) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    var address by remember(photo.id) { mutableStateOf<String?>(null) }
    var isPlaying by remember { mutableStateOf(true) }
    var hasEnded by remember { mutableStateOf(false) }
    var isScrubbing by remember { mutableStateOf(false) }
    var areControlsShown by remember { mutableStateOf(true) }
    var elapsedSeconds by remember { mutableFloatStateOf(0f) }
    var durationSeconds by remember { mutableFloatStateOf(photo.durationMS / 1000f) }
    var lastTouchedAtMS by remember { mutableStateOf(System.currentTimeMillis()) }
    LaunchedEffect(photo.id) {
        try {
            address = connection.photoPlaybackAddress(photo.id, PhotoPlaybackSource.Preview("video"))
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: Throwable) {
            snackbar.showProblem(context.getString(R.string.photos_cannot_play))
            onEnded()
        }
    }
    val player = remember(address) { address?.let { url -> ExoPlayer.Builder(context).build().apply { setMediaItem(MediaItem.fromUri(url)); prepare(); playWhenReady = true } } }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) { isPlaying = isPlayingNow }
            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) hasEnded = true
                if (state == Player.STATE_READY) durationSeconds = (player?.duration ?: 0L) / 1000f
            }
        }
        player?.addListener(listener)
        onDispose { player?.removeListener(listener); player?.release() }
    }
    LaunchedEffect(player, isScrubbing) {
        while (player != null) {
            if (!isScrubbing) elapsedSeconds = player.currentPosition / 1000f
            if (areControlsShown && VideoControls.shouldHide(isPlaying, hasEnded, isScrubbing, isScreenReaderRunning(context)) &&
                System.currentTimeMillis() - lastTouchedAtMS > VideoControls.HIDE_DELAY_SECONDS * 1000
            ) {
                areControlsShown = false
            }
            delay(250)
        }
    }
    Box(Modifier.fillMaxSize()) {
        if (player == null) {
            CircularProgressIndicator(Modifier.align(Alignment.Center), color = Color.White)
        } else {
            AndroidView(
                factory = { viewContext -> PlayerView(viewContext).apply { useController = false; layoutParams = ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT); this.player = player } },
                modifier = Modifier.fillMaxSize(),
            )
            Box(Modifier.fillMaxSize().androidxClickable { areControlsShown = !areControlsShown; lastTouchedAtMS = System.currentTimeMillis() })
            if (areControlsShown) {
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(16.dp)) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = androidx.compose.foundation.layout.Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                        IconButton(onClick = { player.seekTo((VideoControls.seconds(elapsedSeconds.toDouble(), -VideoControls.ADJUST_STEP_SECONDS, durationSeconds.toDouble()) * 1000).toLong()); lastTouchedAtMS = System.currentTimeMillis() }) {
                            Icon(Icons.Outlined.Replay5, contentDescription = stringResource(R.string.photos_video_back), tint = Color.White)
                        }
                        IconButton(onClick = {
                            if (hasEnded || VideoControls.isAtEnd(elapsedSeconds.toDouble(), durationSeconds.toDouble())) { player.seekTo(0); hasEnded = false }
                            if (isPlaying) player.pause() else player.play()
                            lastTouchedAtMS = System.currentTimeMillis()
                        }, modifier = Modifier.size(64.dp)) {
                            Icon(if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow, contentDescription = stringResource(if (isPlaying) R.string.photos_video_pause else R.string.photos_play), tint = Color.White, modifier = Modifier.size(40.dp))
                        }
                        IconButton(onClick = { player.seekTo((VideoControls.seconds(elapsedSeconds.toDouble(), VideoControls.ADJUST_STEP_SECONDS, durationSeconds.toDouble()) * 1000).toLong()); lastTouchedAtMS = System.currentTimeMillis() }) {
                            Icon(Icons.Outlined.Forward5, contentDescription = stringResource(R.string.photos_video_forward), tint = Color.White)
                        }
                    }
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(VideoControls.timeText(elapsedSeconds.toDouble()), color = Color.White)
                        Slider(
                            value = VideoControls.playedFraction(elapsedSeconds.toDouble(), durationSeconds.toDouble()).toFloat(),
                            onValueChange = { fraction -> isScrubbing = true; elapsedSeconds = VideoControls.seconds(fraction.toDouble(), durationSeconds.toDouble()).toFloat() },
                            onValueChangeFinished = { player.seekTo((elapsedSeconds * 1000).toLong()); isScrubbing = false; lastTouchedAtMS = System.currentTimeMillis() },
                            modifier = Modifier.weight(1f).padding(horizontal = 8.dp),
                        )
                        Text(VideoControls.timeText(durationSeconds.toDouble()), color = Color.White)
                    }
                }
            }
        }
    }
}

private fun isScreenReaderRunning(context: android.content.Context): Boolean =
    (context.getSystemService(android.content.Context.ACCESSIBILITY_SERVICE) as? android.view.accessibility.AccessibilityManager)?.isTouchExplorationEnabled == true

private fun Modifier.androidxClickable(onClick: () -> Unit): Modifier = this.clickable(indication = null, interactionSource = null, onClick = onClick)
