package com.teanode.app.photos

import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.content.FileProvider
import com.teanode.kit.Connection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoShareFiles
import java.io.File

/** One photo's file, written where another app may read it. */
class SharedPhotoFile(val uri: Uri, val contentType: String)

/** A share's files stay a while, for the app that was chosen to read them, and are cleared by the next share. */
private const val SHARE_FILES_KEPT_MS = 60L * 60 * 1000

/**
 * Reads the photos' files from the server and hands them to the apps the person picks, as the iPhone share sheet does:
 * the original of each (or the role asked for, when one photo is shared), without putting anything in the gallery.
 */
suspend fun Connection.sharePhotoFiles(context: Context, photos: List<Photo>, photoFileRole: String? = null) {
    val folder = newShareFolder(context)
    val taken = mutableSetOf<String>()
    val files = photos.map { photo ->
        val download = downloadPhotoFile(photo, photoFileRole)
        writeSharedFile(context, folder, download.filename, download.bytes, download.contentType, taken)
    }
    offerSharedFiles(context, files)
}

/** A folder of its own for one share, in the place the file provider hands out; the shares of an hour ago are cleared first. */
internal fun newShareFolder(context: Context): File {
    val root = File(context.cacheDir, "chat-files/photo-share")
    val oldest = System.currentTimeMillis() - SHARE_FILES_KEPT_MS
    root.listFiles()?.filter { folder -> folder.lastModified() < oldest }?.forEach { folder -> folder.deleteRecursively() }
    return root.resolve(System.currentTimeMillis().toString()).also { folder -> folder.mkdirs() }
}

/** Writes one file of a share under a name no other file of it has, and says where other apps may read it. */
internal fun writeSharedFile(context: Context, folder: File, wantedName: String, bytes: ByteArray, contentType: String, taken: MutableSet<String>): SharedPhotoFile {
    val name = PhotoShareFiles.fileName(wantedName, taken).also { chosen -> taken += chosen }
    val file = folder.resolve(name)
    file.writeBytes(bytes)
    return SharedPhotoFile(FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", file), contentType)
}

/** Opens the system share sheet for the files. */
internal fun offerSharedFiles(context: Context, files: List<SharedPhotoFile>) {
    if (files.isEmpty()) return
    val contentType = PhotoShareFiles.commonContentType(files.map { file -> file.contentType })
    val send = if (files.size == 1) {
        Intent(Intent.ACTION_SEND).putExtra(Intent.EXTRA_STREAM, files.first().uri)
    } else {
        Intent(Intent.ACTION_SEND_MULTIPLE).putParcelableArrayListExtra(Intent.EXTRA_STREAM, ArrayList(files.map { file -> file.uri }))
    }
    send.type = if (files.size == 1) files.first().contentType.ifEmpty { contentType } else contentType
    send.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    send.clipData = ClipData.newRawUri(null, files.first().uri).also { clip -> files.drop(1).forEach { file -> clip.addItem(ClipData.Item(file.uri)) } }
    context.startActivity(Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}
