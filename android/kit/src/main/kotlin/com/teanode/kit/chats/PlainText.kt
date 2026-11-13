package com.teanode.kit.chats

/**
 * HiddenMarkers are the HTML comments an answer carries for the dashboard and the app to read and never to show: the
 * suggested replies it ends with, and any other. The server takes them off wherever it sends an answer as plain text
 * (`models.StripHiddenMarkers`); this is the same for what the app puts on a screen or says aloud outside the chat.
 */
object HiddenMarkers {
    /** A fenced code block, a code span, or a comment, whole or cut off by the end of the text. Code is matched so that a comment shown as an example inside it is left alone. */
    private val pattern = Regex("""```[\s\S]*?(?:```|\z)|`[^`\n]*`|<!--[\s\S]*?(?:-->|\z)""")

    /** The text without its comments. */
    fun removed(text: String): String = pattern.replace(text) { match -> if (match.value.startsWith("<!--")) "" else match.value }
}

/**
 * PlainText is an answer written in Markdown as a surface that draws none shows it: a widget, a line in a list.
 * Nothing hidden, a link or a picture as its words, and no bold or code marks (`models.PlainText`).
 */
object PlainText {
    fun of(markdown: String): String {
        var text = HiddenMarkers.removed(markdown)
        text = text.replace(Regex("""!?\[([^\]]*)\]\([^)]*\)"""), "$1")
        for (mark in listOf("**", "__", "`")) text = text.replace(mark, "")
        return text.trim()
    }
}
