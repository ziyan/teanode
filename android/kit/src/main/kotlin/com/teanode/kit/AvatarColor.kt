package com.teanode.kit

/**
 * AvatarColor picks which of the twelve colors a person without a picture is drawn on. The server chooses one for
 * every person from a hash of their id and sends it as `avatarColorIndex`; this is the same function, for a screen
 * that shows someone before the server has said, and so the app and the dashboard cannot disagree.
 */
object AvatarColor {
    /** How many colors there are. */
    const val COLOR_COUNT = 12

    /** An FNV-1a hash of the UTF-8 bytes of the text, modulo the color count. */
    fun index(text: String): Int {
        var hash = 0x811c9dc5L
        for (byte in text.toByteArray(Charsets.UTF_8)) {
            hash = hash xor (byte.toLong() and 0xFF)
            hash = (hash * 0x01000193L) and 0xFFFFFFFFL
        }
        return (hash % COLOR_COUNT).toInt()
    }

    /**
     * The color to draw: the one the server chose when it sent a usable index, otherwise the one worked out from the
     * text (the person's id, or their name when only that is known).
     */
    fun index(serverIndex: Int?, fallbackText: String): Int =
        if (serverIndex != null && serverIndex in 0 until COLOR_COUNT) serverIndex else index(fallbackText)
}
