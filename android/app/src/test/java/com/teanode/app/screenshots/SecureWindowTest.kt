package com.teanode.app.screenshots

import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import com.teanode.app.ui.components.SecureWindow
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class SecureWindowTest {
    @get:Rule val composeRule = createAndroidComposeRule<ComponentActivity>()

    private fun isSecure(): Boolean = composeRule.activity.window.attributes.flags and WindowManager.LayoutParams.FLAG_SECURE != 0

    @Test fun `the window is secure while shown and open again after`() {
        var isShown by mutableStateOf(true)
        composeRule.setContent { if (isShown) SecureWindow() }
        composeRule.waitForIdle()
        assertEquals(true, isSecure())
        isShown = false
        composeRule.waitForIdle()
        assertEquals(false, isSecure())
    }
}
