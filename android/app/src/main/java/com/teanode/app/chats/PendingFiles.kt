package com.teanode.app.chats

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import android.provider.OpenableColumns
import com.teanode.kit.chats.SharedFiles
import java.io.ByteArrayOutputStream
import java.util.UUID
import kotlin.math.max

/** Reads what the person chose into files ready to send with a message. */
object PendingFiles {
    /** The longest side a picture is sent at: plenty for the agent to read a receipt or a screen, and a fraction of what a camera takes. */
    private const val LONGEST_SIDE_PIXELS = 2048

    private fun readCapped(context: Context, address: Uri): ByteArray? =
        runCatching { context.contentResolver.openInputStream(address)?.use { stream -> SharedFiles.readCapped(stream) } }.getOrNull()

    /** A picture from the library or the camera, as a JPEG at most [LONGEST_SIDE_PIXELS] on its longest side, whatever it was taken as. */
    fun picture(context: Context, address: Uri, index: Int): PendingFile? {
        val bytes = readCapped(context, address) ?: return null
        // A picture too large to send is not decoded: it is kept as it came so the sheet says it is too large.
        if (bytes.size > SharedFiles.BYTES_MOST) return file(context, address)
        return picture(bytes, index)
    }

    fun picture(bytes: ByteArray, index: Int): PendingFile? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sample = 1
        while (max(bounds.outWidth, bounds.outHeight) / (sample * 2) >= LONGEST_SIDE_PIXELS) sample *= 2
        val decoded = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
        val turned = turnedUpright(decoded, bytes)
        val scale = minOf(1f, LONGEST_SIDE_PIXELS.toFloat() / max(turned.width, turned.height))
        val resized = if (scale < 1f) Bitmap.createScaledBitmap(turned, (turned.width * scale).toInt().coerceAtLeast(1), (turned.height * scale).toInt().coerceAtLeast(1), true) else turned
        val output = ByteArrayOutputStream()
        resized.compress(Bitmap.CompressFormat.JPEG, 80, output)
        return PendingFile(UUID.randomUUID().toString(), "picture-${index + 1}.jpg", "image/jpeg", output.toByteArray(), resized)
    }

    private fun turnedUpright(bitmap: Bitmap, bytes: ByteArray): Bitmap {
        val orientation = runCatching { ExifInterface(bytes.inputStream()).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL) }
            .getOrDefault(ExifInterface.ORIENTATION_NORMAL)
        val degrees = when (orientation) {
            ExifInterface.ORIENTATION_ROTATE_90 -> 90f
            ExifInterface.ORIENTATION_ROTATE_180 -> 180f
            ExifInterface.ORIENTATION_ROTATE_270 -> 270f
            else -> return bitmap
        }
        return Bitmap.createBitmap(bitmap, 0, 0, bitmap.width, bitmap.height, Matrix().apply { postRotate(degrees) }, true)
    }

    /** A file from the document picker, sent as it is. */
    fun file(context: Context, address: Uri): PendingFile? {
        val bytes = readCapped(context, address) ?: return null
        var name = address.lastPathSegment ?: "file"
        context.contentResolver.query(address, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst() && cursor.getString(0) != null) name = cursor.getString(0)
        }
        val contentType = context.contentResolver.getType(address) ?: "application/octet-stream"
        val preview = if (contentType.startsWith("image/")) runCatching { BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = 8 }) }.getOrNull() else null
        return PendingFile(UUID.randomUUID().toString(), name, contentType, bytes, preview)
    }
}
