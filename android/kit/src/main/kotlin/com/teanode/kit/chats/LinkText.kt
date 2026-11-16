package com.teanode.kit.chats

/**
 * LinkText finds the web links in a message's words and makes them followable: shown as their short text, opened
 * without their tracking parameters. It is the same work as linkText.ts in the dashboard. The screen draws the result:
 * what is here is where the links are.
 */
object LinkText {
    /**
     * A link runs to the next white space, an angle bracket, a quote, or a character of the CJK and full-width
     * punctuation blocks, so that a link written straight against Chinese or Japanese words does not carry them off.
     */
    private val barePattern = Regex("""https?://[^\s<>"'`\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]+""", RegexOption.IGNORE_CASE)

    private val hostPattern = Regex("""^https?://[^/?#]""", RegexOption.IGNORE_CASE)

    private val closers = mapOf(')' to '(', ']' to '[', '}' to '{')

    /**
     * The link with the punctuation that ends a sentence taken off its end, and a closing bracket that has no opening
     * one in the link: the full stop after "see https://example.com/a." and the bracket in "(see
     * https://example.com/a)" are the writer's, a bracket in "https://example.com/wiki/Thing_(film)" is the link's.
     */
    fun trimmedEnd(link: String): String {
        var text = link
        while (text.isNotEmpty()) {
            val last = text.last()
            if (last in ".,;:!?*_~") {
                text = text.dropLast(1)
                continue
            }
            val opener = closers[last]
            if (opener != null && text.count { character -> character == last } > text.count { character -> character == opener }) {
                text = text.dropLast(1)
                continue
            }
            break
        }
        return text
    }

    /** One link in a text: where it is (the characters from `start` up to `endExclusive`), and the link itself. */
    data class Span(val start: Int, val endExclusive: Int, val link: String)

    /** Where each web link is in the text, in order. */
    fun links(text: String): List<Span> {
        val spans = mutableListOf<Span>()
        for (match in barePattern.findAll(text)) {
            val link = trimmedEnd(match.value)
            // A link needs a host: "https://" and nothing after it is not one.
            if (!hostPattern.containsMatchIn(link)) continue
            spans.add(Span(match.range.first, match.range.first + link.length, link))
        }
        return spans
    }

    private val fencePattern = Regex("""```[\s\S]*?(?:```|\z)""")
    private val codePattern = Regex("""`[^`]*`""")
    private val picturePattern = Regex("""!\[[^\]]*\]\([^)]*\)""")

    /**
     * The first web link in the words of a message, or null. In words written as Markdown, code and pictures are not
     * links to show a card for, so they are left out first.
     */
    fun firstLink(text: String, isMarkdown: Boolean): String? {
        var words = text
        if (isMarkdown) {
            for (pattern in listOf(fencePattern, codePattern, picturePattern)) words = pattern.replace(words, " ")
        }
        return links(words).firstOrNull()?.link
    }
}
