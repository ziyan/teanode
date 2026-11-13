package com.teanode.kit.coding

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class TerminalTextTest {
    @Test fun `each run takes the style the codes before it set`() {
        val runs = terminalRuns("\u001B[38;2;220;40;40mred\u001B[0m plain \u001B[1;32mbold green\u001B[22m green\u001B[0m")
        assertEquals(listOf("red", " plain ", "bold green", " green"), runs.map { run -> run.text })
        assertEquals(TerminalColor(220, 40, 40), runs[0].foreground)
        assertNull(runs[1].foreground)
        assertTrue(runs[2].isBold)
        assertFalse(runs[3].isBold)
        assertEquals(runs[2].foreground, runs[3].foreground)
    }

    @Test fun `the two hundred fifty six colors and an empty code`() {
        var style = TerminalRun(text = "")
        style = applyTerminalCodes(listOf(38, 5, 196), style)
        assertEquals(TerminalColor(255, 0, 0), style.foreground)
        style = applyTerminalCodes(listOf(48, 5, 232), style)
        assertEquals(TerminalColor(8, 8, 8), style.background)
        val runs = terminalRuns("\u001B[31mred\u001B[mplain")
        assertEquals(listOf("red", "plain"), runs.map { run -> run.text })
        assertNull(runs[1].foreground)
    }

    @Test fun `a full color written with colons`() {
        val runs = terminalRuns("\u001B[38:2::10:20:30mone\u001B[38:2:40:50:60mtwo")
        assertEquals(TerminalColor(10, 20, 30), runs[0].foreground)
        assertEquals(TerminalColor(40, 50, 60), runs[1].foreground)
    }

    @Test fun `text without codes is one run`() {
        assertEquals(listOf("$ npm test"), terminalRuns("$ npm test").map { run -> run.text })
    }
}
