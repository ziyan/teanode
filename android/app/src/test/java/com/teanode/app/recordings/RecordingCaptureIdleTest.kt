package com.teanode.app.recordings

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The screen a link or a widget opens must not start the microphone: only the person's tap on Start does. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class RecordingCaptureIdleTest {
    @get:Rule val composeRule = createComposeRule()

    @Test fun `opening the capture screen waits for the start button`() {
        composeRule.setContent { DemoHost { RecordingCaptureScreen(onBack = {}) } }
        composeRule.mainClock.advanceTimeBy(3_000)
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Start recording").assertIsDisplayed()
        assertFalse(RecordingCaptureService.state.value.isRecording)
    }
}
