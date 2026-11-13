package com.teanode.app.recordings

import android.content.Context
import androidx.annotation.OptIn
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import com.teanode.kit.Connection
import com.teanode.kit.recordings.RecordingMedia
import com.teanode.kit.recordings.RecordingTimelinePieces
import com.teanode.kit.recordings.prepareRecordingPlayback
import com.teanode.kit.recordings.recordingCompatiblePlaybackRequest
import com.teanode.kit.recordings.recordingPlayback
import com.teanode.kit.recordings.recordingPlaybackRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Plays a recording's originals from the server as one timeline (ios/TeaNode/Sources/RecordingPlayer.swift): ExoPlayer
 * with one item for each original that is complete on the server, read with the person's token. A point of the
 * timeline is found with [RecordingTimelinePieces]; a piece the player cannot decode is played from the compatible
 * audio the server prepares (a WAV), and the original is left as it was.
 */
@OptIn(UnstableApi::class)
@Stable
class RecordingPlayerController(private val context: Context, private val connection: Connection, private val scope: CoroutineScope) {
    var isPlaying by mutableStateOf(false)
        private set
    var timelineMS by mutableLongStateOf(0)
        private set
    var problem by mutableStateOf<Throwable?>(null)
        private set
    var isPreparing by mutableStateOf(false)
        private set

    private var player: ExoPlayer? = null
    private var pieces: List<RecordingMedia> = emptyList()
    private var compatibleIndexes = mutableSetOf<Int>()

    /** The whole timeline's length: where the last original ends. */
    val durationMS: Long get() = pieces.lastOrNull()?.let { last -> RecordingTimelinePieces.endMS(last, pieces) } ?: 0

    private suspend fun ensurePlayer(mediaFiles: List<RecordingMedia>): ExoPlayer {
        player?.let { existing -> if (pieces.map { piece -> piece.id } == RecordingTimelinePieces.playable(mediaFiles).map { piece -> piece.id }) return existing }
        player?.release()
        pieces = RecordingTimelinePieces.playable(mediaFiles)
        compatibleIndexes = mutableSetOf()
        val headers = connection.recordingPlaybackRequest(pieces.first().id).headers
        val factory = DefaultHttpDataSource.Factory().setDefaultRequestProperties(headers)
        val created = ExoPlayer.Builder(context).setMediaSourceFactory(DefaultMediaSourceFactory(factory)).build()
        created.setMediaItems(pieces.map { piece -> MediaItem.fromUri(connection.account.server.endpoint("/api/v1/recordings/media/${piece.id}").toString()) })
        created.addListener(object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) { isPlaying = isPlayingNow }

            override fun onPlayerError(error: PlaybackException) {
                val index = created.currentMediaItemIndex
                if (index in compatibleIndexes) { problem = error; return }
                // The original is in a format this phone cannot play: play the compatible audio the server prepares.
                scope.launch {
                    try {
                        val piece = pieces[index]
                        val playback = connection.recordingPlayback(piece) ?: connection.prepareRecordingPlayback(piece)
                        val request = connection.recordingCompatiblePlaybackRequest(playback, piece)
                        compatibleIndexes.add(index)
                        val position = created.currentPosition
                        created.replaceMediaItem(index, MediaItem.fromUri(request.url))
                        created.seekTo(index, position)
                        created.prepare()
                        created.play()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        problem = failure
                    }
                }
            }
        })
        created.prepare()
        player = created
        scope.launch {
            while (isActive && player === created) {
                val index = created.currentMediaItemIndex
                pieces.getOrNull(index)?.let { piece -> timelineMS = piece.timelineStartMS + created.currentPosition }
                delay(250)
            }
        }
        return created
    }

    /** Plays from a point of the timeline (a passage's start): a point in a gap plays from the next original. */
    fun playFrom(mediaFiles: List<RecordingMedia>, atTimelineMS: Long) {
        scope.launch {
            try {
                isPreparing = true
                problem = null
                val active = ensurePlayer(mediaFiles)
                val location = RecordingTimelinePieces.locate(atTimelineMS, mediaFiles) ?: return@launch
                val index = pieces.indexOfFirst { piece -> piece.id == location.media.id }
                active.seekTo(index, location.offsetMS)
                active.play()
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                problem = failure
            } finally {
                isPreparing = false
            }
        }
    }

    fun toggle(mediaFiles: List<RecordingMedia>) {
        val active = player
        if (active == null) { playFrom(mediaFiles, 0); return }
        if (active.isPlaying) active.pause() else active.play()
    }

    fun seekTo(mediaFiles: List<RecordingMedia>, atTimelineMS: Long) {
        val active = player
        if (active == null) { playFrom(mediaFiles, atTimelineMS); return }
        val location = RecordingTimelinePieces.locate(atTimelineMS, mediaFiles) ?: return
        active.seekTo(pieces.indexOfFirst { piece -> piece.id == location.media.id }, location.offsetMS)
    }

    fun release() {
        player?.release()
        player = null
        isPlaying = false
    }
}
