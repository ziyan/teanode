package com.teanode.app.calendar

import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import java.time.Instant
import java.time.LocalDate

/** The places of Calendar: the views, a day, an event, the editor, a reminder, the calendars and who they are shared with, and Find a time. */
object CalendarRoutes {
    val HOME: String = Routes.home(AppSection.Calendar)
    val DAY: String = Routes.child(AppSection.Calendar, "day/{epochDay}")
    val SEARCH: String = Routes.child(AppSection.Calendar, "search")
    val CALENDARS: String = Routes.child(AppSection.Calendar, "calendars")
    val SHARE: String = Routes.child(AppSection.Calendar, "share")
    val FIND_A_TIME: String = Routes.child(AppSection.Calendar, "find-a-time")
    val NEW_EVENT: String = Routes.child(AppSection.Calendar, "event/new/{startEpochMS}")
    val EVENT: String = Routes.child(AppSection.Calendar, "event/{calendarId}/{eventId}")
    val REMINDER: String = Routes.child(AppSection.Calendar, "reminder/{reminderId}")
    val VIEW: String = Routes.child(AppSection.Calendar, "view/{mode}")

    /** A day opened as hours, by its number of days since 1970. */
    fun day(epochDay: Long): String = Routes.child(AppSection.Calendar, "day/$epochDay")

    /** The editor for a new event that starts at a moment, in milliseconds since 1970. */
    fun newEvent(startEpochMS: Long): String = Routes.child(AppSection.Calendar, "event/new/$startEpochMS")

    /** An event of a calendar, read whole and then edited. */
    fun event(calendarId: String, eventId: String): String = Routes.child(AppSection.Calendar, "event/${Uri.encode(calendarId)}/${Uri.encode(eventId)}")

    /** A reminder, or a new one for a null id. */
    fun reminder(reminderId: String?): String = Routes.child(AppSection.Calendar, "reminder/${Uri.encode(reminderId ?: NEW_REMINDER)}")

    /** The calendar opened in one of its views, which Search offers by name. */
    fun view(mode: CalendarViewMode): String = Routes.child(AppSection.Calendar, "view/${mode.name.lowercase()}")

    const val NEW_REMINDER = "new"

    /** The calendar of an event that is only known by its identifier, such as one a page of memory points to: the opener finds it. */
    const val UNKNOWN_CALENDAR = "-"
}

@Composable
private fun model(): CalendarModel = rememberCalendarModel()

/** The model of the calendar with the calendars read, for a screen opened straight from a link before the month view read them. */
@Composable
private fun loadedModel(): CalendarModel {
    val model = rememberCalendarModel()
    LaunchedEffect(model) { if (model.calendar == null) model.load() }
    return model
}

/** The routes of Calendar. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.calendarRoutes(navigator: AppNavigator) {
    composable(CalendarRoutes.HOME, deepLinks = Routes.deepLinks(CalendarRoutes.HOME)) { CalendarSection() }
    composable(CalendarRoutes.VIEW, deepLinks = Routes.deepLinks(CalendarRoutes.VIEW)) { entry ->
        val mode = CalendarViewMode.entries.firstOrNull { each -> each.name.equals(entry.arguments?.getString("mode"), ignoreCase = true) } ?: CalendarViewMode.Month
        CalendarSection(initialMode = mode)
    }
    composable(CalendarRoutes.DAY, arguments = listOf(navArgument("epochDay") { type = NavType.LongType })) { entry ->
        CalendarDayScreen(model(), LocalDate.ofEpochDay(entry.arguments?.getLong("epochDay") ?: LocalDate.now().toEpochDay()))
    }
    composable(CalendarRoutes.SEARCH) { CalendarSearchScreen(model()) }
    composable(CalendarRoutes.CALENDARS) { CalendarListScreen(model()) }
    composable(CalendarRoutes.SHARE) {
        val model = loadedModel()
        model.calendar?.let { own -> CalendarShareScreen(model, own) }
    }
    composable(CalendarRoutes.FIND_A_TIME) {
        val model = loadedModel()
        model.calendar?.ownerUserId?.let { ownUserId -> FindATimeScreen(model, ownUserId) }
    }
    composable(CalendarRoutes.NEW_EVENT, arguments = listOf(navArgument("startEpochMS") { type = NavType.LongType })) { entry ->
        NewEventScreen(model(), Instant.ofEpochMilli(entry.arguments?.getLong("startEpochMS") ?: System.currentTimeMillis()))
    }
    composable(CalendarRoutes.EVENT, deepLinks = Routes.deepLinks(CalendarRoutes.EVENT)) { entry ->
        EventOpenerScreen(model(), entry.arguments?.getString("calendarId").orEmpty(), entry.arguments?.getString("eventId").orEmpty())
    }
    composable(CalendarRoutes.REMINDER) { entry ->
        val reminderId = entry.arguments?.getString("reminderId")?.takeIf { id -> id != CalendarRoutes.NEW_REMINDER }
        ReminderEditorScreen(model(), reminderId)
    }
}

/** What Calendar plugs into the shell; listed in `Areas`. */
val calendarArea = Area(
    section = AppSection.Calendar,
    routes = NavGraphBuilder::calendarRoutes,
    demoAnswerers = calendarDemoAnswerers,
)
