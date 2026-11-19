package com.teanode.app.feed

import android.content.Context
import android.provider.Settings
import androidx.annotation.OptIn
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.VolumeOff
import androidx.compose.material.icons.outlined.VolumeUp
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.ui.PlayerView
import com.teanode.app.R
import com.teanode.app.chats.PictureLightbox
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.feed.AgentFeedMediaArrangement
import com.teanode.kit.feed.AgentFeedMediaKind
import com.teanode.kit.feed.AgentFeedMediaView

/** Whether the person turned animations off in the system settings (a scale of 0), which the deck's motion respects. */
@Composable
fun rememberAreAnimationsOff(): Boolean {
    val context = LocalContext.current
    return remember(context) { areAnimationsOff(context) }
}

fun areAnimationsOff(context: Context): Boolean =
    runCatching { Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f }.getOrDefault(false)

private val GAP = 2.dp

/**
 * The pictures of a card, at its top edge to edge: one full width (16:9, a portrait photo 4:5), two to four in a grid,
 * more in a row that scrolls sideways. A caption sits under a single picture in secondary text; in a grid or a row it
 * is on the picture. Every image is read from the server with the session, never from a public address.
 */
@Composable
fun FeedMediaBlock(media: List<AgentFeedMediaView>, actions: FeedActions, modifier: Modifier = Modifier) {
    when (AgentFeedMediaArrangement.of(media)) {
        AgentFeedMediaArrangement.None -> Unit
        AgentFeedMediaArrangement.Single, AgentFeedMediaArrangement.SinglePortrait -> {
            val only = media.first()
            Column(modifier.fillMaxWidth()) {
                FeedMediaTile(only, actions, Modifier.fillMaxWidth().aspectRatio(if (only.isPortrait) 4f / 5f else 16f / 9f), isCaptionShown = false)
                if (only.mediaCaption.isNotEmpty()) {
                    Text(only.mediaCaption, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp))
                }
            }
        }
        AgentFeedMediaArrangement.Grid -> Box(modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
            when (media.size) {
                2 -> Row(Modifier.fillMaxSize(), horizontalArrangement = Arrangement.spacedBy(GAP)) {
                    for (each in media) FeedMediaTile(each, actions, Modifier.weight(1f).fillMaxHeight())
                }
                3 -> Row(Modifier.fillMaxSize(), horizontalArrangement = Arrangement.spacedBy(GAP)) {
                    FeedMediaTile(media[0], actions, Modifier.weight(1f).fillMaxHeight())
                    Column(Modifier.weight(1f).fillMaxHeight(), verticalArrangement = Arrangement.spacedBy(GAP)) {
                        FeedMediaTile(media[1], actions, Modifier.weight(1f).fillMaxWidth())
                        FeedMediaTile(media[2], actions, Modifier.weight(1f).fillMaxWidth())
                    }
                }
                else -> Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(GAP)) {
                    for (pair in media.chunked(2)) {
                        Row(Modifier.weight(1f).fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(GAP)) {
                            for (each in pair) FeedMediaTile(each, actions, Modifier.weight(1f).fillMaxHeight())
                        }
                    }
                }
            }
        }
        AgentFeedMediaArrangement.Row -> Row(modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(GAP)) {
            for (each in media) FeedMediaTile(each, actions, Modifier.width(160.dp).height(110.dp))
        }
    }
}

/** One picture: its image cropped to fill, a play button on a video (which plays in place when pressed), a photo opens full screen, a page's picture opens the page. */
@Composable
fun FeedMediaTile(media: AgentFeedMediaView, actions: FeedActions, modifier: Modifier = Modifier, isCaptionShown: Boolean = true) {
    var isPlaying by remember(media.mediaKey) { mutableStateOf(false) }
    var isLightboxOpen by remember(media.mediaKey) { mutableStateOf(false) }
    val kindWords = stringResource(
        when (media.kind) {
            AgentFeedMediaKind.Photo -> R.string.feed_media_photo
            AgentFeedMediaKind.Video -> R.string.feed_media_video
            AgentFeedMediaKind.Picture -> R.string.feed_media_picture
        },
    )
    val description = media.mediaCaption.ifEmpty { media.siteName.ifEmpty { kindWords } }
    val pageAddress = media.pageAddress
    Box(modifier.background(MaterialTheme.colorScheme.surfaceContainerHighest)) {
        if (isPlaying && media.kind == AgentFeedMediaKind.Video) {
            FeedVideoPlayer(media, Modifier.fillMaxSize())
        } else {
            FeedMediaImage(media.imagePath, description, Modifier.fillMaxSize().clickable(role = Role.Button, onClickLabel = if (media.kind == AgentFeedMediaKind.Video) stringResource(R.string.feed_media_play) else null) {
                when {
                    media.kind == AgentFeedMediaKind.Video && media.videoPath != null -> isPlaying = true
                    media.kind == AgentFeedMediaKind.Photo -> isLightboxOpen = true
                    pageAddress != null -> actions.followLink(pageAddress)
                }
            })
            if (media.kind == AgentFeedMediaKind.Video) {
                Box(Modifier.align(Alignment.Center).size(48.dp).background(Color.Black.copy(alpha = 0.55f), CircleShape), contentAlignment = Alignment.Center) {
                    Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = Color.White, modifier = Modifier.size(32.dp))
                }
            }
            if (isCaptionShown && media.mediaCaption.isNotEmpty()) {
                Text(
                    media.mediaCaption, style = MaterialTheme.typography.labelSmall, color = Color.White, maxLines = 2,
                    modifier = Modifier.align(Alignment.BottomStart).fillMaxWidth().background(Color.Black.copy(alpha = 0.6f)).padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
    }
    val path = media.imagePath
    if (isLightboxOpen && path != null) PictureLightbox(path, description, path, onClose = { isLightboxOpen = false })
}

/** A picture read from the server as the signed-in person; a tile in the card's grey while it comes, or when it cannot. */
@Composable
fun FeedMediaImage(path: String?, description: String, modifier: Modifier = Modifier) {
    val picture = rememberServerPicture(path)
    if (picture != null) {
        Image(picture, contentDescription = description, contentScale = ContentScale.Crop, modifier = modifier)
    } else {
        Box(modifier.semantics { contentDescription = description }, contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.Image, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(28.dp))
        }
    }
}

/** A square thumbnail at the leading edge of a list row. */
@Composable
fun FeedRowThumbnail(media: AgentFeedMediaView, size: Dp, modifier: Modifier = Modifier) {
    FeedMediaImage(
        media.imagePath, media.mediaCaption.ifEmpty { media.siteName },
        modifier.size(size).clip(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceContainerHighest),
    )
}

/**
 * A video played in place with ExoPlayer, from the server path with the session's token (never a public address). It
 * starts when pressed, muted: the sound is the person's to turn on. A tap on the picture pauses and plays.
 */
@OptIn(UnstableApi::class)
@Composable
fun FeedVideoPlayer(media: AgentFeedMediaView, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    var player by remember(media.mediaKey) { mutableStateOf<ExoPlayer?>(null) }
    var isMuted by remember(media.mediaKey) { mutableStateOf(true) }
    var isPaused by remember(media.mediaKey) { mutableStateOf(false) }
    LaunchedEffect(media.mediaKey) {
        val path = media.videoPath ?: return@LaunchedEffect
        val token = runCatching { connection.accessToken() }.getOrNull() ?: return@LaunchedEffect
        val factory = DefaultHttpDataSource.Factory().setDefaultRequestProperties(mapOf("Authorization" to "Bearer $token"))
        player = ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(factory)).build().apply {
            volume = 0f
            setMediaItem(MediaItem.fromUri(connection.account.server.endpoint(path).toString()))
            prepare()
            playWhenReady = true
        }
    }
    DisposableEffect(media.mediaKey) { onDispose { player?.release() } }
    Box(modifier.background(Color.Black)) {
        val current = player
        if (current != null) {
            AndroidView(
                factory = { viewContext -> PlayerView(viewContext).apply { useController = false; this.player = current } },
                modifier = Modifier.fillMaxSize().clickable(role = Role.Button) {
                    isPaused = !isPaused
                    if (isPaused) current.pause() else current.play()
                },
            )
            IconButton(onClick = { isMuted = !isMuted; current.volume = if (isMuted) 0f else 1f }, modifier = Modifier.align(Alignment.BottomEnd).padding(4.dp)) {
                Icon(
                    if (isMuted) Icons.Outlined.VolumeOff else Icons.Outlined.VolumeUp,
                    contentDescription = stringResource(if (isMuted) R.string.feed_media_unmute else R.string.feed_media_mute), tint = Color.White,
                )
            }
        }
    }
}
