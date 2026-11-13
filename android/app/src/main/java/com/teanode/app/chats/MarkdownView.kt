package com.teanode.app.chats

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.teanode.app.R
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.ChatMention
import com.teanode.kit.chats.ChatMentionWord
import com.teanode.kit.chats.LinkClean
import com.teanode.kit.chats.LinkText
import com.teanode.kit.chats.MarkdownBlock
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** The colors the inline reading of a text needs, taken from the theme once. */
class InlineStyle(val linkColor: Color, val codeBackground: Color, val mentionBackground: Color)

@Composable
fun rememberInlineStyle(isOnAccent: Boolean = false): InlineStyle {
    val scheme = MaterialTheme.colorScheme
    return InlineStyle(
        // The accent on the gray of somebody else's, the words' own color on the person's, whose accent tint would make an accent link too faint to read.
        linkColor = if (isOnAccent) scheme.onPrimaryContainer else scheme.primary,
        codeBackground = scheme.onSurface.copy(alpha = 0.08f),
        mentionBackground = Color(0xFFFFD54F).copy(alpha = 0.35f),
    )
}

/**
 * The inline styles of a text, read the way the dashboard reads them: bold, italic, struck, code, `[words](address)`
 * links, and a bare address shown short. Every address opens without its tracking parameters. Code is left as it is.
 * The words a message of a chat mentions people by are marked.
 */
fun inlineText(text: String, style: InlineStyle, mentions: List<ChatMentionWord> = emptyList(), isPlain: Boolean = false): AnnotatedString {
    val built = buildAnnotatedString { if (isPlain) appendPlain(text, style) else appendInline(text, style) }
    if (mentions.isEmpty() || !built.text.contains('@')) return built
    val ranges = ChatMention.ranges(mentions.map { mention -> mention.word }, built.text)
    if (ranges.isEmpty()) return built
    return buildAnnotatedString {
        append(built)
        for (range in ranges) {
            val said = built.text.substring(range.first + 1, range.last + 1).lowercase()
            addStyle(SpanStyle(fontWeight = FontWeight.SemiBold), range.first, range.last + 1)
            if (mentions.any { mention -> mention.isReader && mention.word.lowercase() == said }) {
                addStyle(SpanStyle(background = style.mentionBackground), range.first, range.last + 1)
            }
        }
    }
}

private fun AnnotatedString.Builder.appendLink(address: String, style: InlineStyle, words: AnnotatedString.Builder.() -> Unit) {
    val styles = TextLinkStyles(SpanStyle(color = style.linkColor, textDecoration = TextDecoration.Underline))
    withLink(LinkAnnotation.Url(LinkClean.cleaned(address), styles)) { words() }
}

/** Words that are a person's own, typed: only the addresses in them are followable. */
private fun AnnotatedString.Builder.appendPlain(text: String, style: InlineStyle) {
    var at = 0
    for (span in LinkText.links(text)) {
        append(text.substring(at, span.start))
        appendLink(span.link, style) { append(LinkClean.displayText(span.link)) }
        at = span.endExclusive
    }
    append(text.substring(at))
}

private val boundaryCharacters = " \t\n.,;:!?()[]{}\"'"

private fun AnnotatedString.Builder.appendInline(text: String, style: InlineStyle) {
    var index = 0
    val plain = StringBuilder()
    fun flushPlain() {
        if (plain.isNotEmpty()) {
            appendPlain(plain.toString(), style)
            plain.clear()
        }
    }
    while (index < text.length) {
        val character = text[index]
        // A backslash makes the next character itself.
        if (character == '\\' && index + 1 < text.length && text[index + 1] in "\\`*_{}[]()#+-.!~|>") {
            plain.append(text[index + 1])
            index += 2
            continue
        }
        if (character == '`') {
            val end = text.indexOf('`', index + 1)
            if (end > index) {
                flushPlain()
                withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = style.codeBackground)) { append(text.substring(index + 1, end)) }
                index = end + 1
                continue
            }
        }
        if (character == '[') {
            val closing = text.indexOf("](", index + 1)
            val end = if (closing > 0) text.indexOf(')', closing + 2) else -1
            if (closing > 0 && end > closing) {
                val address = text.substring(closing + 2, end).trim()
                if (address.startsWith("http://", ignoreCase = true) || address.startsWith("https://", ignoreCase = true)) {
                    flushPlain()
                    appendLink(address, style) { appendInline(text.substring(index + 1, closing), style) }
                    index = end + 1
                    continue
                }
                // A link to something inside the app (a mail, a page) is its words.
                flushPlain()
                appendInline(text.substring(index + 1, closing), style)
                index = end + 1
                continue
            }
        }
        val marker = when {
            text.startsWith("**", index) -> "**"
            text.startsWith("__", index) -> "__"
            text.startsWith("~~", index) -> "~~"
            character == '*' -> "*"
            character == '_' && (index == 0 || text[index - 1] in boundaryCharacters) -> "_"
            else -> null
        }
        if (marker != null) {
            val end = text.indexOf(marker, index + marker.length)
            val inner = if (end > index + marker.length) text.substring(index + marker.length, end) else null
            if (inner != null && inner.isNotBlank() && !inner.startsWith(' ')) {
                flushPlain()
                val spanStyle = when (marker) {
                    "**", "__" -> SpanStyle(fontWeight = FontWeight.Bold)
                    "~~" -> SpanStyle(textDecoration = TextDecoration.LineThrough)
                    else -> SpanStyle(fontStyle = FontStyle.Italic)
                }
                withStyle(spanStyle) { appendInline(inner, style) }
                index = end + marker.length
                continue
            }
        }
        plain.append(character)
        index += 1
    }
    flushPlain()
}

/**
 * MarkdownView draws an answer's Markdown: its blocks, each with its inline styles. Headings are no larger than a
 * headline.
 */
@Composable
fun MarkdownView(
    text: String,
    modifier: Modifier = Modifier,
    mentions: List<ChatMentionWord> = emptyList(),
    conversationId: String? = null,
) {
    val style = rememberInlineStyle()
    val blocks = remember(text) { MarkdownBlock.parse(text) }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (block in blocks) {
            when (block) {
                is MarkdownBlock.Paragraph -> Text(inlineText(block.text, style, mentions), style = MaterialTheme.typography.bodyLarge)
                is MarkdownBlock.Heading -> Text(inlineText(block.text, style, mentions), style = MaterialTheme.typography.titleMedium)
                is MarkdownBlock.Quote -> Row(Modifier.fillMaxWidth()) {
                    Box(Modifier.width(3.dp).heightIn(min = 20.dp).clip(RoundedCornerShape(2.dp)).background(MaterialTheme.colorScheme.outlineVariant))
                    Text(
                        inlineText(block.text, style, mentions), color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 8.dp), style = MaterialTheme.typography.bodyLarge,
                    )
                }
                is MarkdownBlock.ListBlock -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    for (item in block.items) {
                        Row {
                            Text(item.marker, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(24.dp), style = MaterialTheme.typography.bodyLarge)
                            Text(inlineText(item.text, style, mentions), style = MaterialTheme.typography.bodyLarge)
                        }
                    }
                }
                is MarkdownBlock.Code -> CodeBlock(block.text)
                is MarkdownBlock.Table -> Column(Modifier.horizontalScroll(rememberScrollState())) {
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        for (cell in block.header) Text(inlineText(cell, style, mentions), fontWeight = FontWeight.Bold, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.width(110.dp))
                    }
                    HorizontalDivider(Modifier.padding(vertical = 4.dp).width((block.header.size * 124).dp))
                    for (row in block.rows) {
                        Row(horizontalArrangement = Arrangement.spacedBy(14.dp), modifier = Modifier.padding(vertical = 2.dp)) {
                            for (cell in row) Text(inlineText(cell, style, mentions), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.width(110.dp))
                        }
                    }
                }
                MarkdownBlock.Rule -> HorizontalDivider()
                is MarkdownBlock.Image -> AnswerPicture(block.alt, block.address, block.link, conversationId)
            }
        }
    }
}

/** CodeBlock is code or a tool's JSON: monospaced, scrolling when long, and copied with a tap. */
@Composable
fun CodeBlock(text: String, modifier: Modifier = Modifier, maxHeightDp: Int = 400) {
    val clipboard = LocalClipboardManager.current
    var isCopied by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    Box(modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.onSurface.copy(alpha = 0.07f))) {
        SelectionContainer {
            Box(Modifier.heightIn(max = maxHeightDp.dp).verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
                Text(text, fontFamily = FontFamily.Monospace, fontSize = 12.sp, lineHeight = 16.sp, modifier = Modifier.padding(start = 10.dp, top = 10.dp, bottom = 10.dp, end = 40.dp))
            }
        }
        IconButton(
            onClick = {
                clipboard.setText(AnnotatedString(text))
                isCopied = true
                scope.launch {
                    delay(900)
                    isCopied = false
                }
            },
            modifier = Modifier.align(Alignment.TopEnd),
        ) {
            Icon(
                if (isCopied) Icons.Outlined.Check else Icons.Outlined.ContentCopy,
                contentDescription = stringResource(if (isCopied) R.string.chats_copied else R.string.chats_copy),
                modifier = Modifier.padding(2.dp),
            )
        }
    }
}

/** A tool's arguments or result made readable: the untrusted-data fence a result is wrapped in taken off, and JSON indented. */
fun tidyToolText(text: String): String {
    var trimmed = text.trim()
    if (trimmed.startsWith("<untrusted-data>") && trimmed.endsWith("</untrusted-data>")) {
        trimmed = trimmed.removePrefix("<untrusted-data>").removeSuffix("</untrusted-data>").trim()
    }
    val parsed = try {
        TeaNodeJson.parseToJsonElement(trimmed)
    } catch (_: Exception) {
        return trimmed
    }
    if (parsed is JsonPrimitive) return trimmed
    if (parsed !is JsonObject && parsed !is JsonArray) return trimmed
    return prettyJson(parsed, 0)
}

private fun prettyJson(element: kotlinx.serialization.json.JsonElement, depth: Int): String {
    val indent = "  ".repeat(depth)
    val inner = "  ".repeat(depth + 1)
    return when (element) {
        is JsonObject -> if (element.isEmpty()) "{}" else element.entries.sortedBy { entry -> entry.key }.joinToString(",\n", "{\n", "\n$indent}") { entry ->
            "$inner\"${entry.key}\": ${prettyJson(entry.value, depth + 1)}"
        }
        is JsonArray -> if (element.isEmpty()) "[]" else element.joinToString(",\n", "[\n", "\n$indent]") { item -> inner + prettyJson(item, depth + 1) }
        else -> element.toString()
    }
}
