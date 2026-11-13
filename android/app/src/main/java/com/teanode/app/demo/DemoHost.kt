package com.teanode.app.demo

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.compose.rememberNavController
import com.teanode.app.session.AppSession
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.rememberSnackbarController
import com.teanode.app.ui.theme.AppearanceChoice
import com.teanode.app.ui.theme.TeaNodeTheme

@Composable
fun rememberDemoSession(): AppSession {
    val context = LocalContext.current
    return remember { DemoSession.create(context) }
}

/**
 * DemoHost draws one screen the way the shell would, signed in to the invented server: the theme, the session, the
 * connection, the snackbars and a navigator, all answering from the parts' demo data. The screenshot tests of a part
 * put its screens in it:
 *
 *     composeRule.setContent { DemoHost { MailSection() } }
 *
 * To photograph the whole app with its drawer and bar, use `AppRoot(rememberDemoSession())` instead.
 */
@Composable
fun DemoHost(
    modifier: Modifier = Modifier,
    appearance: AppearanceChoice = AppearanceChoice.System,
    session: AppSession = rememberDemoSession(),
    content: @Composable () -> Unit,
) {
    val connection by session.connection.collectAsState()
    val signedIn = connection ?: error("the demo session is not signed in")
    TeaNodeTheme(appearance) {
        val snackbar = rememberSnackbarController(session)
        val navigator = AppNavigator(rememberNavController())
        CompositionLocalProvider(
            LocalAppSession provides session,
            LocalSnackbar provides snackbar,
            LocalConnection provides signedIn,
            LocalConnectionOrNull provides signedIn,
            LocalAppNavigator provides navigator,
        ) {
            Surface(modifier.fillMaxSize()) { content() }
        }
    }
}
