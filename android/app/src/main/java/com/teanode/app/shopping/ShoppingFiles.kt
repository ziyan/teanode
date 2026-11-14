package com.teanode.app.shopping

import android.content.Context
import android.graphics.Bitmap
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.R
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.finance.financeReceipts
import com.teanode.kit.finance.readReceipt
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream

/** A picture or file waiting to be uploaded. */
internal class ShoppingPendingFile(val name: String, val contentType: String, val data: ByteArray)

internal fun Context.readPendingFile(uri: Uri, name: String): ShoppingPendingFile {
    val type = contentResolver.getType(uri) ?: "image/jpeg"
    val data = contentResolver.openInputStream(uri)?.use { stream -> stream.readBytes() } ?: ByteArray(0)
    return ShoppingPendingFile(name, type, data)
}

internal fun Bitmap.toPendingFile(name: String): ShoppingPendingFile {
    val stream = ByteArrayOutputStream()
    compress(Bitmap.CompressFormat.JPEG, 90, stream)
    return ShoppingPendingFile(name, "image/jpeg", stream.toByteArray())
}

/**
 * Uploads a picture of a receipt, has the agent read it, and waits for it to be kept; the sheet that says what it did
 * to the lists then opens. A screen that offers to scan a receipt holds one.
 */
internal class ShoppingReceiptReader(
    private val connection: Connection,
    private val scope: CoroutineScope,
    private val snackbar: SnackbarController,
    private val say: (String) -> Unit,
    private val context: Context,
) {
    /** What the reading is doing, null when none is going on. */
    var readingText by mutableStateOf<String?>(null)
        private set

    /** The receipt whose effect on the lists a sheet shows. */
    var target by mutableStateOf<String?>(null)

    /** Waits every four seconds, two minutes at most, as Finance waits. */
    fun scan(file: ShoppingPendingFile) {
        readingText = context.getString(R.string.shopping_receipt_reading)
        say(context.getString(R.string.shopping_receipt_being_read))
        scope.launch {
            try {
                val attachmentId = connection.upload(file.name, file.contentType, file.data)
                connection.readReceipt(attachmentId = attachmentId)
                repeat(30) {
                    delay(4_000)
                    val recent = try {
                        connection.financeReceipts(limit = 20).financeReceipts.filterNotNull()
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (_: Throwable) {
                        emptyList()
                    }
                    val kept = recent.firstOrNull { receipt -> receipt.agentAttachmentId == attachmentId }
                    if (kept != null) {
                        readingText = null
                        target = kept.id
                        return@launch
                    }
                }
                readingText = null
                say(context.getString(R.string.shopping_receipt_still_reading))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                readingText = null
                snackbar.showError(failure)
            }
        }
    }
}
