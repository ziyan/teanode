package com.teanode.app

import java.io.File
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Rules for what a screen reader is told, checked over the source so a new screen cannot slip past them. */
class AccessibilityRulesTest {
    private val sources = File("src/main/java").walkTopDown().filter { file -> file.extension == "kt" && !file.path.contains("/widgets/") }.toList()

    private fun callAt(text: String, start: Int): String {
        var depth = 0
        var position = text.indexOf('(', start)
        val begin = position
        while (position < text.length) {
            when (text[position]) {
                '(' -> depth++
                ')' -> { depth--; if (depth == 0) return text.substring(begin, position + 1) }
            }
            position++
        }
        return text.substring(begin)
    }

    @Test
    fun everyClickableSaysWhatItIs() {
        val missing = ArrayList<String>()
        for (file in sources) {
            val text = file.readText()
            for (match in Regex("\\.(clickable|combinedClickable)(\\(| \\{)").findAll(text)) {
                val arguments = if (match.groupValues[2] == "(") callAt(text, match.range.last) else ""
                if (arguments.contains("role =") || arguments.contains("indication = null")) continue
                if (arguments.contains("onClick = onDismiss")) continue
                missing.add(file.name + ":" + (text.substring(0, match.range.first).count { character -> character == '\n' } + 1))
            }
        }
        assertTrue(missing.isEmpty(), "clickables without a role: $missing")
    }

    @Test
    fun noIconButtonHasAnIconWithoutADescription() {
        val missing = ArrayList<String>()
        for (file in sources) {
            val text = file.readText()
            for (match in Regex("\\b(IconButton|FloatingActionButton|FilledTonalIconButton|FilledIconButton)\\(").findAll(text)) {
                val call = callAt(text, match.range.first)
                val rest = text.substring(match.range.first + call.length, minOf(text.length, match.range.first + call.length + 400))
                val body = rest.substringBefore("\n\n")
                if (Regex("\\bText\\(").containsMatchIn(body.substringBefore("}\n"))) continue
                if (Regex("\\bIcon\\([^,()]+(\\([^()]*\\))?[^,()]*, null[,)]").containsMatchIn(body.substringBefore("}\n"))) {
                    missing.add(file.name + ":" + (text.substring(0, match.range.first).count { character -> character == '\n' } + 1))
                }
            }
        }
        assertTrue(missing.isEmpty(), "icon-only controls without a description: $missing")
    }
}
