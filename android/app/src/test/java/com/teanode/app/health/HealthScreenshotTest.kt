package com.teanode.app.health

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.session.LocalConnection
import com.teanode.kit.health.HealthPeriod
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.time.Instant

/** Photographs of every Health screen and state, light and dark, with the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class HealthScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @After
    fun resetScenario() {
        HealthDemo.scenario = HealthDemo.Scenario.Everyday
    }

    private fun waitFor(text: String) {
        composeRule.waitUntil(timeoutMillis = 10_000) { composeRule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    private fun draw(look: Look, fontScale: Float = 1f, scenario: HealthDemo.Scenario = HealthDemo.Scenario.Everyday, syncing: HealthSyncing = DemoHealthSyncing(), content: @Composable () -> Unit) {
        HealthDemo.scenario = scenario
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                CompositionLocalProvider(LocalHealthSyncing provides syncing) { WithFontScale(fontScale, content) }
            }
        }
    }

    @Composable
    private fun Types() {
        val model = rememberHealthModel()
        HealthScreen(model, {}, {})
    }

    @Composable
    private fun TypeOf(healthType: String) {
        val model = rememberHealthModel()
        LaunchedEffect(model) { model.load() }
        val type = model.types.firstOrNull { each -> each.healthType == healthType }
        if (type != null) HealthTypeScreen(model, type, {}, {})
    }

    @Test fun `kinds light`() { draw(Look.Light) { Types() }; waitFor("Steps"); composeRule.photograph("health-types-light") }

    @Test fun `kinds dark`() { draw(Look.Dark) { Types() }; waitFor("Steps"); composeRule.photograph("health-types-dark") }

    @Test fun `kinds at the largest text`() { draw(Look.Light, fontScale = 2f) { Types() }; waitFor("Steps"); composeRule.photograph("health-types-largest-text-light") }

    @Test fun `empty with Health off`() {
        draw(Look.Light, scenario = HealthDemo.Scenario.Empty, syncing = DemoHealthSyncing(HealthSyncSnapshot(isOn = false))) { Types() }
        waitFor("No Health data yet")
        composeRule.photograph("health-empty-off-light")
    }

    @Test fun `empty while copying`() {
        draw(Look.Dark, scenario = HealthDemo.Scenario.Empty, syncing = DemoHealthSyncing(HealthSyncSnapshot(isOn = true))) { Types() }
        waitFor("No Health data yet")
        composeRule.photograph("health-empty-copying-dark")
    }

    @Test fun `steps light`() { draw(Look.Light) { TypeOf("stepCount") }; waitFor("Average per day"); composeRule.photograph("health-steps-light") }

    @Test fun `steps dark`() { draw(Look.Dark) { TypeOf("stepCount") }; waitFor("Average per day"); composeRule.photograph("health-steps-dark") }

    @Test fun `steps at the largest text`() { draw(Look.Light, fontScale = 2f) { TypeOf("stepCount") }; waitFor("Average per day"); composeRule.photograph("health-steps-largest-text-light") }

    @Test fun `sleep light`() { draw(Look.Light) { TypeOf("sleepAnalysis") }; waitFor("Average per day"); composeRule.photograph("health-sleep-light") }

    @Test fun `sleep dark`() { draw(Look.Dark) { TypeOf("sleepAnalysis") }; waitFor("Average per day"); composeRule.photograph("health-sleep-dark") }

    @Test fun `heart rate light`() { draw(Look.Light) { TypeOf("heartRate") }; waitFor("Highest"); composeRule.photograph("health-heart-light") }

    @Test fun `heart rate dark`() { draw(Look.Dark) { TypeOf("heartRate") }; waitFor("Highest"); composeRule.photograph("health-heart-dark") }

    @Test fun `workouts`() { draw(Look.Light) { TypeOf("workout") }; waitFor("Running"); composeRule.photograph("health-workouts-light") }

    @Test fun `route light`() {
        draw(Look.Light) { HealthRouteScreen(LocalConnection.current, HealthRouteRequest(HealthDemo.RUN_SAMPLE_ID, "Running", Instant.now().minusSeconds(6 * 3600), 2400.0, 5200.0, 410.0), {}) }
        waitFor("Distance")
        composeRule.waitForIdle()
        composeRule.photograph("health-route-light")
    }

    @Test fun `route dark`() {
        draw(Look.Dark) { HealthRouteScreen(LocalConnection.current, HealthRouteRequest(HealthDemo.RUN_SAMPLE_ID, "Running", Instant.now().minusSeconds(6 * 3600), 2400.0, 5200.0, 410.0), {}) }
        waitFor("Distance")
        composeRule.photograph("health-route-dark")
    }

    @Test fun `settings light`() {
        draw(Look.Light, syncing = DemoHealthSyncing(HealthSyncSnapshot(isOn = true, lastSentAt = Instant.now().minusSeconds(600), sentSampleCount = 18_420, pendingDayCount = 3, droppedEntryCount = 2, passProgress = HealthPassProgress(12, 40)))) { HealthSettingsScreen({}) }
        waitFor("Send now")
        composeRule.photograph("health-settings-light")
    }

    @Test fun `settings dark off`() {
        draw(Look.Dark, syncing = DemoHealthSyncing(HealthSyncSnapshot(isOn = false, isStoppedByErase = true))) { HealthSettingsScreen({}) }
        waitFor("Sharing")
        composeRule.photograph("health-settings-dark")
    }

    @Test fun `settings at the largest text`() {
        draw(Look.Light, fontScale = 2f) { HealthSettingsScreen({}) }
        waitFor("Send now")
        composeRule.photograph("health-settings-largest-text-light")
    }

    @Test fun `setup`() {
        draw(Look.Light) { HealthSetupContent(isTurningOn = false, onContinue = {}, onCancel = {}) }
        composeRule.waitForIdle()
        composeRule.photograph("health-setup-light")
    }

    @Test fun `share sheet`() {
        draw(Look.Light) {
            val connection = LocalConnection.current
            val context = androidx.compose.ui.platform.LocalContext.current
            val state = androidx.compose.runtime.remember { HealthSettingsState(connection, { error -> error.message.orEmpty() }, HealthText(context)) { _, _ -> } }
            HealthShareSheet(state) {}
        }
        waitFor("Share Health")
        composeRule.waitForIdle()
        composeRule.photograph("health-share-sheet-light")
    }

    @Test fun `person sharing`() {
        draw(Look.Light) {
            val model = rememberHealthModel()
            LaunchedEffect(model) { model.load(); model.select(HealthDemo.SHARING_PERSON_ID) }
            HealthScreen(model, {}, {})
        }
        waitFor("Steps")
        composeRule.photograph("health-person-light")
    }
}
