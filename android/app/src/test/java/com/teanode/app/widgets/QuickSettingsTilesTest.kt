package com.teanode.app.widgets

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The tiles only open the app: the call tile asks for a call, which the app confirms, and the recording tile opens the recorder. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class QuickSettingsTilesTest {
    private val context: Application = ApplicationProvider.getApplicationContext()

    @Test
    fun theCallTileOpensTheChatsAskingForACall() {
        val intent = openingIntent(context, CallAgentTileService().route, CallAgentTileService().isCall)
        assertEquals("teanode://app/agent/home", intent.data.toString())
        assertTrue(intent.getBooleanExtra(EXTRA_CALL_AGENT, false))
    }

    @Test
    fun theRecordingTileOpensTheRecorderWithoutACall() {
        val intent = openingIntent(context, StartRecordingTileService().route, StartRecordingTileService().isCall)
        assertEquals("teanode://app/recordings/capture", intent.data.toString())
        assertFalse(intent.getBooleanExtra(EXTRA_CALL_AGENT, false))
    }
}
