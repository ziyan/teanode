package com.teanode.app.recordings

import android.content.Context
import com.teanode.app.photos.newShareFolder
import com.teanode.app.photos.offerSharedFiles
import com.teanode.app.photos.sharedFileOf
import com.teanode.kit.photos.PhotoShareFiles
import com.teanode.kit.Connection
import com.teanode.kit.recordings.RecordingMedia
import com.teanode.kit.recordings.downloadRecordingOriginal

/**
 * Reads one original from the server straight into a file, as it arrives, checks it is the file the server recorded (its
 * length and digest), and offers it to the apps the person picks (ios RecordingPlaybackScreen, "Share full original").
 */
suspend fun Connection.shareRecordingOriginal(context: Context, media: RecordingMedia) {
    val folder = newShareFolder(context)
    val file = folder.resolve(PhotoShareFiles.fileName(media.originalFilename, emptySet()))
    downloadRecordingOriginal(media, file)
    offerSharedFiles(context, listOf(sharedFileOf(context, file, media.originalContentType)))
}
