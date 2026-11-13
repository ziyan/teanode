package com.teanode.app.health.sync

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavDeepLink
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.health.DemoHealthSyncing
import com.teanode.app.health.LocalHealthSyncing
import com.teanode.app.session.LocalAppSession

/** Gives the screens inside the copying of Health Connect to the server, or a copying that does nothing when the session is the demo. */
@Composable
fun ProvideHealthSyncing(content: @Composable () -> Unit) {
    val session = LocalAppSession.current
    val context = LocalContext.current
    val syncing = remember(session.isDemo) { if (session.isDemo) DemoHealthSyncing() else HealthSync.get(context).also { each -> each.refresh() } }
    // A pass when a Health screen opens, since Health Connect cannot say when there is new data.
    LaunchedEffect(syncing) { if (syncing is HealthSync && syncing.snapshot.value.isOn) syncing.syncNow() }
    CompositionLocalProvider(LocalHealthSyncing provides syncing, content = content)
}

/** A route of Health, drawn inside [ProvideHealthSyncing]. */
fun NavGraphBuilder.healthComposable(
    route: String, arguments: List<NamedNavArgument> = emptyList(), deepLinks: List<NavDeepLink> = emptyList(), content: @Composable (NavBackStackEntry) -> Unit,
) = composable(route, arguments, deepLinks) { entry -> ProvideHealthSyncing { content(entry) } }
