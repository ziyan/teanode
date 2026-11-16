package com.teanode.app.photos

import android.content.ContentValues
import android.content.Context
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

/** A photo's file as the server gave it, in a file of the folder it was read into: the name to give it, and whether it is the original. */
class DownloadedPhoto(val file: java.io.File, val filename: String, val contentType: String, val isOriginal: Boolean)

/**
 * Reads the original file of a photo, or the role asked for (the edited version, a Live Photo's video), straight into a
 * file of [folder] as it arrives, never into memory: a video may be hundreds of megabytes. When the server says the file is
 * not there, the large preview (or the video preview) stands in for the original, as on the iPhone.
 */
suspend fun Connection.downloadPhotoFile(photo: Photo, folder: java.io.File, photoFileRole: String? = null): DownloadedPhoto {
    folder.mkdirs()
    suspend fun fetch(address: String): java.io.File? {
        val target = java.io.File.createTempFile("download-", ".part", folder)
        val response = download(HTTPRequest(address, "GET"), target)
        if (response.statusCode == 200) return target
        target.delete()
        if (response.statusCode != 404 && response.statusCode != 403 && response.statusCode != 410) throw PhotoError.FileCouldNotBeRead
        return null
    }
    val detail = photoDetail(photo.id)
    val file = detail.file(photoFileRole ?: "original")
    if (file != null) {
        val target = fetch(photoPlaybackAddress(photo.id, PhotoPlaybackSource.File(file.photoFileRole)))
        if (target != null) return DownloadedPhoto(target, file.originalFilename.ifEmpty { photo.filename }, file.contentType, file.photoFileRole == "original")
    }
    if (photoFileRole != null) throw PhotoError.NothingToSaveYet
    val previewSize = if (photo.isVideo) "video" else "large"
    val preview = detail.photoPreviews.firstOrNull { candidate -> candidate.photoPreviewSize == previewSize }
    if (preview == null && (if (photo.isVideo) photo.videoPath else photo.viewerPreviewPath) == null) throw PhotoError.NothingToSaveYet
    val extension = preview?.let { candidate -> PhotoDownload.fileExtension(candidate.contentType) } ?: if (photo.isVideo) "mp4" else "jpg"
    val target = fetch(photoPlaybackAddress(photo.id, PhotoPlaybackSource.Preview(previewSize))) ?: throw PhotoError.FileCouldNotBeRead
    return DownloadedPhoto(target, photo.filename.substringBeforeLast('.') + "." + extension, preview?.contentType ?: if (photo.isVideo) "video/mp4" else "image/jpeg", false)
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
    context.contentResolver.openOutputStream(uri)?.use { output -> download.file.inputStream().use { input -> input.copyTo(output) } } ?: throw PhotoError.FileCouldNotBeRead
    return uri
}
