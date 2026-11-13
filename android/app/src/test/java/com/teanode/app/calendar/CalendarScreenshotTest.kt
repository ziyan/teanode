package com.teanode.app.calendar

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.kit.calendar.CalendarDay
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.time.LocalDate
import java.time.ZoneId

/** Photographs of Calendar in demo mode: the months, a day as hours, the week, the agenda, the reminders and every screen behind them. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class CalendarScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale, content) } }
        settle()
    }

    private fun settle() {
        repeat(4) {
            composeRule.waitForIdle()
            Thread.sleep(150)
        }
        composeRule.waitForIdle()
    }

    @Test fun `month light`() { draw(Look.Light) { CalendarSection() }; composeRule.photograph("calendar-month-light") }

    @Test fun `month dark`() { draw(Look.Dark) { CalendarSection() }; composeRule.photograph("calendar-month-dark") }

    @Test fun `month at the largest text`() { draw(Look.Light, 2f) { CalendarSection() }; composeRule.photograph("calendar-month-largest-text-light") }

    @Test fun `week light`() { draw(Look.Light) { CalendarSection(CalendarViewMode.Week) }; composeRule.photograph("calendar-week-light") }

    @Test fun `five days dark`() { draw(Look.Dark) { CalendarSection(CalendarViewMode.Workweek) }; composeRule.photograph("calendar-five-days-dark") }

    @Test fun `agenda light`() { draw(Look.Light) { CalendarSection(CalendarViewMode.Agenda) }; composeRule.photograph("calendar-agenda-light") }

    @Test fun `agenda at the largest text`() { draw(Look.Light, 2f) { CalendarSection(CalendarViewMode.Agenda) }; composeRule.photograph("calendar-agenda-largest-text-light") }

    @Test fun `day view light`() { draw(Look.Light) { CalendarSection(CalendarViewMode.Day) }; composeRule.photograph("calendar-day-light") }

    @Test fun `a wednesday as hours dark`() {
        draw(Look.Dark) { CalendarDayScreen(rememberCalendarModel(), nextWeekday(3)) }
        composeRule.photograph("calendar-day-wednesday-dark")
    }

    @Test fun `reminders light`() { draw(Look.Light) { CalendarSection(CalendarViewMode.Reminders) }; composeRule.photograph("calendar-reminders-light") }

    @Test fun `reminders dark`() { draw(Look.Dark) { CalendarSection(CalendarViewMode.Reminders) }; composeRule.photograph("calendar-reminders-dark") }

    @Test fun `done reminders light`() {
        draw(Look.Light) { CalendarSection(CalendarViewMode.Reminders) }
        composeRule.onNodeWithText("Done").performClick()
        settle()
        composeRule.photograph("calendar-reminders-done-light")
    }

    @Test fun `reminder editor light`() {
        draw(Look.Light) { ReminderEditorScreen(rememberCalendarModel(), null) }
        composeRule.photograph("calendar-reminder-new-light")
    }

    @Test fun `search empty dark`() { draw(Look.Dark) { CalendarSearchScreen(rememberCalendarModel()) }; composeRule.photograph("calendar-search-dark") }

    @Test fun `calendars light`() { draw(Look.Light) { CalendarListScreen(rememberCalendarModel()) }; composeRule.photograph("calendar-calendars-light") }

    @Test fun `calendars dark`() { draw(Look.Dark) { CalendarListScreen(rememberCalendarModel()) }; composeRule.photograph("calendar-calendars-dark") }

    @Test fun `share light`() {
        draw(Look.Light) {
            val model = rememberCalendarModel()
            androidx.compose.runtime.LaunchedEffect(Unit) { model.load() }
            model.calendar?.let { own -> CalendarShareScreen(model, own) }
        }
        composeRule.photograph("calendar-share-light")
    }

    @Test fun `find a time light`() {
        draw(Look.Light) { FindATimeScreen(rememberCalendarModel(), "demo-user") }
        settle()
        composeRule.onNodeWithText("Sam Rivera (sam)").performClick()
        composeRule.onNodeWithText("Alex Chen (alex)").performClick()
        composeRule.onNodeWithText("Find times").performClick()
        settle()
        composeRule.photograph("calendar-find-a-time-light")
    }

    @Test fun `new event light`() {
        draw(Look.Light) { NewEventScreen(rememberCalendarModel(), java.time.Instant.now()) }
        composeRule.photograph("calendar-new-event-light")
    }

    @Test fun `new event dark`() {
        draw(Look.Dark) { NewEventScreen(rememberCalendarModel(), java.time.Instant.now()) }
        composeRule.photograph("calendar-new-event-dark")
    }

    @Test fun `an event of the weekly standup dark`() {
        draw(Look.Dark) { EventOpenerScreen(rememberCalendarModel(), "home", "home.standup.${nextWeekday(2).toEpochDay()}") }
        composeRule.photograph("calendar-event-standup-dark")
    }

    @Test fun `a shared event is read light`() {
        draw(Look.Light) { EventOpenerScreen(rememberCalendarModel(), "ben", "ben.plan.${nextEvenWeekday().toEpochDay()}") }
        composeRule.photograph("calendar-event-shared-light")
    }

    @Test fun `custom repeat light`() {
        val zone = ZoneId.systemDefault()
        val context = com.teanode.kit.calendar.CalendarRepeat.Context(CalendarDay.of(nextWeekday(2)), zone, false)
        draw(Look.Light) { CustomRepeatScreen("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20271231T235959Z", context, {}, {}) }
        composeRule.photograph("calendar-custom-repeat-light")
    }

    @Test fun `custom monthly repeat dark`() {
        val zone = ZoneId.systemDefault()
        val context = com.teanode.kit.calendar.CalendarRepeat.Context(CalendarDay.of(nextWeekday(2)), zone, false)
        draw(Look.Dark) { CustomRepeatScreen("FREQ=MONTHLY;BYDAY=2TU;COUNT=10", context, {}, {}) }
        composeRule.photograph("calendar-custom-repeat-monthly-dark")
    }

    @Test fun `repeat set elsewhere light`() {
        val zone = ZoneId.systemDefault()
        val context = com.teanode.kit.calendar.CalendarRepeat.Context(CalendarDay.of(nextWeekday(2)), zone, false)
        draw(Look.Light) { CustomRepeatScreen("FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1", context, {}, {}) }
        composeRule.photograph("calendar-custom-repeat-kept-light")
    }

    private fun nextWeekday(weekday: Int): LocalDate {
        var day = LocalDate.now().plusDays(1)
        while (day.dayOfWeek.value != weekday) day = day.plusDays(1)
        return day
    }

    private fun nextEvenWeekday(): LocalDate {
        var day = LocalDate.now().plusDays(1)
        while (day.dayOfWeek.value !in 1..5 || day.toEpochDay() % 2L != 0L) day = day.plusDays(1)
        return day
    }

}

/** The month view with Chinese days, which show their lunar dates under them. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = "zh-rCN-$PHONE_QUALIFIERS")
class CalendarChineseScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test fun `month in Chinese light`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { CalendarSection() } }
        repeat(4) {
            composeRule.waitForIdle()
            Thread.sleep(150)
        }
        composeRule.photograph("calendar-month-zh-light")
    }
}
