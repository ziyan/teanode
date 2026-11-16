package com.teanode.kit.chats

import java.text.BreakIterator

/**
 * EmojiReaction tells emoji from everything else typed on the emoji keyboard, so a reaction can be any one of them.
 * The server takes one or two emoji of at most 32 bytes (`ValidateReactionEmoji`); a person picks one, so the first
 * emoji typed is the reaction.
 */
object EmojiReaction {
    /** The most bytes the server stores for one reaction. */
    const val BYTES_MOST = 32

    private const val TEXT_PRESENTATION_SELECTOR = 0xFE0E
    private const val KEYCAP_MARK = 0x20E3
    private val regionalIndicators = 0x1F1E6..0x1F1FF

    /** Symbols below U+2BFF that are drawn as a picture by default. */
    private val pictureByDefault = listOf(
        0x231A..0x231B, 0x23E9..0x23EC, 0x23F0..0x23F0, 0x23F3..0x23F3, 0x25FD..0x25FE, 0x2614..0x2615, 0x2648..0x2653, 0x267F..0x267F,
        0x2693..0x2693, 0x26A1..0x26A1, 0x26AA..0x26AB, 0x26BD..0x26BE, 0x26C4..0x26C5, 0x26CE..0x26CE, 0x26D4..0x26D4, 0x26EA..0x26EA,
        0x26F2..0x26F3, 0x26F5..0x26F5, 0x26FA..0x26FA, 0x26FD..0x26FD, 0x2705..0x2705, 0x270A..0x270B, 0x2728..0x2728, 0x274C..0x274C,
        0x274E..0x274E, 0x2753..0x2755, 0x2757..0x2757, 0x2795..0x2797, 0x27B0..0x27B0, 0x27BF..0x27BF, 0x2B1B..0x2B1C, 0x2B50..0x2B50,
        0x2B55..0x2B55,
    )

    /** Symbols that are text by default and become a picture with the emoji selector after them. */
    private val pictureWithSelector = listOf(
        0x00A9..0x00A9, 0x00AE..0x00AE, 0x203C..0x203C, 0x2049..0x2049, 0x2122..0x2122, 0x2139..0x2139, 0x2194..0x2199, 0x21A9..0x21AA,
        0x2328..0x2328, 0x23CF..0x23CF, 0x23ED..0x23EF, 0x23F1..0x23F2, 0x23F8..0x23FA, 0x24C2..0x24C2, 0x25AA..0x25AB, 0x25B6..0x25B6,
        0x25C0..0x25C0, 0x25FB..0x25FC, 0x2600..0x2604, 0x260E..0x260E, 0x2611..0x2611, 0x2618..0x2618, 0x261D..0x261D, 0x2620..0x2620,
        0x2622..0x2623, 0x2626..0x2626, 0x262A..0x262A, 0x262E..0x262F, 0x2638..0x263A, 0x2640..0x2640, 0x2642..0x2642, 0x265F..0x2660,
        0x2663..0x2663, 0x2665..0x2666, 0x2668..0x2668, 0x267B..0x267B, 0x267E..0x267E, 0x2692..0x2692, 0x2694..0x2697, 0x2699..0x2699,
        0x269B..0x269C, 0x26A0..0x26A0, 0x26A7..0x26A7, 0x26B0..0x26B1, 0x26C8..0x26C8, 0x26CF..0x26CF, 0x26D1..0x26D1, 0x26D3..0x26D3,
        0x26E9..0x26E9, 0x26F0..0x26F1, 0x26F4..0x26F4, 0x26F7..0x26F9, 0x2702..0x2702, 0x2708..0x2709, 0x270C..0x270D, 0x270F..0x270F,
        0x2712..0x2712, 0x2714..0x2714, 0x2716..0x2716, 0x271D..0x271D, 0x2721..0x2721, 0x2733..0x2734, 0x2744..0x2744, 0x2747..0x2747,
        0x2763..0x2764, 0x27A1..0x27A1, 0x2934..0x2935, 0x2B05..0x2B07, 0x3030..0x3030, 0x303D..0x303D, 0x3297..0x3297, 0x3299..0x3299,
    )

    private fun isPictureByDefault(codePoint: Int): Boolean =
        codePoint in 0x1F000..0x1FAFF && codePoint !in regionalIndicators || pictureByDefault.any { range -> codePoint in range }

    private fun isEmojiBase(codePoint: Int): Boolean = isPictureByDefault(codePoint) || pictureWithSelector.any { range -> codePoint in range }

    /**
     * Whether one character, as a person sees it, is an emoji: a picture drawn in color by default, or a base the emoji
     * selector, a joiner, a skin tone or a flag's second half turns into one. A digit, a letter or a symbol that is
     * drawn as text is not.
     */
    fun isEmoji(character: String): Boolean {
        val codePoints = character.codePoints().toArray()
        val first = codePoints.firstOrNull() ?: return false
        // The selector that asks for the plain-text drawing says it is not.
        if (codePoints.contains(TEXT_PRESENTATION_SELECTOR)) return false
        // A regional indicator alone is a letter in a box; two make a flag.
        if (first in regionalIndicators) return codePoints.size == 2 && codePoints[1] in regionalIndicators
        // A digit, # or * is an emoji only inside its keycap.
        if (Character.isDigit(first) || first == '#'.code || first == '*'.code) return codePoints.contains(KEYCAP_MARK)
        if (isPictureByDefault(first)) return true
        return isEmojiBase(first) && codePoints.size > 1
    }

    /** The first emoji in what was typed, or null when there is none or it is longer than the server takes. */
    fun firstEmoji(text: String): String? {
        val breaks = BreakIterator.getCharacterInstance()
        breaks.setText(text)
        var start = breaks.first()
        var end = breaks.next()
        while (end != BreakIterator.DONE) {
            val character = text.substring(start, end)
            if (isEmoji(character)) return if (character.toByteArray(Charsets.UTF_8).size <= BYTES_MOST) character else null
            start = end
            end = breaks.next()
        }
        return null
    }
}
