package com.teanode.app.settings

import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class UpkeepWordsTest {
    private val resources = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    @Test
    fun theMemoryCheckUsesTheReadersWords() {
        assertEquals("Confirmed", UpkeepWords.questionState(resources, "confirmed"))
        assertEquals("Dropped", UpkeepWords.questionState(resources, "dropped"))
        assertEquals("Should not know", UpkeepWords.questionKind(resources, "abstain"))
        assertEquals("Out of date", UpkeepWords.verdict(resources, "stale"))
        assertEquals("a-new-state", UpkeepWords.questionState(resources, "a-new-state"))
    }
}
