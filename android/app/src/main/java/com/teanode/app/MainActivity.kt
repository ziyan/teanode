package com.teanode.app

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.teanode.app.demo.DemoServer
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.ConnectLink
import com.teanode.kit.appRedirectScheme
import kotlinx.coroutines.flow.MutableSharedFlow

/**
 * MainActivity is the app's one window. It hands the links Android opens it with to the session: the server's sign-in
 * page returning with a code (`com.teanode.app:/oauth`), a connect code (`https://teanode.com/app/connect#...`), and the
 * app's own places (`teanode://app/...`).
 */
class MainActivity : ComponentActivity() {
    private val newIntents = MutableSharedFlow<Intent>(extraBufferCapacity = 8)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val application = application as TeaNodeApplication
        // Demo mode answers from invented data instead of a server, for screenshots and trying a screen without one.
        val isDemo = BuildConfig.DEBUG && intent.getBooleanExtra(DemoServer.EXTRA_DEMO, false)
        if (isDemo) application.enterDemo()
        val session = application.session
        session.openSignInPage = { address -> openWebPage(this, address, isEphemeral = true) }
        if (savedInstanceState == null) handle(intent)
        val startRoute = if (isDemo) intent.getStringExtra(DemoServer.EXTRA_ROUTE) else null
        setContent { AppRoot(session = session, startRoute = startRoute, newIntents = newIntents) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handle(intent)
    }

    private fun handle(intent: Intent) {
        val address: Uri = intent.data ?: return
        val session = (application as TeaNodeApplication).session
        when {
            address.scheme == appRedirectScheme -> session.completeSignIn(address.toString())
            address.scheme == "https" -> ConnectLink.parse(address.toString())?.let { link -> session.open(link) }
            address.scheme == "teanode" -> newIntents.tryEmit(intent)
        }
    }
}
