package com.teanode.kit.chats

/**
 * MarkdownBlock is one block of an answer's Markdown: what the platform's own Markdown reading leaves out, since it
 * reads only inline styles. Each block's text still carries its inline styles, for the screen to read.
 */
sealed interface MarkdownBlock {
    data class Paragraph(val text: String) : MarkdownBlock

    data class Heading(val text: String) : MarkdownBlock

    data class Quote(val text: String) : MarkdownBlock

    data class ListBlock(val items: List<ListItem>) : MarkdownBlock

    data class Code(val text: String) : MarkdownBlock

    /** A table: its header cells, then its rows. */
    data class Table(val header: List<String>, val rows: List<List<String>>) : MarkdownBlock

    data object Rule : MarkdownBlock

    /** A picture on a line of its own, and where it links to if it does. */
    data class Image(val alt: String, val address: String, val link: String?) : MarkdownBlock

    data class ListItem(
        /** "•" for a bullet, or the item's own number with its dot. */
        val marker: String,
        val text: String,
    )

    companion object {
        private val listItemPattern = Regex("""^\s*([-*+]|\d{1,3}[.)])\s+(.*)$""")
        private val tableSeparatorPattern = Regex("""^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$""")
        private val imagePattern = Regex("""^\s*(?:\[)?!\[([^\]]*)\]\(([^)\s]+)\)(?:\]\(([^)\s]+)\))?\s*$""")

        /** The picture a line is, when the line is nothing but a picture. */
        fun imageOf(line: String): MarkdownBlock? {
            val match = imagePattern.find(line) ?: return null
            val link = match.groups[3]?.value
            return Image(match.groupValues[1], match.groupValues[2], link)
        }

        fun cells(line: String): List<String> {
            var trimmed = line.trim()
            if (trimmed.startsWith("|")) trimmed = trimmed.substring(1)
            if (trimmed.endsWith("|")) trimmed = trimmed.dropLast(1)
            return trimmed.split("|").map { cell -> cell.trim() }
        }

        fun isTableSeparator(line: String): Boolean = line.contains("-") && tableSeparatorPattern.containsMatchIn(line)

        /** The blocks of a Markdown text, in order. */
        fun parse(text: String): List<MarkdownBlock> {
            val blocks = mutableListOf<MarkdownBlock>()
            val paragraph = mutableListOf<String>()
            val quote = mutableListOf<String>()
            val items = mutableListOf<ListItem>()
            var code: MutableList<String>? = null
            var tableHeader: List<String>? = null
            val tableRows = mutableListOf<List<String>>()

            fun flush() {
                if (paragraph.isNotEmpty()) blocks.add(Paragraph(paragraph.joinToString("\n")))
                if (quote.isNotEmpty()) blocks.add(Quote(quote.joinToString("\n")))
                if (items.isNotEmpty()) blocks.add(ListBlock(items.toList()))
                paragraph.clear()
                quote.clear()
                items.clear()
            }

            fun flushTable() {
                tableHeader?.let { header -> blocks.add(Table(header, tableRows.toList())) }
                tableHeader = null
                tableRows.clear()
            }

            val allLines = HiddenMarkers.removed(text).split("\n")
            var index = 0
            while (index < allLines.size) {
                val line = allLines[index]
                index += 1
                val trimmed = line.trim()
                if (tableHeader != null) {
                    if (code == null && trimmed.contains("|")) {
                        tableRows.add(cells(trimmed))
                        continue
                    }
                    flushTable()
                }
                if (code == null && trimmed.contains("|") && index < allLines.size && isTableSeparator(allLines[index])) {
                    flush()
                    tableHeader = cells(trimmed)
                    index += 1
                    continue
                }
                if (code == null && listOf("---", "***", "___").any { rule -> trimmed.startsWith(rule) && trimmed.all { character -> character == trimmed.first() || character == ' ' } }) {
                    flush()
                    blocks.add(Rule)
                    continue
                }
                if (trimmed.startsWith("```")) {
                    val fenced = code
                    if (fenced != null) {
                        blocks.add(Code(fenced.joinToString("\n")))
                        code = null
                    } else {
                        flush()
                        code = mutableListOf()
                    }
                    continue
                }
                if (code != null) {
                    code.add(line)
                    continue
                }
                if (trimmed.isEmpty()) {
                    flush()
                    continue
                }
                val picture = imageOf(trimmed)
                if (picture != null) {
                    flush()
                    blocks.add(picture)
                    continue
                }
                if (trimmed.startsWith("#")) {
                    flush()
                    blocks.add(Heading(trimmed.trimStart('#').trim()))
                    continue
                }
                if (trimmed.startsWith(">")) {
                    if (quote.isEmpty()) flush()
                    quote.add(trimmed.drop(1).trim())
                    continue
                }
                val match = listItemPattern.find(line)
                if (match != null) {
                    if (items.isEmpty()) flush()
                    val marker = match.groupValues[1]
                    items.add(ListItem(if (marker.first().isDigit()) marker else "•", match.groupValues[2]))
                    continue
                }
                if (items.isNotEmpty() && (line.firstOrNull() == ' ' || line.firstOrNull() == '\t')) {
                    val last = items.removeAt(items.size - 1)
                    items.add(last.copy(text = last.text + " " + trimmed))
                    continue
                }
                if (quote.isNotEmpty() || items.isNotEmpty()) flush()
                paragraph.add(line)
            }
            code?.let { fenced -> blocks.add(Code(fenced.joinToString("\n"))) }
            flushTable()
            flush()
            return blocks
        }
    }
}
