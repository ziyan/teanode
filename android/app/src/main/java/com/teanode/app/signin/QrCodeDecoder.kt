package com.teanode.app.signin

import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.MultiFormatReader
import com.google.zxing.NotFoundException
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.HybridBinarizer

/** QrCodeDecoder reads the text of a QR code out of a camera frame's brightness values. */
class QrCodeDecoder {
    private val reader = MultiFormatReader().apply {
        setHints(mapOf(DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE), DecodeHintType.TRY_HARDER to true))
    }

    /**
     * @param luminance one byte of brightness per pixel, [rowStrideBytes] apart row to row, as the camera's first
     *   image plane gives them.
     * @return the code's text, or null when the frame holds none.
     */
    fun decode(luminance: ByteArray, widthPixels: Int, heightPixels: Int, rowStrideBytes: Int = widthPixels): String? {
        val source = PlanarYUVLuminanceSource(luminance, rowStrideBytes, heightPixels, 0, 0, widthPixels, heightPixels, false)
        return try {
            reader.decodeWithState(BinaryBitmap(HybridBinarizer(source))).text
        } catch (_: NotFoundException) {
            null
        } finally {
            reader.reset()
        }
    }
}
