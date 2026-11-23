package com.teanode.app.coding

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp

/**
 * The agent's words, which it writes as Markdown: paragraphs, lists, headings (no larger than the body's heading), fenced
 * code, inline code, bold and tables. A table is drawn as columns of its cells. The chat's own renderer is the Chats part's.
 */
@Composable
internal fun CodingMarkdown(text: String) {
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (block in markdownBlocks(text)) {
            when (block) {
                is MarkdownBlock.Paragraph -> Text(inlineMarkdown(block.text), style = MaterialTheme.typography.bodyLarge)
                is MarkdownBlock.Heading -> Text(inlineMarkdown(block.text), style = MaterialTheme.typography.titleSmall)
                is MarkdownBlock.Bullet -> Row {
                    Text(if (block.number != null) "${block.number}." else "•", modifier = Modifier.padding(start = 4.dp, end = 8.dp), style = MaterialTheme.typography.bodyLarge)
                    Text(inlineMarkdown(block.text), style = MaterialTheme.typography.bodyLarge)
                }
                is MarkdownBlock.Code -> Text(
                    block.text,
                    modifier = Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(8.dp)).horizontalScroll(rememberScrollState()).padding(10.dp),
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace), softWrap = false,
                )
                is MarkdownBlock.Table -> Column(
                    Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(8.dp)).horizontalScroll(rememberScrollState()).padding(10.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    block.rows.forEachIndexed { index, cells ->
                        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                            cells.forEachIndexed { column, cell ->
                                Text(
                                    inlineMarkdown(cell), modifier = Modifier.padding(end = 4.dp), softWrap = false,
                                    style = MaterialTheme.typography.bodySmall.copy(fontWeight = if (index == 0) FontWeight.Bold else FontWeight.Normal),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

internal sealed interface MarkdownBlock {
    data class Paragraph(val text: String) : MarkdownBlock
    data class Heading(val text: String) : MarkdownBlock
    data class Bullet(val text: String, val number: Int?) : MarkdownBlock
    data class Code(val text: String) : MarkdownBlock
    data class Table(val rows: List<List<String>>) : MarkdownBlock
}

internal fun markdownBlocks(text: String): List<MarkdownBlock> {
    val blocks = mutableListOf<MarkdownBlock>()
    val lines = text.split("\n")
    var index = 0
    val paragraph = StringBuilder()
    fun endParagraph() {
        if (paragraph.isNotBlank()) blocks.add(MarkdownBlock.Paragraph(paragraph.toString().trim()))
        paragraph.clear()
    }
    while (index < lines.size) {
        val line = lines[index]
        val trimmed = line.trim()
        when {
            trimmed.startsWith("```") -> {
                endParagraph()
                val code = mutableListOf<String>()
                index++
                while (index < lines.size && !lines[index].trim().startsWith("```")) {
                    code.add(lines[index])
                    index++
                }
                blocks.add(MarkdownBlock.Code(code.joinToString("\n")))
            }
            trimmed.startsWith("|") && index + 1 < lines.size && lines[index + 1].trim().matches(Regex("""\|?[\s:|-]+\|[\s:|-]*""")) -> {
                endParagraph()
                val rows = mutableListOf<List<String>>()
                rows.add(tableCells(trimmed))
                index += 2
                while (index < lines.size && lines[index].trim().startsWith("|")) {
                    rows.add(tableCells(lines[index].trim()))
                    index++
                }
                blocks.add(MarkdownBlock.Table(rows))
                continue
            }
            trimmed.isEmpty() -> endParagraph()
            trimmed.startsWith("#") -> {
                endParagraph()
                blocks.add(MarkdownBlock.Heading(trimmed.trimStart('#').trim()))
            }
            trimmed.startsWith("- ") || trimmed.startsWith("* ") -> {
                endParagraph()
                blocks.add(MarkdownBlock.Bullet(trimmed.drop(2), null))
            }
            Regex("""^\d+\.\s""").containsMatchIn(trimmed) -> {
                endParagraph()
                val number = trimmed.substringBefore('.').toInt()
                blocks.add(MarkdownBlock.Bullet(trimmed.substringAfter('.').trim(), number))
            }
            else -> {
                if (paragraph.isNotEmpty()) paragraph.append('\n')
                paragraph.append(trimmed)
            }
        }
        index++
    }
    endParagraph()
    return blocks
}

private fun tableCells(line: String): List<String> = line.trim().trim('|').split("|").map { cell -> cell.trim() }

/** Bold, italic and code spans of one line. */
internal fun inlineMarkdown(text: String): AnnotatedString = buildAnnotatedString {
    var index = 0
    while (index < text.length) {
        when {
            text.startsWith("**", index) && text.indexOf("**", index + 2) > 0 -> {
                val end = text.indexOf("**", index + 2)
                withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(text.substring(index + 2, end)) }
                index = end + 2
            }
            text[index] == '`' && text.indexOf('`', index + 1) > 0 -> {
                val end = text.indexOf('`', index + 1)
                withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = androidx.compose.ui.graphics.Color(0x22888888))) { append(text.substring(index + 1, end)) }
                index = end + 1
            }
            else -> {
                append(text[index])
                index++
            }
        }
    }
}
