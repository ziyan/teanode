package com.teanode.kit.chats

/**
 * A terminal's screen as the program in it colored it. The text comes with the codes that select a color or a style
 * (SGR) and no other escape sequence: the computer program keeps only those. `terminalRuns` splits it into runs of
 * text, each with the style the codes before it set.
 */
data class TerminalColor(val red: Int, val green: Int, val blue: Int)

data class TerminalRun(
    val text: String,
    val foreground: TerminalColor? = null,
    val background: TerminalColor? = null,
    val isBold: Boolean = false,
    val isDim: Boolean = false,
    val isItalic: Boolean = false,
    val isUnderlined: Boolean = false,
    val isInverse: Boolean = false,
)

/** The sixteen colors most terminals start with. */
private val basicColors: List<TerminalColor> = listOf(
    Triple(0, 0, 0), Triple(205, 49, 49), Triple(13, 188, 121), Triple(229, 229, 16), Triple(36, 114, 200), Triple(188, 63, 188),
    Triple(17, 168, 205), Triple(229, 229, 229), Triple(102, 102, 102), Triple(241, 76, 76), Triple(35, 209, 139), Triple(245, 245, 67),
    Triple(59, 142, 234), Triple(214, 112, 214), Triple(41, 184, 219), Triple(255, 255, 255),
).map { triple -> TerminalColor(triple.first, triple.second, triple.third) }

/** One of the 256 colors a code may name by number. */
internal fun terminalColor(index: Int): TerminalColor? = when (index) {
    in 0 until 16 -> basicColors[index]
    in 16 until 232 -> {
        val cube = index - 16
        fun level(step: Int): Int = if (step == 0) 0 else 55 + step * 40
        TerminalColor(level(cube / 36), level(cube / 6 % 6), level(cube % 6))
    }
    in 232..255 -> {
        val gray = 8 + (index - 232) * 10
        TerminalColor(gray, gray, gray)
    }
    else -> null
}

/** Changes a run's style as one code's numbers say. */
internal fun applyTerminalCodes(numbers: List<Int>, style: TerminalRun): TerminalRun {
    var current = style
    var at = 0
    while (at < numbers.size) {
        val number = numbers[at]
        when (number) {
            0 -> current = TerminalRun("")
            1 -> current = current.copy(isBold = true)
            2 -> current = current.copy(isDim = true)
            3 -> current = current.copy(isItalic = true)
            4 -> current = current.copy(isUnderlined = true)
            7 -> current = current.copy(isInverse = true)
            22 -> current = current.copy(isBold = false, isDim = false)
            23 -> current = current.copy(isItalic = false)
            24 -> current = current.copy(isUnderlined = false)
            27 -> current = current.copy(isInverse = false)
            in 30..37 -> current = current.copy(foreground = basicColors[number - 30])
            in 90..97 -> current = current.copy(foreground = basicColors[number - 90 + 8])
            in 40..47 -> current = current.copy(background = basicColors[number - 40])
            in 100..107 -> current = current.copy(background = basicColors[number - 100 + 8])
            39 -> current = current.copy(foreground = null)
            49 -> current = current.copy(background = null)
            38, 48 -> {
                var color: TerminalColor? = null
                if (at + 2 < numbers.size && numbers[at + 1] == 5) {
                    color = terminalColor(numbers[at + 2])
                    at += 2
                } else if (at + 4 < numbers.size && numbers[at + 1] == 2) {
                    val parts = numbers.subList(at + 2, at + 5)
                    if (parts.all { part -> part in 0..255 }) color = TerminalColor(parts[0], parts[1], parts[2])
                    at += 4
                }
                current = if (number == 38) current.copy(foreground = color) else current.copy(background = color)
            }
            else -> Unit
        }
        at += 1
    }
    return current
}

/** The runs of a colored screen, in order; text without codes is one run. */
fun terminalRuns(text: String): List<TerminalRun> {
    val runs = mutableListOf<TerminalRun>()
    var style = TerminalRun("")
    var rest = text
    while (true) {
        val start = rest.indexOf("\u001B[")
        if (start < 0) break
        if (start > 0) runs.add(style.copy(text = rest.substring(0, start)))
        val afterIntroducer = rest.substring(start + 2)
        val end = afterIntroducer.indexOfFirst { character -> character !in "0123456789;:" }
        if (end < 0 || afterIntroducer[end] != 'm') {
            // Not a color code: skip the escape character and go on.
            rest = rest.substring(start + 1)
            continue
        }
        val parameters = afterIntroducer.substring(0, end)
        val numbers = if (parameters.isEmpty()) {
            listOf(0)
        } else {
            parameters.split(";").flatMap { group ->
                val parts = group.split(":").toMutableList()
                // The colon form of a full color may name a color space before the red, green and blue, often empty: 38:2::r:g:b.
                if (parts.size == 6 && parts[0] in listOf("38", "48") && parts[1] == "2") parts.removeAt(2)
                parts
            }.map { part -> part.toIntOrNull() ?: 0 }
        }
        style = applyTerminalCodes(numbers, style)
        rest = afterIntroducer.substring(end + 1)
    }
    if (rest.isNotEmpty()) runs.add(style.copy(text = rest))
    return runs
}
