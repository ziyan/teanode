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
import com.teanode.kit.photos.photoPlaybackAddress
import com.teanode.kit.photos.PhotoPlaybackSource
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
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
import androidx.media3.exoplayer.ExoPlayer
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

private val GRID_GAP = 6.dp
private val ROW_GAP = 8.dp
private val SCROLLED_TILE_WIDTH = 220.dp

/**
 * The pictures of a card, as the iPhone draws them (ios FeedMediaViews.swift, FeedCardMedia): one runs the card's whole
 * width at its top edge, 16:9 (4:5 for a portrait photo, 16:9 when its size is not known), cut to the card's corners by
 * the card; two to four are a grid of squares; more are a row that scrolls sideways. A caption sits under its picture in
 * secondary text. Every image is read from the server with the session, never from a public address, and the space is
 * kept for it while it comes, so a card that gets its picture later does not change shape when the picture arrives.
 */
@Composable
fun FeedMediaBlock(media: List<AgentFeedMediaView>, actions: FeedActions, modifier: Modifier = Modifier) {
    when (AgentFeedMediaArrangement.of(media)) {
        AgentFeedMediaArrangement.None -> Unit
        AgentFeedMediaArrangement.Single, AgentFeedMediaArrangement.SinglePortrait ->
            FeedMediaTile(media.first(), actions, media.first().singleAspectRatio, modifier.fillMaxWidth(), captionInset = 14.dp)
        AgentFeedMediaArrangement.Grid -> Column(modifier.fillMaxWidth().padding(start = 14.dp, end = 14.dp, top = 14.dp), verticalArrangement = Arrangement.spacedBy(GRID_GAP)) {
            for (pair in media.chunked(2)) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(GRID_GAP)) {
                    for (each in pair) FeedMediaTile(each, actions, 1f, Modifier.weight(1f), cornerRadius = 10.dp)
                    // A pair of one leaves the empty half empty, as the grid of two columns does.
                    if (pair.size == 1) Spacer(Modifier.weight(1f))
                }
            }
        }
        AgentFeedMediaArrangement.Row -> Row(
            modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(start = 14.dp, end = 14.dp, top = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(ROW_GAP), verticalAlignment = Alignment.Top,
        ) {
            for (each in media) FeedMediaTile(each, actions, 4f / 3f, Modifier.width(SCROLLED_TILE_WIDTH), cornerRadius = 10.dp)
        }
    }
}

/**
 * One picture or video with its caption under it. A video shows a play button and plays in place, muted until the
 * person unmutes it; a picture of a web page opens the page through the link opener; a photo opens full screen.
 */
@Composable
fun FeedMediaTile(
    media: AgentFeedMediaView,
    actions: FeedActions,
    aspectRatio: Float,
    modifier: Modifier = Modifier,
    cornerRadius: androidx.compose.ui.unit.Dp = 0.dp,
    /** How far the caption is kept from the sides, for a picture that runs to the card's edges. */
    captionInset: androidx.compose.ui.unit.Dp = 0.dp,
) {
    var isPlaying by remember(media.mediaKey) { mutableStateOf(false) }
    var isLightboxOpen by remember(media.mediaKey) { mutableStateOf(false) }
    val kindWords = stringResource(
        when (media.kind) {
            AgentFeedMediaKind.Photo -> R.string.feed_media_photo
            AgentFeedMediaKind.Video -> R.string.feed_media_video
            AgentFeedMediaKind.Picture -> R.string.feed_media_picture
        },
    )
    val description = media.caption.ifEmpty { kindWords }
    val pageAddress = media.pageAddress
    val canPlay = media.kind == AgentFeedMediaKind.Video && media.videoPhotoId != null
    val pressHint = stringResource(if (media.kind == AgentFeedMediaKind.Video) R.string.feed_media_plays_video else R.string.feed_media_opens_page)
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(Modifier.fillMaxWidth().aspectRatio(aspectRatio).clip(RoundedCornerShape(cornerRadius)).background(MaterialTheme.colorScheme.surfaceContainerHighest)) {
            if (isPlaying && canPlay) {
                FeedVideoPlayer(media, Modifier.fillMaxSize())
            } else {
                val isPressable = canPlay || media.kind == AgentFeedMediaKind.Photo || (media.kind == AgentFeedMediaKind.Picture && pageAddress != null)
                FeedMediaImage(
                    media.imagePath, description,
                    if (isPressable) {
                        Modifier.fillMaxSize().clickable(role = Role.Button, onClickLabel = if (media.kind == AgentFeedMediaKind.Photo) null else pressHint) {
                            when {
                                canPlay -> isPlaying = true
                                media.kind == AgentFeedMediaKind.Photo -> isLightboxOpen = true
                                pageAddress != null -> actions.followLink(pageAddress)
                            }
                        }
                    } else {
                        Modifier.fillMaxSize()
                    },
                )
                if (media.kind == AgentFeedMediaKind.Video) {
                    Box(Modifier.align(Alignment.Center).size(48.dp).background(Color.Black.copy(alpha = 0.55f), CircleShape), contentAlignment = Alignment.Center) {
                        Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = Color.White, modifier = Modifier.size(32.dp))
                    }
                }
            }
        }
        if (media.caption.isNotEmpty()) {
            Text(media.caption, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = captionInset))
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
        media.imagePath, media.caption.ifEmpty { media.siteLabel },
        modifier.size(size).clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.surfaceContainerHighest),
    )
}

/**
 * A video played in place with ExoPlayer, from the playable address the server gives for the photo it is (as the Photos
 * viewer asks for one), never a public address. It starts when pressed, muted: the sound is the person's to turn on. A
 * tap on the picture pauses and plays.
 */
@OptIn(UnstableApi::class)
@Composable
fun FeedVideoPlayer(media: AgentFeedMediaView, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    var player by remember(media.mediaKey) { mutableStateOf<ExoPlayer?>(null) }
    var isFailed by remember(media.mediaKey) { mutableStateOf(false) }
    var isMuted by remember(media.mediaKey) { mutableStateOf(true) }
    var isPaused by remember(media.mediaKey) { mutableStateOf(false) }
    LaunchedEffect(media.mediaKey) {
        val photoId = media.videoPhotoId
        if (photoId == null) {
            isFailed = true
            return@LaunchedEffect
        }
        try {
            val address = connection.photoPlaybackAddress(photoId, PhotoPlaybackSource.Preview("video"))
            player = ExoPlayer.Builder(context).build().apply {
                volume = 0f
                setMediaItem(MediaItem.fromUri(address))
                prepare()
                playWhenReady = true
            }
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (_: Throwable) {
            isFailed = true
        }
    }
    DisposableEffect(media.mediaKey) { onDispose { player?.release() } }
    Box(modifier.background(Color.Black), contentAlignment = Alignment.Center) {
        val current = player
        when {
            current != null -> {
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
            isFailed -> Text(stringResource(R.string.feed_media_cannot_play), color = Color.White, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(16.dp))
            else -> CircularProgressIndicator(color = Color.White)
        }
    }
}
