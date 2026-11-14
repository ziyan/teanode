package com.teanode.app.recordings

import android.content.Context
import com.teanode.app.photos.newShareFolder
import com.teanode.app.photos.offerSharedFiles
import com.teanode.app.photos.writeSharedFile
import com.teanode.kit.Connection
import com.teanode.kit.recordings.RecordingMedia
import com.teanode.kit.recordings.downloadRecordingOriginal

/**
 * Reads one original from the server, checks it is the file the server recorded (its length and digest), and offers it to
 * the apps the person picks (ios RecordingPlaybackScreen, "Share full original").
 */
suspend fun Connection.shareRecordingOriginal(context: Context, media: RecordingMedia) {
    val bytes = downloadRecordingOriginal(media)
    val file = writeSharedFile(context, newShareFolder(context), media.originalFilename, bytes, media.originalContentType, mutableSetOf())
    offerSharedFiles(context, listOf(file))
}
