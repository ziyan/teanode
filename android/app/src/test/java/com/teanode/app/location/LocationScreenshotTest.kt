package com.teanode.app.location

import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SheetValue
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.session.LocalConnection
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.ServerTime
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.time.Instant
import java.time.LocalDate

/** Photographs of every Location screen and state, light and dark, with the dense ones at the largest text. */
@OptIn(ExperimentalMaterial3Api::class)
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class LocationScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @After
    fun resetScenario() {
        LocationDemo.scenario = LocationDemo.Scenario.Everyday
    }

    private fun waitFor(text: String) {
        composeRule.waitUntil(timeoutMillis = 10_000) { composeRule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
    }

    private fun draw(
        look: Look, scenario: LocationDemo.Scenario = LocationDemo.Scenario.Everyday, fontScale: Float = 1f, recording: LocationRecording = DemoLocationRecording(),
        content: @Composable () -> Unit,
    ) {
        LocationDemo.scenario = scenario
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                CompositionLocalProvider(LocalLocationRecording provides recording) { WithFontScale(fontScale, content) }
            }
        }
    }

    @Composable
    private fun Home(initialSheetValue: SheetValue = SheetValue.PartiallyExpanded, initialSegment: LocationSegment = LocationSegment.Visits, afterLoad: (LocationModel) -> Unit = {}) {
        val model = rememberLocationModel()
        androidx.compose.runtime.LaunchedEffect(model) { model.load(); afterLoad(model) }
        LocationHome(model, {}, {}, {}, {}, {}, {}, {}, initialSheetValue, initialSegment)
    }

    private fun both(name: String, wait: String, scenario: LocationDemo.Scenario = LocationDemo.Scenario.Everyday, fontScale: Float = 1f, content: @Composable () -> Unit) {
        draw(Look.Light, scenario, fontScale, content = content)
        waitFor(wait)
        composeRule.photograph("location-$name-light")
    }

    @Test fun `visits light`() = both("visits", "Riverside Park") { Home() }

    @Test fun `visits dark`() {
        draw(Look.Dark) { Home() }
        waitFor("Riverside Park")
        composeRule.photograph("location-visits-dark")
    }

    @Test fun `visits at the largest text`() = both("visits-largest-text", "Riverside Park", fontScale = 2f) { Home() }

    @Test fun `drawer full`() = both("drawer-full", "Riverside Park") { Home(initialSheetValue = SheetValue.Expanded) }

    @Test fun `places light`() = both("places", "Hillside School") { Home(initialSegment = LocationSegment.Places) }

    @Test fun `places dark`() {
        draw(Look.Dark) { Home(initialSegment = LocationSegment.Places) }
        waitFor("Hillside School")
        composeRule.photograph("location-places-dark")
    }

    @Test fun `sharing light`() = both("sharing", "Example Household") { Home(initialSegment = LocationSegment.Sharing) }

    @Test fun `sharing dark`() {
        draw(Look.Dark) { Home(initialSegment = LocationSegment.Sharing) }
        waitFor("Example Household")
        composeRule.photograph("location-sharing-dark")
    }

    @Test fun `setup light`() = both("setup", "Set up Location", LocationDemo.Scenario.Setup) { Home() }

    @Test fun `setup dark`() {
        draw(Look.Dark, LocationDemo.Scenario.Setup) { Home() }
        waitFor("Set up Location")
        composeRule.photograph("location-setup-dark")
    }

    @Test fun `paused light`() = both("paused", "Location paused", LocationDemo.Scenario.Paused) { Home(SheetValue.Expanded) }

    @Test fun `limited light`() = both("limited", "visits", LocationDemo.Scenario.Limited) { Home(SheetValue.Expanded) }

    @Test fun `empty light`() {
        draw(Look.Light, LocationDemo.Scenario.Empty, recording = DemoLocationRecording(isSetupAlwaysDismissed = true)) { Home() }
        waitFor("No visits yet")
        composeRule.photograph("location-empty-light")
    }

    @Test fun `person with history chosen`() = both("person", "Sam Example") { Home { model -> it_select(model) } }

    private fun it_select(model: LocationModel) { model.select(LocationDemo.HISTORY_PERSON_ID) }

    @Test fun `person with history chosen dark`() {
        draw(Look.Dark) { Home { model -> model.select(LocationDemo.HISTORY_PERSON_ID) } }
        waitFor("Sam Example")
        composeRule.photograph("location-person-dark")
    }

    @Test fun `person sharing now chosen`() = both("person-current", "Alex Sample") { Home { model -> model.select(LocationDemo.CURRENT_PERSON_ID) } }

    @Test fun `check in`() {
        draw(Look.Light) {
            val model = rememberLocationModel()
            CheckInSheet(model) {}
        }
        waitFor("Corner")
        composeRule.photograph("location-check-in-light")
    }

    @Test fun `day light`() {
        draw(Look.Light) {
            val model = rememberLocationModel()
            LocationDayScreen(model, LocalDate.now(), {}, {})
        }
        waitFor("Riverside Park")
        composeRule.photograph("location-day-light")
    }

    @Test fun `day dark`() {
        draw(Look.Dark) {
            val model = rememberLocationModel()
            LocationDayScreen(model, LocalDate.now(), {}, {})
        }
        waitFor("Riverside Park")
        composeRule.photograph("location-day-dark")
    }

    @Composable
    private fun PlaceScreen() {
        val model = rememberLocationModel()
        androidx.compose.runtime.LaunchedEffect(model) { model.load() }
        LocationPlaceScreen(model, LocationDemo.PARK_PLACE_ID, {}, {})
    }

    @Test fun `place light`() {
        draw(Look.Light) { PlaceScreen() }
        waitFor("Riverside Park")
        composeRule.photograph("location-place-light")
    }

    @Test fun `place dark`() {
        draw(Look.Dark) { PlaceScreen() }
        waitFor("Riverside Park")
        composeRule.photograph("location-place-dark")
    }

    @Test fun `address search`() {
        draw(Look.Light) {
            val model = rememberLocationModel()
            LocationAddressScreen(model, {}, keepAsTyped = { null }, choose = { null })
        }
        composeRule.waitForIdle()
        composeRule.photograph("location-address-light")
    }

    @Test fun `travel light`() {
        draw(Look.Light) { LocationTravelScreen(LocalConnection.current, {}) }
        waitFor("Week of")
        composeRule.photograph("location-travel-light")
    }

    @Test fun `travel dark`() {
        draw(Look.Dark) { LocationTravelScreen(LocalConnection.current, {}) }
        waitFor("Week of")
        composeRule.photograph("location-travel-dark")
    }

    @Test fun `travel at the largest text`() {
        draw(Look.Light, fontScale = 2f) { LocationTravelScreen(LocalConnection.current, {}) }
        waitFor("Week of")
        composeRule.photograph("location-travel-largest-text-light")
    }

    @Test fun `settings light`() {
        draw(Look.Light, recording = DemoLocationRecording(unsentCount = 12, permission = LocationPermissionState.WhileUsing)) { LocationSettingsScreen(rememberLocationModel(), {}) }
        composeRule.waitForIdle()
        composeRule.photograph("location-settings-light")
    }

    @Test fun `settings dark paused`() {
        val pause = LocationPause.of("demo", Instant.now().minusSeconds(1200), Instant.now().plusSeconds(2400))
        draw(Look.Dark, recording = DemoLocationRecording(activePause = pause)) { LocationSettingsScreen(rememberLocationModel(), {}) }
        composeRule.waitForIdle()
        composeRule.photograph("location-settings-paused-dark")
    }

    @Test fun `settings at the largest text`() {
        draw(Look.Light, fontScale = 2f, recording = DemoLocationRecording(permission = LocationPermissionState.Denied)) { LocationSettingsScreen(rememberLocationModel(), {}) }
        composeRule.waitForIdle()
        composeRule.photograph("location-settings-largest-text-light")
    }

    @Test fun `people`() {
        draw(Look.Light) {
            val model = rememberLocationModel()
            androidx.compose.runtime.LaunchedEffect(model) { model.load() }
            LocationPeopleScreen(model) {}
        }
        waitFor("Sam Example")
        composeRule.photograph("location-people-light")
    }

    @Test fun `share sheet`() {
        draw(Look.Light) {
            val model = rememberLocationModel()
            LocationShareSheet(model) {}
        }
        waitFor("Share where I am")
        composeRule.waitForIdle()
        composeRule.photograph("location-share-sheet-light")
    }

    @Test fun `always explanation`() {
        draw(Look.Light) { AlwaysExplanation({}, {}) }
        composeRule.waitForIdle()
        composeRule.photograph("location-always-explanation-light")
    }
}
