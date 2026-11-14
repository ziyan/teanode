package com.teanode.app.journal

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.journal.JournalPeriodKind

/** The places of Journal. A day is written `2026-03-14`; a period is named by its kind and a day in it. */
object JournalRoutes {
    val HOME: String = Routes.home(AppSection.Journal)
    val SETTINGS: String = Routes.child(AppSection.Journal, "settings")
    val DAY: String = Routes.child(AppSection.Journal, "day/{day}")
    val PERIOD: String = Routes.child(AppSection.Journal, "period/{kind}/{day}")
    val WRITE: String = Routes.child(AppSection.Journal, "write/{kind}/{day}")
    val CORRECT: String = Routes.child(AppSection.Journal, "correct/{kind}/{day}")

    /** `day` is the kind of a day, which the routes with a kind spell so. */
    private const val DAY_KIND = "day"

    fun day(day: String): String = Routes.child(AppSection.Journal, "day/$day")

    fun period(periodKind: JournalPeriodKind, day: String): String = Routes.child(AppSection.Journal, "period/${periodKind.name.lowercase()}/$day")

    /** The screen of a zoom: a day, or a week, month or year. */
    fun zoomed(zoom: JournalZoom, day: String): String = zoom.periodKind?.let { kind -> period(kind, day) } ?: day(day)

    /** Where the words of a day (null kind) or of a period are written. */
    fun write(periodKind: JournalPeriodKind?, day: String): String = Routes.child(AppSection.Journal, "write/${periodKind?.name?.lowercase() ?: DAY_KIND}/$day")

    /** Where what a story got wrong is told. */
    fun correct(periodKind: JournalPeriodKind?, day: String): String = Routes.child(AppSection.Journal, "correct/${periodKind?.name?.lowercase() ?: DAY_KIND}/$day")

    fun periodKindOf(name: String?): JournalPeriodKind? = JournalPeriodKind.entries.firstOrNull { kind -> kind.name.equals(name, ignoreCase = true) }
}

/** The routes of Journal. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.journalRoutes(navigator: AppNavigator) {
    composable(JournalRoutes.HOME, deepLinks = Routes.deepLinks(JournalRoutes.HOME)) { JournalSection() }
    composable(JournalRoutes.SETTINGS) { JournalSettingsScreen() }
    composable(JournalRoutes.DAY, arguments = listOf(navArgument("day") { type = NavType.StringType })) { entry ->
        JournalDayScreen(entry.arguments?.getString("day").orEmpty())
    }
    composable(JournalRoutes.PERIOD) { entry ->
        val periodKind = JournalRoutes.periodKindOf(entry.arguments?.getString("kind")) ?: JournalPeriodKind.Week
        JournalPeriodScreen(periodKind, entry.arguments?.getString("day").orEmpty())
    }
    composable(JournalRoutes.WRITE) { entry ->
        JournalWriteScreen(JournalRoutes.periodKindOf(entry.arguments?.getString("kind")), entry.arguments?.getString("day").orEmpty())
    }
    composable(JournalRoutes.CORRECT) { entry ->
        JournalCorrectionScreen(JournalRoutes.periodKindOf(entry.arguments?.getString("kind")), entry.arguments?.getString("day").orEmpty())
    }
}

/** What Journal plugs into the shell; listed in `Areas`. */
val journalArea = Area(
    section = AppSection.Journal,
    routes = NavGraphBuilder::journalRoutes,
    demoAnswerers = journalDemoAnswerers,
)
