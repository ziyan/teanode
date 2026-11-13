package com.teanode.app.finance

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Finance. Register every screen of this part here, with `Routes.child(AppSection.Finance, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.financeRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Finance), deepLinks = Routes.deepLinks(Routes.home(AppSection.Finance))) {
        FinanceSection()
    }
}

/** What Finance plugs into the shell; listed in `Areas`. */
val financeArea = Area(
    section = AppSection.Finance,
    routes = NavGraphBuilder::financeRoutes,
    demoAnswerers = financeDemoAnswerers,
)
