package com.teanode.app.goals

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The places of Goals: the list, one goal, and the form that starts one. */
object GoalRoutes {
    val HOME: String = Routes.home(AppSection.Goals)
    val NEW: String = Routes.child(AppSection.Goals, "new")
    val DETAIL: String = Routes.child(AppSection.Goals, "goal/{conversationId}")

    fun detail(conversationId: String): String = Routes.child(AppSection.Goals, "goal/${android.net.Uri.encode(conversationId)}")
}

/** The routes of Goals. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.goalsRoutes(navigator: AppNavigator) {
    composable(GoalRoutes.HOME, deepLinks = Routes.deepLinks(GoalRoutes.HOME)) { GoalsSection() }
    composable(GoalRoutes.NEW, deepLinks = Routes.deepLinks(GoalRoutes.NEW)) { GoalStarterScreen() }
    composable(GoalRoutes.DETAIL, deepLinks = Routes.deepLinks(GoalRoutes.DETAIL)) { entry ->
        GoalDetailScreen(entry.arguments?.getString("conversationId").orEmpty())
    }
}

/** What Goals plugs into the shell; listed in `Areas`. */
val goalsArea = Area(
    section = AppSection.Goals,
    routes = NavGraphBuilder::goalsRoutes,
    demoAnswerers = goalsDemoAnswerers,
)
