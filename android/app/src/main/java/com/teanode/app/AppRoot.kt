package com.teanode.app

import android.content.Intent
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.calls.CallPresenter
import com.teanode.app.calls.LocalCallCenter
import com.teanode.app.notifications.PushEffects
import com.teanode.app.session.AppSession
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.shell.AppShell
import com.teanode.app.signin.ConfirmConnectionSheet
import com.teanode.app.signin.SignInRoute
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.rememberSnackbarController
import com.teanode.app.ui.theme.TeaNodeTheme
import kotlinx.coroutines.flow.Flow

/**
 * AppRoot is the whole app: the theme, the sign-in screen while nobody is signed in, the shell once somebody is, the
 * sign-in shown over the shell to add an account, and the question whether a connect code is the right account.
 */
@Composable
fun AppRoot(session: AppSession, startRoute: String? = null, newIntents: Flow<Intent>? = null) {
    val appearance by session.appearance.collectAsState()
    TeaNodeTheme(appearance) {
        val snackbar = rememberSnackbarController(session)
        val connection by session.connection.collectAsState()
        val isAddingAccount by session.isAddingAccount.collectAsState()
        val pending by session.pendingConnection.collectAsState()
        // The calls of the phone. Demo mode has none: nothing there is a real call.
        val callCenter = if (session.isDemo) null else (LocalContext.current.applicationContext as? TeaNodeApplication)?.push?.calls
        CompositionLocalProvider(
            LocalAppSession provides session,
            LocalSnackbar provides snackbar,
            LocalConnectionOrNull provides connection,
            LocalCallCenter provides callCenter,
        ) {
            Surface(Modifier.fillMaxSize()) {
                val signedIn = connection
                if (signedIn == null) {
                    SignInRoute()
                } else {
                    // The whole app is drawn again for another account.
                    key(signedIn.accountKey) {
                        CompositionLocalProvider(LocalConnection provides signedIn) {
                            if (!session.isDemo) PushEffects(signedIn)
                            CallPresenter { AppShell(startRoute = startRoute, newIntents = newIntents) }
                            com.teanode.app.calls.CallOfferDialog()
                        }
                    }
                    if (isAddingAccount) {
                        BackHandler { session.stopAddingAccount() }
                        Surface(Modifier.fillMaxSize()) { SignInRoute(isAdding = true, onCancel = { session.stopAddingAccount() }) }
                    }
                }
                pending?.let { waiting ->
                    ConfirmConnectionSheet(waiting) { isConfirmed -> if (isConfirmed) session.confirm(waiting) else session.decline(waiting) }
                }
            }
        }
    }
}
