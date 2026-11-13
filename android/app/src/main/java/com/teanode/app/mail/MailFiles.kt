package com.teanode.app.mail

import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.provider.MediaStore
import android.webkit.MimeTypeMap
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError

/**
 * Reads a file of the server (an attachment, a message's original) as the person and opens it in an app that shows it.
 * The file is saved to the phone's Downloads folder first, which needs no permission, and opened from there.
 */
suspend fun openServerFile(context: Context, connection: Connection, path: String, name: String) {
    val response = connection.get(path)
    if (response.statusCode != 200) throw TeaNodeError.Refused("The server could not give that file (${response.statusCode}).")
    val extension = name.substringAfterLast('.', "").lowercase()
    val mimeType = MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension) ?: if (extension == "eml") "message/rfc822" else "application/octet-stream"
    val values = ContentValues().apply {
        put(MediaStore.Downloads.DISPLAY_NAME, name)
        put(MediaStore.Downloads.MIME_TYPE, mimeType)
        put(MediaStore.Downloads.IS_PENDING, 1)
    }
    val resolver = context.contentResolver
    val address = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) ?: throw TeaNodeError.Refused("The phone could not save that file.")
    resolver.openOutputStream(address)?.use { output -> output.write(response.body) }
    resolver.update(address, ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }, null, null)
    val view = Intent(Intent.ACTION_VIEW).setDataAndType(address, mimeType).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    try {
        context.startActivity(view)
    } catch (_: android.content.ActivityNotFoundException) {
        throw TeaNodeError.Refused("No app on this phone opens that kind of file. It is in your Downloads.")
    }
}
