package com.teanode.app.shell

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.Scaffold
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Modifier
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.navigation
import androidx.navigation.compose.rememberNavController
import com.teanode.app.search.searchRoutes
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.DrawerController
import com.teanode.app.ui.components.LocalDrawer
import com.teanode.kit.AppSection
import com.teanode.kit.AppSectionGroup
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import android.content.Intent

/**
 * AppShell is the signed-in app: the navigation drawer with every part, the bottom bar with the three the person
 * keeps there (Chats, Mail and Photos at first) beside More and Search, and one navigation host holding the routes of
 * every part ([Areas]).
 *
 * @param startRoute opens this route first instead of the first part in the bar (a deep link, or the demo's screen).
 * @param newIntents deep links that arrive while the app is open.
 */
@Composable
fun AppShell(
    modifier: Modifier = Modifier,
    startRoute: String? = null,
    controller: NavHostController = rememberNavController(),
    newIntents: kotlinx.coroutines.flow.Flow<Intent>? = null,
) {
    val session = LocalAppSession.current
    val facts by session.facts.collectAsState()
    val hasReadFacts by session.hasReadFacts.collectAsState()
    val tabBarSections by session.tabBarSections.collectAsState()
    val badgeCounts by session.badgeCounts.collectAsState()
    val navigator = remember(controller) { AppNavigator(controller) }
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    val scope = rememberCoroutineScope()
    val backStackEntry by controller.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val currentSection = Routes.sectionOf(currentRoute)
    val isHome = Routes.isHome(currentRoute)
    var isEditingTabBar by remember { mutableStateOf(false) }

    // The parts in the bar. One the server turned off has no tab, so it holds no place in the bar while it is off.
    val barSections = if (hasReadFacts) tabBarSections.filter { section -> facts.isAvailable(section) } else tabBarSections
    val startSection = barSections.firstOrNull() ?: AppSection.Settings

    // Keeps the screen on a part that exists: when the server says the one open is off, the first in the bar opens.
    LaunchedEffect(facts, hasReadFacts, currentSection) {
        if (hasReadFacts && currentSection != null && !facts.isAvailable(currentSection)) navigator.openSection(startSection)
    }
    LaunchedEffect(startRoute) {
        // The graph is set once the host below has composed; the first entry says it has.
        if (startRoute != null) {
            controller.currentBackStackEntryFlow.first()
            controller.navigate(startRoute)
        }
    }
    LaunchedEffect(newIntents) {
        newIntents?.collect { intent -> controller.handleDeepLink(intent) }
    }
    BackHandler(enabled = drawerState.isOpen) { scope.launch { drawerState.close() } }

    val drawer = remember(drawerState) { DrawerController(open = { scope.launch { drawerState.open() } }) }
    CompositionLocalProvider(LocalAppNavigator provides navigator, LocalDrawer provides drawer) {
        ModalNavigationDrawer(
            drawerContent = {
                AppDrawer(
                    currentSection = currentSection,
                    facts = facts,
                    badgeCounts = badgeCounts,
                    onOpen = { section ->
                        scope.launch { drawerState.close() }
                        navigator.openSection(section)
                    },
                    onEditTabBar = {
                        scope.launch { drawerState.close() }
                        isEditingTabBar = true
                    },
                )
            },
            drawerState = drawerState,
            gesturesEnabled = isHome || drawerState.isOpen,
            modifier = modifier,
        ) {
            Scaffold(
                contentWindowInsets = WindowInsets(0),
                bottomBar = {
                    if (isHome) {
                        AppBottomBar(
                            barSections = barSections,
                            currentRoute = currentRoute,
                            badgeCounts = badgeCounts,
                            onOpenSection = { section -> navigator.openSection(section) },
                            onOpenSearch = { navigator.openSearch() },
                            onOpenMore = drawer.open,
                        )
                    }
                },
            ) { innerPadding ->
                Box(Modifier.padding(innerPadding).consumeWindowInsets(innerPadding)) {
                    NavHost(navController = controller, startDestination = Routes.graph(startSection)) {
                        for (area in Areas.all) {
                            val section = area.section
                            if (section == null) {
                                area.routes(this, navigator)
                            } else {
                                navigation(startDestination = Routes.home(section), route = Routes.graph(section)) {
                                    area.routes(this, navigator)
                                }
                            }
                        }
                    }
                }
            }
        }
        if (isEditingTabBar) {
            TabBarEditor(
                chosen = barSections,
                others = AppSectionGroup.entries.flatMap { group -> group.sections }.filter { section ->
                    section !in barSections && (!hasReadFacts || facts.isAvailable(section))
                },
                onChange = { sections -> session.setTabBarSections(sections) },
                onDismiss = { isEditingTabBar = false },
            )
        }
    }
}

