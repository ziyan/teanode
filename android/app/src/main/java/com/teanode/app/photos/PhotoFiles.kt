package com.teanode.app.photos

import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Environment
import android.provider.MediaStore
import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoDownload
import com.teanode.kit.photos.PhotoError
import com.teanode.kit.photos.PhotoPlaybackSource
import com.teanode.kit.photos.photoDetail
import com.teanode.kit.photos.photoPlaybackAddress

/** A photo's file as the server gave it: its bytes, the name to give it, and whether it is the original. */
class DownloadedPhoto(val bytes: ByteArray, val filename: String, val contentType: String, val isOriginal: Boolean)

/**
 * Reads the original file of a photo, or the role asked for (the edited version, a Live Photo's video). When the server
 * says the file is not there, the large preview (or the video preview) stands in for the original, as on the iPhone.
 */
suspend fun Connection.downloadPhotoFile(photo: Photo, photoFileRole: String? = null): DownloadedPhoto {
    val detail = photoDetail(photo.id)
    val file = detail.file(photoFileRole ?: "original")
    if (file != null) {
        val address = photoPlaybackAddress(photo.id, PhotoPlaybackSource.File(file.photoFileRole))
        val response = send(HTTPRequest(address, "GET"))
        if (response.statusCode == 200) {
            return DownloadedPhoto(response.body, file.originalFilename.ifEmpty { photo.filename }, file.contentType, file.photoFileRole == "original")
        }
        if (response.statusCode != 404 && response.statusCode != 403 && response.statusCode != 410) throw PhotoError.FileCouldNotBeRead
    }
    if (photoFileRole != null) throw PhotoError.NothingToSaveYet
    val previewSize = if (photo.isVideo) "video" else "large"
    val preview = detail.photoPreviews.firstOrNull { candidate -> candidate.photoPreviewSize == previewSize }
    if (preview == null && (if (photo.isVideo) photo.videoPath else photo.viewerPreviewPath) == null) throw PhotoError.NothingToSaveYet
    val extension = preview?.let { candidate -> PhotoDownload.fileExtension(candidate.contentType) } ?: if (photo.isVideo) "mp4" else "jpg"
    val address = photoPlaybackAddress(photo.id, PhotoPlaybackSource.Preview(previewSize))
    val response = send(HTTPRequest(address, "GET"))
    if (response.statusCode != 200) throw PhotoError.FileCouldNotBeRead
    return DownloadedPhoto(response.body, photo.filename.substringBeforeLast('.') + "." + extension, preview?.contentType ?: if (photo.isVideo) "video/mp4" else "image/jpeg", false)
}

/** Puts a downloaded photo in the phone's gallery (Pictures or Movies, in a TeaNode folder) and returns where it is. */
fun saveToGallery(context: Context, download: DownloadedPhoto): Uri {
    val isVideo = download.contentType.startsWith("video/")
    val values = ContentValues().apply {
        put(MediaStore.MediaColumns.DISPLAY_NAME, download.filename)
        put(MediaStore.MediaColumns.MIME_TYPE, download.contentType.ifEmpty { if (isVideo) "video/mp4" else "image/jpeg" })
        put(MediaStore.MediaColumns.RELATIVE_PATH, (if (isVideo) Environment.DIRECTORY_MOVIES else Environment.DIRECTORY_PICTURES) + "/TeaNode")
    }
    val collection = if (isVideo) MediaStore.Video.Media.EXTERNAL_CONTENT_URI else MediaStore.Images.Media.EXTERNAL_CONTENT_URI
    val uri = context.contentResolver.insert(collection, values) ?: throw PhotoError.FileCouldNotBeRead
    context.contentResolver.openOutputStream(uri)?.use { output -> output.write(download.bytes) } ?: throw PhotoError.FileCouldNotBeRead
    return uri
}

/** Offers a saved photo to the apps that take one. */
fun sharePhoto(context: Context, uri: Uri, contentType: String) {
    val send = Intent(Intent.ACTION_SEND).apply {
        type = contentType.ifEmpty { "image/*" }
        putExtra(Intent.EXTRA_STREAM, uri)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    context.startActivity(Intent.createChooser(send, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}
