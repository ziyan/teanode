package com.teanode.app.shell

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.health.HealthRoutes
import com.teanode.app.location.LocationRoutes
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Location and Health open from the real shell, through the routes the areas list registers, in the demo app. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ShellRoutesTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun opens(route: String, expectedText: String) {
        composeRule.setContent { DemoHost { AppShell(startRoute = route) } }
        composeRule.waitUntil(timeoutMillis = 15_000) { composeRule.onAllNodesWithText(expectedText, substring = true).fetchSemanticsNodes().isNotEmpty() }
    }

    @Test fun `location opens from the shell`() = opens(LocationRoutes.home, "Sam Example")

    @Test fun `location settings open from the shell`() = opens(LocationRoutes.settings, "Location settings")

    @Test fun `travel opens from the shell`() = opens(LocationRoutes.travel, "Week")

    @Test fun `a person's travel opens from the shell`() = opens(LocationRoutes.personTravel("sam-example"), "Week")

    @Test fun `health opens from the shell`() = opens(HealthRoutes.home, "Steps")

    @Test fun `health settings open from the shell`() = opens(HealthRoutes.settings, "Send now")
}
